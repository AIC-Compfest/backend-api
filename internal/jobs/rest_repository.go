package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/AIC-Compfest/backend-api/internal/supabase"
)

// RESTRepository consumes the RPC and table contract provisioned by infra/supabase.
// The claim RPC must atomically lock one QUEUED processing job and return []Job.
type RESTRepository struct { client *supabase.Client }
func NewRESTRepository(client *supabase.Client) *RESTRepository { return &RESTRepository{client:client} }
func (r *RESTRepository) ClaimNext(ctx context.Context) (*Job, error) {
	resp, err := r.client.Request(ctx, http.MethodPost, "/rest/v1/rpc/claim_next_processing_job", "{}")
	if err != nil { return nil, err }; defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { return nil, fmt.Errorf("claim job returned %s", resp.Status) }
	var jobs []Job
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil { return nil, err }
	if len(jobs) == 0 { return nil, nil }
	return &jobs[0], nil
}
func (r *RESTRepository) Complete(ctx context.Context, id string) error { return r.updateStatus(ctx, id, "COMPLETED", "") }
func (r *RESTRepository) Fail(ctx context.Context, id, reason string) error { return r.updateStatus(ctx, id, "FAILED", reason) }
func (r *RESTRepository) updateStatus(ctx context.Context, id, status, reason string) error {
	body, _ := json.Marshal(map[string]string{"status":status, "error_message":reason})
	path := "/rest/v1/processing_jobs?id=eq."+url.QueryEscape(id)+"&status=eq.PROCESSING"
	resp, err := r.client.Request(ctx, http.MethodPatch, path, string(body))
	if err != nil { return err }; defer resp.Body.Close(); io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { return fmt.Errorf("update job returned %s", resp.Status) }
	return nil // zero affected rows are a successful idempotent terminal update.
}
