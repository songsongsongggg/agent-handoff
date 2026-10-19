package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/songsongsongggg/agent-handoff/internal/baton"
)

func TestCLISyntheticRoundTrip(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "state")
	call := func(args ...string) (string, error) {
		t.Helper()
		var out, diag bytes.Buffer
		err := run(context.Background(), append([]string{"--state-dir", state}, args...), &out, &diag)
		return out.String(), err
	}
	text, err := call("project", "register", "--path", workspace)
	if err != nil {
		t.Fatal(err)
	}
	var p baton.Project
	if err := json.Unmarshal([]byte(text), &p); err != nil {
		t.Fatal(err)
	}
	text, err = call("task", "new", "--project", p.ID, "--title", "Synthetic round trip", "--session", "codex-old", "--source", "codex")
	if err != nil {
		t.Fatal(err)
	}
	var task baton.Task
	if err := json.Unmarshal([]byte(text), &task); err != nil {
		t.Fatal(err)
	}
	for i, session := range []string{"codex-old", "claude-new", "codex-new"} {
		text, err = call("resume", "--project", p.ID, "--task", task.ID, "--session", session, "--claim")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, "Synthetic round trip") {
			t.Fatal("goal lost in brief")
		}
		if i == 2 {
			break
		}
		draft := filepath.Join(root, "draft.json")
		raw := `{"goal":"Synthetic round trip","decisions":[{"decision":"Explicit IDs","rationale":"Avoid mixed tasks"}],"next_steps":["Resume in next simulated client"],"verification":[{"check":"CLI simulation","result":"passed","evidence":"Isolated only"}],"unverified":["Real client acceptance"]}`
		if err := os.WriteFile(draft, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		n := "1"
		if i == 1 {
			n = "2"
		}
		_, err = call("checkpoint", "--project", p.ID, "--task", task.ID, "--session", session, "--source", "simulation", "--draft", draft, "--expect-revision", n)
		if err != nil {
			t.Fatal(err)
		}
		_, err = call("writer", "release", "--project", p.ID, "--session", session)
		if err != nil {
			t.Fatal(err)
		}
	}
	text, err = call("check", "--project", p.ID, "--task", task.ID, "--session", "codex-old")
	if err == nil || !strings.Contains(text, "stale") {
		t.Fatal("old CLI session not blocked")
	}
	text, err = call("history", "--project", p.ID, "--task", task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, `"revision": 3`) {
		t.Fatal("round trip history lost")
	}
}

func TestCLIRejectsAmbiguousOrInvalidInput(t *testing.T) {
	for _, args := range [][]string{{"unknown"}, {"task"}, {"checkpoint", "--expect-revision", "0"}, {"resume", "--format", "raw"}, {"resume", "--budget", "0"}, {"handoff"}, {"handoff", "--project", "p", "--task", "t", "--to-session", "new", "--to-source", "client", "--format", "raw"}, {"project", "register"}, {"project", "list", "unexpected"}} {
		var out, diag bytes.Buffer
		if err := run(context.Background(), args, &out, &diag); err == nil {
			t.Fatalf("invalid args accepted: %v", args)
		}
	}
	var out, diag bytes.Buffer
	if err := run(context.Background(), []string{"help"}, &out, &diag); err != nil || !strings.Contains(out.String(), "cooperative") {
		t.Fatal("help failed")
	}
}

func TestCLIHandoffGeneratedArgumentsResumeLatest(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace 'quoted' $literal")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "wip.go"), []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "state with spaces")
	t.Setenv(baton.StateDirEnv, state)
	call := func(args ...string) (string, error) {
		t.Helper()
		var out, diag bytes.Buffer
		err := run(context.Background(), args, &out, &diag)
		return out.String(), err
	}
	text, err := call("project", "register", "--path", workspace)
	if err != nil {
		t.Fatal(err)
	}
	var p baton.Project
	if err := json.Unmarshal([]byte(text), &p); err != nil {
		t.Fatal(err)
	}
	text, err = call("task", "new", "--project", p.ID, "--title", "Native argument roundtrip", "--session", "sender", "--source", "codex")
	if err != nil {
		t.Fatal(err)
	}
	var task baton.Task
	if err := json.Unmarshal([]byte(text), &task); err != nil {
		t.Fatal(err)
	}
	args := []string{"handoff", "--project", p.ID, "--task", task.ID, "--to-session", "fresh receiver", "--to-source", "claude-code"}
	text, err = call(args...)
	if err != nil || !strings.Contains(text, "Use the agent-handoff skill") {
		t.Fatalf("markdown handoff failed: %v, %s", err, text)
	}
	text, err = call(append(args, "--format", "json")...)
	if err != nil {
		t.Fatal(err)
	}
	var plan baton.HandoffPlan
	if err := json.Unmarshal([]byte(text), &plan); err != nil {
		t.Fatal(err)
	}
	// Explicit state in the generated arguments must win over a changed environment.
	t.Setenv(baton.StateDirEnv, "relative-invalid-state")
	text, err = call(plan.ResumeArgs...)
	if err != nil || !strings.Contains(text, `"wip.go": file`) || !strings.Contains(text, "Native argument roundtrip") {
		t.Fatalf("generated arguments failed to resume the exact task: %v, %s", err, text)
	}
	_, err = call(append([]string{"--state-dir", plan.StateDir}, args...)...)
	if err == nil || !strings.Contains(err.Error(), "writer") {
		t.Fatalf("handoff did not block active writer: %v", err)
	}
}
