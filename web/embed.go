package web

import "embed"

// Assets contains the production frontend. Build web before building the executable.
//
//go:embed all:dist
var Assets embed.FS
