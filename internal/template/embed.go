// Package templatefs exposes the project templates embedded in the JGO binary.
package templatefs

import (
	"embed"
	"io/fs"
)

//go:embed all:project
var files embed.FS

// Project returns the root of the embedded project template filesystem.
func Project() (fs.FS, error) {
	return fs.Sub(files, "project")
}
