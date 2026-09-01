---
docs: true
title: Usage
order: 3
---

## Branch-based review flow

Create a PR for the current worktree branch, review its live basis, and use the
exact reported pair for a verdict:

```bash
gitpr create --title "Improve validation"
gitpr list
gitpr review <pr-id>
gitpr comment <pr-id> --file internal/example.go --line-start 20 \
  --text "Handle the empty value here."
gitpr approve <pr-id> --basis <source-head>:<base-head>
gitpr merge <pr-id>
```

Approve records an accepted event and does not merge. Merge reads the latest
event and refuses rejected verdicts, head drift, deleted source branches,
divergent histories, and equal source/base heads. Reject records a rejected
event for the reviewed pair:

```bash
gitpr reject <pr-id> --basis <source-head>:<base-head>
```

Comments can be anchored, PR-level, or replies:

```bash
gitpr comment <pr-id> --pr-level --text "Ready for another review."
gitpr comment <pr-id> --thread <thread-id> --text "Addressed."
gitpr resolve <pr-id> <thread-id>
gitpr reopen <pr-id> <thread-id>
```

Comments are allowed while a PR is open, merged, or closed. A post-closure
comment is marked in the record.

End work without merging through gitpr:

```bash
gitpr close <pr-id> --reason abandoned
gitpr close <pr-id> --reason superseded --superseded-by <replacement-id>
gitpr close <pr-id> --reason integrated --destination main \
  --commit <landed-commit-sha>
```

Records remain retained after merge or closure. `gitpr delete <id>` previews the
destructive operation; `--force` performs it and may make pinned commits
collectable.

The CLI accepts a full ULID or a unique prefix. `show` and `comments` open a
picker without an ID. The TUI lists both formats and provides a read-only detail
view for branch-based records; branch-based actions use the CLI review and basis
flow.

## Existing legacy records

Prior-generation records are immutable snapshots without a `schema` field.
Existing snapshots retain their `open`, `approved`, and `rejected` vocabulary
and historical comment, refresh, reject, merge, cleanup, and TUI behavior. No
command creates a legacy snapshot.
