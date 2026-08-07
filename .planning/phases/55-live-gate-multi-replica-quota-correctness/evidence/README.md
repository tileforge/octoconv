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

---

## gate-run4-FAILED-round4-20260807T141252Z.log + quota-multireplica-20260807T141252Z.tsv

The first full ten-round run after the in-cluster fallback landed. It reached round 4 of 10
(56 checks passed) and failed on `round 4: admitted count -- expected [2], got [3]`.

**This is most likely a defect in the GATE, not in the quota mechanism — but that is a
reconstruction, not a proven fact.** What the committed TSV does establish:

| round | requests | send spread |
|---|---|---|
| 1 | 10 | 9 ms |
| 2 | 10 | 1 ms |
| 3 | 10 | 92 ms |
| 4 | 10 | **159 ms** |

In round 4 the send offsets from the first request were:
`+0 +0 +1 +1 +1 +1 +2 +3 +4 **+159**` ms. Nine requests went out together inside 4 ms and
yielded **exactly 2 admits** — the cap held for the genuinely simultaneous set. The tenth
request left 155 ms after the others and was admitted, giving 3.

Timeline for that round: the first admit (`index 3`) was answered at `…026734`; the straggler
(`index 10`) was sent at `…026637` and answered at `…026944`, i.e. 310 ms after the first
admission response. The fixture is an 86-byte PNG and the image engine converts it in well
under that, so a slot very plausibly freed **legitimately** between the two decisions — the
concurrency cap bounds jobs concurrently in flight (`queued`+`active`, Phase 52 D-06/D-08),
not admissions per burst.

**What could NOT be verified:** the run tore the namespace down on exit (correctly — OrbStack
must never be left hot), so there is no job-status timeline proving `index 3` reached a
terminal status before `index 10`'s admission transaction ran. Only a further run can
discriminate "legitimate slot reuse" from "real cap violation".

### Two gate defects this evidences

1. **`BURST_SEND_SPREAD_MS=1000` is far too loose.** Nine requests within 4 ms plus one at
   +159 ms passes a 1000 ms bound, yet the straggler is objectively racing a different
   population. The bound must be tight enough that a job cannot terminate inside the window.
2. **`admitted == K` asserts more than the cap promises.** It is only sound while no admitted
   job can reach a terminal status during the burst. With an 86-byte fixture on the image
   engine that assumption does not hold.

Neither is fixed by relaxing the assertion to `<=`, which would reintroduce the vacuity the
`==` was chosen to prevent (a round admitting 0 would pass). The fix has to make the premise
true: shrink the burst window and/or make an admitted job outlive the burst.
