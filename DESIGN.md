---
version: alpha
name: gitpr
description: >-
  The visual identity of gitpr: a Git ref tag that holds a check on a blue
  wyrd.foo product tile, a dark social image, and a terminal UI that uses the
  terminal's own font and a small ANSI 256-color palette. Colour tokens
  without a suffix are the dark theme; tokens with `-light` are the light
  theme.
colors:
  primary: "#6aa5ff"
  primary-light: "#0b4fa8"
  on-primary: "#0b0b0c"
  on-primary-light: "#ffffff"
  page: "#0b0b0c"
  text: "#ededef"
  text-faint: "#8e8e97"
  header-line: "#3b6fc4"
  tui-title: "#5fffd7"
  tui-cursor: "#ff87d7"
  tui-muted: "#808080"
  tui-info: "#87d7ff"
  tui-error: "#ff5f5f"
typography:
  product-name:
    fontFamily: Geist Mono
    fontSize: 151px
    fontWeight: 500
    lineHeight: 1
    letterSpacing: -0.04em
  tagline:
    fontFamily: Geist
    fontSize: 44px
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: -0.02em
  footnote:
    fontFamily: Geist
    fontSize: 25px
    fontWeight: 400
    lineHeight: 1.3
  terminal:
    fontFamily: monospace
    fontSize: 1em
    fontWeight: 400
    lineHeight: 1
  terminal-strong:
    fontFamily: monospace
    fontSize: 1em
    fontWeight: 700
    lineHeight: 1
rounded:
  mark: 8px
spacing:
  mark-grid: 56px
  social-margin: 96px
  tui-gap: 1em
components:
  mark:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    rounded: "{rounded.mark}"
    size: "{spacing.mark-grid}"
  mark-light:
    backgroundColor: "{colors.primary-light}"
    textColor: "{colors.on-primary-light}"
    rounded: "{rounded.mark}"
    size: "{spacing.mark-grid}"
  social-image:
    backgroundColor: "{colors.page}"
    textColor: "{colors.text}"
    width: 1200px
    height: 630px
    padding: "{spacing.social-margin}"
  social-image-footnote:
    textColor: "{colors.text-faint}"
    typography: "{typography.footnote}"
  social-image-rule:
    backgroundColor: "{colors.header-line}"
    height: 6px
  tui-title:
    textColor: "{colors.tui-title}"
    typography: "{typography.terminal-strong}"
  tui-row-selected:
    textColor: "{colors.tui-cursor}"
    typography: "{typography.terminal-strong}"
  tui-row:
    typography: "{typography.terminal}"
  tui-help:
    textColor: "{colors.tui-muted}"
    typography: "{typography.terminal}"
  tui-info:
    textColor: "{colors.tui-info}"
    typography: "{typography.terminal}"
  tui-error:
    textColor: "{colors.tui-error}"
    typography: "{typography.terminal}"
---

# gitpr design system

## Overview

gitpr reviews worktree branches as local pull requests. Its review records
live in Git refs, with no server. The mark says that: a Git ref tag that
holds a check.

gitpr is a Wyrd Tools product. Its mark, favicon and social image follow the
wyrd.foo design system (`DESIGN.md` in `wyrd-company/wyrd.foo`): one blue
accent, Geist type, a dark-first page. This file owns only what is gitpr's
own: the mark's drawing, the social image layout and the terminal UI.

Two surfaces carry the identity:

- **Brand surfaces.** The mark in the README, on wyrd.foo and in browser
  tabs, and the social image for link previews.
- **Terminal UI.** `gitpr` with no arguments. It renders in the user's
  terminal, so it uses the terminal's font and background and adds colour
  only for meaning.

## Colors

- **Primary (`primary`, `primary-light`).** The wyrd.foo accent blue. It is
  the tile fill of the mark, and the colour of the check and the ref hole cut
  into the tag. `primary` is for dark backgrounds; `primary-light` is the
  darker blue for light backgrounds.
- **On primary (`on-primary`, `on-primary-light`).** The tag drawn on the
  tile: near-black on `primary`, white on `primary-light`.
- **Page, text, text faint, header line.** The social image only. They are
  the wyrd.foo dark theme values.
- **Terminal palette (`tui-*`).** ANSI 256-color indexes, given here as their
  xterm hex values: title 86, selected row 212, muted 244, info 117 and
  error 203. The terminal theme decides the final colour.

## Typography

- **Product name.** `gitpr` in Geist Mono 500 with tight tracking, as on
  wyrd.foo. The name is always lower case.
- **Tagline and footnote.** Geist. The social image tagline is the
  `tagline` in `docs/user/docs.yml`.
- **Terminal.** The terminal's own monospace font. Bold is the only weight
  change: titles and the selected row.

The sizes given for `product-name`, `tagline` and `footnote` are the
1200×630 social image values.

## Layout

- **Mark grid.** The mark is drawn on a 56×56 grid. The tag sits centered
  inside it, with its point to the right.
- **Social image.** Left aligned on a margin of 8% of the width. The mark and
  `gitpr` share a row, the tagline sits below them and the footnote near the
  bottom. A 6px `header-line` rule runs along the bottom edge. The GitHub
  social preview (1280×640) uses the same layout, scaled to its size.
- **Terminal UI.** Plain lines. A title, one blank line, the content, one
  blank line and a muted key help line. Messages go after the help line.
  List columns are fixed width, so rows align.

## Elevation & Depth

None. Every surface is flat: no shadows, gradients or layers.

## Shapes

- **Tile.** A square with a corner radius of one seventh of its side
  (`rounded.mark` on the 56 grid), the wyrd.foo product-mark rule.
- **Tag.** A Git ref tag: a rectangle with a pointed right end and slightly
  rounded joins. A round hole near the left end, and a check stroke with
  round caps.

## Components

- **Mark (`mark`, `mark-light`).** `docs/user/assets/logo-dark.svg` and
  `logo.svg`. Use `mark` on dark backgrounds and `mark-light` on light
  backgrounds. The README switches between them with `prefers-color-scheme`.
- **Favicon.** `docs/user/assets/favicon.svg`. One file that switches
  between the two marks with `prefers-color-scheme`.
- **Social image (`social-image`).** `docs/user/assets/social-image.png`,
  published to wyrd.foo. Its source and the GitHub social preview are in
  `docs/brand/`; `task brand:render` renders both.
- **Terminal title (`tui-title`).** The screen title: `Open PRs`, `All PRs`
  or `PR <id>: <title>`.
- **Terminal rows (`tui-row`, `tui-row-selected`).** One pull request per
  row. The selected row starts with `>` and takes the cursor colour.
- **Terminal help (`tui-help`).** The key help line and empty states.
- **Terminal messages (`tui-info`, `tui-error`).** Guidance after an action,
  and errors.

## Do's and Don'ts

- Do use `mark` on dark backgrounds and `mark-light` on light ones; never
  place the blue tile on a blue background.
- Do keep the tile corner radius at one seventh of the side at every size.
- Don't add text, outlines or effects to the mark, or rotate the tag.
- Don't render the mark below 16px; the check and the hole merge.
- Do write the product name as `gitpr`, in lower case.
- Do keep colour in the terminal UI for meaning only, and keep every screen
  readable with colour off.
- Don't set a background colour in the terminal UI; the terminal owns it.
