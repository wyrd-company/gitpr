#!/usr/bin/env bash
# ---
# relationships:
#   references: release
# ---
set -euo pipefail

version="${1:?version is required}"
checksums="${2:?GoReleaser checksums file is required}"
output="${3:?output path is required}"
base_url="https://github.com/wyrd-company/gitpr/releases/download/$version"

sha_for() {
  local filename="gitpr_${version}_$1.tar.gz"
  local digest
  digest="$(awk -v filename="$filename" '$2 == filename { print $1 }' "$checksums")"
  if [[ ! "$digest" =~ ^[[:xdigit:]]{64}$ ]]; then
    echo "Missing or invalid checksum for $filename" >&2
    exit 1
  fi
  printf '%s' "$digest"
}

mac_arm="$(sha_for darwin_arm64)"
mac_intel="$(sha_for darwin_x86_64)"
linux_arm="$(sha_for linux_arm64)"
linux_intel="$(sha_for linux_x86_64)"

cat > "$output" <<FORMULA
class Gitpr < Formula
  desc "Review local git worktree branches as lightweight PRs"
  homepage "https://github.com/wyrd-company/gitpr"

  on_macos do
    on_arm do
      url "$base_url/gitpr_${version}_darwin_arm64.tar.gz"
      sha256 "$mac_arm"
    end
    on_intel do
      url "$base_url/gitpr_${version}_darwin_x86_64.tar.gz"
      sha256 "$mac_intel"
    end
  end

  on_linux do
    on_arm do
      url "$base_url/gitpr_${version}_linux_arm64.tar.gz"
      sha256 "$linux_arm"
    end
    on_intel do
      url "$base_url/gitpr_${version}_linux_x86_64.tar.gz"
      sha256 "$linux_intel"
    end
  end

  def install
    bin.install "gitpr"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gitpr --version")
  end
end
FORMULA
