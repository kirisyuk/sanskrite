package qa

import (
	"context"
	"log"
	"strings"

	"github.com/igorkirisyuk/sanskrite/internal/horizon/interpretation"
	horizonmodels "github.com/igorkirisyuk/sanskrite/internal/horizon/models"
)

// RunFinishInterpretation runs Interpreter on top affected nodes after propagation.
// Humanization only at the end: takes 12-axis simulation output → human-readable interpretations.
func RunFinishInterpretation(
	ctx context.Context,
	interp *interpretation.Interpreter,
	simResult *horizonmodels.SimulationResult,
	fragments []*horizonmodels.HorizonFragment,
	polarity string,
	maxNodes int,
) []*interpretation.RiskInterpretation {
	if interp == nil || simResult == nil || len(simResult.NodeImpacts) == 0 {
		return nil
	}
	if maxNodes <= 0 {
		maxNodes = 5
	}
	if polarity == "" {
		polarity = "risk"
	}

	fragByID := make(map[string]*horizonmodels.HorizonFragment)
	for _, f := range fragments {
		if f != nil && f.ID != "" {
			fragByID[f.ID] = f
		}
	}

	// Leaf evidence by fragmentID
	leafByFragID := make(map[string][]horizonmodels.LeafSignalEvidence)
	for _, le := range simResult.LeafEvidence {
		if le.FragmentID != "" {
			leafByFragID[le.FragmentID] = append(leafByFragID[le.FragmentID], le)
		}
	}

	var out []*interpretation.RiskInterpretation
	for i := 0; i < len(simResult.NodeImpacts) && i < maxNodes; i++ {
		ni := simResult.NodeImpacts[i]
		if ni.FragmentID == "" || len(ni.AxisAmplitudes) == 0 {
			continue
		}
		frag := fragByID[ni.FragmentID]
		leafEv := leafByFragID[ni.FragmentID]

		interpr, err := interp.InterpretFromNodeImpact(ctx, frag, &ni, leafEv, polarity)
		if err != nil {
			log.Printf("qa finish_interpret skip fragmentId=%s err=%v", strings.TrimSpace(ni.FragmentID), err)
			continue
		}
		out = append(out, interpr)
	}
	return out
}
