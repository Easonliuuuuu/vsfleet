#!/usr/bin/env python3
"""Compare a stable GitHub release with the public WinGet source index."""

import argparse
from contextlib import closing
import json
from pathlib import Path
import re
import sqlite3
import sys
import tempfile
import zipfile


PACKAGE_ID = "Easonliuuuuu.vsfleet"
STABLE_VERSION = re.compile(r"\d+\.\d+\.\d+")


def catalog_versions(source):
    with zipfile.ZipFile(source) as archive, tempfile.TemporaryDirectory() as tmp:
        database = Path(tmp) / "index.db"
        database.write_bytes(archive.read("Public/index.db"))
        with closing(sqlite3.connect(database.as_uri() + "?mode=ro&immutable=1", uri=True)) as conn:
            rows = conn.execute(
                "SELECT versions.version FROM manifest "
                "JOIN ids ON manifest.id = ids.rowid "
                "JOIN versions ON manifest.version = versions.rowid "
                "WHERE ids.id = ? COLLATE NOCASE",
                (PACKAGE_ID,),
            ).fetchall()
    return {row[0] for row in rows}


def check(release_file, source):
    release = json.loads(release_file.read_text())
    tag = release["tag_name"]
    if not isinstance(tag, str):
        raise ValueError("expected a stable release tag such as v0.6.3")
    version = tag.removeprefix("v")
    if release.get("draft") or release.get("prerelease") or not STABLE_VERSION.fullmatch(version):
        raise ValueError("expected a stable release tag such as v0.6.3")

    versions = catalog_versions(source)
    stable = [v for v in versions if STABLE_VERSION.fullmatch(v)]
    available = max(stable, key=lambda v: tuple(map(int, v.split("."))), default="not listed")
    published = version in versions
    status = "published" if published else "pending"
    badge = {
        "schemaVersion": 1,
        "label": "WinGet",
        "message": f"v{version} {status}",
        "color": "brightgreen" if published else "orange",
        "cacheSeconds": 300,
    }
    summary = (
        "## WinGet publication\n\n"
        "| Latest stable release | Latest catalog version | Status |\n"
        "| --- | --- | --- |\n"
        f"| v{version} | {available} | {status.capitalize()} |\n\n"
        f"Checked `{PACKAGE_ID}` in Microsoft's public WinGet catalog. "
        "Publication is confirmed only when the exact release version is present.\n"
    )
    return badge, summary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--release", required=True, type=Path)
    parser.add_argument("--source", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--summary", type=Path)
    args = parser.parse_args()

    try:
        badge, summary = check(args.release, args.source)
        result = 0
    except (OSError, ValueError, KeyError, TypeError, sqlite3.Error, zipfile.BadZipFile) as error:
        badge = {
            "schemaVersion": 1,
            "label": "WinGet",
            "message": "check failed",
            "color": "lightgrey",
            "isError": True,
            "cacheSeconds": 300,
        }
        summary = "## WinGet publication\n\nAvailability could not be checked. See this run's logs.\n"
        print(f"WinGet publication check failed: {error}", file=sys.stderr)
        result = 1

    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(badge, indent=2) + "\n")
    if args.summary:
        with args.summary.open("a") as summary_file:
            summary_file.write(summary)
    print(badge["message"])
    return result


if __name__ == "__main__":
    sys.exit(main())
