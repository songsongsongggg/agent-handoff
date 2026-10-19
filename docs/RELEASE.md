# Publication checklist (not authorization)

The initial source publication is [songsongsongggg/agent-handoff](https://github.com/songsongsongggg/agent-handoff), public, licensed under [Apache-2.0](../LICENSE), copyright 2026 songsongsongggg. The module path is `github.com/songsongsongggg/agent-handoff`. The approved publication set is 31 reviewed source/document/discovery paths plus `LICENSE`; private state, drafts, binaries and reports are excluded. Public history starts from a separately authored source snapshot; earlier private commits remain local. No packaged release or CI is included.

Before a remote write, present and confirm its concrete target and public contents. The initial owner/repo, visibility, license, copyright holder, module path, source set and separate public root commit were explicitly approved on 2026-10-19. That approval does not authorize unrelated future pushes, releases or configuration changes. Confirm project/name availability separately when creating another repository.

- Review `git status` and the complete tracked/untracked publication set, including symlinks.
- Exclude `work/`, `outputs/`, local state, drafts, real session identifiers, raw chats, filesystem/user paths and credentials. An ignore file is not a complete privacy review.
- Use only synthetic examples. Inspect public artifacts and build metadata for personal paths; use `-trimpath` for distributable binaries.
- Run the documented local checks. Run Linux and Windows filesystem/process tests on real hosts before claiming those platforms supported. Cross-compilation is not runtime acceptance.
- Finish real dual-client acceptance or label it explicitly unverified. Review discovery docs at release time.
- Add the approved license, authorship details, module path and release notes. Add CI only with reviewed actions pinned to immutable revisions and a minimal permissions declaration.
- Prepare recovery instructions and known limitations. Never publish personal handoff state as a demonstration.

No commit, push, remote repository, CI execution or release is created by this checklist.
