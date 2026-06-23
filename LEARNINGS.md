# Project Learnings

## Patterns That Work

- [2026-06-23] Atomic state transitions: a single guarded `UPDATE tasks SET ... WHERE <preconditions> RETURNING`, mapping `pgx.ErrNoRows` → an exported domain error (e.g. `ErrPickRejected`) → a huma 4xx (Pick uses 409 Conflict) → client toast. The precondition check and the write happen in one statement, so they can't race; no explicit tx needed. Reuse for Complete/Cancel/Move. Never return 500 for a domain rejection.
- [2026-06-23] Backend is one `task` concern module, not layered: `task.go` (domain), `service.go` (rules + exported domain errors), `store.go` (raw pgx SQL via shared `scanTask`/`taskColumns`), `handlers.go` (huma HTTP + DTO mapping). Add new operations to these four files.
- [2026-06-23] HTTP-seam integration tests in `backend/internal/server/integration_test.go` against real Postgres, asserting BOTH the response and the DB row state. Harness is parameterized: `newHarness(t)` defaults `maxInProgress=1`; `newHarnessWithLimit(t, n)` for raised limits. Rejection tests assert no DB state change.
- [2026-06-23] Expose server config the UI needs in the relevant response body (e.g. `max_in_progress` in `GET /tasks`) rather than adding a separate endpoint — keeps the disposable frontend to one fetch. `Service.MaxInProgress()` accessor feeds the handler.
- [2026-06-23] Emergent-but-fair animation draw (ADR-0002): compute the winner up-front as `(start + steps) % n` where `steps` is drawn uniformly from a window of EXACTLY `n` consecutive values — this makes every residue (every Pool member) equally likely. Apply ease-out deceleration to step TIMING only (a per-step `setTimeout` delay that grows), never to which index is chosen, so the easing can't bias the draw. The "roll" is genuinely undecided until it plays, yet uniform. Motion via `import { motion } from "motion/react"`; the outdent is a `motion.li` shifting `x` on the highlighted row. Guard against mid-animation re-trigger with a ref + disabled button.

## Mistakes to Avoid

- [2026-06-23] Editor/LSP TypeScript diagnostics go STALE after `src/api/schema.d.ts` is regenerated and can report phantom "property does not exist" errors. Trust `cd frontend && npm run build` (it runs `gen:api` → `tsc --noEmit` → `vite build` in order), not the inline diagnostics.
- [2026-06-23] Don't hand-edit `backend/api/openapi.yaml` — huma handlers are the source of truth; regenerate with `make openapi`.

## Domain Knowledge

- [2026-06-23] Stack: Go + huma on net/http (OpenAPI generated code-first), raw pgx + goose over Postgres 14, no ORM/codegen. Frontend is a disposable Vite+React+TS prototype with a typed client generated from the OpenAPI spec.
- [2026-06-23] After any backend API change, regenerate the frontend client; `npm run build` does this first via `gen:api` (`openapi-typescript ../backend/api/openapi.yaml -o src/api/schema.d.ts`).
- [2026-06-23] Vocabulary (CONTEXT.md): Pick / Pending / In Progress / Completed / Pool / Cancel (soft delete). Avoid draw/roll/select/todo/done. A Task flows Pending → In Progress → Completed once; Cancel is an orthogonal soft delete (`deleted_at`) from any status.
- [2026-06-23] Tasks are sliced as GitHub issues: #1 PRD; #2 Slice 1 (DONE, walking skeleton); #3 Slice 2 (DONE, Pick endpoint + plain button); #4 Slice 3 (DONE, Pick animation). Remaining: #5 Slice 4 complete + recently-completed (rolling 24h), #6 Slice 5 cancel, #7 Slice 6 edit, #8 Slice 7 reorder.
- [2026-06-23] Frontend "feel" slices (e.g. the Pick animation) have NO automated test surface (PRD out-of-scope; ADR-0002 says distribution isn't asserted). The /iterate loop can only mechanically verify build/deps/code-exists/committed; the actual feel needs manual `make dev` verification by the user.
- [2026-06-23] Postgres must be running for `go test` (tests skip if no Postgres). Commits: one slice = one commit, conventional 50/70 subject, `Co-Authored-By: Claude Opus 4.8` trailer; work goes directly on `main`.

## Open Questions

## Consolidated Principles
