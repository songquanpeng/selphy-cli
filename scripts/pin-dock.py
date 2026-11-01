#!/usr/bin/env python3
"""Append the installed app to Dock without replacing other Dock preferences."""
import pathlib
import plistlib
import subprocess
import sys
app = pathlib.Path(sys.argv[1]).resolve()
if not app.is_dir():
    raise SystemExit(f"App not found: {app}")
raw = subprocess.check_output(["defaults", "export", "com.apple.dock", "-"])
prefs = plistlib.loads(raw)
url = app.as_uri() + "/"
items = prefs.get("persistent-apps", [])
if not any(i.get("tile-data", {}).get("bundle-identifier") == "com.justsong.selphy-studio" or i.get("tile-data", {}).get("file-data", {}).get("_CFURLString", "").rstrip("/") == url.rstrip("/") for i in items):
    item = {"tile-data": {"file-data": {"_CFURLString": url, "_CFURLStringType": 15}, "file-label": "SELPHY Studio", "bundle-identifier": "com.justsong.selphy-studio", "file-type": 41}, "tile-type": "file-tile"}
    xml = plistlib.dumps(item).decode()
    subprocess.run(["defaults", "write", "com.apple.dock", "persistent-apps", "-array-add", xml], check=True)
    subprocess.run(["killall", "Dock"], check=False)
check = plistlib.loads(subprocess.check_output(["defaults", "export", "com.apple.dock", "-"]))
assert any(i.get("tile-data", {}).get("bundle-identifier") == "com.justsong.selphy-studio" for i in check.get("persistent-apps", []))
print("Dock: SELPHY Studio pinned")
