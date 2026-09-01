# gitpr

`gitpr` is a local Go CLI/TUI for reviewing worktree branches as lightweight
pull requests. Review metadata and retained commit identities live in Git refs,
so every worktree and clone can use the same records without a server.

![gitpr local review demo](docs/assets/demo.gif)

## Versioning

This repo uses [`tagver`](https://github.com/wyrd-company/tagver) for tag-driven
version calculation.

- Release tags should use the `vX.Y.Z` format
- `task build` embeds the current calculated version into `gitpr --version` when
  `tagver` is installed
- The release workflow validates that the pushed tag matches the
  `tagver`-calculated version before publishing

Helpful commands:

```bash
task version
task version:json
```

## Installation

Build from source locally:

```bash
task build
gitpr --version
```

Install with Go:

```bash
go install github.com/wyrd-company/gitpr/cmd/gitpr@latest
```

Release builds are published to GitHub Releases.

Homebrew is wired for the `wyrd-company/homebrew-tools` repository:

```bash
brew tap wyrd-company/tools
brew install gitpr
```

Taskfile shortcuts:

```bash
task build
task test
task version
task version:json
task release
task release:check
task release:snapshot
task uat
task uat:setup
task uat:clean
task uat:paths
task uat:reset
```

## Branch-based review

A pull request tracks a source branch and a base branch. Each approve or reject
operation appends an immutable review event for one exact source/base head pair;
no diff is stored in the record.

Create and review a PR:

```bash
gitpr create --title "Improve validation" --description "Adds boundary checks"
gitpr create --worktree /path/to/worktree --base main --title "Improve validation"
gitpr review <pr-id>
```

Only one open branch-based PR can track a source/base pair. The review YAML
contains a machine-readable `basis`, the live diff and base containment,
projected thread state, the latest event, and an interdiff when a prior event
exists. Review performs no writes.

Record a verdict with the exact reviewed basis:

```bash
gitpr approve <pr-id> --basis <source-head>:<base-head>
gitpr reject <pr-id> --basis <source-head>:<base-head>
gitpr approve <pr-id> --source-head <source-head> --base-head <base-head>
```

Both heads are mandatory full 40-character lowercase commit IDs. Approve refuses
live source or base drift. Reject records the supplied existing pair even if
branches moved. A verdict does not merge or change PR state.

Merge the latest accepted event:

```bash
gitpr merge <pr-id>
gitpr merge <pr-id> --cleanup
```

Merge requires exact live heads and strict fast-forward ancestry. The base ref,
metadata, state index, and open-pair ownership change in one transaction.
`--cleanup` removes the recorded source worktree and source branch after
success.

A branch-based base-worktree refresh or cleanup failure after merge exits
non-zero and prints a repair command; the merge remains complete. Legacy records
retain their historical exit-zero cleanup behavior.

## Comment threads

Create an anchored or PR-level thread, reply, and manage its state:

```bash
gitpr comment <pr-id> --file internal/example.go --line-start 20 \
  --line-end 24 --side new --text "Handle the empty value here."
gitpr comment <pr-id> --pr-level --text "Ready for another review."
gitpr comment <pr-id> --thread <thread-id> --text "Updated."
gitpr resolve <pr-id> <thread-id>
gitpr reopen <pr-id> <thread-id>
gitpr comments <pr-id>
```

`--side` accepts `new` by default or `old`. Omit head flags to use live branch
heads, or pass `--basis` or both head flags. PR-level threads refuse file, line,
and side flags. Resolved threads accept replies. Comments are legal after merge
or closure and carry a post-closure marker. Anchored ranges move with unchanged
content; changed or unmappable ranges remain visible as outdated.

## Closure and retention

Close an open branch-based PR without changing branches or worktrees:

```bash
gitpr close <pr-id> --reason abandoned --note "Not proceeding"
gitpr close <pr-id> --reason superseded --superseded-by <replacement-pr-id>
gitpr close <pr-id> --reason integrated --destination main \
  --commit <landed-commit-sha>
gitpr close <pr-id> --reason integrated --destination main \
  --patch-id <patch-equivalent-id>
```

Integrated closure requires a destination and at least one repeated `--commit`
or `--patch-id`. Superseded closure requires another existing branch-based PR.
Evidence flags for other reasons are refused.

Merged and closed records remain retained. Deletion is exceptional:

```bash
gitpr delete <pr-id>          # preview and warning; exits non-zero
gitpr delete <pr-id> --force  # remove metadata, indexes, and retained refs
```

Deleting retained refs may make reviewed commits collectable by Git.

## Listing and inspection

```bash
gitpr list
gitpr list --state closed --reason abandoned
gitpr list --state merged
gitpr list --state approved
gitpr list --all
gitpr show <pr-id>
gitpr comments <pr-id>
```

`list` defaults to open records. Filters are vocabulary-scoped:

| Filter                 | Record format     |
| ---------------------- | ----------------- |
| `open`                 | Both formats      |
| `merged`, `closed`     | Branch-based only |
| `approved`, `rejected` | Legacy only       |

`--reason integrated|superseded|abandoned` requires `--state closed`. `--status`
is a deprecated alias for `--state`. `--all` cannot be combined with a state or
reason filter. Branch-based `show` output includes events, threads, closure
evidence, and a thread summary.

## TUI

```bash
gitpr tui
```

The list displays both record formats. A branch-based record opens a read-only
detail view with its branches, state, latest event, thread summary, and closure
evidence. Branch-based review, verdict, merge, and comment actions use the CLI.

Existing legacy records retain the historical TUI diff and actions:

- `j` / `k`: move
- `Enter`: open the selected record
- `Esc`: return to the list
- `v`: select a diff block
- `c`: cycle or edit legacy inline comments
- `o`: expand or collapse legacy comments
- `r`: reject a legacy snapshot
- `m`: merge a legacy snapshot
- `q`: quit

## Prior-generation snapshot records

Records without a `schema` field are legacy snapshots. No command creates them,
but existing records remain readable and operable with their original YAML shape
and vocabulary.

- `show`, `comments`, `comment`, `reject`, `merge`, and `delete --force`
  dispatch to legacy behavior.
- `refresh` recomputes stored merge-conflict metadata for an open legacy
  snapshot.
- Legacy comment `--update` and `--commit` flags retain their historical
  meaning.
- Legacy states remain `open`, `approved`, and `rejected`; they are not
  reinterpreted.
- Approve is not a merge alias. `gitpr approve` applies only to branch-based
  records.

## Git ref storage

Branch-based records use:

```text
refs/gitpr/pr/<id>/meta
refs/gitpr/pr/<id>/events/<event-id>/{head,base}
refs/gitpr/pr/<id>/anchors/<thread-id>/{head,base}
refs/gitpr/index/{open,merged,closed}/<id>
refs/gitpr/openpair/<branch-pair-hash>
```

Legacy snapshots retain their `meta`, `head`, `base`, and
`open|approved|rejected` index refs. Each record uses a ULID; commands accept a
full ID or unique prefix.

## Debugging

```bash
gitpr debug export <pr-id> --ref meta --to /tmp/gitpr-meta
gitpr debug export <legacy-pr-id> --ref head --to /tmp/gitpr-head
gitpr debug export <legacy-pr-id> --ref base --to /tmp/gitpr-base
```

## Development

Run `task uat:setup` to prepare the manual acceptance flow.

```bash
task build
task check
```
