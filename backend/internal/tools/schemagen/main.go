package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/igorkirisyuk/sanskrite/internal/models"
	"github.com/invopop/jsonschema"
)

func writeSchema(path string, v any) error {
	reflector := &jsonschema.Reflector{
		DoNotReference: true, // Inline all definitions instead of using $ref to root
	}
	r := reflector.Reflect(v)
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func main() {
	outDir := flag.String("out", "../web/src/types/schemas", "output directory for JSON schemas")
	flag.Parse()

	targets := []struct {
		File string
		Val  any
	}{
		{File: "User.schema.json", Val: new(models.User)},
		{File: "Project.schema.json", Val: new(models.Project)},
		{File: "DataSource.schema.json", Val: new(models.DataSource)},
	}

	for _, t := range targets {
		dest := filepath.Join(*outDir, t.File)
		if err := writeSchema(dest, t.Val); err != nil {
			fmt.Fprintf(os.Stderr, "failed to write %s: %v\n", dest, err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s\n", dest)
	}
}
