package repohygiene_test

import (
	"os/exec"
	"testing"
)

// Dependency updates must leave the CAD probes able to pass their setup checks.
// The locks mode uses the same checks as a real run, without Docker or evidence writes.
func TestCADLockPreflights(t *testing.T) {
	for _, script := range []string{"cad-dxf-probe.sh", "cad-spike.sh"} {
		t.Run(script, func(t *testing.T) {
			output, err := exec.Command("bash", "../../scripts/"+script, "locks").CombinedOutput()
			if err != nil {
				t.Fatalf("CAD lock preflight failed: %v\n%s", err, output)
			}
		})
	}
}
