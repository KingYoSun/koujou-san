// Package apicontract contains the shared HTTP/MCP wire boundary. Domain services
// provide authenticated grants and authoritative data; clients never supply grants.
package apicontract

type Reference struct {
	WorkspaceID string `json:"workspace_id"`
	ID          string `json:"id"`
	Version     string `json:"version"`
}

type Completeness struct {
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

type Meta struct {
	WorkspaceID  string       `json:"workspace_id"`
	OperationID  string       `json:"operation_id,omitempty"`
	Position     *string      `json:"position"`
	NextCursor   *string      `json:"next_cursor"`
	Completeness Completeness `json:"completeness"`
}

type Problem struct {
	Code           string `json:"code"`
	CurrentVersion string `json:"current_version,omitempty"`
}

type Response[T any] struct {
	Data  *T       `json:"data,omitempty"`
	Error *Problem `json:"error,omitempty"`
	Meta  Meta     `json:"meta"`
}

// Grant is internal, derived from verified identity and current server policy.
// It is deliberately absent from every request/response DTO.
type Grant struct {
	WorkspaceID string `json:"-"`
	CanRead     bool   `json:"-"`
	CanUpdate   bool   `json:"-"`
}

func Authorize(grant Grant, workspaceID string, update bool) *Problem {
	if workspaceID == "" {
		return &Problem{Code: "invalid_request"}
	}
	if grant.WorkspaceID != workspaceID || !grant.CanRead || (update && !grant.CanUpdate) {
		return &Problem{Code: "forbidden"}
	}
	return nil
}

type Mutation struct {
	OperationID     string `json:"operation_id"`
	ExpectedVersion string `json:"expected_version"`
	RequestDigest   string `json:"-"`
}

type Receipt struct {
	OperationID   string
	RequestDigest string
}

func CheckAuthorizedMutation(grant Grant, workspaceID, currentVersion string, request Mutation, previous *Receipt) (bool, *Problem) {
	if problem := Authorize(grant, workspaceID, true); problem != nil {
		return false, problem
	}
	return CheckMutation(currentVersion, request, previous)
}

// CheckMutation runs after current authorization. Receipts are scoped to the
// authenticated actor, Workspace and operation ID by the persistence service.
// A replay precedes the current-version comparison because the first request
// already changed that version. Storage and atomicity belong to Issue #4.
func CheckMutation(currentVersion string, request Mutation, previous *Receipt) (replay bool, problem *Problem) {
	if request.OperationID == "" || request.ExpectedVersion == "" || request.RequestDigest == "" {
		return false, &Problem{Code: "invalid_request"}
	}
	if previous != nil && previous.OperationID == request.OperationID {
		if previous.RequestDigest != request.RequestDigest {
			return false, &Problem{Code: "operation_conflict"}
		}
		return true, nil
	}
	if currentVersion != request.ExpectedVersion {
		return false, &Problem{Code: "version_conflict", CurrentVersion: currentVersion}
	}
	return false, nil
}
