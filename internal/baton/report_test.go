package baton

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestCheckReportsWriterConflictAcrossTasks(t *testing.T) {
	f := setup(t)
	other, err := f.s.NewTask(testCtx, f.p.ID, "Other task", "other-session", "claude-code")
	must(t, err)
	check, err := f.s.Check(testCtx, f.p.ID, other.ID, "other-session")
	must(t, err)
	if !strings.Contains(strings.Join(check.Issues, "\n"), "workspace writer is session-a") {
		t.Fatal("check reported a ready session while another task owned the workspace")
	}
	check, err = f.s.Check(testCtx, f.p.ID, f.task.ID, "session-a")
	must(t, err)
	if len(check.Issues) != 0 {
		t.Fatalf("own writer should be ready: %v", check.Issues)
	}
	must(t, f.s.Release(f.p.ID, "session-a"))
	check, err = f.s.Check(testCtx, f.p.ID, other.ID, "other-session")
	must(t, err)
	if len(check.Issues) != 0 {
		t.Fatalf("released writer still blocks check: %v", check.Issues)
	}
}

func TestBriefShowsSavedWIPWithoutFollowingLinks(t *testing.T) {
	r := Revision{Workspace: Workspace{
		Files: []FileStamp{
			{Path: "a\nfile.go", Exists: true, Size: 12, Modified: 42},
			{Path: "deleted.go"},
			{Path: "link", Exists: true, Mode: uint32(os.ModeSymlink | 0700)},
			{Path: "nested", Exists: true, Mode: uint32(os.ModeDir | 0700)},
		},
		WIPPaths: []string{"a\nfile.go", "deleted.go", "link", "nested"},
	}}
	brief := Brief(r, 1)
	for _, want := range []string{`"a\nfile.go"`, "size 12 bytes; modified_ns 42", `"deleted.go": absent at checkpoint`, `"link": symlink (target not followed)`, `"nested": directory (contents not covered)`, "Soft brief budget exceeded"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief omitted %q", want)
		}
	}
}

func TestWorkspaceDiffListsMetadataChanges(t *testing.T) {
	saved := Workspace{Files: []FileStamp{
		{Path: "changed.go", Exists: true, Size: 10},
		{Path: "removed.go", Exists: true},
		{Path: "unchanged.go", Exists: true},
	}, WIPPaths: []string{"removed.go"}}
	current := Workspace{Files: []FileStamp{
		{Path: "added.go", Exists: true},
		{Path: "changed.go", Exists: true, Size: 11},
		{Path: "removed.go", Exists: false},
		{Path: "unchanged.go", Exists: true},
	}}
	diff := Compare(saved, current)
	want := []FileChange{{"added.go", "added"}, {"changed.go", "metadata_changed"}, {"removed.go", "removed"}}
	if !diff.Changed || !reflect.DeepEqual(diff.FileChanges, want) || !reflect.DeepEqual(diff.MissingWIP, []string{"removed.go"}) {
		t.Fatalf("incorrect file details: %+v", diff)
	}
	if len(Compare(current, current).FileChanges) != 0 {
		t.Fatal("unchanged files reported as changed")
	}
}
