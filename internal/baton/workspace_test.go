package baton

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func gitFixture(t *testing.T, root string, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git unavailable")
	}
	c := exec.Command("git", append([]string{"-C", root}, args...)...)
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	b, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git fixture %v: %v: %s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}

func TestGitWorkspaceUnbornCommitRenameAndBranch(t *testing.T) {
	root := t.TempDir()
	gitFixture(t, root, "init", "-b", "main")
	put(t, filepath.Join(root, "file.txt"), "one")
	w, err := Capture(testCtx, root)
	must(t, err)
	if w.Kind != "git" || w.Branch != "main" || w.Commit != "" || len(w.WIPPaths) != 1 {
		t.Fatalf("bad unborn metadata: %+v", w)
	}
	gitFixture(t, root, "add", "file.txt")
	gitFixture(t, root, "-c", "core.hooksPath="+t.TempDir(), "-c", "commit.gpgsign=false", "commit", "-m", "synthetic fixture")
	w, err = Capture(testCtx, root)
	must(t, err)
	if w.Commit == "" || len(w.WIPPaths) != 0 {
		t.Fatal("committed identity missing")
	}
	gitFixture(t, root, "mv", "file.txt", "renamed.txt")
	renamed, err := Capture(testCtx, root)
	must(t, err)
	if len(renamed.WIPPaths) != 2 || !Compare(w, renamed).Changed {
		t.Fatal("rename not detected")
	}
	gitFixture(t, root, "checkout", "-b", "feature")
	branch, err := Capture(testCtx, root)
	must(t, err)
	if !Compare(renamed, branch).Changed || branch.Branch != "feature" {
		t.Fatal("branch change missed")
	}
	sub := filepath.Join(root, "sub")
	must(t, os.Mkdir(sub, 0700))
	_, err = Capture(testCtx, sub)
	containsError(t, err, "real Git workspace root")
}

func TestGitIdentityChangeRequiresReconciliation(t *testing.T) {
	f := setup(t)
	gitFixture(t, f.workspace, "init", "-b", "main")
	_, _, err := f.s.Save(testCtx, f.options(1), draft("Now Git"))
	containsError(t, err, "identity changed")
	o := f.options(1)
	o.AcceptWorkspaceChange = true
	_, _, err = f.s.Save(testCtx, o, draft("Explicit Git transition"))
	must(t, err)
	gitFixture(t, f.workspace, "checkout", "-b", "feature")
	_, err = f.s.Resume(testCtx, f.p.ID, f.task.ID, "reader", false, false)
	containsError(t, err, "workspace changed")
}

func TestNonGitAndSymlinksNeverFollowExternalContents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privilege fixture")
	}
	root := t.TempDir()
	outside := t.TempDir()
	put(t, filepath.Join(outside, "not-in-snapshot.txt"), "external fixture")
	must(t, os.Symlink(outside, filepath.Join(root, "link")))
	w, err := Capture(testCtx, root)
	must(t, err)
	if w.Kind != "directory" || w.Commit != "" || len(w.Files) != 1 || w.Files[0].Path != "link" {
		t.Fatal("followed symlink or fabricated Git metadata")
	}
	gitFixture(t, root, "init", "-b", "main")
	must(t, os.Remove(filepath.Join(root, "link")))
	must(t, os.Mkdir(filepath.Join(root, "tracked"), 0700))
	put(t, filepath.Join(root, "tracked", "file.txt"), "local")
	gitFixture(t, root, "add", "tracked/file.txt")
	must(t, os.Remove(filepath.Join(root, "tracked", "file.txt")))
	must(t, os.Remove(filepath.Join(root, "tracked")))
	must(t, os.Symlink(outside, filepath.Join(root, "tracked")))
	_, err = Capture(testCtx, root)
	containsError(t, err, "symlink directory")
}

func TestBrokenGitAndCancelledCaptureFailClosed(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, ".git"), "gitdir: missing-directory\n")
	_, err := Capture(testCtx, root)
	if err == nil {
		t.Fatal("broken Git treated as non-Git")
	}
	must(t, os.Remove(filepath.Join(root, ".git")))
	put(t, filepath.Join(root, "file.txt"), "fixture")
	ctx, cancel := context.WithCancel(testCtx)
	cancel()
	_, err = Capture(ctx, root)
	if err == nil {
		t.Fatal("cancelled capture continued")
	}
}

func TestReplacedWorkspaceIdentityDetected(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Unix device/inode identity")
	}
	root := filepath.Join(t.TempDir(), "workspace")
	must(t, os.Mkdir(root, 0700))
	before, err := Capture(testCtx, root)
	must(t, err)
	if before.RootID == "" {
		t.Fatal("Unix root identity missing")
	}
	must(t, os.Rename(root, root+"-old"))
	must(t, os.Mkdir(root, 0700))
	after, err := Capture(testCtx, root)
	must(t, err)
	if !Compare(before, after).Changed || before.RootID == after.RootID {
		t.Fatal("replacement workspace was accepted as the original")
	}
}
