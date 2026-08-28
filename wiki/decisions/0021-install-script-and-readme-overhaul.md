---
tags: [install, release, github-actions, readme, cli]
---

---
tags: [install, release, github-actions, readme, cli]
---

## Decision

User: "Based on my other repo johnvilela/memoria - lets also create a install script and add the instructions on the README.md, also improve a bit the README with instructions and examples." `scripts/install.sh` was ported from the sibling project `memoria`'s own install script rather than written from scratch, and adapted to pecunia's release format.

## Two install modes

- **In-repo** — builds the local checkout with the same flags `build.sh` already uses (`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`). Requires Go.
- **Standalone (curl-pipeable)** — resolves the latest tag from the GitHub releases API, downloads the matching platform tarball (`pecunia_<version>_<GOOS>_<GOARCH>.tar.gz`, the naming [[decisions/0019-pr-only-master-with-ci-and-release-on-merge]] already established — pecunia publishes version-named tarballs, unlike memoria's raw per-platform binaries), verifies it against a `checksums.txt` release asset, and installs to `~/.local/bin` (override with the `BIN_DIR` env var).

Either way the script finishes by running `pecunia setup`, reading the prompt from `/dev/tty` so it stays interactive even when the whole thing is invoked as `curl ... | sh`.

## checksums.txt added to the release pipeline

`.github/workflows/release.yml` now publishes `checksums.txt` alongside the four platform tarballs — the one thing the install script needs that the release workflow didn't produce before.

## Version bump is what ships it

`cmd/main.go`'s `version` was bumped `0.2.0` → `0.2.1` in the same PR. Per [[decisions/0019-pr-only-master-with-ci-and-release-on-merge]], merging a PR that bumps the version literal is what cuts a release — and this bump is deliberate: it makes v0.2.1 the first release to actually carry `checksums.txt`, so the install script's own curl one-liner doesn't fail on a missing file the moment someone follows the new README. The gap self-resolves the moment this PR merges.

## README rewritten from the real help strings, not invented

The README was 3 lines plus a "Technology used" list before this. Rewritten to cover install instructions, a quick start, a full command table (every alias and flag pulled from the actual per-command `Help` constants in `cmd/*.go`), the 9 MCP tools, the database location/precedence order, and development notes.

`go install` was deliberately left out of the instructions: the module path is bare `pecunia` ([[decisions/0002-flat-cmd-package-layout]] territory), so `go install` can never resolve it to the right binary name — the tarballs (and now the install script) are the only distribution path, per [[decisions/0019-pr-only-master-with-ci-and-release-on-merge]].

## No AI attribution anywhere

User: "Dont write on the PR or commit message that this was created by an IA agent." Neither commit nor the PR body names an AI as author — an extension of the existing no-trailer commit convention ([[rules/commit-messages]]) to PR bodies as well, now recorded there.

## Shipped for manual review, not merged

Two commits split by concern per [[rules/commit-messages]] — `feat(install): add install script and release checksums`, then `docs(readme): add install instructions, usage examples and command reference` — pushed as **PR #3** on branch `feat/install-script` and left open, per the user's explicit request to review it manually before merge. Same pattern as PR #2 in [[decisions/0020-upgrade-command-self-updates-and-migrates]].

## Verification

`sh -n` on the script; an in-repo install into a scratch `BIN_DIR` (installed binary reported `0.2.1`; the no-TTY path printed the setup hint rather than hanging); tag resolution against the live GitHub API (returned `0.2.0`, the release current before this PR); the tarball URL pattern returning HTTP 200; `go build`, `go test ./...`, `gofmt -l .` all clean.

## Known thread

A background agent was separately dispatched near session end with the task "merge it and check the release runs" — its outcome is not confirmed by the transcript, and the session's own closing summary still described the PR as awaiting the user's manual review.

Links: [[sessions/52842417-3730-4f49-af46-f9654d86c3c8]] · [[decisions/0019-pr-only-master-with-ci-and-release-on-merge]] · [[decisions/0020-upgrade-command-self-updates-and-migrates]] · [[decisions/0002-flat-cmd-package-layout]] · [[rules/commit-messages]]