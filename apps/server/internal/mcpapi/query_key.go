package mcpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Page cursors vary; the query identity remains stable across its pages.
func queryKey(input any) string {
	b, _ := json.Marshal(input)
	var v any
	_ = json.Unmarshal(b, &v)
	var clear func(any)
	clear = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			delete(x, "cursor")
			for _, child := range x {
				clear(child)
			}
		case []any:
			for _, child := range x {
				clear(child)
			}
		}
	}
	clear(v)
	b, _ = json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
