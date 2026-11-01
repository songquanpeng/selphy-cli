#!/usr/bin/env python3
"""Collect licenses for Go modules linked into a production executable."""
import pathlib
import subprocess
import sys

root = pathlib.Path(__file__).resolve().parent.parent
module = (root / sys.argv[1]).resolve()
output = pathlib.Path(sys.argv[2]).resolve()
target = "./cmd/selphy" if module == root else "."
listing = subprocess.check_output(
    ["go", "list", "-deps", "-tags", "production", "-f",
     "{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}", target],
    cwd=module, text=True,
)
goroot = pathlib.Path(subprocess.check_output(["go", "env", "GOROOT"], cwd=module, text=True).strip())
parts = ["Third-party software notices\n", "Go standard library\n\n" + (goroot / "LICENSE").read_text()]
seen = set()
for line in sorted(set(listing.splitlines())):
    if not line.strip():
        continue
    name, version, directory = line.split("|", 2)
    if name in {"github.com/songquanpeng/selphy-cli", "github.com/songquanpeng/selphy-cli/desktop"}:
        continue
    directory = pathlib.Path(directory)
    licenses = sorted(p for p in directory.iterdir() if p.is_file() and p.name.upper().startswith(("LICENSE", "COPYING", "NOTICE")))
    if name == "github.com/wailsapp/wails/v2":
        licenses += sorted((directory / "internal").rglob("LICENSE*"))
    if not licenses:
        raise SystemExit(f"Missing license for {name}; review before distributing")
    for path in licenses:
        relative = path.relative_to(directory).as_posix()
        key = (name, relative)
        if key not in seen:
            seen.add(key)
            parts.append(f"{name} {version} — {relative}\n\n{path.read_text()}")
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text("\n\n" + ("\n\n" + "=" * 72 + "\n\n").join(parts), encoding="utf-8")
print(f"Collected license files: {len(seen)}")
