package baton

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (s *Store) projectDir(id string) string { return filepath.Join(s.Root, "projects", id) }
func (s *Store) taskDir(p, t string) string  { return filepath.Join(s.projectDir(p), "tasks", t) }
func (s *Store) revPath(p, t string, n int) string {
	return filepath.Join(s.taskDir(p, t), "revisions", fmt.Sprintf("%06d.json", n))
}
func (s *Store) bindingPath(session string) string {
	return filepath.Join(s.Root, "sessions", digest([]byte(session))+".json")
}

func (s *Store) Project(id string) (Project, error) {
	var p Project
	if err := validID(id); err != nil {
		return p, err
	}
	if err := s.readJSON(filepath.Join(s.projectDir(id), "project.json"), &p); err != nil {
		return p, err
	}
	if p.Schema != SchemaVersion || p.ID != id || !filepath.IsAbs(p.Root) || p.CreatedAt.IsZero() {
		return p, errors.New("invalid project identity/schema")
	}
	return p, nil
}

func (s *Store) Projects() ([]Project, error) {
	result := []Project{}
	dir := filepath.Join(s.Root, "projects")
	if err := s.guard(dir); errors.Is(err, os.ErrNotExist) {
		return result, nil
	} else if err != nil {
		return nil, err
	}
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range es {
		if strings.HasPrefix(e.Name(), ".new-") {
			continue
		}
		p, err := s.Project(e.Name())
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, nil
}

func (s *Store) Register(ctx context.Context, path string) (Project, error) {
	var p Project
	err := s.mutate(func() error {
		w, err := Capture(ctx, path)
		if err != nil {
			return err
		}
		if within(w.Root, s.Root) {
			return errors.New("state directory must be outside the registered workspace")
		}
		ps, err := s.Projects()
		if err != nil {
			return err
		}
		for _, old := range ps {
			if old.Root == w.Root {
				p = old
				return nil
			}
		}
		id, err := newID()
		if err != nil {
			return err
		}
		p = Project{Schema: SchemaVersion, ID: id, Root: w.Root, CreatedAt: time.Now().UTC()}
		parent := filepath.Join(s.Root, "projects")
		if err := s.mkdir(parent); err != nil {
			return err
		}
		stage, err := os.MkdirTemp(parent, ".new-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		if err := s.writeJSON(filepath.Join(stage, "project.json"), p, false); err != nil {
			return err
		}
		if err := os.Rename(stage, s.projectDir(id)); err != nil {
			return err
		}
		return syncDir(parent)
	})
	return p, err
}

func (s *Store) Task(p, t string) (Task, error) {
	var task Task
	if _, err := s.Project(p); err != nil {
		return task, err
	}
	if err := validID(t); err != nil {
		return task, err
	}
	if err := s.readJSON(filepath.Join(s.taskDir(p, t), "task.json"), &task); err != nil {
		return task, err
	}
	if task.Schema != SchemaVersion || task.ID != t || task.ProjectID != p || task.CreatedAt.IsZero() || validLabel(task.Title) != nil {
		return task, errors.New("invalid task identity/schema")
	}
	return task, nil
}

func (s *Store) Tasks(p string) ([]Task, error) {
	if _, err := s.Project(p); err != nil {
		return nil, err
	}
	result := []Task{}
	dir := filepath.Join(s.projectDir(p), "tasks")
	if err := s.guard(dir); errors.Is(err, os.ErrNotExist) {
		return result, nil
	} else if err != nil {
		return nil, err
	}
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range es {
		if strings.HasPrefix(e.Name(), ".new-") {
			continue
		}
		t, err := s.Task(p, e.Name())
		if err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, nil
}

func (s *Store) NewTask(ctx context.Context, p, title, session, source string) (Task, error) {
	var task Task
	for _, v := range []string{title, session, source} {
		if err := validLabel(v); err != nil {
			return task, err
		}
	}
	err := s.mutate(func() error {
		project, err := s.Project(p)
		if err != nil {
			return err
		}
		if err := s.checkSessionTarget(session, p, ""); err != nil {
			return err
		}
		w, err := Capture(ctx, project.Root)
		if err != nil {
			return err
		}
		id, err := newID()
		if err != nil {
			return err
		}
		task = Task{Schema: SchemaVersion, ID: id, ProjectID: p, Title: title, CreatedAt: time.Now().UTC()}
		parent := filepath.Join(s.projectDir(p), "tasks")
		if err := s.mkdir(parent); err != nil {
			return err
		}
		stage, err := os.MkdirTemp(parent, ".new-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		if err := s.writeJSON(filepath.Join(stage, "task.json"), task, false); err != nil {
			return err
		}
		for _, d := range []string{"revisions", "drafts"} {
			if err := s.mkdir(filepath.Join(stage, d)); err != nil {
				return err
			}
		}
		r := Revision{Schema: SchemaVersion, ProjectID: p, TaskID: id, Number: 1, Source: source, Session: session, CreatedAt: time.Now().UTC(), Workspace: w, Content: Content{Goal: title, Unverified: []string{"Initial checkpoint; acceptance and task details need refinement"}}}
		b, err := encode(r)
		if err != nil {
			return err
		}
		if err := s.atomicWrite(filepath.Join(stage, "revisions", "000001.json"), b, false); err != nil {
			return err
		}
		if err := s.writeJSON(filepath.Join(stage, "HEAD"), Head{Revision: 1, Digest: digest(b)}, false); err != nil {
			return err
		}
		if err := os.Rename(stage, s.taskDir(p, id)); err != nil {
			return err
		}
		if err := syncDir(parent); err != nil {
			return err
		}
		return s.bind(Binding{Schema: SchemaVersion, ProjectID: p, TaskID: id, Session: session, Revision: 1, Workspace: w})
	})
	return task, err
}

func (s *Store) validateRevision(r Revision, p, t string, n int) error {
	project, err := s.Project(p)
	if err != nil {
		return err
	}
	if r.Schema != SchemaVersion || r.ProjectID != p || r.TaskID != t || r.Number != n || n < 1 || r.CreatedAt.IsZero() || r.Workspace.Root != project.Root {
		return errors.New("revision identity/schema mismatch")
	}
	if err := validLabel(r.Session); err != nil {
		return err
	}
	if err := validLabel(r.Source); err != nil {
		return err
	}
	if (n == 1 && r.ParentDigest != "") || (n > 1 && len(r.ParentDigest) != 64) {
		return errors.New("invalid revision parent")
	}
	if r.RestoredFrom < 0 || r.RestoredFrom >= n {
		return errors.New("invalid restore reference")
	}
	if r.Workspace.Kind != "git" && r.Workspace.Kind != "directory" {
		return errors.New("invalid workspace kind")
	}
	w := r.Workspace
	if w.Kind == "directory" && (w.GitDir != "" || w.GitID != "" || w.Commit != "" || w.Branch != "" || w.IndexDigest != "" || w.Status != "") {
		return errors.New("non-Git workspace contains Git identity")
	}
	if w.Kind == "git" && (!filepath.IsAbs(w.GitDir) || w.Branch == "" || len(w.IndexDigest) != 64) {
		return errors.New("incomplete Git workspace identity")
	}
	paths := map[string]bool{}
	last := ""
	for _, f := range w.Files {
		if !fs.ValidPath(f.Path) || strings.Contains(f.Path, "\\") || paths[f.Path] || f.Path <= last {
			return errors.New("invalid workspace file metadata")
		}
		paths[f.Path] = true
		last = f.Path
	}
	last = ""
	for _, p := range w.WIPPaths {
		if !paths[p] || p <= last {
			return errors.New("invalid WIP paths")
		}
		last = p
	}
	return r.Content.Validate()
}

func (s *Store) Revision(p, t string, n int) (Revision, Head, error) {
	var r Revision
	var h Head
	if n < 1 {
		return r, h, errors.New("revision must be positive")
	}
	if _, err := s.Task(p, t); err != nil {
		return r, h, err
	}
	b, err := s.read(s.revPath(p, t, n))
	if err != nil {
		return r, h, err
	}
	if err := DecodeStrict(b, &r); err != nil {
		return r, h, err
	}
	if err := s.validateRevision(r, p, t, n); err != nil {
		return r, h, err
	}
	h = Head{Revision: n, Digest: digest(b)}
	return r, h, nil
}

func (s *Store) Current(p, t string) (Revision, Head, error) {
	var r Revision
	var h Head
	if _, err := s.Task(p, t); err != nil {
		return r, h, err
	}
	if err := s.readJSON(filepath.Join(s.taskDir(p, t), "HEAD"), &h); err != nil {
		return r, h, err
	}
	r, actual, err := s.Revision(p, t, h.Revision)
	if err != nil {
		return r, h, err
	}
	if h.Digest != actual.Digest {
		return r, h, errors.New("HEAD digest mismatch; state is corrupt")
	}
	return r, h, nil
}

func (s *Store) pending(p, t string, h Head) error {
	es, err := os.ReadDir(filepath.Join(s.taskDir(p, t), "revisions"))
	if err != nil {
		return err
	}
	for _, e := range es {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(e.Name(), "%d.json", &n); err != nil || e.Name() != fmt.Sprintf("%06d.json", n) {
			return errors.New("unexpected revision entry")
		}
		if n > h.Revision {
			return fmt.Errorf("pending revision %d detected; inspect history then repair --expect-revision %d", n, h.Revision)
		}
	}
	return nil
}

func (s *Store) checkSessionTarget(session, p, t string) error {
	var b Binding
	err := s.readJSON(s.bindingPath(session), &b)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if b.Schema != SchemaVersion || b.Session != session || b.ProjectID != p || b.TaskID != t {
		return errors.New("session is already bound to another task; use a new explicit session label")
	}
	return nil
}

func (s *Store) bind(b Binding) error {
	if err := s.mkdir(filepath.Join(s.Root, "sessions")); err != nil {
		return err
	}
	return s.writeJSON(s.bindingPath(b.Session), b, true)
}

func (s *Store) Writer(p string) (Writer, error) {
	var w Writer
	if _, err := s.Project(p); err != nil {
		return w, err
	}
	err := s.readJSON(filepath.Join(s.projectDir(p), "writer.json"), &w)
	if errors.Is(err, os.ErrNotExist) {
		return w, nil
	}
	if err != nil {
		return w, err
	}
	if err := validLabel(w.Session); err != nil {
		return w, err
	}
	if err := validID(w.TaskID); err != nil {
		return w, err
	}
	return w, nil
}

func (s *Store) Resume(ctx context.Context, p, t, session string, claim, accept bool) (Revision, error) {
	var r Revision
	if err := validLabel(session); err != nil {
		return r, err
	}
	err := s.mutate(func() error {
		var h Head
		var err error
		r, h, err = s.Current(p, t)
		if err != nil {
			return err
		}
		if err := s.pending(p, t, h); err != nil {
			return err
		}
		if err := s.checkSessionTarget(session, p, t); err != nil {
			return err
		}
		w, err := Capture(ctx, r.Workspace.Root)
		if err != nil {
			return err
		}
		if err := reconcile(r.Workspace, w, accept); err != nil {
			return err
		}
		if claim {
			owner, err := s.Writer(p)
			if err != nil {
				return err
			}
			if owner.Session != "" && (owner.Session != session || owner.TaskID != t) {
				return fmt.Errorf("workspace writer is %s for task %s; explicit release required", owner.Session, owner.TaskID)
			}
			if err := s.writeJSON(filepath.Join(s.projectDir(p), "writer.json"), Writer{Session: session, TaskID: t}, true); err != nil {
				return err
			}
		}
		return s.bind(Binding{Schema: SchemaVersion, ProjectID: p, TaskID: t, Session: session, Revision: h.Revision, Workspace: w})
	})
	return r, err
}

func (s *Store) Release(p, session string) error {
	if err := validLabel(session); err != nil {
		return err
	}
	return s.mutate(func() error {
		w, err := s.Writer(p)
		if err != nil {
			return err
		}
		if w.Session == "" {
			return nil
		}
		if w.Session != session {
			return errors.New("writer session does not match; ownership retained")
		}
		if err := os.Remove(filepath.Join(s.projectDir(p), "writer.json")); err != nil {
			return err
		}
		return syncDir(s.projectDir(p))
	})
}

type SaveOptions struct {
	Project               string
	Task                  string
	Session               string
	Source                string
	Expected              int
	AcceptWorkspaceChange bool
}

func (s *Store) Save(ctx context.Context, o SaveOptions, raw []byte) (Revision, string, error) {
	var r Revision
	var draft string
	if len(raw) > maxJSON {
		return r, draft, errors.New("draft exceeds 8 MiB resource limit; original file remains unchanged")
	}
	for _, v := range []string{o.Session, o.Source} {
		if err := validLabel(v); err != nil {
			return r, draft, err
		}
	}
	err := s.mutate(func() error {
		if _, err := s.Task(o.Project, o.Task); err != nil {
			return err
		}
		id, err := newID()
		if err != nil {
			return err
		}
		draft = filepath.Join(s.taskDir(o.Project, o.Task), "drafts", id+".json")
		if err := s.mkdir(filepath.Dir(draft)); err != nil {
			return err
		}
		if err := s.atomicWrite(draft, raw, false); err != nil {
			return err
		}
		var c Content
		if err := DecodeStrict(raw, &c); err != nil {
			return err
		}
		if err := c.Validate(); err != nil {
			return err
		}
		r, err = s.saveLocked(ctx, o, c, 0)
		return err
	})
	var conflict *ConflictError
	if errors.As(err, &conflict) {
		conflict.Draft = draft
	}
	return r, draft, err
}

func (s *Store) saveLocked(ctx context.Context, o SaveOptions, c Content, restored int) (Revision, error) {
	var r Revision
	prev, h, err := s.Current(o.Project, o.Task)
	if err != nil {
		return r, err
	}
	if o.Expected != h.Revision {
		return r, &ConflictError{Expected: o.Expected, Actual: h.Revision}
	}
	if err := s.pending(o.Project, o.Task, h); err != nil {
		return r, err
	}
	var b Binding
	if err := s.readJSON(s.bindingPath(o.Session), &b); err != nil {
		return r, fmt.Errorf("resume with this session first: %w", err)
	}
	if b.Schema != SchemaVersion || b.Session != o.Session || b.ProjectID != o.Project || b.TaskID != o.Task || b.Revision != h.Revision {
		return r, errors.New("session binding is stale or belongs to another task; resume latest first")
	}
	owner, err := s.Writer(o.Project)
	if err != nil {
		return r, err
	}
	if owner.Session != o.Session || owner.TaskID != o.Task {
		return r, errors.New("resume --claim is required before saving")
	}
	w, err := Capture(ctx, prev.Workspace.Root)
	if err != nil {
		return r, err
	}
	// Local edits since resume are normal; branch/HEAD changes and lost WIP
	// still require reconciliation. Saved missing WIP cannot be overridden.
	if err := reconcileMissing(prev.Workspace, w); err != nil {
		return r, err
	}
	if err := reconcileMissing(b.Workspace, w); err != nil {
		return r, err
	}
	identity := Compare(b.Workspace, w)
	if (b.Workspace.Kind != w.Kind || b.Workspace.Root != w.Root || b.Workspace.RootID != w.RootID || b.Workspace.GitDir != w.GitDir || b.Workspace.GitID != w.GitID || b.Workspace.Branch != w.Branch || b.Workspace.Commit != w.Commit) && !o.AcceptWorkspaceChange {
		return r, fmt.Errorf("workspace identity changed: %v; inspect before accepting", identity.Reasons)
	}
	r = Revision{Schema: SchemaVersion, ProjectID: o.Project, TaskID: o.Task, Number: h.Revision + 1, ParentDigest: h.Digest, Source: o.Source, Session: o.Session, CreatedAt: time.Now().UTC(), RestoredFrom: restored, Workspace: w, Content: c}
	if err := s.validateRevision(r, o.Project, o.Task, r.Number); err != nil {
		return r, err
	}
	data, err := encode(r)
	if err != nil {
		return r, err
	}
	if err := s.boundary("before_revision"); err != nil {
		return r, err
	}
	if err := s.atomicWrite(s.revPath(o.Project, o.Task, r.Number), data, false); err != nil {
		return r, err
	}
	if err := s.boundary("revision_published"); err != nil {
		return r, err
	}
	if err := s.writeJSON(filepath.Join(s.taskDir(o.Project, o.Task), "HEAD"), Head{Revision: r.Number, Digest: digest(data)}, true); err != nil {
		return r, err
	}
	if err := s.boundary("head_replaced"); err != nil {
		return r, err
	}
	if err := s.bind(Binding{Schema: SchemaVersion, ProjectID: o.Project, TaskID: o.Task, Session: o.Session, Revision: r.Number, Workspace: w}); err != nil {
		return r, err
	}
	return r, nil
}

func reconcileMissing(a, b Workspace) error {
	d := Compare(a, b)
	if len(d.MissingWIP) > 0 {
		return fmt.Errorf("saved WIP paths are missing: %v; checkpoint cannot recover code", d.MissingWIP)
	}
	return nil
}

func (s *Store) History(p, t string) ([]Revision, error) {
	_, h, err := s.Current(p, t)
	if err != nil {
		return nil, err
	}
	result := []Revision{}
	parent := ""
	for n := 1; n <= h.Revision; n++ {
		r, rh, err := s.Revision(p, t, n)
		if err != nil {
			return nil, err
		}
		if r.ParentDigest != parent {
			return nil, errors.New("broken revision digest chain")
		}
		parent = rh.Digest
		result = append(result, r)
	}
	return result, nil
}

func (s *Store) Restore(ctx context.Context, o SaveOptions, n int) (Revision, string, error) {
	// Read history before mutation, then CAS the latest HEAD under lock.
	history, err := s.History(o.Project, o.Task)
	if err != nil {
		return Revision{}, "", err
	}
	if n < 1 || n > len(history) {
		return Revision{}, "", errors.New("restore revision is outside committed history")
	}
	c := history[n-1].Content
	raw, err := encode(c)
	if err != nil {
		return Revision{}, "", err
	}
	var r Revision
	var draft string
	for _, v := range []string{o.Source, o.Session} {
		if err := validLabel(v); err != nil {
			return r, draft, err
		}
	}
	err = s.mutate(func() error {
		id, err := newID()
		if err != nil {
			return err
		}
		draft = filepath.Join(s.taskDir(o.Project, o.Task), "drafts", id+".json")
		if err := s.atomicWrite(draft, raw, false); err != nil {
			return err
		}
		r, err = s.saveLocked(ctx, o, c, n)
		return err
	})
	var conflict *ConflictError
	if errors.As(err, &conflict) {
		conflict.Draft = draft
	}
	return r, draft, err
}

func (s *Store) Repair(p, t string, expected int) (Head, error) {
	var out Head
	err := s.mutate(func() error {
		_, h, err := s.Current(p, t)
		if err != nil {
			return err
		}
		if h.Revision != expected {
			return &ConflictError{Expected: expected, Actual: h.Revision}
		}
		if _, err := s.History(p, t); err != nil {
			return err
		}
		r, rh, err := s.Revision(p, t, h.Revision+1)
		if err != nil {
			return err
		}
		if r.ParentDigest != h.Digest {
			return errors.New("pending revision has wrong parent")
		}
		// Only one adjacent orphan is a recoverable interrupted transaction.
		es, err := os.ReadDir(filepath.Join(s.taskDir(p, t), "revisions"))
		if err != nil {
			return err
		}
		sort.Slice(es, func(i, j int) bool { return es[i].Name() < es[j].Name() })
		for _, e := range es {
			if strings.HasPrefix(e.Name(), ".tmp-") {
				continue
			}
			var n int
			if _, err := fmt.Sscanf(e.Name(), "%d.json", &n); err != nil || e.Name() != fmt.Sprintf("%06d.json", n) || n > rh.Revision {
				return errors.New("unexpected revision file; manual inspection required")
			}
		}
		if err := s.writeJSON(filepath.Join(s.taskDir(p, t), "HEAD"), rh, true); err != nil {
			return err
		}
		out = rh
		return nil
	})
	return out, err
}
