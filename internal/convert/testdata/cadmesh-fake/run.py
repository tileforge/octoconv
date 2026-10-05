import json
import os
import runpy
import struct
import sys
import types

scenario = json.loads(os.environ["FAKE_MESH"])


def module(name, **attrs):
    mod = types.ModuleType(name)
    mod.__dict__.update(attrs)
    sys.modules[name] = mod
    return mod


class Point:
    def __init__(self, x, y, z):
        self.x, self.y, self.z = x, y, z

    def X(self):
        return self.x

    def Y(self):
        return self.y

    def Z(self):
        return self.z


class Box:
    def __init__(self):
        self.void = True

    def IsVoid(self):
        return self.void

    def CornerMin(self):
        return Point(0.0, 0.0, 0.0)

    def CornerMax(self):
        return Point(10.0, 10.0, 10.0)

    def Add(self, other):
        self.void = False


class Shape:
    def IsNull(self):
        return False


class Checks:
    def Start(self):
        pass

    def More(self):
        return False


class Workspace:
    def ModelCheckList(self, _):
        return Checks()


class Reader:
    def ReadFile(self, _):
        return 1

    def WS(self):
        return Workspace()

    def TransferRoots(self):
        pass

    def OneShape(self):
        return Shape()


class Mesher:
    def __init__(self, *_):
        pass

    def IsDone(self):
        return scenario.get("done", True)

    def GetStatusFlags(self):
        return scenario.get("flags", 0)


class Triangulation:
    def __init__(self, n):
        self.n = n

    def NbTriangles(self):
        return self.n


class Face:
    def __init__(self, n):
        self.n = n


class Explorer:
    def __init__(self, shape, kind):
        self.items = [Face(n) for n in scenario["faces"]]

    def More(self):
        return bool(self.items)

    def Current(self):
        return self.items[0]

    def Next(self):
        self.items.pop(0)


class Writer:
    ASCIIMode = False

    def Write(self, shape, out):
        count = scenario["stl"]
        with open(out, "wb") as fh:
            fh.write(b"\0" * 80 + struct.pack("<I", count) + b"\0" * (50 * count + scenario.get("stl_extra", 0)))
        return True


def triangulation(face, loc):
    return None if face.n is None else Triangulation(face.n)


status = {"IMeshData_" + name: types.SimpleNamespace(value=value) for name, value in {
    "NoError": 0, "OpenWire": 1, "SelfIntersectingWire": 2, "Failure": 4, "ReMesh": 8, "UnorientedWire": 16,
    "TooFewPoints": 32, "Outdated": 64, "Reused": 128, "UserBreak": 256}.items()}

module("OCP")
module("OCP.BRepMesh", BRepMesh_IncrementalMesh=Mesher)
module("OCP.IFSelect", IFSelect_RetDone=1)
module("OCP.IGESControl", IGESControl_Reader=Reader)
module("OCP.STEPControl", STEPControl_Reader=Reader)
module("OCP.Interface", Interface_Static=types.SimpleNamespace(SetCVal_s=staticmethod(lambda *_: True)))
module("OCP.StlAPI", StlAPI_Writer=Writer)
module("OCP.BRepBndLib", BRepBndLib=types.SimpleNamespace(Add_s=staticmethod(lambda shape, box: box.Add(None)), AddOptimal_s=staticmethod(lambda *_: None)))
module("OCP.Bnd", Bnd_Box=Box)
module("OCP.TopAbs", TopAbs_FACE=0)
module("OCP.TopExp", TopExp_Explorer=Explorer)
module("OCP.TopLoc", TopLoc_Location=lambda: None)
module("OCP.TopoDS", TopoDS=types.SimpleNamespace(Face_s=staticmethod(lambda x: x)))
module("OCP.BRep", BRep_Tool=types.SimpleNamespace(Triangulation_s=staticmethod(triangulation)))
module("OCP.IMeshData", IMeshData_Status=types.SimpleNamespace(**status))

sys.argv = sys.argv[1:]
runpy.run_path(sys.argv[0], run_name="__main__")
