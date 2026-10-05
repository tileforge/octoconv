import os
import subprocess
import sys
import tempfile

TIMEOUT = 900
ROOT = "/corpus"


def kind_of(path):
    ext = os.path.splitext(path)[1].lower().lstrip(".")
    if ext in ("step", "stp"):
        return "step"
    if ext in ("iges", "igs"):
        return "iges"
    if ext in ("dxf", "stl"):
        return ext
    return None


def read_occ(path, kind):
    from OCP.IFSelect import IFSelect_RetDone
    from OCP.IGESControl import IGESControl_Reader
    from OCP.STEPControl import STEPControl_Reader

    reader = IGESControl_Reader() if kind == "iges" else STEPControl_Reader()
    parse = reader.ReadFile(path) == IFSelect_RetDone
    transfer = 0
    if parse:
        reader.TransferRoots()
        transfer = 0 if reader.OneShape().IsNull() else 1
    name = "ocp-iges" if kind == "iges" else "ocp-step"
    return name, int(parse), "parse=%d,transfer=%d" % (int(parse), transfer)


def read_dxf(path):
    import ezdxf
    from ezdxf import recover

    strict = "1"
    try:
        ezdxf.readfile(path)
    except Exception as exc:
        strict = "0:" + type(exc).__name__
    rec = "1"
    try:
        recover.readfile(path)
    except Exception as exc:
        rec = "0:" + type(exc).__name__
    return "ezdxf", int(rec == "1"), "strict=%s,recover=%s" % (strict, rec)


def read_stl(path):
    sys.path.insert(0, "/opt/cad-measure")
    import cad_mesh

    tri, binary = cad_mesh.read_stl(path)
    if tri is None:
        return "stl-numpy", 0, "binary=%d,triangles=none" % int(binary)
    return "stl-numpy", 1, "binary=%d,triangles=%d" % (int(binary), len(tri))


def one(path, result):
    kind = kind_of(path)
    try:
        if kind in ("step", "iges"):
            reader, accepts, detail = read_occ(path, kind)
        elif kind == "dxf":
            reader, accepts, detail = read_dxf(path)
        elif kind == "stl":
            reader, accepts, detail = read_stl(path)
        else:
            reader, accepts, detail = "none", "-", "-"
    except Exception as exc:
        reader = {"step": "ocp-step", "iges": "ocp-iges", "dxf": "ezdxf", "stl": "stl-numpy"}.get(kind, "none")
        accepts, detail = 0, "exception:" + type(exc).__name__
    with open(result, "w") as fh:
        fh.write("%s\t%s\t%s\n" % (reader, accepts, detail))
    return 0


def batch(rels):
    fallback = {"step": "ocp-step", "iges": "ocp-iges", "dxf": "ezdxf", "stl": "stl-numpy"}
    with tempfile.TemporaryDirectory() as tmp:
        for rel in rels:
            path = os.path.join(ROOT, rel)
            result = os.path.join(tmp, "result.tsv")
            if os.path.exists(result):
                os.remove(result)
            reader = fallback.get(kind_of(path), "none")
            try:
                proc = subprocess.run(
                    [sys.executable, "-I", "-B", os.path.abspath(__file__), "--one", path, "--result", result],
                    stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                    timeout=TIMEOUT)
                rc = proc.returncode
                timed_out = False
            except subprocess.TimeoutExpired:
                rc = -1
                timed_out = True
            if timed_out:
                line = "%s\t0\ttimeout" % reader
            elif rc != 0 or not os.path.exists(result):
                line = "%s\t0\tcrash:rc=%d" % (reader, rc)
            else:
                with open(result) as fh:
                    line = fh.read().rstrip("\n")
            sys.stdout.write("%s\t%s\n" % (rel, line))
            sys.stdout.flush()
    return 0


def main(argv):
    if len(argv) >= 2 and argv[0] == "--one":
        if len(argv) != 4 or argv[2] != "--result":
            return 64
        return one(argv[1], argv[3])
    return batch(argv)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
