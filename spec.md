# Tasky — Spec

Personal, single-user web app holding a pool of tasks; a button plays an
animation that picks one at random to do. See [`CONTEXT.md`](./CONTEXT.md)
for the domain glossary and [`docs/adr/`](./docs/adr/) for the decisions behind
the choices below. This file is the functional + structural entry point; it
references those rather than repeating them.

## Functional requirements

### Task
- Fields the user enters: **title** (required, short), **notes** (optional).
- System fields: `id`, `status`, `position`, `created_at`, `completed_at`,
  `deleted_at`.
- Deliberately excluded: due dates, priority, tags, assignees, recurrence.

### Lifecycle
- Not recurring. Status flows Pending → In Progress → Completed, once.
- **Pick**: move a random Pending task to In Progress. The random draw happens
  client-side, emergent from the animation — see
  [ADR-0002](./docs/adr/0002-pick-is-the-roll-client-side.md).
- **Concurrency limit**: max In Progress tasks, **default 1**, must scale to 3.
  Configurable (see Config). Pick allowed only while `in_progress_count < limit`.
  **Server enforces** the limit inside the Pick transaction; client also disables
  the Pick control at the limit or when the pool is empty.
- **Complete**: In Progress → Completed, stamps `completed_at`.
- **Cancel**: soft delete via `deleted_at`, allowed from any status, filtered
  out of every view.
- **Edit**: title and notes are editable while Pending or In Progress, not once
  Completed.

### Main view (top → bottom)
1. **Active pool** — Pending + In Progress in one **manually ordered** list
   (persisted `position`, drag to reorder). In Progress tasks keep their
   position and are highlighted in place (not moved).
2. **Recently completed** — tasks with `completed_at > now − 24h` (rolling
   window, not calendar day), newest-first.
3. **Expand toggle** (collapsed by default) — older completed tasks,
   newest-first.

### Reorder
- API takes a **single move**: `taskId` + `newPosition`; the server shifts the
  affected range in one transaction. Integer positions, renumber on reorder.

### Pick flow
- Client holds the Pending list; the highlight-cycle animation (with an outdent
  for physicality) *is* the draw. On landing it auto-commits — no confirm — by
  POSTing to transition the chosen task.
- Server validates: task still Pending **and** under the limit. On rejection,
  return an error the UI shows as a toast.

## Data model

Single table `tasks` (seeded from the deleted prototype migration):

```sql
CREATE TABLE tasks (
    id           BIGSERIAL PRIMARY KEY,
    title        TEXT NOT NULL,
    notes        TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending','in_progress','completed')),
    position     INT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    deleted_at   TIMESTAMPTZ
);
```

Domain `Task` type must carry the timestamps (the prototype's omitted them) so
the 24h recently-completed split and any history view have what they need.

## API surface (REST + OpenAPI, code-first via huma)

- `GET /tasks` — active pool + recently-completed (server applies the 24h split
  and ordering, or returns enough for the client to).
- `POST /tasks` — create (title, notes).
- `PATCH /tasks/{id}` — edit title/notes.
- `POST /tasks/{id}/pick` — Pending → In Progress, limit-validated.
- `POST /tasks/{id}/complete` — In Progress → Completed.
- `DELETE /tasks/{id}` — cancel (soft delete).
- `POST /tasks/{id}/move` — reorder (single move to `newPosition`).

Exact shapes fall out of the Go handlers; the spec is generated, not authored —
see [ADR-0003](./docs/adr/0003-go-backend-rest-openapi-replaceable-frontend.md).

## Architecture & toolchain

- **Monorepo:**
  ```
  backend/   cmd/tasky/main.go, internal/task/ (domain+store+service+handlers),
             internal/db/ (pgx pool + goose runner), db/migrations/, api/openapi.yaml
  frontend/  Vite + React + TS; OpenAPI-generated client; Motion for the animation
  public/    built frontend, served in production
  ```
  Backend is organized **by concern** (`task`), not by technical layer.
- **Persistence:** raw `pgx` + `goose`, behind the `task` store; no ORM/codegen —
  [ADR-0004](./docs/adr/0004-pgx-goose-no-codegen.md). Migrations embedded via
  `//go:embed`.
- **Frontend served from `public/`**, not embedded —
  [ADR-0005](./docs/adr/0005-serve-frontend-from-public-dir.md).
- **Auth:** none; bind `127.0.0.1` by default —
  [ADR-0001](./docs/adr/0001-no-app-auth-bind-loopback.md).
- **Nix flake** pins Postgres 14 today; extend to also pin Go, Node, goose.

### Makefile targets
- `make dev` — run the Go API and the Vite dev server (Vite proxies API to Go).
- `make build` — build the Go binary and the Vite frontend into `public/`.
- `make run` — production: run the Go binary serving `public/` + the API.

### Config (env)
- `TASKY_HOST` — bind address; default `127.0.0.1` (ADR-0001).
- `TASKY_PORT` — HTTP port; default `8080`.
- `TASKY_MAX_IN_PROGRESS` — concurrency limit; default `1`.
- DB connection via standard `PG*` libpq vars (already set in `.envrc`).

## Testing
- Integration tests for every store query against a real Postgres (the safety
  net for hand-written SQL).
- Service-level tests for limit enforcement and the Pending/In-Progress/Completed
  transitions, including `pgx.ErrNoRows` → domain-error mapping.
- Pick *distribution* is not unit-testable by design (ADR-0002); everything after
  the animation lands is.

## Open / not yet decided
- `position` storage stays integer-renumber; revisit only if the pool grows huge.
- Empty-pool / at-limit UX copy.
- Animation fairness/bias tuning (ADR-0002).

## Security note
`.envrc` contains a live `ANTHROPIC_API_KEY` that must be rotated and moved out
of `.envrc` into an untracked secret. It is unused by the app design.
