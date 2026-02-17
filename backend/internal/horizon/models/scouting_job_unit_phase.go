package models

import (
	"fmt"
	"strings"
)

// GetUnitPipelinePhase returns normalized unit pipeline phase from job state.
func (j *ScoutingJob) GetUnitPipelinePhase() UnitPipelinePhase {
	if j == nil || j.State == nil {
		return UnitPipelinePhaseP3
	}
	raw, ok := j.State[StateKeyUnitPipelinePhase]
	if !ok || raw == nil {
		return UnitPipelinePhaseP3
	}
	if s, ok := raw.(string); ok {
		return NormalizeUnitPipelinePhase(s)
	}
	return NormalizeUnitPipelinePhase(strings.TrimSpace(strings.ToUpper(fmt.Sprintf("%v", raw))))
}

// SetUnitPipelinePhase writes normalized phase into state.
func (j *ScoutingJob) SetUnitPipelinePhase(phase UnitPipelinePhase) {
	if j == nil {
		return
	}
	if j.State == nil {
		j.State = map[string]interface{}{}
	}
	j.State[StateKeyUnitPipelinePhase] = string(NormalizeUnitPipelinePhase(string(phase)))
}

// HasUnitDiveRequest returns true when a dive request payload is attached to job state.
func (j *ScoutingJob) HasUnitDiveRequest() bool {
	if j == nil || j.State == nil {
		return false
	}
	v, ok := j.State[StateKeyUnitDiveRequest]
	if !ok || v == nil {
		return false
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t) != ""
	case map[string]interface{}:
		return len(t) > 0
	default:
		return true
	}
}
