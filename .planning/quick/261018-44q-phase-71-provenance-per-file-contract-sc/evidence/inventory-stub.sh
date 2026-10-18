set -uo pipefail
W="$(mktemp -d "${TMPDIR:-/tmp}/inventory-stub.XXXXXX")"
trap 'rm -rf "$W"' EXIT
mkdir -p "$W/bin" "$W/private/reject"
cat > "$W/bin/docker" <<STUB
LOG="$W/docker.log"
echo "docker \$1" >> "\$LOG"
case "\$1" in
  image) case "\$*" in *Architecture*) echo arm64 ;; *Id*) echo sha256:stub ;; esac; exit 0 ;;
  version) echo arm64; exit 0 ;;
  ps) exit 0 ;;
  run)
    case "\$*" in
      *counters.py*)
        mount=\$(printf '%s\n' "\$@" | sed -n 's|:/corpus:ro\$||p' | head -n 1)
        names=0; case "\$*" in *--names*) names=1 ;; esac
        case "\$*" in *find*) echo "STUB_FIND_IN_CONTAINER" >> "\$LOG" ;; esac
        n=0
        while IFS= read -r p; do
          n=\$((n + 1))
          echo "\$mount|\$p" >> "$W/paths.log"
          rel=\${p#/corpus/}
          if [ ! -f "\$mount/\$rel" ]; then echo "CNT,error=missing"; continue; fi
          if [ "\$names" = 1 ]; then echo "CNT,name=\$(basename "\$p"),fmt=stub,bytes=1"; else echo "CNT,fmt=stub,bytes=1"; fi
        done
        echo "STUB_STDIN_LINES,mount=\$mount,lines=\$n" >> "\$LOG"
        exit 0 ;;
      *) echo "HDR,stub=1"; exit 0 ;;
    esac ;;
esac
exit 0
STUB
chmod 0755 "$W/bin/docker"
: > "$W/private/SECRET-client-a.step"; : > "$W/private/notes.md"; : > "$W/private/reject/SECRET-client-b.step"

PATH="$W/bin:$PATH" OCTOCONV_CAD_PRIVATE_CORPUS="$W/private" bash scripts/cad-corpus-inventory.sh --out "$W/inv.txt" 2> "$W/inv.err"
rc=$?
echo "INVENTORY_STUB,rc=$rc"
grep -E '^(INV|PRIVATE_CORPUS|PRIVATE_COUNTERS),' "$W/inv.txt"
grep '^STUB_' "$W/docker.log"
root=$(cd internal/convert/testdata/cad && pwd -P)
committed=$(grep -c "^$root|" "$W/paths.log")
groups=$(awk -F'|' -v r="$root" '$1 == r {print $2}' "$W/paths.log" | sed 's#^/corpus/##; s#/.*##' | LC_ALL=C sort | uniq -c | awk '{printf "%s:%s ", $2, $1}')
order=$(awk -F'|' -v r="$root" '$1 == r {print $2}' "$W/paths.log" | LC_ALL=C awk 'NR > 1 && $0 < prev {bad = 1} {prev = $0} END {print bad ? "unsorted" : "sorted"}')
leak=$(grep -c 'SECRET' "$W/inv.txt")
echo "COMMITTED_PATHS_ON_STDIN,count=$committed,groups=[${groups% }],order=$order"
echo "PRIVATE_NAMES_IN_OUTPUT,count=$leak"
echo "CONTAINER_FIND,count=$(grep -c STUB_FIND_IN_CONTAINER "$W/docker.log")"
if [ "$rc" = 0 ] && [ "$committed" = 37 ] && [ "$groups" = "public:19 synthetic:18 " ] && [ "$order" = sorted ] && [ "$leak" = 0 ] \
  && grep -q '^INV,group=committed,files=37$' "$W/inv.txt" && grep -q '^PRIVATE_CORPUS,files=3,status=measured$' "$W/inv.txt" \
  && grep -q '^PRIVATE_COUNTERS,files=3,counted=1,unreadable=0$' "$W/inv.txt" && ! grep -q STUB_FIND_IN_CONTAINER "$W/docker.log"; then
  echo INVENTORY-STUB-OK
else
  cat "$W/inv.err"
  echo INVENTORY-STUB-FAILED
  exit 1
fi
