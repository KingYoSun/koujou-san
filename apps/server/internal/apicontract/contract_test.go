package apicontract

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestGoFixturesMatchSharedWireData(t *testing.T) {
	bytes, err := os.ReadFile("../../../../contracts/fixtures-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var saved []Fixture
	if err := json.Unmarshal(bytes, &saved); err != nil {
		t.Fatal(err)
	}
	current, err := json.Marshal(Fixtures())
	if err != nil {
		t.Fatal(err)
	}
	var wire []Fixture
	if err := json.Unmarshal(current, &wire); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(wire, saved) {
		t.Fatal("Go output differs from the shared contract fixtures")
	}
}

func TestMutationReplayAndConflicts(t *testing.T) {
	request := Mutation{OperationID: "operation-1", ExpectedVersion: "version-1", RequestDigest: "canonical-digest"}
	if replay, problem := CheckMutation("version-1", request, nil); replay || problem != nil {
		t.Fatalf("valid update: replay=%v, problem=%v", replay, problem)
	}
	receipt := &Receipt{OperationID: request.OperationID, RequestDigest: request.RequestDigest}
	if replay, problem := CheckMutation("version-2", request, receipt); !replay || problem != nil {
		t.Fatalf("retry after update: replay=%v, problem=%v", replay, problem)
	}
	if _, problem := CheckMutation("version-2", request, nil); problem == nil || problem.Code != "version_conflict" || problem.CurrentVersion != "version-2" {
		t.Fatalf("stale update: %v", problem)
	}
	request.RequestDigest = "changed-payload"
	if _, problem := CheckMutation("version-2", request, receipt); problem == nil || problem.Code != "operation_conflict" {
		t.Fatalf("reused ID with changed payload: %v", problem)
	}
	for _, invalid := range []Mutation{{}, {OperationID: "op"}, {OperationID: "op", ExpectedVersion: "version"}} {
		if _, problem := CheckMutation("version", invalid, nil); problem == nil || problem.Code != "invalid_request" {
			t.Fatalf("incomplete operation accepted: %#v", invalid)
		}
	}
}

func TestGrantIsServerOwnedAndScoped(t *testing.T) {
	grant := Grant{WorkspaceID: "workspace-a", CanRead: true}
	if problem := Authorize(grant, "workspace-a", false); problem != nil {
		t.Fatal(problem)
	}
	for _, update := range []bool{false, true} {
		if problem := Authorize(grant, "workspace-b", update); problem == nil || problem.Code != "forbidden" {
			t.Fatalf("cross-Workspace access accepted: %v", problem)
		}
	}
	if problem := Authorize(grant, "workspace-a", true); problem == nil || problem.Code != "forbidden" {
		t.Fatal("read permission granted an update")
	}
	if problem := Authorize(Grant{}, "workspace-a", false); problem == nil {
		t.Fatal("missing server grant accepted")
	}
	input := []byte(`{"operation_id":"op","expected_version":"v1","request_digest":"untrusted"}`)
	var request Mutation
	if err := json.Unmarshal(input, &request); err != nil {
		t.Fatal(err)
	}
	if request.RequestDigest != "" {
		t.Fatal("client set the server request digest")
	}
	var supplied Grant
	if err := json.Unmarshal([]byte(`{"WorkspaceID":"workspace-a","CanRead":true,"CanUpdate":true}`), &supplied); err != nil {
		t.Fatal(err)
	}
	if supplied != (Grant{}) {
		t.Fatal("client supplied a server grant")
	}
	request.RequestDigest = "server-digest"
	receipt := &Receipt{OperationID: request.OperationID, RequestDigest: request.RequestDigest}
	if replay, problem := CheckAuthorizedMutation(grant, "workspace-a", "v2", request, receipt); replay || problem == nil || problem.Code != "forbidden" {
		t.Fatal("cached receipt bypassed current update permission")
	}
}
