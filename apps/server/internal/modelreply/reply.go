// Package modelreply decodes the provider envelope shared by client and server LLM work.
package modelreply

import (
	"encoding/json"
	"strconv"

	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
)

type Reply struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content *string `json:"content"`
			Calls   []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		Finish string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		Input        *uint64 `json:"prompt_tokens"`
		Output       *uint64 `json:"completion_tokens"`
		InputDetails struct {
			Cached *uint64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		OutputDetails struct {
			Reasoning *uint64 `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
}

func Parse(b json.RawMessage, allowed []string) (Reply, string) {
	var r Reply
	if json.Unmarshal(b, &r) != nil || len(r.Choices) != 1 {
		return r, "invalid_model_response"
	}
	found := false
	for _, model := range allowed {
		found = found || model == r.Model
	}
	if !found {
		return r, "model_not_allowed"
	}
	if r.Choices[0].Finish == "length" || r.Choices[0].Finish == "content_filter" {
		return r, "incomplete_model_response"
	}
	return r, ""
}

func Quantity(n *uint64, includes []string) usage.Quantity {
	q := usage.Quantity{State: "missing", Reason: "provider_did_not_report", Unit: "token", Includes: includes}
	if n != nil {
		v := strconv.FormatUint(*n, 10)
		q.Value = &v
		q.State = "reported"
		q.Reason = ""
	}
	return q
}

func (r Reply) Quantities() map[string]usage.Quantity {
	return map[string]usage.Quantity{"input": Quantity(r.Usage.Input, []string{"cache_read"}), "cache_read": Quantity(r.Usage.InputDetails.Cached, nil), "output": Quantity(r.Usage.Output, []string{"reasoning"}), "reasoning": Quantity(r.Usage.OutputDetails.Reasoning, nil)}
}
