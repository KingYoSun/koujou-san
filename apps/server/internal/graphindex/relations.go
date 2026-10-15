package graphindex

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
)

var errInput = errors.New("invalid graph request")

type database interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Service) validateRef(ctx context.Context, q database, p access.Principal, w string, r contexts.Ref) error {
	if r.ID == "" || r.Revision == "" || r.Revision == "head" {
		return errInput
	}
	var permission error
	if tx, ok := q.(pgx.Tx); ok {
		permission = s.Access.CheckTx(ctx, tx, p, w, resource(r), "get")
	} else {
		permission = s.Access.Check(ctx, p, w, resource(r), "get")
	}
	if err := permission; err != nil {
		return err
	}
	if unitKind(r.Kind) || r.Kind == "scope" {
		var actual string
		if err := q.QueryRow(ctx, "SELECT kind FROM context_records WHERE workspace_id=$1 AND resource_id=$2 AND revision=$3", w, resource(r), r.Revision).Scan(&actual); err != nil {
			return errInput
		}
		if actual != r.Kind {
			return errInput
		}
		return nil
	}
	if r.Kind != "run" {
		return errInput
	}
	var exists bool
	if err := q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2 AND version=$3 AND NOT deleted) OR EXISTS(SELECT 1 FROM graph_relations r WHERE r.workspace_id=$1 AND ((r.payload->'from'->>'kind'='run' AND r.payload->'from'->>'id'=$4 AND r.payload->'from'->>'revision'=$3) OR (r.payload->'to'->>'kind'='run' AND r.payload->'to'->>'id'=$4 AND r.payload->'to'->>'revision'=$3)))", w, resource(r), r.Revision, r.ID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return errInput
	}
	return nil
}
func (s *Service) PutRelation(ctx context.Context, p access.Principal, w, id string, r RelationWrite) (apicontract.Response[apicontract.Reference], error) {
	if id == "" || r.Value.Order < 0 || !(r.Value.Kind == "reference" || r.Value.Kind == "uses" || r.Value.Kind == "generated" || r.Value.Kind == "semantic") {
		return apicontract.Response[apicontract.Reference]{}, errInput
	}
	if (r.Value.Kind == "uses" || r.Value.Kind == "generated") && r.Value.From.Kind != "run" {
		return apicontract.Response[apicontract.Reference]{}, errInput
	}
	if r.Value.Origin != nil && (r.Value.Origin.Kind == "" || r.Value.Origin.ID == "" || r.Value.Origin.Revision == "" || r.Value.Origin.Digest == "") {
		return apicontract.Response[apicontract.Reference]{}, errInput
	}
	if err := s.Access.Check(ctx, p, w, "", "register"); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	scope := persistence.Scope{ActorID: "user:" + p.UserID, Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}, Recheck: func(tx pgx.Tx) *apicontract.Problem {
		action, target := "register", ""
		if r.ExpectedVersion != "0" {
			action, target = "update", "relation:"+id
		}
		if err := s.Access.CheckTx(ctx, tx, p, w, target, action); err != nil {
			return &apicontract.Problem{Code: "forbidden"}
		}
		for _, ref := range []contexts.Ref{r.Value.From, r.Value.To} {
			if err := s.validateRef(ctx, tx, p, w, ref); err != nil {
				return &apicontract.Problem{Code: "forbidden"}
			}
		}
		return nil
	}}
	return s.Store.Apply(ctx, scope, persistence.Command{WorkspaceID: w, ResourceID: "relation:" + id, Kind: "graph-relation", Mutation: apicontract.Mutation{OperationID: r.OperationID, ExpectedVersion: r.ExpectedVersion}, Payload: encode(r.Value)}, func(tx pgx.Tx, ref apicontract.Reference) (json.RawMessage, error) {
		if _, err := tx.Exec(ctx, "INSERT INTO graph_relations SELECT $1,$2,$3,$4,sequence+1 FROM shared_streams WHERE workspace_id=$1", w, id, ref.Version, encode(r.Value)); err != nil {
			return nil, err
		}
		if r.ExpectedVersion == "0" {
			if err := s.Access.PutResource(ctx, tx, access.Resource{WorkspaceID: w, ID: "relation:" + id}, nil); err != nil {
				return nil, err
			}
		}
		return encode(struct {
			Ref   contexts.Ref `json:"ref"`
			Value Relation     `json:"value"`
		}{contexts.Ref{Kind: "relation", ID: id, Revision: ref.Version}, r.Value}), nil
	})
}
