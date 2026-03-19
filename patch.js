const fs = require('fs');

// Patching fusion.go
let fusionGo = fs.readFileSync('/workspace/sanskrite/backend/internal/criticalchain/signal/fusion.go', 'utf8');
fusionGo = fusionGo.replace(/func \(\w+ \*fusionService\) fuseSignals\(signals \[\]types\.RiskSignal\) types\.RiskScore \{/g,
  `func (s *fusionService) fuseSignals(signals []types.RiskSignal) types.RiskScore {
	if len(signals) == 0 {
		return types.RiskScore{Level: 1.0, Confidence: 1.0}
	}

	decayHalflifeHours := 24.0 // configurable or constant

	// applying decay
	var decayedSignals []types.RiskSignal
	now := time.Now()
	for _, sig := range signals {
		ageHours := now.Sub(sig.Timestamp).Hours()
		if ageHours < 0 {
			ageHours = 0
		}
		weight := math.Exp(-math.Ln2 * ageHours / decayHalflifeHours)
		decayedSig := sig
		decayedSig.Confidence *= weight
		decayedSignals = append(decayedSignals, decayedSig)
	}

	signals = decayedSignals
`);
// Wait, we need to import time and math
if (!fusionGo.includes('"time"')) {
    fusionGo = fusionGo.replace('import (', "import (\n\t\"time\"\n\t\"math\"\n");
}
fs.writeFileSync('/workspace/sanskrite/backend/internal/criticalchain/signal/fusion.go', fusionGo);
