package convert

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
)

// imageFormats are the raster formats libvips converts between in this slice.
var imageFormats = []string{"png", "jpg", "webp", "heic", "tiff"}

// LibvipsConverter converts raster images by shelling out to the `vips` CLI.
// The output format is selected by outPath's extension (e.g. out.webp).
type LibvipsConverter struct {
	// Sandbox confines the engine's filesystem access. Zero value runs the
	// engine directly, which is what every unit test wants and what a
	// deployment gets only by saying so out loud -- cmd/worker refuses to
	// start otherwise.
	Sandbox *engineSandbox
}

// Pairs returns every ordered pair of supported image formats (from != to).
func (LibvipsConverter) Pairs() []Pair {
	pairs := make([]Pair, 0, len(imageFormats)*(len(imageFormats)-1))
	for _, from := range imageFormats {
		for _, to := range imageFormats {
			if from != to {
				pairs = append(pairs, Pair{From: from, To: to})
			}
		}
	}
	return pairs
}

// libvipsReadOnly is what the engine needs to read, MEASURED rather than
// reasoned: every path here was observed being opened during real conversions
// and then proven necessary by running the five format pairs under a ruleset
// built from this list. See .planning/research/SANDBOX-PATHS.md.
//
// Nothing from /etc but the loader cache, no /tmp, no home directory. The
// engine's own executable is NOT here because it is resolved at run time --
// vips is execve'd and never opened, so it appears in no trace and a set built
// from the trace alone fails every conversion with "exec: permission denied".
var libvipsReadOnly = []string{
	"/lib",
	"/usr/lib",
	"/etc/ld.so.cache",
	"/proc/self/auxv",
	"/proc/self/status",
	"/proc/filesystems",
	"/proc/cpuinfo",
	"/sys/devices/system/cpu/online",
	"/sys/devices/system/cpu/possible",
}

// libvipsSpec grants the job's own directory read-write and nothing else
// writable.
//
// The directory rather than the two files: libvips writes its output through a
// temporary in the same directory for some targets, and a grant on out.webp
// alone would deny that without saying why.
func libvipsSpec(inPath, outPath, enginePath string) []string {
	jobDir := filepath.Dir(outPath)
	ro := append(append([]string{}, libvipsReadOnly...), enginePath)
	if d := filepath.Dir(inPath); d != jobDir {
		ro = append(ro, d)
	}
	return spec([]string{jobDir}, ro)
}

// Convert runs `vips copy <in> <out>`; libvips infers both codecs from the file
// extensions. ctx must carry the engine timeout.
func (c LibvipsConverter) Convert(ctx context.Context, inPath, outPath string, _ map[string]any) error {
	enginePath, err := enginePathFor("vips")
	if err != nil {
		return fmt.Errorf("libvips: %w", err)
	}

	args := []string{"copy", inPath, outPath}
	var s []string
	if c.Sandbox.confines() {
		s = libvipsSpec(inPath, outPath, enginePath)
	}
	name, args, err := c.Sandbox.wrapEngine("vips", s, args)
	if err != nil {
		return fmt.Errorf("libvips: %w", err)
	}

	if _, err := runCommand(ctx, name, args...); err != nil {
		return fmt.Errorf("libvips: %w", err)
	}
	return nil
}

// Engine reports the image engine class (D-01).
func (LibvipsConverter) Engine() string { return EngineImage }

// ConfinedLibvips returns a converter that runs vips through helper under the
// measured ruleset. An empty helper runs the engine directly, which is what
// IMAGE_SANDBOX_DISABLED=true produces and what tests use.
func ConfinedLibvips(helper []string) LibvipsConverter {
	return LibvipsConverter{Sandbox: &engineSandbox{Helper: helper, Confine: len(helper) > 0}}
}

// LibvipsPreflight proves the engine can actually be EXECUTED under the
// ruleset this converter would build, before any job depends on it.
//
// Asking the kernel whether Landlock exists is not the same question: a
// seccomp profile or an LSM stacking limit can answer that yes and still deny
// restrict_self, and the worker would then start, consume the queue, and fail
// every job. So this converts a real image through the real helper.
//
// It generates its own input rather than carrying a fixture: a file in the
// image is one more thing that can be absent, and the point is to exercise the
// mechanism, not any particular picture.
func LibvipsPreflight(ctx context.Context, helper []string) error {
	if len(helper) == 0 {
		return nil // unconfined by explicit configuration; nothing to prove
	}

	dir, err := os.MkdirTemp("", "libvips-preflight-")
	if err != nil {
		return fmt.Errorf("preflight work directory: %w", err)
	}
	defer os.RemoveAll(dir)

	inPath := filepath.Join(dir, "in.png")
	f, err := os.Create(inPath)
	if err != nil {
		return fmt.Errorf("preflight input: %w", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return fmt.Errorf("preflight input: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("preflight input: %w", err)
	}

	outPath := filepath.Join(dir, "out.webp")
	c := LibvipsConverter{Sandbox: &engineSandbox{Helper: helper, Confine: true}}
	if err := c.Convert(ctx, inPath, outPath, nil); err != nil {
		return fmt.Errorf("the engine cannot convert under the sandbox: %w", err)
	}
	// The exit status is not enough: an engine denied its input can exit 0 and
	// write nothing, which is how ghostscript behaved when this project
	// measured it. The output has to exist and be non-empty.
	st, err := os.Stat(outPath)
	if err != nil || st.Size() == 0 {
		return fmt.Errorf("the engine produced no output under the sandbox")
	}
	return nil
}
