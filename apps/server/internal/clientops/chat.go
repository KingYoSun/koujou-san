package clientops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/jackc/pgx/v5"
)

type ChatThread struct {
	Name    string `json:"name"`
	Project Ref    `json:"project"`
}
type ChatMessage struct {
	Thread    Ref    `json:"thread"`
	Role      string `json:"role"`
	Text      string `json:"text"`
	User      string `json:"user,omitempty"`
	State     string `json:"state"`
	Operation *Ref   `json:"operation,omitempty"`
}
type ChatSource struct {
	Thread   Ref   `json:"thread"`
	Messages []Ref `json:"messages"`
}
type ChatLink struct {
	Thread Ref    `json:"thread"`
	Target Ref    `json:"target"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
}

func chatFields(v *ChatSource) bool {
	if v == nil || !pinned(v.Thread) || !strings.HasPrefix(v.Thread.ID, "chat-thread:") || len(v.Messages) == 0 {
		return false
	}
	for _, ref := range v.Messages {
		if !pinned(ref) || !strings.HasPrefix(ref.ID, "chat-message:") {
			return false
		}
	}
	return true
}

func (s *Service) chatThread(ctx context.Context, q query, p access.Principal, w string, ref Ref, edit bool) (ChatThread, []Ref, error) {
	var v ChatThread
	r, err := record(ctx, q, w, ref)
	if err != nil || r.Kind != "chat-thread" || decode(r.Value, &v) != nil || !pinned(ref) {
		return v, nil, ErrInvalid
	}
	deps := append(append([]Ref{}, r.Refs...), r.Ref)
	if err = s.checkRefs(ctx, q, p, w, deps); err != nil {
		return v, nil, err
	}
	if edit {
		if tx, ok := q.(pgx.Tx); ok {
			err = s.Access.CheckTx(ctx, tx, p, w, v.Project.ID, "update")
		} else {
			err = s.Access.Check(ctx, p, w, v.Project.ID, "update")
		}
	}
	return v, deps, err
}

func (s *Service) chatSources(ctx context.Context, q query, p access.Principal, w string, v *ChatSource) ([]Ref, error) {
	if !chatFields(v) {
		return nil, ErrInvalid
	}
	_, deps, err := s.chatThread(ctx, q, p, w, v.Thread, false)
	if err != nil {
		return nil, err
	}
	for _, ref := range v.Messages {
		r, err := record(ctx, q, w, ref)
		var message ChatMessage
		if err != nil || r.Kind != "chat-message" || r.Root != v.Thread.ID || decode(r.Value, &message) != nil || message.State != "complete" {
			return nil, ErrInvalid
		}
		deps = append(append(deps, r.Ref), r.Refs...)
	}
	deps = unique(deps)
	return deps, s.checkRefs(ctx, q, p, w, deps)
}

func (s *Service) PutChatThread(ctx context.Context, p access.Principal, w, id string, req Write) (apicontract.Response[apicontract.Reference], error) {
	var v ChatThread
	if decode(req.Value, &v) != nil || strings.TrimSpace(v.Name) == "" || !strings.HasPrefix(id, "chat-thread:") || !pinned(v.Project) || !strings.HasPrefix(v.Project.ID, "scope:") || p.OperationID != "" || req.ExpectedVersion != "0" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		project, err := s.Contexts.ReadTx(ctx, tx, p, w, contexts.Ref{Kind: "scope", ID: strings.TrimPrefix(v.Project.ID, "scope:"), Revision: v.Project.Version})
		var scope contexts.Scope
		if err != nil || decode(project.Value, &scope) != nil || scope.Kind != "project" {
			return Record{}, ErrInvalid
		}
		if err = s.Access.CheckTx(ctx, tx, p, w, v.Project.ID, "update"); err != nil {
			return Record{}, err
		}
		return s.save(ctx, tx, w, p, ref, "chat-thread", v, []Ref{v.Project}, v.Project.ID)
	})
}

func (s *Service) PutChatMessage(ctx context.Context, p access.Principal, w, id string, req Write) (apicontract.Response[apicontract.Reference], error) {
	var v ChatMessage
	if decode(req.Value, &v) != nil || !strings.HasPrefix(id, "chat-message:") || strings.TrimSpace(v.Text) == "" || v.Role != "human" || v.Operation != nil || v.User != "" || v.State != "complete" || p.OperationID != "" || req.ExpectedVersion != "0" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		_, deps, err := s.chatThread(ctx, tx, p, w, v.Thread, true)
		if err != nil {
			return Record{}, err
		}
		v.User = p.UserID
		return s.save(ctx, tx, w, p, ref, "chat-message", v, deps, v.Thread.ID)
	})
}

// Link only records already accepted by their owning service. Reading a chat
// link never starts or resumes its target.
func (s *Service) PutChatLink(ctx context.Context, p access.Principal, w, id string, req Write) (apicontract.Response[apicontract.Reference], error) {
	var v ChatLink
	if decode(req.Value, &v) != nil || !strings.HasPrefix(id, "chat-link:") || !pinned(v.Target) || p.OperationID != "" || req.ExpectedVersion != "0" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		_, deps, err := s.chatThread(ctx, tx, p, w, v.Thread, true)
		if err != nil {
			return Record{}, err
		}
		var source Record
		switch v.Kind {
		case "run":
			r, e := s.Runs.Read(ctx, p, w, v.Target)
			err = e
			source = Record{Ref: r.Ref, Value: r.Value, Refs: r.Refs}
		case "run-definition", "workflow-definition", "execution":
			if s.Definitions == nil {
				return Record{}, ErrState
			}
			r, e := s.Definitions.InTransaction(tx).Read(ctx, p, w, v.Target)
			err = e
			if r.Kind != v.Kind {
				return Record{}, ErrInvalid
			}
			source = Record{Ref: r.Ref, Value: r.Value, Refs: r.Refs}
		case "issue":
			if !strings.HasPrefix(v.Target.ID, "scope:") {
				return Record{}, ErrInvalid
			}
			r, e := s.Contexts.ReadTx(ctx, tx, p, w, contexts.Ref{Kind: "scope", ID: strings.TrimPrefix(v.Target.ID, "scope:"), Revision: v.Target.Version})
			err = e
			var scope contexts.Scope
			if err == nil && (decode(r.Value, &scope) != nil || scope.Kind != "issue") {
				return Record{}, ErrInvalid
			}
			source = Record{Ref: v.Target, Value: r.Value}
		default:
			return Record{}, ErrInvalid
		}
		if err != nil {
			return Record{}, err
		}
		var label struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(source.Value, &label)
		v.Name = label.Name
		deps = unique(append(append(deps, source.Ref), source.Refs...))
		if err = s.checkRefs(ctx, tx, p, w, deps); err != nil {
			return Record{}, err
		}
		return s.save(ctx, tx, w, p, ref, "chat-link", v, deps, v.Thread.ID)
	})
}

type ChatAdopt struct {
	apicontract.Mutation
	Message Ref    `json:"message"`
	Start   int    `json:"start_utf8"`
	End     int    `json:"end_utf8"`
	Name    string `json:"name"`
}
type ChatAdoption struct {
	Message Ref          `json:"message"`
	Start   int          `json:"start_utf8"`
	End     int          `json:"end_utf8"`
	Context contexts.Ref `json:"context"`
}

func (s *Service) AdoptChat(ctx context.Context, p access.Principal, w, id string, req ChatAdopt) (apicontract.Response[apicontract.Reference], error) {
	if !strings.HasPrefix(id, "chat-adoption:") || !pinned(req.Message) || req.ExpectedVersion != "0" || p.OperationID != "" {
		return apicontract.Response[apicontract.Reference]{}, ErrInvalid
	}
	return s.mutate(ctx, p, w, id, req.Mutation, req, func(tx pgx.Tx, ref Ref) (Record, error) {
		r, err := record(ctx, tx, w, req.Message)
		var msg ChatMessage
		if err != nil || r.Kind != "chat-message" || decode(r.Value, &msg) != nil || msg.State != "complete" || req.Start < 0 || req.End <= req.Start || req.End > len(msg.Text) || !utf8.ValidString(msg.Text[:req.Start]) || !utf8.ValidString(msg.Text[:req.End]) {
			return Record{}, ErrInvalid
		}
		thread, deps, err := s.chatThread(ctx, tx, p, w, msg.Thread, true)
		if err != nil {
			return Record{}, err
		}
		deps = unique(append(append(deps, r.Ref), r.Refs...))
		if err = s.checkRefs(ctx, tx, p, w, deps); err != nil {
			return Record{}, err
		}
		text := msg.Text[req.Start:req.End]
		digest := sha256.Sum256([]byte(msg.Text))
		origin := &contexts.Origin{Kind: "chat-message", ID: r.ID, Revision: r.Version, Digest: hex.EncodeToString(digest[:]), Path: fmt.Sprintf("/text/utf8/%d:%d", req.Start, req.End), OperationID: req.OperationID}
		contextID := "chat-" + hash(id)
		c := s.Contexts.InTransaction(tx)
		out, err := accepted(c.Put(ctx, p, w, contexts.Identity{Kind: "sentence", ID: contextID}, contexts.Write{Mutation: apicontract.Mutation{OperationID: "chat-context:" + id, ExpectedVersion: "0"}, Value: raw(contexts.Unit{Kind: "sentence", Name: req.Name, Text: &text, Origin: origin})}))
		if err != nil {
			return Record{}, err
		}
		_, err = accepted(c.Put(ctx, p, w, contexts.Identity{Kind: "membership", ID: contextID}, contexts.Write{Mutation: apicontract.Mutation{OperationID: "chat-group:" + id, ExpectedVersion: "0"}, Value: raw(contexts.Membership{Scope: contexts.Identity{Kind: "project", ID: strings.TrimPrefix(thread.Project.ID, "scope:")}, Context: contexts.Identity{Kind: "sentence", ID: contextID}, Present: true})}))
		if err != nil {
			return Record{}, err
		}
		v := ChatAdoption{Message: r.Ref, Start: req.Start, End: req.End, Context: contexts.Ref{Kind: "sentence", ID: contextID, Revision: out.Version}}
		return s.save(ctx, tx, w, p, ref, "chat-adoption", v, append(deps, out), msg.Thread.ID)
	})
}

func (s *Service) finishChat(ctx context.Context, tx pgx.Tx, p access.Principal, w string, operation Ref, op *Operation, policy Policy, reply Reply, reason string) error {
	if policy.Chat == nil || len(reply.Choices) == 0 || reply.Choices[0].Message.Content == nil || len(reply.Choices[0].Message.Calls) > 0 {
		return ErrState
	}
	_, deps, err := s.chatThread(ctx, tx, p, w, policy.Chat.Thread, false)
	if err != nil {
		return err
	}
	for _, source := range policy.Chat.Messages {
		r, err := record(ctx, tx, w, source)
		if err != nil {
			return err
		}
		// Message bodies share the thread's audience. Keep external authorization
		// dependencies, without copying the entire ancestry of conversation IDs.
		deps = append(deps, r.Refs...)
	}
	deps = append(deps, refs(policy.Fixed)...)
	observed, err := s.mcpSources(ctx, tx, w, op.ReadOperation, nil, false)
	if err != nil {
		return err
	}
	deps = unique(append(deps, observed...))
	state := "complete"
	op.State, op.Reason = "completed", ""
	if reason != "" {
		state = "incomplete"
		op.State, op.Reason = "incomplete", reason
	}
	v := ChatMessage{Thread: policy.Chat.Thread, Role: "assistant", Text: *reply.Choices[0].Message.Content, State: state, Operation: &operation}
	ref, err := s.writeRelated(ctx, tx, p, w, "chat-message:"+operation.Version, "chat-message", v, deps, policy.Chat.Thread.ID)
	if err == nil {
		op.Applied = &ref
	}
	return err
}
