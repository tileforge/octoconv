#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

BASE_IMAGE="debian:bookworm-slim@sha256:3783cc01769c7b2b1b83a5c5ad96c815348e28ed7da68e2e3687004faa906251"
LOCK_DIR="$REPO_ROOT/scripts/cad-measure"

COMMON_INPUT='ezdxf==1.4.4
numpy==2.4.6
pillow==12.3.0'
COMMON_ALLOWED="ezdxf numpy pillow fonttools pyparsing typing-extensions"

OCP_NEW=7.9.3.1.1
OCP_CAND=8.0.1.0.0
VTK_VERSION=9.7.1

EXPECT_OCP_NEW_AARCH64=0b566f9e00f0e4255996b2bcb18b580dc758f451fd2b7546542fae08261db535
EXPECT_OCP_CAND_AARCH64=90fb7182fb322b81f8b7fe3934dc3a780cfb58c2e5f02eb6ac1628b0f762c461

echo "BASE_IMAGE=$BASE_IMAGE"

GEN_SCRIPT=$(cat <<'EOF'
set -eu
export DEBIAN_FRONTEND=noninteractive PIP_DISABLE_PIP_VERSION_CHECK=1
apt-get update -qq </dev/null >/dev/null
apt-get install -y -qq python3-venv </dev/null >/dev/null
python3 -m venv /tmp/pt
/tmp/pt/bin/pip install -q pip-tools </dev/null
echo "container uname -m: $(uname -m)"
echo "container python3: $(python3 --version)"
echo "pip-compile: $(/tmp/pt/bin/pip-compile --version)"
echo "pip-tools venv:"
/tmp/pt/bin/pip freeze | sed 's/^/  /'
printf '%s\n' "$COMMON_INPUT" | /tmp/pt/bin/pip-compile --quiet --generate-hashes --strip-extras --no-header \
  --output-file=/out/requirements-common.txt -
chown "$HOST_UID:$HOST_GID" /out/requirements-common.txt
EOF
)

echo "== requirements-common.txt (pip-compile inside the pinned base image)"
docker run --rm -v "$LOCK_DIR:/out" \
  -e "COMMON_INPUT=$COMMON_INPUT" -e "HOST_UID=$(id -u)" -e "HOST_GID=$(id -g)" \
  "$BASE_IMAGE" sh -c "$GEN_SCRIPT"

wheel_lock() {
  local name=$1 version=$2 pattern=$3
  local json hashes
  json=$(curl -fsSL "https://pypi.org/pypi/$name/$version/json")
  hashes=$(printf '%s' "$json" | jq -r --arg re "$pattern" \
    '.urls[] | select(.packagetype == "bdist_wheel") | select(.filename | test($re)) | .digests.sha256' | sort -u)
  if [ -z "$hashes" ]; then
    echo "error: no wheel of $name==$version matches /$pattern/ in the PyPI JSON" >&2
    exit 1
  fi
  printf '%s==%s' "$name" "$version"
  printf '%s\n' "$hashes" | while IFS= read -r h; do
    printf ' \\\n    --hash=sha256:%s' "$h"
  done
  printf '\n'
}

CP311_LINUX='-cp311-cp311-manylinux[^-]*(aarch64|x86_64)[^-]*\.whl$'
ANY_WHEEL='\.whl$'

echo "== requirements-ocp-$OCP_NEW.txt, requirements-ocp-$OCP_CAND.txt, requirements-vtk.txt (PyPI JSON)"
{
  wheel_lock cadquery-ocp-novtk "$OCP_NEW" "$CP311_LINUX"
  wheel_lock cadquery-ocp-proxy "$OCP_NEW" "$ANY_WHEEL"
} > "$LOCK_DIR/requirements-ocp-$OCP_NEW.txt"
{
  wheel_lock cadquery-ocp-novtk "$OCP_CAND" "$CP311_LINUX"
  wheel_lock cadquery-ocp-proxy "$OCP_CAND" "$ANY_WHEEL"
} > "$LOCK_DIR/requirements-ocp-$OCP_CAND.txt"
wheel_lock vtk "$VTK_VERSION" "$CP311_LINUX" > "$LOCK_DIR/requirements-vtk.txt"

LOCKS="requirements-common.txt requirements-ocp-$OCP_NEW.txt requirements-ocp-$OCP_CAND.txt requirements-vtk.txt"

echo "== hash coverage"
for f in $LOCKS; do
  awk -v f="$f" '
    /^[A-Za-z0-9_.-]+==/ { if (need) { print "error: " f ": " last " has no --hash"; bad = 1 } need = 1; last = $1 }
    /--hash=sha256:/ { need = 0; n++ }
    END { if (need) { print "error: " f ": " last " has no --hash"; bad = 1 } print f ": " n " hashes"; exit bad }
  ' "$LOCK_DIR/$f"
done

echo "== package set"
for f in $LOCKS; do
  echo "$f:"
  grep -E '^[A-Za-z0-9_.-]+==' "$LOCK_DIR/$f" | sed -E 's/ \\$//; s/^/  /'
done

UNEXPECTED=""
for n in $(grep -E '^[A-Za-z0-9_.-]+==' "$LOCK_DIR/requirements-common.txt" | sed -E 's/==.*//' | tr 'A-Z_' 'a-z-'); do
  case " $COMMON_ALLOWED " in
    *" $n "*) ;;
    *) UNEXPECTED="$UNEXPECTED $n" ;;
  esac
done
echo "UNEXPECTED:${UNEXPECTED:- (none)}"

crosscheck() {
  local label=$1 file=$2 expected=$3
  if grep -q "$expected" "$LOCK_DIR/$file"; then
    echo "CROSSCHECK $label expected=$expected match=yes"
  else
    echo "CROSSCHECK $label expected=$expected match=NO" >&2
    CROSSCHECK_FAILED=1
  fi
}
CROSSCHECK_FAILED=0
crosscheck "cadquery-ocp-novtk $OCP_NEW cp311 manylinux_2_31_aarch64" "requirements-ocp-$OCP_NEW.txt" "$EXPECT_OCP_NEW_AARCH64"
crosscheck "cadquery-ocp-novtk $OCP_CAND cp311 manylinux_2_28_aarch64" "requirements-ocp-$OCP_CAND.txt" "$EXPECT_OCP_CAND_AARCH64"
if [ "$CROSSCHECK_FAILED" -ne 0 ]; then
  echo "error: lock hash differs from the independently measured research hash; do not edit by hand" >&2
  exit 1
fi

echo "== hash verification by download (--require-hashes, no code from wheels is executed)"
VERIFY_SCRIPT=$(cat <<'EOF'
set -eu
export DEBIAN_FRONTEND=noninteractive PIP_DISABLE_PIP_VERSION_CHECK=1
apt-get update -qq </dev/null >/dev/null
apt-get install -y -qq python3-venv </dev/null >/dev/null
python3 -m venv /v
echo "container python3: $(python3 --version)"
for f in /l/requirements-*.txt; do
  echo "pip download $f"
  /v/bin/pip download -q --no-deps --require-hashes --only-binary=:all: -r "$f" -d /tmp/d </dev/null
done
ls -l /tmp/d | sed 's/^/  /'
echo HASHES-OK
EOF
)
docker run --rm -v "$LOCK_DIR:/l:ro" "$BASE_IMAGE" sh -c "$VERIFY_SCRIPT"
