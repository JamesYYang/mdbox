# CODEBUDDY.md

This file provides guidance to CodeBuddy Code when working with code in this repository.

## What this is

mdbox is a self-hosted Markdown knowledge base. Documents are plain `.md` files with YAML frontmatter on disk; the server is only a view over them. No database. Humans use the Web UI, agents use MCP, both share one store.

## Commands

```bash
go build -o mdbox .                      # build single binary (web assets embedded)
go run . -addr :8080 -data ./data        # run Web + REST + MCP(HTTP)
go run . -stdio -user alice -data ./data # run as MCP stdio server for a local agent, serving user alice
go run . -config ./config.yaml -users ./users.yaml   # explicit config / user registry paths
gofmt -l .                               # formatting check (no linter config in repo)
go test ./...                            # tests live in internal/users, internal/api, internal/render, internal/i18n
```

On first run a `config.yaml` is created (random `secret`, `allow_register: true`); there is no seeded/default account — users register themselves. Both are gitignored; `config.example.yaml` is the committed template.

Automated tests cover `internal/users`, `internal/api`, `internal/i18n` and `internal/render` (the HTML sanitizer — treat it as the XSS regression gate). `store`, `mcp` and the end-to-end flows are verified manually via curl against the REST API and the MCP tools (see PRD §7 for the smoke-test checklist).

## Architecture

Single Go module (`mdbox`), layered and wired entirely in `main.go`:

```
main.go            flags, routing, embed web/, stdio-vs-HTTP branch
  internal/store   persistence + in-memory metadata index (the core); one instance per user
  internal/users   users.yaml registry, bcrypt passwords, per-user tokens, user -> *store.Store
  internal/api     REST handlers over *store.Store
  internal/mcp     MCP tool definitions over the same *store.Store
  internal/render  goldmark Markdown -> HTML
  internal/config  config.yaml (share secret, allow_register)
  internal/auth    in-memory login sessions
  web/             vanilla JS/CSS, embedded with //go:embed web
  web/vendor/      html2pdf.bundle.min.js (local, for PDF export)
```

Key facts that span multiple files:

- **One store per user, two frontends.** `users.Registry` hands out a `*store.Store` per user (`data/users/{name}/`); REST (via request context) and MCP (via a per-user cached server) both resolve to it. REST and MCP are two interfaces onto identical state; any new capability belongs in `store` first.
- **Filesystem is the source of truth.** `internal/store/store.go` keeps a `map[string]*entry` index in memory, rebuilt from disk at startup (`reload`). When a lookup misses, `Get` reloads before failing, so files added/removed externally are picked up on the next request. `Doc.Content` is only populated by `Get`, not by `List`.
- **Document ID = filename.** `newID()` produces `{YYYYMMDD}-{12 hex chars}.md`; the ID stored in JSON is the basename without `.md`. Moving between `docs/` and `archive/` changes `Status` but not the ID.
- **Frontmatter is the persistence format.** `loadFile` / `readBody` / `serialize` parse and emit the `---` YAML block. `CreateFromRaw` (used by upload) honors a file's existing frontmatter title/tags/category, falling back to the filename.
- **Archive is a soft delete** (file moves to `data/archive/`; `Unarchive` moves it back); `Delete` is the only real removal. `List` defaults to `status=active` unless `all` is requested.
- **Writes are bounded.** `main.go`'s `limitBody` caps every request body at 32 MB; `store` rejects (before touching disk) any body that isn't valid UTF-8 or contains a NUL (`ErrNotText` — this is what stops a binary renamed to `.md`), any doc over `store.MaxDocBytes` (2 MB) and any write that would push the user past `store.MaxUserBytes` (200 MB, archived files included). Those two are constants, not config — the error text in `internal/i18n` mirrors their numbers. `api.storeErrStatus` maps the store errors to 404/413/415 (a `*http.MaxBytesError` becomes 413 in `writeBadReq`); MCP goes through the same `store` calls, so agents hit the same wall and get the English text from `errText`.
- **Images are content-addressed.** `store.SaveImage` sniffs the first 512 bytes, accepts only PNG / JPEG / GIF / WebP (SVG and everything else → `ErrUnsupportedImage`, 415), caps one image at `store.MaxImageBytes` (5 MB, `ErrImageTooLarge`, 413), and stores it as `assets/<first 16 bytes of sha256 as hex>.<ext>`. Identical bytes dedupe to one file and one quota charge. `usedBytes` sums `docs` + `archive` + `assets`, so the 200 MB ceiling is shared. `GET /a/{user}/{name}` (`api.AssetHandler`, wired in `main.go` on the mux rather than under `/api/`) deliberately bypasses `withAuth` — share pages must render images without a session — and 404s on any name failing `^[0-9a-f]{32}\.(png|jpg|gif|webp)$` or any unknown user; since the name is a content hash it answers `Cache-Control: immutable`. There is no GC: deleting a doc or rewriting its body leaves the image on disk and in the quota.

### Auth & users

Multi-user, still no database. Both mechanisms resolve a request to a **username**, and everything after that only touches that user's own documents:

- **Registry**: `internal/users` keeps `users.yaml` (path via `-users`, deliberately **outside** `data/` so password hashes/tokens never get git-backed-up): `username`, bcrypt `password_hash`, per-user `token`, `created`. Writes are tmp-file + rename under a mutex. `Registry.Store(name)` lazily builds one `store.Store` per user rooted at `data/users/{name}/`; it refuses unknown users so public paths can't create directories. Usernames are `^[a-z0-9][a-z0-9_-]{2,31}$` (they become directory names, so this is also the path-traversal guard), minus Windows reserved device names.
- **Bootstrap**: no default/seeded user; accounts come only from registration (`allow_register`). Old `config.yaml` files with an `admin:` block are still accepted (the field is ignored). The old shared `token` / `-token` / `MDBOX_TOKEN` are gone.
- **Web UI**: `POST /api/login` / `POST /api/register` (the latter gated by `config.allow_register`, per-IP limit of 5/hour, then auto-login) issue an in-memory session in HttpOnly cookie `mdbox_session` (`internal/auth`); sessions are lost on restart. `GET /api/me` returns the user + their token; `POST /api/me/token` rotates it; `POST /api/me/password` changes the password (needs the old one) and drops the user's other sessions.
- **Agents/scripts**: the user's own token as `Authorization: Bearer` (or `?token=`). `/mcp` in `main.go` maps token → user → a cached per-user `MCPServer`; no valid token means 401 (the old empty-token pass-through is gone). Note `/mcp` still builds a new `NewStreamableHTTPServer` per request.

`withAuth` (`internal/api/server.go`) passes through `POST /api/login`, `/api/register`, `/api/logout` and `GET /api/share/...`, otherwise `identify` accepts a valid session cookie **or** a user token, then puts the user's `*store.Store` in the request context. Handlers fetch it with `stOf(r)` / `userOf(r)` — never hold a global store. `-stdio` requires `-user <name>` (must exist in `users.yaml`).

### Sharing

Per-document `share` has three states (`off | view | edit`); `Doc.Shared` is derived (`mode != off`). A shared doc is readable anonymously at `GET /api/share/{user}/{id}/{token}` and `/s/{user}/{id}/{token}` (serves `index.html`; the SPA enters read-only share mode by pathname). `sig = HMAC-SHA256(secret, user+"/"+id)[:24]` is deterministic, so links are stable; changing `secret` invalidates every link, and unsharing makes it 404. An archived document's links are dead: `sharedDoc` / `editableDoc` reject `Status == store.StatusArchived`, so read/download/clone/edit all 404 while archived and come back automatically after `Unarchive` (the `share` field is untouched). A forged `user` fails the signature check.

In `edit` mode any **logged-in** user holding the link can rewrite the body in place via `PUT /api/share/{user}/{id}/{token}`, which targets the **owner's** store (`s.reg.Store(user)`), not `stOf(r)`. `publicPath` must therefore stay method-aware: only `GET` under `/api/share/` is anonymous — a bare path-prefix check would silently expose the write endpoint. Writes reuse `store.Update`, so size/text/quota limits still apply. There is no version history, no conflict detection, no per-person attribution, and the write counts against the owner's quota.

On the frontend the share dialog picks one of the three modes; in `edit` mode the share page shows an Edit button that switches in place to the two-pane editor (live preview) and returns to read-only after saving. Saving while logged out swaps in the login view and, after login, returns to the share page with the draft restored.

### Rendering & frontend

Markdown is rendered **server-side** (`internal/render`) so the frontend has zero dependencies and needs no CDN — important for the target (China) network environment. `WithUnsafe()` permits inline HTML, but the output is always run through a bluemonday UGC whitelist (`policy` in `internal/render/render.go`): doc bodies come from users/agents and every frontend sink assigns the result to `innerHTML`, so renderer output is treated as untrusted. `main.go`'s `securityHeaders` adds a CSP (`script-src 'self'`) plus `nosniff` on top as defence in depth. `web/app.js` is a single IIFE with a global `state` object. The content area defaults to **read-only preview**; an Edit button switches to the two-pane source+preview mode, whose live preview calls `POST /api/preview` (debounced 300ms). The editor inserts images three ways — paste, drag-drop and 「插入图片」 — all posting to `POST /api/assets` and inserting `![alt](/a/<user>/<name>)`; saving is refused while an upload is still in flight, and `exportPdf()` waits for every `<img>` to load or error before capture so images don't export blank. The only bundled third-party JS is `web/vendor/html2pdf.bundle.min.js` (used for client-side PDF export via `exportPdf()`), which is a canvas slicer rather than a real layout engine — pagination is controlled by `pagebreak: { mode: ['css','legacy'], avoid: [...] }` in `app.js` plus `break-inside`/`break-after` rules on `.pdf-export` children in `style.css`. Both are needed to stop text lines being cut across pages.

Horizontal clipping (a half character cut off at the right edge) is a **width** problem, not a wrapping one: html2pdf's `toContainer` puts the cloned element into a container whose width is `pageSize.inner.width` (A4 − margins ≈ **718px**), and html2canvas clips at that container edge. So `.pdf-export` must have `width:auto` (never a fixed px width wider than ~718) and must carry the `markdown` class (otherwise tables lose their borders and look unrendered) plus wrapping rules (`overflow-wrap:anywhere`, `pre{white-space:pre-wrap}`, `table{width:100%;table-layout:fixed}`, `img{max-width:100%}`) so intrinsic-width content can't overflow. Do **not** "fix" clipping by overriding `html2canvas.width`/`scrollX`/`scrollY` in `exportPdf()` — html2canvas locates the element by document coordinates and needs its default `scrollX`/`scrollY` (page scroll offset), so pinning them to 0 yields a blank capture. `main.go` embeds `web/` via `//go:embed`, so **frontend changes require a rebuild** to take effect.

### Search

`store.List` does naive case-insensitive substring matching over title, joined tags, and — when `Query` is set — the file body re-read from disk. Not indexed; fine for the intended scale (README: ~10k docs max).

## Data layout & backup

```
data/users/{username}/
├── docs/*.md             active documents
├── archive/*.md          archived documents
└── assets/<hash>.<ext>   images referenced from documents
```

`data/` is meant to be its own git repo for versioning/off-site backup (images are committed with the docs). `scripts/git-backup.sh <data_dir>` commits and pushes, run from crontab. It no-ops on a clean tree and tolerates push failure (local commit is kept). `data/`, `config.yaml`, `users.yaml`, and the `mdbox` binary are gitignored.

## Conventions

- Code comments and user-facing strings are in Chinese; match this when editing.
- Product scope is deliberately narrow — see `docs/PRD.md` §2.3 "明确非目标" before adding features (no owned embeddings, no built-in chat UI, no DB, no WYSIWYG, no roles/permissions, no multi-person co-editing — cross-user writes happen only through edit links the author explicitly issues). New functionality should favor "agent writes in via MCP" over manual upload flows.

The document page is routed by hash (`#/doc/<id>`) so the browser Back/Forward/reload work: `openDoc` pushes the entry, `popstate` → `route()` shows the doc or the list, and the in-app Back button calls `history.back()` when the entry was pushed (`state.pushed`) or `replaceState`s otherwise. Leaving an edit with unsaved changes via Back re-pushes the doc URL and asks for confirmation first. Share pages use the pathname (`/s/...`), not the hash.
