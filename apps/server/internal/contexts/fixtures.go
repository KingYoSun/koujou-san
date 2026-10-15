package contexts

import "context"

func fixtureGraph() (map[Ref]Unit, []Ref) {
	refs := []Ref{{"sentence", "9007199254740993", "9007199254740994"}, {"sentence", "s2", "r1"}, {"paragraph", "p1", "r1"}, {"paragraph", "p2", "r1"}, {"section", "sec", "r1"}, {"document", "doc", "r1"}}
	a, b := "共有の原文。", "二つ目の原文。"
	return map[Ref]Unit{
		refs[0]: {Kind: "sentence", Text: &a}, refs[1]: {Kind: "sentence", Text: &b},
		refs[2]: {Kind: "paragraph", Children: refs[:2]}, refs[3]: {Kind: "paragraph", Children: refs[:1]},
		refs[4]: {Kind: "section", Title: "判断の基準", Children: refs[2:4], Attachments: []Fixed{{URI: "fixture://code", Revision: "code-v1", Range: "lines:1-2", Digest: digest([]byte(`"return 1"`)), Delivery: "reference", State: "ready"}}}, refs[5]: {Kind: "document", Title: "製品方針", Children: refs[4:5]},
	}, refs
}

// Fixtures is consumed by both Go and TypeScript, including the exact UTF-8
// input string. It is a domain contract fixture, not a live Workspace store.
func Fixtures() map[string]Resolution {
	units, refs := fixtureGraph()
	newer := refs[0]
	newer.Revision = "next"
	text := "更新した原文。"
	units[newer] = Unit{Kind: "sentence", Text: &text}
	load := func(_ context.Context, r Ref) (Unit, error) {
		u, ok := units[r]
		if !ok {
			return Unit{}, invalid("missing_reference", &r)
		}
		return u, nil
	}
	selection := Selection{Mode: "select", Include: []Ref{refs[5], refs[0], refs[5]}}
	normal := ResolveRequest{RunID: "implementation", Selection: selection}
	excerpt := normal
	excerpt.Selection.Exclude = []Ref{refs[0]}
	conflict := ResolveRequest{Selection: Selection{Mode: "select", Include: []Ref{refs[5], newer}}}
	fixed := Fixed{URI: "fixture://source", Revision: "r1", Range: "whole", Digest: digest([]byte(`"external"`)), State: "unfetched", Delivery: "snapshot", Structure: "unstructured"}
	output := Direct{Value: []byte(`{"result":"原出力"}`), State: "ready", Origin: &Origin{Kind: "output", ID: "source-run", Revision: "o1", Digest: digest([]byte(`{"result":"原出力"}`)), Path: "/result"}}
	requests := map[string]ResolveRequest{
		"normal": normal, "excerpt": excerpt, "conflict": conflict, "none": {Selection: Selection{Mode: "none"}}, "unspecified": {},
		"none-conflict":      {Selection: Selection{Mode: "none", Direct: []Direct{output}}},
		"external-unfetched": {Selection: Selection{Mode: "select", External: []Fixed{fixed}}},
		"partial-candidates": {Selection: selection, Evidence: []CandidateEvidence{{Engine: "arcadedb", Position: "p1", RequiredPosition: "p2", State: "partial", Reason: "truncated"}}},
		"output":             {Selection: Selection{Mode: "select", Direct: []Direct{output}}},
	}
	result := map[string]Resolution{}
	for name, request := range requests {
		result[name] = Resolve(context.Background(), load, "workspace-fixture", request, DefaultLimits())
	}
	return result
}
