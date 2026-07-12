// Package game holds the embedded word files for the cowbull API server.
package game

import "embed"

//go:embed words.txt words_easy.txt words_medium.txt words_hard.txt
var WordFS embed.FS
