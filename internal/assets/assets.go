package assets

import (
	"embed"
	"io/fs"
)

// FS contains MiniWiki's built-in templates and static files.
//
//go:embed templates/* static/*
var FS embed.FS

func Templates() fs.FS {
	sub, err := fs.Sub(FS, ".")
	if err != nil {
		panic(err)
	}
	return sub
}

func Static() fs.FS {
	sub, err := fs.Sub(FS, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
