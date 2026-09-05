package e2e

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The CI wiring for the compose bake, held to docker-compose.yml itself.
//
// TWO JOBS BAKE, and neither names its targets, so both evaluate EVERY service
// in docker-compose.yml. Phase 64 added a service whose Dockerfile COPYs a
// 122 MiB gitignored release asset out of the build context, and the fetch that
// puts it there was added to one of the two. The other -- a separate runner
// with its own fresh checkout -- would have died on the missing COPY source,
// which is the same failure the fix was for, one job over. Found in review
// (PR #16, codex rounds 1 and 2, once each).
//
// This test is DERIVED rather than listed: the services come from compose, so
// a tenth engine added there without touching CI fails here by name.
//
// It lives in internal/e2e because this is the package about the live compose
// stack, and it does not call e2eSetup, so it runs in the offline tier too.

const (
	bakeComposePath = "../../docker-compose.yml"
	bakeCIPath      = "../../.github/workflows/ci.yml"
)

var (
	bakeServiceRe = regexp.MustCompile(`^  ([a-z0-9][a-z0-9._-]*):\s*$`)
	bakeBuildRe   = regexp.MustCompile(`^    build:\s*$`)
	bakeJobRe     = regexp.MustCompile(`^  ([a-z0-9][a-z0-9._-]*):\s*$`)
	bakeScopeRe   = regexp.MustCompile(`([a-z0-9][a-z0-9._-]*)\.cache-from=type=gha,scope=`)
)

// buildableComposeServices returns every service with a build: section --
// the exact set `docker buildx bake -f docker-compose.yml` will evaluate.
func buildableComposeServices(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(bakeComposePath)
	if err != nil {
		t.Fatalf("read %s: %v", bakeComposePath, err)
	}
	lines := strings.Split(string(raw), "\n")

	var out []string
	current := ""
	for _, line := range lines {
		if m := bakeServiceRe.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		if current != "" && bakeBuildRe.MatchString(line) {
			out = append(out, current)
			current = "" // one build: per service is enough
		}
	}
	sort.Strings(out)
	if len(out) < 5 {
		t.Fatalf("found only %d buildable services in %s -- the parser is wrong and every "+
			"assertion below would be vacuous", len(out), bakeComposePath)
	}
	return out
}

// bakeJobs returns, per CI job that runs docker/bake-action, the cache scopes
// it names and whether the bundle fetch runs before it in that job.
func bakeJobs(t *testing.T) map[string]struct {
	scopes  map[string]bool
	fetched bool
} {
	t.Helper()
	raw, err := os.ReadFile(bakeCIPath)
	if err != nil {
		t.Fatalf("read %s: %v", bakeCIPath, err)
	}

	result := map[string]struct {
		scopes  map[string]bool
		fetched bool
	}{}

	job, inJobs, fetched := "", false, false
	scopes := map[string]bool{}
	bakes := false
	flush := func() {
		if job != "" && bakes {
			result[job] = struct {
				scopes  map[string]bool
				fetched bool
			}{scopes: scopes, fetched: fetched}
		}
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "jobs:") {
			inJobs = true
			continue
		}
		if !inJobs {
			continue
		}
		if m := bakeJobRe.FindStringSubmatch(line); m != nil {
			flush()
			job, scopes, fetched, bakes = m[1], map[string]bool{}, false, false
			continue
		}
		if strings.Contains(line, "tectonic-bundle-fetch.sh") && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			fetched = true
		}
		if strings.Contains(line, "docker/bake-action") {
			bakes = true
		}
		if m := bakeScopeRe.FindStringSubmatch(line); m != nil {
			scopes[m[1]] = true
		}
	}
	flush()

	if len(result) == 0 {
		t.Fatalf("no baking job found in %s -- the parser is wrong", bakeCIPath)
	}
	return result
}

// TestEveryBakingJobFetchesTheBundle is the finding itself, as an invariant.
func TestEveryBakingJobFetchesTheBundle(t *testing.T) {
	for name, job := range bakeJobs(t) {
		if !job.fetched {
			t.Errorf("CI job %q runs docker/bake-action without fetching the tectonic bundle first: "+
				"the bake has no `targets:`, so it evaluates tectonic-worker, whose Dockerfile COPYs a "+
				"gitignored 122 MiB asset out of the build context", name)
		}
	}
}

// TestEveryBakingJobCachesEveryService is the weaker half, and the one that
// would have caught the missing scope rather than the missing fetch: a target
// with no cache-from rebuilds from scratch on every run, which reads as a slow
// job rather than as a mistake.
func TestEveryBakingJobCachesEveryService(t *testing.T) {
	services := buildableComposeServices(t)
	for name, job := range bakeJobs(t) {
		for _, svc := range services {
			if !job.scopes[svc] {
				t.Errorf("CI job %q bakes %s without a cache-from scope: it is in docker-compose.yml "+
					"and the bake names no targets, so it IS built there", name, svc)
			}
		}
	}
}
