package baton

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func stateBytes(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	must(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = string(raw)
		}
		return nil
	}))
	return files
}

func TestHandoffReadOnlyAndFreshReceiver(t *testing.T) {
	f := setup(t)
	_, err := f.s.PrepareHandoff(testCtx, f.p.ID, f.task.ID, "receiver", "claude-code")
	containsError(t, err, "explicitly release")
	must(t, f.s.Release(f.p.ID, "session-a"))
	before := stateBytes(t, f.s.Root)
	plan, err := f.s.PrepareHandoff(testCtx, f.p.ID, f.task.ID, "receiver", "claude-code")
	must(t, err)
	if !reflect.DeepEqual(before, stateBytes(t, f.s.Root)) {
		t.Fatal("generating a handoff mutated state")
	}
	if plan.Head.Revision != 1 || plan.StateDir != f.s.Root || plan.WorkspaceRoot != f.p.Root {
		t.Fatalf("incorrect handoff locator: %+v", plan)
	}
	if strings.Contains(plan.Prompt(), "Synthetic task") {
		t.Fatal("handoff copied task content instead of requiring latest resume")
	}
	// A previously generated locator is safe to use after the sender saves again:
	// the receiving session must learn the new revision instead of trusting it.
	_, err = f.s.Resume(testCtx, f.p.ID, f.task.ID, "session-a", true, false)
	must(t, err)
	_, _, err = f.s.Save(testCtx, f.options(1), draft("Latest task goal"))
	must(t, err)
	must(t, f.s.Release(f.p.ID, "session-a"))
	r, err := f.s.Resume(testCtx, plan.ProjectID, plan.TaskID, plan.TargetSession, true, false)
	must(t, err)
	if r.Number != 2 || r.Content.Goal != "Latest task goal" {
		t.Fatal("receiver failed to load latest checkpoint")
	}
	must(t, f.s.Release(f.p.ID, plan.TargetSession))
	_, err = f.s.PrepareHandoff(testCtx, f.p.ID, f.task.ID, plan.TargetSession, plan.TargetSource)
	containsError(t, err, "fresh receiving session")
}

func TestHandoffBlocksUncheckpointedAndMissingWork(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed", true: "missing"}[missing], func(t *testing.T) {
			f := setup(t)
			must(t, f.s.Release(f.p.ID, "session-a"))
			if missing {
				must(t, os.Remove(filepath.Join(f.workspace, "change.txt")))
			} else {
				put(t, filepath.Join(f.workspace, "new.txt"), "not checkpointed")
			}
			before := stateBytes(t, f.s.Root)
			_, err := f.s.PrepareHandoff(testCtx, f.p.ID, f.task.ID, "receiver", "claude-code")
			if missing {
				containsError(t, err, "WIP paths are missing")
			} else {
				containsError(t, err, "workspace changed")
			}
			if !reflect.DeepEqual(before, stateBytes(t, f.s.Root)) {
				t.Fatal("failed handoff mutated state")
			}
		})
	}
}

func TestHandoffRejectsPendingTransaction(t *testing.T) {
	f := setup(t)
	f.s.fault = func(stage string) error {
		if stage == "revision_published" {
			return errors.New("injected interruption")
		}
		return nil
	}
	_, _, err := f.s.Save(testCtx, f.options(1), draft("Pending"))
	containsError(t, err, "injected interruption")
	f.s.fault = nil
	must(t, f.s.Release(f.p.ID, "session-a"))
	_, err = f.s.PrepareHandoff(testCtx, f.p.ID, f.task.ID, "receiver", "claude-code")
	containsError(t, err, "pending revision")
}

func TestHandoffPreservesLiteralArguments(t *testing.T) {
	f := setup(t)
	must(t, f.s.Release(f.p.ID, "session-a"))
	session := "new 'session' $(never-evaluate) R&D <x>"
	plan, err := f.s.PrepareHandoff(testCtx, f.p.ID, f.task.ID, session, "test-client")
	must(t, err)
	if strings.Contains(plan.Prompt(), `\u00`) || !strings.Contains(plan.Prompt(), "R&D <x>") {
		t.Fatal("prompt HTML-escaped a literal value")
	}
	var args []string
	for _, line := range strings.Split(plan.Prompt(), "\n") {
		if strings.HasPrefix(line, "[") {
			must(t, json.Unmarshal([]byte(line), &args))
		}
	}
	if !reflect.DeepEqual(args, plan.ResumeArgs) || args[len(args)-2] != session {
		t.Fatal("prompt did not preserve literal argument array")
	}
	for _, want := range []string{"not an expected revision", "grant no external authorization", "do not force takeover", "release the writer"} {
		if !strings.Contains(plan.Prompt(), want) {
			t.Fatalf("missing receiving constraint %q", want)
		}
	}
	_, err = f.s.PrepareHandoff(testCtx, f.p.ID, f.task.ID, "\n", "test-client")
	containsError(t, err, "label")
}
