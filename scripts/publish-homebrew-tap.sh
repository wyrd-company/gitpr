#!/usr/bin/env bash
# ---
# relationships:
#   references: release
# ---
set -euo pipefail

checksums="${1:?GoReleaser checksums file is required}"
tap_directory="${2:?checked-out tap directory is required}"
version="${3:-$(git describe --tags --exact-match)}"
script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

mkdir -p "$tap_directory/Formula"
bash "$script_directory/render-homebrew-formula.sh" \
  "$version" "$checksums" "$tap_directory/Formula/gitpr.rb"

cd "$tap_directory"
git config user.name "gitpr release bot"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
git add Formula/gitpr.rb
if git diff --cached --quiet; then
  echo "Formula unchanged; checking remote publication."
else
  git commit -m "Brew formula update for gitpr version $version"
fi
# A failed push can leave an unpublished commit in this checkout. Retry it even
# when rendering the same release no longer stages any changes.
git push origin HEAD:main
