package serverllm

import (
	"context"
	"encoding/json"
	"github.com/KingYoSun/koujou-san/apps/server/internal/modelreply"
	"time"
)

// Dispatch runs only after the owner has durably reserved this request. Saving
// its returned bytes is independent of cancellation and business adoption.
func Dispatch(ctx context.Context, c Connection, spec *Spec, request json.RawMessage, id string, save func(context.Context, Response, modelreply.Reply, string) error) error {
	call, cancel := context.WithTimeout(ctx, time.Duration(spec.Limits.Seconds)*time.Second)
	response, err := c.Call(call, request, id)
	cancel()
	reply, reason := modelreply.Parse(response.Body, spec.Allowed)
	if err != nil || response.Status != 200 {
		reason = "model_request_failed"
	}
	completion, stop := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer stop()
	return save(completion, response, reply, reason)
}
