#!/usr/bin/env python3
"""Validate documentation and the frozen source scope; no product tests."""
import hashlib
import json
import re
import sys
from pathlib import Path
from urllib.parse import unquote, urlsplit

BASELINE_SHA = "baed6c354e1966cdae7bc5328e771dc50ae6c70b"
FILES_DIGEST = "f361e072d531274a0e0148a9668ba5de0a1ec58fc7c4466c29730681e3d01d8d"
REQUIREMENTS = {"FR-%02d" % n for n in range(1, 19)}
MODULES = {
    "MOD-CORE", "MOD-IAM", "MOD-MODEL", "MOD-PROMPT", "MOD-AGENT",
    "MOD-TOOL", "MOD-KB", "MOD-SKILL", "MOD-APPROVAL", "MOD-TEAM",
    "MOD-GUARD", "MOD-AUDIT", "MOD-OBS", "MOD-EVAL", "MOD-CACHE",
    "MOD-CHANNEL", "MOD-SDK", "MOD-BUSINESS", "MOD-DELIVERY",
}


def check_root(root):
    root = Path(root).resolve()
    errors = []
    required = [
        "README.md", "LICENSE", "THIRD_PARTY.md", "CONTRIBUTING.md",
        "docs/product/PRD.md", "docs/architecture/ARCHITECTURE.md",
        "docs/architecture/MODULES.md", "docs/architecture/DATA-MODEL.md",
        "docs/architecture/API-CONVENTIONS.md", "docs/delivery/BACKLOG.md",
        "docs/delivery/STATUS.md", "docs/tasks/ENG-001.md",
        "docs/reference/modules.json", "docs/reference/upstream-files.json",
    ]
    for name in required:
        if not (root / name).is_file():
            errors.append("missing required file: " + name)
    for path in root.rglob("*.md"):
        if ".git" in path.parts or "node_modules" in path.parts:
            continue
        text = path.read_text(encoding="utf-8")
        if "/Users/" in text:
            errors.append("local absolute path in " + str(path.relative_to(root)))
        # Exclude examples inside fenced code blocks.
        prose = re.sub(r"```.*?```", "", text, flags=re.S)
        for target in re.findall(r"\]\(([^\s)]+)\)", prose):
            target = unquote(target.strip("<>"))
            parsed = urlsplit(target)
            if parsed.scheme or target.startswith("#"):
                continue
            local = parsed.path
            if not local:
                continue
            candidate = (path.parent / local).resolve()
            try:
                candidate.relative_to(root)
            except ValueError:
                errors.append("link escapes repository: " + target)
                continue
            if not candidate.exists():
                errors.append("broken link in %s: %s" % (path.relative_to(root), target))
    try:
        registry = json.loads((root / "docs/reference/modules.json").read_text())
        entries = registry["modules"]
        ids = [entry["id"] for entry in entries]
        if set(ids) != MODULES or len(ids) != len(MODULES):
            errors.append("module registry does not match the complete scope")
        covered = {fr for entry in entries for fr in entry["requirements"]}
        if covered != REQUIREMENTS:
            errors.append("requirement coverage is incomplete or unknown")
        prd = (root / "docs/product/PRD.md").read_text()
        if set(re.findall(r"^### (FR-\d{2})\b", prd, flags=re.M)) != REQUIREMENTS:
            errors.append("PRD requirement headings do not match FR-01..FR-18")
        manifest = json.loads((root / "docs/reference/upstream-files.json").read_text())
        if manifest["commit"] != BASELINE_SHA:
            errors.append("upstream baseline SHA changed")
        source = manifest["files"]
        paths = [entry["path"] for entry in source]
        if len(paths) != 488 or len(set(paths)) != 488:
            errors.append("expected 488 unique frozen upstream files")
        for entry in source + manifest["related_files"]:
            if entry["module"] not in MODULES:
                errors.append("unknown source module: " + entry["module"])
            if not re.fullmatch(r"[0-9a-f]{40}", entry["blob_sha"]):
                errors.append("invalid upstream blob SHA: " + entry["path"])
            if ".." in Path(entry["path"]).parts or entry["path"].startswith("/"):
                errors.append("invalid upstream path: " + entry["path"])
        if any(not path.startswith("kbot-final/") for path in paths):
            errors.append("source file outside frozen subtree")
        frozen = [{key: entry[key] for key in ("path", "mode", "blob_sha")}
                  for entry in source + manifest["related_files"]]
        digest = hashlib.sha256(json.dumps(frozen, sort_keys=True,
                                           separators=(",", ":")).encode()).hexdigest()
        if digest != FILES_DIGEST:
            errors.append("frozen upstream file fingerprint changed")
        related = manifest["related_files"]
        if len(related) != 1 or related[0]["path"] != ".github/workflows/ci.yml":
            errors.append("root CI reference missing")
        if not (root / "README.md").read_text().startswith("# AsterFlow"):
            errors.append("README product name mismatch")
    except (OSError, ValueError, KeyError, TypeError) as exc:
        errors.append("invalid scope data: " + str(exc))
    return errors


def main():
    errors = check_root(Path(__file__).resolve().parents[1])
    if errors:
        for error in errors:
            print("ERROR: " + error, file=sys.stderr)
        return 1
    print("Documentation links, FR/MOD scope and 488-file baseline are valid.")
    print("This does not certify product implementation or external acceptance.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
