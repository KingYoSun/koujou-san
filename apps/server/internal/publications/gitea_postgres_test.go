package publications

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"testing"
	"time"
)

type giteaPublicationBackend struct {
	connection Connection
	posts      int
	issue      Issue
}

func (b *giteaPublicationBackend) Connection(w, id string) (Connection, bool) {
	return b.connection, w == b.connection.Workspace && id == b.connection.ID
}
func (b *giteaPublicationBackend) Create(_ context.Context, _ Connection, d Draft, body string) Outcome {
	b.posts++
	b.issue = Issue{ID: 1, Number: 1, URL: b.connection.Instance + "/proof/private/issues/1", Title: d.Title, Body: body, State: "open", UpdatedAt: time.Now().UTC()}
	return outcome("created", "", &b.issue)
}
func (b *giteaPublicationBackend) Observe(context.Context, Connection, string, int64) Outcome {
	return outcome("created", "", &b.issue)
}

func checkGiteaSharedPublication(t *testing.T, ctx context.Context, store *persistence.Store, a *access.Service) {
	t.Helper()
	r := runs.New(store, a, contexts.New(store, a, contexts.DefaultLimits()))
	backend := &giteaPublicationBackend{connection: Connection{ID: "gitea-proof", Workspace: "one", Provider: "gitea", Instance: "https://gitea.example.test", Repository: "proof/private", RepositoryID: 1, ActorID: 1}}
	s := New(store, a, r, backend)
	p := access.Principal{UserID: "admin"}
	m := func(v string) apicontract.Mutation {
		return apicontract.Mutation{OperationID: "gitea-pg-" + rand.Text(), ExpectedVersion: v}
	}
	accept := func(v apicontract.Response[apicontract.Reference], err error) Ref {
		ref, e := accepted(v, err)
		if e != nil {
			t.Fatal(e)
		}
		return ref
	}
	draft := accept(s.Put(ctx, p, "one", "publication-draft:gitea", Write{Mutation: m("0"), Value: raw(Draft{Name: "Gitea proof", Connection: backend.connection.ID, Title: "proof", Body: "fixed body", Sources: []Ref{}})}))
	request := accept(s.Submit(ctx, p, "one", "publication:gitea", Submit{Mutation: m("0"), Draft: draft}))
	for range 2 {
		if err := s.Process(ctx, "one", request.ID); err != nil {
			t.Fatal(err)
		}
	}
	record, err := s.Read(ctx, p, "one", Ref{ID: request.ID, Version: "head"})
	if err != nil {
		t.Fatal(err)
	}
	var value Request
	_ = json.Unmarshal(record.Value, &value)
	if backend.posts != 1 || value.Connection != backend.connection || value.State != "created" {
		t.Fatal("Gitea shared acceptance/replay lost identity")
	}
	run, err := r.Read(ctx, p, "one", Ref{ID: value.Run, Version: "head"})
	if err != nil {
		t.Fatal(err)
	}
	var state runs.Run
	_ = json.Unmarshal(run.Value, &state)
	if state.Execution == nil || state.Execution.Version != "gitea-issues-v1" {
		t.Fatal("Gitea connector reported a GitHub version")
	}
	held := accept(s.Submit(ctx, p, "one", "publication:gitea-held", Submit{Mutation: m("0"), Draft: draft}))
	backend.connection.Instance = "https://another.example.test"
	if err := s.Process(ctx, "one", held.ID); err != nil {
		t.Fatal(err)
	}
	record, err = s.Read(ctx, p, "one", Ref{ID: held.ID, Version: "head"})
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(record.Value, &value)
	if value.State != "held" || backend.posts != 1 {
		t.Fatal("changed instance sent an accepted request")
	}
	t.Log("Gitea shared fixed connection, one send, connector version and changed-instance hold passed")
}
