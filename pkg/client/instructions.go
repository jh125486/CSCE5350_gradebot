package client

import "embed"

//go:embed instructions/*.txt
var instructionsFS embed.FS

func instructionsFor(project string) string {
	b, _ := instructionsFS.ReadFile("instructions/" + project + ".txt")
	return string(b)
}
