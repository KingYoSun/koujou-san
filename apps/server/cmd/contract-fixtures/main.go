package main

import (
	"encoding/json"
	"os"

	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
)

func main() {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(apicontract.Fixtures()); err != nil {
		os.Exit(1)
	}
}
