---
docs: true
title: Usage
order: 3
---

A review moves through four steps: snapshot a branch, list what is open, review
it in the TUI, and merge once it is approved.

1. Create a PR from your current worktree branch against the local default
   branch. A `--title` is required.
2. List open PRs to grab a ULID prefix.
3. Review the change: launch the TUI for a side-by-side diff and inline
   comments, or inspect it from the CLI with `show` and `comments`.
4. Merge the approved work back into its base branch. Add `--cleanup` to also
   remove the source worktree and branch.

```bash
gitpr create --title "Add diff viewer"
gitpr list
gitpr show <ulid-prefix>
gitpr tui
gitpr merge <ulid-prefix>
```

The CLI accepts a full ULID or any unique prefix. Commands that take a PR ID but
are given none — `show` and `comments` — open an interactive picker instead.
