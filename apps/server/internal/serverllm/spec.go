package serverllm

import (
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
)

type Ref = access.VersionRef
type Limits struct {
	InputBytes   int `json:"input_bytes"`
	OutputTokens int `json:"output_tokens"`
	Seconds      int `json:"seconds"`
}
type Spec struct {
	Connection         string               `json:"connection"`
	Revision           string               `json:"revision"`
	Model              string               `json:"model"`
	Allowed            []string             `json:"allowed_models"`
	Fallbacks          []string             `json:"fallbacks"`
	Instructions       string               `json:"instructions"`
	Context            contexts.Selection   `json:"context"`
	Evidence           []Ref                `json:"evidence"`
	OutputChannels     []string             `json:"output_channels"`
	Limits             Limits               `json:"limits"`
	Prices             map[string]usage.Ref `json:"prices,omitempty"`
	Contract           string               `json:"contract,omitempty"`
	Valuation          string               `json:"valuation,omitempty"`
	Cost               *usage.CostBudget    `json:"cost,omitempty"`
	ReevaluateEvidence bool                 `json:"reevaluate_evidence"`
}

func Valid(s *Spec) bool {
	return s != nil && s.Connection != "" && s.Revision != "" && s.Model != "" && len(s.Allowed) > 0 && s.Instructions != "" && s.Limits.InputBytes > 0 && s.Limits.OutputTokens > 0 && s.Limits.Seconds > 0 && (s.Cost == nil || usage.ValidCostBudget(*s.Cost))
}
