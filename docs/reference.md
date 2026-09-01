---
docs: true
title: Command reference
order: 4
---

## Commands

| Command                          | Description and principal flags                                                                                                                                                                                                                                 |
| -------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `gitpr create`                   | Create an open PR. Requires `--title`; accepts `--description`, `--worktree`, and `--base`. Refuses a duplicate open source/base pair.                                                                                                             |
| `gitpr review <id>`              | Print the live basis, diff, containment, latest event, interdiff, and projected thread state as YAML. Performs no writes.                                                                                                                                       |
| `gitpr approve <id>`             | Append an accepted event. Requires `--basis <source>:<base>` or both head flags; refuses missing branches, live drift, terminal PRs, and noncanonical heads. Does not merge.                                                                                    |
| `gitpr reject <id>`              | Append a rejected event for an expected pair without a live-drift check. Alias: `request-changes`.                                                                                                                |
| `gitpr merge <id>`               | Merge an eligible latest accepted event atomically. `--cleanup` removes the source worktree and branch after success.                                                                                                             |
| `gitpr comment <id>`             | Add an anchored thread with `--file`, `--line-start`, optional `--line-end`, and `--side old\|new`; use `--pr-level` or reply with `--thread`. `--text` is required. Optional basis flags override live heads. |
| `gitpr comments [id]`            | Print a record's threads as YAML. Opens a picker without an ID.                                                                                                                                                                            |
| `gitpr resolve <id> <thread-id>` | Resolve a comment thread.                                                                                                                                                                                                                                  |
| `gitpr reopen <id> <thread-id>`  | Reopen a comment thread.                                                                                                                                                                                                                                   |
| `gitpr close <id>`               | Close an open PR. Requires `--reason`; integrated uses `--destination` plus `--commit` and/or `--patch-id`; superseded uses `--superseded-by`; every reason accepts `--note`.                                                                      |
| `gitpr list`                     | List open records by default. Accepts `--state`, deprecated `--status`, closed `--reason`, or `--all`. Reports a count of records it cannot read.                                                                                                                                                          |
| `gitpr show [id]`                | Print the complete record as YAML. Opens a picker without an ID.                                                                                                                                                                                                |
| `gitpr delete <id>`              | Preview and exit non-zero without `--force`; with it, remove the complete record namespace.                                                                                                                                                                     |
| `gitpr tui`                      | List records. The detail view is read-only; actions use the CLI.                                                                                                                                                                    |
| `gitpr debug export <id>`        | Export `--ref meta` to `--to <directory>`.                                                                                                                                                                                         |

## List filters

`--state` accepts `open`, `merged`, and `closed`; any other value is refused.
`--reason integrated|superseded|abandoned` requires a closed state filter.
`--all` cannot be combined with state or reason filters.

## Refusal and exit behavior

- Verdict and explicit comment heads must be full 40-character lowercase commit
  IDs.
- Approve refuses source deletion and source/base drift. Reject records the
  supplied existing pair despite drift.
- Verdicts and merge refuse terminal records.
- Merge requires a latest accepted event, exact live heads, and strict
  fast-forward ancestry.
- Multiple or dirty base worktrees refuse before merge.
- A post-merge refresh or cleanup failure exits non-zero after reporting success
  and a repair command.
- Close refuses terminal records. Comments remain legal in every state.
- Every ID verb refuses a legacy (schema-absent) record and names the "Legacy
  records" section of the usage guide, which carries the raw-git retrieval and
  removal commands.
- Forced deletion removes retention refs and can make reviewed commits
  collectable.

## Retention

Merge and close retain metadata, event pins, remaining anchor pins, and state
indexes. Default listing hides terminal records, but filters and ID-addressed
commands continue to find them. Only `delete --force` removes the namespace.
