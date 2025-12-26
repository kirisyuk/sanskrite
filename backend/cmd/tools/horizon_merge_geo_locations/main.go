package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

// horizon_merge_geo_locations is a maintenance tool.
// NOTE: The original implementation was accidentally committed as an empty file,
// which breaks `go test ./...` with a syntax error.
//
// For now we keep a safe stub that compiles and provides a clear message.
// If/when you need the actual migration logic, implement it here.

func main() {
	var (
		apply   = flag.Bool("apply", false, "Apply changes (default: dry-run / no-op stub)")
		project = flag.String("project", "", "GCP project id (optional for this stub)")
	)
	flag.Parse()

	if *project == "" {
		*project = os.Getenv("GCP_PROJECT")
	}

	log.Printf("horizon_merge_geo_locations (stub): project=%q apply=%v", *project, *apply)
	fmt.Println("This tool is currently a stub. No actions were performed.")
}