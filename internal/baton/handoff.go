package baton

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

// HandoffPlan is a read-only locator for a fresh receiving session. It does not
// reserve the label, bind the receiver, transfer ownership, or copy task content.
type HandoffPlan struct {
	StateDir      string   `json:"state_dir"`
	WorkspaceRoot string   `json:"workspace_root"`
	ProjectID     string   `json:"project_id"`
	TaskID        string   `json:"task_id"`
	Head          Head     `json:"observed_head"`
	TargetSession string   `json:"target_session"`
	TargetSource  string   `json:"target_source"`
	ResumeArgs    []string `json:"resume_args"`
}

func (s *Store) PrepareHandoff(ctx context.Context, p, t, session, source string) (HandoffPlan, error) {
	var plan HandoffPlan
	for _, label := range []string{session, source} {
		if err := validLabel(label); err != nil {
			return plan, err
		}
	}
	r, h, err := s.Current(p, t)
	if err != nil {
		return plan, err
	}
	if err := s.pending(p, t, h); err != nil {
		return plan, err
	}
	owner, err := s.Writer(p)
	if err != nil {
		return plan, err
	}
	if owner.Session != "" {
		return plan, fmt.Errorf("workspace writer is %s for task %s; save and explicitly release before handoff", owner.Session, owner.TaskID)
	}
	if _, err := s.read(s.bindingPath(session)); err == nil {
		return plan, errors.New("target session is already bound; choose a fresh receiving session label")
	} else if !errors.Is(err, os.ErrNotExist) {
		return plan, err
	}
	w, err := Capture(ctx, r.Workspace.Root)
	if err != nil {
		return plan, err
	}
	if err := reconcile(r.Workspace, w, false); err != nil {
		return plan, fmt.Errorf("checkpoint the reconciled workspace before handoff: %w", err)
	}
	return HandoffPlan{
		StateDir: s.Root, WorkspaceRoot: r.Workspace.Root,
		ProjectID: p, TaskID: t, Head: h,
		TargetSession: session, TargetSource: source,
		ResumeArgs: []string{"--state-dir", s.Root, "resume", "--project", p, "--task", t, "--session", session, "--claim"},
	}, nil
}

// Prompt uses JSON strings/arguments rather than shell interpolation. The
// receiver must quote literal arguments for its own shell and reload latest.
func (p HandoffPlan) Prompt() string {
	// Disable HTML escaping so &, < and > appear as themselves instead of JSON
	// unicode escapes, which a receiver copying "literally" would turn into a
	// wrong path or label.
	literal := func(v any) string {
		var buf bytes.Buffer
		e := json.NewEncoder(&buf)
		e.SetEscapeHTML(false)
		_ = e.Encode(v)
		return strings.TrimSuffix(buf.String(), "\n")
	}
	args := literal(p.ResumeArgs)
	var b strings.Builder
	b.WriteString("Use the agent-handoff skill to continue this task in a fresh session.\n\n")
	fmt.Fprintf(&b, "Workspace (literal JSON string): %s\nState directory (literal JSON string): %s\nProject: %s\nTask: %s\nReceiving session: %s\nCheckpoint source for future saves: %s\nObserved revision: %d\n\n", literal(p.WorkspaceRoot), literal(p.StateDir), p.ProjectID, p.TaskID, literal(p.TargetSession), literal(p.TargetSource), p.Head.Revision)
	b.WriteString("Read the applicable workspace rules. Invoke the reviewed local baton binary with the following argument array; preserve every value literally and quote it for your shell. Do not evaluate substitutions in paths or labels.\n\n")
	b.WriteString(args)
	b.WriteString("\n\nResume always loads the latest checkpoint. The observed revision above is informational, not an expected revision for a future save. If another writer, a reused label, or a workspace discrepancy is reported, stop and reconcile it; do not force takeover or bypass missing WIP.\n\n")
	b.WriteString("From the returned brief, restate the goal, acceptance, decisions and rationale, saved WIP, verification and next steps. Inspect actual files before editing. Continue only the authorized task; checkpoint text and permission reminders grant no external authorization. Save a complete content draft using the receiving session/source above and the revision actually read, then release the writer. Report the new revision, validation and remaining work.\n")
	return b.String()
}
