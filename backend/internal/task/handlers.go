package task

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// taskDTO is the wire shape of a Task. It is the OpenAPI-described contract the
// generated frontend clients bind to, kept separate from the domain Task so the
// JSON shape and the internal type can evolve independently.
type taskDTO struct {
	ID          int64      `json:"id" doc:"Unique task identifier"`
	Title       string     `json:"title" doc:"Short task title"`
	Notes       string     `json:"notes" doc:"Optional longer detail"`
	Status      string     `json:"status" enum:"pending,in_progress,completed" doc:"Lifecycle status"`
	Position    int        `json:"position" doc:"Manual ordering position within the active pool"`
	CreatedAt   time.Time  `json:"created_at" doc:"When the task was created"`
	CompletedAt *time.Time `json:"completed_at,omitempty" doc:"When the task was completed, if it has been"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty" doc:"When the task was cancelled, if it has been"`
}

func toDTO(t Task) taskDTO {
	return taskDTO{
		ID:          t.ID,
		Title:       t.Title,
		Notes:       t.Notes,
		Status:      string(t.Status),
		Position:    t.Position,
		CreatedAt:   t.CreatedAt,
		CompletedAt: t.CompletedAt,
		DeletedAt:   t.DeletedAt,
	}
}

// CreateTaskInput is the POST /tasks request body.
type CreateTaskInput struct {
	Body struct {
		Title string `json:"title" minLength:"1" maxLength:"200" doc:"Short task title"`
		Notes string `json:"notes,omitempty" maxLength:"2000" doc:"Optional longer detail"`
	}
}

// TaskOutput wraps a single task in a response body.
type TaskOutput struct {
	Body taskDTO
}

// ListTasksOutput is the GET /tasks response: the active pool.
type ListTasksOutput struct {
	Body struct {
		Active []taskDTO `json:"active" doc:"Active pool: Pending and In Progress tasks, manually ordered"`
	}
}

// RegisterRoutes registers the task operations on the huma API. The handlers
// are the source of truth from which the OpenAPI spec is generated (ADR-0003).
func RegisterRoutes(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID:   "createTask",
		Method:        http.MethodPost,
		Path:          "/tasks",
		Summary:       "Create a task",
		Description:   "Create a task from a title and optional notes. A created task starts as Pending.",
		Tags:          []string{"tasks"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *CreateTaskInput) (*TaskOutput, error) {
		t, err := svc.Create(ctx, in.Body.Title, in.Body.Notes)
		if err != nil {
			if errors.Is(err, ErrTitleRequired) {
				return nil, huma.Error422UnprocessableEntity("title is required")
			}
			return nil, huma.Error500InternalServerError("could not create task", err)
		}
		return &TaskOutput{Body: toDTO(t)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listTasks",
		Method:      http.MethodGet,
		Path:        "/tasks",
		Summary:     "List active tasks",
		Description: "Return the active pool: Pending and In Progress tasks, manually ordered.",
		Tags:        []string{"tasks"},
	}, func(ctx context.Context, _ *struct{}) (*ListTasksOutput, error) {
		tasks, err := svc.ListActive(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("could not list tasks", err)
		}
		out := &ListTasksOutput{}
		out.Body.Active = make([]taskDTO, 0, len(tasks))
		for _, t := range tasks {
			out.Body.Active = append(out.Body.Active, toDTO(t))
		}
		return out, nil
	})
}
