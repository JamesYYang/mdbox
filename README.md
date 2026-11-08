# mdbox

English · [中文](docs/README-ZH.md)

A self-hosted **Markdown document manager**: documents produced by agents can be written straight into the library through **MCP**, while you read, edit, organize, tag, upload, download, and share them from any browser — humans and agents work on the same store.

Design principle: **a document is always a plain `.md` file with YAML frontmatter; the tool is just a view over it.** There is no database in the storage layer — everything lands in a directory on disk, and git handles versioning and off-site backup.

## Features

- **Web UI**
  - **Login / registration**: multi-user, self-service sign-up (disable with `allow_register: false` in `config.yaml`); each user's documents are isolated
  - **Bilingual UI**: English / Chinese, switchable from the top bar, the login page, and the share page; defaults to your browser language
  - **Top bar**: product name + logo, MCP access (a dialog shows **your own** token and connection config, and can reset it), light/dark theme switch, account menu with change-password and sign-out
  - **Sidebar**: categories / tags / archive, with one-click filtering
  - **Card list**: full-text search; hovering a card reveals a 「⋯」 menu with Archive / Delete (delete asks for confirmation)
  - **List overview**: a compact strip on the documents page shows the last 7 days — new / updated / archived / written by agents
  - **Reading & editing**: read-only preview by default; click Edit for a two-pane source + live-preview editor where you can change the title/category/tags. The 「⋯」 menu in the preview bar holds Archive / Delete
  - **Upload / download**: drag or pick multiple `.md` files; from preview you can download Markdown or **export PDF**
  - **Images**: paste, drag-drop or 「Insert image」 in the editor inserts `![alt](/a/<user>/<name>)`; images are content-addressed under `assets/`, read anonymously from `/a/` with long-lived caching, and count toward the 200 MB per-user quota (5 MB each, PNG / JPEG / GIF / WebP only)
- **Sharing**: enable sharing per document to get a **stable, deterministically signed link**, either **read-only** (anyone with the link, no sign-in) or **editable** (a signed-in visitor rewrites the body in place; title/tags/category stay unchanged). Turning it off makes the link 404 immediately. A signed-in visitor can also use **Copy to my library** to take an independent copy
- **REST API**: login/registration, document CRUD, search, tag/category aggregation, 7-day overview, multipart upload, Markdown rendering, sharing and cloning
- **MCP server**: `list_docs`, `search_docs`, `read_doc`, `write_doc`, `update_doc`, `list_tags`, `archive_doc`, `unarchive_doc`, `upload_image`, over HTTP (Streamable) or stdio
- **Auth**: the web app uses a login session (cookie); agents/scripts use a **per-user token**; either way, only that user's own documents are reachable
- **Automatic git backup**: the data directory is itself a git repository, committed and pushed on a schedule
- **Zero frontend dependencies**: preview is rendered server-side (goldmark) with no CDN; the only bundled third-party JS is a local copy of html2pdf (for PDF export)

## Quick start

Build requirement: Go 1.23+

```bash
go build -o mdbox .
./mdbox -addr :8080 -data ./data
# Open http://<server>:8080 and register the first user — there is no seeded account
```

On first run it creates **`config.yaml`** and **`users.yaml`** in the current directory (both are gitignored):

```yaml
# config.yaml
secret: "<randomly generated: signing key for share links; changing it invalidates every shared link>"
allow_register: true              # false disables self-service registration
```

- There is no seeded/default account. If you set `allow_register` to `false`, create your users while registration is still open — otherwise nobody will be able to sign in.
- `users.yaml` is the user registry (no database): username, **bcrypt password hash**, that user's MCP token, and creation time. It lives outside `data/`, so git backups never carry it.
- Use `-config` / `-users` to set the two paths; `config.example.yaml` is a template.

## Authentication

Two mechanisms, both resolving a request to "a user", after which only that user's documents under `data/users/<username>/` are reachable:

- **Web**: username / password login (`POST /api/login`) or registration (`POST /api/register`, which signs you in immediately), issuing an in-memory session cookie `mdbox_session`. Sessions live in memory, so **a process restart requires signing in again**.
- **Agent / scripts**: each user gets a token at registration; send it as `Authorization: Bearer <token>`. Both `/api/*` and `/mcp` accept it. You can view and reset the token in the web "MCP access" dialog (resetting invalidates the old token immediately).

Username rules: lowercase letters, digits, `_`, `-`, 3-32 characters; passwords 8-72 characters. At most 5 registrations per IP per hour.

## Sharing

Click Share in a document's preview. A link looks like:

```
https://<host>/s/<user>/<id>/<token>    # token = HMAC-SHA256(secret, "<user>/<id>")[:24]
```

The last path segment (`token`) is derived deterministically from `secret`, so the same document always has **the same URL**; changing `secret` invalidates every shared link at once, and turning sharing off makes the link return 404. A document can be shared **read-only** (`view`) or **editable** (`edit`): an editable link lets a signed-in visitor rewrite the body in place, while title, tags and category stay unchanged.

A signed-in visitor can click **Copy to my library** to create an independent copy of a shared document in their own library (title + body only, `source=clone`); the original is untouched, and this is a one-way copy rather than collaborative editing.

## MCP setup

HTTP mode (remote agents, e.g. CodeBuddy / Claude). The token belongs to your own account — view it under "MCP access" in the top bar; the agent can only reach your own documents:

```json
{
  "mcpServers": {
    "mdbox": {
      "url": "http://your-server:8080/mcp",
      "headers": { "Authorization": "Bearer <your-token>" }
    }
  }
}
```

stdio mode (local agents connect directly; no token, and `-user` picks which registered user to serve):

```json
{
  "mcpServers": {
    "mdbox": {
      "command": "/path/to/mdbox",
      "args": ["-stdio", "-user", "alice", "-data", "/path/to/data"]
    }
  }
}
```

## REST API reference

| Method | Path | Description |
|---|---|---|
| POST | `/api/login` | `{username,password}` login; issues a session cookie |
| POST | `/api/register` | `{username,password}` register and log in; generates the user's token (can be disabled via `allow_register`) |
| POST | `/api/logout` | Sign out |
| GET | `/api/me` | Current user and their token |
| POST | `/api/me/token` | Reset the current user's token |
| POST | `/api/me/password` | Change the current user's password, body `{"old","new"}`; invalidates other sessions on success |
| GET | `/api/health` | Health check |
| GET | `/api/overview?days=7` | Last N days: new / updated / archived / written by agents (1–90 days) |
| GET | `/api/docs?tag=&category=&status=&q=&limit=` | List / search |
| POST | `/api/docs` | Create `{title, content, tags, category, source}` |
| GET | `/api/docs/{id}` | Read (with body and rendered HTML; includes shareUrl when shared) |
| PUT | `/api/docs/{id}` | Update `{title?, content?, tags?, category?}` |
| POST | `/api/docs/{id}/archive` | Archive (moves into archive; soft delete) |
| DELETE | `/api/docs/{id}` | Permanently delete (cannot be undone) |
| POST | `/api/docs/{id}/share` | `{mode}` (`off` / `view` / `edit`) sets sharing; returns `{mode, url}` (`url` omitted for `off`) |
| GET | `/api/docs/{id}/download` | Download the .md |
| GET | `/api/share/{user}/{id}/{token}` | **Anonymous** read-only access (share) |
| GET | `/api/share/{user}/{id}/{token}/download` | **Anonymous** .md download (share) |
| PUT | `/api/share/{user}/{id}/{token}` | **Signed-in** rewrite of the shared document's body; only when `share_mode=edit`. Returns `{doc, html}`. Body only — title/tags/category unchanged; the write counts against the **owner's** quota |
| POST | `/api/clone/{user}/{id}/{token}` | **Signed-in** copy of a shared document into your own library (title + body only, `source=clone`) |
| GET | `/api/tags` · `/api/categories` | Tag / category aggregation |
| POST | `/api/upload` | Multipart upload of multiple .md files (UTF-8 text only, ≤2 MB each) |
| POST | `/api/assets` | Upload one image (multipart `file`, ≤5 MB, PNG / JPEG / GIF / WebP); returns `{url, name, bytes}` |
| GET | `/a/{user}/{name}` | **Anonymous** image bytes with `Cache-Control: immutable`; 404 for malformed or unknown names |
| POST | `/api/preview` | `{content}` → `{html}` |
| POST | `/mcp` | MCP Streamable HTTP endpoint (user token only; exposes only that user's own documents) |

## Data & backup

```
data/users/<username>/
├── docs/*.md              active documents
├── archive/*.md           archived documents
└── assets/<hash>.<ext>    images referenced from documents
```

Each user gets a directory; the whole `data/` is still a git repository, so the backup story is unchanged (images are committed along with documents).

Initialize the remote and add a crontab entry:

```bash
cd data && git init && git remote add origin git@github.com:<you>/<repo>.git
crontab: */10 * * * * /path/to/mdbox/scripts/git-backup.sh /path/to/mdbox/data
```

> Deployment note: mdbox is a **stateful, single-instance** service (in-memory index + in-memory sessions + files on disk) and needs a persistent disk mounted as `data/`. Its documents are **not** suited to object storage (S3/OSS) directly — this is ordinary filesystem I/O.

Full Azure deployment steps (GitHub Actions build → ACR → Container Apps + Azure Files persistent volume) are in [`docs/DEPLOY.md`](docs/DEPLOY.md).

## Directory layout

```
main.go                entry point (HTTP server / stdio mode), routing, embed web/
internal/store/        storage: frontmatter parsing, in-memory index, CRUD
internal/render/       Markdown → HTML (goldmark)
internal/api/          REST API and auth
internal/mcp/          MCP tool definitions
internal/config/       config.yaml (share secret / registration toggle; no seeded account)
internal/users/        users.yaml registry, password hashing, tokens, per-user Store
internal/auth/         in-memory login sessions
web/                   frontend (embedded in the binary, single-file deploy)
web/vendor/            local copy of html2pdf (for PDF export)
scripts/git-backup.sh  scheduled git backup of the data directory
```

## Known trade-offs

- The metadata index is fully rebuilt from disk at startup; **if you add or remove files in the data directory externally, the service rebuilds on the next request.** Fine up to roughly 10k documents.
- Login sessions live in memory; a restart signs everyone out — run it as a **single instance**.
- Multi-user stops at "document isolation": no roles/permissions, no multi-person co-editing (cross-user writes happen only through edit links the author explicitly issues), no password-recovery flow (changing a password requires the old one; a forgotten password must be handled by hand).
- Preview rendering permits inline HTML (`WithUnsafe`), but the output goes through a whitelist sanitizer (bluemonday UGC): `<script>`, inline event handlers, `<iframe>` and `javascript:` links are stripped, while safe HTML such as `<details>`, `<br>` and tables is kept.
- Archiving is a soft delete; **deleting is permanent** and cannot be undone.
- Writes are bounded: the body must be UTF-8 text (a binary renamed to `.md` is rejected with 415), max 2 MB per document, 200 MB per user (archived files and images included) and 32 MB per request. The thresholds are constants in `internal/store/store.go`.
- Images are content-addressed and never garbage-collected: deleting a document or rewriting its body leaves the image in `assets/` and still counted against the quota. Their URLs point at this site, so an exported `.md` loses its images, and unsharing a document does not revoke image access.
- PDF export uses client-side html2pdf to snapshot the page into images and slice them into pages; very long code blocks or huge tables can still paginate imperfectly.
