package jobs

import (
	"context"
	"errors"
	"log/slog"
)

var ErrNoJob = errors.New("no queued job")
var ErrNotImplemented = errors.New("job processor is not implemented in phase 0")

type Job struct { ID string `json:"id"`; Status string `json:"status"`; Attempts int `json:"attempts"` }
type Repository interface { ClaimNext(context.Context) (*Job, error); Complete(context.Context, string) error; Fail(context.Context, string, string) error }
type Processor interface { Process(context.Context, Job) error }
type NoopProcessor struct{}
func (NoopProcessor) Process(context.Context, Job) error { return ErrNotImplemented }

type Worker struct { repository Repository; processor Processor; logger *slog.Logger }
func NewWorker(repository Repository, processor Processor, logger *slog.Logger) *Worker { return &Worker{repository:repository, processor:processor, logger:logger} }
func (w *Worker) RunOnce(ctx context.Context) error {
	job, err := w.repository.ClaimNext(ctx)
	if err != nil { return err }
	if job == nil { return ErrNoJob }
	if err := w.processor.Process(ctx, *job); err != nil {
		if failErr := w.repository.Fail(ctx, job.ID, err.Error()); failErr != nil { return failErr }
		w.logger.Warn("job failed", "job_id", job.ID, "error", err)
		return err
	}
	return w.repository.Complete(ctx, job.ID)
}
