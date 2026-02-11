package models

import "time"

const (
	PolicyStatusClean       = "clean"
	PolicyStatusAutoFixed   = "auto_fixed"
	PolicyStatusPenalized   = "penalized"
	PolicyStatusQuarantined = "quarantined"
)

const (
	QualifierPolicyStatus       = "policyStatus"
	QualifierPolicyReason       = "policyReason"
	QualifierPolicyPenalty      = "policyPenalty"
	QualifierPolicyTimestamp    = "policyTimestamp"
	QualifierPolicyOriginalType = "policyOriginalType"
)

func (e *DependencyEdge) SetPolicyStatus(status, reason string, penalty float64) {
	if e == nil {
		return
	}
	if e.Qualifiers == nil {
		e.Qualifiers = make(map[string]interface{})
	}
	if status == "" {
		status = PolicyStatusClean
	}
	e.Qualifiers[QualifierPolicyStatus] = status
	if reason != "" {
		e.Qualifiers[QualifierPolicyReason] = reason
	}
	if penalty > 0 {
		e.Qualifiers[QualifierPolicyPenalty] = penalty
	}
	e.Qualifiers[QualifierPolicyTimestamp] = time.Now().UTC().Format(time.RFC3339)
}

func (e *DependencyEdge) GetPolicyStatus() string {
	if e == nil || e.Qualifiers == nil {
		return PolicyStatusClean
	}
	status, _ := e.Qualifiers[QualifierPolicyStatus].(string)
	if status == "" {
		return PolicyStatusClean
	}
	return status
}

func (e *DependencyEdge) GetPolicyReason() string {
	if e == nil || e.Qualifiers == nil {
		return ""
	}
	reason, _ := e.Qualifiers[QualifierPolicyReason].(string)
	return reason
}

func (e *DependencyEdge) SetPolicyOriginalType(originalType DependencyType) {
	if e == nil || originalType == "" {
		return
	}
	if e.Qualifiers == nil {
		e.Qualifiers = make(map[string]interface{})
	}
	e.Qualifiers[QualifierPolicyOriginalType] = string(originalType)
}

func (e *DependencyEdge) IsQuarantined() bool {
	return e.GetPolicyStatus() == PolicyStatusQuarantined
}

func (f *HorizonFragment) SetPolicyStatus(status, reason string, penalty float64) {
	if f == nil {
		return
	}
	if f.Qualifiers == nil {
		f.Qualifiers = make(map[string]interface{})
	}
	if status == "" {
		status = PolicyStatusClean
	}
	f.Qualifiers[QualifierPolicyStatus] = status
	if reason != "" {
		f.Qualifiers[QualifierPolicyReason] = reason
	}
	if penalty > 0 {
		f.Qualifiers[QualifierPolicyPenalty] = penalty
	}
	f.Qualifiers[QualifierPolicyTimestamp] = time.Now().UTC().Format(time.RFC3339)
}

func (f *HorizonFragment) GetPolicyStatus() string {
	if f == nil || f.Qualifiers == nil {
		return PolicyStatusClean
	}
	status, _ := f.Qualifiers[QualifierPolicyStatus].(string)
	if status == "" {
		return PolicyStatusClean
	}
	return status
}

func (f *HorizonFragment) GetPolicyReason() string {
	if f == nil || f.Qualifiers == nil {
		return ""
	}
	reason, _ := f.Qualifiers[QualifierPolicyReason].(string)
	return reason
}
