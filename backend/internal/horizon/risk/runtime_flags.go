package risk

import (
	"os"
	"strings"
)

const (
	envRiskIdentifyUseFragmentID = "HORIZON_RISK_IDENTIFY_USE_FRAGMENT_ID"
	envRiskNoSignalsScoutRefresh = "HORIZON_RISK_NO_SIGNALS_SCOUT_REFRESH"
	envRiskScoutRefreshCooldownM = "HORIZON_RISK_SCOUT_REFRESH_COOLDOWN_MINUTES"

	riskStateKeyFragmentID          = "fragmentId"
	riskStateKeyTargetFragmentID    = "targetFragmentId"
	riskStateKeyTargetCanonicalPath = "targetCanonicalPath"
	riskStateKeySignalIDs           = "signalIds"
	riskStateKeySignalCount         = "signalCount"
	riskStateKeyIdentifiedAtUTC     = "identifiedAtUtc"
	riskStateKeyIdentifyMode        = "identifyResolutionMode"
	riskStateKeyScoutRefreshMarker  = "riskTriggeredScoutRefresh"
	riskStateKeyScoutRefreshAtUTC   = "riskScoutRefreshAtUtc"
	riskStateKeyScoutRefreshDepth   = "riskScoutRefreshDepth"
	riskStateKeyScoutRefreshReason  = "riskScoutRefreshReason"
	riskStateKeyLastErrorPhase      = "lastErrorPhase"
	riskStateKeyLastErrorReasonCode = "lastErrorReasonCode"
	riskStateKeyLastErrorAtUTC      = "lastErrorAtUtc"
	riskStateKeyLastErrorClass      = "lastErrorClass"
	riskStateKeyBestEffortCount     = "groundingBestEffortCount"
)

func riskEnvEnabled(name string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func riskIdentifyFragmentIDEnabled() bool {
	return riskEnvEnabled(envRiskIdentifyUseFragmentID)
}

func riskNoSignalsScoutRefreshEnabled() bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(envRiskNoSignalsScoutRefresh)))
	if raw == "" {
		return true
	}
	return raw == "1" || raw == "true" || raw == "yes" || raw == "on"
}
