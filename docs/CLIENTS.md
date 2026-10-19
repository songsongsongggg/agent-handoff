# Client adaptation and candidate validation

One canonical body lives at `skills/agent-handoff/SKILL.md`. Project-local `.agents/skills/agent-handoff` and `.claude/skills/agent-handoff` link to that directory. `CLAUDE.md` imports `AGENTS.md`; it does not copy common rules. This repository layout does not install user-level discovery links or change authentication, permissions, hooks or services. Personal deployment is a separate operation.

Official documentation checked 2026-10-17: [Codex local skill discovery](https://learn.chatgpt.com/docs/build-skills) documents `.agents/skills` and symlinked skill folders. [Claude Code skills](https://code.claude.com/docs/en/skills) documents `.claude/skills`, symlinked skill folders, and default model invocation. These support the candidate layout; they do not prove discovery in the user's installed clients. The skill intentionally omits explicit-only policies and tool permission overrides.

Candidate links can be inspected with `readlink` and by reading `SKILL.md` through each link. Removing the two project-local links reverses discovery; the shared body remains. On Windows a checkout may materialize a symlink as text or require Developer Mode; client/runtime support is pending and no automatic installer is provided.

## Real acceptance

Steps 1–4 were observed on 2026-10-18 with an existing Codex desktop chat, a non-interactive Claude Code session and a non-interactive Codex CLI session; see [the record](VALIDATION.md#client-acceptance--2026-10-18). On 2026-10-19 an old Codex desktop chat handed context to a fresh Claude Code chat and then a fresh Codex desktop chat. The old Codex chat also detected a stale binding, reloaded latest and appended a new checkpoint. See [interactive scope](VALIDATION.md#interactive-context-handoff--2026-10-19).

Use an isolated synthetic code project and a private state directory outside it. Record client versions and observed UI discovery. Keep each client on its own native account; do not share credentials or route one subscription through another client.

1. In an existing Codex chat, bind an explicit task, make small WIP, record goal/acceptance, one decision and rationale, one completed check and one unverified check. Save and release.
2. Generate `handoff --project ID --task ID --to-session claude-new --to-source claude-code` with the same state directory. Copy the output into a new Claude Code chat. Confirm normal discovery, resume the exact IDs with a fresh label, inspect actual WIP, and restate goal/decision/validation/next step. Add a small change, run the relevant check, save and release.
3. Start a new Codex chat and resume the same task. Verify all fields and actual files against the newest revision. Return to the old Codex chat and verify that it reloads or is blocked from stale saving.
4. Attempt an out-of-date save and a missing-WIP resume. Confirm conflict draft retention and missing-code blocking. Inspect committed history after summary restore and verify the code was not changed by restore.

The fresh interactive sequence validated context receipt, actual-file reconciliation, independent tests, checkpoint publication and writer release with a clean workspace. Source edits and nonempty WIP in a fresh interactive receiving chat remain unobserved; earlier CLI and existing-chat results are recorded separately. There is no need to buy a subscription to run local development/tests. No native chat UI switching or crash-proof autosave claim is made.

The prompt generator was added after the recorded 2026-10-18 client run. On 2026-10-19 both fresh receiving chats followed its exact project/task/state/session locator and resumed latest. Prompt generation does not create or send a chat for the user.
