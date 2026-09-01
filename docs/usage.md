---
docs: true
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
picker without an ID. The TUI lists records and provides a read-only detail
view; actions use the CLI review and basis flow.

## Legacy records

A record without a `schema` field is a legacy snapshot from the prior model.
gitpr does not read, list, mutate, merge, or delete one: every command that
takes its ID refuses and names this section, and `gitpr list` skips it with a
count.

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

Remove every ref belonging to one record:

```bash
git for-each-ref --format='%(refname)' \
  'refs/gitpr/pr/<id>/*' 'refs/gitpr/index/*/<id>' |
  while read -r ref; do git update-ref -d "$ref"; done
```

Removal is irreversible, and the commits the record pinned become collectable
by Git.
