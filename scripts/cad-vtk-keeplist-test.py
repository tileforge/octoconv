#!/usr/bin/env python3
"""Unit test of scripts/cad-measure/vtk_keeplist.py over synthetic arm64 and amd64 trees.

Usage:
    python3 scripts/cad-vtk-keeplist-test.py

Prints KEEPLIST_TEST,cases=N,failed=M and exits 1 on a failure. No docker, no vtk.
"""

import contextlib
import importlib.metadata
import io
import os
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(HERE, "cad-measure"))
import vtk_keeplist  # noqa: E402

REPO_KEEPLIST = os.path.join(HERE, "cad-measure", "vtk-keeplist.txt")
VTK_VERSION = "9.7.1"
ARCH_TAGS = {"arm64": "cpython-311-aarch64-linux-gnu", "amd64": "cpython-311-x86_64-linux-gnu"}
WRAPPED = ["vtkCommonCore", "vtkRenderingCore"]
NEEDED_LIBS = ["libvtkCommonCore.so", "libvtkRenderingCore.so"]
EXTRA_LIBS = ["libvtkIOXML.so", "libvtkChartsCore.so"]
EXTRA_WRAPPED = ["vtkIOXML", "vtkChartsCore"]


def build_tree(site, arch, drop=()):
    root = os.path.join(site, "vtkmodules")
    os.makedirs(root)
    names = list(NEEDED_LIBS) + list(EXTRA_LIBS)
    for mod in WRAPPED + EXTRA_WRAPPED:
        names.append("%s.%s.so" % (mod, ARCH_TAGS[arch]))
    for name in names:
        if name in drop:
            continue
        with open(os.path.join(root, name), "wb") as fh:
            fh.write(b"x" * 10)
    with open(os.path.join(root, "__init__.py"), "w") as fh:
        fh.write("")


def maps_line(arch, mod):
    return "ffff0000-ffff1000 r-xp 00000000 00:00 1 /opt/venv/lib/python3.11/site-packages/vtkmodules/%s.%s.so\n" % (
        mod, ARCH_TAGS[arch])


def write_maps(path, arch):
    with open(path, "w") as fh:
        for lib in NEEDED_LIBS:
            fh.write("ffff0000-ffff1000 r-xp 00000000 00:00 1 /opt/venv/lib/python3.11/site-packages/vtkmodules/%s\n" % lib)
        for mod in WRAPPED:
            fh.write(maps_line(arch, mod))
        fh.write("ffff0000-ffff1000 r-xp 00000000 00:00 1 /usr/lib/libc.so.6\n")


def run(fn, *argv):
    out, err = io.StringIO(), io.StringIO()
    with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
        rc = fn(*argv)
    return rc, out.getvalue(), err.getvalue()


class Args:
    def __init__(self, **kw):
        self.__dict__.update(kw)


class FakeVersion:
    def __init__(self, version):
        self.version = version
        self.saved = None

    def __enter__(self):
        self.saved = importlib.metadata.version
        importlib.metadata.version = lambda name: self.version

    def __exit__(self, *exc):
        importlib.metadata.version = self.saved


class NormalizeTest(unittest.TestCase):
    def test_arch_tags_collapse(self):
        a = vtk_keeplist.normalize("vtkmodules/vtkCommonCore.cpython-311-aarch64-linux-gnu.so")
        b = vtk_keeplist.normalize("vtkmodules/vtkCommonCore.cpython-311-x86_64-linux-gnu.so")
        self.assertEqual(a, "vtkmodules/vtkCommonCore.so")
        self.assertEqual(a, b)

    def test_plain_library_unchanged(self):
        self.assertEqual(vtk_keeplist.normalize("vtkmodules/libvtkCommonCore.so"), "vtkmodules/libvtkCommonCore.so")
        self.assertEqual(
            vtk_keeplist.normalize("vtkmodules/libvtkWrappingPythonCore3.11.so"),
            "vtkmodules/libvtkWrappingPythonCore3.11.so",
        )

    def test_committed_list_is_arch_neutral(self):
        with open(REPO_KEEPLIST) as fh:
            lines = [ln.rstrip("\n") for ln in fh][2:]
        self.assertTrue(lines)
        for ln in lines:
            self.assertNotIn("aarch64", ln)
            self.assertNotIn("x86_64", ln)
            self.assertNotIn("cpython", ln)
            self.assertEqual(vtk_keeplist.normalize(ln), ln)


class CollectTest(unittest.TestCase):
    def test_same_list_from_arm64_and_amd64_maps(self):
        with tempfile.TemporaryDirectory() as d:
            outs = {}
            for arch in ARCH_TAGS:
                maps = os.path.join(d, "maps-" + arch)
                write_maps(maps, arch)
                out = os.path.join(d, "list-" + arch)
                rc, _, _ = run(vtk_keeplist.collect, Args(vtk_version=VTK_VERSION, out=out, maps=[maps]))
                self.assertEqual(rc, 0)
                with open(out) as fh:
                    outs[arch] = fh.read()
            self.assertEqual(outs["arm64"], outs["amd64"])
            self.assertIn("vtkmodules/vtkCommonCore.so\n", outs["arm64"])


class PruneTest(unittest.TestCase):
    def keeplist_from(self, d, arch):
        maps = os.path.join(d, "maps")
        write_maps(maps, arch)
        out = os.path.join(d, "keeplist.txt")
        run(vtk_keeplist.collect, Args(vtk_version=VTK_VERSION, out=out, maps=[maps]))
        return out

    def prune_arch(self, build_arch, list_arch):
        with tempfile.TemporaryDirectory() as d:
            site = os.path.join(d, "site-packages")
            build_tree(site, build_arch)
            keeplist = self.keeplist_from(d, list_arch)
            with FakeVersion(VTK_VERSION):
                rc, out, err = run(vtk_keeplist.prune, Args(keeplist=keeplist, site=site))
            left = sorted(os.listdir(os.path.join(site, "vtkmodules")))
            return rc, out, err, left

    def test_prune_each_arch_with_a_list_collected_on_the_other(self):
        for build_arch in ARCH_TAGS:
            for list_arch in ARCH_TAGS:
                rc, out, err, left = self.prune_arch(build_arch, list_arch)
                self.assertEqual(rc, 0, (build_arch, list_arch, err))
                self.assertIn("PRUNED,kept=4,removed=4,", out)
                self.assertEqual(
                    left,
                    sorted(["__init__.py"] + NEEDED_LIBS + ["%s.%s.so" % (m, ARCH_TAGS[build_arch]) for m in WRAPPED]),
                )

    def test_committed_list_prunes_a_synthetic_amd64_tree(self):
        with tempfile.TemporaryDirectory() as d:
            site = os.path.join(d, "site-packages")
            root = os.path.join(site, "vtkmodules")
            os.makedirs(root)
            with open(REPO_KEEPLIST) as fh:
                entries = [ln.rstrip("\n") for ln in fh][2:]
            for entry in entries:
                name = os.path.basename(entry)
                if not name.startswith("lib"):
                    name = name[: -len(".so")] + ".cpython-311-x86_64-linux-gnu.so"
                with open(os.path.join(root, name), "wb") as fh:
                    fh.write(b"x")
            with open(os.path.join(root, "vtkIOXML.cpython-311-x86_64-linux-gnu.so"), "wb") as fh:
                fh.write(b"x")
            with open(os.path.join(root, "libvtkIOXML.so"), "wb") as fh:
                fh.write(b"x")
            with FakeVersion(VTK_VERSION):
                rc, out, err = run(vtk_keeplist.prune, Args(keeplist=REPO_KEEPLIST, site=site))
            self.assertEqual(rc, 0, err)
            self.assertIn("PRUNED,kept=%d,removed=2," % len(entries), out)

    def test_stale_version_refused(self):
        with tempfile.TemporaryDirectory() as d:
            site = os.path.join(d, "site-packages")
            build_tree(site, "amd64")
            keeplist = self.keeplist_from(d, "amd64")
            with FakeVersion("9.7.2"):
                rc, _, err = run(vtk_keeplist.prune, Args(keeplist=keeplist, site=site))
            self.assertEqual(rc, 1)
            self.assertIn("keep-list stale: generated for vtk 9.7.1, installed 9.7.2", err)
            self.assertEqual(len(os.listdir(os.path.join(site, "vtkmodules"))), 9)

    def test_missing_required_module_fails_and_removes_nothing(self):
        for arch in ARCH_TAGS:
            with tempfile.TemporaryDirectory() as d:
                site = os.path.join(d, "site-packages")
                build_tree(site, arch, drop=("libvtkRenderingCore.so",))
                keeplist = self.keeplist_from(d, arch)
                with FakeVersion(VTK_VERSION):
                    rc, _, err = run(vtk_keeplist.prune, Args(keeplist=keeplist, site=site))
                self.assertEqual(rc, 1)
                self.assertIn("keep-list stale: missing vtkmodules/libvtkRenderingCore.so", err)
                self.assertEqual(len(os.listdir(os.path.join(site, "vtkmodules"))), 8)

    def test_missing_wrapped_module_fails_on_either_arch(self):
        for arch in ARCH_TAGS:
            with tempfile.TemporaryDirectory() as d:
                site = os.path.join(d, "site-packages")
                build_tree(site, arch, drop=("vtkCommonCore.%s.so" % ARCH_TAGS[arch],))
                keeplist = self.keeplist_from(d, arch)
                with FakeVersion(VTK_VERSION):
                    rc, _, err = run(vtk_keeplist.prune, Args(keeplist=keeplist, site=site))
                self.assertEqual(rc, 1)
                self.assertIn("keep-list stale: missing vtkmodules/vtkCommonCore.so", err)

    def test_arch_specific_old_list_still_matches_its_own_arch(self):
        with tempfile.TemporaryDirectory() as d:
            site = os.path.join(d, "site-packages")
            build_tree(site, "arm64")
            keeplist = os.path.join(d, "old.txt")
            with open(keeplist, "w") as fh:
                fh.write("vtk_version=%s\nscope=vtkmodules\n" % VTK_VERSION)
                for lib in NEEDED_LIBS:
                    fh.write("vtkmodules/%s\n" % lib)
                for mod in WRAPPED:
                    fh.write("vtkmodules/%s.%s.so\n" % (mod, ARCH_TAGS["arm64"]))
            with FakeVersion(VTK_VERSION):
                rc, out, err = run(vtk_keeplist.prune, Args(keeplist=keeplist, site=site))
            self.assertEqual(rc, 0, err)
            self.assertIn("kept=4,removed=4", out)


if __name__ == "__main__":
    suite = unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
    result = unittest.TextTestRunner(stream=io.StringIO(), verbosity=0).run(suite)
    for _, tb in result.failures + result.errors:
        print(tb, file=sys.stderr)
    failed = len(result.failures) + len(result.errors)
    print("KEEPLIST_TEST,cases=%d,failed=%d" % (result.testsRun, failed))
    sys.exit(1 if failed else 0)
