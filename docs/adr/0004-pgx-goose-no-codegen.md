# Persistence: raw pgx + goose, no ORM or codegen

The `store` (repository) package talks to Postgres with hand-written `pgx`
queries; migrations are plain SQL run by `goose`. No ORM, no query codegen.

A throwaway prototype (`proto/pgxstore/store.go`) showed the only real cost of
raw `pgx` — manual row scanning — collapses to a single shared `scan` helper
behind a `scanner` interface that fits both `pgx.Row` and `pgx.Rows`, reused by
every query. The code stays plain Go that a newcomer can read top to bottom.

**Considered and rejected:** `sqlc` (generates typed Go from SQL). It removes
the scanning boilerplate but adds a codegen step and the "don't edit the
generated file" footgun — knowledge overhead that outweighs the benefit for a
one-table domain where scanning is already trivial. `sqlc` remains an easy
future swap precisely because all SQL lives behind the `store` package.

**Consequence:** query/schema mismatches surface at runtime, so each query is
covered by integration tests against a real Postgres. Stores should translate
`pgx.ErrNoRows` from `RETURNING` updates (e.g. a Pick that matched no Pending
row) into a domain error the API maps to a validation response.
