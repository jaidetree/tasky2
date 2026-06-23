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

// ListActive returns the active pool — Pending + In Progress, soft-deleted rows
// excluded — ordered by the manual position (id as a stable tiebreaker).
func (s *Store) ListActive(ctx context.Context) ([]Task, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+taskColumns+` FROM tasks
		 WHERE deleted_at IS NULL
		   AND status IN ('pending', 'in_progress')
		 ORDER BY position ASC, id ASC`,
	)
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
