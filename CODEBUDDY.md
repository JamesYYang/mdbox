# CODEBUDDY.md

This file provides guidance to CodeBuddy Code when working with code in this repository.

## What this is

mdbox is a self-hosted Markdown knowledge base. Documents are plain `.md` files with YAML frontmatter on disk; the server is only a view over them. No database. Humans use the Web UI, agents use MCP, both share one store.

## Commands

```bash
go build -o mdbox .                      # build single binary (web assets embedded)
go run . -addr :8080 -data ./data        # run Web + REST + MCP(HTTP)
go run . -stdio -data ./data             # run as MCP stdio server for a local agent
MDBOX_TOKEN=xxx go run .                 # override the agent token (default comes from config.yaml)
go run . -config ./config.yaml           # explicit config path (default ./config.yaml)
gofmt -l .                               # formatting check (no linter config in repo)
go test ./...                            # currently no test files exist
```

On first run a `config.yaml` is created (admin / `mdbox@111!!!` + random `secret`). It is gitignored; `config.example.yaml` is the committed template.

There are no automated tests in this repository. Verification is done manually via curl against the REST API and the MCP tools (see PRD §7 for the smoke-test checklist).

## Architecture

Single Go module (`mdbox`), layered and wired entirely in `main.go`:

```
main.go            flags, routing, embed web/, stdio-vs-HTTP branch
  internal/store   persistence + in-memory metadata index (the core)
  internal/api     REST handlers over *store.Store
  internal/mcp     MCP tool definitions over the same *store.Store
  internal/render  goldmark Markdown -> HTML
  internal/config  config.yaml (admin credentials + share secret)
  internal/auth    in-memory login sessions
  web/             vanilla JS/CSS, embedded with //go:embed web
  web/vendor/      html2pdf.bundle.min.js (local, for PDF export)
```

Key facts that span multiple files:

- **One store, two frontends.** `main.go:42` builds a single `*store.Store` and passes it to both `api.New` and `mcpsrv.New`. REST and MCP are two interfaces onto identical state; any new capability belongs in `store` first.
- **Filesystem is the source of truth.** `internal/store/store.go` keeps a `map[string]*entry` index in memory, rebuilt from disk at startup (`reload`). When a lookup misses, `Get` reloads before failing, so files added/removed externally are picked up on the next request. `Doc.Content` is only populated by `Get`, not by `List`.
- **Document ID = filename.** `newID()` produces `{YYYYMMDD}-{12 hex chars}.md`; the ID stored in JSON is the basename without `.md`. Moving between `docs/` and `archive/` changes `Status` but not the ID.
- **Frontmatter is the persistence format.** `loadFile` / `readBody` / `serialize` parse and emit the `---` YAML block. `CreateFromRaw` (used by upload) honors a file's existing frontmatter title/tags/category, falling back to the filename.
- **Archive is a soft delete** (file moves to `data/archive/`); `Delete` is the only real removal. `List` defaults to `status=active` unless `all` is requested.

### Auth

Two independent mechanisms over one store:

- **Web UI** logs in with a username/password (no registration). Credentials live in `config.yaml` (`internal/config`), auto-created on first run with `admin` / `mdbox@111!!!` plus a random `secret`. Login (`POST /api/login`) issues an in-memory session token in an HttpOnly cookie `mdbox_session` (`internal/auth`); sessions are lost on restart. `logout`/`me` round it out.
- **Agents/scripts** use a single shared token, resolved in `main.go` as `-token` flag / `MDBOX_TOKEN` env → else `config.yaml`'s `token` (auto-generated on first run). `/mcp` is gated only by this token (`withToken` in `main.go`).

`withAuth` in `internal/api/api.go` wraps `/api/`: it passes through `POST /api/login`, `POST /api/logout`, and `GET /api/share/...`, then accepts **either** a valid session cookie **or** the shared token. So the same `/api/*` serves both the logged-in browser and token-carrying agents; empty token just means agents can't authenticate. The `/mcp` handler builds a **new** `NewStreamableHTTPServer` on every request.

### Sharing

Per-document opt-in flag `shared` (frontmatter + `Doc.Shared`, toggled via `POST /api/docs/{id}/share`). A shared doc is readable anonymously at `GET /api/share/{id}/{sig}` and `/s/{id}/{sig}` (which serves `index.html`; the SPA enters read-only share mode by pathname, with its own topbar: brand, theme toggle, and `.md`/PDF download). Markdown download is `GET /api/share/{id}/{sig}/download`. `sig = HMAC-SHA256(secret, id)[:24]` is deterministic, so the link is stable; changing `secret` invalidates every existing link, and unsharing makes it 404.

### Rendering & frontend

Markdown is rendered **server-side** (`internal/render`) so the frontend has zero dependencies and needs no CDN — important for the target (China) network environment. `WithUnsafe()` permits inline HTML, so only expose this to trusted users. `web/app.js` is a single IIFE with a global `state` object. The content area defaults to **read-only preview**; an Edit button switches to the two-pane source+preview mode, whose live preview calls `POST /api/preview` (debounced 300ms). The only bundled third-party JS is `web/vendor/html2pdf.bundle.min.js` (used for client-side PDF export via `exportPdf()`), which is a canvas slicer rather than a real layout engine — pagination is controlled by `pagebreak: { mode: ['css','legacy'], avoid: [...] }` in `app.js` plus `break-inside`/`break-after` rules on `.pdf-export` children in `style.css`. Both are needed to stop text lines being cut across pages.

Horizontal clipping (a half character cut off at the right edge) is a **width** problem, not a wrapping one: html2pdf's `toContainer` puts the cloned element into a container whose width is `pageSize.inner.width` (A4 − margins ≈ **718px**), and html2canvas clips at that container edge. So `.pdf-export` must have `width:auto` (never a fixed px width wider than ~718) and must carry the `markdown` class (otherwise tables lose their borders and look unrendered) plus wrapping rules (`overflow-wrap:anywhere`, `pre{white-space:pre-wrap}`, `table{width:100%;table-layout:fixed}`, `img{max-width:100%}`) so intrinsic-width content can't overflow. Do **not** "fix" clipping by overriding `html2canvas.width`/`scrollX`/`scrollY` in `exportPdf()` — html2canvas locates the element by document coordinates and needs its default `scrollX`/`scrollY` (page scroll offset), so pinning them to 0 yields a blank capture. `main.go` embeds `web/` via `//go:embed`, so **frontend changes require a rebuild** to take effect.

### Search

`store.List` does naive case-insensitive substring matching over title, joined tags, and — when `Query` is set — the file body re-read from disk. Not indexed; fine for the intended scale (README: ~10k docs max).

## Data layout & backup

```
data/
├── docs/*.md       active documents
└── archive/*.md    archived documents
```

`data/` is meant to be its own git repo for versioning/off-site backup. `scripts/git-backup.sh <data_dir>` commits and pushes, run from crontab. It no-ops on a clean tree and tolerates push failure (local commit is kept). `data/` and the `mdbox` binary are gitignored.

## Conventions

- Code comments and user-facing strings are in Chinese; match this when editing.
- Product scope is deliberately narrow — see `docs/PRD.md` §2.3 "明确非目标" before adding features (no owned embeddings, no built-in chat UI, no DB, no WYSIWYG, no account system). New functionality should favor "agent writes in via MCP" over manual upload flows.
