---
tags: [go, build, cli]
---

## Decision

`build.sh` at the repo root builds the kakei CLI into a single static, stripped binary. Verified output: `file kakei` reports statically linked, the binary is 8.0M, and `./kakei --help` runs correctly from it.

## Why

User request: "Create a build script that will build this CLI into a single binary."

## Deferred

- Version stamping
- Multi-platform build matrix
- Makefile

These were explicitly skipped for now, to be added when releasing.

Links: [[decisions/0002-flat-cmd-package-layout]]