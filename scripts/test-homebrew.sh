#!/usr/bin/env bash
# ---
# relationships:
#   validates: release
# ---
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
version=1.2.3
checksums="$temporary/checksums.txt"
for target in darwin_arm64 darwin_x86_64 linux_arm64 linux_x86_64; do
  printf '%s' "$target" > "$temporary/gitpr_${version}_$target.tar.gz"
done
(
  cd "$temporary"
  sha256sum ./*.tar.gz | sed 's|  ./|  |' > "$checksums"
)

bash "$root/scripts/render-homebrew-formula.sh" "$version" "$checksums" "$temporary/gitpr.rb"
awk '/^[[:space:]]*url / {url=$2} /^[[:space:]]*sha256 / {print url, $2}' \
  "$temporary/gitpr.rb" > "$temporary/mapping"
while read -r digest filename; do
  if ! grep -Fxq "\"https://github.com/wyrd-company/gitpr/releases/download/$version/$filename\" \"$digest\"" \
    "$temporary/mapping"; then
    echo "FAIL url/checksum mapping: $filename" >&2
    exit 1
  fi
done < "$checksums"
[[ "$(wc -l < "$temporary/mapping")" -eq 4 ]]
if grep -q '^[[:space:]]*version ' "$temporary/gitpr.rb"; then
  echo "FAIL redundant version statement" >&2
  exit 1
fi
grep -Fq 'shell_output("#{bin}/gitpr --version")' "$temporary/gitpr.rb"
echo "PASS platform URL/checksum mapping and version smoke test"

# Missing or corrupt inputs must fail before replacing the existing output.
for fault in missing corrupt; do
  if [[ "$fault" == missing ]]; then
    sed '/linux_x86_64/d' "$checksums" > "$temporary/bad-checksums.txt"
  else
    sed 's/^[^ ]*/invalid/' "$checksums" > "$temporary/bad-checksums.txt"
  fi
  printf sentinel > "$temporary/output.rb"
  if bash "$root/scripts/render-homebrew-formula.sh" \
    "$version" "$temporary/bad-checksums.txt" "$temporary/output.rb" 2> "$temporary/error"; then
    echo "FAIL $fault checksum accepted" >&2
    exit 1
  fi
  [[ "$(cat "$temporary/output.rb")" == sentinel ]]
  grep -q 'Missing or invalid checksum' "$temporary/error"
  echo "PASS $fault checksum rejects before replacing output"
done

# Exercise real Git publication against a disposable local remote, including replay.
git init --quiet --bare --initial-branch=main "$temporary/remote.git"
git clone --quiet "$temporary/remote.git" "$temporary/tap"
bash "$root/scripts/publish-homebrew-tap.sh" "$checksums" "$temporary/tap" "$version"
first="$(git -C "$temporary/remote.git" rev-parse main)"
bash "$root/scripts/publish-homebrew-tap.sh" "$checksums" "$temporary/tap" "$version"
[[ "$(git -C "$temporary/remote.git" rev-parse main)" == "$first" ]]
git -C "$temporary/remote.git" show main:Formula/gitpr.rb > "$temporary/published.rb"
cmp "$temporary/gitpr.rb" "$temporary/published.rb"
echo "PASS publisher writes main and unchanged replay creates no commit"

for recovery in retained fresh; do
  remote="$temporary/recovery-$recovery.git"
  tap="$temporary/recovery-$recovery"
  git init --quiet --bare --initial-branch=main "$remote"
  git clone --quiet "$remote" "$tap"
  cat > "$remote/hooks/pre-receive" <<'REJECT_PUSH'
#!/usr/bin/env bash
echo "synthetic publication outage" >&2
exit 1
REJECT_PUSH
  chmod +x "$remote/hooks/pre-receive"
  if bash "$root/scripts/publish-homebrew-tap.sh" "$checksums" "$tap" "$version" \
    > "$temporary/rejected-push" 2>&1; then
    echo "FAIL $recovery recovery: rejected push reported success" >&2
    exit 1
  fi
  grep -q 'synthetic publication outage' "$temporary/rejected-push"
  rm "$remote/hooks/pre-receive"
  if [[ "$recovery" == fresh ]]; then
    tap="$temporary/fresh-recovery-checkout"
    git clone --quiet "$remote" "$tap"
  fi
  bash "$root/scripts/publish-homebrew-tap.sh" "$checksums" "$tap" "$version"
  if ! git -C "$remote" show main:Formula/gitpr.rb > "$temporary/recovered.rb" 2>/dev/null || \
    ! cmp -s "$temporary/gitpr.rb" "$temporary/recovered.rb"; then
    echo "FAIL $recovery publication recovery did not update main" >&2
    exit 1
  fi
  echo "PASS $recovery checkout recovers publication from release checksums"
done
