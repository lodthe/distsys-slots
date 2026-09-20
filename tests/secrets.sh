#!/bin/sh
set -eu
cd "$(dirname "$0")/.."

# Scan publishable working files, including untracked files. Tracked files are
# included even if an ignore rule was added after their first commit.
git rev-parse --is-inside-work-tree > /dev/null
scan_dir=$(mktemp -d)
trap 'rm -rf "$scan_dir"' EXIT
mkdir "$scan_dir/source"
git ls-files -z --cached --others --exclude-standard > "$scan_dir/files"
tar --null -T "$scan_dir/files" -cf "$scan_dir/source.tar"
tar -xf "$scan_dir/source.tar" -C "$scan_dir/source"
bin/gitleaks dir --redact --no-banner "$scan_dir/source"
# The index may still contain a secret that has already been removed from the
# working copy. Check exactly what the next commit would contain as well.
mkdir "$scan_dir/index"
git checkout-index --all --prefix="$scan_dir/index/"
bin/gitleaks dir --redact --no-banner "$scan_dir/index"
if git rev-parse --verify HEAD > /dev/null 2>&1; then
    bin/gitleaks git --redact --no-banner --log-opts="--all" .
fi
