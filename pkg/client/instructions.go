package client

import "embed"

//go:embed instructions/*.txt
var instructionsFS embed.FS

func instructionsFor(name string) string {
	b, err := instructionsFS.ReadFile("instructions/" + name + ".txt")
	if err != nil {
		b, _ = instructionsFS.ReadFile("instructions/default.txt")
	}
	return string(b)
}
