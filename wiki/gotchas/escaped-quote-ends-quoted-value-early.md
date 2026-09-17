---
tags: [notes, parsing, bug]
---

## What happened

A pre-merge review of the `feat/notes` branch (a `cavecrew-reviewer` subagent walking the full `master...feat/notes` diff) found a bug in `internal/notes/frontmatter.go` that none of the module's own tests had caught: `stripComment`, the function that splits a front matter value from a trailing `# comment`, mishandled a backslash-escaped quote inside a double-quoted value. `splitList` (which splits `[a, b]` inline lists) had the identical bug — the escape-handling logic had been copied between the two.

Root cause: both functions tracked quote state with a single loop over runes, and handled an escaping backslash with `case quote == '"' && r == '\\': continue`. `continue` only skips to the next rune in the range loop — it does not skip *processing* of that next rune. So for the two-character sequence `\"`, the backslash was consumed and the quote left open, but the very next rune — the escaped quote itself — then fell into the ordinary "quote != 0 and this rune matches the quote" branch and closed the quote as if it were unescaped. A value like `title: "Say \" # hash"` therefore split at the `#` instead of keeping the whole string as the title.

The bug was confirmed with a standalone Go reproduction script outside the test suite before being trusted, then turned into a real test case against the actual parser.

## Fix

Both `stripComment` and `splitList` in `internal/notes/frontmatter.go` gained an `escaped bool` variable: on seeing `\` while inside a double-quoted value, set `escaped = true` and do nothing else; on the very next rune, if `escaped` is true, clear it and skip normal processing entirely, so that character can never be mistaken for a closing quote or anything else. Two test cases were added: a title containing an escaped quote followed by a `#` (`Parse` must keep it all as the title, not split it into a comment), and a tag-list item containing an escaped quote (`splitList` must keep it as one item).

## Verification

`gofmt`/`go vet`/`go test ./internal/notes/ ./cmd/` clean. Committed as `fix(notes): an escaped quote no longer ends a quoted front matter value`, pushed to `feat/notes`, and PR #11's CI re-ran green afterward.

## Why it happened

The front matter parser is hand-rolled rather than a YAML library ([[decisions/0025-notes-as-markdown-files-with-a-computed-priority]]), so escape handling is pecunia's own code with no upstream test suite behind it. Reading `continue` inside a `range` loop as "skip the next character" — when it only skips the rest of *this* iteration — is an easy trap in a hand-rolled character-by-character state machine, the same shape of bug (validating one thing while a nearby boundary is left unguarded) that hit account code validation and `huh`'s EOF behavior in earlier modules.

Links: [[decisions/0025-notes-as-markdown-files-with-a-computed-priority]] · [[sessions/b410d49d-989f-41e2-bffb-553cf0bbf03e]]