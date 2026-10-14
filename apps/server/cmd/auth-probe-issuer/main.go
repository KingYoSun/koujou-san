// Synthetic local issuer for the Issue #3 native boundary probe. Not wired into
// the product API or standard environment; production Ory integration is #5.
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

type authorization struct{ redirect, challenge string }

func main() {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	var mutex sync.Mutex
	grants := map[string]authorization{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		redirect, err := url.Parse(query.Get("redirect_uri"))
		challenge, decodeErr := base64.RawURLEncoding.DecodeString(query.Get("code_challenge"))
		if err != nil || redirect.Scheme != "http" || redirect.Hostname() != "127.0.0.1" || redirect.Port() == "" || redirect.Path != "/callback" || query.Get("state") == "" || query.Get("code_challenge_method") != "S256" || decodeErr != nil || len(challenge) != sha256.Size {
			http.Error(w, "invalid authorization request", http.StatusBadRequest)
			return
		}
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			panic(err)
		}
		code := base64.RawURLEncoding.EncodeToString(secret)
		mutex.Lock()
		grants[code] = authorization{redirect.String(), query.Get("code_challenge")}
		mutex.Unlock()
		callback := redirect.Query()
		callback.Set("state", query.Get("state"))
		callback.Set("code", code)
		redirect.RawQuery = callback.Encode()
		http.Redirect(w, r, redirect.String(), http.StatusFound)
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", 400)
			return
		}
		code, verifier := r.Form.Get("code"), r.Form.Get("code_verifier")
		digest := sha256.Sum256([]byte(verifier))
		mutex.Lock()
		grant, found := grants[code]
		valid := found && r.Form.Get("grant_type") == "authorization_code" && r.Form.Get("redirect_uri") == grant.redirect && len(verifier) >= 43 && len(verifier) <= 128 && base64.RawURLEncoding.EncodeToString(digest[:]) == grant.challenge
		if valid {
			delete(grants, code)
		}
		mutex.Unlock()
		if !valid {
			http.Error(w, "invalid_grant", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "synthetic-issue3-token", "token_type": "Bearer"})
	})
	_ = json.NewEncoder(os.Stdout).Encode(map[string]int{"issuer_port": listener.Addr().(*net.TCPAddr).Port})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := server.Serve(listener); err != nil {
		panic(err)
	}
}
