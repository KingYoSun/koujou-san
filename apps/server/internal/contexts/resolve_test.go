package contexts

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestResolutionContract(t *testing.T) {
	fixtures := Fixtures()
	m := fixtures["normal"].Manifest
	if len(fixtures["normal"].Sources) != 1 || len(fixtures["normal"].Sources[0].Attachments) != 1 || fixtures["normal"].Sources[0].Attachments[0].URI != "fixture://code" {
		t.Fatal("preview omitted fixed source/attachment metadata")
	}

	if m == nil || len(m.Placements) != 7 || len(m.Duplicates) != 2 || strings.Count(*m.InputUTF8, "共有の原文。") != 2 {
		t.Fatalf("structure/placement lost: %+v", m)
	}
	if strings.Contains(*m.InputUTF8, "workspace") || strings.Contains(*m.InputUTF8, "9007199254740993") {
		t.Fatal("management metadata entered input")
	}
	excerpt := fixtures["excerpt"].Manifest
	if excerpt == nil || len(excerpt.Placements) != 4 || strings.Contains(*excerpt.InputUTF8, "共有の原文。") || !strings.Contains(*excerpt.InputUTF8, `"excerpt":true`) {
		t.Fatal("global exclusion/empty container removal failed")
	}
	for name, code := range map[string]string{"conflict": "conflicting_revisions", "none-conflict": "selection_conflict", "external-unfetched": "external_unfetched", "partial-candidates": "incomplete_candidates"} {
		result := fixtures[name]
		if result.Ready || result.Manifest != nil || len(result.Missing) != 1 || result.Missing[0].Code != code {
			t.Fatalf("%s: %+v", name, result)
		}
	}
	for _, name := range []string{"none", "unspecified"} {
		if fixtures[name].Manifest.State != "none" || fixtures[name].Manifest.InputUTF8 != nil {
			t.Fatal("inherited context without selection")
		}
	}
	raw, err := os.ReadFile("../../../../contracts/context-fixtures-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected any
	_ = json.Unmarshal(raw, &expected)
	var actual any
	_ = json.Unmarshal(canonical(fixtures), &actual)
	if !reflect.DeepEqual(expected, actual) {
		t.Fatal("Go/TS saved Context fixtures differ; regenerate with cmd/context-fixtures")
	}
}

func TestResolutionLimitsAndInvalidReferences(t *testing.T) {
	ctx := context.Background()
	units, refs := fixtureGraph()
	load := func(_ context.Context, r Ref) (Unit, error) {
		u, ok := units[r]
		if !ok {
			return Unit{}, invalid("missing_reference", &r)
		}
		return u, nil
	}
	request := ResolveRequest{Selection: Selection{Mode: "select", Include: refs[5:6]}}
	result := Resolve(ctx, load, "w", request, DefaultLimits())
	exact := Limits{len(*result.Manifest.InputUTF8), 7}
	if !Resolve(ctx, load, "w", request, exact).Ready {
		t.Fatal("exact limit rejected")
	}
	for _, limit := range []Limits{{exact.Bytes - 1, 7}, {exact.Bytes, 6}} {
		if Resolve(ctx, load, "w", request, limit).Ready {
			t.Fatal("capacity exceeded accepted")
		}
	}
	large := strings.Repeat("a", DefaultLimits().Bytes)
	units[refs[0]] = Unit{Kind: "sentence", Text: &large}
	if got := Resolve(ctx, load, "w", ResolveRequest{Selection: Selection{Mode: "select", Include: refs[:1]}}, DefaultLimits()); got.Ready || got.Missing[0].Code != "byte_limit" {
		t.Fatal("16 MiB default not enforced")
	}
	small := "x"
	units[refs[0]] = Unit{Kind: "sentence", Text: &small}
	children := make([]Ref, DefaultLimits().Placements)
	for i := range children {
		children[i] = refs[0]
	}
	units[refs[2]] = Unit{Kind: "paragraph", Children: children}
	if got := Resolve(ctx, load, "w", ResolveRequest{Selection: Selection{Mode: "select", Include: refs[2:3]}}, DefaultLimits()); got.Ready || got.Missing[0].Code != "placement_limit" {
		t.Fatal("100k placement default not enforced")
	}
	for name, bad := range map[string]Unit{
		"type": {Kind: "document", Children: refs[:1]}, "missing": {Kind: "paragraph", Children: []Ref{{"sentence", "missing", "r1"}}},
		"cycle": {Kind: "paragraph", Children: []Ref{{"paragraph", "p1", "r1"}}},
	} {
		t.Run(name, func(t *testing.T) {
			units[refs[2]] = bad
			if Resolve(ctx, load, "w", ResolveRequest{Selection: Selection{Mode: "select", Include: refs[2:3]}}, DefaultLimits()).Ready {
				t.Fatal("invalid graph resolved")
			}
		})
	}
	fixed := Fixed{URI: "fixture://doc", Revision: "r1", Range: "whole", Digest: digest([]byte(`"原文"`)), Delivery: "snapshot", State: "ready", Snapshot: []byte(`"原文"`), Structure: "unstructured"}
	u := Unit{Kind: "sentence", External: &fixed}
	if validateUnit(u) == nil {
		t.Fatal("unstructured document became sentence")
	}
	fixed.Structure = "sentence"
	if validateUnit(u) != nil {
		t.Fatal("fixed sentence rejected")
	}
	fixed.Snapshot = []byte(`"別の原文"`)
	if validateUnit(u) == nil {
		t.Fatal("snapshot digest mismatch accepted")
	}
	if _, err := canonicalRaw([]byte(`{} {}`)); err == nil {
		t.Fatal("multiple raw values accepted")
	}
	t.Setenv("KOUJOU_CONTEXT_MAX_BYTES", "33554432")
	t.Setenv("KOUJOU_CONTEXT_MAX_PLACEMENTS", "200000")
	if limit, err := EnvironmentLimits(); err != nil || limit.Bytes != 33554432 || limit.Placements != 200000 {
		t.Fatal("configured capacity ignored")
	}
	t.Setenv("KOUJOU_CONTEXT_MAX_BYTES", "bad")
	if _, err := EnvironmentLimits(); err == nil {
		t.Fatal("invalid capacity silently defaulted")
	}
	if !bytes.Equal(canonical(json.RawMessage(`{"x":1}`)), []byte(`{"x":1}`)) {
		t.Fatal("raw JSON altered")
	}
}
