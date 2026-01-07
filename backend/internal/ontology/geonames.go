package ontology

import "context"

// GeoNamesClientInterface is an interface for GeoNames operations.
// Placed in core ontology package to avoid cyclic imports between HTTP clients and domain logic.
type GeoNamesClientInterface interface {
	Search(ctx context.Context, query string, maxResults int, opts GeoNamesSearchOptions) (*GeoNamesSearchResponse, error)
	GetPlace(ctx context.Context, geonameID int) (*GeoNamesPlace, error)
	GetHierarchy(ctx context.Context, geonameID int) (*GeoNamesHierarchyResponse, error)
}

// GeoNamesPlace represents a GeoNames feature/entity (minimal subset).
type GeoNamesPlace struct {
	GeoNameID    int    `json:"geonameId"`
	Name         string `json:"name"`
	ToponymName  string `json:"toponymName,omitempty"`
	CountryCode  string `json:"countryCode,omitempty"`
	AdminCode1   string `json:"adminCode1,omitempty"`
	AdminName1   string `json:"adminName1,omitempty"`
	FeatureClass string `json:"fcl,omitempty"`
	FeatureCode  string `json:"fcode,omitempty"`
	Lat          string `json:"lat,omitempty"`
	Lng          string `json:"lng,omitempty"`
	Population   int64  `json:"population,omitempty"`
}

// GeoNamesSearchResponse represents the response from GeoNames search.
type GeoNamesSearchResponse struct {
	Query             string          `json:"query"`
	TotalResultsCount int             `json:"totalResultsCount,omitempty"`
	Results           []GeoNamesPlace `json:"results"`
}

// GeoNamesHierarchyResponse represents the response from GeoNames hierarchy endpoint.
type GeoNamesHierarchyResponse struct {
	GeoNameID int             `json:"geonameId,omitempty"`
	Results   []GeoNamesPlace `json:"results"`
}

// GeoNamesSearchOptions adds optional filters for GeoNames search.
type GeoNamesSearchOptions struct {
	Country      string
	AdminCode1   string
	FeatureClass string
	FeatureCode  string
	NameEquals   bool
}
