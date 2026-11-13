package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed assets
var assets embed.FS

//go:embed openapi.json
var OpenAPI []byte

//go:embed source.tar.gz
var Source []byte

func Handler() http.Handler {
	files, _ := fs.Sub(assets, "assets")
	return http.FileServer(http.FS(files))
}
