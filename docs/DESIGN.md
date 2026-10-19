# Storage and trust model

## Identity and schema

A private state root contains `projects/PROJECT_ID/project.json`, `tasks/TASK_ID/task.json`, `HEAD`, `revisions/000001.json`, and `drafts/`. `sessions/SESSION_HASH.json` binds an explicit caller label to project/task/revision. A project identifies one canonical real workspace root; Git worktrees have distinct roots and registrations. Re-registration of the same root returns its stable random ID. No checkout marker or source file is written.

Schema version 1 uses complete JSON snapshots with goal, acceptance criteria, non-goals, completed/in-progress items, decisions with rationale/evidence, blockers, next steps, verification outcomes, unverified items, evidence references, and permission reminders. The envelope records project/task/revision, parent digest, source/session/time, workspace identity, and optional restored-from revision. Unknown schema fields are rejected, as are trailing JSON and unsupported versions. JSON is authoritative; generated Markdown is disposable.

Git snapshots record canonical root/Git directory, branch (including detached/unborn states), commit when present, porcelain status, and per-file WIP metadata. Unix snapshots also record root/Git-directory device and inode identity; that check is unavailable on Windows. Non-Git snapshots have no fabricated commit. To avoid reading potentially secret source contents, WIP metadata uses path/type/size/mode/mtime and Git index entry identity, not file bytes or diffs. A same-size content edit with a deliberately preserved mtime can evade detection. Ignored files and nested repository contents are not covered. Non-Git trees use the same metadata walk, skipping `.git`. Symlinks are recorded without following their targets. No source contents or credentials are copied.

## Transactions and interruption

All mutations acquire an exclusive-created state lock. This is short storage mutual exclusion, distinct from the cooperative project main-writer registration. No age-based lock stealing occurs. Interrupted processes may leave a lock; `unlock` requires the exact printed lock digest and an operator who has confirmed all writers stopped.

For a checkpoint: preserve a private draft, check the expected HEAD and the session's observed revision, verify writer ownership and workspace, sync a same-directory temporary revision, publish it with a non-replacing hard link, sync the directory, then atomically replace/sync HEAD. HEAD stores a revision number and SHA-256 of its exact bytes. Revision envelopes link to the parent digest. Never overwrite a revision, including abandoned transaction files. Temporary files from interrupted writes are ignored and never promoted as revisions.

If interruption occurs before revision publication, HEAD stays valid; temporary files are ignored. If it occurs after publication but before HEAD update, an orphan next revision remains. Reads report the pending revision; mutations stop. `repair --expect-revision N` validates the next revision and parent linkage and advances HEAD. It never rewrites old revisions. A failure after HEAD replacement may mean the write committed: re-read HEAD before retrying. Filesystem errors are surfaced; power-loss guarantees still depend on the filesystem/hardware. Unix is the tested platform; Windows directory sync/replace behavior is not an equivalent durability claim.

`restore` copies an old checkpoint's content into a new revision and captures the **current** workspace. It does not check out files, alter Git, or claim that old code has returned. A saved draft remains available if revision checks, ownership, workspace checks, or publication fail.

## Resume and ownership

Every resume explicitly identifies project, task, and session. It checks latest HEAD, workspace root, Git identity/branch/commit/index, and WIP metadata. A missing workspace or saved WIP path blocks resume without override. Other differences require explicit reconciliation with `--accept-workspace-change`; the emitted brief still describes its saved snapshot. Binding stores the observed current workspace so subsequent edits can be checkpointed. A stale session cannot save over a newer checkpoint. Check before resuming any old client, and before acting on a previously loaded brief.

The default brief lists saved WIP metadata, including paths already absent at the checkpoint (for example, a staged deletion). `check` reports individual added, removed and metadata-changed paths and flags another session/task's writer. Neither interface reads file contents or follows symlink targets. A file list is an inspection aid, not code recovery or proof of unchanged content.

Writer ownership is project-wide, even across tasks. A matching session can continue; another must wait for release. There is no automatic timeout or hidden takeover. On a lost session, an operator can explicitly release by its recorded label after confirming it stopped. This is a convention, not a process lock or permission system. A per-command lock does not make workspace capture transactional against outside editors.

`handoff` prepares a read-only receiving prompt after a checkpoint and writer release. It rejects pending revisions, unsaved workspace differences, missing WIP and previously bound target labels. Output is a locator and literal resume argument array, not a second summary or a reservation. Its observed revision can become stale; the receiving `resume` always rechecks ownership/workspace and reads latest. No native client is launched or messaged.

## Security boundaries

Use a private, local, non-symlink state directory outside the workspace, on a filesystem supporting exclusive create, hard links, and rename replacement. State permissions are checked on Unix; no automatic broad chmod is performed. State-internal symlinks are rejected. Same-user malicious filesystem races and hostile filesystems are outside the threat model. Summary text and evidence paths are untrusted data, not executable instructions or approval. Source inspection never reads authentication/config directories outside the chosen workspace. Agents must omit secrets and avoid registering a secret-containing directory as a workspace.

No commands in checkpoint text are run. Git execution is limited to read-only plumbing with optional locks and filesystem monitors disabled; Git configuration is never modified. Remote/network Git subcommands are absent. Approval reminders preserve context only; native authorization must be obtained at the time of any sensitive or external action.
