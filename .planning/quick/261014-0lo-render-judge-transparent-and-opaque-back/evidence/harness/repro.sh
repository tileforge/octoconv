#!/usr/bin/env bash
set -euo pipefail
LO=/private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad/lo
REPO=/Users/apaderin/dev/octoconv
REV=${1:?commit}
OUT=${2:?evidence file}
N4="$LO/out/step-nauo-LinearSlide-MGN15x-Block-n4.step"
API=http://127.0.0.1:18090
rm -rf "$LO/rp"
mkdir -p "$LO/rp"
git -C "$REPO" bundle create "$LO/rp/rp.bundle" gsd/phase-76-measured-bounds-keda-proof-client-docs
git clone -q "$LO/rp/rp.bundle" "$LO/rp/octoconv"
cd "$LO/rp/octoconv"
git checkout -q "$REV"
cp "$REPO/deploy/tectonic-bundle/octoconv-bundle.ttb" deploy/tectonic-bundle/octoconv-bundle.ttb
cat > docker-compose.override.yml <<'YML'
services:
  api:
    ports: !override
      - "127.0.0.1:18090:8090"
YML
export GOTOOLCHAIN=go1.26.4
{
	echo "HDR,date_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ),commit=$(git rev-parse HEAD),tracked_dirt=$(git status --porcelain --untracked-files=no | wc -l | tr -d ' ')"
	echo "HDR,input=$(basename "$N4"),bytes=$(wc -c < "$N4" | tr -d ' '),sha256=$(shasum -a 256 "$N4" | cut -d' ' -f1)"
} > "$OUT"
UP_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)
docker compose up -d --build postgres redis minio createbucket api cad-worker >&2
for _ in $(seq 1 90); do curl -fsS -o /dev/null "$API/healthz" 2>/dev/null && break; sleep 2; done
for _ in $(seq 1 90); do
	docker compose logs --no-color --since "$UP_AT" cad-worker 2>/dev/null | grep -q "cad sandbox proven" && break
	sleep 2
done
docker compose logs --no-color --since "$UP_AT" cad-worker 2>/dev/null | grep -q "cad sandbox proven" || { echo "no sandbox proven line" >> "$OUT"; exit 1; }
echo "HDR,cad_worker_image=$(docker inspect -f '{{.Image}}' octoconv-cad-worker),api_image=$(docker inspect -f '{{.Image}}' octoconv-api)" >> "$OUT"
KEY=$(DATABASE_URL=postgres://octo:octo-pass@127.0.0.1:5434/octo_db API_KEY_SALT=dev-only-change-me-in-real-deploys go run ./cmd/manage-clients create "repro-0lo-$(date +%s)" | awk -F': ' '/^api key/ { print $2 }')
[ -n "$KEY" ]
for bg in transparent opaque; do
	curl -sS -o "$LO/rp/create.json" -H "Authorization: ApiKey $KEY" -X POST "$API/v1/jobs" -F target=png -F "file=@$N4;filename=step-nauo-n4.step" -F operation=render -F "opts={\"background\":\"$bg\"}"
	ID=$(jq -r .job_id "$LO/rp/create.json")
	for _ in $(seq 1 150); do
		curl -sS -o "$LO/rp/job.json" -H "Authorization: ApiKey $KEY" "$API/v1/jobs/$ID"
		case "$(jq -r .status "$LO/rp/job.json")" in done | failed | canceled) break ;; esac
		sleep 2
	done
	ATTEMPTS=$(docker compose exec -T postgres psql -U octo -d octo_db -At -c "select count(*) filter (where to_status = 'active') from job_events where job_id = '$ID'")
	echo "REPRO,background=$bg,job=$ID,status=$(jq -r .status "$LO/rp/job.json"),code=$(jq -r '.error_code // "none"' "$LO/rp/job.json"),message=$(jq -r '.error_message // "none"' "$LO/rp/job.json" | tr ',' ';'),outputs=$(jq '.outputs | length' "$LO/rp/job.json" 2>/dev/null || echo 0),active_transitions=$ATTEMPTS" >> "$OUT"
done
docker compose logs --no-color --since "$UP_AT" cad-worker 2>/dev/null | grep -i "content check" | tail -3 | sed 's/^/WORKER_LOG,/' | tr ',' ';' | sed 's/^WORKER_LOG;/WORKER_LOG,/' >> "$OUT" || true
cat "$OUT"
