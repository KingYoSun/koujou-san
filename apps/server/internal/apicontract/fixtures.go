package apicontract

import (
	"crypto/sha256"
	"encoding/hex"
)

type Fixture struct {
	Name     string              `json:"name"`
	Request  *Mutation           `json:"request,omitempty"`
	Response Response[Reference] `json:"response"`
}

// Fixtures exercises the common boundary only, not a product repository or
// authentication provider. The returned JSON is also checked by TypeScript.
func Fixtures() []Fixture {
	ref := Reference{WorkspaceID: "workspace-a", ID: "fixture-a", Version: "9007199254740993"}
	position := "position-a"
	meta := Meta{WorkspaceID: ref.WorkspaceID, Position: &position, Completeness: Completeness{State: "complete"}}
	grant := Grant{WorkspaceID: ref.WorkspaceID, CanRead: true, CanUpdate: true}
	digest := sha256.Sum256([]byte("fixture-a:replacement"))
	request := Mutation{OperationID: "operation-a", ExpectedVersion: ref.Version, RequestDigest: hex.EncodeToString(digest[:])}
	updated := ref
	updated.Version = "9007199254740994"
	_, updateProblem := CheckAuthorizedMutation(grant, ref.WorkspaceID, ref.Version, request, nil)
	_, replayProblem := CheckAuthorizedMutation(grant, ref.WorkspaceID, updated.Version, request, &Receipt{OperationID: request.OperationID, RequestDigest: request.RequestDigest})
	_, conflict := CheckMutation(updated.Version, Mutation{OperationID: "operation-b", ExpectedVersion: ref.Version, RequestDigest: request.RequestDigest}, nil)
	changed := request
	changed.RequestDigest = "different-request"
	_, reused := CheckMutation(updated.Version, changed, &Receipt{OperationID: request.OperationID, RequestDigest: request.RequestDigest})
	updateMeta := meta
	updateMeta.OperationID = request.OperationID
	updatedPosition := "position-b"
	updateMeta.Position = &updatedPosition
	conflictRequest := Mutation{OperationID: "operation-b", ExpectedVersion: ref.Version, RequestDigest: request.RequestDigest}
	conflictMeta := meta
	conflictMeta.OperationID = conflictRequest.OperationID
	conflictMeta.Position = &updatedPosition
	deniedMeta := meta
	deniedMeta.WorkspaceID = "workspace-b"
	deniedMeta.Position = nil
	deniedMeta.Completeness = Completeness{State: "pending", Reason: "not_accessed"}
	partial := meta
	partial.Completeness = Completeness{State: "partial", Reason: "limit_reached"}
	next := "opaque-page-a"
	partial.NextCursor = &next
	pending := meta
	pending.Completeness = Completeness{State: "pending", Reason: "projection_lag"}
	return []Fixture{
		{Name: "read", Response: Response[Reference]{Data: &ref, Error: Authorize(grant, ref.WorkspaceID, false), Meta: meta}},
		{Name: "update", Request: &request, Response: Response[Reference]{Data: &updated, Error: updateProblem, Meta: updateMeta}},
		{Name: "replay", Request: &request, Response: Response[Reference]{Data: &updated, Error: replayProblem, Meta: updateMeta}},
		{Name: "version-conflict", Request: &conflictRequest, Response: Response[Reference]{Error: conflict, Meta: conflictMeta}},
		{Name: "operation-conflict", Request: &changed, Response: Response[Reference]{Error: reused, Meta: updateMeta}},
		{Name: "workspace-denied", Response: Response[Reference]{Error: Authorize(grant, "workspace-b", false), Meta: deniedMeta}},
		{Name: "update-denied", Response: Response[Reference]{Error: Authorize(Grant{WorkspaceID: ref.WorkspaceID, CanRead: true}, ref.WorkspaceID, true), Meta: meta}},
		{Name: "partial", Response: Response[Reference]{Data: &ref, Meta: partial}},
		{Name: "pending", Response: Response[Reference]{Data: &ref, Meta: pending}},
	}
}
