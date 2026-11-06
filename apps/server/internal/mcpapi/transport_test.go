package mcpapi

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPBehindLoopbackProxy(t *testing.T) {
	handler := mcpTransport(mcp.NewServer(&mcp.Implementation{Name: "owned", Version: "1"}, nil), "koujou.fixture.test", 1<<20)
	for _, host := range []string{"koujou.fixture.test", "untrusted.example", "localhost"} {
		t.Run(host, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"koujou-san-client","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`
			req := httptest.NewRequest("POST", "https://"+host+"/mcp", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			req.Header.Set("MCP-Protocol-Version", "2026-07-28")
			req.Header.Set("Mcp-Method", "server/discover")
			req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			expected := http.StatusForbidden
			if host == "koujou.fixture.test" {
				expected = http.StatusOK
			}
			if response.Code != expected {
				t.Fatalf("status %d want %d: %s", response.Code, expected, response.Body.String())
			}
		})
	}
}
