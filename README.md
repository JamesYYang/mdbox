# mdbox

English · [中文](docs/README-ZH.md)

A self-hosted **Markdown document manager**: documents produced by agents flow in automatically, while you read, edit, organize, tag, upload, download, and share them from any browser — and any agent can read and write the same library through **MCP**.

Design principle: **a document is always a plain `.md` file with YAML frontmatter; the tool is just a view over it.** There is no database in the storage layer — everything lands in a directory on disk, and git handles versioning and off-site backup.

## Features

- **Web UI**
  - **Login / registration**: multi-user, self-service sign-up (disable with `allow_register: false` in `config.yaml`); each user's documents are isolated
  - **Bilingual UI**: English / Chinese, switchable from the top bar, the login page, and the share page; defaults to your browser language
  - **Top bar**: product name + logo, MCP access (a dialog shows **your own** token and connection config, and can reset it), light/dark theme switch, account info and sign-out
  - **Sidebar**: categories / tags / archive, with one-click filtering
  - **Card list**: full-text search; hovering a card reveals a delete button (permanent delete, after a confirmation)
  - **Reading & editing**: read-only preview by default; click Edit for a two-pane source + live-preview editor where you can change the title/category/tags and archive the document
  - **Upload / download**: drag or pick multiple `.md` files; from preview you can download Markdown or **export PDF**
- **Sharing**: enable sharing per document to get a **stable, deterministically signed link**; anyone with the link can read it without signing in, and you can turn it off at any time
- **REST API**: login/registration, document CRUD, search, tag/category aggregation, multipart upload, Markdown rendering, sharing
- **MCP server**: `list_docs`, `search_docs`, `read_doc`, `write_doc`, `update_doc`, `list_tags`, `archive_doc`, over HTTP (Streamable) or stdio
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
https://<host>/s/<user>/<id>/<sig>      # sig = HMAC-SHA256(secret, "<user>/<id>")[:24]
```

`sig` is derived deterministically from `secret`, so the same document always has **the same URL**; changing `secret` invalidates every shared link at once, and turning sharing off makes the link return 404. The share page is read-only.

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
| GET | `/api/docs?tag=&category=&status=&q=&limit=` | List / search |
| POST | `/api/docs` | Create `{title, content, tags, category, source}` |
| GET | `/api/docs/{id}` | Read (with body and rendered HTML; includes shareUrl when shared) |
| PUT | `/api/docs/{id}` | Update `{title?, content?, tags?, category?}` |
| POST | `/api/docs/{id}/archive` | Archive (moves into archive; soft delete) |
| DELETE | `/api/docs/{id}` | Permanently delete (cannot be undone) |
| POST | `/api/docs/{id}/share` | `{shared:bool}` enable/disable sharing; returns the stable link |
| GET | `/api/docs/{id}/download` | Download the .md |
| GET | `/api/share/{user}/{id}/{sig}` | **Anonymous** read-only access (share) |
| GET | `/api/share/{user}/{id}/{sig}/download` | **Anonymous** .md download (share) |
| GET | `/api/tags` · `/api/categories` | Tag / category aggregation |
| POST | `/api/upload` | Multipart upload of multiple .md files |
| POST | `/api/preview` | `{content}` → `{html}` |
| POST | `/mcp` | MCP Streamable HTTP endpoint (user token only; exposes only that user's own documents) |

## Data & backup

```
data/users/<username>/
├── docs/*.md      active documents
└── archive/*.md   archived documents
```

Each user gets a directory; the whole `data/` is still a git repository, so the backup story is unchanged.

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
internal/config/       config.yaml (initial account / share secret / registration toggle)
internal/users/        users.yaml registry, password hashing, tokens, per-user Store
internal/auth/         in-memory login sessions
web/                   frontend (embedded in the binary, single-file deploy)
web/vendor/            local copy of html2pdf (for PDF export)
scripts/git-backup.sh  scheduled git backup of the data directory
```

## Known trade-offs (MVP)

- The metadata index is fully rebuilt from disk at startup; **if you add or remove files in the data directory externally, the service rebuilds on the next request.** Fine up to roughly 10k documents.
- Login sessions live in memory; a restart signs everyone out — run it as a **single instance**.
- Multi-user stops at "document isolation": no roles/permissions, no cross-user sharing, no password-recovery page (a forgotten password must be handled by hand).
- Preview rendering permits inline HTML (`WithUnsafe`), but the output goes through a whitelist sanitizer (bluemonday UGC): `<script>`, inline event handlers, `<iframe>` and `javascript:` links are stripped, while safe HTML such as `<details>`, `<br>` and tables is kept.
- Archiving is a soft delete; **deleting is permanent** and cannot be undone.
- PDF export uses client-side html2pdf to snapshot the page into images and slice them into pages; very long code blocks or huge tables can still paginate imperfectly.
- A "stale-document candidate list + automatic cleanup" is planned for the next phase.
