package task

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store holds Tasks in Postgres with hand-written pgx queries (no ORM, no
// codegen — see ADR-0004). Every query is covered by integration tests against
// a real Postgres, the safety net for the hand-written SQL.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by the given pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// scanner is the shared surface of pgx.Row and pgx.Rows so a single scanTask
// helper serves both single-row and multi-row queries (see ADR-0004).
type scanner interface {
	Scan(dest ...any) error
}

// taskColumns is the canonical column order scanTask expects.
const taskColumns = `id, title, notes, status, position, created_at, completed_at, deleted_at`

func scanTask(s scanner) (Task, error) {
	var t Task
	err := s.Scan(
		&t.ID,
		&t.Title,
		&t.Notes,
		&t.Status,
		&t.Position,
		&t.CreatedAt,
		&t.CompletedAt,
		&t.DeletedAt,
	)
	return t, err
}

// Create inserts a new Task. status/position/created_at fall to their column
// defaults, so a created Task is Pending at position 0.
func (s *Store) Create(ctx context.Context, title, notes string) (Task, error) {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO tasks (title, notes) VALUES ($1, $2)
		 RETURNING `+taskColumns,
		title, notes,
	)
	return scanTask(row)
}

// queryTasks runs a multi-row task query and scans every row into a non-nil
// (possibly empty) slice, so callers — and the clients beyond them — always see
// a list rather than null.
func (s *Store) queryTasks(ctx context.Context, sql string, args ...any) ([]Task, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// ListActive returns the active pool — Pending + In Progress, soft-deleted rows
// excluded — ordered by the manual position (id as a stable tiebreaker).
func (s *Store) ListActive(ctx context.Context) ([]Task, error) {
	return s.queryTasks(ctx,
		`SELECT `+taskColumns+` FROM tasks
		 WHERE deleted_at IS NULL
		   AND status IN ('pending', 'in_progress')
		 ORDER BY position ASC, id ASC`)
}

// ListRecentlyCompleted returns Tasks completed within the rolling 24h window
// (completed_at > now() - 24h), newest-first, soft-deleted rows excluded. The
// rolling boundary lives in SQL so it is exercised at the HTTP seam.
func (s *Store) ListRecentlyCompleted(ctx context.Context) ([]Task, error) {
	return s.queryTasks(ctx,
		`SELECT `+taskColumns+` FROM tasks
		 WHERE deleted_at IS NULL
		   AND status = 'completed'
		   AND completed_at > now() - interval '24 hours'
		 ORDER BY completed_at DESC`)
}

// ListOlderCompleted returns Tasks completed before the rolling 24h window
// (completed_at <= now() - 24h), newest-first, soft-deleted rows excluded —
// the history shown behind the collapsed expand toggle.
func (s *Store) ListOlderCompleted(ctx context.Context) ([]Task, error) {
	return s.queryTasks(ctx,
		`SELECT `+taskColumns+` FROM tasks
		 WHERE deleted_at IS NULL
		   AND status = 'completed'
		   AND completed_at <= now() - interval '24 hours'
		 ORDER BY completed_at DESC`)
}

// Pick atomically transitions a single Pending Task to In Progress, enforcing
// the In-Progress limit. The whole guard lives in one statement so the check
// and the write cannot race: the UPDATE's WHERE clause requires the target row
// to be Pending and live, and a correlated subquery requires the current live
// In-Progress count to be below maxInProgress. If no row qualifies, RETURNING
// yields no rows (pgx.ErrNoRows), which maps to ErrPickRejected.
func (s *Store) Pick(ctx context.Context, id int64, maxInProgress int) (Task, error) {
	row := s.pool.QueryRow(ctx,
		`UPDATE tasks
		    SET status = 'in_progress'
		  WHERE id = $1
		    AND status = 'pending'
		    AND deleted_at IS NULL
		    AND (SELECT count(*) FROM tasks
		          WHERE status = 'in_progress'
		            AND deleted_at IS NULL) < $2
		 RETURNING `+taskColumns,
		id, maxInProgress,
	)
	t, err := scanTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrPickRejected
	}
	return t, err
}

// Complete atomically transitions a single In-Progress Task to Completed and
// stamps completed_at. One guarded statement, mirroring Pick: the WHERE clause
// requires the row to be In Progress and live, so the check and the write
// cannot race. No qualifying row -> pgx.ErrNoRows -> ErrCompleteRejected.
func (s *Store) Complete(ctx context.Context, id int64) (Task, error) {
	row := s.pool.QueryRow(ctx,
		`UPDATE tasks
		    SET status = 'completed', completed_at = now()
		  WHERE id = $1
		    AND status = 'in_progress'
		    AND deleted_at IS NULL
		 RETURNING `+taskColumns,
		id,
	)
	t, err := scanTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrCompleteRejected
	}
	return t, err
}

// Cancel atomically soft-deletes a single Task by stamping deleted_at, allowed
// from any status. One guarded statement, mirroring Pick/Complete: the WHERE
// clause requires the row to still be live (deleted_at IS NULL), so a missing
// or already-cancelled row matches nothing -> pgx.ErrNoRows -> ErrCancelRejected.
func (s *Store) Cancel(ctx context.Context, id int64) (Task, error) {
	row := s.pool.QueryRow(ctx,
		`UPDATE tasks
		    SET deleted_at = now()
		  WHERE id = $1
		    AND deleted_at IS NULL
		 RETURNING `+taskColumns,
		id,
	)
	t, err := scanTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrCancelRejected
	}
	return t, err
}

// Edit atomically updates a Task's title and/or notes in one guarded statement,
// reusing the COALESCE trick so a nil pointer ($2/$3) leaves that field
// unchanged while an explicit empty-string notes clears notes. Lifecycle fields
// are untouched, so editing works in any status. The WHERE clause requires the
// row to still be live; a missing or cancelled row matches nothing ->
// pgx.ErrNoRows -> ErrTaskNotFound.
func (s *Store) Edit(ctx context.Context, id int64, title, notes *string) (Task, error) {
	row := s.pool.QueryRow(ctx,
		`UPDATE tasks
		    SET title = COALESCE($2, title),
		        notes = COALESCE($3, notes)
		  WHERE id = $1
		    AND deleted_at IS NULL
		 RETURNING `+taskColumns,
		id, title, notes,
	)
	t, err := scanTask(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrTaskNotFound
	}
	return t, err
}

// Move repositions one live active Task within the active ordering and renumbers
// the WHOLE active set to contiguous 0..n-1 positions in a single transaction —
// the first endpoint needing an explicit pgx transaction, because the renumber
// is a read-then-write that must be isolated. The active set is locked FOR
// UPDATE, the move is computed in Go (remove, clamp the target index, splice
// back in), then one bulk UPDATE writes every active row's new position. Only
// active rows are touched; completed/cancelled positions are left alone. A target
// id that is not in the locked active set rolls back and returns ErrMoveRejected.
func (s *Store) Move(ctx context.Context, id int64, position int) (Task, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx,
		`SELECT id FROM tasks
		  WHERE deleted_at IS NULL
		    AND status IN ('pending', 'in_progress')
		  ORDER BY position ASC, id ASC
		  FOR UPDATE`)
	if err != nil {
		return Task{}, err
	}
	current := make([]int64, 0)
	for rows.Next() {
		var rowID int64
		if err := rows.Scan(&rowID); err != nil {
			rows.Close()
			return Task{}, err
		}
		current = append(current, rowID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Task{}, err
	}

	// Remove the moved id from the current order; if it was never there, the
	// Task is missing, completed, or cancelled — not a live active task.
	remaining := make([]int64, 0, len(current))
	found := false
	for _, rowID := range current {
		if rowID == id {
			found = true
			continue
		}
		remaining = append(remaining, rowID)
	}
	if !found {
		return Task{}, ErrMoveRejected
	}

	// Clamp the target into [0, len(remaining)] and splice the moved id back in.
	target := position
	if target < 0 {
		target = 0
	}
	if target > len(remaining) {
		target = len(remaining)
	}
	newOrder := make([]int64, 0, len(current))
	newOrder = append(newOrder, remaining[:target]...)
	newOrder = append(newOrder, id)
	newOrder = append(newOrder, remaining[target:]...)

	positions := make([]int32, len(newOrder))
	for i := range newOrder {
		positions[i] = int32(i)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE tasks SET position = data.pos
		   FROM (SELECT unnest($1::bigint[]) AS id,
		                unnest($2::int[]) AS pos) AS data
		  WHERE tasks.id = data.id`,
		newOrder, positions); err != nil {
		return Task{}, err
	}

	row := tx.QueryRow(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE id = $1`, id)
	t, err := scanTask(row)
	if err != nil {
		return Task{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Task{}, err
	}
	return t, nil
}
