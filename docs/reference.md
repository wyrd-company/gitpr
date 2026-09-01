---
docs: true
title: Command reference
order: 4
---

## Commands

| Command                          | Description and principal flags                                                                                                                                                                                                                                 |
| -------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `gitpr create`                   | Create an open branch-based PR. Requires `--title`; accepts `--description`, `--worktree`, and `--base`. Refuses a duplicate open source/base pair.                                                                                                             |
| `gitpr review <id>`              | Print the live basis, diff, containment, latest event, interdiff, and projected thread state as YAML. Performs no writes.                                                                                                                                       |
| `gitpr approve <id>`             | Append an accepted event. Requires `--basis <source>:<base>` or both head flags; refuses missing branches, live drift, terminal PRs, and noncanonical heads. Does not merge.                                                                                    |
| `gitpr reject <id>`              | Append a rejected event for a branch-based expected pair without a live-drift check, or retain legacy reject behavior. Alias: `request-changes`.                                                                                                                |
| `gitpr merge <id>`               | Merge an eligible latest accepted event atomically, or use legacy merge behavior. `--cleanup` removes the source worktree and branch after success.                                                                                                             |
| `gitpr comment <id>`             | Add an anchored thread with `--file`, `--line-start`, optional `--line-end`, and `--side old\|new`; use `--pr-level` or reply with `--thread`. `--text` is required. Optional basis flags override live heads. Legacy records retain `--commit` and `--update`. |
| `gitpr comments [id]`            | Print legacy comments or branch-based threads as YAML. Opens a picker without an ID.                                                                                                                                                                            |
| `gitpr resolve <id> <thread-id>` | Resolve a branch-based thread.                                                                                                                                                                                                                                  |
| `gitpr reopen <id> <thread-id>`  | Reopen a branch-based thread.                                                                                                                                                                                                                                   |
| `gitpr close <id>`               | Close an open branch-based PR. Requires `--reason`; integrated uses `--destination` plus `--commit` and/or `--patch-id`; superseded uses `--superseded-by`; every reason accepts `--note`.                                                                      |
| `gitpr list`                     | List open records by default. Accepts `--state`, deprecated `--status`, closed `--reason`, or `--all`.                                                                                                                                                          |
| `gitpr show [id]`                | Print the complete record as YAML. Opens a picker without an ID.                                                                                                                                                                                                |
| `gitpr delete <id>`              | Preview and exit non-zero without `--force`; with it, remove the complete record namespace.                                                                                                                                                                     |
| `gitpr refresh <id>`             | Recompute stored merge-conflict metadata for an open legacy snapshot.                                                                                                                                                                                           |
| `gitpr tui`                      | List both formats. Branch-based details are read-only; legacy diff actions remain available.                                                                                                                                                                    |
| `gitpr debug export <id>`        | Export `--ref meta` or legacy `head`/`base` refs to `--to <directory>`.                                                                                                                                                                                         |

## List filters

| Filter     | Legacy snapshots | Branch-based PRs |
| ---------- | ---------------- | ---------------- |
| `open`     | Yes              | Yes              |
| `approved` | Yes              | No               |
| `rejected` | Yes              | No               |
| `merged`   | No               | Yes              |
| `closed`   | No               | Yes              |

`--reason integrated|superseded|abandoned` requires a closed state filter.
`--all` cannot be combined with state or reason filters.

## Refusal and exit behavior

- Verdict and explicit comment heads must be full 40-character lowercase commit
  IDs.
- Approve refuses source deletion and source/base drift. Reject records the
  supplied existing pair despite drift.
- Verdicts and merge refuse terminal branch-based records.
- Merge requires a latest accepted event, exact live heads, and strict
  fast-forward ancestry.
- Multiple or dirty base worktrees refuse before merge.
- A branch-based post-merge refresh or cleanup failure exits non-zero after
  reporting success and a repair command. Legacy cleanup failure retains exit
  zero.
- Close refuses terminal and legacy records. Comments remain legal in every
  branch-based state.
- Forced deletion removes retention refs and can make reviewed commits
  collectable.

## Retention

Merge and close retain metadata, event pins, remaining anchor pins, and state
indexes. Default listing hides terminal records, but filters and ID-addressed
commands continue to find them. Only `delete --force` removes the namespace.
