# Contributing

The project is experimental and licensed under [Apache-2.0](LICENSE). Report issues and propose focused changes in the [public repository](https://github.com/songsongsongggg/agent-handoff). Include only synthetic examples; never upload private checkpoints or conversations.

Keep changes focused. Use standard-library Go and synthetic fixtures. Explain the concrete behavior and the check that demonstrates it. Run `gofmt`, `go test ./...`, `go test -race ./...`, `go vet ./...`, and build `./cmd/baton`.

Storage changes need failure-path tests: old HEAD preservation, orphan detection/recovery, no revision replacement, retained conflict drafts, and concurrent sessions. Workspace checks must fail closed when metadata cannot be obtained. Never add credential access, raw chat import, network calls, model routing, automatic execution or hooks as incidental changes.
