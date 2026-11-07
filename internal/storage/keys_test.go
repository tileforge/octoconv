package storage

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAttemptOutputKey(t *testing.T) {
	attempt := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	got := AttemptOutputKey(attempt, 0)
	want := "attempts/22222222-2222-2222-2222-222222222222/out/0"
	if got != want {
		t.Fatalf("AttemptOutputKey = %s", got)
	}
}

func TestAcceptedOutputKey(t *testing.T) {
	job := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	attempt := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	digest := strings.Repeat("ab", 32)
	got := AcceptedOutputKey(job, attempt, 0, digest)
	want := "results/11111111-1111-1111-1111-111111111111/accepted/22222222-2222-2222-2222-222222222222/0-" + digest
	if got != want {
		t.Fatalf("AcceptedOutputKey = %s", got)
	}
	slashed := AcceptedOutputKey(job, attempt, 0, "aa/bb")
	if slashed != "results/11111111-1111-1111-1111-111111111111/accepted/22222222-2222-2222-2222-222222222222/0-aa/bb" {
		t.Fatalf("AcceptedOutputKey = %s", slashed)
	}
	if OutputKey(job, 0, "out.jpg") != "results/11111111-1111-1111-1111-111111111111/0-out.jpg" {
		t.Fatal("OutputKey changed")
	}
}
