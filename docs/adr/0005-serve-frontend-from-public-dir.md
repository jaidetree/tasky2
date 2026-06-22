# Serve the frontend from a public/ directory, not an embedded binary

In production the Go service serves the built frontend as static files from a
`public/` directory on the same host/origin as the API. `make build` builds the
Vite frontend into `public/`; `make run` runs the Go binary serving `public/`
plus the API. In development, `make dev` runs the Go API and the Vite dev server
(which proxies API calls), so this only governs production.

**Considered and rejected:** embedding `frontend/dist` into the Go binary with
`//go:embed` for a single self-contained artifact. Rejected because it makes the
production frontend opaque. Serving real files on disk means you can open the JS
in production and add logging or debug statements to chase a frontend issue —
valuable for a self-hosted personal tool. (Note this differs from migrations,
which remain embedded via `//go:embed`.)

**Consequence:** production is two artifacts to deploy (the binary + `public/`),
not one. Acceptable for single-host self-hosting.
