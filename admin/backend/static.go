package admin

import (
	"embed"
	"io/fs"
)

//go:embed embed/*
var staticFiles embed.FS

func StaticFS() fs.FS {
	return staticFiles
}
