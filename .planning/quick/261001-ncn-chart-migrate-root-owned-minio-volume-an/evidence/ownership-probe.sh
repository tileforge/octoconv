#!/usr/bin/env bash
set -euo pipefail

CHART=deploy/chart/octoconv
VALUES=$CHART/values-local.yaml
REF=$(yq '.minio.image' "$CHART/values.yaml")
CONTENT="octoconv-ncn legacy object written by the old root image"
SEEDCTRS="octoconv-ncn-minio octoconv-ncn-control octoconv-ncn-init octoconv-ncn-snap octoconv-ncn-seed"
VOLS="octoconv-ncn-legacy octoconv-ncn-fresh octoconv-ncn-boundary"
STEP=start

step() { STEP="$1"; echo; echo "=== STEP $1"; }
fail() { echo "FAIL at step $STEP: $*"; exit 1; }

cleanup() {
  local n
  for n in $SEEDCTRS; do docker rm -f "$n" >/dev/null 2>&1 || true; done
  for n in $VOLS; do docker volume rm "$n" >/dev/null 2>&1 || true; done
}

step "b: name guard"
for n in $SEEDCTRS; do
  if [ -n "$(docker ps -aq --filter "name=^${n}\$")" ]; then echo "container $n already exists, refusing to touch it"; exit 1; fi
done
for n in $VOLS; do
  if docker volume ls -q --filter "name=^${n}\$" | grep -q .; then echo "volume $n already exists, refusing to touch it"; exit 1; fi
done
trap cleanup EXIT
echo "no octoconv-ncn-* containers or volumes present"

step "a: build docker flags from the helm render"
echo "REF=$REF"
RENDER=$(helm template octoconv "$CHART" -f "$VALUES" --show-only templates/minio.yaml)
NINIT=$(printf '%s\n' "$RENDER" | yq 'select(.kind=="Deployment") | .spec.template.spec.initContainers[] | select(.volumeMounts[].name=="minio-data") | .name' | grep -c . || true)
[ "$NINIT" = 1 ] || fail "expected exactly 1 init container mounting minio-data in the render, got $NINIT"
INIT=$(printf '%s\n' "$RENDER" | yq 'select(.kind=="Deployment") | .spec.template.spec.initContainers[] | select(.volumeMounts[].name=="minio-data")')
INITIMG=$(printf '%s\n' "$INIT" | yq '.image')
[ "$INITIMG" = "$REF" ] || fail "init image $INITIMG is not minio.image $REF"
SRVMOUNT=$(printf '%s\n' "$RENDER" | yq 'select(.kind=="Deployment") | .spec.template.spec.containers[] | select(.name=="minio") | .volumeMounts[] | select(.name=="minio-data") | .mountPath')
MOUNT=$(printf '%s\n' "$INIT" | yq '.volumeMounts[] | select(.name=="minio-data") | .mountPath')
[ "$MOUNT" = "$SRVMOUNT" ] || fail "init mountPath $MOUNT differs from server mountPath $SRVMOUNT"

U=$(printf '%s\n' "$INIT" | yq '.securityContext.runAsUser')
MEM=$(printf '%s\n' "$INIT" | yq '.resources.limits.memory')
CPU=$(printf '%s\n' "$INIT" | yq '.resources.limits.cpu')
case "$MEM" in *Mi) DMEM="${MEM%Mi}m" ;; *) fail "unhandled memory unit $MEM" ;; esac
case "$CPU" in *m) DCPU=$(awk -v m="${CPU%m}" 'BEGIN{printf "%.3f", m/1000}') ;; *) fail "unhandled cpu unit $CPU" ;; esac

INIT_FLAGS=(--network none -u "$U" --memory "$DMEM" --cpus "$DCPU")
while IFS= read -r c; do [ -n "$c" ] && INIT_FLAGS+=(--cap-drop "$c"); done < <(printf '%s\n' "$INIT" | yq '.securityContext.capabilities.drop[]')
while IFS= read -r c; do [ -n "$c" ] && INIT_FLAGS+=(--cap-add "$c"); done < <(printf '%s\n' "$INIT" | yq '.securityContext.capabilities.add[]')
[ "$(printf '%s\n' "$INIT" | yq '.securityContext.allowPrivilegeEscalation')" = false ] && INIT_FLAGS+=(--security-opt no-new-privileges)
[ "$(printf '%s\n' "$INIT" | yq '.securityContext.readOnlyRootFilesystem')" = true ] && INIT_FLAGS+=(--read-only)

CMD=()
while IFS= read -r c; do CMD+=("$c"); done < <(printf '%s\n' "$INIT" | yq '.command[]')
[ "${#CMD[@]}" -ge 2 ] || fail "init command too short"
[ "${CMD[${#CMD[@]}-1]}" = "$MOUNT" ] || fail "last command argument ${CMD[${#CMD[@]}-1]} is not the mountPath $MOUNT"
ENTRY="${CMD[0]}"
ARGS=("${CMD[@]:1}")

run_init() {
  docker run --rm --name octoconv-ncn-init "${INIT_FLAGS[@]}" -v "$1:$MOUNT" --entrypoint "$ENTRY" "$REF" "${ARGS[@]}"
}
echo "docker run --rm --name octoconv-ncn-init ${INIT_FLAGS[*]} -v VOL:$MOUNT --entrypoint $ENTRY $REF ${ARGS[*]}"

snap() {
  docker run --rm --name octoconv-ncn-snap --network none -u 0 -v "$1:/data" --entrypoint /bin/bash "$REF" -c \
    'shopt -s globstar dotglob nullglob; stat -c "%u:%g|%a|%z|%i|%n" /data /data/**' | LC_ALL=C sort -t'|' -k5
}

check_migration() {
  awk -F'|' -v want="$3" '
    NR==FNR { b[$5]=$0; split($1,bo,":"); bu[$5]=bo[1]; bm[$5]=$2; bi[$5]=$4; next }
    { a[$5]=$0; split($1,ao,":"); au[$5]=ao[1]; ag[$5]=ao[2]; am[$5]=$2; ai[$5]=$4 }
    END {
      bad=0; migrated=0; kept=0
      for (p in b) {
        if (!(p in a)) { print "MISSING after: " p; bad=1; continue }
        if (bu[p]=="0") {
          migrated++
          if (au[p]!="65532" || ag[p]!="65532") { print "NOT MIGRATED: " b[p] " -> " a[p]; bad=1 }
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
docker run --rm --name octoconv-ncn-seed --network none -u 0 -e HOME=/tmp \
  -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin -e CONTENT="$CONTENT" \
  -v octoconv-ncn-legacy:/seed --entrypoint /bin/bash "$REF" -c '
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
docker run --rm --name octoconv-ncn-seed --network none -u 0 -v octoconv-ncn-legacy:/data --entrypoint /bin/bash "$REF" -c '
touch /data/third-uid-file /data/already-65532
chown 1234:1234 /data/third-uid-file
chown 65532:65532 /data/already-65532
echo "added a third-uid file and an already-65532 file (must stay untouched)"'
snap octoconv-ncn-legacy > "$SNAPDIR/legacy-0"
cat "$SNAPDIR/legacy-0"
NONROOT0=$(awk -F'|' '$5!="/data" && $1 !~ /^0:/' "$SNAPDIR/legacy-0" | grep -v 'third-uid-file\|already-65532' || true)
[ -z "$NONROOT0" ] || fail "fixture has non-uid-0 elements besides the two planted ones: $NONROOT0"
echo "OK: every element written by minio is uid 0"

step "e: control - the pinned non-root MinIO on that volume must die"
docker run -d --name octoconv-ncn-control --network none -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin \
  -v octoconv-ncn-legacy:/data "$REF" server /data --console-address :9001 >/dev/null
set +e
docker exec -e HOME=/tmp octoconv-ncn-control bash -c "$READY_LOOP"
CRC=$?
set -e
RUNNING=$(docker inspect -f '{{.State.Running}} {{.State.ExitCode}}' octoconv-ncn-control)
echo "exec rc=$CRC state(running exitcode)=$RUNNING"
docker logs octoconv-ncn-control 2>&1 | tail -15
[ "$CRC" != 0 ] || fail "control MinIO became ready: the fixture does not reproduce the defect"
case "$RUNNING" in "false "[1-9]*) ;; *) fail "control container is not dead with a non-zero exit: $RUNNING" ;; esac
docker logs octoconv-ncn-control 2>&1 | grep -q 'Unable to write to the backend' || fail "control log lacks 'Unable to write to the backend'"
echo "OK: control reproduces the defect"
docker rm -f octoconv-ncn-control >/dev/null

step "f: migration with the rendered init container"
snap octoconv-ncn-legacy > "$SNAPDIR/legacy-before"
run_init octoconv-ncn-legacy
echo "init exit code 0"
snap octoconv-ncn-legacy > "$SNAPDIR/legacy-after"
echo "--- after:"
cat "$SNAPDIR/legacy-after"
check_migration "$SNAPDIR/legacy-before" "$SNAPDIR/legacy-after" 5 || fail "migration assertions"
grep -q '^1234:1234|.*/data/third-uid-file$' "$SNAPDIR/legacy-after" || fail "third-uid file lost its owner"
echo "OK: every uid 0 element is now 65532:65532 with the same mode and inode; others untouched (uid:gid, ctime, inode)"

step "g: pinned MinIO on the migrated volume, as in compose"
docker run -d --name octoconv-ncn-minio --network none -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin \
  -v octoconv-ncn-legacy:/data "$REF" server /data --console-address :9001 >/dev/null
docker exec -e HOME=/tmp octoconv-ncn-minio bash -c "$READY_LOOP" || { docker logs octoconv-ncn-minio 2>&1 | tail -20; fail "MinIO not ready on the migrated volume"; }
docker exec -e HOME=/tmp octoconv-ncn-minio mc alias set p http://localhost:9000 minioadmin minioadmin
WANT=$(printf '%s' "$CONTENT" | shasum -a 256 | cut -d' ' -f1)
GOT=$(docker exec -e HOME=/tmp octoconv-ncn-minio mc cat p/probe/f.txt | shasum -a 256 | cut -d' ' -f1)
[ "$WANT" = "$GOT" ] || fail "old object differs: want $WANT got $GOT"
echo "OK: old object byte-for-byte ($GOT)"
printf '%s' "new object after migration" | docker exec -i -e HOME=/tmp octoconv-ncn-minio mc pipe p/probe/new.txt
GOT2=$(docker exec -e HOME=/tmp octoconv-ncn-minio mc cat p/probe/new.txt)
[ "$GOT2" = "new object after migration" ] || fail "new object read back as: $GOT2"
LS=$(docker exec -e HOME=/tmp octoconv-ncn-minio mc ls p/probe)
echo "$LS"
printf '%s\n' "$LS" | grep -q 'f.txt' || fail "mc ls lacks f.txt"
printf '%s\n' "$LS" | grep -q 'new.txt' || fail "mc ls lacks new.txt"
UIDNOW=$(docker exec octoconv-ncn-minio id -u)
[ "$UIDNOW" = 65532 ] || fail "server runs as uid $UIDNOW"
echo "OK: ready, old object intact, new write accepted, server uid $UIDNOW"
docker rm -f octoconv-ncn-minio >/dev/null

step "h: idempotence - a second run changes nothing (owner, ctime with ns, inode)"
snap octoconv-ncn-legacy > "$SNAPDIR/idem-before"
run_init octoconv-ncn-legacy
snap octoconv-ncn-legacy > "$SNAPDIR/idem-after"
diff "$SNAPDIR/idem-before" "$SNAPDIR/idem-after" || fail "second init run changed the volume"
echo "OK: snapshots identical ($(wc -l < "$SNAPDIR/idem-after" | tr -d ' ') elements)"

step "i: fresh volume, symlink out of the volume, regrouped file, read-only rootfs"
docker run --rm --name octoconv-ncn-seed --network none -u 0 -v octoconv-ncn-fresh:/data --entrypoint /bin/bash "$REF" -c '
ln -s /etc/passwd /data/link
touch /data/regrouped
chown 0:65532 /data/regrouped'
snap octoconv-ncn-fresh > "$SNAPDIR/fresh-before"
cat "$SNAPDIR/fresh-before"
grep -q '^0:0|.*|/data/link$' "$SNAPDIR/fresh-before" || fail "fixture symlink is not uid 0"
grep -q '^0:65532|.*|/data/regrouped$' "$SNAPDIR/fresh-before" || fail "fixture regrouped file is not 0:65532"
run_init octoconv-ncn-fresh
echo "init exit code 0 with the symlink present and the root filesystem read-only"
snap octoconv-ncn-fresh > "$SNAPDIR/fresh-after"
echo "--- after:"
cat "$SNAPDIR/fresh-after"
check_migration "$SNAPDIR/fresh-before" "$SNAPDIR/fresh-after" 2 || fail "fresh-volume migration assertions"
grep -q '^65532:65532|.*|/data/link$' "$SNAPDIR/fresh-after" || fail "symlink itself was not handed over"
grep -q '^65532:65532|.*|/data/regrouped$' "$SNAPDIR/fresh-after" || fail "regrouped file was not handed over"
docker run --rm --name octoconv-ncn-snap --network none -u 0 --entrypoint /bin/bash "$REF" -c 'stat -c "%u:%g %n" /etc/passwd'
run_init octoconv-ncn-fresh
snap octoconv-ncn-fresh > "$SNAPDIR/fresh-after2"
diff "$SNAPDIR/fresh-after" "$SNAPDIR/fresh-after2" || fail "second init run on the fresh volume changed it"
echo "OK: link (lstat) and regrouped handed over, symlink target untouched, second run is a no-op"

step "j: boundary - a subtree root without DAC capabilities cannot read must fail loudly"
docker run --rm --name octoconv-ncn-seed --network none -u 0 -v octoconv-ncn-boundary:/seed --entrypoint /bin/bash "$REF" -c '
mkdir /seed/locked
touch /seed/locked/inner
chmod 0700 /seed/locked
chown 1234:1234 /seed/locked
chown 0:0 /seed/locked/inner'
set +e
BOUT=$(run_init octoconv-ncn-boundary 2>&1)
BRC=$?
set -e
echo "$BOUT"
echo "init exit code: $BRC"
[ "$BRC" != 0 ] || fail "init reported success on a subtree it could not read"
printf '%s\n' "$BOUT" | grep -qiE 'permission denied|cannot read directory' || fail "no permission message in the output"
echo "OK: measured boundary - unreadable subtree gives a non-zero exit and a Permission denied message"

echo
echo "RESULT: PASS"
