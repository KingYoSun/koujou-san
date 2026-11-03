package repositories

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/gitea"
	"github.com/jackc/pgx/v5"
)

type oauthAccount struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}
type OAuthSession struct {
	ID         string        `json:"id"`
	State      string        `json:"state"`
	Account    *oauthAccount `json:"account,omitempty"`
	Expires    time.Time     `json:"expires"`
	Connection string        `json:"connection,omitempty"`
}

func nonce() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func digest(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}
func (s *Service) callbackURL() (string, error) {
	u, err := url.Parse(s.Site)
	if err != nil || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", ErrInvalid
	}
	return strings.TrimRight(s.Site, "/") + "/forge/oauth/callback", nil
}

type OAuthStart struct {
	Connection string `json:"connection"`
	Version    string `json:"expected_version"`
}

func (s *Service) StartOAuth(ctx context.Context, p access.Principal, w, appID string, req OAuthStart) (map[string]string, error) {
	if err := s.Access.Check(ctx, p, w, "", "manage"); err != nil {
		return nil, err
	}
	app, _, err := s.oauthApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	if req.Connection != "" {
		c, _, err := s.connectionRecord(ctx, w, req.Connection)
		if err != nil || c.Instance != app.Instance || c.Version != req.Version {
			return nil, ErrState
		}
	} else {
		req.Version = "0"
	}
	callback, err := s.callbackURL()
	if err != nil {
		return nil, err
	}
	id, state, verifier := nonce(), nonce(), nonce()
	challenge := sha256.Sum256([]byte(verifier))
	encrypted, err := s.seal(w, "oauth-verifier:"+id, verifier)
	if err != nil {
		return nil, err
	}
	version, _ := strconv.ParseInt(app.Version, 10, 64)
	_, err = s.Store.Database().Exec(ctx, `INSERT INTO forge_oauth_sessions(id,state_hash,workspace_id,user_id,app_id,app_version,expires_at,state,verifier,connection_id,connection_version) VALUES($1,$2,$3,$4,$5,$6,now()+interval '10 minutes','started',$7,$8,$9)`, id, digest(state), w, p.UserID, appID, version, encrypted, req.Connection, req.Version)
	if err != nil {
		return nil, err
	}
	query := url.Values{"client_id": {app.ClientID}, "redirect_uri": {callback}, "response_type": {"code"}, "state": {state}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "scope": {"write:user write:repository write:issue read:organization"}}
	return map[string]string{"id": id, "authorization_url": app.Instance + "/login/oauth/authorize?" + query.Encode()}, nil
}
func (s *Service) OAuthStatus(ctx context.Context, p access.Principal, w, id string) (OAuthSession, error) {
	out := OAuthSession{ID: id}
	if err := s.Access.Check(ctx, p, w, "", "manage"); err != nil {
		return out, err
	}
	var account []byte
	err := s.Store.Database().QueryRow(ctx, `SELECT state,account,expires_at,COALESCE(connection_id,'') FROM forge_oauth_sessions WHERE id=$1 AND workspace_id=$2 AND user_id=$3`, id, w, p.UserID).Scan(&out.State, &account, &out.Expires, &out.Connection)
	if err != nil {
		return OAuthSession{}, access.ErrForbidden
	}
	if !out.Expires.After(time.Now()) && out.State != "accepted" {
		out.State = "expired"
	}
	if len(account) > 0 {
		if json.Unmarshal(account, &out.Account) != nil {
			return OAuthSession{}, ErrInvalid
		}
	}
	return out, nil
}
func (s *Service) CompleteOAuth(ctx context.Context, state, code string) error {
	if state == "" {
		return ErrInvalid
	}
	var id, w, user, appID string
	var appVersion int64
	var encrypted []byte
	err := s.Store.Database().QueryRow(ctx, `UPDATE forge_oauth_sessions SET state='exchanging' WHERE state_hash=$1 AND state='started' AND expires_at>now() RETURNING id,workspace_id,user_id,app_id,app_version,verifier`, digest(state)).Scan(&id, &w, &user, &appID, &appVersion, &encrypted)
	if err != nil {
		return ErrState
	}
	fail := func(err error) error {
		_, _ = s.Store.Database().Exec(ctx, `UPDATE forge_oauth_sessions SET state='failed',credential=NULL,verifier=''::bytea WHERE id=$1`, id)
		return err
	}
	if code == "" {
		return fail(ErrInvalid)
	}
	if err = s.Access.Check(ctx, access.Principal{UserID: user}, w, "", "manage"); err != nil {
		return fail(err)
	}
	app, secret, err := s.oauthApp(ctx, appID)
	if err != nil || app.Version != strconv.FormatInt(appVersion, 10) {
		return fail(ErrState)
	}
	var verifier string
	if err = s.openValue(w, "oauth-verifier:"+id, encrypted, &verifier); err != nil {
		return fail(err)
	}
	callback, err := s.callbackURL()
	if err != nil {
		return fail(err)
	}
	token, err := gitea.Exchange(ctx, app.Instance, secret.CA, url.Values{"grant_type": {"authorization_code"}, "client_id": {app.ClientID}, "client_secret": {secret.Secret}, "redirect_uri": {callback}, "code": {code}, "code_verifier": {verifier}})
	if err != nil {
		return fail(err)
	}
	if token.Refresh == "" {
		return fail(ErrState)
	}
	client, err := gitea.FromOAuthCredentials(app.Instance, token.Access, secret.CA)
	if err != nil {
		return fail(err)
	}
	var account oauthAccount
	if err = client.Get(ctx, "/user", &account); err != nil || account.ID < 1 || account.Login == "" {
		return fail(ErrState)
	}
	credentials := credential{Token: token.Access, CA: secret.CA, OAuth: &oauthCredential{App: appID, Refresh: token.Refresh, Expires: time.Now().Add(time.Duration(token.Expires) * time.Second)}}
	encrypted, err = s.seal(w, "oauth-session:"+id, credentials)
	if err != nil {
		return fail(err)
	}
	tag, err := s.Store.Database().Exec(ctx, `UPDATE forge_oauth_sessions SET state='authenticated',credential=$2,account=$3,verifier=''::bytea WHERE id=$1 AND state='exchanging' AND expires_at>now()`, id, encrypted, raw(account))
	if err == nil && tag.RowsAffected() != 1 {
		return fail(ErrState)
	}
	return err
}

type OAuthAccept struct {
	apicontract.Mutation
	Name        string `json:"name"`
	AllowCreate bool   `json:"allow_create"`
}

func (s *Service) AcceptOAuth(ctx context.Context, p access.Principal, w, id string, req OAuthAccept) (apicontract.Response[apicontract.Reference], error) {
	var result apicontract.Response[apicontract.Reference]
	if strings.TrimSpace(req.Name) == "" {
		return result, ErrInvalid
	}
	if err := s.Access.Check(ctx, p, w, "", "manage"); err != nil {
		return result, err
	}
	err := pgx.BeginFunc(ctx, s.Store.Database(), func(tx pgx.Tx) error {
		var appID, state, connectionID, connectionVersion string
		var version int64
		var encrypted, accountRaw []byte
		var expiry time.Time
		if err := tx.QueryRow(ctx, `SELECT app_id,app_version,state,credential,account,expires_at,COALESCE(connection_id,''),connection_version FROM forge_oauth_sessions WHERE id=$1 AND workspace_id=$2 AND user_id=$3 FOR UPDATE`, id, w, p.UserID).Scan(&appID, &version, &state, &encrypted, &accountRaw, &expiry, &connectionID, &connectionVersion); err != nil {
			return access.ErrForbidden
		}
		if state != "authenticated" || !expiry.After(time.Now()) || req.ExpectedVersion != connectionVersion {
			return ErrState
		}
		app, _, err := s.oauthApp(ctx, appID, tx)
		if err != nil || app.Version != strconv.FormatInt(version, 10) {
			return ErrState
		}
		var credentials credential
		var account oauthAccount
		if s.openValue(w, "oauth-session:"+id, encrypted, &credentials) != nil || json.Unmarshal(accountRaw, &account) != nil {
			return ErrState
		}
		if connectionID == "" {
			connectionID = "oauth-" + id
		}
		c := Connection{ID: connectionID, Name: req.Name, Instance: app.Instance, AllowCreate: req.AllowCreate, ActorID: account.ID, Auth: "oauth", Account: account.Login}
		result, err = s.saveConnection(ctx, p, w, c, credentials, req.Mutation, tx)
		if err != nil {
			return err
		}
		if result.Error != nil {
			return ErrState
		}
		_, err = tx.Exec(ctx, `UPDATE forge_oauth_sessions SET state='accepted',connection_id=$2,credential=NULL WHERE id=$1`, id, c.ID)
		return err
	})
	return result, err
}
