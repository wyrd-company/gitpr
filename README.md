# gitpr

`gitpr` is a local Go CLI/TUI for reviewing worktree branches as lightweight
pull requests. Review metadata and retained commit identities live in Git refs,
so every worktree and clone can use the same records without a server.

![gitpr local review demo](docs/assets/demo.gif)

## Versioning and release

[Intentional](https://github.com/wyrd-company/intentional) is the version and
release authority for this repo. It is a tag-only release unit: `go.mod`
carries no version and is never rewritten, and the release version lives
entirely in bare `X.Y.Z` Git tags (no `v` prefix).

- `intentional add` records an intent for the next release
- `intentional status` / `intentional plan` show pending intents and the
  computed next version
- `intentional apply` writes the changelog and consumes intents; the harness
  commits the result
- `intentional tag` creates the annotated release tag after that commit lands

Pushing the resulting tag triggers `.github/workflows/release.yml`, which is
unchanged by Intentional adoption. Separately,
[`tagver`](https://github.com/wyrd-company/tagver) stamps the currently tagged
version into `gitpr --version` at build time:

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

## Review model

A pull request tracks a source branch and a base branch. Each approve or reject
operation appends an immutable review event for one exact source/base head pair;
no diff is stored in the record.

Create and review a PR:

```bash
gitpr create --title "Improve validation" --description "Adds boundary checks"
gitpr create --worktree /path/to/worktree --base main --title "Improve validation"
gitpr review <pr-id>
```

### Shell-safe descriptions

`--description` passes its value through the invoking shell. Backticks,
`$()`, and mixed quoting in a multiline body are shell command-substitution
and quoting syntax, not gitpr syntax — the shell can consume or mangle the
text before gitpr ever sees it. For any description with backticks, `$()`,
quotes, or multiple lines, write the body to a file and pass it with
`--description-file` instead. The flag reads the file (or standard input,
with `-`) verbatim: no shell interpretation, no trimming.

```bash
gitpr create --title "Improve validation" --description-file description.txt
cat description.txt | gitpr create --title "Improve validation" --description-file -
```

`--description` and `--description-file` are mutually exclusive.

To correct the title or description on an already-created open PR — for
example after a shell-mangled `--description` produced an empty or corrupted
body — use `gitpr edit` instead of creating a second record:

```bash
gitpr edit <pr-id> --description-file description.txt
gitpr edit <pr-id> --title "Corrected title"
```

`edit` only accepts open PRs; it refuses closed or merged records without
writing anything, and it never changes source/base heads, state, or any
other metadata.

Only one open PR can track a source/base pair. The review YAML
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

A base-worktree refresh or cleanup failure after merge exits non-zero and prints
a repair command; the merge remains complete.

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
gitpr list --all
gitpr show <pr-id>
gitpr comments <pr-id>
```

`list` defaults to open records and accepts `--state open|merged|closed`.
`--reason integrated|superseded|abandoned` requires `--state closed`. `--status`
is a deprecated alias for `--state`. `--all` cannot be combined with a state or
reason filter. `show` output includes events, threads, closure evidence, and a
thread summary. Records gitpr cannot read — legacy snapshots and records written
by a newer gitpr — are skipped with a count.

## TUI

```bash
gitpr tui
```

The list shows open records and toggles to every state. A record opens a
read-only detail view with its branches, state, latest event, thread summary,
and closure evidence. Review, verdict, merge, and comment actions use the CLI.

- `j` / `k`: move
- `Enter`: open the selected record
- `a`: toggle between open and all records
- `Esc`: return to the list
- `q`: quit

## Legacy records

A record without a `schema` field is a legacy snapshot from the prior model.
gitpr does not read, list, mutate, merge, export, or delete one: every command
that takes its ID refuses and carries the commands below, and `gitpr list` skips
it with a count.

There is no migration. Recreate in-flight work with `gitpr create` and remove
the old record by hand. A legacy record is self-describing YAML behind plain
Git refs, so no tool is needed to retrieve it.

Enumerate the legacy records in a repository:

```bash
for ref in $(git for-each-ref --format='%(refname)' 'refs/gitpr/pr/*/meta'); do
  git show "$ref:pr.yaml" | grep -q '^schema:' || echo "$ref"
done
```

Read one completely, and read how it reached its current state:

```bash
git show refs/gitpr/pr/<id>/meta:pr.yaml
git log --patch refs/gitpr/pr/<id>/meta
```

Export the record before removing it, if the evidence is not carried elsewhere:

```bash
git show refs/gitpr/pr/<id>/meta:pr.yaml > <id>.pr.yaml
```

Remove every ref belonging to one record. The first pattern is a
prefix, not a glob, so it also reaches nested event and anchor pins:

```bash
git for-each-ref --format='%(refname)' \
  refs/gitpr/pr/<id> 'refs/gitpr/index/*/<id>' |
  while read -r ref; do git update-ref -d "$ref"; done
```

Removal is irreversible, and the commits the record pinned become collectable
by Git.

## Git ref storage

Branch-based records use:

```text
refs/gitpr/pr/<id>/meta
refs/gitpr/pr/<id>/events/<event-id>/{head,base}
refs/gitpr/pr/<id>/anchors/<thread-id>/{head,base}
refs/gitpr/index/{open,merged,closed}/<id>
refs/gitpr/openpair/<branch-pair-hash>
```

Each record uses a ULID; commands accept a full ID or unique prefix.

## Debugging

```bash
gitpr debug export <pr-id> --to /tmp/gitpr-meta
```

## Development

Run `task uat:setup` to prepare the manual acceptance flow.

```bash
task build
task check
```
