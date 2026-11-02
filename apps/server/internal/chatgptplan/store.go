// Package chatgptplan owns server-specific SIWC registration and credentials.
// It does not read Codex, client-side model, or application login credentials.
package chatgptplan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

var (
	ErrAuthentication = errors.New("chatgpt_plan_authentication_required")
	ErrUnavailable    = errors.New("chatgpt_plan_unavailable")
)

type Credential struct {
	Email        string    `json:"email,omitempty"`
	Issuer       string    `json:"issuer"`
	Subject      string    `json:"subject,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
	HostID       string    `json:"ext_agent_host_id"`
	IDToken      string    `json:"id_token,omitempty"`
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	ExpiresIn    int64     `json:"expires_in,omitempty"`
	Scopes       []string  `json:"scopes,omitempty"`
	SavedAt      time.Time `json:"saved_at,omitempty"`
}

// Identity binds a connection revision to an account registration and host,
// without incorporating rotating tokens or exposing account metadata.
func (c Credential) Identity() string {
	if c.Issuer == "" || c.ClientID == "" || c.Subject == "" || c.HostID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(c.Issuer + "\x00" + c.ClientID + "\x00" + c.Subject + "\x00" + c.HostID))
	return hex.EncodeToString(sum[:])
}

func (c Credential) ready(issuer string) bool {
	return c.Issuer == issuer && c.Subject != "" && c.ClientID != "" && c.ClientID != "dynamic_agent_client" && c.HostID != "" && c.AccessToken != "" && c.RefreshToken != "" && c.IDToken != "" && strings.EqualFold(c.TokenType, "Bearer") && c.ExpiresIn > 0 && !c.SavedAt.IsZero() && slices.Contains(c.Scopes, "chatgpt.tokens.use.direct") && slices.Contains(c.Scopes, "resource.invoke") && slices.Contains(c.Scopes, "offline_access")
}

func Read(file string) (Credential, error) {
	var c Credential
	if !filepath.IsAbs(file) {
		return c, ErrAuthentication
	}
	if err := privateRead(file); err != nil {
		return c, ErrAuthentication
	}
	b, err := os.ReadFile(file)
	if err != nil || json.Unmarshal(b, &c) != nil {
		return c, ErrAuthentication
	}
	return c, nil
}

func Save(file string, c Credential) error {
	if !filepath.IsAbs(file) {
		return ErrAuthentication
	}
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return ErrUnavailable
	}
	f, err := os.CreateTemp(filepath.Dir(file), ".chatgpt-*")
	if err != nil {
		return ErrUnavailable
	}
	defer os.Remove(f.Name())
	if err = protect(f.Name()); err == nil {
		err = json.NewEncoder(f).Encode(c)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), file)
	}
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

func lock(ctx context.Context, file string) (func(), error) {
	if !filepath.IsAbs(file) {
		return nil, ErrAuthentication
	}
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return nil, ErrUnavailable
	}
	f, err := os.OpenFile(file+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, ErrUnavailable
	}
	for {
		ok, err := tryLock(f)
		if err != nil {
			f.Close()
			return nil, ErrUnavailable
		}
		if ok {
			return func() { f.Close() }, nil
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}
