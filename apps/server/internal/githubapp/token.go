// Package githubapp implements the shared, repository-bound App token exchange.
package githubapp

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Token struct {
	Value      string
	Expires    time.Time
	Repository string
}
type Repository struct {
	InstallationID, ID int64
	Name               string
}
type Request func(context.Context, string, string, string, io.Reader) (*http.Response, error)

func Exchange(ctx context.Context, appID string, key *rsa.PrivateKey, repo Repository, permissions map[string]string, request Request) (Token, error) {
	enc := base64.RawURLEncoding.EncodeToString
	claims, _ := json.Marshal(map[string]any{"iat": time.Now().Add(-time.Minute).Unix(), "exp": time.Now().Add(8 * time.Minute).Unix(), "iss": appID})
	unsigned := enc([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + enc(claims)
	digest := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return Token{}, errors.New("authentication_failed")
	}
	payload, _ := json.Marshal(map[string]any{"repository_ids": []int64{repo.ID}, "permissions": permissions})
	resp, err := request(ctx, "POST", fmt.Sprintf("/app/installations/%d/access_tokens", repo.InstallationID), unsigned+"."+enc(sig), strings.NewReader(string(payload)))
	if err != nil {
		return Token{}, errors.New("connection_failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		return Token{}, errors.New("authentication_failed")
	}
	var result struct {
		Token        string            `json:"token"`
		Expires      time.Time         `json:"expires_at"`
		Permissions  map[string]string `json:"permissions"`
		Repositories []struct {
			ID   int64  `json:"id"`
			Name string `json:"full_name"`
		} `json:"repositories"`
	}
	if json.NewDecoder(resp.Body).Decode(&result) != nil || result.Token == "" || !result.Expires.After(time.Now()) {
		return Token{}, errors.New("authentication_failed")
	}
	write := false
	for _, level := range permissions {
		if level == "write" {
			write = true
		}
	}
	for name, level := range result.Permissions {
		if write && permissions[name] != level || !write && level != "read" {
			return Token{}, errors.New("unexpected_permissions")
		}
	}
	for name, level := range permissions {
		if write && result.Permissions[name] != level {
			return Token{}, errors.New("unexpected_permissions")
		}
	}
	if len(result.Repositories) != 1 || result.Repositories[0].ID != repo.ID || !strings.EqualFold(result.Repositories[0].Name, repo.Name) {
		return Token{}, errors.New("repository_not_allowed")
	}
	return Token{result.Token, result.Expires, result.Repositories[0].Name}, nil
}
