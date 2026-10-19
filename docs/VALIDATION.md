# Local validation — 2026-10-17

Local environment: macOS, arm64, Go 1.27.1. The module declares Go 1.23; an actual Go 1.23 toolchain run is not yet recorded.

Completed checks:

- `gofmt` and `go test ./...`.
- `go test -race ./...` and `go vet ./...`.
- Native `go build -trimpath` and executable end-to-end CLI simulation with separate session labels.
- Linux/amd64 and Windows/amd64 cross-compilation. These binaries were not executed on their target platforms.
- Skill scalar frontmatter, supporting-reference links, both project discovery symlinks, common-rule import, example JSON roundtrip, and a targeted personal-path scan of source files.

Tests cover project/task isolation, explicit bindings, private state boundaries, duplicate/unknown JSON keys, corruption/missing files, expected-revision conflicts with retained drafts, stale sessions, writer conflicts, goroutine/process concurrency, save interruption before publication/after publication/after HEAD replacement, abrupt process exit and manual recovery, Git/unborn/non-Git workspaces, symlinks, WIP loss, directory identity replacement, and append-only summary restore.

The compiled CLI simulation uses synthetic files and session labels only. It compares full content, workspace metadata, verification and next steps across a three-session roundtrip. No Codex or Claude client was driven by that simulation.

Remaining checks:

- At this snapshot, interactive client chats were pending. The later 2026-10-19 context-handoff run is recorded below; fresh interactive source editing with nonempty WIP remains unobserved.
- Upstream skill-creator `quick_validate.py` could not run because PyYAML is absent. No dependency was installed; narrower scalar-frontmatter and reference/link checks were performed separately.
- Runtime filesystem/process checks on Linux/Windows; Windows ACL and directory durability behavior. Native symlink checkout support on Windows is unverified.
- Destructive power-loss/filesystem fault tests. Process exits and injected commit-boundary failures do not prove hardware-level durability.
- The targeted source scan does not replace the complete publication review and scoped approval in [RELEASE.md](RELEASE.md).

# Client acceptance — 2026-10-18

Local environment: macOS, arm64. Each client used its own native account. One synthetic Go workspace and one private state directory outside it were used; no real project was touched.

| Step | Client and session | Observed result |
| --- | --- | --- |
| 1 | Existing Codex desktop chat | Registered the workspace, saved revisions 1–2 with goal, acceptance, non-goals, one decision, one passed check and unverified items, then released the writer. |
| 2 | New Claude Code 2.1.302 session, non-interactive (`claude -p`) | Discovered the project skill, resumed the exact IDs with a fresh label, reconciled the WIP, finished the change, ran tests and saved revision 3. The first attempt could not write its draft because the test harness omitted the drafts directory; the retry with that directory granted succeeded without leaving the sandbox. |
| 3 | New Codex CLI 0.163.0-alpha.10.2 session, non-interactive (`codex exec`) | Resumed revision 3 with a fresh label, verified all saved WIP paths and the tests as received, and saved revision 4. Goal, acceptance and non-goals were identical across revisions 2–4; every earlier decision and rationale and every permission reminder was preserved. |
| 3b, 4 | baton CLI with the old Codex session label | `check` reported the old binding as stale. A save with the old expected revision was rejected as a conflict, and the draft was retained. Reloading bound the label to the latest revision. A guessed old revision still conflicted. A missing saved WIP file blocked `resume`, also with `--accept-workspace-change`. Restoring revision 2 and then revision 4 appended revisions 5–6. Revisions 1–4 and the workspace bytes were unchanged. |

The same day, `BATON_STATE_DIR` support was added and covered by `TestStateDirEnvironmentOverride`. A fresh Claude Code session (sandbox enforced, no unsandboxed fallback) and a fresh read-only Codex session, both started from an unrelated directory with user-level configuration, each received the variable from native client settings. Both found `baton` on `PATH` and discovered the skill from user-level links.

Not observed: interactive Claude Code chat UI, a new Codex desktop chat UI, and the old Codex chat's agent reacting to the stale state (only the CLI behavior for its label was exercised). Steps 2–3 were non-interactive client sessions, not UI tests.

# Handoff prompts and diagnostics — 2026-10-19

Added a read-only `handoff` prompt generator, full saved WIP metadata in the default brief, per-path workspace change diagnostics and writer-conflict reporting in `check`. No storage schema or revision format changed.

Completed on macOS arm64 with Go 1.27.1:

- `gofmt`, `go test ./...` (29 top-level tests and their subtests), `go test -race ./...`, `go vet ./...` and the native executable build.
- Linux/amd64 and Windows/amd64 cross-compilation; no target-platform execution.
- Fourteen isolated executable/artifact checks, including literal Unicode/space/quote/dollar-sign arguments, state/environment precedence, generation leaving all state bytes/modes/mtimes unchanged, active-writer and reused-label rejection, receiving a newer revision from an older locator, per-path metadata differences, and missing-WIP rejection.
- Existing project/user discovery links still resolve to the one canonical skill. A targeted public-source scan found no personal filesystem paths or selected conversation identifier; this is not a complete publication audit.

An independent review in a separate Claude Code session reproduced these checks and a byte-identical `-trimpath` build. It found that the prompt's JSON strings HTML-escaped `&`, `<` and `>` as unicode escapes, so a receiver copying values literally would get a wrong path such as one containing `R&D`. The prompt now encodes without HTML escaping. `TestHandoffPreservesLiteralArguments` covers this, and the rebuilt candidate resumed correctly from copied arguments whose state path contained `&`. The `--format json` output still uses standard escaping, which JSON decoders read correctly.

At the initial isolated-validation stage the new prompt had not been sent to a native receiving chat. The later interactive run is recorded below; the 2026-10-18 non-interactive client record remains separate. Windows runtime behavior, Go 1.23 execution and power-loss durability remain unverified. The initial build did not replace any user-level executable.

# Interactive context handoff — 2026-10-19

Observed on macOS arm64 in user-opened native chats. The audit inspected receiving-session records, authoritative checkpoints, actual Git/workspace state and the independent test logs. Personal state directories, session identifiers and raw records are excluded from this source tree.

| Stage | Observed behavior |
| --- | --- |
| Original Codex desktop | Saved the implementation, decisions/rationales, checks and next steps; released writer. |
| Fresh Claude Code chat | One initial user request, no earlier conversation entries or reads of other session records observed. Followed the generated locator, resumed latest, restated the task, reconciled actual files, ran independent uncached tests, saved and released writer. |
| Fresh Codex desktop chat | One completed user turn. Followed the Claude-generated locator and latest checkpoint without reading other native conversations, restated the brief, ran independent uncached tests, saved and released writer. Normal user-level memory/rule configuration remained enabled. |
| Return to original Codex | Detected its stale binding, loaded latest before acting and appended verification using the complete incoming content. Earlier revisions and repository contents remained unchanged; writer was released. |

Goal and acceptance criteria remained identical. All earlier decisions and rationales survived; history/permission text was condensed explicitly rather than preserved verbatim, with prior detail still available in immutable revisions. Final workspace checks reported no discrepancies or missing WIP, and the writer was free.

Both fresh receiving chats used new empty Go caches with module-network access disabled. Their `go test ./...` runs passed with Go 1.27.1. This was a context-handoff and verification run with a clean workspace and zero WIP; neither fresh receiving chat edited source. It is not a test of zero shared memory or rules, all UI surfaces, fresh interactive code edits, Windows/Linux execution, Go 1.23 execution or hardware power loss. Client build versions for this interactive run were not independently captured; earlier CLI versions apply only to the earlier record.

The reviewed source was subsequently rebuilt with `-trimpath` from a clean local commit for personal deployment. Fourteen isolated executable/artifact checks passed against both that build and the installed executable. Only the existing user-owned CLI file was replaced, with a private rollback backup; project code, native authentication, permissions, discovery links and hooks were not changed by the deployment. The earlier candidate build remains a separate dated artifact.
