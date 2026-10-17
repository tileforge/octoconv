#!/usr/bin/env bash
set -euo pipefail
LO=/private/tmp/claude-501/-Users-apaderin-dev-octoconv/95661ab2-afd1-49be-a390-64117af2c14a/scratchpad/lo
cd "$LO"
field() { printf '%s' "$1" | tr ',' '\n' | sed -n "s/^$2=//p" | head -1; }
rule() { { grep -oE 'content check: (J[0-9]|transparent fraction|background [a-z]+ but [a-z ]+)' "$1" || true; } | head -1 | sed 's/content check: //' | tr ' ' '_'; }
jline() {
	local bin=$1 png=$2 size=$3 bg=$4
	"$LO/bin/$bin" --size "$size" --background "$bg" "$png" 2>"$LO/j.err" || true
}
total=0 bad=0 maxgap=0
echo "HDR,stage=pairs,image=$(docker image inspect -f '{{.Id}}' octoconv-cad-worker:latest),judge_head=8b12a80,judge_new=working_tree_of_quick_261014-0lo,tolerance=a_same_verdict;b_box_margin_le_1px;c_same_dark;d_transparent_subset;e_blob_gap_le_0.10"
while read -r n v s; do
	o="png/$n.$v.$s.opaque.png"; t="png/$n.$v.$s.transparent.png"
	ho=$(jline judge-head "$o" "$s" opaque); hro=$(rule j.err)
	ht=$(jline judge-head "$t" "$s" transparent); hrt=$(rule j.err)
	no=$(jline judge-new "$o" "$s" opaque); nro=$(rule j.err)
	nt=$(jline judge-new "$t" "$s" transparent); nrt=$(rule j.err)
	a=$(bin/ana "$o" "$t")
	only_t=$(printf '%s' "$a" | sed -n 's/.*only_transparent=\([0-9]*\).*/\1/p')
	obox=$(printf '%s' "$a" | sed -n 's/.*opaque_box=\([^ ]*\).*/\1/p')
	tbox=$(printf '%s' "$a" | sed -n 's/.*transparent_box=\([^ ]*\).*/\1/p')
	oc=$(field "$no" count); tc=$(field "$nt" count)
	od=$(field "$no" dark_count); td=$(field "$nt" dark_count)
	om=$(field "$no" margin_px); tm=$(field "$nt" margin_px)
	ook=$(field "$no" ok); tok=$(field "$nt" ok); omode=$(field "$no" mode); tmode=$(field "$nt" mode)
	gap=$(awk -v a="$oc" -v b="$tc" 'BEGIN { if (a > 0) printf "%.4f", (a - b) / a; else print "0.0000" }')
	boxd=$(awk -v a="$obox" -v b="$tbox" 'BEGIN { split(a, p, /[,.]+/); split(b, q, /[,.]+/); m = 0; for (i = 1; i <= 4; i++) { d = p[i] - q[i]; if (d < 0) d = -d; if (d > m) m = d } print m }')
	md=$(( om > tm ? om - tm : tm - om ))
	va=1; [ "$ook" = "$tok" ] || va=0
	if [ "$ook" = 1 ]; then [ "$omode" = "$tmode" ] || va=0; else [ "$nro" = "$nrt" ] || va=0; fi
	vb=1; [ "$boxd" -le 1 ] && [ "$md" -le 1 ] || vb=0
	vc=1; [ "$od" = "$td" ] || vc=0
	vd=1; [ "$only_t" = 0 ] || vd=0
	ve=1; if [ "$omode" = blob ] && [ "$ook" = 1 ]; then awk -v g="$gap" 'BEGIN { exit !(g <= 0.10) }' || ve=0; fi
	res=ok; [ "$va$vb$vc$vd$ve" = 11111 ] || { res=fail; bad=$((bad + 1)); }
	total=$((total + 1))
	echo "PAIR,frame=$n.$v.$s,head_opaque=$(field "$ho" ok):$(field "$ho" mode):${hro:-none},head_transparent=$(field "$ht" ok):$(field "$ht" mode):${hrt:-none},new_opaque=$ook:$omode:${nro:-none},new_transparent=$tok:$tmode:${nrt:-none},count_o=$oc,count_t=$tc,gap=$gap,dark_o=$od,dark_t=$td,box_o=$obox,box_t=$tbox,box_max_diff=$boxd,margin_o=$om,margin_t=$tm,only_transparent=$only_t,ring_t=$(field "$nt" ring_nontransparent),a=$va,b=$vb,c=$vc,d=$vd,e=$ve,result=$res"
done < pairs.list
echo "PAIRS_SUMMARY,pairs=$total,outside_tolerance=$bad,result=$([ "$bad" = 0 ] && echo ok || echo fail)"
