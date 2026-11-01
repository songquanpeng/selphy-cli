#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${VERSION:-0.2.0}"
export VERSION
APP="$(scripts/build-desktop.sh)"
mkdir -p dist
ZIP="$PWD/dist/SELPHY-Studio-$VERSION-macos-arm64.zip"
CLI="$PWD/dist/selphy-$VERSION-darwin-arm64"
NOTICES="$PWD/dist/THIRD-PARTY-NOTICES-$VERSION.txt"
if [ "$(uname -m)" != "arm64" ]; then
  printf '%s\n' '此脚本需要在 Apple Silicon Mac 上构建。' >&2
  exit 1
fi
if [ -n "${SELPHY_NOTARY_PROFILE:-}" ]; then
  ditto -c -k --keepParent --norsrc --noextattr "$APP" "$ZIP"
  xcrun notarytool submit "$ZIP" --keychain-profile "$SELPHY_NOTARY_PROFILE" --wait
  xcrun stapler staple "$APP"
  xcrun stapler validate "$APP"
fi
ditto -c -k --keepParent --norsrc --noextattr "$APP" "$ZIP"
go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$CLI" ./cmd/selphy
"$CLI" --version
python3 scripts/third-party-notices.py . "$NOTICES"
cp LICENSE "dist/LICENSE-$VERSION.txt"
CHECK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/selphy-release.XXXXXX")"
trap 'rm -rf "$CHECK_DIR"' EXIT
ditto -x -k "$ZIP" "$CHECK_DIR"
codesign --verify --deep --strict --verbose=2 "$CHECK_DIR/SELPHY Studio.app"
EXPECTED_VERSION="$VERSION" python3 - "$CHECK_DIR/SELPHY Studio.app/Contents/Info.plist" <<'PY'
import os, plistlib, sys
with open(sys.argv[1], 'rb') as f:
    assert plistlib.load(f)['CFBundleShortVersionString'] == os.environ['EXPECTED_VERSION']
PY
(cd dist && shasum -a 256 "SELPHY-Studio-$VERSION-macos-arm64.zip" "selphy-$VERSION-darwin-arm64" "LICENSE-$VERSION.txt" "THIRD-PARTY-NOTICES-$VERSION.txt" > "SHA256SUMS-$VERSION.txt")
printf '%s\n' "$ZIP" "$CLI" "$PWD/dist/SHA256SUMS-$VERSION.txt"
