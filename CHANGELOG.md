# Changelog

## 0.4.2

### Features

- Read comment text verbatim from a file or standard input with --text-file, preserving whitespace and shell-sensitive text.
- Add gitpr review --summary for live review state and bounded interdiff file counts without patch or comment bodies.
- Support lightweight and annotated tag review bases with preserved tag identity, atomic drift checks on verdicts, and external integration closure without changing tags.

### Fixes

- Use adaptive brand blue accents across the TUI and picker, respect colour-off terminals, and refresh the demo.
- Keep terminal navigation, read-only review, selection and adaptive colour working with Charm v2.
- Build development binaries into the gitignored ./bin directory and use ./bin/gitpr in source-build instructions and UAT helpers.
- Update ANSI handling and Git diff parsing dependencies, and remove unused syntax highlighting dependencies.
- Restore the read-only TUI diff with live review basis, per-file hunks, thread discussions, scrolling, and refresh.
- Refuse missing detected bases before creating a PR, with explicit-base recovery
  guidance, and preserve full remote default branch names.

## 0.4.1

### Fixes

- Generate Homebrew formulae that pass strict audit and style checks.
- Publish the public docs from docs/user in the wyrd.foo format, and point the legacy-record messages at docs/user/usage.md.

## 0.4.0

### Breaking

- Pull requests are branch-scoped records with immutable accepted or rejected review events, expected-head verdicts, ancestry-gated merges, closure reasons, and durable resolvable comment threads.
- Remove the legacy snapshot record model. Legacy records are refused with inlined raw-git retrieval and removal guidance. Remove the approved and rejected filters, refresh, and comment --update and --commit flags.

### Features

- Write descriptions from a file with 'create --description-file <path|->', preserving bytes exactly and refusing a blank path. Add a new open-only 'gitpr edit' command for correcting a title or description.

### Fixes

- Re-record the demo for the branch-based pull request flow.
- The test task now fails the build when Go files are not gofmt-formatted.

Earlier releases: see git tags.
