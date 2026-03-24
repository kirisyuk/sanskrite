package main

import "testing"

func TestCasebookManifestDomainsAreSupported(t *testing.T) {
	for _, item := range casebookSynonyms {
		if !isSupportedDomain(item.Domain) {
			t.Fatalf("unsupported synonym domain %q", item.Domain)
		}
		if item.CanonicalPath == "" || item.SynonymPath == "" {
			t.Fatalf("empty synonym path in %+v", item)
		}
		if item.CanonicalPath == item.SynonymPath {
			t.Fatalf("degenerate synonym %+v", item)
		}
	}
	for _, pair := range casebookPairs {
		if !isSupportedDomain(pair.Domain) {
			t.Fatalf("unsupported pair domain %q", pair.Domain)
		}
		if pair.FromPath == "" || pair.ToPath == "" {
			t.Fatalf("empty pair path in %+v", pair)
		}
		if pair.FromPath == pair.ToPath {
			t.Fatalf("degenerate pair %+v", pair)
		}
		if pair.RelationKind != "transformation" && pair.RelationKind != "assembly" {
			t.Fatalf("unexpected relation kind %q", pair.RelationKind)
		}
	}
	for _, item := range casebookDeactivations {
		if !isSupportedDomain(item.Domain) {
			t.Fatalf("unsupported deactivation domain %q", item.Domain)
		}
		if item.FromPath == "" || item.ToPath == "" {
			t.Fatalf("empty deactivation path in %+v", item)
		}
		if item.FromPath == item.ToPath {
			t.Fatalf("degenerate deactivation %+v", item)
		}
	}
}

func TestCasebookManifestHasNoDuplicateOperations(t *testing.T) {
	seenSynonyms := map[string]struct{}{}
	for _, item := range casebookSynonyms {
		key := item.Domain + "\n" + item.CanonicalPath + "\n" + item.SynonymPath
		if _, ok := seenSynonyms[key]; ok {
			t.Fatalf("duplicate synonym %s", key)
		}
		seenSynonyms[key] = struct{}{}
	}

	seenPairs := map[string]struct{}{}
	for _, pair := range casebookPairs {
		key := pair.Domain + "\n" + pair.FromPath + "\n" + pair.ToPath
		if _, ok := seenPairs[key]; ok {
			t.Fatalf("duplicate pair %s", key)
		}
		seenPairs[key] = struct{}{}
	}

	seenDeactivations := map[string]struct{}{}
	for _, item := range casebookDeactivations {
		key := item.Domain + "\n" + item.FromPath + "\n" + item.ToPath
		if _, ok := seenDeactivations[key]; ok {
			t.Fatalf("duplicate deactivation %s", key)
		}
		seenDeactivations[key] = struct{}{}
	}
}
