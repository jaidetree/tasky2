// Command tasky is the Tasky HTTP service: it runs migrations and serves the
// REST API (and, in production, the built frontend).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/jaidetree/tasky2/backend/internal/config"
	"github.com/jaidetree/tasky2/backend/internal/db"
	"github.com/jaidetree/tasky2/backend/internal/server"
	"github.com/jaidetree/tasky2/backend/internal/task"
)

func main() {
	dumpOpenAPI := flag.Bool("openapi", false, "write the generated OpenAPI spec to stdout and exit")
	flag.Parse()

	if *dumpOpenAPI {
		if err := writeOpenAPI(os.Stdout); err != nil {
			log.Fatalf("tasky: %v", err)
		}
		return
	}

	if err := run(); err != nil {
		log.Fatalf("tasky: %v", err)
	}
}

func run() error {
	cfg := config.Load()
	ctx := context.Background()

	pool, err := db.Connect(ctx, "")
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}

	store := task.NewStore(pool)
	svc := task.NewService(store, cfg.MaxInProgress)
	handler := server.New(svc, cfg.PublicDir)

	addr := net.JoinHostPort(cfg.Host, cfg.Port)
	log.Printf("tasky listening on http://%s", addr)
	return http.ListenAndServe(addr, handler)
}

// writeOpenAPI emits the spec generated from the huma handlers. It needs no DB
// or service, since generating the spec never invokes the handler functions.
func writeOpenAPI(w *os.File) error {
	_, api := server.NewAPI(nil)
	spec, err := api.OpenAPI().YAML()
	if err != nil {
		return fmt.Errorf("generate openapi: %w", err)
	}
	_, err = w.Write(spec)
	return err
}
