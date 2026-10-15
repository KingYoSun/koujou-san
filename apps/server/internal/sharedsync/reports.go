package sharedsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type ReportInput struct {
	ID               string `json:"event_id"`
	TargetID         string `json:"target_id"`
	Sequence         string `json:"sequence"`
	Kind             string `json:"kind"`
	ArtifactID       string `json:"artifact_id,omitempty"`
	ArtifactRevision string `json:"artifact_revision,omitempty"`
}
type ReportSummary struct {
	TargetID           string      `json:"target_id"`
	DeviceID           string      `json:"device_id"`
	Highest            string      `json:"highest_observed"`
	Contiguous         string      `json:"contiguous_through"`
	Missing            bool        `json:"missing"`
	CompletionObserved bool        `json:"completion_observed"`
	ResultState        string      `json:"result_state"`
	LastEvent          ReportInput `json:"last_event"`
}

var errSequence = errors.New("report_sequence_conflict")
var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func topic(target, device string) string {
	sum := sha256.Sum256(encode([]string{target, device}))
	return hex.EncodeToString(sum[:])
}
func (s *Service) readReport(ctx context.Context, p access.Principal, w, id, revision string) (contexts.Record, error) {
	var payload json.RawMessage
	var version string
	err := s.Store.Database().QueryRow(ctx, `SELECT revision,payload FROM sync_report_versions WHERE workspace_id=$1 AND topic_id=$2 AND ($3='' OR revision=$3) ORDER BY stream_sequence DESC LIMIT 1`, w, id, revision).Scan(&version, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return contexts.Record{}, access.ErrForbidden
	}
	if err != nil {
		return contexts.Record{}, err
	}
	var summary ReportSummary
	if err = json.Unmarshal(payload, &summary); err != nil {
		return contexts.Record{}, err
	}
	if err = s.Access.Check(ctx, p, w, summary.TargetID, "get"); err != nil {
		return contexts.Record{}, err
	}
	return contexts.Record{Ref: contexts.Ref{Kind: "report", ID: id, Revision: version}, Value: payload}, nil
}
func (s *Service) Report(ctx context.Context, p access.Principal, w string, event ReportInput) (apicontract.Response[apicontract.Reference], error) {
	sequence, err := strconv.ParseInt(event.Sequence, 10, 64)
	validKind := event.Kind == "input_observed" || event.Kind == "pause_observed" || event.Kind == "output_observed" || event.Kind == "completion_observed"
	if err != nil || sequence <= 0 || !identifier.MatchString(event.ID) || event.TargetID == "" || !validKind || (event.ArtifactID == "") != (event.ArtifactRevision == "") || (event.ArtifactID != "" && (!identifier.MatchString(event.ArtifactID) || !identifier.MatchString(event.ArtifactRevision))) {
		return apicontract.Response[apicontract.Reference]{}, errInput
	}
	if p.DeviceID == "" {
		return apicontract.Response[apicontract.Reference]{}, access.ErrForbidden
	}
	if err = s.Access.Check(ctx, p, w, event.TargetID, "report"); err != nil {
		return apicontract.Response[apicontract.Reference]{}, err
	}
	scope := persistence.Scope{ActorID: "device:" + p.DeviceID, Grant: apicontract.Grant{WorkspaceID: w, CanRead: true, CanUpdate: true}, Recheck: func(tx pgx.Tx) *apicontract.Problem {
		if err := s.Access.CheckTx(ctx, tx, p, w, event.TargetID, "report"); err != nil {
			code := "unavailable"
			if errors.Is(err, access.ErrForbidden) {
				code = "forbidden"
			}
			return &apicontract.Problem{Code: code}
		}
		return nil
	}}
	return s.Store.Apply(ctx, scope, persistence.Command{WorkspaceID: w, ResourceID: "report-event:" + event.ID, Kind: "sync-report", Mutation: apicontract.Mutation{OperationID: "report:" + event.ID, ExpectedVersion: "0"}, Payload: encode(event)}, func(tx pgx.Tx, r apicontract.Reference) (json.RawMessage, error) {
		id := topic(event.TargetID, p.DeviceID)
		if _, err := tx.Exec(ctx, "INSERT INTO sync_reports VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", w, event.ID, id, event.TargetID, p.DeviceID, sequence, event.Kind, event.ArtifactID, event.ArtifactRevision); err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				return nil, errSequence
			}
			return nil, err
		}
		summary := ReportSummary{TargetID: event.TargetID, DeviceID: p.DeviceID, Highest: "0", Contiguous: "0", ResultState: "unconfirmed"}
		var previous json.RawMessage
		err := tx.QueryRow(ctx, "SELECT payload FROM sync_report_versions WHERE workspace_id=$1 AND topic_id=$2 ORDER BY stream_sequence DESC LIMIT 1", w, id).Scan(&previous)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			if err = json.Unmarshal(previous, &summary); err != nil {
				return nil, err
			}
		}
		highest, _ := strconv.ParseInt(summary.Highest, 10, 64)
		contiguous, _ := strconv.ParseInt(summary.Contiguous, 10, 64)
		if sequence > highest {
			highest = sequence
		}
		for contiguous < highest {
			var exists bool
			if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM sync_reports WHERE workspace_id=$1 AND topic_id=$2 AND source_sequence=$3)", w, id, contiguous+1).Scan(&exists); err != nil {
				return nil, err
			}
			if !exists {
				break
			}
			contiguous++
		}
		summary.Highest = strconv.FormatInt(highest, 10)
		summary.Contiguous = strconv.FormatInt(contiguous, 10)
		summary.Missing = contiguous < highest
		summary.CompletionObserved = summary.CompletionObserved || event.Kind == "completion_observed"
		summary.LastEvent = event
		payload := encode(summary)
		if _, err = tx.Exec(ctx, "INSERT INTO sync_report_versions SELECT $1,$2,$3,sequence+1,$4 FROM shared_streams WHERE workspace_id=$1", w, id, r.Version, payload); err != nil {
			return nil, err
		}
		return encode(contexts.Record{Ref: contexts.Ref{Kind: "report", ID: id, Revision: r.Version}, Value: payload}), nil
	})
}
