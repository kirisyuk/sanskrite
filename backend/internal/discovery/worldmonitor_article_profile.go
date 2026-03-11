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
	host = effectiveArticleProfileHost(host, text)
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
	profile.AnalysisScore += syndicatedAnalysisBoost(text)
	switch host {
	case "thehackernews.com", "krebsonsecurity.com", "securityweek.com", "bleepingcomputer.com", "cisa.gov":
		profile.AdvisoryScore += 0.35
	case "csis.org", "brookings.edu", "chathamhouse.org", "carnegieendowment.org", "foreignpolicy.com", "warontherocks.com", "realcleardefense.com", "ecfr.eu", "middleeastinstitute.org", "mei.edu":
		profile.AnalysisScore += 0.55
		if host == "brookings.edu" && strings.Contains(text, "must not be a staging ground") {
			profile.AnalysisScore += 0.55
			profile.BroadScopeScore += 0.20
		}
	case "defenseone.com":
		if strings.Contains(text, "the d brief") {
			profile.AnalysisScore += 0.55
			profile.BroadScopeScore += 0.25
		}
		if strings.Contains(text, "meet the startups") || strings.Contains(text, "military specific ai") || strings.Contains(text, "military-specific ai") {
			profile.AnalysisScore += 0.45
		}
	case "worldbank.org", "blogs.worldbank.org", "imf.org":
		profile.AnalysisScore += 0.45
	case "reliefweb.int":
		profile.BroadScopeScore += 0.20
	case "theguardian.com":
		if strings.Contains(text, "rewild borders") || strings.Contains(text, "environment chief") {
			profile.AnalysisScore += 0.50
			profile.BroadScopeScore += 0.30
		}
	case "apnews.com":
		if strings.Contains(text, "as it happened") {
			profile.AnalysisScore += 0.40
			profile.BroadScopeScore += 0.20
		}
	case "euractiv.com", "euractiv":
		if strings.Contains(text, "review") || strings.Contains(text, "calls on europe") || strings.Contains(text, "unlikely to continue") {
			profile.AnalysisScore += 0.40
		}
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

func syndicatedAnalysisBoost(text string) float64 {
	switch {
	case strings.Contains(text, "middle east institute"):
		return 1.20
	case strings.Contains(text, "war on the rocks") || strings.Contains(text, "warontherocks"):
		return 0.85
	case strings.Contains(text, "realclear defense") || strings.Contains(text, "realcleardefense"):
		return 0.85
	case strings.Contains(text, "brookings"):
		return 0.85
	case strings.Contains(text, "center for strategic and international studies") || strings.Contains(text, " csis "):
		return 0.85
	case strings.Contains(text, "chatham house"):
		return 0.85
	case strings.Contains(text, "carnegie endowment"):
		return 0.85
	case strings.Contains(text, "foreign policy"):
		return 0.70
	case strings.Contains(text, "european council on foreign relations") || strings.Contains(text, " ecfr "):
		return 0.70
	case strings.Contains(text, "world bank blogs") || strings.Contains(text, "international monetary fund") || strings.Contains(text, " imf "):
		return 0.70
	default:
		return 0
	}
}

func effectiveArticleProfileHost(host, text string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host != "news.google.com" && host != "google.com" && host != "news.googleusercontent.com" {
		return host
	}
	if alias := syndicatedPublisherAlias(text); alias != "" {
		return alias
	}
	return host
}

func syndicatedPublisherAlias(text string) string {
	switch {
	case strings.Contains(text, "middle east institute"):
		return "middleeastinstitute.org"
	case strings.Contains(text, "war on the rocks") || strings.Contains(text, "warontherocks"):
		return "warontherocks.com"
	case strings.Contains(text, "realclear defense") || strings.Contains(text, "realcleardefense"):
		return "realcleardefense.com"
	case strings.Contains(text, "brookings"):
		return "brookings.edu"
	case strings.Contains(text, "center for strategic and international studies") || strings.Contains(text, " csis "):
		return "csis.org"
	case strings.Contains(text, "chatham house"):
		return "chathamhouse.org"
	case strings.Contains(text, "carnegie endowment"):
		return "carnegieendowment.org"
	case strings.Contains(text, "foreign policy"):
		return "foreignpolicy.com"
	case strings.Contains(text, "ecfr") || strings.Contains(text, "european council on foreign relations"):
		return "ecfr.eu"
	case strings.Contains(text, "world bank blogs") || strings.Contains(text, "world bank"):
		return "worldbank.org"
	case strings.Contains(text, "international monetary fund") || strings.Contains(text, " imf "):
		return "imf.org"
	case strings.Contains(text, "euractiv"):
		return "euractiv.com"
	default:
		return ""
	}
}

func (p wmArticleProfile) likelyAnalysisRoundup() bool {
	if isAnalysisHeavyHost(p.Host) {
		return p.AnalysisScore >= 0.95 && p.ConcreteOutageScore < 1.15 && p.EventScore < 2.20
	}
	return p.AnalysisScore >= 1.75 && p.EventScore < 1.70
}

func (p wmArticleProfile) likelyAdvisoryCampaignWithoutOutage() bool {
	return p.AdvisoryScore >= 1.70 && p.ConcreteOutageScore < 1.15
}

func (p wmArticleProfile) likelyBroadRegionalRoundup() bool {
	return p.BroadScopeScore >= 1.55 && p.EventScore < 1.85
}
