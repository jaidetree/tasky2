package task

import (
	"context"

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
