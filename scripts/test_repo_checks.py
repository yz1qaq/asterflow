import json
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

import check_repo

ROOT = Path(__file__).resolve().parents[1]


class RepositoryChecksTest(unittest.TestCase):
    def fixture(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        target = Path(temp.name) / "repo"
        shutil.copytree(
            ROOT, target,
            ignore=shutil.ignore_patterns(".git", "__pycache__", "node_modules"),
        )
        return target

    def test_repository_documentation_is_consistent(self):
        self.assertEqual(check_repo.check_root(ROOT), [])

    def test_broken_document_link_fails(self):
        root = self.fixture()
        path = root / "README.md"
        path.write_text(path.read_text() + "\n[broken](missing-doc.md)\n")
        self.assertTrue(any("broken link" in err for err in check_repo.check_root(root)))

    def test_dropped_source_file_fails(self):
        root = self.fixture()
        path = root / "docs/reference/upstream-files.json"
        data = json.loads(path.read_text())
        data["files"].pop()
        path.write_text(json.dumps(data))
        self.assertTrue(any("488" in err for err in check_repo.check_root(root)))

    def test_unknown_module_fails(self):
        root = self.fixture()
        path = root / "docs/reference/upstream-files.json"
        data = json.loads(path.read_text())
        data["files"][0]["module"] = "MOD-UNKNOWN"
        path.write_text(json.dumps(data))
        self.assertTrue(any("unknown source module" in err
                            for err in check_repo.check_root(root)))

    def test_hook_blocks_main_and_allows_feature(self):
        hook = ROOT / ".githooks/pre-push"
        for remote, expected in [("refs/heads/main", 1),
                                 ("refs/heads/feat/ENG-001-config-health", 0)]:
            result = subprocess.run(
                ["sh", str(hook), "origin", "https://github.com/yz1qaq/asterflow.git"],
                input="refs/heads/topic abc %s def\n" % remote,
                text=True, capture_output=True, check=False,
            )
            self.assertEqual(result.returncode, expected, result.stderr)
