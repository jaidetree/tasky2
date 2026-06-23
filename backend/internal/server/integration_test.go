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
	"strconv"
	"testing"
	"time"

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

// newHarness brings up the full stack against a freshly truncated test database
// with the default In-Progress limit of 1.
func newHarness(t *testing.T) *harness {
	return newHarnessWithLimit(t, 1)
}

// newHarnessWithLimit is newHarness parameterized by the In-Progress limit, so
// tests can exercise the configurable limit at both the default 1 and a raised
// value (the PRD requires verifying both).
func newHarnessWithLimit(t *testing.T, maxInProgress int) *harness {
	t.Helper()
	ctx := context.Background()

	pool := connectTestDB(t, ctx)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE tasks RESTART IDENTITY"); err != nil {
		t.Fatalf("truncate tasks: %v", err)
	}

	svc := task.NewService(task.NewStore(pool), maxInProgress)
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
	ID          int64   `json:"id"`
	Title       string  `json:"title"`
	Notes       string  `json:"notes"`
	Status      string  `json:"status"`
	Position    int     `json:"position"`
	CreatedAt   string  `json:"created_at"`
	CompletedAt *string `json:"completed_at"`
}

type listResponse struct {
	Active            []taskResponse `json:"active"`
	RecentlyCompleted []taskResponse `json:"recently_completed"`
	OlderCompleted    []taskResponse `json:"older_completed"`
	MaxInProgress     int            `json:"max_in_progress"`
}

// containsID reports whether any task in the list has the given id.
func containsID(tasks []taskResponse, id int64) bool {
	for _, t := range tasks {
		if t.ID == id {
			return true
		}
	}
	return false
}

// idsOf returns the ids of a task list in order, for asserting ordering.
func idsOf(tasks []taskResponse) []int64 {
	ids := make([]int64, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	return ids
}

// statusOf reads the persisted status of a task directly from the DB, for
// asserting that a rejected pick left state unchanged.
func (h *harness) statusOf(t *testing.T, id int64) string {
	t.Helper()
	var status string
	if err := h.pool.QueryRow(context.Background(),
		"SELECT status FROM tasks WHERE id = $1", id).Scan(&status); err != nil {
		t.Fatalf("read status of task %d: %v", id, err)
	}
	return status
}

// seedTask inserts a task directly with the given status/position and returns
// its id, bypassing the API so picks can be set up from any starting state.
func (h *harness) seedTask(t *testing.T, title, status string, position int) int64 {
	t.Helper()
	var id int64
	if err := h.pool.QueryRow(context.Background(),
		`INSERT INTO tasks (title, status, position) VALUES ($1, $2, $3) RETURNING id`,
		title, status, position).Scan(&id); err != nil {
		t.Fatalf("seed %q: %v", title, err)
	}
	return id
}

// seedCompleted inserts a Completed task whose completed_at is `ago` before now
// (a Postgres interval literal, e.g. "1 hour", "25 hours"), so the rolling-24h
// split can be exercised across the boundary without waiting.
func (h *harness) seedCompleted(t *testing.T, title, ago string) int64 {
	t.Helper()
	var id int64
	if err := h.pool.QueryRow(context.Background(),
		`INSERT INTO tasks (title, status, completed_at)
		 VALUES ($1, 'completed', now() - $2::interval) RETURNING id`,
		title, ago).Scan(&id); err != nil {
		t.Fatalf("seed completed %q: %v", title, err)
	}
	return id
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
	seed("finished", "completed", 0, true, false) // excluded: completed
	seed("cancelled", "pending", 0, false, true)  // excluded: soft-deleted

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

// The list response carries the server-enforced In-Progress limit so the
// frontend can disable Pick at the limit; it reflects the configured value.
func TestListReportsMaxInProgress(t *testing.T) {
	h := newHarnessWithLimit(t, 3)

	list := decode[listResponse](t, h.get(t, "/tasks"))
	if list.MaxInProgress != 3 {
		t.Errorf("max_in_progress = %d, want 3", list.MaxInProgress)
	}
}

// --- pick ------------------------------------------------------------------

func pickPath(id int64) string {
	return "/tasks/" + strconv.FormatInt(id, 10) + "/pick"
}

// Pick a Pending task → it becomes In Progress, in both the response and the DB.
func TestPickPendingBecomesInProgress(t *testing.T) {
	h := newHarness(t)
	id := h.seedTask(t, "Wash the dishes", "pending", 1)

	resp := h.post(t, pickPath(id), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pick status = %d, want 200", resp.StatusCode)
	}
	picked := decode[taskResponse](t, resp)
	if picked.ID != id {
		t.Errorf("picked id = %d, want %d", picked.ID, id)
	}
	if picked.Status != "in_progress" {
		t.Errorf("response status = %q, want in_progress", picked.Status)
	}
	if got := h.statusOf(t, id); got != "in_progress" {
		t.Errorf("persisted status = %q, want in_progress", got)
	}
}

// Pick at the In-Progress limit is rejected with 409 (a client toast, not a
// 500) and leaves the Pending task untouched.
func TestPickAtLimitRejected(t *testing.T) {
	h := newHarness(t) // limit 1
	h.seedTask(t, "already going", "in_progress", 1)
	pending := h.seedTask(t, "waiting", "pending", 2)

	resp := h.post(t, pickPath(pending), nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("pick status = %d, want 409", resp.StatusCode)
	}
	resp.Body.Close()
	if got := h.statusOf(t, pending); got != "pending" {
		t.Errorf("persisted status = %q, want pending (no state change)", got)
	}
}

// With a raised limit the same scenario succeeds: a second pick is allowed up
// to the limit, then the third is rejected — proving the limit is configurable.
func TestPickRespectsRaisedLimit(t *testing.T) {
	h := newHarnessWithLimit(t, 3)
	first := h.seedTask(t, "one", "pending", 1)
	second := h.seedTask(t, "two", "pending", 2)
	third := h.seedTask(t, "three", "pending", 3)
	fourth := h.seedTask(t, "four", "pending", 4)

	for _, id := range []int64{first, second, third} {
		resp := h.post(t, pickPath(id), nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("pick %d status = %d, want 200", id, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// Fourth pick is over the limit of 3 → rejected, left Pending.
	resp := h.post(t, pickPath(fourth), nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("over-limit pick status = %d, want 409", resp.StatusCode)
	}
	resp.Body.Close()
	if got := h.statusOf(t, fourth); got != "pending" {
		t.Errorf("persisted status = %q, want pending (no state change)", got)
	}
}

// Pick on an empty Pool (no such task / nothing eligible) is rejected with 409.
func TestPickEmptyPoolRejected(t *testing.T) {
	h := newHarness(t)

	resp := h.post(t, pickPath(999), nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("pick status = %d, want 409", resp.StatusCode)
	}
	resp.Body.Close()
}

// Pick of a non-Pending task (In Progress or Completed) is rejected with 409
// and the task keeps its status — the transition runs once, server-validated.
func TestPickNonPendingRejected(t *testing.T) {
	for _, status := range []string{"in_progress", "completed"} {
		t.Run(status, func(t *testing.T) {
			h := newHarnessWithLimit(t, 3) // headroom, so only the status guard applies
			id := h.seedTask(t, "already "+status, status, 1)

			resp := h.post(t, pickPath(id), nil)
			if resp.StatusCode != http.StatusConflict {
				t.Fatalf("pick status = %d, want 409", resp.StatusCode)
			}
			resp.Body.Close()
			if got := h.statusOf(t, id); got != status {
				t.Errorf("persisted status = %q, want %q (no state change)", got, status)
			}
		})
	}
}

// --- complete --------------------------------------------------------------

func completePath(id int64) string {
	return "/tasks/" + strconv.FormatInt(id, 10) + "/complete"
}

// Complete an In-Progress task → it becomes Completed with completed_at stamped
// (response + DB), leaves the active list, and shows up in recently-completed.
func TestCompleteInProgressBecomesCompleted(t *testing.T) {
	h := newHarness(t)
	id := h.seedTask(t, "Wash the dishes", "in_progress", 1)

	resp := h.post(t, completePath(id), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("complete status = %d, want 200", resp.StatusCode)
	}
	completed := decode[taskResponse](t, resp)
	if completed.Status != "completed" {
		t.Errorf("response status = %q, want completed", completed.Status)
	}
	if completed.CompletedAt == nil || *completed.CompletedAt == "" {
		t.Errorf("response completed_at not stamped")
	}

	// DB: status completed and completed_at non-null.
	var (
		status      string
		completedAt *time.Time
	)
	if err := h.pool.QueryRow(context.Background(),
		"SELECT status, completed_at FROM tasks WHERE id = $1", id,
	).Scan(&status, &completedAt); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if status != "completed" {
		t.Errorf("persisted status = %q, want completed", status)
	}
	if completedAt == nil {
		t.Errorf("persisted completed_at is NULL, want a timestamp")
	}

	// It leaves the active list and appears in recently-completed.
	list := decode[listResponse](t, h.get(t, "/tasks"))
	if containsID(list.Active, id) {
		t.Errorf("completed task %d still in active list", id)
	}
	if !containsID(list.RecentlyCompleted, id) {
		t.Errorf("completed task %d not in recently_completed", id)
	}
}

// The rolling-24h split: tasks completed within the last 24h are in
// recently_completed (newest-first); a task completed >24h ago falls out of
// recently_completed and into older_completed.
func TestCompletedRollingWindowSplit(t *testing.T) {
	h := newHarness(t)
	recentNew := h.seedCompleted(t, "an hour ago", "1 hour")
	recentOld := h.seedCompleted(t, "three hours ago", "3 hours")
	older := h.seedCompleted(t, "a day and an hour ago", "25 hours")

	list := decode[listResponse](t, h.get(t, "/tasks"))

	// recently_completed holds both within-window tasks, newest-first.
	if got, want := idsOf(list.RecentlyCompleted), []int64{recentNew, recentOld}; !equalIDs(got, want) {
		t.Errorf("recently_completed = %v, want %v (newest-first)", got, want)
	}
	// older_completed holds the >24h task, and only it.
	if got, want := idsOf(list.OlderCompleted), []int64{older}; !equalIDs(got, want) {
		t.Errorf("older_completed = %v, want %v", got, want)
	}
	// The boundary: the >24h task must not leak into recently_completed.
	if containsID(list.RecentlyCompleted, older) {
		t.Errorf("task completed >24h ago leaked into recently_completed")
	}
}

// Completing a task that is not In Progress (Pending or already Completed) is
// rejected with 409 (a client toast, not a 500) and leaves state unchanged.
func TestCompleteNonInProgressRejected(t *testing.T) {
	for _, status := range []string{"pending", "completed"} {
		t.Run(status, func(t *testing.T) {
			h := newHarness(t)
			id := h.seedTask(t, "not in progress", status, 1)

			resp := h.post(t, completePath(id), nil)
			if resp.StatusCode != http.StatusConflict {
				t.Fatalf("complete status = %d, want 409", resp.StatusCode)
			}
			resp.Body.Close()
			if got := h.statusOf(t, id); got != status {
				t.Errorf("persisted status = %q, want %q (no state change)", got, status)
			}
		})
	}
}

// --- cancel ----------------------------------------------------------------

func cancelPath(id int64) string {
	return "/tasks/" + strconv.FormatInt(id, 10)
}

// del issues a DELETE request, the verb Cancel (soft delete) is registered on.
func (h *harness) del(t *testing.T, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, h.baseURL+path, nil)
	if err != nil {
		t.Fatalf("build DELETE %s: %v", path, err)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	return resp
}

// deletedAtOf reads the persisted deleted_at directly from the DB, for asserting
// the soft-delete stamp (or its absence after a rejected cancel).
func (h *harness) deletedAtOf(t *testing.T, id int64) *time.Time {
	t.Helper()
	var deletedAt *time.Time
	if err := h.pool.QueryRow(context.Background(),
		"SELECT deleted_at FROM tasks WHERE id = $1", id).Scan(&deletedAt); err != nil {
		t.Fatalf("read deleted_at of task %d: %v", id, err)
	}
	return deletedAt
}

// Cancel a Pending task → 200 with the cancelled DTO, deleted_at stamped in the
// DB, and the task gone from the active list.
func TestCancelPendingSoftDeletes(t *testing.T) {
	h := newHarness(t)
	id := h.seedTask(t, "Wash the dishes", "pending", 1)

	resp := h.del(t, cancelPath(id))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status = %d, want 200", resp.StatusCode)
	}
	cancelled := decode[taskResponse](t, resp)
	if cancelled.ID != id {
		t.Errorf("cancelled id = %d, want %d", cancelled.ID, id)
	}

	if h.deletedAtOf(t, id) == nil {
		t.Errorf("persisted deleted_at is NULL, want a timestamp")
	}

	list := decode[listResponse](t, h.get(t, "/tasks"))
	if containsID(list.Active, id) {
		t.Errorf("cancelled task %d still in active list", id)
	}
}

// Cancel an In-Progress task → 200 and gone from the active list (Cancel is
// allowed from any status).
func TestCancelInProgressRemovesFromActive(t *testing.T) {
	h := newHarness(t)
	id := h.seedTask(t, "already going", "in_progress", 1)

	resp := h.del(t, cancelPath(id))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	if h.deletedAtOf(t, id) == nil {
		t.Errorf("persisted deleted_at is NULL, want a timestamp")
	}

	list := decode[listResponse](t, h.get(t, "/tasks"))
	if containsID(list.Active, id) {
		t.Errorf("cancelled task %d still in active list", id)
	}
}

// Cancel a Completed task → 200 and gone from BOTH completed views, recently and
// older (Cancel filters from every view).
func TestCancelCompletedRemovesFromCompletedViews(t *testing.T) {
	h := newHarness(t)
	recent := h.seedCompleted(t, "an hour ago", "1 hour")
	older := h.seedCompleted(t, "a day and an hour ago", "25 hours")

	for _, id := range []int64{recent, older} {
		resp := h.del(t, cancelPath(id))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("cancel %d status = %d, want 200", id, resp.StatusCode)
		}
		resp.Body.Close()
		if h.deletedAtOf(t, id) == nil {
			t.Errorf("task %d: persisted deleted_at is NULL, want a timestamp", id)
		}
	}

	list := decode[listResponse](t, h.get(t, "/tasks"))
	if containsID(list.RecentlyCompleted, recent) {
		t.Errorf("cancelled task %d still in recently_completed", recent)
	}
	if containsID(list.OlderCompleted, older) {
		t.Errorf("cancelled task %d still in older_completed", older)
	}
}

// Cancel of a missing id → 404, and (for an already-cancelled id) a second
// cancel → 404 with no unexpected DB change (deleted_at stays at its first
// stamp; the cancelled task never reappears in any list).
func TestCancelMissingOrAlreadyCancelledRejected(t *testing.T) {
	h := newHarness(t)

	// Missing id → 404.
	resp := h.del(t, cancelPath(999))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cancel of missing id status = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()

	// Cancel once, capture the stamp, then cancel again → 404, stamp unchanged.
	id := h.seedTask(t, "to cancel twice", "pending", 1)
	if r := h.del(t, cancelPath(id)); r.StatusCode != http.StatusOK {
		t.Fatalf("first cancel status = %d, want 200", r.StatusCode)
	} else {
		r.Body.Close()
	}
	firstStamp := h.deletedAtOf(t, id)
	if firstStamp == nil {
		t.Fatalf("after first cancel deleted_at is NULL, want a timestamp")
	}

	resp = h.del(t, cancelPath(id))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("second cancel status = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()

	if got := h.deletedAtOf(t, id); got == nil || !got.Equal(*firstStamp) {
		t.Errorf("deleted_at changed on rejected re-cancel: got %v, want %v", got, firstStamp)
	}
}

// equalIDs reports whether two id slices are equal in order.
func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
