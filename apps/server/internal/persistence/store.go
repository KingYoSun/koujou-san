// Package persistence commits domain writes, versions, change records and replay
// results in one PostgreSQL transaction. Grants come from trusted services, not wire DTOs.
package persistence

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed 001_shared.sql
var initialSchema string

//go:embed 002_auth.sql
var authSchema string

//go:embed 003_access.sql
var accessSchema string

//go:embed 004_context.sql
var contextSchema string

//go:embed 005_sync.sql
var syncSchema string

//go:embed 006_search.sql
var searchSchema string

//go:embed 007_graph.sql
var graphSchema string

//go:embed 008_operations.sql
var operationsSchema string

//go:embed 009_catalog.sql
var catalogSchema string

//go:embed 010_github.sql
var githubSchema string

//go:embed 011_usage.sql
var usageSchema string

//go:embed 012_automation.sql
var automationSchema string

//go:embed 013_runs.sql
var runsSchema string

//go:embed 014_assignment.sql
var assignmentSchema string

//go:embed 015_runner_queue.sql
var runnerQueueSchema string

//go:embed 016_workflows.sql
var workflowSchema string

//go:embed 017_inspections.sql
var inspectionSchema string

//go:embed 018_publications.sql
var publicationSchema string

//go:embed 019_workbench.sql
var workbenchSchema string

//go:embed 020_hopper.sql
var hopperSchema string

//go:embed 021_client_operations.sql
var clientOperationsSchema string

//go:embed 022_hopper_semantic.sql
var hopperSemanticSchema string

//go:embed 023_triage.sql
var triageSchema string

//go:embed 024_context_generation.sql
var contextGenerationSchema string

type Store struct {
	pool *pgxpool.Pool
	// Accepted is installed once at startup. It may write an outbox in this same
	// transaction; it must not perform external I/O or recursively call Apply.
	Accepted func(context.Context, pgx.Tx, Command, apicontract.Reference, json.RawMessage) error
}

func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, errors.New("invalid PostgreSQL configuration")
	}
	if err = pool.Ping(ctx); err == nil {
		err = migrate(ctx, pool, []string{initialSchema, authSchema, accessSchema, contextSchema, syncSchema, searchSchema, graphSchema, operationsSchema, catalogSchema, githubSchema, usageSchema, automationSchema, runsSchema, assignmentSchema, runnerQueueSchema, workflowSchema, inspectionSchema, publicationSchema, workbenchSchema, hopperSchema, clientOperationsSchema, hopperSemanticSchema, triageSchema, contextGenerationSchema})
	}
	if err != nil {
		pool.Close()
		return nil, errors.New("PostgreSQL connection or migration failed")
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close()                          { s.pool.Close() }
func (s *Store) Ready(ctx context.Context) error { return s.pool.Ping(ctx) }

// Database supplies the same pool to global identity/session services, whose
// data is separate from Workspace mutations and the Ory-owned databases.
func (s *Store) Database() *pgxpool.Pool { return s.pool }

// ProjectionChanged ignores operation/usage bookkeeping, which does not change
// Context candidates. The partial index bounds this lookup to projected data.
func (s *Store) ProjectionChanged(ctx context.Context, w string, after, through int64) (bool, error) {
	if after == through {
		return false, nil
	}
	var changed bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shared_changes WHERE workspace_id=$1 AND sequence>$2 AND sequence<=$3 AND (resource_id LIKE 'context:%' OR resource_id LIKE 'membership:%' OR resource_id LIKE 'relation:%'))`, w, after, through).Scan(&changed)
	return changed, err
}

func migrate(ctx context.Context, pool *pgxpool.Pool, scripts []string) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(724061401)"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS shared_migrations (version integer PRIMARY KEY, digest text NOT NULL)"); err != nil {
			return err
		}
		for index, source := range scripts {
			hash := sha256.Sum256([]byte(source))
			digest := hex.EncodeToString(hash[:])
			var previous string
			err := tx.QueryRow(ctx, "SELECT digest FROM shared_migrations WHERE version=$1", index+1).Scan(&previous)
			if err == nil {
				if previous != digest {
					return errors.New("applied migration differs")
				}
				continue
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if _, err = tx.Exec(ctx, source); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, "INSERT INTO shared_migrations VALUES ($1,$2)", index+1, digest); err != nil {
				return err
			}
		}
		return nil
	})
}

type Scope struct {
	ActorID string
	Grant   apicontract.Grant
	// Recheck runs under the Workspace stream lock, before receipt replay.
	Recheck func(pgx.Tx) *apicontract.Problem
}

type Command struct {
	WorkspaceID string
	ResourceID  string
	Kind        string
	Deleted     bool
	Mutation    apicontract.Mutation
	Payload     json.RawMessage
}

func metadata(workspace string, position *string) apicontract.Meta {
	return apicontract.Meta{WorkspaceID: workspace, Position: position, Completeness: apicontract.Completeness{State: "complete"}}
}

func rejected[T any](workspace, code, reason string) apicontract.Response[T] {
	return apicontract.Response[T]{Error: &apicontract.Problem{Code: code}, Meta: apicontract.Meta{WorkspaceID: workspace, Completeness: apicontract.Completeness{State: "pending", Reason: reason}}}
}

// Apply invokes the domain callback only for an authorized, unreplayed mutation.
// The callback writes its own tables and returns the committed state for projections.
// ExpectedVersion "0" means an ID that has never been created in this Workspace.
// A supplied transaction lets an orchestration owner reserve an action and use
// the normal domain mutation atomically. The caller owns commit/rollback.
func (s *Store) Apply(ctx context.Context, scope Scope, command Command, apply func(pgx.Tx, apicontract.Reference) (json.RawMessage, error), within ...pgx.Tx) (apicontract.Response[apicontract.Reference], error) {
	if problem := apicontract.Authorize(scope.Grant, command.WorkspaceID, true); problem != nil {
		return rejected[apicontract.Reference](command.WorkspaceID, problem.Code, "not_accessed"), nil
	}
	if scope.ActorID == "" || command.ResourceID == "" || command.Kind == "" || !json.Valid(command.Payload) {
		return rejected[apicontract.Reference](command.WorkspaceID, "invalid_request", "not_accessed"), nil
	}
	var payload any
	decoder := json.NewDecoder(bytes.NewReader(command.Payload))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return rejected[apicontract.Reference](command.WorkspaceID, "invalid_request", "not_accessed"), nil
	}
	canonical, err := json.Marshal(struct {
		ID, Kind, Expected string
		Deleted            bool
		Payload            any
	}{command.ResourceID, command.Kind, command.Mutation.ExpectedVersion, command.Deleted, payload})
	if err != nil {
		return rejected[apicontract.Reference](command.WorkspaceID, "invalid_request", "not_accessed"), nil
	}
	hash := sha256.Sum256(canonical)
	command.Mutation.RequestDigest = hex.EncodeToString(hash[:])
	if _, problem := apicontract.CheckMutation(command.Mutation.ExpectedVersion, command.Mutation, nil); problem != nil {
		return rejected[apicontract.Reference](command.WorkspaceID, problem.Code, "not_accessed"), nil
	}
	response := rejected[apicontract.Reference](command.WorkspaceID, "unavailable", "storage_unavailable")
	perform := func(tx pgx.Tx) error {
		origin := rand.Text()
		if _, err := tx.Exec(ctx, "INSERT INTO shared_streams (workspace_id,origin,position) VALUES ($1,$2,$2) ON CONFLICT DO NOTHING", command.WorkspaceID, origin); err != nil {
			return err
		}
		var position string
		var sequence int64
		if err := tx.QueryRow(ctx, "SELECT position,sequence FROM shared_streams WHERE workspace_id=$1 FOR UPDATE", command.WorkspaceID).Scan(&position, &sequence); err != nil {
			return err
		}
		if scope.Recheck != nil {
			if problem := scope.Recheck(tx); problem != nil {
				response = rejected[apicontract.Reference](command.WorkspaceID, problem.Code, "not_accessed")
				return nil
			}
		}
		var savedDigest string
		var saved json.RawMessage
		err := tx.QueryRow(ctx, "SELECT digest,result FROM shared_receipts WHERE workspace_id=$1 AND actor_id=$2 AND operation_id=$3", command.WorkspaceID, scope.ActorID, command.Mutation.OperationID).Scan(&savedDigest, &saved)
		if err == nil {
			previous := &apicontract.Receipt{OperationID: command.Mutation.OperationID, RequestDigest: savedDigest}
			if _, problem := apicontract.CheckMutation("", command.Mutation, previous); problem != nil {
				response = apicontract.Response[apicontract.Reference]{Error: problem, Meta: metadata(command.WorkspaceID, &position)}
				return nil
			}
			response = apicontract.Response[apicontract.Reference]{}
			return json.Unmarshal(saved, &response)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		current := "0"
		err = tx.QueryRow(ctx, "SELECT version FROM shared_versions WHERE workspace_id=$1 AND resource_id=$2", command.WorkspaceID, command.ResourceID).Scan(&current)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, problem := apicontract.CheckMutation(current, command.Mutation, nil); problem != nil {
			response = apicontract.Response[apicontract.Reference]{Error: problem, Meta: metadata(command.WorkspaceID, &position)}
			return nil
		}
		reference := apicontract.Reference{WorkspaceID: command.WorkspaceID, ID: command.ResourceID, Version: rand.Text()}
		state, err := apply(tx, reference)
		if err != nil {
			return err
		}
		// Nested domain writes may have advanced this already-locked stream.
		if err = tx.QueryRow(ctx, "SELECT sequence FROM shared_streams WHERE workspace_id=$1", command.WorkspaceID).Scan(&sequence); err != nil {
			return err
		}
		position = rand.Text()
		sequence++
		if _, err = tx.Exec(ctx, "INSERT INTO shared_versions VALUES ($1,$2,$3,$4) ON CONFLICT (workspace_id,resource_id) DO UPDATE SET version=EXCLUDED.version,deleted=EXCLUDED.deleted", reference.WorkspaceID, reference.ID, reference.Version, command.Deleted); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO shared_changes VALUES ($1,$2,$3,$4,$5,$6,$7,$8)", reference.WorkspaceID, sequence, position, reference.ID, reference.Version, command.Kind, command.Deleted, state); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE shared_streams SET sequence=$2,position=$3 WHERE workspace_id=$1", reference.WorkspaceID, sequence, position); err != nil {
			return err
		}
		if s.Accepted != nil {
			if err = s.Accepted(ctx, tx, command, reference, state); err != nil {
				return err
			}
		}
		response = apicontract.Response[apicontract.Reference]{Data: &reference, Meta: metadata(reference.WorkspaceID, &position)}
		response.Meta.OperationID = command.Mutation.OperationID
		result, err := json.Marshal(response)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "INSERT INTO shared_receipts VALUES ($1,$2,$3,$4,$5)", reference.WorkspaceID, scope.ActorID, command.Mutation.OperationID, command.Mutation.RequestDigest, result)
		return err
	}
	if len(within) > 0 && within[0] != nil {
		err = perform(within[0])
	} else {
		err = pgx.BeginFunc(ctx, s.pool, perform)
	}
	if err != nil {
		return rejected[apicontract.Reference](command.WorkspaceID, "unavailable", "storage_unavailable"), err
	}
	return response, nil
}

func (s *Store) Version(ctx context.Context, scope Scope, workspace, id string) (apicontract.Response[apicontract.Reference], error) {
	if problem := apicontract.Authorize(scope.Grant, workspace, false); problem != nil {
		return rejected[apicontract.Reference](workspace, problem.Code, "not_accessed"), nil
	}
	response := rejected[apicontract.Reference](workspace, "invalid_request", "not_accessed")
	var version, position string
	err := s.pool.QueryRow(ctx, "SELECT v.version,s.position FROM shared_versions v JOIN shared_streams s USING(workspace_id) WHERE v.workspace_id=$1 AND v.resource_id=$2", workspace, id).Scan(&version, &position)
	if errors.Is(err, pgx.ErrNoRows) {
		return response, nil
	}
	if err != nil {
		return rejected[apicontract.Reference](workspace, "unavailable", "storage_unavailable"), err
	}
	return apicontract.Response[apicontract.Reference]{Data: &apicontract.Reference{WorkspaceID: workspace, ID: id, Version: version}, Meta: metadata(workspace, &position)}, nil
}

type Change struct {
	apicontract.Reference
	Position string          `json:"position"`
	Kind     string          `json:"kind"`
	Deleted  bool            `json:"deleted"`
	Payload  json.RawMessage `json:"payload"`
}

type cursor struct{ Workspace, After, Through string }

// Changes freezes a page range at one committed position. Pass next_cursor to
// continue that range, or the last confirmed position to read later changes.
func (s *Store) Changes(ctx context.Context, scope Scope, workspace, after string, limit int) (apicontract.Response[[]Change], error) {
	if problem := apicontract.Authorize(scope.Grant, workspace, false); problem != nil {
		return rejected[[]Change](workspace, problem.Code, "not_accessed"), nil
	}
	if limit <= 0 {
		return rejected[[]Change](workspace, "invalid_request", "not_accessed"), nil
	}
	response := rejected[[]Change](workspace, "unavailable", "history_unavailable")
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var origin, head string
		var headSequence int64
		err := tx.QueryRow(ctx, "SELECT origin,position,sequence FROM shared_streams WHERE workspace_id=$1", workspace).Scan(&origin, &head, &headSequence)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		rangeCursor := cursor{Workspace: workspace, After: after, Through: head}
		if len(after) > 2 && after[:2] == "c." {
			raw, err := base64.RawURLEncoding.DecodeString(after[2:])
			if err != nil || json.Unmarshal(raw, &rangeCursor) != nil || rangeCursor.Workspace != workspace {
				response = rejected[[]Change](workspace, "invalid_request", "not_accessed")
				return nil
			}
		}
		if rangeCursor.After == "" {
			rangeCursor.After = origin
		}
		lookup := func(marker string) (int64, error) {
			if marker == origin {
				return 0, nil
			}
			var sequence int64
			err := tx.QueryRow(ctx, "SELECT sequence FROM shared_changes WHERE workspace_id=$1 AND position=$2", workspace, marker).Scan(&sequence)
			return sequence, err
		}
		begin, err := lookup(rangeCursor.After)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		end, err := lookup(rangeCursor.Through)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if begin > end || end > headSequence {
			return nil
		}
		rows, err := tx.Query(ctx, "SELECT sequence,position,resource_id,version,kind,deleted,payload FROM shared_changes WHERE workspace_id=$1 AND sequence>$2 AND sequence<=$3 ORDER BY sequence LIMIT $4", workspace, begin, end, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		changes := []Change{}
		sequence := begin
		for rows.Next() {
			var actual int64
			var change Change
			change.WorkspaceID = workspace
			if err = rows.Scan(&actual, &change.Position, &change.ID, &change.Version, &change.Kind, &change.Deleted, &change.Payload); err != nil {
				return err
			}
			if actual != sequence+1 {
				return nil
			}
			sequence = actual
			changes = append(changes, change)
		}
		if err = rows.Err(); err != nil {
			return err
		}
		if len(changes) <= limit && sequence != end {
			return nil
		}
		response = apicontract.Response[[]Change]{Data: &changes, Meta: metadata(workspace, &rangeCursor.Through)}
		if len(changes) > limit {
			changes = changes[:limit]
			response.Data = &changes
			rangeCursor.After = changes[len(changes)-1].Position
			raw, err := json.Marshal(rangeCursor)
			if err != nil {
				return err
			}
			next := "c." + base64.RawURLEncoding.EncodeToString(raw)
			response.Meta.NextCursor = &next
			response.Meta.Completeness = apicontract.Completeness{State: "partial", Reason: "limit_reached"}
		}
		return nil
	})
	if err != nil {
		return rejected[[]Change](workspace, "unavailable", "storage_unavailable"), err
	}
	return response, nil
}
