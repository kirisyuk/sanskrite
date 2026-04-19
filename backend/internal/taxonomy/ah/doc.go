// Package ah is the AgentHorde-aligned canonical taxonomy generated from the
// single source of truth at agents/shared/agenthorde_common/agenthorde_common/canonicalizer/taxonomy.py.
//
// The runtime SoT is exported to a stable JSON snapshot at
// shared/taxonomy/taxonomy.json by the python script
//
//	cd agents/shared/agenthorde_common && python -m scripts.export_taxonomy \
//	  --out ../../../shared/taxonomy/taxonomy.json
//
// and that JSON snapshot is consumed by the codegen tool at
// backend/internal/tools/taxonomygen to emit taxonomy_generated.go in this
// package. Both steps are wired via go generate so a top-level
//
//	go generate ./internal/taxonomy/ah
//
// is sufficient to regenerate this file (the python step is invoked by the
// codegen tool itself when --refresh is passed; CI uses --check to fail loudly
// when this file drifts from the SoT).
//
// DO NOT edit taxonomy_generated.go by hand. Modify taxonomy.py and re-run
// go generate. New code that consumes AgentHorde semantics (fragment classes,
// relations, subtype enums, attribute keys) MUST import this package and
// MUST NOT duplicate the constants in domain packages.
package ah

//go:generate go run github.com/igorkirisyuk/sanskrite/internal/tools/taxonomygen -in ../../../../shared/taxonomy/taxonomy.json -out ./taxonomy_generated.go -package ah
