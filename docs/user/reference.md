---
product: gitpr
title: Command reference
order: 4
---

## Commands

| Command                          | Description and principal flags                                                                                                                                                                                                                                 |
| -------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `gitpr create`                   | Create an open PR. Requires `--title`; accepts `--description`, `--worktree`, and `--base`. Refuses a duplicate open source/base pair.                                                                                                             |
| `gitpr review <id>`              | Print the live basis, diff, containment, latest event, interdiff, and projected thread state as YAML. `--summary` omits patches and comment bodies and reports bounded interdiff file counts. Performs no writes.                                                                                                                                       |
| `gitpr approve <id>`             | Append an accepted event. Requires `--basis <source>:<base>` or both head flags; refuses missing branches, live drift, terminal PRs, and noncanonical heads. Does not merge.                                                                                    |
| `gitpr reject <id>`              | Append a rejected event for an expected pair. Branch drift is allowed; tag loss or drift refuses. Alias: `request-changes`.                                                                                                                |
| `gitpr merge <id>`               | Merge an eligible latest accepted event atomically. `--cleanup` removes the source worktree and branch after success.                                                                                                             |
| `gitpr comment <id>`             | Add an anchored thread with `--file`, `--line-start`, optional `--line-end`, and `--side old\|new`; use `--pr-level` or reply with `--thread`. Supply `--text` or `--text-file <path>` (`-` for stdin), exclusively. File/stdin bodies preserve bytes; blank paths and blank bodies are rejected. Optional basis flags override live heads. |
| `gitpr comments [id]`            | Print a record's threads as YAML. Opens a picker without an ID.                                                                                                                                                                            |
| `gitpr resolve <id> <thread-id>` | Resolve a comment thread.                                                                                                                                                                                                                                  |
| `gitpr reopen <id> <thread-id>`  | Reopen a comment thread.                                                                                                                                                                                                                                   |
| `gitpr close <id>`               | Close an open PR. Requires `--reason`; integrated uses `--destination` plus `--commit` and/or `--patch-id`; superseded uses `--superseded-by`; every reason accepts `--note`.                                                                      |
| `gitpr list`                     | List open records by default. Accepts `--state`, deprecated `--status`, closed `--reason`, or `--all`. Reports a count of records it cannot read.                                                                                                                                                          |
| `gitpr show [id]`                | Print the complete record as YAML. Opens a picker without an ID.                                                                                                                                                                                                |
| `gitpr delete <id>`              | Preview and exit non-zero without `--force`; with it, remove the complete record namespace.                                                                                                                                                                     |
| `gitpr tui`                      | List records. Detail is read-only; `d` loads live file hunks, basis, and threads with outdated state. Diff: `j`/`k` scroll, `PgUp`/`PgDn` page, `g`/`G` start/end, `R` refresh, `Esc` back. Actions use the CLI.                                                                                                                                                                    |
| `gitpr debug export <id>`        | Export the record metadata ref tree to `--to <directory>`.                                                                                                                                                                                         |

## Create base selection

`--base` overrides the saved repository default. Without it, detection uses the
saved default, origin’s default branch (full name), then local `main` or
`master`. A missing detected local branch refuses creation before any write;
the error names the branch and directs a retry with `--base <branch>`.
Successful creation saves the selected base as the repository default.

## List filters

`--state` accepts `open`, `merged`, and `closed`; any other value is refused.
`--reason integrated|superseded|abandoned` requires a closed state filter.
`--all` cannot be combined with state or reason filters.

## Refusal and exit behavior

- Verdict and explicit comment heads must be full 40-character lowercase commit
  IDs.
- Approve refuses source deletion and source/base drift. Reject records the
  supplied existing pair despite branch drift. Tag verdicts refuse tag loss or drift.
- Verdicts and merge refuse terminal records.
- Merge requires a latest accepted event, exact live heads, and strict
  fast-forward ancestry.
- Multiple or dirty base worktrees refuse before merge.
- A post-merge refresh or cleanup failure exits non-zero after reporting success
  and a repair command.
- Close refuses terminal records. Comments remain legal in every state.
- Every ID verb, `debug export` included, refuses a legacy (schema-absent)
  record and carries the raw-git read and removal commands inline; the "Legacy
  records" section of the usage guide carries the full story.
- Forced deletion removes retention refs and can make reviewed commits
  collectable.

## Retention

Merge and close retain metadata, event pins, remaining anchor pins, and state
indexes. Default listing hides terminal records, but filters and ID-addressed
commands continue to find them. Only `delete --force` removes the namespace.

## Review summary

`gitpr review <id> --summary` keeps the machine-readable `basis`, containment,
latest review event (including its verdict and judged heads), `verdict_hint`,
and projected thread IDs, kinds, statuses, anchors, and outdated flags.
Thread comments and diff bodies are omitted. A missing source or base is
reported in the basis, as in the default review.

When a prior event exists, `interdiff_summary` has fixed fields:
`previous_files`, `current_files`, `added_to_diff`, `removed_from_diff`,
`changed`, and `unchanged`. These compare diff entries by blob IDs and file
modes between the latest event's merge-base/source pair and the live pair.
Each compared diff treats renames as a deletion and an addition. The counters
track membership in those diffs: a path that changes from a modification to a
deletion is `changed`, rather than `removed_from_diff`. Binary and mode-only changes are
included. Counts do not measure hunk or line changes. No per-file list or patch
is computed. Without a prior event, the interdiff fields are omitted.

Both review modes perform no writes. Summary containment does not establish
that a PR meets every merge gate; `gitpr merge` still checks eligibility.

## Tag base contract

`create --base` accepts a branch, a lightweight tag, or an annotated tag that
peels to a commit. Unqualified names prefer branches. `refs/heads/<name>`
selects a branch; `refs/tags/<name>` selects a tag. Source identities remain
branches. Tag bases do not replace the saved default branch.

Tag records add `base_tag: {ref, object_sha, commit_sha}` to schema 2 and use
the full tag ref as `base_branch`. Records without `base_tag` retain branch
semantics. The original tag object and peeled commit are pinned under
`refs/gitpr/pr/<id>/tag/{object,commit}` until record deletion. Review reports
`base_tag` alongside its basis; `base_head_sha` is the peeled commit.

Both verdicts require the stored tag identity to remain live and unchanged and
the expected base head to equal its peeled commit. A ref transaction verifies
the tag object with the verdict append. Missing tags, changed commits, and
changed annotated objects refuse without writing a verdict. Branch rejection
still accepts a historical expected pair without checking branch drift.

`merge` refuses tag bases without mutation. The terminal integration action is
`close --reason integrated --destination <branch> --commit <sha>` (or
`--patch-id <id>`). It records external integration or verification evidence;
it never moves the tag or performs integration. Abandoned and superseded
closure, edits, and explicit-basis comments remain supported after tag loss.
A replacement tag needs a new review record.
