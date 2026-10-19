# Agent Handoff

`baton` is a local checkpoint CLI for continuing one task across official coding-agent clients. Each client uses its own account and subscription. An agent writes a structured summary; baton validates and versions it without calling a model or reading raw conversations.

**Status:** experimental open-source CLI. Codex → Claude Code → Codex context handoff passed with non-interactive sessions and, on macOS, fresh interactive receiving chats. The fresh interactive sequence verified a clean workspace without source edits; see [validation scope](docs/VALIDATION.md). The [public repository](https://github.com/songsongsongggg/agent-handoff) contains source only; packaged releases and Linux/Windows runtime acceptance remain pending.

## Build and try

Requires Go 1.23+ and Git only for Git workspaces. No third-party Go modules.

```sh
go test ./...
go build -o bin/baton ./cmd/baton
bin/baton help
```

Choose a private state directory **outside** the workspace. Use the same explicit IDs when switching clients:

```sh
bin/baton --state-dir /private/state project register --path /path/to/workspace
bin/baton --state-dir /private/state task new --project PROJECT_ID --title "Implement pagination" --session codex-a --source codex
bin/baton --state-dir /private/state resume --project PROJECT_ID --task TASK_ID --session codex-a --claim
bin/baton --state-dir /private/state checkpoint --project PROJECT_ID --task TASK_ID --session codex-a --source codex --draft /private/draft.json --expect-revision 1
bin/baton --state-dir /private/state writer release --project PROJECT_ID --session codex-a
bin/baton --state-dir /private/state handoff --project PROJECT_ID --task TASK_ID --to-session claude-b --to-source claude-code
```

Instead of repeating `--state-dir`, set an absolute `BATON_STATE_DIR` in each client's environment configuration. Replace `PROJECT_ID` and `TASK_ID` with returned IDs. `examples/checkpoint.json` is a synthetic content draft. `resume` renders only the latest brief and binds that explicit session. Before an old session writes again, it must check/reload the current revision. A session identifier is a caller-chosen non-secret label, not authentication.

Copy the entire `handoff` output into a fresh receiving chat opened on the same workspace. It supplies the exact state directory, project/task IDs, new session label and literal argument array for `resume --claim`. Generating it is read-only: it neither opens a client nor reserves a label. The receiver reloads the latest revision; the prompt does not duplicate a potentially stale summary. Save the current workspace and release the writer first.

Commands include `project register/list`, `task new/list`, `resume`, `handoff`, `check`, `checkpoint`, `history`, `restore`, `writer release`, `repair`, and `unlock`. See [CLI reference](docs/CLI.md), [design](docs/DESIGN.md), [implementation/acceptance plan](docs/PLAN.md), and [recorded validation](docs/VALIDATION.md).

## Boundaries

- Checkpoints preserve context, **not source code**. Lost directories or missing/changed WIP block normal resume. There is no code recovery promise.
- Main-writer registration is cooperative. It does not prevent a client or editor from changing files. A short storage lock serializes baton mutations separately.
- JSON is the only state source. Markdown is generated on demand; history and long evidence are read only when needed. The brief budget is a soft character target and never truncates decisions.
- No network, model routing, credentials, hooks, background services, chat scraping, automatic command execution, or native chat replacement.
- Unix state directories/files use `0700`/`0600`; Windows requires an appropriately private parent ACL. Windows is a compile-only candidate until filesystem and client tests are run.
- `--accept-workspace-change` records explicit reconciliation of changed files. It cannot bypass a missing workspace or missing saved WIP path; recover the actual files separately first.

The shared candidate skill is [here](skills/agent-handoff/SKILL.md). Project discovery links do not install anything into a user's account. [Client adaptation](docs/CLIENTS.md) distinguishes documented discovery from real acceptance.

## License

Copyright 2026 songsongsongggg. Licensed under the [Apache License, Version 2.0](LICENSE).
