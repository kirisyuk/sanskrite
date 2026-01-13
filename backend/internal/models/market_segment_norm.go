package models

// MarketSegmentNorm is a derived, normalized view of a market segment path.
// It is stored on Horizon fragments as a denormalized helper for UI + filtering.
type MarketSegmentNorm struct {
	SegmentPath  string `json:"segmentPath" firestore:"segmentPath"`
	NormDomain   string `json:"normDomain" firestore:"normDomain"`
	NormVertical string `json:"normVertical,omitempty" firestore:"normVertical,omitempty"`
	NormPair     string `json:"normPair" firestore:"normPair"`
	// Channel captures B2B/B2C/Public-sector-like context when the original segment used a channel-like domain.
	// It is optional and may be empty.
	Channel string `json:"channel,omitempty" firestore:"channel,omitempty"`
}
