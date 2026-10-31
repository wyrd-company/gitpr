# Changelog

## 0.1.2

### Fixes

- Use adaptive brand blue accents across the TUI and picker, respect colour-off terminals, and refresh the demo.
- Keep terminal navigation, read-only review, selection and adaptive colour working with Charm v2.
- Read comment text verbatim from a file or standard input with --text-file, preserving whitespace and shell-sensitive text.
- Add gitpr review --summary for live review state and bounded interdiff file counts without patch or comment bodies.
- Support lightweight and annotated tag review bases with preserved tag identity, atomic drift checks on verdicts, and external integration closure without changing tags.
- Build development binaries into the gitignored ./bin directory and use ./bin/gitpr in source-build instructions and UAT helpers.
- Restore the read-only TUI diff with live review basis, per-file hunks, thread discussions, scrolling, and refresh.

## 0.1.1

### Fixes

- Publish the public docs from docs/user in the wyrd.foo format, and point the legacy-record messages at docs/user/usage.md.
- Add the gitpr logo, favicon and social image to the published docs.
