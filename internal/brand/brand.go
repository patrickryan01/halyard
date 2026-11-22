// Package brand is the one place the product name lives, so renaming it once
// the name is chosen is one edit here plus the packaging variables.
package brand

const (
	Name = "Mail"
	Slug = "mailclient"
	ID   = "ai.embernet.mailclient"
)

// Version is set at build time with -ldflags "-X .../internal/brand.Version=1.0.0".
var Version = "dev"
