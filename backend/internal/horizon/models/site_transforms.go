package models

import (
	"encoding/json"
	"sort"
	"strings"
)

// Attribute key for site-local product transformation records (AH).
const attrSiteTransforms = "site_transforms"

// SiteTransform describes extraction / refining / processing / assembly on a site node.
type SiteTransform struct {
	Kind           string   `json:"kind"` // extraction|refining|processing|assembly|transport (transport is rare on-site; prefer leg_connects)
	InputProducts  []string `json:"inputProducts,omitempty"`
	OutputProducts []string `json:"outputProducts,omitempty"`
	YieldRatio     float64  `json:"yieldRatio,omitempty"`
	Confidence     float64  `json:"confidence,omitempty"`
	Source         string   `json:"source,omitempty"`
}

// SiteTransforms decodes Attributes[attrSiteTransforms] into a slice.
func (f *HorizonFragment) SiteTransforms() []SiteTransform {
	if f == nil || f.Attributes == nil {
		return nil
	}
	raw, ok := f.Attributes[attrSiteTransforms]
	if !ok || raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out []SiteTransform
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

// SetSiteTransforms replaces the site_transforms attribute bag entry.
func (f *HorizonFragment) SetSiteTransforms(st []SiteTransform) {
	if f == nil {
		return
	}
	if len(st) == 0 {
		f.Attributes = attrBagDelete(f.Attributes, attrSiteTransforms)
		if len(f.Attributes) == 0 {
			f.Attributes = nil
		}
		return
	}
	f.Attributes = attrBagSet(f.Attributes, attrSiteTransforms, st)
}

// AppendSiteTransform appends st unless an entry with the same (kind, inputProducts, outputProducts) already exists.
func (f *HorizonFragment) AppendSiteTransform(st SiteTransform) {
	if f == nil {
		return
	}
	cur := f.SiteTransforms()
	key := siteTransformIdentityKey(st)
	for _, x := range cur {
		if siteTransformIdentityKey(x) == key {
			return
		}
	}
	cur = append(cur, st)
	f.SetSiteTransforms(cur)
}

func siteTransformIdentityKey(st SiteTransform) string {
	in := append([]string(nil), st.InputProducts...)
	out := append([]string(nil), st.OutputProducts...)
	sort.Strings(in)
	sort.Strings(out)
	return strings.TrimSpace(st.Kind) + "|" + strings.Join(in, ",") + "->" + strings.Join(out, ",")
}
