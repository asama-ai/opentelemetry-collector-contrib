#!/usr/bin/env bash
set -euo pipefail

: "${RPM_GPG_PRIVATE_KEY:?RPM_GPG_PRIVATE_KEY is required}"
install -d -m 700 ~/.gnupg
printf '%s\n' "$RPM_GPG_PRIVATE_KEY" | gpg --batch --import
gpg --armor --export > /tmp/rpm-signing-key.pub
test -s /tmp/rpm-signing-key.pub
sudo rpm --import /tmp/rpm-signing-key.pub
key_id=$(gpg --list-secret-keys --keyid-format LONG 2>/dev/null | awk '/^sec/{split($2,a,"/"); print a[2]; exit}')
: "${key_id:=${RPM_GPG_NAME:-}}"
: "${key_id:?Unable to determine RPM signing key ID}"
printf '%%_signature gpg\n%%_gpg_name %s\n%%_gpg_path ~/.gnupg\n' "$key_id" > ~/.rpmmacros
