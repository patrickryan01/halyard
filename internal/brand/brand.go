// Package brand is the one place the product name lives, so a rename is one
// edit here plus the packaging variables.
package brand

const (
	Name = "Halyard"
	Slug = "halyard"
	ID   = "io.github.patrickryan01.halyard"
)

// Version is set at build time with -ldflags "-X .../internal/brand.Version=1.0.0".
var Version = "dev"
