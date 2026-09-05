// Command texpackages prints the declared LaTeX package set, one per line.
//
// It exists so scripts/tectonic-bundle-build.sh can compile the allowlist into
// the bundle's ignore rules without carrying its own copy of the list. A second
// copy is exactly the drift the three-way manifest test exists to catch, and a
// build script is the worst place to keep one: it would be consulted only when
// the bundle is rebuilt, which is rarely.
package main

import (
	"fmt"

	"github.com/apaderin/octoconv/internal/convert"
)

func main() {
	for _, p := range convert.TeXPackages() {
		fmt.Println(p)
	}
}
