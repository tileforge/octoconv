# Phase 55 evidence

## calibration-FAILED-20260806T214551Z.log

The first operator run of `scripts/quota-multireplica-liveproof.sh --calibrate-only`.
It **failed at STEP 4, by design**, and that failure is the reason the harness's burst
driver had to move inside the cluster.

RESEARCH.md's assumption A1 — that OrbStack routes a Service ClusterIP from the macOS
host through the real kube-proxy DNAT path — **does not hold in this environment**:
0 of 20 probes to `http://192.168.194.162:8090/healthz` returned 200. Not "poorly
distributed" — unreachable outright. Independently confirmed against a service this
chart does not own: the `kubernetes` service in `default` (`192.168.194.129:443`) is
equally unreachable from the host, so the cause is the environment, not the chart.
NetworkPolicy is a documented non-cause (`templates/networkpolicy-metrics.yaml:20-23`
records that OrbStack ships no CNI NetworkPolicy controller).

Everything before STEP 4 passed, so the rest of the harness is validated by this run:
preflight (6 checks), the two-phase install that dodges the migration race,
`api spec.replicas == 2`, `api Service ready endpoints == 2`, both api pods discovered
(`api-6769bc54f7-h8nkz`, `api-6769bc54f7-p5pk7`), and the start-of-run pod set recorded.

The calibration step cost one install/uninstall cycle (~5 min) and caught a false
premise before the ten-round budget was spent. Had the gate been written without it,
this would have surfaced mid-run — or, worse, not surfaced at all if someone had
accepted 20 requests landing on one pod as normal.

No assertion was weakened in response. The remedy is the pre-documented Pattern 1
fallback: relocate the FIFO-barrier burst driver into a throwaway pod inside the
`octoconv` namespace, where Service routing is ordinary Kubernetes behavior.
