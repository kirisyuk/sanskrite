package focusobject

import (
	"time"

	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
	coremodels "github.com/igorkirisyuk/sanskrite/internal/models"
)

// Fixture is the local end-to-end input for focus-object analysis.
// It intentionally uses core models so the same logic can later move into runtime services.
type Fixture struct {
	Focus     FocusObjectInput                      `json:"focus"`
	Fragments []*horizonmodels.HorizonFragment      `json:"fragments,omitempty"`
	Edges     []*horizonmodels.DependencyEdge       `json:"edges,omitempty"`
	Artifacts []*horizonmodels.MaterializedArtifact `json:"artifacts,omitempty"`
	Facts     []*coremodels.OntologyFact            `json:"facts,omitempty"`
	Signals   []*horizonmodels.SignalDescriptor     `json:"signals,omitempty"`
}

type FocusObjectInput struct {
	AnchorCanonicalPath   string   `json:"anchorCanonicalPath"`
	DisplayLabel          string   `json:"displayLabel,omitempty"`
	RelatedCanonicalPaths []string `json:"relatedCanonicalPaths,omitempty"`
	DomainModes           []string `json:"domainModes,omitempty"`
	QueryOverrides        []string `json:"queryOverrides,omitempty"`
	UniverseDepth         int      `json:"universeDepth,omitempty"`
	LookbackHours         int      `json:"lookbackHours,omitempty"`
	MaxImpactedEntities   int      `json:"maxImpactedEntities,omitempty"`
	MaxPropagationHops    int      `json:"maxPropagationHops,omitempty"`
}

type AnalyzeRequest struct {
	Focus     FocusObjectInput
	Fragments []*horizonmodels.HorizonFragment
	Edges     []*horizonmodels.DependencyEdge
	Artifacts []*horizonmodels.MaterializedArtifact
	Facts     []*coremodels.OntologyFact
	Signals   []*horizonmodels.SignalDescriptor
	Now       time.Time
}

type SuggestedWatchSubject struct {
	AnchorCanonicalPath   string   `json:"anchorCanonicalPath"`
	DisplayLabel          string   `json:"displayLabel"`
	UniversePaths         []string `json:"universePaths,omitempty"`
	BackingCanonicalPaths []string `json:"backingCanonicalPaths,omitempty"`
	ArtifactCanonicalKeys []string `json:"artifactCanonicalKeys,omitempty"`
	DomainModes           []string `json:"domainModes,omitempty"`
	QueryOverrides        []string `json:"queryOverrides,omitempty"`
	SourceMode            string   `json:"sourceMode"`
	RuntimeMode           string   `json:"runtimeMode"`
}

type UniverseNode struct {
	CanonicalPath string   `json:"canonicalPath"`
	Label         string   `json:"label,omitempty"`
	Type          string   `json:"type,omitempty"`
	Reasons       []string `json:"reasons,omitempty"`
	Distance      int      `json:"distance"`
	InMatchedPath bool     `json:"inMatchedPath,omitempty"`
}

type PositionSummary struct {
	RiskPressure        float64  `json:"riskPressure"`
	OpportunityPressure float64  `json:"opportunityPressure"`
	ArtifactExposure    float64  `json:"artifactExposure"`
	CoverageScore       float64  `json:"coverageScore"`
	MonitoringReadiness float64  `json:"monitoringReadiness"`
	DominantAxes        []string `json:"dominantAxes,omitempty"`
	Warnings            []string `json:"warnings,omitempty"`
}

type SignalSummary struct {
	SignalID       string    `json:"signalId"`
	FactID         string    `json:"factId,omitempty"`
	Headline       string    `json:"headline"`
	Summary        string    `json:"summary,omitempty"`
	Polarity       string    `json:"polarity"`
	Axis           string    `json:"axis"`
	Mechanism      string    `json:"mechanism,omitempty"`
	Score          float64   `json:"score"`
	Directness     string    `json:"directness"`
	ObservedAt     time.Time `json:"observedAt"`
	SourceRefs     []string  `json:"sourceRefs,omitempty"`
	MatchedPaths   []string  `json:"matchedPaths,omitempty"`
	ConcernedPaths []string  `json:"concernedPaths,omitempty"`
	Why            string    `json:"why,omitempty"`
}

type PathExposure struct {
	ArtifactID           string   `json:"artifactId,omitempty"`
	ArtifactCanonicalKey string   `json:"artifactCanonicalKey"`
	DisplayName          string   `json:"displayName,omitempty"`
	ProductAxis          string   `json:"productAxis,omitempty"`
	ExposureScore        float64  `json:"exposureScore"`
	PathStressScore      float64  `json:"pathStressScore"`
	BindingScore         float64  `json:"bindingScore"`
	RecoveryLatencyHrs   float64  `json:"recoveryLatencyHours"`
	MatchedSignals       []string `json:"matchedSignals,omitempty"`
	CounterpartyPaths    []string `json:"counterpartyPaths,omitempty"`
	Why                  string   `json:"why,omitempty"`
}

type ImpactedEntity struct {
	CanonicalPath  string   `json:"canonicalPath"`
	Label          string   `json:"label,omitempty"`
	Type           string   `json:"type,omitempty"`
	ImpactScore    float64  `json:"impactScore"`
	Hops           int      `json:"hops,omitempty"`
	Classification string   `json:"classification,omitempty"`
	Drivers        []string `json:"drivers,omitempty"`
	Path           []string `json:"path,omitempty"`
	Why            string   `json:"why,omitempty"`
}

type PropagationReached struct {
	CanonicalPath  string   `json:"canonicalPath"`
	Label          string   `json:"label,omitempty"`
	Type           string   `json:"type,omitempty"`
	Score          float64  `json:"score"`
	Hops           int      `json:"hops"`
	Classification string   `json:"classification,omitempty"`
	Path           []string `json:"path,omitempty"`
}

type PropagationStep struct {
	FromPath        string  `json:"fromPath"`
	ToPath          string  `json:"toPath"`
	EdgeType        string  `json:"edgeType"`
	Reverse         bool    `json:"reverse,omitempty"`
	Hop             int     `json:"hop"`
	AxisIn          string  `json:"axisIn,omitempty"`
	AxisOut         string  `json:"axisOut,omitempty"`
	InputScore      float64 `json:"inputScore"`
	EdgeWeight      float64 `json:"edgeWeight"`
	Amplification   float64 `json:"amplification"`
	Criticality     float64 `json:"criticality"`
	PreDampingScore float64 `json:"preDampingScore"`
	TransformFactor float64 `json:"transformFactor,omitempty"`
	TransformReason string  `json:"transformReason,omitempty"`
	OutputScore     float64 `json:"outputScore"`
	Outcome         string  `json:"outcome"`
	Reason          string  `json:"reason,omitempty"`
}

type PropagationAudit struct {
	SignalID               string                `json:"signalId"`
	Headline               string                `json:"headline"`
	Axis                   string                `json:"axis"`
	Polarity               string                `json:"polarity"`
	SignalScore            float64               `json:"signalScore"`
	SeedPaths              []string              `json:"seedPaths,omitempty"`
	MaxHops                int                   `json:"maxHops"`
	MinAmplitudeThreshold  float64               `json:"minAmplitudeThreshold"`
	WeakAmplitudeThreshold float64               `json:"weakAmplitudeThreshold"`
	WeakAmplitudeHopCutoff int                   `json:"weakAmplitudeHopCutoff"`
	PathFamilies           []PathFamilyHighlight `json:"pathFamilies,omitempty"`
	Reached                []PropagationReached  `json:"reached,omitempty"`
	Steps                  []PropagationStep     `json:"steps,omitempty"`
}

type PathFamilyHighlight struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Score       float64  `json:"score"`
	Steps       int      `json:"steps,omitempty"`
	MaxHops     int      `json:"maxHops,omitempty"`
	Channels    []string `json:"channels,omitempty"`
	EdgeTypes   []string `json:"edgeTypes,omitempty"`
	SamplePaths []string `json:"samplePaths,omitempty"`
}

type AffinityCluster struct {
	ClusterID       string   `json:"clusterId"`
	Kind            string   `json:"kind"`
	Label           string   `json:"label,omitempty"`
	CanonicalPaths  []string `json:"canonicalPaths,omitempty"`
	AnchorContained bool     `json:"anchorContained,omitempty"`
	Reasons         []string `json:"reasons,omitempty"`
}

type EndpointHit struct {
	CanonicalPath   string   `json:"canonicalPath"`
	Label           string   `json:"label,omitempty"`
	Type            string   `json:"type,omitempty"`
	PeakScore       float64  `json:"peakScore"`
	AggregateScore  float64  `json:"aggregateScore"`
	MaxHops         int      `json:"maxHops,omitempty"`
	DominantAxis    string   `json:"dominantAxis,omitempty"`
	Polarities      []string `json:"polarities,omitempty"`
	SignalIDs       []string `json:"signalIds,omitempty"`
	SignalHeadlines []string `json:"signalHeadlines,omitempty"`
	PathFamilies    []string `json:"pathFamilies,omitempty"`
	Classifications []string `json:"classifications,omitempty"`
	SamplePath      []string `json:"samplePath,omitempty"`
	Scope           string   `json:"scope,omitempty"`
	ClusterID       string   `json:"clusterId,omitempty"`
	Why             string   `json:"why,omitempty"`
}

type InterpretationContextNode struct {
	CanonicalPath string   `json:"canonicalPath"`
	Label         string   `json:"label,omitempty"`
	Type          string   `json:"type,omitempty"`
	Relationship  string   `json:"relationship"`
	Reasons       []string `json:"reasons,omitempty"`
}

type EndpointInterpretationPacket struct {
	PacketID        string                      `json:"packetId"`
	CanonicalPath   string                      `json:"canonicalPath"`
	Label           string                      `json:"label,omitempty"`
	Type            string                      `json:"type,omitempty"`
	PeakScore       float64                     `json:"peakScore"`
	AggregateScore  float64                     `json:"aggregateScore"`
	MaxHops         int                         `json:"maxHops,omitempty"`
	Axes            []string                    `json:"axes,omitempty"`
	Polarities      []string                    `json:"polarities,omitempty"`
	SignalIDs       []string                    `json:"signalIds,omitempty"`
	SignalHeadlines []string                    `json:"signalHeadlines,omitempty"`
	PathFamilies    []string                    `json:"pathFamilies,omitempty"`
	SamplePath      []string                    `json:"samplePath,omitempty"`
	ClusterID       string                      `json:"clusterId,omitempty"`
	ClusterPeers    []string                    `json:"clusterPeers,omitempty"`
	Context         []InterpretationContextNode `json:"context,omitempty"`
	Why             string                      `json:"why,omitempty"`
}

type BusinessImpact struct {
	CanonicalPath   string   `json:"canonicalPath"`
	Label           string   `json:"label,omitempty"`
	Type            string   `json:"type,omitempty"`
	Stance          string   `json:"stance"`
	Interestingness float64  `json:"interestingness"`
	ImpactScore     float64  `json:"impactScore"`
	Hops            int      `json:"hops,omitempty"`
	Remote          bool     `json:"remote,omitempty"`
	Families        []string `json:"families,omitempty"`
	Drivers         []string `json:"drivers,omitempty"`
	Reasons         []string `json:"reasons,omitempty"`
	Path            []string `json:"path,omitempty"`
	Why             string   `json:"why,omitempty"`
}

// TodayMention is the compact machine-readable unit for later daily digest compilation.
type TodayMention struct {
	ID                   string   `json:"id"`
	Headline             string   `json:"headline"`
	Polarity             string   `json:"polarity"`
	Score                float64  `json:"score"`
	SubjectCanonicalPath string   `json:"subjectCanonicalPath"`
	ConcernedPaths       []string `json:"concernedPaths,omitempty"`
	Why                  string   `json:"why,omitempty"`
	SourceSignalID       string   `json:"sourceSignalId,omitempty"`
}

type Snapshot struct {
	Focus            SuggestedWatchSubject          `json:"focus"`
	Position         PositionSummary                `json:"position"`
	Universe         []UniverseNode                 `json:"universe,omitempty"`
	Threats          []SignalSummary                `json:"threats,omitempty"`
	Tailwinds        []SignalSummary                `json:"tailwinds,omitempty"`
	PathExposures    []PathExposure                 `json:"pathExposures,omitempty"`
	AffinityClusters []AffinityCluster              `json:"affinityClusters,omitempty"`
	EndpointHits     []EndpointHit                  `json:"endpointHits,omitempty"`
	EndpointPackets  []EndpointInterpretationPacket `json:"endpointPackets,omitempty"`
	ImpactedEntities []ImpactedEntity               `json:"impactedEntities,omitempty"`
	BusinessImpacts  []BusinessImpact               `json:"businessImpacts,omitempty"`
	Propagation      []PropagationAudit             `json:"propagation,omitempty"`
	TodayMentions    []TodayMention                 `json:"todayMentions,omitempty"`
	GeneratedAt      time.Time                      `json:"generatedAt"`
}
