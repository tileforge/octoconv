package convert

import (
	"testing"
	"time"
)

func TestPctForPage_Basic(t *testing.T) {
	cases := []struct {
		done, total, want int
	}{
		{0, 10, 0},
		{5, 10, 50},
		{10, 10, 100},
	}
	for _, c := range cases {
		pct, ok := pctForPage(c.done, c.total)
		if !ok {
			t.Fatalf("pctForPage(%d,%d): ok=false, want true", c.done, c.total)
		}
		if pct != c.want {
			t.Errorf("pctForPage(%d,%d) = %d, want %d", c.done, c.total, pct, c.want)
		}
	}
}

func TestPctForPage_ZeroTotalNotDerivable(t *testing.T) {
	pct, ok := pctForPage(3, 0)
	if ok {
		t.Fatalf("pctForPage(3,0): ok=true, want false (null semantics -- never divide by zero)")
	}
	if pct != 0 {
		t.Fatalf("pctForPage(3,0): pct=%d, want 0", pct)
	}
}

func TestPctForPage_ClampsOutOfRangeDone(t *testing.T) {
	if pct, ok := pctForPage(15, 10); !ok || pct != 100 {
		t.Fatalf("pctForPage(15,10) = (%d,%v), want (100,true) clamped", pct, ok)
	}
	if pct, ok := pctForPage(-5, 10); !ok || pct != 0 {
		t.Fatalf("pctForPage(-5,10) = (%d,%v), want (0,true) clamped", pct, ok)
	}
}

func TestOCRPageProgress_Monotonic(t *testing.T) {
	p := newOCRPageProgress(10)
	lastPct := -1
	for i := 0; i < 10; i++ {
		u := p.recordPage(2 * time.Second)
		if u.Pct < lastPct {
			t.Fatalf("page %d: Pct regressed from %d to %d", i+1, lastPct, u.Pct)
		}
		lastPct = u.Pct
	}
	if lastPct != 100 {
		t.Fatalf("final Pct = %d, want 100", lastPct)
	}
}

func TestOCRPageETA_NilBeforeFirstPage(t *testing.T) {
	p := newOCRPageProgress(5)
	if eta := p.etaSeconds(); eta != nil {
		t.Fatalf("etaSeconds before any recordPage = %v, want nil (no seconds/page sample yet)", *eta)
	}
}

func TestOCRPageETA_DerivesFromRunningAverage(t *testing.T) {
	p := newOCRPageProgress(4)

	u1 := p.recordPage(3 * time.Second)
	if u1.ETA == nil {
		t.Fatalf("ETA nil after first completed page, want a derived estimate")
	}
	// 1 page done @ 3s/page average, 3 pages remaining -> 9s.
	if *u1.ETA != 9 {
		t.Fatalf("ETA after page 1 = %d, want 9 (3 remaining x 3s/page average)", *u1.ETA)
	}

	u2 := p.recordPage(5 * time.Second)
	// avg = (3+5)/2 = 4s/page, 2 pages remaining -> 8s.
	if u2.ETA == nil || *u2.ETA != 8 {
		t.Fatalf("ETA after page 2 = %v, want 8 (running average x remaining pages)", u2.ETA)
	}
}

func TestOCRPageProgress_FinalPageZeroRemainingETA(t *testing.T) {
	p := newOCRPageProgress(2)
	p.recordPage(4 * time.Second)
	final := p.recordPage(4 * time.Second)
	if final.ETA == nil || *final.ETA != 0 {
		t.Fatalf("ETA at the final page = %v, want 0 (zero pages remaining)", final.ETA)
	}
	if final.Pct != 100 {
		t.Fatalf("Pct at the final page = %d, want 100", final.Pct)
	}
}
