package criticalchainview

import (
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"google.golang.org/genproto/googleapis/type/latlng"
)

// Material is the CriticalChain API wire shape (legacy JSON field names).
type Material struct {
	ID               string      `json:"id"`
	Name             string      `json:"name"`
	Aliases          []string    `json:"aliases,omitempty"`
	Category         string      `json:"category"`
	CriticalityScore float64     `json:"criticalityScore"`
	RiskScore        float64     `json:"riskScore"`
	RiskHistory      []RiskPoint `json:"riskHistory,omitempty"`
	RiskDelta7d      float64     `json:"riskDelta7d"`
	HSCodes          []string    `json:"hsCodes,omitempty"`
	CASNumber        string      `json:"casNumber,omitempty"`
	UpdatedAt        time.Time   `json:"updatedAt"`
}

// RiskPoint is a timestamped risk sample.
type RiskPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Value     float64   `json:"value"`
}

// Facility is the CriticalChain API wire shape for a physical site.
type Facility struct {
	ID                  string                `json:"id"`
	CanonicalPath       string                `json:"canonicalPath"`
	Name                string                `json:"name"`
	FacilityKind        taxonomy.FacilityKind `json:"facilityKind"`
	FacilityRole        taxonomy.FacilityRole `json:"facilityRole"`
	Country             string                `json:"country"`
	Coordinates         *latlng.LatLng        `json:"coordinates,omitempty"`
	Operators           []string              `json:"operators,omitempty"`
	Capacity            float64               `json:"capacity,omitempty"`
	CapacityUnit        string                `json:"capacityUnit,omitempty"`
	ProductionMaterials []string              `json:"productionMaterials,omitempty"`
	MaterialID          string                `json:"materialId"`
	Stage               int                   `json:"stage"`
	RiskScore           float64               `json:"riskScore"`
	Sanctioned          bool                  `json:"sanctioned"`
	UpdatedAt           time.Time             `json:"updatedAt"`
}

// RouteType for transport.
type RouteType string

const (
	RouteTypeSea      RouteType = "SEA"
	RouteTypeLand     RouteType = "LAND"
	RouteTypeAir      RouteType = "AIR"
	RouteTypePipeline RouteType = "PIPELINE"
	RouteTypeRail     RouteType = "RAIL"
)

// Route is the CriticalChain API wire shape for logistics corridors.
type Route struct {
	ID            string           `json:"id"`
	Type          RouteType        `json:"type"`
	Waypoints     []*latlng.LatLng `json:"waypoints,omitempty"`
	FromFacility  string           `json:"fromFacility"`
	ToFacility    string           `json:"toFacility"`
	TypicalVolume float64          `json:"typicalVolume,omitempty"`
	VolumeUnit    string           `json:"volumeUnit,omitempty"`
	RiskScore     float64          `json:"riskScore"`
	UpdatedAt     time.Time        `json:"updatedAt"`
}

// ConflictZoneSeverity from whitepaper.
type ConflictZoneSeverity string

const (
	SeverityLow      ConflictZoneSeverity = "LOW"
	SeverityMedium   ConflictZoneSeverity = "MEDIUM"
	SeverityHigh     ConflictZoneSeverity = "HIGH"
	SeverityCritical ConflictZoneSeverity = "CRITICAL"
)

// ConflictZoneType from whitepaper.
type ConflictZoneType string

const (
	ConflictTypeArmed     ConflictZoneType = "ARMED"
	ConflictTypeSanctions ConflictZoneType = "SANCTIONS"
	ConflictTypeNatural   ConflictZoneType = "NATURAL"
	ConflictTypePolitical ConflictZoneType = "POLITICAL"
)

// ConflictZone is the CriticalChain API wire shape.
type ConflictZone struct {
	ID                string               `json:"id"`
	Name              string               `json:"name"`
	Country           string               `json:"country,omitempty"`
	BoundingBox       []*latlng.LatLng     `json:"boundingBox,omitempty"`
	Severity          ConflictZoneSeverity `json:"severity"`
	Type              ConflictZoneType     `json:"type"`
	AffectedMaterials []string             `json:"affectedMaterials,omitempty"`
	Source            string               `json:"source"`
	UpdatedAt         time.Time            `json:"updatedAt"`
}

// ProcessingStepType for edges in the demo graph.
type ProcessingStepType string

const (
	StepExtraction    ProcessingStepType = "EXTRACTION"
	StepRefining      ProcessingStepType = "REFINING"
	StepProcessing    ProcessingStepType = "PROCESSING"
	StepManufacturing ProcessingStepType = "MANUFACTURING"
	StepTransport     ProcessingStepType = "TRANSPORT"
)

// ProcessingStep is an edge in the supply chain graph response.
type ProcessingStep struct {
	From                    string             `json:"from"`
	To                      string             `json:"to"`
	Type                    ProcessingStepType `json:"type"`
	YieldRatio              float64            `json:"yieldRatio,omitempty"`
	KeyPlayers              []string           `json:"keyPlayers,omitempty"`
	GeographicConcentration float64            `json:"geographicConcentration,omitempty"`
	RiskMultiplier          float64            `json:"riskMultiplier,omitempty"`
}

// GraphResult is the graph response for a material.
type GraphResult struct {
	Material        *Material         `json:"material,omitempty"`
	Materials       []*Material       `json:"materials,omitempty"`
	Facilities      []*Facility       `json:"facilities"`
	ProcessingSteps []*ProcessingStep `json:"processingSteps"`
}

// Chokepoint represents a strategic maritime/transit point (whitepaper 4.4).
type Chokepoint struct {
	ID             string  `json:"id"`
	Path           string  `json:"path"`
	Name           string  `json:"name"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	ShareOfSupply  float64 `json:"shareOfSupply,omitempty"`
	RiskMultiplier float64 `json:"riskMultiplier,omitempty"`
	MaterialID     string  `json:"materialId,omitempty"`
}

// ListFacilitiesOpts filters ListFacilities.
type ListFacilitiesOpts struct {
	MaterialID   string
	Country      string
	MinRiskScore float64
}
