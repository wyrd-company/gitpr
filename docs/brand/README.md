# Brand sources

The gitpr mark is a Git ref tag that holds a check: review records live in
Git refs, not on a server.

| File | Use |
| --- | --- |
| `../user/assets/logo.svg` | Mark for light backgrounds. Published to wyrd.foo. |
| `../user/assets/logo-dark.svg` | Mark for dark backgrounds. Published to wyrd.foo. |
| `../user/assets/favicon.svg` | Favicon. Follows the browser color scheme. Published to wyrd.foo. |
| `social-image.svg` | Source of `../user/assets/social-image.png` (1200×630), the wyrd.foo social image. |
| `social-preview.svg` | Source of `social-preview.png` (1280×640), the GitHub repository social preview. |

The marks follow the product-mark rules in the wyrd.foo `DESIGN.md`: a square
`primary` tile with a corner radius of one seventh of its side.

Run `task brand:render` to render the PNGs from their sources. It uses
`agent-browser` so the Geist fonts load.
