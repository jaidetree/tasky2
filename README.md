# Tasky

Personal, single-user web app holding a pool of tasks; a button plays an
animation that picks one at random to do. See [`spec.md`](./spec.md) for the
functional + structural spec, [`CONTEXT.md`](./CONTEXT.md) for the domain
glossary, and [`docs/adr/`](./docs/adr/) for the decisions behind the design.

## Stack

- **Backend** — Go, `huma` on stdlib `net/http` (OpenAPI generated code-first
  from the handlers), raw `pgx` + `goose` over Postgres 14 (no ORM/codegen).
- **Frontend** — disposable Vite + React + TS prototype, typed client generated
  from the OpenAPI spec.
- **Toolchain** — pinned by the Nix flake (Postgres 14, Go, Node, goose).

## Setup

The dev shell provides every tool. With [direnv](https://direnv.net) it loads
automatically; otherwise run `nix develop`.

```sh
direnv allow          # one-time, loads the Nix dev shell + PG* env vars
scripts/db init       # initialize and start the local Postgres data dir
scripts/db start      # start Postgres (if not already running)
```

Postgres connection comes from the standard `PG*` libpq vars set in `.envrc`
(`PGHOST`, `PGPORT`, `PGDATABASE=tasky_dev`, …).

## Run

```sh
make dev      # Go API on :8080 + Vite dev server (proxies /tasks to Go)
make build    # build the Go binary + frontend into public/
make run      # production: Go binary serving public/ + the API
make test     # backend HTTP-integration tests (needs Postgres running)
make openapi  # regenerate backend/api/openapi.yaml from the handlers
```

In dev, open the Vite URL it prints (default <http://localhost:5173>). In
production, `make run` serves everything from <http://127.0.0.1:8080>.

## Config

| Env var                 | Default     | Meaning                                  |
| ----------------------- | ----------- | ---------------------------------------- |
| `TASKY_HOST`            | `127.0.0.1` | Bind address; loopback-only by default (ADR-0001). Set to expose on a network. |
| `TASKY_PORT`            | `8080`      | HTTP port.                               |
| `TASKY_MAX_IN_PROGRESS` | `1`         | Concurrency limit (scales to 3).         |
| `TASKY_PUBLIC_DIR`      | `public`    | Built frontend directory served in production (ADR-0005). |
| `PG*`                   | from `.envrc` | Postgres connection (libpq vars).      |

## Layout

```
backend/   cmd/tasky/ (entrypoint), internal/task/ (domain+store+service+handlers),
           internal/{db,config,server}/ (infrastructure), db/migrations/ (embedded SQL),
           api/openapi.yaml (generated)
frontend/  Vite + React + TS prototype; generated typed client in src/api/
public/    built frontend, served in production (git-ignored)
```

## Tests

HTTP-level integration tests drive real requests against the running handlers
backed by a real Postgres test database (`tasky_test`, created on demand),
asserting on both responses and resulting DB state. See
`backend/internal/server/integration_test.go`.
