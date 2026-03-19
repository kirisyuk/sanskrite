package models

import "time"

// IndustryNodeRegistry stores the canonical /industry tree for a given version.
// Collection: horizon_industry_nodes, DocID: version (for example "v1").
type IndustryNodeRegistry struct {
	ID        string             `json:"id" firestore:"id"`
	Version   string             `json:"version" firestore:"version"`
	UpdatedAt time.Time          `json:"updatedAt" firestore:"updatedAt"`
	Nodes     []IndustryNodeData `json:"nodes,omitempty" firestore:"nodes,omitempty"`
}

// IndustryNodeData defines one canonical /industry node.
type IndustryNodeData struct {
	Path            string   `json:"path" firestore:"path"`
	ParentPath      string   `json:"parentPath,omitempty" firestore:"parentPath,omitempty"`
	Level           int      `json:"level" firestore:"level"`
	Label           string   `json:"label,omitempty" firestore:"label,omitempty"`
	Status          string   `json:"status,omitempty" firestore:"status,omitempty"`
	Definition      string   `json:"definition,omitempty" firestore:"definition,omitempty"`
	AllowedForTypes []string `json:"allowedForTypes,omitempty" firestore:"allowedForTypes,omitempty"`
}

// TaxonomyAliasRegistry stores alias keys for taxonomy namespaces.
// Collection: horizon_taxonomy_aliases, DocID: version (for example "v1").
type TaxonomyAliasRegistry struct {
	ID        string              `json:"id" firestore:"id"`
	Version   string              `json:"version" firestore:"version"`
	UpdatedAt time.Time           `json:"updatedAt" firestore:"updatedAt"`
	Aliases   []TaxonomyAliasData `json:"aliases,omitempty" firestore:"aliases,omitempty"`
}

// TaxonomyAliasData defines one canonical alias resolution rule.
type TaxonomyAliasData struct {
	Namespace     string `json:"namespace" firestore:"namespace"`
	KeyKind       string `json:"keyKind" firestore:"keyKind"`
	KeyNorm       string `json:"keyNorm" firestore:"keyNorm"`
	CanonicalPath string `json:"canonicalPath" firestore:"canonicalPath"`
	Status        string `json:"status,omitempty" firestore:"status,omitempty"`
	Lang          string `json:"lang,omitempty" firestore:"lang,omitempty"`
	Source        string `json:"source,omitempty" firestore:"source,omitempty"`
	Comment       string `json:"comment,omitempty" firestore:"comment,omitempty"`
}
