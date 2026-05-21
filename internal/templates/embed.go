// Package templates embeds the forge project template files.
package templates

import "embed"

//go:embed all:go-api all:go-crud all:go-worker all:fullstack
var FS embed.FS
