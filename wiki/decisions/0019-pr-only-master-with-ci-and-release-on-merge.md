---
tags: [ci, release, github-actions, branch-protection, versioning]
---

## Decision

User: "Lets block the 'master' branch to commits. All commits there must come from PR. Also we must setup github actions to execute when a PR is created to check the code and run the test suite. And another github actions to execute when we merge the PR, it will create a new github release using the version of the CLI."

This picks up the deferred list from [[decisions/0003-static-stripped-build-script]] (version stamping, multi-platform matrix) — the "when releasing" moment arrived.

## Version source of truth

The CLI had **no version at all** before this. Now: `var version = "0.1.0"` in `cmd/main.go`, printed by `pecunia version` / `-v` / `--version`. The MCP server identity in `cmd/mcp.go` reuses the same var (it was a hardcoded, drifting `"1.0.0"`).

**Releasing = bumping that var in a PR.** No ldflags stamping, no bots, no VERSION file — the checked-in literal is the version. The release workflow greps it out of `cmd/main.go`.

## The two workflows

- `.github/workflows/ci.yml` — on `pull_request` to master, job `checks`: `gofmt -l` (must be empty), `go vet ./...`, `go build ./cmd`, `go test ./...`. Mirrors `.githooks/pre-commit` plus tests (the hook's "no tests yet" deferral in [[rules/git-hooks]] was stale — 48 test files). golangci-lint stays deferred per that rule.
- `.github/workflows/release.yml` — on push to master: greps the version, exits early if tag `v<version>` exists, else cross-compiles 4 tarballs (linux/darwin × amd64/arm64, `CGO_ENABLED=0` + the `build.sh` flags — pure-Go sqlite makes this trivial) and `gh release create` with `--generate-notes`. Merges that don't bump the version release nothing.

Module path is bare `pecunia` ([[decisions/0002-flat-cmd-package-layout]] territory), so `go install` can never work — the tarballs are the only distribution.

## Branch protection

A repo **ruleset** (id 21690437), not classic protection — classic can't require PRs with 0 approvals. Rules on the default branch: no deletion, no force-push, PR required (0 approvals — solo dev can't approve own PR anyway), required status check `checks`. **No bypass actors — applies to the admin too** (user's explicit choice over an admin-bypass variant). Tags are untouched, so the release workflow's tag creation is unaffected.

## Verification (all live)

- `pecunia version` → `0.1.0`; suite green locally.
- Setup commit `7ce910f` was the last direct push to master (ruleset created only after it landed — ordering matters, the ruleset would have blocked its own setup commit).
- Release run created v0.1.0 with all 4 tarballs; the linux_amd64 one was downloaded, extracted and prints `0.1.0`.
- A probe push to master was rejected with GH013: "Changes must be made through a pull request" + "Required status check 'checks' is expected".

One flake note: the very first `go test ./...` after the edits failed in `pecunia/cmd` and never reproduced across 6 re-runs (full suite and package-level). Not chased; if CI ever fails the same way, it's a pre-existing flake, not the workflow.

## Skipped

- Windows binaries — add a `windows/amd64` target to the loop when someone asks.
- Checksums/signing, changelog file, release-please — `--generate-notes` covers notes; revisit if distribution grows.

Links: [[decisions/0003-static-stripped-build-script]] · [[decisions/0002-flat-cmd-package-layout]] · [[rules/git-hooks]] · [[rules/commit-messages]]