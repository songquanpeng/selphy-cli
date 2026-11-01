#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
if pgrep -x selphy-studio >/dev/null; then
  printf '%s\n' '请先退出 SELPHY Studio，再安装更新。' >&2
  exit 1
fi
APP="$(scripts/build-desktop.sh)"
DEST="${SELPHY_INSTALL_DIR:-/Applications}/SELPHY Studio.app"
ditto --norsrc --noextattr "$APP" "$DEST"
codesign --verify --deep --strict --verbose=2 "$DEST"
python3 scripts/pin-dock.py "$DEST"
open "$DEST"
