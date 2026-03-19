package models

import (
	"time"

	"github.com/igorkirisyuk/sanskrite/internal/taxonomy"
	"google.golang.org/genproto/googleapis/type/latlng"
)

// NodeKind distinguishes Material from Facility in the graph.
type NodeKind string

const (
	NodeKindMaterial NodeKind = "material"
	NodeKindFacility NodeKind = "facility"
)

// Material category from whitepaper.
const (
	CategoryCriticalMineral  = "critical_mineral"
	CategoryEnergyCommodity  = "energy_commodity"
	CategoryAgricultural    = "agricultural"
)

// RouteType for transport.
type RouteType string

const (
	RouteTypeSea       RouteType = "SEA"
	RouteTypeLand      RouteType = "LAND"
	RouteTypeAir       RouteType = "AIR"
	RouteTypePipeline  RouteType = "PIPELINE"
	RouteTypeRail      RouteType = "RAIL"
)

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

// ProcessingStepType for edges.
type ProcessingStepType string

const (
	StepExtraction    ProcessingStepType = "EXTRACTION"
	StepRefining      ProcessingStepType = "REFINING"
	StepProcessing    ProcessingStepType = "PROCESSING"
	StepManufacturing ProcessingStepType = "MANUFACTURING"
	StepTransport     ProcessingStepType = "TRANSPORT"
)

// Material represents a critical material (whitepaper 3.3).
type RiskPoint struct {
	Timestamp time.Time `firestore:"timestamp" json:"timestamp"`
	Value     float64   `firestore:"value" json:"value"`
}

type Material struct {
	ID               string      `json:"id" firestore:"id"`
	Name             string      `json:"name" firestore:"name"`
	Aliases          []string    `json:"aliases,omitempty" firestore:"aliases,omitempty"`
	Category         string      `json:"category" firestore:"category"`
	CriticalityScore float64     `json:"criticalityScore" firestore:"criticalityScore"`
	RiskScore        float64     `json:"riskScore" firestore:"riskScore"`
	RiskHistory      []RiskPoint `json:"riskHistory,omitempty" firestore:"riskHistory,omitempty"`
	RiskDelta7d      float64     `json:"riskDelta7d" firestore:"-"`
	HSCodes          []string    `json:"hsCodes,omitempty" firestore:"hsCodes,omitempty"`
	CASNumber        string      `json:"casNumber,omitempty" firestore:"casNumber,omitempty"`
	UpdatedAt        time.Time   `json:"updatedAt" firestore:"updatedAt"`
}

// Facility represents a physical facility (mine, refinery, etc.) (whitepaper 3.3).
type Facility struct {
	ID                   string              `json:"id" firestore:"id"`
	CanonicalPath        string              `json:"canonicalPath" firestore:"canonicalPath"`
	Name                 string              `json:"name" firestore:"name"`
	FacilityKind         taxonomy.FacilityKind `json:"facilityKind" firestore:"facilityKind"`
	FacilityRole         taxonomy.FacilityRole `json:"facilityRole" firestore:"facilityRole"`
	Country              string              `json:"country" firestore:"country"`
	Coordinates          *latlng.LatLng      `json:"coordinates,omitempty" firestore:"coordinates,omitempty"`
	Operators            []string            `json:"operators,omitempty" firestore:"operators,omitempty"`
	Capacity             float64             `json:"capacity,omitempty" firestore:"capacity,omitempty"`
	CapacityUnit         string              `json:"capacityUnit,omitempty" firestore:"capacityUnit,omitempty"`
	ProductionMaterials  []string            `json:"productionMaterials,omitempty" firestore:"productionMaterials,omitempty"`
	MaterialID           string              `json:"materialId" firestore:"materialId"`
	Stage                int                 `json:"stage" firestore:"stage"`
	RiskScore            float64             `json:"riskScore" firestore:"riskScore"`
	Sanctioned           bool                `json:"sanctioned" firestore:"sanctioned"`
	UpdatedAt            time.Time           `json:"updatedAt" firestore:"updatedAt"`
}

// Route represents a transport route between facilities (whitepaper 3.3).
type Route struct {
	ID            string        `json:"id" firestore:"id"`
	Type          RouteType     `json:"type" firestore:"type"`
	Waypoints     []*latlng.LatLng `json:"waypoints,omitempty" firestore:"waypoints,omitempty"`
	FromFacility  string        `json:"fromFacility" firestore:"fromFacility"`
	ToFacility    string        `json:"toFacility" firestore:"toFacility"`
	TypicalVolume float64       `json:"typicalVolume,omitempty" firestore:"typicalVolume,omitempty"`
	VolumeUnit    string        `json:"volumeUnit,omitempty" firestore:"volumeUnit,omitempty"`
	RiskScore     float64       `json:"riskScore" firestore:"riskScore"`
	UpdatedAt     time.Time     `json:"updatedAt" firestore:"updatedAt"`
}

// ConflictZone represents a risk zone (whitepaper 3.3).
type ConflictZone struct {
	ID                string               `json:"id" firestore:"id"`
	Name              string               `json:"name" firestore:"name"`
	Country           string               `json:"country,omitempty" firestore:"country,omitempty"` // ISO 3166-1 alpha-2/3 for choropleth
	BoundingBox       []*latlng.LatLng    `json:"boundingBox,omitempty" firestore:"boundingBox,omitempty"`
	Severity          ConflictZoneSeverity `json:"severity" firestore:"severity"`
	Type              ConflictZoneType    `json:"type" firestore:"type"`
	AffectedMaterials []string             `json:"affectedMaterials,omitempty" firestore:"affectedMaterials,omitempty"`
	Source            string               `json:"source" firestore:"source"`
	UpdatedAt         time.Time            `json:"updatedAt" firestore:"updatedAt"`
}

// ProcessingStep represents an edge in the supply chain graph (whitepaper 3.3).
type ProcessingStep struct {
	From                   string             `json:"from" firestore:"from"`
	To                     string             `json:"to" firestore:"to"`
	Type                   ProcessingStepType `json:"type" firestore:"type"`
	YieldRatio             float64            `json:"yieldRatio,omitempty" firestore:"yieldRatio,omitempty"`
	KeyPlayers             []string           `json:"keyPlayers,omitempty" firestore:"keyPlayers,omitempty"`
	GeographicConcentration float64            `json:"geographicConcentration,omitempty" firestore:"geographicConcentration,omitempty"`
	RiskMultiplier         float64            `json:"riskMultiplier,omitempty" firestore:"riskMultiplier,omitempty"`
}

// NodeDocument is the Firestore document shape for criticalchain_nodes.
// Stores either Material or Facility; Kind discriminates.
type NodeDocument struct {
	ID        string    `firestore:"id"`
	Kind      NodeKind  `firestore:"kind"`
	Material  *Material `firestore:"material,omitempty"`
	Facility  *Facility `firestore:"facility,omitempty"`
	UpdatedAt time.Time `firestore:"updatedAt"`
}

// EdgeDocument is the Firestore document shape for criticalchain_edges.
// Stores either ProcessingStep or Route; Kind discriminates.
type EdgeKind string

const (
	EdgeKindProcessingStep EdgeKind = "processing_step"
	EdgeKindRoute          EdgeKind = "route"
)

type EdgeDocument struct {
	ID             string           `firestore:"id"`
	Kind           EdgeKind         `firestore:"kind"`
	ProcessingStep *ProcessingStep  `firestore:"processingStep,omitempty"`
	Route          *Route           `firestore:"route,omitempty"`
	UpdatedAt      time.Time        `firestore:"updatedAt"`
}
