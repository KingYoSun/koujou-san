package repositories

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/gitea"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubobserve"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
)

var ErrInvalid = errors.New("invalid_repository_request")
var ErrState = errors.New("repository_state_conflict")

type Service struct {
	Site     string
	Observer *githubobserve.Service
	Store    *persistence.Store
	Access   *access.Service
	crypt    cipher.AEAD
	key      []byte
}
type Connection struct {
	Auth        string `json:"auth,omitempty"`
	Account     string `json:"account,omitempty"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Instance    string `json:"instance"`
	AllowCreate bool   `json:"allow_create"`
	ActorID     int64  `json:"actor_id"`
	Version     string `json:"version"`
}
type credential struct {
	Token string           `json:"token"`
	CA    string           `json:"ca,omitempty"`
	OAuth *oauthCredential `json:"oauth,omitempty"`
}
type ConnectionWrite struct {
	apicontract.Mutation
	Name        string `json:"name"`
	Instance    string `json:"instance"`
	AllowCreate bool   `json:"allow_create"`
	Token       string `json:"token,omitempty"`
	CA          string `json:"ca,omitempty"`
}
type Page[T any] struct {
	Items []T    `json:"items"`
	Next  string `json:"next,omitempty"`
}

func New(store *persistence.Store, a *access.Service, key []byte) *Service {
	hash := sha256.Sum256(append([]byte("koujou-forge-credentials\x00"), key...))
	block, _ := aes.NewCipher(hash[:])
	crypt, _ := cipher.NewGCM(block)
	return &Service{Store: store, Access: a, crypt: crypt, key: hash[:]}
}
func raw(value any) []byte { b, _ := json.Marshal(value); return b }
func (s *Service) seal(w, id string, value any) ([]byte, error) {
	nonce := make([]byte, s.crypt.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.crypt.Seal(nonce, nonce, raw(value), []byte(w+"\x00"+id)), nil
}
func (s *Service) open(w, id string, value []byte) (credential, error) {
	var out credential
	err := s.openValue(w, id, value, &out)
	return out, err
}
func (s *Service) openValue(w, id string, value []byte, out any) error {
	n := s.crypt.NonceSize()
	if len(value) < n {
		return ErrInvalid
	}
	plain, err := s.crypt.Open(nil, value[:n], value[n:], []byte(w+"\x00"+id))
	if err == nil {
		err = json.Unmarshal(plain, out)
	}
	return err
}
func (s *Service) Connection(ctx context.Context, w, id string) (Connection, *gitea.Client, error) {
	c, secret, err := s.connectionRecord(ctx, w, id)
	if err != nil {
		return c, nil, err
	}
	credentials, err := s.open(w, id, secret)
	if err != nil {
		return c, nil, err
	}
	client, err := s.oauthClient(ctx, w, id, c, credentials)
	return c, client, err
}
func (s *Service) connectionRecord(ctx context.Context, w, id string) (Connection, []byte, error) {
	var c Connection
	var spec, secret []byte
	err := s.Store.Database().QueryRow(ctx, `SELECT c.spec,c.credential,v.version FROM forge_connections c JOIN shared_versions v ON v.workspace_id=c.workspace_id AND v.resource_id='forge-connection:'||c.id WHERE c.workspace_id=$1 AND c.id=$2`, w, id).Scan(&spec, &secret, &c.Version)
	if err != nil {
		return c, nil, err
	}
	version := c.Version
	if json.Unmarshal(spec, &c) != nil {
		return c, nil, ErrInvalid
	}
	c.Version = version
	return c, secret, nil
}
func (s *Service) Connections(ctx context.Context, p access.Principal, w, after string) (Page[Connection], error) {
	out := Page[Connection]{Items: []Connection{}}
	if err := s.Access.Check(ctx, p, w, "", "read"); err != nil {
		return out, err
	}
	rows, err := s.Store.Database().Query(ctx, `SELECT c.spec,v.version FROM forge_connections c JOIN shared_versions v ON v.workspace_id=c.workspace_id AND v.resource_id='forge-connection:'||c.id WHERE c.workspace_id=$1 AND c.id>$4 AND `+access.ReadFilterSQL(`jsonb_build_array(jsonb_build_object('id',v.resource_id,'version',v.version))`)+` ORDER BY c.id LIMIT 51`, w, p.UserID, p.OperationID, after)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var spec []byte
		var c Connection
		var version string
		if err = rows.Scan(&spec, &version); err != nil {
			return out, err
		}
		if json.Unmarshal(spec, &c) != nil {
			return out, ErrInvalid
		}
		c.Version = version
		out.Items = append(out.Items, c)
	}
	if len(out.Items) > 50 {
		out.Items = out.Items[:50]
		out.Next = out.Items[49].ID
	}
	return out, rows.Err()
}
func (s *Service) PutConnection(ctx context.Context, p access.Principal, w, id string, req ConnectionWrite) (apicontract.Response[apicontract.Reference], error) {
	if err := s.Access.Check(ctx, p, w, "", "manage"); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	origin, err := gitea.Origin(req.Instance)
	if err != nil || id == "" || strings.TrimSpace(req.Name) == "" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	credentials := credential{Token: req.Token, CA: req.CA}
	actorID := int64(0)
	if req.ExpectedVersion != "0" {
		current, _, err := s.connectionRecord(ctx, w, id)
		if err != nil {
			return apicontract.Response[apicontract.Reference]{}, err
		}
		if current.Instance != origin {
			return apicontract.Response[apicontract.Reference]{}, ErrState
		}
		if req.Token == "" && req.CA == "" {
			current.Name = req.Name
			current.AllowCreate = req.AllowCreate
			current.Version = ""
			return s.mutate(ctx, p, w, "forge-connection:"+id, "forge-connection", req.Mutation, false, current, func(tx pgx.Tx, ref apicontract.Reference) (any, error) {
				current.Version = ref.Version
				_, err := tx.Exec(ctx, `UPDATE forge_connections SET spec=$3 WHERE workspace_id=$1 AND id=$2`, w, id, raw(current))
				return current, err
			})
		}
		if current.Auth == "oauth" && req.Token == "" {
			return apicontract.Response[apicontract.Reference]{}, ErrInvalid
		}
		actorID = current.ActorID
		{
			var encrypted []byte
			if err = s.Store.Database().QueryRow(ctx, `SELECT credential FROM forge_connections WHERE workspace_id=$1 AND id=$2`, w, id).Scan(&encrypted); err != nil {
				return apicontract.Response[apicontract.Reference]{}, err
			}
			saved, openErr := s.open(w, id, encrypted)
			err = openErr
			if err != nil {
				return apicontract.Response[apicontract.Reference]{}, err
			}
			if req.Token == "" {
				credentials.Token = saved.Token
				credentials.OAuth = saved.OAuth
			}
			if req.CA == "" {
				credentials.CA = saved.CA
			}
		}
	}
	client, err := gitea.FromCredentials(origin, credentials.Token, credentials.CA)
	if credentials.OAuth != nil {
		client, err = gitea.FromOAuthCredentials(origin, credentials.Token, credentials.CA)
	}
	if err != nil {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	var actor struct {
		ID int64 `json:"id"`
	}
	if actorID == 0 || req.Token != "" || req.CA != "" {
		if err = client.Get(ctx, "/user", &actor); err != nil || actor.ID < 1 {
			return apicontract.Response[apicontract.Reference]{}, ErrInvalid
		}
		actorID = actor.ID
	}
	c := Connection{ID: id, Name: req.Name, Instance: origin, AllowCreate: req.AllowCreate, ActorID: actorID}
	if credentials.OAuth != nil {
		c.Auth = "oauth"
		old, _, err := s.connectionRecord(ctx, w, id)
		if err != nil {
			return apicontract.Response[apicontract.Reference]{}, err
		}
		c.Account = old.Account
	}
	return s.saveConnection(ctx, p, w, c, credentials, req.Mutation)
}

func (s *Service) saveConnection(ctx context.Context, p access.Principal, w string, c Connection, credentials credential, m apicontract.Mutation, within ...pgx.Tx) (apicontract.Response[apicontract.Reference], error) {
	id := c.ID
	encrypted, err := s.seal(w, id, credentials)
	if err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write(raw(credentials))
	payload := struct {
		Connection
		Digest string
	}{c, hex.EncodeToString(mac.Sum(nil))}
	return s.mutate(ctx, p, w, "forge-connection:"+id, "forge-connection", m, false, payload, func(tx pgx.Tx, ref apicontract.Reference) (any, error) {
		_, err := tx.Exec(ctx, `INSERT INTO forge_connections VALUES($1,$2,$3,$4) ON CONFLICT(workspace_id,id) DO UPDATE SET spec=excluded.spec,credential=excluded.credential`, w, id, raw(c), encrypted)
		c.Version = ref.Version
		return c, err
	}, within...)
}
func (s *Service) mutate(ctx context.Context, p access.Principal, w, id, kind string, m apicontract.Mutation, deleted bool, payload any, apply func(pgx.Tx, apicontract.Reference) (any, error), within ...pgx.Tx) (apicontract.Response[apicontract.Reference], error) {
	if err := s.Access.Check(ctx, p, w, "", "manage"); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	scope := persistence.Scope{ActorID: "user:" + p.UserID, Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}, Recheck: func(tx pgx.Tx) *apicontract.Problem {
		if err := s.Access.CheckTx(ctx, tx, p, w, "", "manage"); err != nil {
			return &apicontract.Problem{Code: "forbidden"}
		}
		if deleted && s.Access.CheckTx(ctx, tx, p, w, id, "update") != nil {
			return &apicontract.Problem{Code: "forbidden"}
		}
		return nil
	}}
	return s.Store.Apply(ctx, scope, persistence.Command{WorkspaceID: w, ResourceID: id, Kind: kind, Deleted: deleted, Mutation: m, Payload: raw(payload)}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		value, err := apply(tx, ref)
		if err != nil {
			return nil, err
		}
		if m.ExpectedVersion == "0" {
			if err = s.Access.PutResource(ctx, tx, access.Resource{WorkspaceID: w, ID: id}, nil); err != nil {
				return nil, err
			}
		}
		return raw(value), nil
	}, within...)
}
func send(w http.ResponseWriter, value any, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	status := 200
	if err != nil {
		status = 503
		code := "unavailable"
		if errors.Is(err, access.ErrForbidden) {
			status = 403
			code = "forbidden"
		} else if errors.Is(err, ErrInvalid) {
			status = 400
			code = "invalid_request"
		} else if errors.Is(err, ErrState) {
			status = 409
			code = "state_conflict"
		}
		value = map[string]string{"error": code}
	}
	if r, ok := value.(apicontract.Response[apicontract.Reference]); ok && r.Error != nil {
		status = 409
		if r.Error.Code == "forbidden" {
			status = 403
		}
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
