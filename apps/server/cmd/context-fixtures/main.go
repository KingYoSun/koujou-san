package main

import (
	"encoding/json"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"os"
)

func main() {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if encoder.Encode(contexts.Fixtures()) != nil {
		os.Exit(1)
	}
}
