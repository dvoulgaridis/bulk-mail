// Package frontend exposes the compiled frontend for embedding in the application binary.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed dist
var embedded embed.FS

// FS returns the root of the compiled frontend filesystem.
func FS() (fs.FS, error) {
	return fs.Sub(embedded, "dist")
}
