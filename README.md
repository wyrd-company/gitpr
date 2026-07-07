# gitpr

`gitpr` is a local Go CLI/TUI for reviewing worktree branches as lightweight pull requests against the local default branch.

## What It Does

- Stores PR snapshots in Git refs under `refs/gitpr/...`
- Lists open and closed PRs from the command line
- Shows full PR YAML or just review comments
- Opens a TUI for human review with a side-by-side diff view
- Saves comments and merge conflicts back into the PR metadata
- Merges approved work into the default branch
- Exports PR refs into a directory for debugging and UAT

## Git Ref Layout

`gitpr` stores PR data in Git refs, so linked worktrees and separate clones can use shared PR metadata without a `.prs` working-tree directory.

- `refs/gitpr/pr/<ulid>/meta`
- `refs/gitpr/pr/<ulid>/head`
- `refs/gitpr/pr/<ulid>/base`
- `refs/gitpr/index/open/<ulid>`
- `refs/gitpr/index/approved/<ulid>`
- `refs/gitpr/index/rejected/<ulid>`
- `refs/gitpr/config/meta`

Each PR gets a ULID identifier. The CLI accepts the full ID or a unique prefix. The list output and TUI show a shortened prefix.

## Versioning

This repo uses [`tagver`](https://github.com/wyrd-company/tagver) for tag-driven version calculation.

- Release tags should use the `vX.Y.Z` format
- `task build` embeds the current calculated version into `gitpr --version` when `tagver` is installed
- The release workflow validates that the pushed tag matches the `tagver`-calculated version before publishing

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

## Commands

Build:

```bash
task build
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

Create a PR from the current worktree:

```bash
gitpr create --title "Add diff viewer" --description "Initial reviewable version"
```

Create a PR from another worktree path:

```bash
gitpr create --worktree /path/to/worktree --title "Fix merge handling"
```

List PRs:

```bash
gitpr list --status open
gitpr list --status closed
gitpr list --status approved
gitpr list --status rejected
gitpr list --status all
```

Show a full PR:

```bash
gitpr show 01K0ABCDEFGH
```

Open an interactive picker and then show the selected PR:

```bash
gitpr show
gitpr show --status open
gitpr show --status approved
```

Show only comments:

```bash
gitpr comments 01K0ABCDEFGH
```

Open an interactive picker and then show comments for the selected PR:

```bash
gitpr comments
gitpr comments --status open
gitpr comments --status approved
```

Add a comment from the CLI:

```bash
gitpr comment 01K0ABCDEFGH \
  --file internal/tui/tui.go \
  --line-start 120 \
  --line-end 126 \
  --text "Please handle the empty state here." \
  --commit abc1234
```

Refresh merge-conflict metadata without opening the TUI:

```bash
gitpr refresh 01K0ABCDEFGH
```

Request changes from the CLI:

```bash
gitpr reject 01K0ABCDEFGH
# or
gitpr request-changes 01K0ABCDEFGH
```

Merge and mark approved from the CLI:

```bash
gitpr merge 01K0ABCDEFGH
# or
gitpr approve 01K0ABCDEFGH

# Remove the source worktree and branch after merge:
gitpr merge 01K0ABCDEFGH --cleanup
```

Launch the TUI:

```bash
gitpr tui
```

Export a PR ref for debugging:

```bash
gitpr debug export 01K0ABCDEFGH --ref meta --to /tmp/gitpr-meta
gitpr debug export 01K0ABCDEFGH --ref head --to /tmp/gitpr-head
gitpr debug export 01K0ABCDEFGH --ref base --to /tmp/gitpr-base
```

## TUI Keys

- `j` / `k`: move
- `Enter`: open selected PR from the list
- `Esc`: go back to the PR list
- `v`: start or clear a block selection in the diff
- `c`: add or edit a comment on the current line or selected block
- `o`: expand or collapse inline comments for the current line or selected block
- `r`: request changes and mark the PR as `rejected`
- `m`: merge if there are no merge conflicts
- `q`: quit

When editing a comment in the TUI, `Enter` inserts a newline, `Ctrl+S` saves, and `Esc` cancels.

When merging from the TUI, the app asks whether the source worktree should also be cleaned up.

## PR Lifecycle

- `open`: reviewable and visible in the TUI
- `approved`: merged and indexed as approved
- `rejected`: request-changes outcome, indexed as rejected

This version uses the simple lifecycle: once a PR is rejected, it is closed. A new revision should be submitted as a new PR.

## YAML Shape

Example:

```yaml
id: 01K0ABCDEFGHJKMNPQRSTVWXYZ
title: Add review TUI
source_branch: feature/review-tui
source_worktree_path: /repo-worktrees/review-tui
repository_root: /repo
base_branch: main
source_head_sha: abcdef1234567890abcdef1234567890abcdef12
base_head_sha: 1234567890abcdef1234567890abcdef12345678
merge_base_sha: ffffffffffffffffffffffffffffffffffffffff
description: Initial implementation of the review experience.
file_diffs:
  - old_path: internal/tui/tui.go
    new_path: internal/tui/tui.go
    status: modified
    patch: |
      diff --git a/internal/tui/tui.go b/internal/tui/tui.go
      ...
    hunks:
      - header: ""
        old_start: 40
        old_lines: 3
        new_start: 40
        new_lines: 6
        lines:
          - kind: context
            old_line: 40
            new_line: 40
            content: existing code
          - kind: add
            new_line: 41
            content: new code
commits:
  - sha: abcdef1234567890
    message: Add review TUI
comments:
  - file_path: internal/tui/tui.go
    line_start: 41
    line_end: 43
    comment: Please split this state transition out.
    commit_sha: abcdef1234567890
    created_at: 2026-04-25T00:00:00Z
merge_conflicts:
  - path: internal/tui/tui.go
    message: "CONFLICT (content): Merge conflict in internal/tui/tui.go"
status: open
created_at: 2026-04-25T00:00:00Z
updated_at: 2026-04-25T00:00:00Z
closed_at: 2026-04-25T00:05:00Z
```

## Notes

- Default branch detection prefers `origin/HEAD`, then `main`, then `master`
- Merge conflict detection is saved into the PR YAML
- Merge writes directly into the local base branch using the reviewed `source_head_sha`
- `debug export` is the intended way to inspect ref-backed state on disk

## UAT

Recommended user acceptance test flow:

1. Build the binary.

```bash
task build
```

Or use:

```bash
task build
task uat:setup
```

2. Create a throwaway repo and feature worktree.

```bash
mkdir /tmp/gitpr-uat && cd /tmp/gitpr-uat
git init -b main
git config user.name tester
git config user.email tester@example.com
printf "base\n" > app.txt
git add app.txt
git commit -m "base"
git worktree add -b feature ../gitpr-uat-feature HEAD
cd ../gitpr-uat-feature
printf "feature\n" >> app.txt
git add app.txt
git commit -m "feature change"
```

3. Create a PR and inspect it.

```bash
/workspaces/gitpr/gitpr create --title "Feature PR" --description "UAT run"
/workspaces/gitpr/gitpr list --status open
/workspaces/gitpr/gitpr show <pr-id-prefix>
```

4. Add comments both ways.

```bash
/workspaces/gitpr/gitpr comment <pr-id-prefix> --file app.txt --line-start 2 --text "Looks good, but rename this."
/workspaces/gitpr/gitpr comments <pr-id-prefix>
/workspaces/gitpr/gitpr tui
```

5. Verify debug export.

```bash
/workspaces/gitpr/gitpr debug export <pr-id-prefix> --ref meta --to /tmp/gitpr-meta
find /tmp/gitpr-meta -maxdepth 2 -type f
```

6. Verify merge path.

From the TUI, merge the PR and choose whether to clean up the source worktree. Then confirm:

```bash
/workspaces/gitpr/gitpr list --status approved
git log --oneline --graph --decorate --all
git for-each-ref "refs/gitpr/*"
```

7. Verify conflict blocking.

Create another feature branch, change the same line on `main` and on the feature branch, then:

```bash
/workspaces/gitpr/gitpr create --title "Conflict PR"
/workspaces/gitpr/gitpr show <pr-id-prefix>
```

The PR YAML should contain `merge_conflicts`, and the TUI should refuse merge until conflicts are gone.
