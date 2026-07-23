#!/usr/bin/env bash
# Refresh the swe-swe npx cache with the freshly built binary.
#
# swe-swe does NOT launch agent-chat through `bin/agent-chat.js` or
# `npm-platforms/`. It launches
#   $SWE_SWE_HOME/npx-cache/@choonkeat/agent-chat-<platform>@<version>/bin/agent-chat
# where <version> comes from the sibling `agent-chat-<platform>.latest` pointer
# file. `npm link` does not touch that cache, so without this step a rebuilt fix
# never reaches a newly started session — it silently keeps running the last
# published binary.
#
# This copies the just-built host-platform binary over the cached copies that
# swe-swe would actually launch: the version named by the `.latest` pointer, and
# the version in package.json. Other cached versions are left alone so an
# intentional old-version session still runs the old code.
#
# Never fatal: a machine without the cache (CI, a plain checkout) is fine.

set -uo pipefail

SWE_SWE_HOME="${SWE_SWE_HOME:-$HOME/.swe-swe}"
CACHE_DIR="$SWE_SWE_HOME/npx-cache/@choonkeat"

if [ ! -d "$CACHE_DIR" ]; then
  echo "refresh-npx-cache: no cache at $CACHE_DIR — skipping"
  exit 0
fi

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "refresh-npx-cache: unsupported OS $(uname -s) — skipping"; exit 0 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=x64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "refresh-npx-cache: unsupported arch $(uname -m) — skipping"; exit 0 ;;
esac
platform="$os-$arch"

src="npm-platforms/$platform/bin/agent-chat"
if [ ! -x "$src" ]; then
  echo "refresh-npx-cache: $src not built — skipping"
  exit 0
fi

versions=""
pointer="$CACHE_DIR/agent-chat-$platform.latest"
[ -f "$pointer" ] && versions="$(tr -d '[:space:]' <"$pointer")"
pkg_version="$(node -e 'process.stdout.write(require("./package.json").version)' 2>/dev/null)"
[ -n "$pkg_version" ] && versions="$versions $pkg_version"

copied=0
for v in $(printf '%s\n' $versions | sort -u); do
  dest="$CACHE_DIR/agent-chat-$platform@$v/bin/agent-chat"
  [ -f "$dest" ] || continue
  # A running session is executing this exact file, so a plain `cp` onto it
  # fails with ETXTBSY ("Text file busy") — which is precisely the case this
  # script exists for. Rename the old inode aside first: the running process
  # keeps it open and unaffected, and the new binary lands under the name the
  # next session will launch.
  if [ -e "$dest" ] && ! mv -f "$dest" "$dest.prev"; then
    echo "refresh-npx-cache: cannot move aside $dest — skipping"
    continue
  fi
  if cp "$src" "$dest"; then
    chmod +x "$dest"
    echo "refresh-npx-cache: updated $dest"
    copied=$((copied + 1))
  else
    # Put the old binary back rather than leaving no binary at all.
    mv -f "$dest.prev" "$dest" 2>/dev/null
    echo "refresh-npx-cache: copy to $dest failed — original restored"
  fi
done

if [ "$copied" -eq 0 ]; then
  echo "refresh-npx-cache: no matching cached version for $platform — skipping"
else
  echo "refresh-npx-cache: $copied cached binary(ies) refreshed; start a NEW session to pick it up"
fi
exit 0
