package developmentworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const (
	testProject = "project:workspace"
	testThread  = "mcp:development-task"
)

func testClient(t *testing.T, respond func(string, values) values) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var rpc struct {
			Method string `json:"method"`
			Params struct {
				Name string `json:"name"`
				Meta values `json:"_meta"`
				Args values `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(req.Body).Decode(&rpc); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if req.Method != http.MethodPost || req.URL.Path != "/mcp" ||
			req.Header.Get("Authorization") != "Bearer fixture" ||
			req.Header.Get("MCP-Protocol-Version") != mcpProtocol || req.Header.Get("MCP-Name") != rpc.Params.Name ||
			req.Header.Get("MCP-Method") != rpc.Method || rpc.Method != "tools/call" ||
			rpc.Params.Meta["io.modelcontextprotocol/protocolVersion"] != mcpProtocol {
			t.Error("MCP headers or request envelope are invalid")
		}
		result := respond(rpc.Params.Name, rpc.Params.Args)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(values{"jsonrpc": "2.0", "id": 1, "result": result}); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	client, err := New(Config{URL: server.URL + "/mcp", Token: "fixture", ProjectID: testProject, DefaultBranch: "main", Provider: "codex", Model: "codex-model"})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func structured(data values) values {
	return values{"structuredContent": data}
}

func thread() values {
	return values{
		"threadId": testThread, "projectId": testProject, "title": "Change title", "status": "completed",
		"runtimeMode": fullAccess, "providerInstanceId": "codex", "creationSource": "mcp",
		"branch": "worktree/topic", "worktreePath": "internal-path",
	}
}

func TestWorkspaceRoundTrip(t *testing.T) {
	var operations []string
	client := testClient(t, func(name string, args values) values {
		operations = append(operations, name)
		switch name {
		case "t3_project_read":
			return structured(values{"id": testProject, "title": "Fixture"})
		case "orchestrator_capabilities":
			return structured(values{"runtimeMode": fullAccess, "providers": []values{{
				"providerInstanceId": "codex", "driverKind": "codex", "canRunChildTask": true,
				"models": []values{{"id": "codex-model"}},
			}}})
		case "t3_thread_launch":
			want := values{
				"projectId": testProject, "title": "Change title", "message": "Change title",
				"runtimeMode": fullAccess, "interactionMode": "default", "clientRequestId": "create-key",
				"workspaceStrategy": map[string]any{"type": "worktree", "baseRef": "release/next"},
				"modelSelection":    map[string]any{"instanceId": "codex", "model": "codex-model"},
			}
			if !reflect.DeepEqual(args, want) {
				t.Errorf("launch args = %#v, want %#v", args, want)
			}
			return structured(values{"threadId": testThread, "projectId": testProject, "status": "starting"})
		case "t3_thread_list":
			public := thread()
			delete(public, "worktreePath")
			unrelated := thread()
			unrelated["projectId"] = "other-project"
			otherSource := thread()
			otherSource["creationSource"] = "web"
			return structured(values{"threads": []values{public, unrelated, otherSource}})
		case "t3_thread_read":
			if args["beforePosition"] != float64(latestPosition) {
				t.Error("thread read must request the newest items")
			}
			if args["view"] == "messages" {
				return structured(values{"thread": thread(), "items": []values{
					{"itemId": "assistant", "position": 4, "type": "assistant_message", "text": "Done"},
					{"itemId": "user", "position": 1, "type": "user_message", "text": "Start"},
				}})
			}
			return structured(values{"thread": thread(), "items": []values{
				{"itemId": "read-only", "position": 6, "type": "checkpoint", "text": `[]`},
				{"itemId": "end", "position": 5, "type": "checkpoint", "text": `[{"path":"README.md","additions":2,"deletions":1}]`},
				{"itemId": "log", "position": 2, "type": "command_execution", "text": "$ git status"},
				{"itemId": "internal", "position": 3, "type": "reasoning", "text": "not in the activity feed"},
			}})
		case "t3_thread_send":
			if args["clientRequestId"] != "message-key" || args["threadId"] != testThread || args["mode"] != "auto" {
				t.Errorf("send args = %#v", args)
			}
			return structured(values{"threadId": testThread, "status": "running"})
		case "t3_thread_interrupt":
			if args["clientRequestId"] != "stop-key" || args["threadId"] != testThread || args["stop"] != true {
				t.Errorf("stop args = %#v", args)
			}
			return structured(values{"threadId": testThread, "status": "interrupt_requested"})
		default:
			t.Errorf("unexpected operation %s", name)
			return nil
		}
	})
	ctx := context.Background()
	if got, err := client.Status(ctx); err != nil || got != (Status{Enabled: true, Project: "Fixture", DefaultBranch: "main", Model: "codex-model"}) {
		t.Fatalf("status = %#v, %v", got, err)
	}
	if got, err := client.Create(ctx, CreateInput{Message: "Change title", Branch: "release/next", RequestID: "create-key"}); err != nil || got.ID != testThread || got.Status != "starting" {
		t.Fatalf("create = %#v, %v", got, err)
	}
	if got, err := client.List(ctx); err != nil || len(got) != 1 || got[0].ID != testThread {
		t.Fatalf("list = %#v, %v", got, err)
	}
	got, err := client.Get(ctx, testThread)
	if err != nil || got.Branch != "worktree/topic" || len(got.Messages) != 2 || got.Messages[0].Role != "user" || got.Messages[1].Text != "Done" || len(got.Activity) != 1 || len(got.Changes) != 1 || got.Changes[0].Additions != 2 {
		t.Fatalf("detail = %#v, %v", got, err)
	}
	if err := client.Send(ctx, testThread, SendInput{Message: "Next", RequestID: "message-key"}); err != nil {
		t.Fatal(err)
	}
	if err := client.StopWithRequestID(ctx, testThread, "stop-key"); err != nil {
		t.Fatal(err)
	}
	want := []string{"t3_project_read", "orchestrator_capabilities", "orchestrator_capabilities", "t3_thread_launch", "t3_thread_list", "t3_thread_read", "t3_thread_read", "t3_thread_read", "t3_thread_send", "t3_thread_read", "t3_thread_interrupt"}
	if !reflect.DeepEqual(operations, want) {
		t.Errorf("operations = %v, want %v", operations, want)
	}
}

func TestWorkspaceRejectsUnscopedTasks(t *testing.T) {
	for name, wrong := range map[string]values{
		"different project": {"projectId": "other-project"},
		"root checkout":     {"worktreePath": ""},
		"native thread":     {"creationSource": "web"},
		"subagent":          {"parentThreadId": "parent"},
	} {
		t.Run(name, func(t *testing.T) {
			client := testClient(t, func(tool string, _ values) values {
				if tool != "t3_thread_read" {
					t.Errorf("dispatched tool %s against an unscoped task", tool)
				}
				item := thread()
				for k, v := range wrong {
					item[k] = v
				}
				return structured(values{"thread": item, "items": []values{}})
			})
			if err := client.Send(context.Background(), testThread, SendInput{Message: "test", RequestID: "request"}); !errors.Is(err, ErrNotFound) {
				t.Errorf("send error = %v", err)
			}
			if err := client.Stop(context.Background(), testThread); !errors.Is(err, ErrNotFound) {
				t.Errorf("stop error = %v", err)
			}
		})
	}
}

func TestWorkspaceReadsTaskWhileT3PreparesItsWorktree(t *testing.T) {
	launchID := "mcp:launch:" + strings.Repeat("a", 64)
	status := "preparing"
	client := testClient(t, func(tool string, args values) values {
		if args["threadId"] != launchID {
			t.Errorf("unexpected thread ID %v", args["threadId"])
		}
		switch tool {
		case "t3_thread_read":
			item := thread()
			item["threadId"] = launchID
			item["status"] = status
			item["worktreePath"] = nil
			return structured(values{"thread": item, "items": []values{}})
		case "t3_thread_interrupt":
			return structured(values{"threadId": launchID, "status": "interrupt_requested"})
		default:
			t.Errorf("unexpected T3 operation %s", tool)
			return nil
		}
	})
	ctx := context.Background()
	detail, err := client.Get(ctx, launchID)
	if err != nil || detail.ID != launchID || detail.Status != "preparing" {
		t.Fatalf("preparing detail = %#v, %v", detail, err)
	}
	if err := client.Send(ctx, launchID, SendInput{Message: "Continue", RequestID: "request"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("send to preparing worktree error = %v", err)
	}
	if err := client.StopWithRequestID(ctx, launchID, "stop-key"); err != nil {
		t.Errorf("stop preparing worktree error = %v", err)
	}
	status = "interrupted"
	detail, err = client.Get(ctx, launchID)
	if err != nil || detail.ID != launchID || detail.Status != "interrupted" {
		t.Fatalf("interrupted detail = %#v, %v", detail, err)
	}
	if err := client.Send(ctx, launchID, SendInput{Message: "Continue", RequestID: "request"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("send to interrupted task without worktree error = %v", err)
	}
}

func TestWorkspaceT3ErrorRedaction(t *testing.T) {
	client := testClient(t, func(_ string, _ values) values {
		return values{"isError": true, "content": []values{{"type": "text", "text": `{"_tag":"OrchestratorMcpFailure","code":"thread_not_found","message":"private path"}`}}}
	})
	if _, err := client.Get(context.Background(), testThread); !errors.Is(err, ErrNotFound) || strings.Contains(err.Error(), "private path") {
		t.Fatalf("get error = %v", err)
	}
}

func TestWorkspaceRejectsUnreadyProvider(t *testing.T) {
	for _, ceiling := range []string{"full-access", "approval-required"} {
		t.Run(ceiling, func(t *testing.T) {
			client := testClient(t, func(name string, _ values) values {
				if name != "orchestrator_capabilities" {
					t.Errorf("unexpected operation %s", name)
				}
				return structured(values{"runtimeMode": ceiling, "providers": []values{{
					"providerInstanceId": "codex", "driverKind": "codex", "canRunChildTask": false,
					"models": []values{{"id": "codex-model"}},
				}}})
			})
			if _, err := client.Create(context.Background(), CreateInput{Message: "test", RequestID: "request"}); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("launch error = %v", err)
			}
		})
	}
}

func TestWorkspaceConfiguration(t *testing.T) {
	client, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := client.Status(context.Background()); err != nil || got.Enabled {
		t.Fatalf("disabled status = %#v, %v", got, err)
	}
	if _, err := client.List(context.Background()); !errors.Is(err, ErrDisabled) {
		t.Fatalf("disabled list error = %v", err)
	}
	for _, endpoint := range []string{"http://example.com/mcp", "https://user:password@example.com/mcp", "https://example.com/api", "https://example.com/mcp?token=another"} {
		if _, err := New(Config{URL: endpoint, Token: "fixture", ProjectID: testProject, DefaultBranch: "main", Provider: "codex", Model: "codex-model"}); !errors.Is(err, ErrInvalid) {
			t.Errorf("expected invalid config, got %v", err)
		}
	}
}
