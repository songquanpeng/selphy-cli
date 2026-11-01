#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${VERSION:-0.2.0}"
BUILD_DIR="${SELPHY_BUILD_DIR:-$HOME/Library/Caches/selphy-studio/build}"
APP="$BUILD_DIR/SELPHY Studio.app"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources" .local/studio.iconset
(cd desktop && CGO_ENABLED=1 go build -tags production -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$APP/Contents/MacOS/selphy-studio" .)
swift scripts/icon.swift .local/studio-icon.png
for n in 16 32 128 256 512; do
  sips -z "$n" "$n" .local/studio-icon.png --out ".local/studio.iconset/icon_${n}x${n}.png" >/dev/null
  double=$((n*2))
  sips -z "$double" "$double" .local/studio-icon.png --out ".local/studio.iconset/icon_${n}x${n}@2x.png" >/dev/null
done
iconutil -c icns .local/studio.iconset -o "$APP/Contents/Resources/AppIcon.icns"
cp LICENSE "$APP/Contents/Resources/LICENSE"
python3 scripts/third-party-notices.py desktop "$APP/Contents/Resources/THIRD_PARTY_NOTICES.txt" >&2
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleName</key><string>SELPHY Studio</string>
<key>CFBundleDisplayName</key><string>SELPHY Studio</string>
<key>CFBundleIdentifier</key><string>com.justsong.selphy-studio</string>
<key>CFBundleExecutable</key><string>selphy-studio</string>
<key>CFBundleIconFile</key><string>AppIcon</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleShortVersionString</key><string>$VERSION</string>
<key>CFBundleVersion</key><string>2</string>
<key>LSMinimumSystemVersion</key><string>12.0</string>
<key>NSHighResolutionCapable</key><true/>
<key>NSLocalNetworkUsageDescription</key><string>连接同一网络中的 SELPHY 打印机，查询状态并打印你选择的照片。</string>
<key>NSHumanReadableCopyright</key><string>SELPHY Studio · selphy-cli</string>
</dict></plist>
PLIST
# Local Developer ID signing uses the existing Keychain identity; no credentials
# are embedded in source or artifacts. Set SIGN_IDENTITY=- for ad-hoc builds.
SIGN_IDENTITY="${SIGN_IDENTITY:-$(security find-identity -v -p codesigning | sed -n 's/.*"\(Developer ID Application:[^"]*\)".*/\1/p' | head -1)}"
SIGN_IDENTITY="${SIGN_IDENTITY:--}"
xattr -cr "$APP"
if [ "$SIGN_IDENTITY" = "-" ]; then
  codesign --force --sign - "$APP"
else
  codesign --force --options runtime --timestamp --sign "$SIGN_IDENTITY" "$APP"
fi
codesign --verify --deep --strict --verbose=2 "$APP"
printf '%s\n' "$APP"
