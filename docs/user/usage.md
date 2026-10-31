---
product: gitpr
title: Usage
order: 3
---

## Review flow

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

For a compact state check, use:

```bash
gitpr review <pr-id> --summary
```

This keeps the live heads, containment, latest verdict, and projected thread
state. It omits patch and comment bodies. With a prior verdict, it reports
fixed interdiff file counts from blob IDs and modes. Each compared diff
represents renames as deletions and additions. Use the default review command to inspect code changes.

## Base selection

`gitpr create --base <branch>` selects the base explicitly. Without `--base`,
create uses the saved repository default, then `refs/remotes/origin/HEAD`, then
local `main` or `master`. Remote branch names retain their full path, such as
`epic/current`. Create does not infer the branch from which a worktree branched.

A detected base must be a local branch. If the saved default was deleted, or
origin’s default has no corresponding local branch, create fails with the
detected name and `--base` guidance before writing configuration or a PR record.
It does not silently select another base. Retry with an existing base:

```bash
gitpr create --base main --title "Improve validation"
```

A successful create saves the selected base as the repository default. For epic
child branches, pass the intended epic branch with `--base`.

## Descriptions

`--description` passes its value through the invoking shell, so backticks,
`$()`, and mixed quoting in a multiline body can be consumed or mangled by the
shell before gitpr sees them. Use `--description-file <path>` (or `-` for
standard input) instead; it is read verbatim, byte-for-byte, with no shell
interpretation or trimming, and is mutually exclusive with `--description`:

```bash
gitpr create --title "Improve validation" --description-file description.txt
```

To correct title or description on an open PR without creating a second
record — for example after a shell-mangled description — use `gitpr edit`.
It refuses closed or merged PRs without writing, and never changes anything
but the fields given:

```bash
gitpr edit <pr-id> --description-file description.txt
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

Use `--text-file <path>` for multiline comments or text with backticks, `$()`,
or mixed quotes. Use `--text-file -` to read standard input. Both preserve the
body byte-for-byte, including leading and trailing whitespace. The path must
be nonblank; the body must contain non-whitespace text. `--text-file` and
`--text` are mutually exclusive. These inputs work for anchored comments,
PR-level comments, and replies:

```bash
gitpr comment <pr-id> --pr-level --text-file comment.txt
gitpr comment <pr-id> --thread <thread-id> --text-file - <<'COMMENT'
Keep `value` and $(expression) literal.

  Preserve this indentation.
COMMENT
```

The CLI accepts a full ULID or a unique prefix. `show` and `comments` open a
picker without an ID. The TUI lists records and provides a read-only detail
view. Press `d` in detail to read the live review report: per-file hunks with
old/new line numbers, source/base heads, merge base, and existing threads with
anchors, comments, resolution status, and outdated state. Use `j`/`k` or arrow
keys to scroll, `PgUp`/`PgDn` to page, `g`/`G` for start/end, and `R` to refresh.
`Esc` returns to detail, then to the list. Actions use the CLI review and basis
flow; the diff does not write verdicts, merges, or comments.

If a branch is missing, the diff reports it instead of showing an empty change.
Live hunks and remapped threads are unavailable in that state; detail still
shows the stored thread counts. A load or refresh error appears in the current
screen; a failed refresh retains the last successfully loaded report and its
basis. Only the latest requested report can update the view. Returning with
`Esc` discards pending responses, so a late refresh does not reopen the diff.

The terminal UI and PR picker adapt to the terminal background and honour
`NO_COLOR` and the terminal colour capability. Terminals without background
query support can supply `COLORFGBG` (for example, `15;0` for dark or `0;15`
for light). Quit or cancel restores the terminal screen.

## Legacy records

A record without a `schema` field is a legacy snapshot from the prior model.
gitpr does not read, list, mutate, merge, export, or delete one: every command
that takes its ID refuses and carries the commands below, and `gitpr list` skips
it with a count.

There is no migration. Recreate in-flight work with `gitpr create` and remove
the old record by hand. A legacy record is self-describing YAML behind plain
Git refs, so no tool is needed to retrieve it.

List the legacy records in a repository:

```bash
for ref in $(git for-each-ref --format='%(refname)' 'refs/gitpr/pr/*/meta'); do
  git show "$ref:pr.yaml" | grep -q '^schema:' || echo "$ref"
done
```

Read one, read its history, and export it before removal:

```bash
git show refs/gitpr/pr/<id>/meta:pr.yaml
git log --patch refs/gitpr/pr/<id>/meta
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

## Review against an immutable tag

```bash
gitpr create --title "Release review" --base refs/tags/1.2.3
gitpr review <id>
gitpr approve <id> --basis <source-head>:<base-head>
gitpr close <id> --reason integrated --destination <branch> --commit <sha>
```

Lightweight and annotated tags are supported when they peel to a commit.
Use `refs/tags/<name>` to choose a tag when a branch has the same name;
unqualified names prefer branches. A tag review does not set the default branch.
The review output includes `base_tag.ref`, `base_tag.object_sha`, and
`base_tag.commit_sha`; `basis.base_head_sha` is the peeled commit.

Approve and reject require the original tag ref and object to remain unchanged.
Replacing an annotation counts as drift even if the commit stays the same.
Deletion or drift refuses the verdict without changing review refs. Create a
new PR to review the replacement tag. Original objects remain pinned until
record deletion. Explicit-basis comments, metadata edits, and closure remain
available after tag loss; live-basis review and comments report the missing or
changed tag.

`gitpr merge` refuses a tag base, including with `--cleanup`. Tags are never
integration destinations. Close as `integrated` to record external integration
or verification, with a destination branch and resulting commits or patch IDs.
Closure records evidence; it does not perform or prove external integration,
move refs, or remove worktrees. An accepted event records approval separately.
You can also close as abandoned or superseded.
