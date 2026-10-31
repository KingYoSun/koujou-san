package clientops

import (
	"encoding/json"
	"testing"

	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
)

func TestChatPolicyKeepsSelectedHistory(t *testing.T) {
	var p Policy
	if err := decode([]byte(`{"mode":"proposal","fixed":{"mode":"none"},"scopes":[],"limits":{"model_requests":32,"mcp_requests":256,"seconds":600},"chat":{"thread":{"id":"chat-thread:one","version":"v1"},"messages":[{"id":"chat-message:one","version":"v2"}]}}`), &p); err != nil || !validPolicyKind("chat-policy:one", p) {
		t.Fatalf("a project conversation cannot select fixed messages: %v", err)
	}
	if validPolicyKind("selection-policy:one", p) || validPolicyKind("definition-policy:one", p) {
		t.Fatal("chat history became input to another purpose")
	}
}

func TestSelectionProposal(t *testing.T) {
	a := contexts.Ref{Kind: "sentence", ID: "required", Revision: "v1"}
	b := contexts.Ref{Kind: "paragraph", ID: "chosen", Revision: "v2"}
	x := contexts.Ref{Kind: "section", ID: "excluded", Revision: "v3"}
	locked := contexts.Selection{Mode: "select", Include: []contexts.Ref{a}, Exclude: []contexts.Ref{x}}
	proposal := Proposal{Items: []Item{{Ref: b, Reason: "作業の条件"}}, Questions: []string{}, Unknown: []string{}}
	selected, err := selection(locked, proposal)
	if err != nil || len(selected.Include) != 2 || selected.Include[0] != a || selected.Include[1] != b || len(selected.Exclude) != 1 || selected.Exclude[0] != x {
		t.Fatalf("fixed choices/order changed: %+v %v", selected, err)
	}
	for _, tc := range []struct {
		name     string
		base     contexts.Selection
		proposal Proposal
	}{
		{"none", contexts.Selection{Mode: "none"}, proposal},
		{"excluded", locked, Proposal{Items: []Item{{Ref: x, Reason: "conflict"}}}},
		{"moving_version", locked, Proposal{Items: []Item{{Ref: contexts.Ref{Kind: "sentence", ID: "moving", Revision: "head"}, Reason: "not pinned"}}}},
		{"wrong_granularity", locked, Proposal{Items: []Item{{Ref: contexts.Ref{Kind: "scope", ID: "scope", Revision: "v1"}, Reason: "not a document unit"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := selection(tc.base, tc.proposal); err == nil {
				t.Fatal("invalid proposal accepted")
			}
		})
	}
	t.Run("none_stays_empty", func(t *testing.T) {
		v, err := selection(contexts.Selection{Mode: "none"}, Proposal{})
		if err != nil || v.Mode != "none" || len(v.Include) != 0 {
			t.Fatal(v, err)
		}
	})
	t.Run("questions_and_unknown_never_auto_apply", func(t *testing.T) {
		for _, p := range []Proposal{{Questions: []string{"対象は？"}}, {Unknown: []string{"探索打切り"}}} {
			if complete(p) {
				t.Fatal("incomplete output called complete")
			}
		}
	})
	t.Run("typed_refs_do_not_accept_generated_replacement", func(t *testing.T) {
		var p Proposal
		if decode([]byte(`{"items":[{"ref":{"kind":"sentence","id":"one","revision":"v1"},"reason":"ok","text":"generated replacement"}],"questions":[],"unknown":[]}`), &p) == nil {
			t.Fatal("generated body accepted as selection")
		}
		if decode([]byte(`{"items":[],"questions":[],"unknown":[]} {}`), &p) == nil {
			t.Fatal("trailing JSON accepted")
		}
		for _, b := range []string{`{}`, `{"items":null,"questions":[],"unknown":[]}`} {
			var missing Proposal
			if decodeProposal([]byte(b), &missing) == nil {
				t.Fatal("missing result fields accepted")
			}
		}
	})
	t.Run("agreed_initial_limits", func(t *testing.T) {
		p := DefaultPolicy()
		if p.Mode != "auto" || p.Limits.Models != 32 || p.Limits.MCP != 256 || p.Limits.Seconds != 600 {
			t.Fatal(p)
		}
		b, err := json.Marshal(p)
		if err != nil || !json.Valid(b) {
			t.Fatal(err)
		}
	})
}
