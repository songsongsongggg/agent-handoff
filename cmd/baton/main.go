package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/songsongsongggg/agent-handoff/internal/baton"
)

const usage = `baton: local task checkpoints (development candidate)

Usage: baton [--state-dir PATH] COMMAND [flags]
  project register --path PATH
  project list
  task new --project ID --title TITLE --session LABEL --source CLIENT
  task list --project ID
  resume --project ID --task ID --session LABEL [--claim] [--format markdown|json] [--budget 12000] [--accept-workspace-change]
  handoff --project ID --task ID --to-session NEW_LABEL --to-source CLIENT [--format markdown|json]
  check --project ID --task ID --session LABEL
  checkpoint --project ID --task ID --session LABEL --source CLIENT --draft FILE --expect-revision N [--accept-workspace-change]
  history --project ID --task ID [--revision N]
  restore --project ID --task ID --session LABEL --source CLIENT --revision N --expect-revision N [--accept-workspace-change]
  writer release --project ID --session LABEL
  repair --project ID --task ID --expect-revision N
  unlock --lock-token SHA256

State defaults to $BATON_STATE_DIR (absolute), else the OS user configuration directory / agent-handoff.
IDs and session labels are explicit; no recent-session guessing.
Writer registration is cooperative; checkpoints never back up or restore code.
Unlock only after confirming all baton writers stopped. No command uses the network.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "baton:", err)
		os.Exit(1)
	}
}

func printJSON(w io.Writer, v any) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(v)
}

func run(ctx context.Context, args []string, out, errOut io.Writer) error {
	global := flag.NewFlagSet("baton", flag.ContinueOnError)
	global.SetOutput(errOut)
	state := global.String("state-dir", "", "private state directory")
	if err := global.Parse(args); err != nil {
		return err
	}
	args = global.Args()
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		_, err := io.WriteString(out, usage)
		return err
	}
	cmd := args[0]
	args = args[1:]
	if cmd == "project" || cmd == "task" || cmd == "writer" {
		if len(args) == 0 {
			return errors.New("missing subcommand; run baton help")
		}
		cmd += " " + args[0]
		args = args[1:]
	}
	f := flag.NewFlagSet(cmd, flag.ContinueOnError)
	f.SetOutput(errOut)
	var p, t, session, source, path, title, draft, format, token, toSession, toSource string
	var expected, revision, budget int
	var claim, accept bool
	projectFlag := func() { f.StringVar(&p, "project", "", "explicit project ID") }
	taskFlags := func() { projectFlag(); f.StringVar(&t, "task", "", "explicit task ID") }
	sessionFlag := func() { f.StringVar(&session, "session", "", "explicit non-secret session label") }
	acceptFlag := func() {
		f.BoolVar(&accept, "accept-workspace-change", false, "record reconciliation after inspecting workspace differences")
	}
	saveFlags := func() {
		taskFlags()
		sessionFlag()
		f.StringVar(&source, "source", "", "client label")
		f.IntVar(&expected, "expect-revision", -1, "required observed revision")
		acceptFlag()
	}
	switch cmd {
	case "project register":
		f.StringVar(&path, "path", "", "workspace root")
	case "project list":
	case "task new":
		projectFlag()
		sessionFlag()
		f.StringVar(&title, "title", "", "task goal/title")
		f.StringVar(&source, "source", "", "client label")
	case "task list":
		projectFlag()
	case "resume":
		taskFlags()
		sessionFlag()
		acceptFlag()
		f.BoolVar(&claim, "claim", false, "claim cooperative main-writer registration")
		f.StringVar(&format, "format", "markdown", "markdown or json")
		f.IntVar(&budget, "budget", 12000, "soft brief character target; never truncates")
	case "check":
		taskFlags()
		sessionFlag()
	case "handoff":
		taskFlags()
		f.StringVar(&toSession, "to-session", "", "fresh receiving session label (not reserved)")
		f.StringVar(&toSource, "to-source", "", "receiving client label for future checkpoints")
		f.StringVar(&format, "format", "markdown", "markdown prompt or json locator/argument array")
	case "checkpoint":
		saveFlags()
		f.StringVar(&draft, "draft", "", "content JSON file; never modified")
	case "history":
		taskFlags()
		f.IntVar(&revision, "revision", 0, "show a full committed revision instead of metadata list")
	case "restore":
		saveFlags()
		f.IntVar(&revision, "revision", 0, "committed content revision to copy into a new revision")
	case "writer release":
		projectFlag()
		sessionFlag()
	case "repair":
		taskFlags()
		f.IntVar(&expected, "expect-revision", -1, "required current revision")
	case "unlock":
		f.StringVar(&token, "lock-token", "", "exact lock digest; operator must confirm all writers stopped")
	default:
		return fmt.Errorf("unknown command %q; run baton help", cmd)
	}
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(f.Args(), " "))
	}
	for key, value := range map[string]string{"project": p, "task": t, "session": session, "source": source, "title": title, "to-session": toSession, "to-source": toSource} {
		if f.Lookup(key) != nil && value == "" {
			return fmt.Errorf("--%s is required", key)
		}
	}
	if cmd == "resume" && (format != "json" && format != "markdown" || budget < 1) {
		return errors.New("resume requires json/markdown format and a positive budget")
	}
	if cmd == "handoff" && format != "json" && format != "markdown" {
		return errors.New("handoff requires json/markdown format")
	}
	if (cmd == "checkpoint" || cmd == "restore" || cmd == "repair") && expected < 1 {
		return errors.New("--expect-revision must be supplied and positive")
	}
	if cmd == "project register" && path == "" {
		return errors.New("--path is required")
	}
	if cmd == "checkpoint" && draft == "" {
		return errors.New("--draft is required")
	}
	var s *baton.Store
	var err error
	if cmd == "project register" {
		s, err = baton.Open(*state)
	} else {
		s, err = baton.OpenExisting(*state)
	}
	if err != nil {
		return err
	}
	o := baton.SaveOptions{Project: p, Task: t, Session: session, Source: source, Expected: expected, AcceptWorkspaceChange: accept}
	switch cmd {
	case "project register":
		v, err := s.Register(ctx, path)
		if err != nil {
			return err
		}
		return printJSON(out, v)
	case "project list":
		v, err := s.Projects()
		if err != nil {
			return err
		}
		return printJSON(out, v)
	case "task new":
		v, err := s.NewTask(ctx, p, title, session, source)
		if err != nil {
			if v.ID != "" {
				fmt.Fprintln(errOut, "task may already exist; inspect task list:", v.ID)
			}
			return err
		}
		return printJSON(out, v)
	case "task list":
		v, err := s.Tasks(p)
		if err != nil {
			return err
		}
		return printJSON(out, v)
	case "resume":
		v, err := s.Resume(ctx, p, t, session, claim, accept)
		if err != nil {
			return err
		}
		if accept {
			fmt.Fprintln(errOut, "workspace differences explicitly accepted for this binding; brief describes the saved workspace")
		}
		if !claim {
			fmt.Fprintln(errOut, "read context only; resume --claim before saving or editing as main writer")
		}
		if format == "json" {
			return printJSON(out, v)
		}
		_, err = io.WriteString(out, baton.Brief(v, budget))
		return err
	case "handoff":
		v, err := s.PrepareHandoff(ctx, p, t, toSession, toSource)
		if err != nil {
			return err
		}
		if format == "json" {
			return printJSON(out, v)
		}
		_, err = io.WriteString(out, v.Prompt())
		return err
	case "check":
		v, err := s.Check(ctx, p, t, session)
		if err != nil {
			return err
		}
		if err := printJSON(out, v); err != nil {
			return err
		}
		if len(v.Issues) > 0 {
			return errors.New("check found issues; inspect JSON output")
		}
		return nil
	case "checkpoint":
		file, err := os.Open(draft)
		if err != nil {
			return err
		}
		raw, readErr := io.ReadAll(io.LimitReader(file, (8<<20)+1))
		closeErr := file.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return err
		}
		v, saved, err := s.Save(ctx, o, raw)
		if saved != "" {
			fmt.Fprintln(errOut, "private draft:", saved)
		}
		if err != nil {
			return err
		}
		return printJSON(out, v)
	case "history":
		vs, err := s.History(p, t)
		if err != nil {
			return err
		}
		if revision != 0 {
			if revision < 1 || revision > len(vs) {
				return errors.New("revision is outside committed history")
			}
			return printJSON(out, vs[revision-1])
		}
		type entry struct {
			Revision     int    `json:"revision"`
			Source       string `json:"source"`
			Session      string `json:"session"`
			CreatedAt    string `json:"created_at"`
			RestoredFrom int    `json:"restored_from,omitempty"`
		}
		entries := []entry{}
		for _, v := range vs {
			entries = append(entries, entry{v.Number, v.Source, v.Session, v.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), v.RestoredFrom})
		}
		return printJSON(out, entries)
	case "restore":
		v, saved, err := s.Restore(ctx, o, revision)
		if saved != "" {
			fmt.Fprintln(errOut, "private draft:", saved)
		}
		if err != nil {
			return err
		}
		return printJSON(out, v)
	case "writer release":
		if err := s.Release(p, session); err != nil {
			return err
		}
		fmt.Fprintln(out, "writer registration released")
		return nil
	case "repair":
		v, err := s.Repair(p, t, expected)
		if err != nil {
			return err
		}
		return printJSON(out, v)
	case "unlock":
		if err := s.Unlock(token); err != nil {
			return err
		}
		fmt.Fprintln(out, "storage lock removed; inspect check/history before continuing")
		return nil
	}
	return nil
}
