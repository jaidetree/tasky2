package server_test

// HTTP-level integration tests: they drive real HTTP requests against the
// running handlers backed by a real Postgres test database, asserting on both
// the response and the resulting DB state. This is the repo's first test
// pattern (see the PRD's testing decisions) — assert external behaviour a
// frontend would observe, not internals.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jaidetree/tasky2/backend/internal/db"
	"github.com/jaidetree/tasky2/backend/internal/server"
	"github.com/jaidetree/tasky2/backend/internal/task"
)

// harness is a running server plus a handle on its database for state asserts.
type harness struct {
	client  *http.Client
	baseURL string
	pool    *pgxpool.Pool
}

// newHarness brings up the full stack against a freshly truncated test database.
func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()

	pool := connectTestDB(t, ctx)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE tasks RESTART IDENTITY"); err != nil {
		t.Fatalf("truncate tasks: %v", err)
	}

	svc := task.NewService(task.NewStore(pool), 1)
	srv := httptest.NewServer(server.New(svc, ""))

	t.Cleanup(func() {
		srv.Close()
		pool.Close()
	})

	return &harness{client: srv.Client(), baseURL: srv.URL, pool: pool}
}

// connectTestDB ensures the test database exists, then returns a pool to it.
func connectTestDB(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	testDB := envOr("TASKY_TEST_DB", "tasky_test")

	admin, err := db.Connect(ctx, "dbname=postgres")
	if err != nil {
		t.Skipf("no Postgres available for integration tests: %v", err)
	}
	defer admin.Close()

	var exists bool
	if err := admin.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", testDB,
	).Scan(&exists); err != nil {
		t.Fatalf("check test db: %v", err)
	}
	if !exists {
		if _, err := admin.Exec(ctx, "CREATE DATABASE "+testDB); err != nil {
			t.Fatalf("create test db: %v", err)
		}
	}

	pool, err := db.Connect(ctx, "dbname="+testDB)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	return pool
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// --- request helpers -------------------------------------------------------

func (h *harness) post(t *testing.T, path string, body any) *http.Response {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	resp, err := h.client.Post(h.baseURL+path, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func (h *harness) get(t *testing.T, path string) *http.Response {
	t.Helper()
	resp, err := h.client.Get(h.baseURL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("decode response: %v (body: %s)", err, body)
	}
	return v
}

type taskResponse struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Notes     string `json:"notes"`
	Status    string `json:"status"`
	Position  int    `json:"position"`
	CreatedAt string `json:"created_at"`
}

type listResponse struct {
	Active []taskResponse `json:"active"`
}

// --- tests -----------------------------------------------------------------

// Create → the task appears Pending in the active list, with the response and
// the persisted DB row both reflecting it.
func TestCreateTaskAppearsPending(t *testing.T) {
	h := newHarness(t)

	resp := h.post(t, "/tasks", map[string]string{
		"title": "Wash the dishes",
		"notes": "include the pans soaking in the sink",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	created := decode[taskResponse](t, resp)

	if created.ID == 0 {
		t.Errorf("created task has zero id")
	}
	if created.Title != "Wash the dishes" {
		t.Errorf("title = %q, want %q", created.Title, "Wash the dishes")
	}
	if created.Notes != "include the pans soaking in the sink" {
		t.Errorf("notes = %q, want the provided notes", created.Notes)
	}
	if created.Status != "pending" {
		t.Errorf("status = %q, want pending", created.Status)
	}
	if created.CreatedAt == "" {
		t.Errorf("created_at not set")
	}

	// Response: it shows up in the active list as Pending.
	list := decode[listResponse](t, h.get(t, "/tasks"))
	if len(list.Active) != 1 {
		t.Fatalf("active list has %d tasks, want 1", len(list.Active))
	}
	if list.Active[0].ID != created.ID {
		t.Errorf("active[0].id = %d, want %d", list.Active[0].ID, created.ID)
	}
	if list.Active[0].Status != "pending" {
		t.Errorf("active[0].status = %q, want pending", list.Active[0].Status)
	}

	// DB state: exactly one live row, pending, not soft-deleted.
	var (
		count     int
		status    string
		deletedAt *string
	)
	if err := h.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM tasks").Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("tasks row count = %d, want 1", count)
	}
	if err := h.pool.QueryRow(context.Background(),
		"SELECT status, deleted_at FROM tasks WHERE id = $1", created.ID,
	).Scan(&status, &deletedAt); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if status != "pending" {
		t.Errorf("persisted status = %q, want pending", status)
	}
	if deletedAt != nil {
		t.Errorf("persisted deleted_at = %v, want NULL", *deletedAt)
	}
}

// Notes are optional: a create with only a title succeeds and stores empty notes.
func TestCreateTaskNotesOptional(t *testing.T) {
	h := newHarness(t)

	resp := h.post(t, "/tasks", map[string]string{"title": "Take out the bins"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	created := decode[taskResponse](t, resp)
	if created.Notes != "" {
		t.Errorf("notes = %q, want empty", created.Notes)
	}
}

// Title is required: an empty title is rejected by validation with no row written.
func TestCreateTaskRequiresTitle(t *testing.T) {
	h := newHarness(t)

	resp := h.post(t, "/tasks", map[string]string{"title": ""})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("create status = %d, want 422", resp.StatusCode)
	}
	resp.Body.Close()

	list := decode[listResponse](t, h.get(t, "/tasks"))
	if len(list.Active) != 0 {
		t.Errorf("active list has %d tasks, want 0", len(list.Active))
	}
}

// A whitespace-only title is rejected (it would slip past huma's minLength but
// trims to empty), and nothing is persisted.
func TestCreateTaskRejectsBlankTitle(t *testing.T) {
	h := newHarness(t)

	resp := h.post(t, "/tasks", map[string]string{"title": "   "})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("create status = %d, want 422", resp.StatusCode)
	}
	resp.Body.Close()

	var count int
	if err := h.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM tasks").Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 0 {
		t.Errorf("tasks row count = %d, want 0", count)
	}
}

// The active list shows only Pending + In Progress, excludes completed and
// soft-deleted (cancelled) tasks, and is ordered by position. This exercises
// the ListActive store query's filter and ordering directly.
func TestListActiveFiltersAndOrders(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	seed := func(title, status string, position int, completed, deleted bool) {
		_, err := h.pool.Exec(ctx,
			`INSERT INTO tasks (title, status, position, completed_at, deleted_at)
			 VALUES ($1, $2, $3,
			         CASE WHEN $4 THEN now() ELSE NULL END,
			         CASE WHEN $5 THEN now() ELSE NULL END)`,
			title, status, position, completed, deleted)
		if err != nil {
			t.Fatalf("seed %q: %v", title, err)
		}
	}

	// Out-of-order positions to prove ORDER BY position, not insertion order.
	seed("second", "pending", 2, false, false)
	seed("first", "pending", 1, false, false)
	seed("third", "in_progress", 3, false, false)
	seed("finished", "completed", 0, true, false)  // excluded: completed
	seed("cancelled", "pending", 0, false, true)   // excluded: soft-deleted

	list := decode[listResponse](t, h.get(t, "/tasks"))

	gotTitles := make([]string, len(list.Active))
	for i, task := range list.Active {
		gotTitles[i] = task.Title
	}
	want := []string{"first", "second", "third"}
	if len(gotTitles) != len(want) {
		t.Fatalf("active titles = %v, want %v", gotTitles, want)
	}
	for i := range want {
		if gotTitles[i] != want[i] {
			t.Errorf("active[%d] = %q, want %q", i, gotTitles[i], want[i])
		}
	}
}

// An empty pool returns an empty active list (not null), so clients can render it.
func TestListEmptyPool(t *testing.T) {
	h := newHarness(t)

	list := decode[listResponse](t, h.get(t, "/tasks"))
	if list.Active == nil {
		t.Errorf("active = nil, want empty array")
	}
	if len(list.Active) != 0 {
		t.Errorf("active has %d tasks, want 0", len(list.Active))
	}
}
