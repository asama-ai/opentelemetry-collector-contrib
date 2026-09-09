#!/usr/bin/env bash
set -euo pipefail

: "${RPM_GPG_PRIVATE_KEY:?RPM_GPG_PRIVATE_KEY is required}"
install -d -m 700 ~/.gnupg
printf '%s\n' "$RPM_GPG_PRIVATE_KEY" | gpg --batch --import
pubkey="$(mktemp)"
trap 'rm -f "$pubkey"' EXIT
gpg --armor --export > "$pubkey"
test -s "$pubkey"
sudo rpm --import "$pubkey"
key_id=$(gpg --list-secret-keys --keyid-format LONG 2>/dev/null | awk '/^sec/{split($2,a,"/"); print a[2]; exit}')
: "${key_id:=${RPM_GPG_NAME:-}}"
: "${key_id:?Unable to determine RPM signing key ID}"
printf '%%_signature gpg\n%%_gpg_name %s\n%%_gpg_path %s/.gnupg\n' "$key_id" "$HOME" > ~/.rpmmacros
