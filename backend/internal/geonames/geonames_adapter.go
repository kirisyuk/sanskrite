package geonames

import (
	"context"

	httphandlers "github.com/igorkirisyuk/sanskrite/internal/http"
	"github.com/igorkirisyuk/sanskrite/internal/ontology"
)

// Adapter adapts http.GeoNamesClient to ontology.GeoNamesClientInterface.
// Placed in a separate package to avoid cyclic imports.
type Adapter struct {
	client *httphandlers.GeoNamesClient
}

// NewAdapter creates a new GeoNames adapter.
func NewAdapter(client *httphandlers.GeoNamesClient) *Adapter {
	return &Adapter{client: client}
}

func (a *Adapter) Search(ctx context.Context, query string, maxResults int, opts ontology.GeoNamesSearchOptions) (*ontology.GeoNamesSearchResponse, error) {
	resp, err := a.client.Search(ctx, query, maxResults, httphandlers.GeoNamesSearchOptions{
		Country:      opts.Country,
		AdminCode1:   opts.AdminCode1,
		FeatureClass: opts.FeatureClass,
		FeatureCode:  opts.FeatureCode,
		NameEquals:   opts.NameEquals,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return &ontology.GeoNamesSearchResponse{Query: query}, nil
	}

	results := make([]ontology.GeoNamesPlace, 0, len(resp.Results))
	for _, r := range resp.Results {
		results = append(results, ontology.GeoNamesPlace{
			GeoNameID:    r.GeoNameID,
			Name:         r.Name,
			ToponymName:  r.ToponymName,
			CountryCode:  r.CountryCode,
			AdminCode1:   r.AdminCode1,
			AdminName1:   r.AdminName1,
			FeatureClass: r.FeatureClass,
			FeatureCode:  r.FeatureCode,
			Lat:          r.Lat,
			Lng:          r.Lng,
			Population:   r.Population,
		})
	}

	return &ontology.GeoNamesSearchResponse{
		Query:             resp.Query,
		TotalResultsCount: resp.TotalResultsCount,
		Results:           results,
	}, nil
}

func (a *Adapter) GetPlace(ctx context.Context, geonameID int) (*ontology.GeoNamesPlace, error) {
	p, err := a.client.GetPlace(ctx, geonameID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, nil
	}
	return &ontology.GeoNamesPlace{
		GeoNameID:    p.GeoNameID,
		Name:         p.Name,
		ToponymName:  p.ToponymName,
		CountryCode:  p.CountryCode,
		AdminCode1:   p.AdminCode1,
		AdminName1:   p.AdminName1,
		FeatureClass: p.FeatureClass,
		FeatureCode:  p.FeatureCode,
		Lat:          p.Lat,
		Lng:          p.Lng,
		Population:   p.Population,
	}, nil
}

func (a *Adapter) GetHierarchy(ctx context.Context, geonameID int) (*ontology.GeoNamesHierarchyResponse, error) {
	resp, err := a.client.GetHierarchy(ctx, geonameID)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return &ontology.GeoNamesHierarchyResponse{GeoNameID: geonameID}, nil
	}

	results := make([]ontology.GeoNamesPlace, 0, len(resp.Results))
	for _, r := range resp.Results {
		results = append(results, ontology.GeoNamesPlace{
			GeoNameID:    r.GeoNameID,
			Name:         r.Name,
			ToponymName:  r.ToponymName,
			CountryCode:  r.CountryCode,
			AdminCode1:   r.AdminCode1,
			AdminName1:   r.AdminName1,
			FeatureClass: r.FeatureClass,
			FeatureCode:  r.FeatureCode,
			Lat:          r.Lat,
			Lng:          r.Lng,
			Population:   r.Population,
		})
	}

	return &ontology.GeoNamesHierarchyResponse{
		GeoNameID: resp.GeoNameID,
		Results:   results,
	}, nil
}
