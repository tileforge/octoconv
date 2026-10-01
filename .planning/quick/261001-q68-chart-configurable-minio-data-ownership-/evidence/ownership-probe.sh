#!/usr/bin/env bash
set -euo pipefail

MODE="${1:-}"
CHART=deploy/chart/octoconv
VALUES=$CHART/values-local.yaml
REF=$(yq '.minio.image' "$CHART/values.yaml")
case "$MODE" in
  default) SETARGS=(); TU=65532; TG=65532 ;;
  custom) SETARGS=(--set minio.dataOwnership.uid=1000 --set minio.dataOwnership.gid=1001); TU=1000; TG=1001 ;;
  *) echo "usage: ownership-probe.sh default|custom"; exit 2 ;;
esac
TGT="$TU:$TG"
CONTENT="octoconv-q68 legacy object written by the old root image"
SEEDCTRS="octoconv-q68-minio octoconv-q68-control octoconv-q68-init octoconv-q68-snap octoconv-q68-seed"
LEGACY="octoconv-q68-$MODE-legacy"
FRESH="octoconv-q68-$MODE-fresh"
BOUNDARY="octoconv-q68-$MODE-boundary"
VOLS="$LEGACY $FRESH $BOUNDARY"
STEP=start

step() { STEP="$1"; echo; echo "=== STEP $1 (mode $MODE, target $TGT)"; }
fail() { echo "FAIL at step $STEP: $*"; exit 1; }

cleanup() {
  local n
  for n in $SEEDCTRS; do docker rm -f "$n" >/dev/null 2>&1 || true; done
  for n in $VOLS; do docker volume rm "$n" >/dev/null 2>&1 || true; done
}

step "b: name guard and image presence"
docker image inspect "$REF" >/dev/null 2>&1 || { echo "image $REF is not present locally, refusing to pull"; exit 1; }
for n in $SEEDCTRS; do
  if [ -n "$(docker ps -aq --filter "name=^${n}\$")" ]; then echo "container $n already exists, refusing to touch it"; exit 1; fi
done
for n in $VOLS; do
  if docker volume ls -q --filter "name=^${n}\$" | grep -q .; then echo "volume $n already exists, refusing to touch it"; exit 1; fi
done
trap cleanup EXIT
echo "no octoconv-q68-* containers or volumes present, image $REF present"

step "a: build docker flags from the helm render"
echo "REF=$REF"
RENDER=$(helm template octoconv "$CHART" -f "$VALUES" ${SETARGS[@]+"${SETARGS[@]}"} --show-only templates/minio.yaml)
NINIT=$(printf '%s\n' "$RENDER" | yq 'select(.kind=="Deployment") | .spec.template.spec.initContainers[] | select(.volumeMounts[].name=="minio-data") | .name' | grep -c . || true)
[ "$NINIT" = 1 ] || fail "expected exactly 1 init container mounting minio-data in the render, got $NINIT"
POD=$(printf '%s\n' "$RENDER" | yq 'select(.kind=="Deployment") | .spec.template.spec')
INIT=$(printf '%s\n' "$RENDER" | yq 'select(.kind=="Deployment") | .spec.template.spec.initContainers[] | select(.volumeMounts[].name=="minio-data")')
SRV=$(printf '%s\n' "$RENDER" | yq 'select(.kind=="Deployment") | .spec.template.spec.containers[] | select(.name=="minio")')
INITIMG=$(printf '%s\n' "$INIT" | yq '.image')
[ "$INITIMG" = "$REF" ] || fail "init image $INITIMG is not minio.image $REF"
SRVMOUNT=$(printf '%s\n' "$SRV" | yq '.volumeMounts[] | select(.name=="minio-data") | .mountPath')
MOUNT=$(printf '%s\n' "$INIT" | yq '.volumeMounts[] | select(.name=="minio-data") | .mountPath')
[ "$MOUNT" = "$SRVMOUNT" ] || fail "init mountPath $MOUNT differs from server mountPath $SRVMOUNT"

PODU=$(printf '%s\n' "$POD" | yq '.securityContext.runAsUser')
PODG=$(printf '%s\n' "$POD" | yq '.securityContext.runAsGroup')
FSG=$(printf '%s\n' "$POD" | yq '.securityContext.fsGroup')
[ "$PODU:$PODG" = "$TGT" ] || fail "pod runAsUser:runAsGroup is $PODU:$PODG, requested $TGT"
[ "$FSG" = "$TG" ] || fail "pod fsGroup is $FSG, requested gid $TG"

IU=$(printf '%s\n' "$INIT" | yq '.securityContext.runAsUser // '"$PODU")
IG=$(printf '%s\n' "$INIT" | yq '.securityContext.runAsGroup // '"$PODG")
MEM=$(printf '%s\n' "$INIT" | yq '.resources.limits.memory')
CPU=$(printf '%s\n' "$INIT" | yq '.resources.limits.cpu')
case "$MEM" in *Mi) DMEM="${MEM%Mi}m" ;; *) fail "unhandled memory unit $MEM" ;; esac
case "$CPU" in *m) DCPU=$(awk -v m="${CPU%m}" 'BEGIN{printf "%.3f", m/1000}') ;; *) fail "unhandled cpu unit $CPU" ;; esac

INIT_FLAGS=(--pull never --network none -u "$IU:$IG" --group-add "$FSG" --memory "$DMEM" --cpus "$DCPU")
while IFS= read -r c; do [ -n "$c" ] && INIT_FLAGS+=(--cap-drop "$c"); done < <(printf '%s\n' "$INIT" | yq '.securityContext.capabilities.drop[]')
while IFS= read -r c; do [ -n "$c" ] && INIT_FLAGS+=(--cap-add "$c"); done < <(printf '%s\n' "$INIT" | yq '.securityContext.capabilities.add[]')
[ "$(printf '%s\n' "$INIT" | yq '.securityContext.allowPrivilegeEscalation')" = false ] && INIT_FLAGS+=(--security-opt no-new-privileges)
[ "$(printf '%s\n' "$INIT" | yq '.securityContext.readOnlyRootFilesystem')" = true ] && INIT_FLAGS+=(--read-only)

CMD=()
while IFS= read -r c; do CMD+=("$c"); done < <(printf '%s\n' "$INIT" | yq '.command[]')
[ "${#CMD[@]}" -ge 2 ] || fail "init command too short"
[ "${CMD[${#CMD[@]}-1]}" = "$MOUNT" ] || fail "last command argument ${CMD[${#CMD[@]}-1]} is not the mountPath $MOUNT"
OWNERARG=$(printf '%s\n' "${CMD[@]}" | grep -E '^[0-9]+:[0-9]+$' || true)
[ "$OWNERARG" = "$TGT" ] || fail "chown target in the init command is [$OWNERARG], requested $TGT"
ENTRY="${CMD[0]}"
ARGS=("${CMD[@]:1}")

SRV_FLAGS=(--pull never --network none -u "$PODU:$PODG" --group-add "$FSG")
while IFS= read -r c; do [ -n "$c" ] && SRV_FLAGS+=(-e "$c"); done < <(printf '%s\n' "$SRV" | yq '.env[] | .name + "=" + .value')
SRV_ARGS=()
while IFS= read -r c; do SRV_ARGS+=("$c"); done < <(printf '%s\n' "$SRV" | yq '.args[]')

INIT_EXPECT_U=0; INIT_EXPECT_G=0
[ "$IU:$IG" = "$INIT_EXPECT_U:$INIT_EXPECT_G" ] || fail "init identity is $IU:$IG, the render must pin 0:0"
if [ "$MODE" = default ]; then
  [[ " ${INIT_FLAGS[*]} " == *" -u 0:0 --group-add 65532 "* ]] || fail "default init flags: ${INIT_FLAGS[*]}"
  [[ " ${SRV_FLAGS[*]} " == *" -u 65532:65532 --group-add 65532 "* ]] || fail "default server flags: ${SRV_FLAGS[*]}"
fi

run_init() {
  docker run --rm --name octoconv-q68-init "${INIT_FLAGS[@]}" -v "$1:$MOUNT" --entrypoint "$ENTRY" "$REF" "${ARGS[@]}"
}
echo "init:   docker run --rm --name octoconv-q68-init ${INIT_FLAGS[*]} -v VOL:$MOUNT --entrypoint $ENTRY $REF ${ARGS[*]}"
echo "server: docker run -d --name octoconv-q68-minio ${SRV_FLAGS[*]} -v VOL:$MOUNT $REF ${SRV_ARGS[*]}"

snap() {
  docker run --rm --pull never --name octoconv-q68-snap --network none -u 0 -v "$1:/data" --entrypoint /bin/bash "$REF" -c \
    'shopt -s globstar dotglob nullglob; stat -c "%u:%g|%a|%z|%i|%n" /data /data/**' | LC_ALL=C sort -t'|' -k5
}

check_migration() {
  awk -F'|' -v want="$3" -v tu="$TU" -v tg="$TG" '
    NR==FNR { b[$5]=$0; split($1,bo,":"); bu[$5]=bo[1]; bm[$5]=$2; bi[$5]=$4; next }
    { a[$5]=$0; split($1,ao,":"); au[$5]=ao[1]; ag[$5]=ao[2]; am[$5]=$2; ai[$5]=$4 }
    END {
      bad=0; migrated=0; kept=0
      for (p in b) {
        if (!(p in a)) { print "MISSING after: " p; bad=1; continue }
        if (bu[p]=="0") {
          migrated++
          if (au[p]!=tu || ag[p]!=tg) { print "NOT MIGRATED: " b[p] " -> " a[p]; bad=1 }
          if (am[p]!=bm[p] || ai[p]!=bi[p]) { print "MODE/INODE CHANGED: " b[p] " -> " a[p]; bad=1 }
        } else {
          kept++
          if (a[p]!=b[p]) { print "NON-ROOT ELEMENT TOUCHED: " b[p] " -> " a[p]; bad=1 }
        }
      }
      for (p in a) { if (!(p in b)) { print "NEW after: " p; bad=1 }; if (au[p]=="0") { print "STILL uid 0: " a[p]; bad=1 } }
      printf "migrated=%d untouched=%d\n", migrated, kept
      if (want!="" && migrated<want+0) { print "expected at least " want " migrated elements"; bad=1 }
      exit bad
    }' "$1" "$2"
}

SNAPDIR=$(mktemp -d)
trap 'cleanup; rm -rf "$SNAPDIR"' EXIT

READY_LOOP='for ((i=0;i<30;i++)); do if mc ready local >/dev/null 2>&1; then echo "ready after ${i}s"; exit 0; fi; sleep 1; done; echo "not ready after 30s"; exit 1'

step "d: fixture - volume written by the old root image (seeded via /seed so docker copies nothing)"
docker run --rm --pull never --name octoconv-q68-seed --network none -u 0 -e HOME=/tmp \
  -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin -e CONTENT="$CONTENT" \
  -v "$LEGACY":/seed --entrypoint /bin/bash "$REF" -c '
set -u
chmod 0777 /seed
minio server /seed --console-address :9001 >/tmp/minio.log 2>&1 &
srv=$!
ok=0
for ((i=0;i<30;i++)); do
  kill -0 $srv 2>/dev/null || { echo "server died"; cat /tmp/minio.log; exit 1; }
  if mc ready local >/dev/null 2>&1; then ok=1; break; fi
  sleep 1
done
[ $ok = 1 ] || { echo "server never became ready"; cat /tmp/minio.log; exit 1; }
mc alias set p http://localhost:9000 minioadmin minioadmin
mc mb p/probe
printf "%s" "$CONTENT" | mc pipe p/probe/f.txt
mc ls p/probe
kill $srv
wait $srv || true
echo "fixture written, server stopped"
'
docker run --rm --pull never --name octoconv-q68-seed --network none -u 0 -e TU="$TU" -e TG="$TG" -v "$LEGACY":/data --entrypoint /bin/bash "$REF" -c '
touch /data/third-uid-file /data/already-65532
chown 1234:1234 /data/third-uid-file
chown 65532:65532 /data/already-65532
if [ "$TU:$TG" != "65532:65532" ]; then
  touch /data/already-target
  chown "$TU:$TG" /data/already-target
fi
echo "added a third-uid file, an already-65532 file and (custom only) an already-target file: all must stay untouched"'
snap "$LEGACY" > "$SNAPDIR/legacy-0"
cat "$SNAPDIR/legacy-0"
NONROOT0=$(awk -F'|' '$5!="/data" && $1 !~ /^0:/' "$SNAPDIR/legacy-0" | grep -v 'third-uid-file\|already-65532\|already-target' || true)
[ -z "$NONROOT0" ] || fail "fixture has non-uid-0 elements besides the planted ones: $NONROOT0"
echo "OK: every element written by minio is uid 0"

step "e: control - the non-root MinIO on that volume must die"
docker run -d --name octoconv-q68-control "${SRV_FLAGS[@]}" \
  -v "$LEGACY":/data "$REF" "${SRV_ARGS[@]}" >/dev/null
set +e
docker exec -e HOME=/tmp octoconv-q68-control bash -c "$READY_LOOP"
CRC=$?
set -e
RUNNING=$(docker inspect -f '{{.State.Running}} {{.State.ExitCode}}' octoconv-q68-control)
echo "exec rc=$CRC state(running exitcode)=$RUNNING"
docker logs octoconv-q68-control 2>&1 | tail -15
[ "$CRC" != 0 ] || fail "control MinIO became ready: the fixture does not reproduce the defect"
case "$RUNNING" in "false "[1-9]*) ;; *) fail "control container is not dead with a non-zero exit: $RUNNING" ;; esac
docker logs octoconv-q68-control 2>&1 | grep -q 'Unable to write to the backend' || fail "control log lacks 'Unable to write to the backend'"
echo "OK: control reproduces the defect"
docker rm -f octoconv-q68-control >/dev/null

step "f: migration with the rendered init container"
snap "$LEGACY" > "$SNAPDIR/legacy-before"
run_init "$LEGACY"
echo "init exit code 0"
snap "$LEGACY" > "$SNAPDIR/legacy-after"
echo "--- after:"
cat "$SNAPDIR/legacy-after"
check_migration "$SNAPDIR/legacy-before" "$SNAPDIR/legacy-after" 5 || fail "migration assertions"
grep -q '^1234:1234|.*/data/third-uid-file$' "$SNAPDIR/legacy-after" || fail "third-uid file lost its owner"
grep -q '^65532:65532|.*/data/already-65532$' "$SNAPDIR/legacy-after" || fail "already-65532 file was rewritten"
if [ "$MODE" = custom ]; then
  grep -q "^$TGT|.*/data/already-target\$" "$SNAPDIR/legacy-after" || fail "already-target file was rewritten"
  if grep -q '^65532:65532|.*/data/already-65532$' "$SNAPDIR/legacy-after"; then echo "OK (measured boundary): a file already owned by 65532 is NOT rewritten to $TGT"; fi
fi
echo "OK: every uid 0 element is now $TGT with the same mode and inode; others untouched (uid:gid, ctime, inode)"

step "g: MinIO on the migrated volume, with the identity the render gives the pod"
docker run -d --name octoconv-q68-minio "${SRV_FLAGS[@]}" \
  -v "$LEGACY":/data "$REF" "${SRV_ARGS[@]}" >/dev/null
docker exec -e HOME=/tmp octoconv-q68-minio bash -c "$READY_LOOP" || { docker logs octoconv-q68-minio 2>&1 | tail -20; fail "MinIO not ready on the migrated volume"; }
docker exec -e HOME=/tmp octoconv-q68-minio mc alias set p http://localhost:9000 minioadmin minioadmin
WANT=$(printf '%s' "$CONTENT" | shasum -a 256 | cut -d' ' -f1)
GOT=$(docker exec -e HOME=/tmp octoconv-q68-minio mc cat p/probe/f.txt | shasum -a 256 | cut -d' ' -f1)
[ "$WANT" = "$GOT" ] || fail "old object differs: want $WANT got $GOT"
echo "OK: old object byte-for-byte ($GOT)"
printf '%s' "new object after migration" | docker exec -i -e HOME=/tmp octoconv-q68-minio mc pipe p/probe/new.txt
GOT2=$(docker exec -e HOME=/tmp octoconv-q68-minio mc cat p/probe/new.txt)
[ "$GOT2" = "new object after migration" ] || fail "new object read back as: $GOT2"
LS=$(docker exec -e HOME=/tmp octoconv-q68-minio mc ls p/probe)
echo "$LS"
printf '%s\n' "$LS" | grep -q 'f.txt' || fail "mc ls lacks f.txt"
printf '%s\n' "$LS" | grep -q 'new.txt' || fail "mc ls lacks new.txt"
UIDNOW=$(docker exec octoconv-q68-minio id -u)
GIDNOW=$(docker exec octoconv-q68-minio id -g)
[ "$UIDNOW" = "$TU" ] || fail "server runs as uid $UIDNOW, expected $TU"
[ "$GIDNOW" = "$TG" ] || fail "server runs as gid $GIDNOW, expected $TG"
echo "OK: ready, old object intact, new write accepted, server uid:gid $UIDNOW:$GIDNOW"
docker rm -f octoconv-q68-minio >/dev/null

step "h: idempotence - a second run changes nothing (owner, ctime with ns, inode)"
snap "$LEGACY" > "$SNAPDIR/idem-before"
run_init "$LEGACY"
snap "$LEGACY" > "$SNAPDIR/idem-after"
diff "$SNAPDIR/idem-before" "$SNAPDIR/idem-after" || fail "second init run changed the volume"
echo "OK: snapshots identical ($(wc -l < "$SNAPDIR/idem-after" | tr -d ' ') elements)"

step "i: fresh volume, symlink out of the volume, regrouped file, read-only rootfs"
docker run --rm --pull never --name octoconv-q68-seed --network none -u 0 -v "$FRESH":/data --entrypoint /bin/bash "$REF" -c '
ln -s /etc/passwd /data/link
touch /data/regrouped
chown 0:65532 /data/regrouped'
snap "$FRESH" > "$SNAPDIR/fresh-before"
cat "$SNAPDIR/fresh-before"
grep -q '^0:0|.*|/data/link$' "$SNAPDIR/fresh-before" || fail "fixture symlink is not uid 0"
grep -q '^0:65532|.*|/data/regrouped$' "$SNAPDIR/fresh-before" || fail "fixture regrouped file is not 0:65532"
run_init "$FRESH"
echo "init exit code 0 with the symlink present and the root filesystem read-only"
snap "$FRESH" > "$SNAPDIR/fresh-after"
echo "--- after:"
cat "$SNAPDIR/fresh-after"
check_migration "$SNAPDIR/fresh-before" "$SNAPDIR/fresh-after" 2 || fail "fresh-volume migration assertions"
grep -q "^$TGT|.*|/data/link\$" "$SNAPDIR/fresh-after" || fail "symlink itself was not handed over"
grep -q "^$TGT|.*|/data/regrouped\$" "$SNAPDIR/fresh-after" || fail "regrouped file was not handed over"
docker run --rm --pull never --name octoconv-q68-snap --network none -u 0 --entrypoint /bin/bash "$REF" -c 'stat -c "%u:%g %n" /etc/passwd'
run_init "$FRESH"
snap "$FRESH" > "$SNAPDIR/fresh-after2"
diff "$SNAPDIR/fresh-after" "$SNAPDIR/fresh-after2" || fail "second init run on the fresh volume changed it"
echo "OK: link (lstat) and regrouped handed over, symlink target untouched, second run is a no-op"

step "j: boundary - a subtree root without DAC capabilities cannot read must fail loudly"
docker run --rm --pull never --name octoconv-q68-seed --network none -u 0 -v "$BOUNDARY":/seed --entrypoint /bin/bash "$REF" -c '
mkdir /seed/locked
touch /seed/locked/inner
chmod 0700 /seed/locked
chown 1234:1234 /seed/locked
chown 0:0 /seed/locked/inner'
set +e
BOUT=$(run_init "$BOUNDARY" 2>&1)
BRC=$?
set -e
echo "$BOUT"
echo "init exit code: $BRC"
[ "$BRC" != 0 ] || fail "init reported success on a subtree it could not read"
printf '%s\n' "$BOUT" | grep -qiE 'permission denied|cannot read directory' || fail "no permission message in the output"
echo "OK: measured boundary - unreadable subtree gives a non-zero exit and a Permission denied message"

echo
echo "RESULT: PASS"
