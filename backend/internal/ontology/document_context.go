package ontology

import "time"

// DocumentContext is a minimal document-like input for ontology extraction.
// Used when extracting from NormalizedDocument without persisting a Document.
// OntologyExtractor uses it for provenance (SourceRefs, entity attributes) and source creation.
type DocumentContext struct {
	ID                 string     // Synthetic ID for provenance (e.g. externalID or hash)
	SourceID           string     // DataSource ID
	URL                string     // Document URL
	Title              string     // Document title
	DocumentType       string     // article, post, etc.
	FullText           string     // Document content
	OriginPublishedAt  *time.Time // RSS pubDate / article publication date — для time decay сигналов
}
