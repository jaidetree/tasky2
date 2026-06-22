# Go backend, REST+OpenAPI contract, replaceable frontend

Tasky is a polyglot split: a **Go backend** and a **TypeScript frontend** that
is an explicit prototype, to be replaced later by a ClojureScript
implementation. The backend, the API contract, and the database schema are the
durable artifacts where the project's quality bar lives; the TS frontend is
disposable and should be kept minimal.

Because the frontend will be swapped, the contract between them must be
language-neutral. We use **REST over HTTP with JSON, described by an OpenAPI
spec**, generated **code-first** from the Go handlers (handlers are the source of
truth; the spec falls out of them). Each frontend generates a typed client from
that spec.

**Considered and rejected:** TypeScript end-to-end (rejected — author prefers
not to run a TS server, and a single language buys little once the frontend is
slated to become ClojureScript); tRPC (TS-coupled, would die with the
prototype); GraphQL (overkill for a handful of endpoints).

**Consequence:** no compile-time shared types across the boundary; type safety
there comes from the generated OpenAPI clients, not a shared language. The
backend stays frontend-agnostic and must not depend on any frontend-specific
tooling.
