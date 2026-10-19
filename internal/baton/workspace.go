package baton

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

func realDir(path string) (string, error) {
	a, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	a, err = filepath.EvalSymlinks(a)
	if err != nil {
		return "", fmt.Errorf("workspace unavailable (checkpoint is not a code backup): %w", err)
	}
	i, err := os.Stat(a)
	if err != nil {
		return "", err
	}
	if !i.IsDir() {
		return "", errors.New("workspace is not a directory")
	}
	return filepath.Clean(a), nil
}

func within(root, path string) bool {
	r, err := filepath.Rel(root, path)
	return err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

// Unix exposes stable device/inode identity through FileInfo.Sys. On other
// platforms the empty value makes this check explicitly unavailable.
func fileID(i os.FileInfo) string {
	v := reflect.ValueOf(i.Sys())
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	d, n := v.FieldByName("Dev"), v.FieldByName("Ino")
	number := func(v reflect.Value) string {
		if v.IsValid() && v.CanUint() {
			return fmt.Sprint(v.Uint())
		}
		if v.IsValid() && v.CanInt() {
			return fmt.Sprint(v.Int())
		}
		return ""
	}
	dev, ino := number(d), number(n)
	if dev != "" && ino != "" {
		return dev + ":" + ino
	}
	return ""
}

func exitCode(err error, code int) bool {
	var e *exec.ExitError
	return errors.As(err, &e) && e.ExitCode() == code
}

func safeStamp(root, p string) (FileStamp, error) {
	stamp := FileStamp{Path: p}
	full := filepath.Join(root, filepath.FromSlash(p))
	for dir := filepath.Dir(full); dir != root; dir = filepath.Dir(dir) {
		i, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			return stamp, nil
		}
		if err != nil {
			return stamp, err
		}
		if i.Mode()&os.ModeSymlink != 0 {
			return stamp, errors.New("workspace path traverses a symlink directory; refusing outside inspection")
		}
	}
	i, err := os.Lstat(full)
	if errors.Is(err, os.ErrNotExist) {
		return stamp, nil
	}
	if err != nil {
		return stamp, err
	}
	stamp.Mode = uint32(i.Mode())
	stamp.Size = i.Size()
	stamp.Modified = i.ModTime().UnixNano()
	stamp.Exists = true
	return stamp, nil
}

func git(ctx context.Context, root string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	base := []string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-C", root}
	c := exec.CommandContext(ctx, "git", append(base, args...)...)
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_") {
			c.Env = append(c.Env, e)
		}
	}
	c.Env = append(c.Env, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	b, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("read-only git %s failed: %w", args[0], err)
	}
	if !utf8.Valid(b) {
		return nil, errors.New("workspace paths must be UTF-8")
	}
	return b, nil
}

func Capture(ctx context.Context, path string) (Workspace, error) {
	root, err := realDir(path)
	if err != nil {
		return Workspace{}, err
	}
	w := Workspace{Root: root, Kind: "directory", Files: []FileStamp{}, WIPPaths: []string{}}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return w, err
	}
	w.RootID = fileID(rootInfo)
	// Only inspect Git when a .git entry exists in this root or an ancestor.
	// A broken repository must not silently turn into a non-Git snapshot.
	hasGit := false
	for p := root; ; p = filepath.Dir(p) {
		if _, e := os.Lstat(filepath.Join(p, ".git")); e == nil {
			hasGit = true
			break
		} else if !errors.Is(e, os.ErrNotExist) {
			return w, e
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	paths := map[string]bool{}
	if hasGit {
		top, e := git(ctx, root, "rev-parse", "--show-toplevel")
		if e != nil {
			return w, e
		}
		topRoot, e := realDir(strings.TrimSuffix(string(top), "\n"))
		if e != nil {
			return w, e
		}
		if topRoot != root {
			return w, errors.New("register the real Git workspace root, not a subdirectory")
		}
		w.Kind = "git"
		gd, e := git(ctx, root, "rev-parse", "--absolute-git-dir")
		if e != nil {
			return w, e
		}
		w.GitDir = strings.TrimSuffix(string(gd), "\n")
		w.GitDir, e = filepath.EvalSymlinks(w.GitDir)
		if e != nil {
			return w, e
		}
		gitInfo, e := os.Stat(w.GitDir)
		if e != nil {
			return w, e
		}
		w.GitID = fileID(gitInfo)
		branch, e := git(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD")
		if e == nil {
			w.Branch = strings.TrimSpace(string(branch))
		} else if exitCode(e, 1) {
			w.Branch = "(detached)"
		} else {
			return w, e
		}
		commit, e := git(ctx, root, "rev-parse", "--verify", "HEAD")
		if e == nil {
			w.Commit = strings.TrimSpace(string(commit))
		} else {
			if w.Branch == "(detached)" {
				return w, errors.New("cannot identify Git HEAD")
			}
			// An unborn branch has no refs/heads entry. Other failures are errors.
			_, refErr := git(ctx, root, "show-ref", "--verify", "--quiet", "refs/heads/"+w.Branch)
			if !exitCode(refErr, 1) {
				return w, errors.New("cannot read existing Git HEAD or verify an unborn branch")
			}
		}
		index, e := git(ctx, root, "ls-files", "--stage", "-z")
		if e != nil {
			return w, e
		}
		w.IndexDigest = digest(index)
		status, e := git(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
		if e != nil {
			return w, e
		}
		w.Status = string(status)
		parts := strings.Split(string(status), "\x00")
		for i := 0; i < len(parts) && parts[i] != ""; i++ {
			if len(parts[i]) < 4 {
				return w, errors.New("invalid Git porcelain status")
			}
			w.WIPPaths = append(w.WIPPaths, parts[i][3:])
			if strings.ContainsAny(parts[i][:2], "RC") {
				i++
				if i >= len(parts) || parts[i] == "" {
					return w, errors.New("invalid Git rename status")
				}
				w.WIPPaths = append(w.WIPPaths, parts[i])
			}
		}
		files, e := git(ctx, root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
		if e != nil {
			return w, e
		}
		for _, p := range strings.Split(string(files), "\x00") {
			if p != "" {
				paths[p] = true
			}
		}
		for _, p := range w.WIPPaths {
			paths[p] = true
		}
	} else {
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if path == root {
				return nil
			}
			if d.Name() == ".git" && d.IsDir() {
				return filepath.SkipDir
			}
			if d.IsDir() {
				return nil
			}
			r, e := filepath.Rel(root, path)
			if e != nil {
				return e
			}
			r = filepath.ToSlash(r)
			paths[r] = true
			if len(paths) > 100000 {
				return errors.New("workspace exceeds 100000 paths")
			}
			return nil
		})
		if err != nil {
			return w, err
		}
		for p := range paths {
			w.WIPPaths = append(w.WIPPaths, p)
		}
	}
	if len(paths) > 100000 {
		return w, errors.New("workspace exceeds 100000 paths")
	}
	for p := range paths {
		if err := ctx.Err(); err != nil {
			return w, err
		}
		if !utf8.ValidString(p) || !fs.ValidPath(p) || strings.Contains(p, "\\") {
			return w, errors.New("unsupported workspace path")
		}
		stamp, e := safeStamp(root, p)
		if e != nil {
			return w, e
		}
		w.Files = append(w.Files, stamp)
	}
	sort.Slice(w.Files, func(i, j int) bool { return w.Files[i].Path < w.Files[j].Path })
	sort.Strings(w.WIPPaths)
	unique := w.WIPPaths[:0]
	for _, p := range w.WIPPaths {
		if len(unique) == 0 || unique[len(unique)-1] != p {
			unique = append(unique, p)
		}
	}
	w.WIPPaths = unique
	return w, nil
}

type WorkspaceDiff struct {
	Changed     bool         `json:"changed"`
	MissingWIP  []string     `json:"missing_wip"`
	FileChanges []FileChange `json:"file_changes"`
	Reasons     []string     `json:"reasons"`
}

type FileChange struct {
	Path   string `json:"path"`
	Change string `json:"change"` // added, removed, metadata_changed; never content diffs
}

func Compare(saved, current Workspace) WorkspaceDiff {
	d := WorkspaceDiff{MissingWIP: []string{}, FileChanges: []FileChange{}, Reasons: []string{}}
	if saved.Root != current.Root || saved.RootID != current.RootID {
		d.Reasons = append(d.Reasons, "workspace root changed")
	}
	if saved.Kind != current.Kind || saved.GitDir != current.GitDir || saved.GitID != current.GitID {
		d.Reasons = append(d.Reasons, "workspace/Git identity changed")
	}
	if saved.Branch != current.Branch || saved.Commit != current.Commit {
		d.Reasons = append(d.Reasons, "Git branch or HEAD changed")
	}
	if saved.IndexDigest != current.IndexDigest || saved.Status != current.Status {
		d.Reasons = append(d.Reasons, "Git index or status changed")
	}
	if !reflect.DeepEqual(saved.Files, current.Files) {
		d.Reasons = append(d.Reasons, "workspace file metadata changed")
	}
	before, after := map[string]FileStamp{}, map[string]FileStamp{}
	paths := map[string]bool{}
	for _, f := range saved.Files {
		before[f.Path] = f
		paths[f.Path] = true
	}
	for _, f := range current.Files {
		after[f.Path] = f
		paths[f.Path] = true
	}
	for path := range paths {
		a, b := before[path], after[path]
		change := ""
		switch {
		case !a.Exists && b.Exists:
			change = "added"
		case a.Exists && !b.Exists:
			change = "removed"
		case a.Exists && b.Exists && a != b:
			change = "metadata_changed"
		}
		if change != "" {
			d.FileChanges = append(d.FileChanges, FileChange{Path: path, Change: change})
		}
	}
	sort.Slice(d.FileChanges, func(i, j int) bool { return d.FileChanges[i].Path < d.FileChanges[j].Path })
	for _, p := range saved.WIPPaths {
		if before[p].Exists && !after[p].Exists {
			d.MissingWIP = append(d.MissingWIP, p)
		}
	}
	d.Changed = len(d.Reasons) != 0
	return d
}

func reconcile(saved, current Workspace, accept bool) error {
	d := Compare(saved, current)
	if len(d.MissingWIP) != 0 {
		return fmt.Errorf("saved WIP paths are missing: %v; checkpoint cannot recover code", d.MissingWIP)
	}
	if d.Changed && !accept {
		return fmt.Errorf("workspace changed: %s; inspect and reconcile before --accept-workspace-change", strings.Join(d.Reasons, "; "))
	}
	return nil
}
