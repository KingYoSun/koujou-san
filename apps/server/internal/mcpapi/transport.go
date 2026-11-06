package mcpapi

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"strings"
)

func mcpTransport(server *mcp.Server, host string, maxBytes int64) http.Handler {
	// The distribution edge preserves the public Host while connecting over loopback.
	// Pin that configured Host instead of the SDK's listener-address heuristic.
	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, PropagateRequestCancellation: true, MaxRequestBodyBytes: maxBytes, DisableLocalhostProtection: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if host == "" || !strings.EqualFold(r.Host, host) {
			send(w, http.StatusForbidden, map[string]string{"error": "mcp_host_forbidden"})
			return
		}
		transport.ServeHTTP(w, r)
	})
}
