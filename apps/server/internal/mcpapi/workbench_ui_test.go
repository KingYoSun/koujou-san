package mcpapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/apicontract"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workbench"
)

// The common UI observes two actual owned subprocesses through this fixture
// bridge. Native Runner/OS evidence is reused; this is not a production executor.
func prepareWorkbenchUI(t *testing.T, s *Service, p access.Principal, otherUser string, key []byte, origin string) (func(http.ResponseWriter, *http.Request) bool, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	w := "mcp-one"
	mutation := func(version string) apicontract.Mutation {
		return apicontract.Mutation{OperationID: "workbench-ui-" + rand.Text(), ExpectedVersion: version}
	}
	accept := func(value apicontract.Response[apicontract.Reference], err error) (runs.Ref, error) {
		if err != nil {
			return runs.Ref{}, err
		}
		if value.Data == nil {
			return runs.Ref{}, fmt.Errorf("workbench fixture mutation rejected")
		}
		return runs.Ref{ID: value.Data.ID, Version: value.Data.Version}, nil
	}
	if _, err := s.Access.DB.Exec(ctx, `INSERT INTO access_devices(id,workspace_id,registered_by,identifier,name,generation,can_input,can_report) VALUES('workbench-device',$1,$2,'workbench-fixture','共通画面の実CLI試験端末','g',true,true)`, w, p.UserID); err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("device\x00workbench-device\x00g"))
	device, err := s.Access.DevicePrincipal(ctx, w, "d.workbench-device."+base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	if err != nil {
		t.Fatal(err)
	}
	connections := []runs.Connection{{Kind: "cli", Target: "workbench-fixture", Version: "owned-cli-fixture-v1", Environment: "owned-subprocess-fixture", Subject: "synthetic-fixture", Confirmed: true, Bindings: []string{"stdin"}, Capabilities: []string{"pause"}, Users: []string{p.UserID}}}
	runnerRef, err := accept(s.Runs.ContactRunner(ctx, device, w, runs.RunnerContact{Mutation: mutation("0"), RunnerID: "owned-workbench-bridge", Connections: connections}))
	if err != nil {
		t.Fatal(err)
	}
	var runnerLock sync.Mutex
	lastContact := time.Now()
	contact := func() error {
		runnerLock.Lock()
		defer runnerLock.Unlock()
		if time.Since(lastContact) < 20*time.Second {
			return nil
		}
		var err error
		runnerRef, err = accept(s.Runs.ContactRunner(ctx, device, w, runs.RunnerContact{Mutation: mutation(runnerRef.Version), RunnerID: "owned-workbench-bridge", Connections: connections}))
		if err == nil {
			lastContact = time.Now()
		}
		return err
	}
	preview := `<!doctype html><html lang="ja"><meta charset="utf-8"><title>固定ビルド build-v1</title><style>body{font:20px sans-serif;max-width:700px;margin:64px auto;padding:24px}button{font:inherit;padding:16px 30px}small{display:block;margin-top:32px}</style><h1>編集内容の確認</h1><p>入力した内容を保存して、前の画面へ戻ります。</p><button>保存</button><small>受入確認用の固定成果物・build-v1</small></html>`
	digest := sha256.Sum256([]byte(preview))
	artifact := contexts.Fixed{URI: origin + "/__fixture/workbench-build", Revision: "build-v1", Range: "whole", Digest: hex.EncodeToString(digest[:]), Delivery: "reference", State: "unfetched"}
	deliver, resume := make(chan struct{}, 1), make(chan struct{}, 1)
	ready := make(chan struct{}, 2)
	for _, scenario := range []struct {
		id, name string
		human    bool
	}{{"workbench-team", "受入: 専門判断を別メンバーへ回す", false}, {"workbench-human", "受入: 固定ビルドの画面を確認", true}} {
		profile := map[string]any{"question": "利用者向けの案内をどちらにしますか？", "background": "次の画面へ進む前に、業務を判断できる担当者へ表現を確認します。", "instructions": "選択肢と影響を読み、判断できない場合もその回答を残してください。", "waiting_for": "利用者向け案内の採用", "expertise": "業務の表現を判断できる担当者", "options": []map[string]string{{"value": "direct", "label": "短い案内", "impact": "操作を短く案内します。"}, {"value": "detailed", "label": "詳しい案内", "impact": "確認点を文章で補います。"}, {"value": "INCONCLUSIVE", "label": "判断できない", "impact": "理由を残し、追加確認を判断します。"}}}
		if scenario.human {
			profile = map[string]any{"question": "この固定ビルドの保存ボタンは、条件を満たしていますか？", "background": "編集した内容を保存し、前の画面へ戻る操作の案内を確認します。製品の過去の経緯やCLIの知識は不要です。", "instructions": "成果物を開き、保存ボタンの表示を条件と比べてください。判断・理由・確認した成果物URLを回答します。", "waiting_for": "固定ビルドの受入", "response_format": "verdict", "evidence_required": true, "target": map[string]string{"label": "編集内容の確認画面", "revision": artifact.Revision, "uri": artifact.URI}, "criteria": []map[string]string{{"id": "AC-save-label", "requirement": "保存して戻る操作であることが分かるよう、ボタンに『保存して戻る』と表示される。"}}, "options": []map[string]string{{"value": "PASS", "label": "条件を満たした", "impact": "この条件の確認結果を保存します。"}, {"value": "FAIL", "label": "条件を満たさない", "impact": "問題を記録し、修正と再検品を判断します。"}, {"value": "INCONCLUSIVE", "label": "判断できない", "impact": "未確認事項を残し、追加確認を判断します。"}}}
		}
		spec := runs.Spec{Name: scenario.name, Input: runs.Input{Command: runs.Command{Kind: "cli", Target: "workbench-fixture", Binding: map[string]string{"prompt": "stdin"}}, Prompt: rawUI(profile)}, Artifacts: []contexts.Fixed{artifact}, Derivation: &runs.Contract{Name: "HumanReviewRun", Version: "ui-fixture-v1", VerdictField: "verdict"}}
		root, err := accept(s.Runs.Create(ctx, p, w, "run:"+scenario.id, runs.Create{Mutation: mutation("0"), Spec: spec}))
		if err != nil {
			t.Fatal(err)
		}
		root, err = accept(s.Runs.Assign(ctx, p, w, root.ID, runs.Assign{Mutation: mutation(root.Version), DeviceID: device.DeviceID}))
		if err != nil {
			t.Fatal(err)
		}
		record, err := s.Runs.Read(ctx, p, w, root)
		if err != nil {
			t.Fatal(err)
		}
		var state runs.Run
		_ = json.Unmarshal(record.Value, &state)
		if state.Assignment == nil {
			t.Fatal("workbench fixture assignment absent")
		}
		generation := state.Assignment.Generation
		sequence := 0
		report := func(e runs.Event) error {
			sequence++
			e.Sequence = fmt.Sprint(sequence)
			e.Generation = generation
			e.Mutation = mutation(root.Version)
			var err error
			root, err = accept(s.Runs.Report(ctx, device, w, root.ID, e))
			return err
		}
		if err := report(runs.Event{Type: "prepared"}); err != nil {
			t.Fatal(err)
		}
		fixturePath, err := filepath.Abs("../../../client/src-tauri/tests/fixtures/cli.py")
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, "python", fixturePath, "workbench")
		cmd.Dir = t.TempDir()
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "SYSTEMROOT=" + os.Getenv("SYSTEMROOT"), "PYTHONUTF8=1", "PYTHONIOENCODING=utf-8"}
		stdin, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		if err := report(runs.Event{Type: "started", Execution: &runs.ExecutionInfo{Environment: "owned-subprocess-fixture", Version: "owned-cli-fixture-v1", Pause: true}}); err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(stdin).Encode(map[string]any{"input": spec.Input, "request": profile}); err != nil {
			t.Fatal(err)
		}
		workers.Add(1)
		go func(human bool) {
			defer workers.Done()
			waited := false
			defer func() {
				_ = stdin.Close()
				if waited {
					return
				}
				if err := cmd.Wait(); err != nil && ctx.Err() == nil {
					t.Errorf("owned workbench CLI failed: %v %s", err, stderr.String())
				}
			}()
			lastRenew := time.Now()
			poll := func() (runs.RunnerState, error) {
				if err := contact(); err != nil {
					return runs.RunnerState{}, err
				}
				value, err := s.Runs.RunnerState(ctx, device, w, root.ID, generation)
				if err != nil {
					return value, err
				}
				root = value.Run
				if time.Since(lastRenew) >= 20*time.Second {
					response, err := s.Runs.Connection(ctx, device, w, root.ID, runs.RunnerConnection{Mutation: mutation(root.Version), Generation: generation})
					if err != nil {
						return value, err
					}
					if response.Data != nil {
						root = runs.Ref{ID: response.Data.ID, Version: response.Data.Version}
						value.Run = root
						lastRenew = time.Now()
					} else if response.Error != nil && response.Error.Code == "version_conflict" {
						value, err = s.Runs.RunnerState(ctx, device, w, root.ID, generation)
						root = value.Run
						return value, err
					} else {
						return value, fmt.Errorf("owned CLI heartbeat rejected")
					}
				}
				return value, nil
			}
			waitControl := func(ch <-chan struct{}) error {
				ticker := time.NewTicker(20 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-ch:
						return nil
					case <-ticker.C:
						if _, err := poll(); err != nil {
							return err
						}
					}
				}
			}
			scanner := bufio.NewScanner(stdout)
			var result json.RawMessage
			for scanner.Scan() {
				line := append([]byte{}, scanner.Bytes()...)
				var frame struct {
					Event struct {
						Type       string             `json:"type"`
						Pause      *runs.PauseRequest `json:"pause"`
						PauseID    string             `json:"pause_id"`
						ResponseID string             `json:"response_id"`
						Report     json.RawMessage    `json:"report"`
					} `json:"koujou_event"`
				}
				_ = json.Unmarshal(line, &frame)
				if err := report(runs.Event{Type: "output", Channel: "stdout", Source: "owned-workbench-cli", Value: rawUI(string(line))}); err != nil {
					if ctx.Err() == nil {
						t.Error(err)
					}
					return
				}
				switch frame.Event.Type {
				case "pause":
					if err := report(runs.Event{Type: "pause", Pause: frame.Event.Pause}); err != nil {
						t.Error(err)
						return
					}
					current, err := s.Runs.Read(ctx, p, w, root)
					if err != nil {
						t.Error(err)
						return
					}
					_ = json.Unmarshal(current.Value, &state)
					if human {
						_, err = accept(workbench.New(s.Access.Store, s.Access, s.Runs, s.Workflows).Put(ctx, p, w, "work-note:human-review", workbench.Write{Mutation: mutation("0"), Note: workbench.Note{Kind: "route", Subject: root, Pause: state.ActivePause, Recipient: otherUser, Reason: "背景を知らない確認担当へ固定ビルドの判断を依頼"}}))
						if err != nil {
							t.Error(err)
							return
						}
					}
					ready <- struct{}{}
					var response *runs.Pause
					for response == nil {
						select {
						case <-ctx.Done():
							return
						case <-time.After(200 * time.Millisecond):
						}
						v, err := poll()
						if err != nil {
							if ctx.Err() == nil {
								t.Error(err)
							}
							return
						}
						if v.Pause != nil && v.Pause.State == "received" {
							response = v.Pause
							root = v.Run
						}
					}
					if !human {
						if err := waitControl(deliver); err != nil {
							if ctx.Err() == nil {
								t.Error(err)
							}
							return
						}
					}
					if err := json.NewEncoder(stdin).Encode(map[string]any{"type": "answer", "pause_id": response.ID, "response_id": response.ResponseID, "value": response.Response}); err != nil {
						t.Error(err)
						return
					}
					if err := report(runs.Event{Type: "delivered", PauseID: response.ID, ResponseID: response.ResponseID}); err != nil {
						t.Error(err)
						return
					}
				case "resumed":
					if !human {
						if err := waitControl(resume); err != nil {
							if ctx.Err() == nil {
								t.Error(err)
							}
							return
						}
					}
					if err := report(runs.Event{Type: "resumed", PauseID: frame.Event.PauseID, ResponseID: frame.Event.ResponseID}); err != nil {
						t.Error(err)
						return
					}
				case "result":
					result = frame.Event.Report
				}
			}
			if ctx.Err() != nil {
				return
			}
			if err := scanner.Err(); err != nil {
				t.Error(err)
				return
			}
			if err := cmd.Wait(); err != nil {
				waited = true
				t.Errorf("owned workbench CLI failed: %v %s", err, stderr.String())
				return
			}
			waited = true
			zero := 0
			if err := report(runs.Event{Type: "finished", ExitCode: &zero, Report: result}); err != nil {
				t.Error(err)
			}
		}(scenario.human)
	}
	for range 2 {
		select {
		case <-ready:
		case <-time.After(10 * time.Second):
			cancel()
			workers.Wait()
			t.Fatal("owned CLI did not reach Pause")
		}
	}
	control := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == "GET" && r.URL.Path == "/__fixture/workbench-build" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(preview))
			return true
		}
		if r.Method == "POST" && (r.URL.Path == "/__fixture/workbench-deliver" || r.URL.Path == "/__fixture/workbench-resume") {
			ch := deliver
			if strings.HasSuffix(r.URL.Path, "resume") {
				ch = resume
			}
			select {
			case ch <- struct{}{}:
			default:
			}
			w.WriteHeader(204)
			return true
		}
		return false
	}
	return control, func() { cancel(); workers.Wait() }
}
func rawUI(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
