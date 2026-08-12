package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)
type memoryRepo struct { job *Job; completed, failed string }
func (r *memoryRepo) ClaimNext(context.Context) (*Job,error) { return r.job,nil }
func (r *memoryRepo) Complete(_ context.Context,id string) error { if r.completed == "" { r.completed=id }; return nil }
func (r *memoryRepo) Fail(_ context.Context,id, _ string) error { if r.failed == "" { r.failed=id }; return nil }
type processorFunc func(context.Context, Job) error
func (f processorFunc) Process(c context.Context,j Job) error { return f(c,j) }
func TestWorkerCompletesJobIdempotently(t *testing.T) { r:=&memoryRepo{job:&Job{ID:"job-1"}}; w:=NewWorker(r,processorFunc(func(context.Context,Job)error{return nil}),slog.New(slog.NewTextHandler(io.Discard,nil))); if err:=w.RunOnce(context.Background());err!=nil{t.Fatal(err)}; if r.completed!="job-1"{t.Fatal("job not completed")} }
func TestWorkerFailsJob(t *testing.T) { r:=&memoryRepo{job:&Job{ID:"job-1"}}; w:=NewWorker(r,processorFunc(func(context.Context,Job)error{return errors.New("failure")}),slog.New(slog.NewTextHandler(io.Discard,nil))); if err:=w.RunOnce(context.Background());err==nil{t.Fatal("expected processor error")}; if r.failed!="job-1"{t.Fatal("job not failed")} }
func TestWorkerReportsNoJob(t *testing.T) { w:=NewWorker(&memoryRepo{},NoopProcessor{},slog.New(slog.NewTextHandler(io.Discard,nil))); if !errors.Is(w.RunOnce(context.Background()),ErrNoJob){t.Fatal("expected no job")} }
