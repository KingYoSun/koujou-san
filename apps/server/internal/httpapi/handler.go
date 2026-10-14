package httpapi

import (
	"encoding/json"
	"net/http"
)

// NewHandler serves development liveness only. Application readiness, login and
// domain routes are added with their own implementations in later Issues.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			writer.Header().Set("Allow", "GET, HEAD")
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		if localOrigin(request.Header.Get("Origin")) {
			writer.Header().Set("Access-Control-Allow-Origin", request.Header.Get("Origin"))
			writer.Header().Set("Vary", "Origin")
		}
		if request.Method == http.MethodHead {
			return
		}
		_ = json.NewEncoder(writer).Encode(struct {
			Status  string `json:"status"`
			Service string `json:"service"`
			Stage   string `json:"stage"`
		}{Status: "ok", Service: "koujou-san-api", Stage: "bootstrap"})
	})
	return mux
}

func localOrigin(origin string) bool {
	switch origin {
	case "http://127.0.0.1:1420", "http://localhost:1420", "http://tauri.localhost", "https://tauri.localhost", "tauri://localhost":
		return true
	default:
		return false
	}
}
