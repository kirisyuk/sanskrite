package discovery

import "strings"

type wmArticleProfile struct {
	Host                string
	NonEventScore       float64
	EventScore          float64
	AnalysisScore       float64
	AdvisoryScore       float64
	BroadScopeScore     float64
	ConcreteOutageScore float64
}

func (e *wmHeuristicEngine) ProfileArticle(text, host string) wmArticleProfile {
	text = normalizeKeywordText(text)
	host = strings.ToLower(strings.TrimSpace(host))
	if e == nil {
		return wmArticleProfile{Host: host}
	}
	profile := wmArticleProfile{
		Host:          host,
		NonEventScore: e.nonEventScore(text),
		EventScore:    e.strongEventScore(text),
	}
	if e.articleProfile != nil {
		scores := e.articleProfile.Score(text)
		profile.AnalysisScore = scores["analysis"]
		profile.AdvisoryScore = scores["advisory"]
		profile.BroadScopeScore = scores["broad_scope"]
		profile.ConcreteOutageScore = scores["concrete_outage"]
	}
	switch host {
	case "thehackernews.com", "krebsonsecurity.com", "securityweek.com", "bleepingcomputer.com", "cisa.gov":
		profile.AdvisoryScore += 0.35
	case "csis.org", "brookings.edu", "chathamhouse.org", "carnegieendowment.org", "foreignpolicy.com", "warontherocks.com", "realcleardefense.com", "ecfr.eu":
		profile.AnalysisScore += 0.35
	case "worldbank.org", "blogs.worldbank.org", "imf.org":
		profile.AnalysisScore += 0.45
	case "reliefweb.int":
		profile.BroadScopeScore += 0.20
	}
	if strings.Contains(text, " world bank blogs") || strings.Contains(text, "world bank blogs ") {
		profile.AnalysisScore += 0.35
	}
	if strings.Contains(text, " csis ") || strings.Contains(text, "center for strategic and international studies") {
		profile.AnalysisScore += 0.25
	}
	if strings.Contains(text, "reliefweb") {
		profile.BroadScopeScore += 0.16
	}
	return profile
}

func (p wmArticleProfile) likelyAnalysisRoundup() bool {
	return p.AnalysisScore >= 1.75 && p.EventScore < 1.70
}

func (p wmArticleProfile) likelyAdvisoryCampaignWithoutOutage() bool {
	return p.AdvisoryScore >= 1.70 && p.ConcreteOutageScore < 1.15
}

func (p wmArticleProfile) likelyBroadRegionalRoundup() bool {
	return p.BroadScopeScore >= 1.55 && p.EventScore < 1.85
}
