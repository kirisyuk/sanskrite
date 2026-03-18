package models

// Response-only deterministic overlays for supply-chain explainability on
// object, path/leg, and lens scopes. These are derived from stored metrics,
// qualifiers, and path artifacts; they are not persisted as canonical truth.

type SupplyChainOverlayMechanism struct {
	Key           string  `json:"key"`
	Label         string  `json:"label,omitempty"`
	Score         float64 `json:"score,omitempty"`
	EvidenceCount int     `json:"evidenceCount,omitempty"`
}

type SupplyChainOverlayMetric struct {
	Key        string  `json:"key"`
	Label      string  `json:"label,omitempty"`
	Value      float64 `json:"value,omitempty"`
	Unit       string  `json:"unit,omitempty"`
	Scope      string  `json:"scope,omitempty"`
	Importance float64 `json:"importance,omitempty"`
}

type SupplyChainOverlayProduct struct {
	Ref           string  `json:"ref"`
	Label         string  `json:"label,omitempty"`
	Score         float64 `json:"score,omitempty"`
	LinkCount     int     `json:"linkCount,omitempty"`
	PathCount     int     `json:"pathCount,omitempty"`
	CapabilityRef string  `json:"capabilityRef,omitempty"`
}

type SupplyChainOverlayNode struct {
	Ref       string  `json:"ref"`
	Label     string  `json:"label,omitempty"`
	Role      string  `json:"role,omitempty"`
	Score     float64 `json:"score,omitempty"`
	PathCount int     `json:"pathCount,omitempty"`
}

type SupplyChainOverlayPathRef struct {
	ArtifactID        string   `json:"artifactId,omitempty"`
	CanonicalKey      string   `json:"canonicalKey,omitempty"`
	DisplayName       string   `json:"displayName,omitempty"`
	ProductRef        string   `json:"productRef,omitempty"`
	ProductLabel      string   `json:"productLabel,omitempty"`
	SupplierRef       string   `json:"supplierRef,omitempty"`
	SupplierLabel     string   `json:"supplierLabel,omitempty"`
	CustomerRef       string   `json:"customerRef,omitempty"`
	CustomerLabel     string   `json:"customerLabel,omitempty"`
	PrimaryMechanism  string   `json:"primaryMechanism,omitempty"`
	Criticality       float64  `json:"criticality,omitempty"`
	StressScore       float64  `json:"stressScore,omitempty"`
	Bottleneck        bool     `json:"bottleneck,omitempty"`
	TransportViaRefs  []string `json:"transportViaRefs,omitempty"`
	RouteSummary      string   `json:"routeSummary,omitempty"`
	CriticalSummary   string   `json:"criticalSummary,omitempty"`
}

type SupplyChainLegRiskOverlay struct {
	SegmentIndex       int                           `json:"segmentIndex"`
	Summary            string                        `json:"summary,omitempty"`
	PrimaryMechanism   string                        `json:"primaryMechanism,omitempty"`
	Criticality        float64                       `json:"criticality,omitempty"`
	StressScore        float64                       `json:"stressScore,omitempty"`
	Bottleneck         bool                          `json:"bottleneck,omitempty"`
	ProductRef         string                        `json:"productRef,omitempty"`
	ProductLabel       string                        `json:"productLabel,omitempty"`
	SupplierRef        string                        `json:"supplierRef,omitempty"`
	SupplierLabel      string                        `json:"supplierLabel,omitempty"`
	CustomerRef        string                        `json:"customerRef,omitempty"`
	CustomerLabel      string                        `json:"customerLabel,omitempty"`
	TransportViaRefs   []string                      `json:"transportViaRefs,omitempty"`
	Flags              []string                      `json:"flags,omitempty"`
	Mechanisms         []SupplyChainOverlayMechanism `json:"mechanisms,omitempty"`
	DriverMetrics      []SupplyChainOverlayMetric    `json:"driverMetrics,omitempty"`
}

type SupplyChainPathRiskOverlay struct {
	Scope             string                        `json:"scope,omitempty"`
	Summary           string                        `json:"summary,omitempty"`
	PrimaryMechanism  string                        `json:"primaryMechanism,omitempty"`
	Criticality       float64                       `json:"criticality,omitempty"`
	StressScore       float64                       `json:"stressScore,omitempty"`
	BottleneckLegIndex int                          `json:"bottleneckLegIndex,omitempty"`
	Flags             []string                      `json:"flags,omitempty"`
	Mechanisms        []SupplyChainOverlayMechanism `json:"mechanisms,omitempty"`
	Products          []SupplyChainOverlayProduct   `json:"products,omitempty"`
	Bottlenecks       []SupplyChainOverlayNode      `json:"bottlenecks,omitempty"`
	DriverMetrics     []SupplyChainOverlayMetric    `json:"driverMetrics,omitempty"`
	Legs              []SupplyChainLegRiskOverlay   `json:"legs,omitempty"`
}

type SupplyChainObjectRiskOverlay struct {
	Scope             string                        `json:"scope,omitempty"`
	FragmentID        string                        `json:"fragmentId,omitempty"`
	FragmentRef       string                        `json:"fragmentRef,omitempty"`
	FragmentLabel     string                        `json:"fragmentLabel,omitempty"`
	Summary           string                        `json:"summary,omitempty"`
	PrimaryMechanism  string                        `json:"primaryMechanism,omitempty"`
	Criticality       float64                       `json:"criticality,omitempty"`
	IncidentPathCount int                           `json:"incidentPathCount,omitempty"`
	StressedPathCount int                           `json:"stressedPathCount,omitempty"`
	TopProducts       []SupplyChainOverlayProduct   `json:"topProducts,omitempty"`
	TopMechanisms     []SupplyChainOverlayMechanism `json:"topMechanisms,omitempty"`
	TopBottlenecks    []SupplyChainOverlayNode      `json:"topBottlenecks,omitempty"`
	DriverMetrics     []SupplyChainOverlayMetric    `json:"driverMetrics,omitempty"`
	TopPaths          []SupplyChainOverlayPathRef   `json:"topPaths,omitempty"`
}

type SupplyChainLensRiskOverlay struct {
	Scope            string                        `json:"scope,omitempty"`
	Summary          string                        `json:"summary,omitempty"`
	PrimaryMechanism string                        `json:"primaryMechanism,omitempty"`
	Criticality      float64                       `json:"criticality,omitempty"`
	TotalPaths       int                           `json:"totalPaths,omitempty"`
	StressedPaths    int                           `json:"stressedPaths,omitempty"`
	TopProducts      []SupplyChainOverlayProduct   `json:"topProducts,omitempty"`
	TopMechanisms    []SupplyChainOverlayMechanism `json:"topMechanisms,omitempty"`
	TopBottlenecks   []SupplyChainOverlayNode      `json:"topBottlenecks,omitempty"`
	TopNodes         []SupplyChainOverlayNode      `json:"topNodes,omitempty"`
	DriverMetrics    []SupplyChainOverlayMetric    `json:"driverMetrics,omitempty"`
	TopPaths         []SupplyChainOverlayPathRef   `json:"topPaths,omitempty"`
}
