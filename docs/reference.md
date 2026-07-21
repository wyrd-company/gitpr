---
docs: true
title: Command reference
order: 4
---

| Command | Description |
| --- | --- |
| `gitpr create` | Snapshot the current worktree branch as a new PR (`--title` required) |
| `gitpr list` | List PRs, filtered by `--status` (open by default) |
| `gitpr show [id]` | Print a PR as YAML; opens a picker when no ID is given |
| `gitpr comments [id]` | Print only a PR's review comments; opens a picker when no ID is given |
| `gitpr comment <id>` | Add a review comment to an open PR |
| `gitpr refresh <id>` | Recompute merge-conflict metadata for an open PR |
| `gitpr reject <id>` | Close an open PR as rejected (alias `request-changes`) |
| `gitpr merge <id>` | Merge an open PR into its base branch and mark it approved (alias `approve`, `--cleanup` optional) |
| `gitpr debug export <id>` | Export a PR ref tree (`--ref meta\|head\|base`) to a directory |
| `gitpr tui` | Launch the PR review TUI |
