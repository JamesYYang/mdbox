# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

mdbox is a self-hosted Markdown knowledge base. Documents are plain `.md` files with YAML frontmatter on disk; the server is only a view over them. No database. Humans use the Web UI, agents use MCP, both share one store.

## Commands

```bash
go build -o mdbox .                      # build single binary (web assets embedded)
go run . -addr :8080 -data ./data        # run Web + REST + MCP(HTTP)
go run . -stdio -user alice -data ./data # run as MCP stdio server for a local agent, serving user alice
go run . -config ./config.yaml -users ./users.yaml   # explicit config / user registry paths
gofmt -l .                               # formatting check (no linter config in repo)
go test ./...                            # only internal/users has tests
```

`web/` is embedded via `//go:embed`, so **frontend changes (app.js/style.css/index.html) require a rebuild/restart** to take effect.

Automated tests only cover `internal/users`. Everything else is verified manually: curl against the REST API and the MCP tools (see `docs/PRD.md` §7 for the smoke-test checklist).

On first run a `config.yaml` is created (random `secret`, `allow_register: true`); there is no seeded/default account — users register themselves. Both are gitignored; `config.example.yaml` is the committed template.

## Architecture

Single Go module (`mdbox`), layered and wired entirely in `main.go` (flags, routing, embed, stdio-vs-HTTP branch):

- `internal/store` — persistence + in-memory metadata index (the core); one instance per user
- `internal/users` — `users.yaml` registry, bcrypt passwords, per-user tokens, user → `*store.Store`
- `internal/api` — REST handlers over `*store.Store`
- `internal/mcp` — MCP tool definitions over the same `*store.Store`
- `internal/render` — goldmark Markdown → HTML
- `internal/config` — `config.yaml` (share secret, `allow_register`)
- `internal/auth` — in-memory login sessions
- `web/` — vanilla JS/CSS single-page UI; `web/vendor/html2pdf.bundle.min.js` is the only bundled third-party JS

Key facts that span multiple files:

- **One store per user, two frontends.** `users.Registry` hands out a `*store.Store` per user (`data/users/{name}/`); REST (via request context) and MCP (via a per-user cached server) both resolve to it. REST and MCP are two interfaces onto identical state; any new capability belongs in `store` first.
- **Filesystem is the source of truth.** `store.go` keeps a `map[string]*entry` index in memory, rebuilt from disk at startup (`reload`). When a lookup misses, `Get` reloads before failing, so files added/removed externally are picked up on the next request. `Doc.Content` is only populated by `Get`, not by `List`.
- **Document ID = filename.** `newID()` produces `{YYYYMMDD}-{12 hex chars}.md`; the ID in JSON is the basename without `.md`. Moving between `docs/` and `archive/` changes `Status` but not the ID.
- **Frontmatter is the persistence format.** `loadFile` / `readBody` / `serialize` parse and emit the `---` YAML block. `CreateFromRaw` (used by upload) honors a file's existing frontmatter title/tags/category, falling back to the filename.
- **Archive is a soft delete** (file moves to `data/archive/`); `Delete` is the only real removal. `List` defaults to `status=active` unless `all` is requested.
- **Search** is naive case-insensitive substring matching over title, joined tags, and (when `Query` is set) the body re-read from disk. Not indexed; fine for the intended scale (~10k docs).

### Auth & users

Multi-user, still no database. Both mechanisms resolve a request to a **username**, and everything after that only touches that user's own documents:

- **Registry**: `internal/users` keeps `users.yaml` (path via `-users`, deliberately **outside** `data/` so password hashes/tokens never get git-backed-up): `username`, bcrypt `password_hash`, per-user `token`, `created`. Writes are tmp-file + rename under a mutex. `Registry.Store(name)` lazily builds one `store.Store` per user rooted at `data/users/{name}/`; it refuses unknown users so public paths can't create directories. Usernames are `^[a-z0-9][a-z0-9_-]{2,31}$` (they become directory names, so this is also the path-traversal guard), minus Windows reserved device names.
- **Bootstrap**: no default/seeded user; accounts come only from registration (`allow_register`). Old `config.yaml` files with an `admin:` block are still accepted (the field is ignored). The old shared `token` / `-token` / `MDBOX_TOKEN` are gone.
- **Web UI**: `POST /api/login` / `POST /api/register` (the latter gated by `config.allow_register`, per-IP limit of 5/hour, then auto-login) issue an in-memory session in HttpOnly cookie `mdbox_session` (`internal/auth`); sessions are lost on restart. `GET /api/me` returns the user + their token; `POST /api/me/token` rotates it.
- **Agents/scripts**: the user's own token as `Authorization: Bearer` (or `?token=`). `/mcp` in `main.go` maps token → user → a cached per-user `MCPServer`; no valid token means 401 (the old empty-token pass-through is gone). Note `/mcp` still builds a new `NewStreamableHTTPServer` per request.

`withAuth` (`internal/api/server.go`) passes through `POST /api/login`, `/api/register`, `/api/logout` and `GET /api/share/...`, otherwise `identify` accepts a valid session cookie **or** a user token, then puts the user's `*store.Store` in the request context. Handlers fetch it with `stOf(r)` / `userOf(r)` — never hold a global store. `-stdio` requires `-user <name>` (must exist in `users.yaml`).

### Sharing

Per-document opt-in flag `shared` (frontmatter + `Doc.Shared`, toggled via `POST /api/docs/{id}/share`). A shared doc is readable anonymously at `GET /api/share/{user}/{id}/{sig}` and `/s/{user}/{id}/{sig}` (serves `index.html`; the SPA enters read-only share mode by pathname). `sig = HMAC-SHA256(secret, user+"/"+id)[:24]` is deterministic, so links are stable; changing `secret` invalidates every link, and unsharing makes it 404. A forged `user` fails the signature check.

### Rendering & frontend

Markdown is rendered **server-side** (`internal/render`) so the frontend has zero dependencies and needs no CDN (target is the China network environment). `WithUnsafe()` permits inline HTML, so only expose this to trusted users. `web/app.js` is a single IIFE with a global `state` object. The content area defaults to read-only preview; Edit switches to source+preview panes whose live preview calls `POST /api/preview` (debounced 300ms).

The document page is routed by hash (`#/doc/<id>`) so the browser Back/Forward/reload work: `openDoc` pushes the entry, `popstate` → `route()` shows the doc or the list, and the in-app Back button calls `history.back()` when the entry was pushed (`state.pushed`) or `replaceState`s otherwise. Leaving an edit with unsaved changes via Back re-pushes the doc URL and asks for confirmation first. Share pages use the pathname (`/s/...`), not the hash.

PDF export (`exportPdf()` in `app.js`) uses html2pdf, a canvas slicer rather than a layout engine:

- Pagination is controlled by `pagebreak: { mode: ['css','legacy'], avoid: [...] }` in `app.js` **plus** `break-inside`/`break-after` rules on `.pdf-export` children in `style.css`; both are needed to stop text lines being cut across pages.
- Right-edge clipping is a **width** problem: html2pdf's container is `pageSize.inner.width` (A4 − margins ≈ **718px**) and html2canvas clips there. So `.pdf-export` must have `width:auto` (never a fixed px wider than ~718), carry the `markdown` class (else tables lose borders), and have wrapping rules (`overflow-wrap:anywhere`, `pre{white-space:pre-wrap}`, `table{width:100%;table-layout:fixed}`, `img{max-width:100%}`).
- Do **not** fix clipping by overriding `html2canvas.width`/`scrollX`/`scrollY` — html2canvas locates the element by document coordinates and needs the default scroll offsets; pinning them to 0 yields a blank capture.

## Data layout & backup

`data/users/{username}/` holds `docs/*.md` (active) and `archive/*.md` per user, and `data/` is meant to be its own git repo. `scripts/git-backup.sh <data_dir>` commits and pushes from crontab; it no-ops on a clean tree and tolerates push failure. `data/`, `config.yaml`, `users.yaml`, and the `mdbox` binary are gitignored. The service is stateful and single-instance (in-memory index and sessions).

## Conventions

- Code comments, commit messages (conventional-commit prefixes like `docs:`/`chore:`), and user-facing strings are in Chinese; match this when editing.
- Product scope is deliberately narrow — see `docs/PRD.md` §2.3 "明确非目标" before adding features (no owned embeddings, no built-in chat UI, no DB, no WYSIWYG, no roles/permissions or cross-user sharing). Favor "agent writes in via MCP" over manual upload flows.
- `README.md` documents the REST API table and MCP config; update it (and `docs/PRD.md` when behavior changes) alongside API changes.
- `CODEBUDDY.md` is the equivalent guide for CodeBuddy Code; keep the two in sync if you change shared facts.
