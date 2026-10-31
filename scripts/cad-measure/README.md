# CAD dependency updates

The measurement tools and CAD worker share pinned Python packages. Keep their
generated locks and probe checks in the same dependency-update PR.

## Dependabot security PRs

The `exclude-paths` rules in [dependabot.yml](../../.github/dependabot.yml) exclude
the fixed OCP/VTK benchmark variants from scheduled version updates only.
[GitHub does not apply this option to security updates](https://docs.github.com/en/code-security/reference/supply-chain-security/dependabot-options-reference#exclude-paths).
Security PRs can therefore change `requirements-ocp-*.txt` or `requirements-vtk.txt`.

Before merging any CAD dependency PR, including a security PR:

1. Apply the intended version changes to the generator inputs: `requirements-common.in`
   for common packages, or `OCP_NEW`, `OCP_CAND` and `VTK_VERSION` in `lock.sh` for
   benchmark variants. For an OCP change, verify the selected wheel hashes independently
   and update the corresponding `EXPECT_OCP_*_AARCH64` values. Update references to
   versioned filenames if a variant changes. Regenerating from old inputs would undo
   the proposed security fix.
2. Run the generator from the repository root. It needs Docker, curl and jq, and
   downloads wheels with `--require-hashes` to validate the generated locks:

   ```bash
   bash scripts/cad-measure/lock.sh
   ```

3. Synchronize the worker locks from those generated files. The DXF worker uses
   every common block except NumPy; the mesh worker uses NumPy and the selected
   production OCP variant. Preserve the source comments as well as versions and hashes:

   ```bash
   common=scripts/cad-measure/requirements-common.txt
   ocp_version=$(sed -n 's/^OCP_NEW=//p' scripts/cad-measure/lock.sh)
   awk '/^[A-Za-z0-9_.-]+==/{keep = ($0 !~ /^numpy==/)} keep' "$common" \
     > cmd/cad-worker/requirements-dxf.txt
   {
     awk '/^[A-Za-z0-9_.-]+==/{keep = ($0 ~ /^numpy==/)} keep' "$common"
     cat "scripts/cad-measure/requirements-ocp-$ocp_version.txt"
   } > cmd/cad-worker/requirements-mesh.txt
   cp scripts/cad-measure/requirements-vtk.txt cmd/cad-worker/requirements-vtk.txt
   ```

4. Review the diff, then record the validated common-lock SHA in `LOCK_COMMON_SHA`
   in `scripts/cad-spike.sh`. If OCP changed, also update its `LOCK_OCP` path and
   `LOCK_OCP_SHA`. Update the expected DXF hash count in `scripts/cad-dxf-probe.sh`
   if wheel coverage changed. For VTK updates, update the version and wheel-count
   checks in `scripts/cad-render-probe.sh` too. Do not remove these checks to make
   an update pass.
5. Run the offline preflights and regression tests:

   ```bash
   bash scripts/cad-dxf-probe.sh locks
   bash scripts/cad-spike.sh locks
   go test ./internal/repohygiene -run TestCADLockPreflights -count=1
   ```

The `locks` commands do not need Docker and do not write measurement evidence.
They check consistency, not rendering or geometry. For package-version changes,
rebuild the CAD worker and run the relevant DXF, mesh or render probes on a native
arm64 Docker host before merging. Record new measurements separately from the
historical benchmark evidence; a security update does not validate old results
against a new OCP/VTK version.
