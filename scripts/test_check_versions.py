#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Tests of scripts/check-versions.sh, on a copy of the repository: python3 -m unittest discover -s scripts"""

import pathlib
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parent.parent


class CheckVersions(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = pathlib.Path(tmp.name) / "repo"
        shutil.copytree(ROOT, self.root, ignore=shutil.ignore_patterns(".git", "reports", "__pycache__"))

    def check(self):
        """The exit status and output of check-versions.sh in the copy."""
        run = subprocess.run(["bash", str(self.root / "scripts" / "check-versions.sh")],
                             stdout=subprocess.PIPE, stderr=subprocess.STDOUT, universal_newlines=True)
        return run.returncode, run.stdout

    def edit(self, path, old, new):
        """Replaces the first old in the copy's file at path, which must contain it."""
        file = self.root / path
        text = file.read_text()
        self.assertIn(old, text, f"{path} has no {old!r}")
        file.write_text(text.replace(old, new, 1))

    def own_build(self):
        """The directory of the first own/*/Dockerfile, as versions.yaml names it."""
        dockerfiles = sorted(self.root.glob("own/*/Dockerfile"))
        self.assertTrue(dockerfiles, "no own/*/Dockerfile")
        return dockerfiles[0].parent.relative_to(self.root).as_posix()

    def released(self):
        """The adapters of ADAPTERS in the Makefile."""
        line = re.search(r"^ADAPTERS \?= (.*)$", (self.root / "Makefile").read_text(), re.M)
        self.assertIsNotNone(line, "Makefile has no ADAPTERS ?=")
        return line.group(0), line.group(1).split()

    def assertFails(self, needle):
        code, out = self.check()
        self.assertEqual(code, 1, out)
        self.assertIn(needle, out)

    def test_the_repository_as_it_is_passes(self):
        code, out = self.check()
        self.assertEqual(code, 0, out)
        self.assertNotIn("FAIL", out)

    def test_an_own_stack_digest_the_entry_does_not_list_fails(self):
        build = self.own_build()
        dockerfile = f"{build}/Dockerfile"
        digest = re.search(r"^ARG GOLANG_DIGEST=(sha256:[0-9a-f]{64})$", (self.root / dockerfile).read_text(), re.M)
        self.assertIsNotNone(digest, f"{dockerfile} has no ARG GOLANG_DIGEST=")
        self.edit(dockerfile, digest.group(1), "sha256:" + "0" * 64)
        self.assertFails(f"FAIL {build} GOLANG_DIGEST")

    def test_an_own_stack_digest_changed_in_versions_yaml_only_fails(self):
        # Only the entry of the own stack counts, not the same digest in an
        # adapter's entry.
        versions = self.root / "versions.yaml"
        head, own = versions.read_text().split("\nown_implementations:\n", 1)
        digest = re.search(r'debian: "debian:[^@"]+@(sha256:[0-9a-f]{64})"', own)
        self.assertIsNotNone(digest, "own_implementations lists no debian base image")
        versions.write_text(head + "\nown_implementations:\n" + own.replace(digest.group(1), "sha256:" + "0" * 64))
        self.assertFails(f"FAIL {self.own_build()} DEBIAN_DIGEST")

    def test_an_own_stack_without_an_entry_fails(self):
        build = self.own_build()
        self.edit("versions.yaml", f'build: "{build}"', 'build: "own/elsewhere"')
        self.assertFails(f"FAIL {build}: no entry of own_implementations")

    def test_a_released_adapter_that_is_not_published_fails(self):
        _, adapters = self.released()
        name = adapters[-1]
        versions = self.root / "versions.yaml"
        text, n = re.subn(rf"(^  {name}:\n(?:    .*\n)*?    status: )published", r"\1implemented",
                          versions.read_text(), count=1, flags=re.M)
        self.assertEqual(n, 1, f"versions.yaml has no status: published for {name}")
        versions.write_text(text)
        self.assertFails(f"FAIL {name}: in ADAPTERS of the Makefile")

    def test_a_published_adapter_that_is_not_released_fails(self):
        line, adapters = self.released()
        name = adapters[-1]
        self.edit("Makefile", line, "ADAPTERS ?= " + " ".join(adapters[:-1]))
        self.assertFails(f"FAIL {name}: versions.yaml says status: published")

    def test_a_requirements_version_other_than_the_dockerfile_fails(self):
        requirements = "adapters/elaadnl/requirements.txt"
        pin = re.search(r"^openadr3-client==([^ \\]+)", (self.root / requirements).read_text(), re.M)
        self.assertIsNotNone(pin, f"{requirements} pins no openadr3-client")
        self.edit(requirements, pin.group(0), pin.group(0) + ".post1")
        self.assertFails("FAIL openadr3-client in Dockerfile and requirements.txt")

    def test_a_wheel_hash_that_requirements_does_not_allow_fails(self):
        wheel = re.search(r'wheel_sha256: "([0-9a-f]{64})"', (self.root / "versions.yaml").read_text())
        self.assertIsNotNone(wheel, "versions.yaml has no wheel_sha256")
        self.edit("adapters/elaadnl/requirements.txt", wheel.group(1), "0" * 64)
        self.assertFails("FAIL elaadnl wheel_sha256")


if __name__ == "__main__":
    unittest.main()
