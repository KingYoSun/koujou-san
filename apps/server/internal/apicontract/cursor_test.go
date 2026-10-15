package apicontract

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func TestExistingSearchCursorCompatibility(t *testing.T) {
	// Synthetic binary fixture captured from the Issue #9 encoder. Its key and
	// identities below are public test values, not authentication credentials.
	legacy, err := os.ReadFile("testdata/legacy-search-cursor.bin")
	if err != nil {
		t.Fatal(err)
	}
	token := base64.RawURLEncoding.EncodeToString(legacy)
	codec := NewCursorCodec([]byte(strings.Repeat("k", 32)), "koujou-search-cursor-v1")
	var value struct {
		User, Workspace, Query, Generation, Position string
		After                                        []string
	}
	domain := "koujou-search-v1-cjk-bleve-2.6.1"
	if err := codec.Open(token, domain, &value); err != nil {
		t.Fatal(err)
	}
	if value.User != "compat-user" || value.Workspace != "compat-workspace" || value.Position != "compat-position" || len(value.After) != 2 || value.After[1] != "compat-id" {
		t.Fatal("prior cursor changed")
	}
	if codec.Open(token, "another-schema", &value) == nil {
		t.Fatal("cursor accepted in another domain")
	}
}
