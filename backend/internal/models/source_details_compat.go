package models

import "strings"

func cloneStringPointer(in *string) *string {
	if in == nil {
		return nil
	}
	v := strings.TrimSpace(*in)
	if v == "" {
		return nil
	}
	out := v
	return &out
}

func cloneBoolPointer(in *bool) *bool {
	if in == nil {
		return nil
	}
	v := *in
	return &v
}

func normalizeFacetListCompat(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func normalizeSourceCategoryCompat(paths []string, primary *string) ([]string, *string) {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}

	var normalizedPrimary *string
	if primary != nil {
		v := strings.TrimSpace(*primary)
		if v != "" {
			normalizedPrimary = &v
			if _, ok := seen[v]; !ok {
				out = append([]string{v}, out...)
			} else {
				reordered := make([]string, 0, len(out))
				reordered = append(reordered, v)
				for _, p := range out {
					if p != v {
						reordered = append(reordered, p)
					}
				}
				out = reordered
			}
		}
	}
	if normalizedPrimary == nil && len(out) > 0 {
		v := out[0]
		normalizedPrimary = &v
	}
	return out, normalizedPrimary
}

// NormalizeHorizonMonitorAliases reads legacy worldmonitor* aliases into canonical horizonMonitor* fields.
func (d *SourceDetails) NormalizeHorizonMonitorAliases() {
	if d == nil {
		return
	}
	if d.HorizonMonitorCategory == nil {
		d.HorizonMonitorCategory = cloneStringPointer(d.WorldMonitorCategoryLegacy)
	}
	if d.HorizonMonitorFeedType == nil {
		d.HorizonMonitorFeedType = cloneStringPointer(d.WorldMonitorFeedTypeLegacy)
	}
	if d.HorizonMonitorIngestMode == nil {
		d.HorizonMonitorIngestMode = cloneStringPointer(d.WorldMonitorIngestModeLegacy)
	}
	if d.HorizonMonitorSignalType == nil {
		d.HorizonMonitorSignalType = cloneStringPointer(d.WorldMonitorSignalTypeLegacy)
	}
	if d.HorizonMonitorRequiresAuth == nil {
		d.HorizonMonitorRequiresAuth = cloneBoolPointer(d.WorldMonitorRequiresAuthLegacy)
	}
}

// SyncHorizonMonitorAliases mirrors canonical horizonMonitor* fields into legacy worldmonitor* aliases.
func (d *SourceDetails) SyncHorizonMonitorAliases() {
	if d == nil {
		return
	}
	d.NormalizeHorizonMonitorAliases()

	d.WorldMonitorCategoryLegacy = cloneStringPointer(d.HorizonMonitorCategory)
	d.WorldMonitorFeedTypeLegacy = cloneStringPointer(d.HorizonMonitorFeedType)
	d.WorldMonitorIngestModeLegacy = cloneStringPointer(d.HorizonMonitorIngestMode)
	d.WorldMonitorSignalTypeLegacy = cloneStringPointer(d.HorizonMonitorSignalType)
	d.WorldMonitorRequiresAuthLegacy = cloneBoolPointer(d.HorizonMonitorRequiresAuth)
}

// NormalizeCompat normalizes source kind aliases and legacy detail fields.
func (ds *DataSource) NormalizeCompat() {
	if ds == nil {
		return
	}
	ds.Kind = NormalizeSourceKind(ds.Kind)
	ds.Details.NormalizeHorizonMonitorAliases()
	ds.SourceCategoryPaths, ds.PrimarySourceCategoryPath = normalizeSourceCategoryCompat(ds.SourceCategoryPaths, ds.PrimarySourceCategoryPath)
	ds.SourceGeoFacets = normalizeFacetListCompat(ds.SourceGeoFacets)
	ds.SourceFormatFacets = normalizeFacetListCompat(ds.SourceFormatFacets)
}

// SyncCompatForWrite performs compatibility sync before writes/serialization.
func (ds *DataSource) SyncCompatForWrite() {
	if ds == nil {
		return
	}
	ds.Kind = NormalizeSourceKind(ds.Kind)
	ds.Details.SyncHorizonMonitorAliases()
	ds.SourceCategoryPaths, ds.PrimarySourceCategoryPath = normalizeSourceCategoryCompat(ds.SourceCategoryPaths, ds.PrimarySourceCategoryPath)
	ds.SourceGeoFacets = normalizeFacetListCompat(ds.SourceGeoFacets)
	ds.SourceFormatFacets = normalizeFacetListCompat(ds.SourceFormatFacets)
}
