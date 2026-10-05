package convert

import (
	"fmt"
	"runtime"
)

var cadMeshReadOnly = []string{"/opt/venv", "/usr/bin/python3.11", "/usr/lib/python3.11"}

var cadGuardReadOnly = []string{"/usr/bin/python3.11", "/usr/lib/python3.11"}

var cadMeshEngines = []string{"cad-mesh"}

var cadGuardEngines = []string{"cad-guard"}

var cadMultiarchLib = map[string]string{"arm64": "aarch64-linux-gnu", "amd64": "x86_64-linux-gnu"}

func (c CADConverter) cadStage(jobDir string, readOnly, engines []string) (engineExec, error) {
	ro := append([]string{}, readOnly...)
	if c.Sandbox.confines() {
		triple, ok := cadMultiarchLib[runtime.GOARCH]
		if !ok {
			return engineExec{}, fmt.Errorf("cad: no multiarch library directory is known for GOARCH %s", runtime.GOARCH)
		}
		ro = append(ro, "/usr/lib/"+triple)
	}
	env := append(engineEnv(jobDir), "LP_NUM_THREADS=2")
	return c.Sandbox.forJob(jobDir, ro, engines).withEnv(env), nil
}
