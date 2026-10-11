#!/usr/bin/env python3
"""Offline acceptance checks for the WinGet publication badge."""

import json
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
import unittest
import zipfile


CHECKER = Path(__file__).with_name("check-winget-publication.py")


class PublicationTest(unittest.TestCase):
    def test_catalog_states(self):
        cases = [
            ("absent", [], [], "v0.6.3 pending", "orange", "not listed"),
            ("older", ["0.6.2"], [], "v0.6.3 pending", "orange", "0.6.2"),
            ("exact", ["0.6.2", "0.6.3"], [], "v0.6.3 published", "brightgreen", "0.6.3"),
            ("other package", [], ["0.6.3"], "v0.6.3 pending", "orange", "not listed"),
            ("different version", ["0.6.30", "0.6.9", "0.6.10"], [], "v0.6.3 pending", "orange", "0.6.30"),
        ]
        for name, versions, other, message, color, available in cases:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                release = root / "release.json"
                release.write_text(json.dumps({"tag_name": "v0.6.3", "draft": False, "prerelease": False}))
                database = root / "index.db"
                with sqlite3.connect(database) as conn:
                    conn.executescript(
                        "CREATE TABLE ids(id TEXT); CREATE TABLE versions(version TEXT); "
                        "CREATE TABLE manifest(id INTEGER, version INTEGER);"
                    )
                    for package, values in (("Easonliuuuuu.vsfleet", versions), ("Other.Tool", other)):
                        package_id = conn.execute("INSERT INTO ids VALUES (?)", (package,)).lastrowid
                        for version in values:
                            version_id = conn.execute("INSERT INTO versions VALUES (?)", (version,)).lastrowid
                            conn.execute("INSERT INTO manifest VALUES (?, ?)", (package_id, version_id))
                source = root / "source.msix"
                with zipfile.ZipFile(source, "w") as archive:
                    archive.write(database, "Public/index.db")
                badge, summary = self.run_check(root, 0)
                self.assertEqual(badge["message"], message)
                self.assertEqual(badge["color"], color)
                self.assertIn(f"| v0.6.3 | {available} |", summary)

    def test_errors_replace_published_badge(self):
        for failure in ("missing input", "corrupt source", "unsupported index", "prerelease"):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                (root / "badge.json").write_text('{"message":"v0.6.3 published","color":"brightgreen"}')
                if failure != "missing input":
                    (root / "release.json").write_text(json.dumps({
                        "tag_name": "v0.6.3-rc1" if failure == "prerelease" else "v0.6.3",
                        "prerelease": failure == "prerelease",
                    }))
                    if failure == "unsupported index":
                        with zipfile.ZipFile(root / "source.msix", "w") as archive:
                            archive.writestr("Public/index.db", b"not a sqlite database")
                    else:
                        (root / "source.msix").write_bytes(b"not a zip archive")
                badge, summary = self.run_check(root, 1)
                self.assertEqual(badge["message"], "check failed")
                self.assertTrue(badge["isError"])
                self.assertIn("could not be checked", summary)

    def run_check(self, root, expected):
        result = subprocess.run(
            [sys.executable, str(CHECKER), "--release", str(root / "release.json"),
             "--source", str(root / "source.msix"), "--output", str(root / "badge.json"),
             "--summary", str(root / "summary.md")],
            capture_output=True, text=True, timeout=10,
        )
        self.assertEqual(result.returncode, expected, result.stderr)
        return json.loads((root / "badge.json").read_text()), (root / "summary.md").read_text()


if __name__ == "__main__":
    unittest.main()
