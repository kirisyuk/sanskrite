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
}

// SyncCompatForWrite performs compatibility sync before writes/serialization.
func (ds *DataSource) SyncCompatForWrite() {
	if ds == nil {
		return
	}
	ds.Kind = NormalizeSourceKind(ds.Kind)
	ds.Details.SyncHorizonMonitorAliases()
}
