# Agent Handoff project rules

This repository contains the local `baton` CLI and a candidate handoff skill.

- `cmd/baton` owns CLI parsing; `internal/baton` owns schemas, storage, workspace checks, and rendering.
- Use Go's standard library only. The CLI does not call models, access authentication, import chats, execute checkpoint commands, or use the network. Git inspection is read-only and scoped to the registered workspace.
- JSON checkpoints are authoritative. Never overwrite revision files or infer a task from recency. Permission reminders are not authorization.
- Treat fixture paths and session names as synthetic. Keep real state, drafts, binaries, reports, and personal data out of the public source tree.
- `skills/agent-handoff/SKILL.md` is the only skill body. Discovery links are project-local; do not install user-level skills, hooks, services, or binaries as part of development.
- Validate changes with `gofmt`, `go test ./...`, `go test -race ./...`, and `go build ./cmd/baton`. Use isolated temporary workspaces for behavioral tests. Real client acceptance is a separate check; see `docs/VALIDATION.md` for its observed scope.
- No commit, push, remote creation, release, or license selection without the user's scoped request. Before publication, follow `docs/RELEASE.md`.
