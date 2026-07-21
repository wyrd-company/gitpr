---
docs: true
title: Overview
order: 1
---

gitpr reviews branches the way a forge does, but entirely inside your local
repository. PR snapshots live in Git refs under `refs/gitpr/…`, so linked
worktrees and separate clones share the same review metadata without a
working-tree directory.

Each PR gets a ULID identifier. The CLI accepts the full ID or any unique
prefix; list output and the TUI show a shortened prefix.

Because everything is a Git ref, review state travels with the repository and is
visible to every worktree and clone — no server, no remote, no forge.

![gitpr local review demo](assets/demo.gif)

```text
refs/gitpr/pr/<ulid>/meta
refs/gitpr/pr/<ulid>/head
refs/gitpr/pr/<ulid>/base
refs/gitpr/index/open/<ulid>
refs/gitpr/index/approved/<ulid>
```
