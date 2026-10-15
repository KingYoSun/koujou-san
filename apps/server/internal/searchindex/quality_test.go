package searchindex

import "fmt"

type qualityCase struct {
	text, word, kind, id string
	request              Request
}

func qualityCases() []qualityCase {
	jp := []string{"承認待ち", "依存関係", "競合解決", "入力確定", "権限失効", "同期再開", "版管理", "処理順序", "共有範囲", "復旧手順"}
	en := []string{"orchestration", "idempotency", "authorization", "checkpoint", "provenance", "encryption", "membership", "reconciliation", "transaction", "continuation"}
	code := []string{"createRun", "read_context", "ContextManifest", "operation_id", "ResolveSelection", "shared_versions", "RebuildIndex", "can_execute", "CandidateEvidence", "stream_sequence"}
	kinds := []string{"sentence", "paragraph", "section", "document"}
	result := []qualityCase{}
	for n := 0; n < 40; n++ {
		c := qualityCase{kind: kinds[n%4]}
		c.id = fmt.Sprintf("q%02d-%s", n, c.kind)
		switch {
		case n < 10:
			c.word = jp[n]
			c.text = c.word + "を使ってソフトウェアの工程を確認します。"
		case n < 20:
			c.word = en[n-10]
			c.text = "Software factory " + c.word + " preserves an explicit workflow."
		case n < 30:
			c.word = code[n-20]
			c.text = "The implementation calls " + c.word + " for this operation."
		default:
			c.word = c.id
			c.text = "A context with an exact logical identifier."
		}
		c.request = Request{Text: c.word, Kinds: []string{c.kind}, Limit: 10}
		if n >= 30 {
			c.request.Text = ""
			c.request.IDs = []string{c.id}
		}
		result = append(result, c)
	}
	return result
}
