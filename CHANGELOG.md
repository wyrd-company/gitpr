# Changelog

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
