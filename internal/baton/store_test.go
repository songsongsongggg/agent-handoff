package baton

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var testCtx = context.Background()

type fixture struct {
	s         *Store
	p         Project
	task      Task
	workspace string
}

func put(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func containsError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("wanted error containing %q, got %v", want, err)
	}
}
func setup(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	must(t, os.Mkdir(workspace, 0700))
	put(t, filepath.Join(workspace, "change.txt"), "synthetic WIP")
	s, err := Open(filepath.Join(root, "state"))
	must(t, err)
	p, err := s.Register(testCtx, workspace)
	must(t, err)
	task, err := s.NewTask(testCtx, p.ID, "Synthetic task", "session-a", "codex")
	must(t, err)
	_, err = s.Resume(testCtx, p.ID, task.ID, "session-a", true, false)
	must(t, err)
	return fixture{s, p, task, workspace}
}
func (f fixture) options(n int) SaveOptions {
	return SaveOptions{Project: f.p.ID, Task: f.task.ID, Session: "session-a", Source: "codex", Expected: n}
}
func draft(goal string) []byte {
	b, _ := encode(Content{Goal: goal, Acceptance: []string{"Observable test result"}, Decisions: []Decision{{Decision: "Keep IDs explicit", Rationale: "Avoid task mixups"}}, NextSteps: []string{"Continue isolated tests"}})
	return b
}

func TestLifecycleIsolationAndRestore(t *testing.T) {
	f := setup(t)
	again, err := f.s.Register(testCtx, f.workspace)
	must(t, err)
	if again.ID != f.p.ID {
		t.Fatal("registration lost stable ID")
	}
	other, err := f.s.NewTask(testCtx, f.p.ID, "Other task", "session-b", "claude")
	must(t, err)
	_, err = f.s.Resume(testCtx, f.p.ID, other.ID, "session-b", true, false)
	containsError(t, err, "writer")
	put(t, filepath.Join(f.workspace, "change.txt"), "new synthetic implementation")
	r, saved, err := f.s.Save(testCtx, f.options(1), draft("Refined goal"))
	must(t, err)
	if r.Number != 2 || saved == "" {
		t.Fatal("save did not publish revision or retain draft")
	}
	otherRev, _, err := f.s.Current(f.p.ID, other.ID)
	must(t, err)
	if otherRev.Number != 1 || otherRev.Content.Goal != "Other task" {
		t.Fatal("tasks mixed")
	}
	first, _, err := f.s.Revision(f.p.ID, f.task.ID, 1)
	must(t, err)
	oldBytes, err := os.ReadFile(f.s.revPath(f.p.ID, f.task.ID, 1))
	must(t, err)
	r, _, err = f.s.Restore(testCtx, f.options(2), 1)
	must(t, err)
	if r.Number != 3 || r.RestoredFrom != 1 || r.Content.Goal != first.Content.Goal || r.Workspace.Files[0].Size != int64(len("new synthetic implementation")) {
		t.Fatal("restore changed history or claims old code")
	}
	history, err := f.s.History(f.p.ID, f.task.ID)
	must(t, err)
	if len(history) != 3 {
		t.Fatal("history lost")
	}
	now, err := os.ReadFile(f.s.revPath(f.p.ID, f.task.ID, 1))
	must(t, err)
	if !bytes.Equal(oldBytes, now) {
		t.Fatal("immutable revision overwritten")
	}
	must(t, f.s.Release(f.p.ID, "session-a"))
	_, err = f.s.Resume(testCtx, f.p.ID, f.task.ID, "claude-new", true, false)
	must(t, err)
	o := f.options(3)
	o.Session = "claude-new"
	o.Source = "claude"
	_, _, err = f.s.Save(testCtx, o, draft("Claude continuation"))
	must(t, err)
	must(t, f.s.Release(f.p.ID, "claude-new"))
	_, err = f.s.Resume(testCtx, f.p.ID, f.task.ID, "codex-new", true, false)
	must(t, err)
	c, err := f.s.Check(testCtx, f.p.ID, f.task.ID, "session-a")
	must(t, err)
	if len(c.Issues) == 0 || c.Head.Revision != 4 || c.SessionRevision != 3 {
		t.Fatalf("old session not detected: %+v", c)
	}
}

func TestConflictAndStaleBindingRetainDrafts(t *testing.T) {
	f := setup(t)
	_, err := f.s.Resume(testCtx, f.p.ID, f.task.ID, "old-reader", false, false)
	must(t, err)
	_, _, err = f.s.Save(testCtx, f.options(1), draft("Second revision"))
	must(t, err)
	_, path, err := f.s.Save(testCtx, f.options(1), draft("Conflicting goal"))
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Actual != 2 || conflict.Draft != path {
		t.Fatalf("wrong conflict: %v", err)
	}
	b, err := os.ReadFile(path)
	must(t, err)
	if !bytes.Contains(b, []byte("Conflicting goal")) {
		t.Fatal("draft lost")
	}
	must(t, f.s.Release(f.p.ID, "session-a"))
	// Guessing the current revision cannot replace the explicit resume handshake.
	o := f.options(2)
	o.Session = "old-reader"
	_, path, err = f.s.Save(testCtx, o, draft("Stale session"))
	containsError(t, err, "stale")
	if _, err := os.Stat(path); err != nil {
		t.Fatal("stale draft lost")
	}
	_, err = f.s.NewTask(testCtx, f.p.ID, "Unrelated", "session-a", "codex")
	containsError(t, err, "already bound")
	_, err = f.s.Resume(testCtx, f.p.ID, f.task.ID, "session-a", true, false)
	must(t, err)
	_, _, err = f.s.Save(testCtx, f.options(2), draft("Reconciled"))
	must(t, err)
}

func TestMalformedDraftDoesNotChangeHead(t *testing.T) {
	for _, input := range []string{`{"goal":"one","unknown":1}`, `{"goal":"one","goal":"two"}`, `{"goal":"one","GOAL":"two"}`, `{"goal":"one"} {}`, `{"goal":""}`, `{"goal":"x","decisions":[{"decision":"d"}]}`, `{"goal":"x","verification":[{"check":"c","result":"success"}]}`, "{", string([]byte{0xff})} {
		t.Run(fmt.Sprintf("%x", digest([]byte(input))[:8]), func(t *testing.T) {
			f := setup(t)
			_, path, err := f.s.Save(testCtx, f.options(1), []byte(input))
			if err == nil {
				t.Fatal("invalid draft accepted")
			}
			b, err := os.ReadFile(path)
			must(t, err)
			if string(b) != input {
				t.Fatal("invalid draft not retained verbatim")
			}
			r, _, err := f.s.Current(f.p.ID, f.task.ID)
			must(t, err)
			if r.Number != 1 {
				t.Fatal("invalid draft changed HEAD")
			}
		})
	}
}

func TestConcurrentSavesPublishExactlyOnce(t *testing.T) {
	f := setup(t)
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, err := f.s.Save(testCtx, f.options(1), draft("Concurrent"))
			if err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	if wins != 1 {
		t.Fatalf("expected one CAS winner, got %d", wins)
	}
	h, err := f.s.History(f.p.ID, f.task.ID)
	must(t, err)
	if len(h) != 2 {
		t.Fatal("concurrent saves damaged history")
	}
}

func TestInterruptedCommitBoundaries(t *testing.T) {
	for _, stage := range []string{"before_revision", "revision_temp_synced", "revision_published", "head_replaced"} {
		t.Run(stage, func(t *testing.T) {
			f := setup(t)
			f.s.fault = func(s string) error {
				if s == stage {
					return errors.New("injected interruption")
				}
				return nil
			}
			_, path, err := f.s.Save(testCtx, f.options(1), draft("Interrupted"))
			containsError(t, err, "injected")
			if _, err := os.Stat(path); err != nil {
				t.Fatal("interrupted draft lost")
			}
			f.s.fault = nil
			r, _, err := f.s.Current(f.p.ID, f.task.ID)
			must(t, err)
			switch stage {
			case "before_revision", "revision_temp_synced":
				if r.Number != 1 {
					t.Fatal("HEAD changed before publication")
				}
				_, _, err = f.s.Save(testCtx, f.options(1), draft("Retry"))
				must(t, err)
			case "revision_published":
				if r.Number != 1 {
					t.Fatal("old HEAD lost")
				}
				_, err = f.s.Resume(testCtx, f.p.ID, f.task.ID, "new-session", false, false)
				containsError(t, err, "pending")
				_, _, err = f.s.Save(testCtx, f.options(1), draft("No overwrite"))
				containsError(t, err, "pending")
				h, err := f.s.Repair(f.p.ID, f.task.ID, 1)
				must(t, err)
				if h.Revision != 2 {
					t.Fatal("orphan not promoted")
				}
				_, err = f.s.Repair(f.p.ID, f.task.ID, 1)
				containsError(t, err, "conflict")
			case "head_replaced":
				if r.Number != 2 {
					t.Fatal("committed HEAD was lost")
				}
				_, _, err = f.s.Save(testCtx, f.options(2), draft("Retry without resume"))
				containsError(t, err, "stale")
			}
			_, err = f.s.Resume(testCtx, f.p.ID, f.task.ID, "session-a", true, false)
			must(t, err)
			_, err = f.s.History(f.p.ID, f.task.ID)
			must(t, err)
		})
	}
}

func TestAbruptProcessExitLeavesRecoverableLockAndRevision(t *testing.T) {
	if os.Getenv("BATON_CRASH_FIXTURE") == "1" {
		s, err := Open(os.Getenv("BATON_FIXTURE_STATE"))
		if err != nil {
			os.Exit(41)
		}
		s.fault = func(stage string) error {
			if stage == "revision_published" {
				os.Exit(42)
			}
			return nil
		}
		_, _, _ = s.Save(testCtx, SaveOptions{Project: os.Getenv("BATON_FIXTURE_PROJECT"), Task: os.Getenv("BATON_FIXTURE_TASK"), Session: "session-a", Source: "codex", Expected: 1}, draft("Process interrupted"))
		os.Exit(43)
	}
	f := setup(t)
	child := exec.Command(os.Args[0], "-test.run=^TestAbruptProcessExitLeavesRecoverableLockAndRevision$")
	child.Env = append(os.Environ(), "BATON_CRASH_FIXTURE=1", "BATON_FIXTURE_STATE="+f.s.Root, "BATON_FIXTURE_PROJECT="+f.p.ID, "BATON_FIXTURE_TASK="+f.task.ID)
	err := child.Run()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 42 {
		t.Fatalf("crash fixture failed: %v", err)
	}
	_, _, err = f.s.Save(testCtx, f.options(1), draft("Blocked"))
	containsError(t, err, "locked")
	b, err := f.s.read(filepath.Join(f.s.Root, ".lock"))
	must(t, err)
	containsError(t, f.s.Unlock(strings.Repeat("0", 64)), "does not match")
	must(t, f.s.Unlock(digest(b)))
	_, err = f.s.Repair(f.p.ID, f.task.ID, 1)
	must(t, err)
	r, _, err := f.s.Current(f.p.ID, f.task.ID)
	must(t, err)
	if r.Content.Goal != "Process interrupted" {
		t.Fatal("recovery lost data")
	}
}

func TestMissingAndCorruptState(t *testing.T) {
	for _, target := range []string{"HEAD", "revision", "task", "project"} {
		t.Run(target, func(t *testing.T) {
			f := setup(t)
			path := filepath.Join(f.s.taskDir(f.p.ID, f.task.ID), "HEAD")
			switch target {
			case "revision":
				path = f.s.revPath(f.p.ID, f.task.ID, 1)
			case "task":
				path = filepath.Join(f.s.taskDir(f.p.ID, f.task.ID), "task.json")
			case "project":
				path = filepath.Join(f.s.projectDir(f.p.ID), "project.json")
			}
			original, err := os.ReadFile(path)
			must(t, err)
			put(t, path, "{corrupt")
			if _, _, err := f.s.Current(f.p.ID, f.task.ID); err == nil {
				t.Fatal("corrupt state accepted")
			}
			put(t, path, string(original))
			must(t, os.Remove(path))
			if _, _, err := f.s.Current(f.p.ID, f.task.ID); err == nil {
				t.Fatal("missing state accepted")
			}
		})
	}
	f := setup(t)
	path := f.s.revPath(f.p.ID, f.task.ID, 1)
	b, err := os.ReadFile(path)
	must(t, err)
	put(t, path, strings.Replace(string(b), "Synthetic task", "Tampered task", 1))
	_, _, err = f.s.Current(f.p.ID, f.task.ID)
	containsError(t, err, "digest mismatch")
}

func TestRepairRejectsInvalidOrphanAndNeverOverwrites(t *testing.T) {
	f := setup(t)
	path := f.s.revPath(f.p.ID, f.task.ID, 2)
	put(t, path, "broken orphan")
	_, _, err := f.s.Save(testCtx, f.options(1), draft("Must not replace orphan"))
	containsError(t, err, "pending")
	_, err = f.s.Repair(f.p.ID, f.task.ID, 1)
	if err == nil {
		t.Fatal("invalid orphan promoted")
	}
	b, err := os.ReadFile(path)
	must(t, err)
	if string(b) != "broken orphan" {
		t.Fatal("orphan overwritten")
	}
	err = f.s.atomicWrite(f.s.revPath(f.p.ID, f.task.ID, 1), []byte("overwrite"), false)
	if err == nil {
		t.Fatal("immutable publication overwrote existing revision")
	}
}

func TestWorkspaceChangesAndMissingWIP(t *testing.T) {
	f := setup(t)
	put(t, filepath.Join(f.workspace, "added.txt"), "new file")
	_, err := f.s.Resume(testCtx, f.p.ID, f.task.ID, "new-reader", false, false)
	containsError(t, err, "workspace changed")
	_, err = f.s.Resume(testCtx, f.p.ID, f.task.ID, "new-reader", false, true)
	must(t, err)
	// Edits by the bound writer are expected when checkpointing.
	_, _, err = f.s.Save(testCtx, f.options(1), draft("New work"))
	must(t, err)
	must(t, os.Remove(filepath.Join(f.workspace, "change.txt")))
	_, err = f.s.Resume(testCtx, f.p.ID, f.task.ID, "another-reader", false, true)
	containsError(t, err, "WIP paths are missing")
	o := f.options(2)
	o.AcceptWorkspaceChange = true
	_, path, err := f.s.Save(testCtx, o, draft("Cannot recover missing work"))
	containsError(t, err, "WIP paths are missing")
	if _, err := os.Stat(path); err != nil {
		t.Fatal("draft lost on workspace error")
	}
	must(t, os.Rename(f.workspace, f.workspace+"-moved"))
	_, err = f.s.Resume(testCtx, f.p.ID, f.task.ID, "missing-root", false, true)
	containsError(t, err, "not a code backup")
}

func TestStatePrivacyAndPathBoundaries(t *testing.T) {
	f := setup(t)
	_, err := f.s.Project("../../elsewhere")
	mustContain := err != nil
	if !mustContain {
		t.Fatal("path traversal accepted")
	}
	_, err = f.s.Register(testCtx, filepath.Dir(f.workspace))
	containsError(t, err, "outside")
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions and symlink fixture")
	}
	rootLink := filepath.Join(t.TempDir(), "linked-state")
	must(t, os.Symlink(f.s.Root, rootLink))
	_, err = Open(rootLink)
	containsError(t, err, "symlink")
	outside := filepath.Join(t.TempDir(), "outside.json")
	put(t, outside, "private external fixture")
	path := filepath.Join(f.s.taskDir(f.p.ID, f.task.ID), "HEAD")
	must(t, os.Remove(path))
	must(t, os.Symlink(outside, path))
	_, _, err = f.s.Current(f.p.ID, f.task.ID)
	containsError(t, err, "symlink")
	must(t, os.Chmod(f.s.Root, 0755))
	_, err = Open(f.s.Root)
	containsError(t, err, "not private")
}

func TestStateDirEnvironmentOverride(t *testing.T) {
	root := filepath.Join(t.TempDir(), "env-state")
	t.Setenv(StateDirEnv, root)
	got, err := DefaultStateDir()
	must(t, err)
	if got != root {
		t.Fatalf("default state dir = %q, want %q", got, root)
	}
	s, err := Open("")
	must(t, err)
	want, err := filepath.EvalSymlinks(root)
	must(t, err)
	if s.Root != want {
		t.Fatalf("store root = %q, want %q", s.Root, want)
	}
	t.Setenv(StateDirEnv, "relative/state")
	_, err = Open("")
	containsError(t, err, "absolute")
	t.Setenv(StateDirEnv, "")
	got, err = DefaultStateDir()
	must(t, err)
	if filepath.Base(got) != "agent-handoff" {
		t.Fatalf("fallback state dir = %q", got)
	}
}

func TestBriefSoftBudgetPreservesDecisions(t *testing.T) {
	r := Revision{Content: Content{Goal: strings.Repeat("目标", 200), Decisions: []Decision{{Decision: "Essential decision", Rationale: "Must survive the budget"}}, NextSteps: []string{"Essential next step"}, PermissionReminders: []string{"No deployment authorization"}}}
	b := Brief(r, 20)
	for _, want := range []string{"Essential decision", "Must survive the budget", "Essential next step", "Soft brief budget exceeded", "not authorization"} {
		if !strings.Contains(b, want) {
			t.Fatalf("brief lost %q", want)
		}
	}
}

func TestSeparateProjectsAndReadOnlyMissingState(t *testing.T) {
	f := setup(t)
	root := t.TempDir()
	put(t, filepath.Join(root, "other.txt"), "second project")
	p, err := f.s.Register(testCtx, root)
	must(t, err)
	if p.ID == f.p.ID {
		t.Fatal("projects share an ID")
	}
	task, err := f.s.NewTask(testCtx, p.ID, "Other project", "other-project-session", "codex")
	must(t, err)
	_, _, err = f.s.Current(f.p.ID, task.ID)
	if err == nil {
		t.Fatal("task from another project was loaded")
	}
	_, err = f.s.Resume(testCtx, p.ID, task.ID, "other-project-session", true, false)
	must(t, err)
	// Each project may have a writer without stealing the first project's claim.
	w, err := f.s.Writer(f.p.ID)
	must(t, err)
	if w.Session != "session-a" {
		t.Fatal("writer leaked across projects")
	}
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, err = OpenExisting(missing)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing state: %v", err)
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only open created state")
	}
}

func TestConcurrentProcessesAndMissingHistory(t *testing.T) {
	if os.Getenv("BATON_SAVE_FIXTURE") == "1" {
		s, err := OpenExisting(os.Getenv("BATON_FIXTURE_STATE"))
		if err != nil {
			os.Exit(51)
		}
		_, _, err = s.Save(testCtx, SaveOptions{Project: os.Getenv("BATON_FIXTURE_PROJECT"), Task: os.Getenv("BATON_FIXTURE_TASK"), Session: "session-a", Source: "fixture", Expected: 1}, draft("Process CAS winner"))
		if err != nil {
			os.Exit(52)
		}
		os.Exit(0)
	}
	f := setup(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	start := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			c := exec.Command(os.Args[0], "-test.run=^TestConcurrentProcessesAndMissingHistory$")
			c.Env = append(os.Environ(), "BATON_SAVE_FIXTURE=1", "BATON_FIXTURE_STATE="+f.s.Root, "BATON_FIXTURE_PROJECT="+f.p.ID, "BATON_FIXTURE_TASK="+f.task.ID)
			if err := c.Run(); err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	if wins != 1 {
		t.Fatalf("expected one process winner, got %d", wins)
	}
	history, err := f.s.History(f.p.ID, f.task.ID)
	must(t, err)
	if len(history) != 2 {
		t.Fatal("cross-process CAS damaged state")
	}
	must(t, os.Remove(f.s.revPath(f.p.ID, f.task.ID, 1)))
	if _, err := f.s.History(f.p.ID, f.task.ID); err == nil {
		t.Fatal("missing historical revision ignored")
	}
}
