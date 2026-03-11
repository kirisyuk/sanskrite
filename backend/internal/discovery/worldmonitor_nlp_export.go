package discovery

import discoverynlp "github.com/igorkirisyuk/sanskrite/internal/discovery/nlp"

// BuildHorizonMonitorWeakLabel exposes the deterministic article assessment used by the
// Horizon Monitor pipeline so dataset builders and offline evaluation can stay aligned
// with the current core rules.
func BuildHorizonMonitorWeakLabel(doc discoverynlp.DocumentInput) *discoverynlp.ArticleAssessment {
	cls := classifyHorizonMonitorNewsKeyword(doc.Title + " " + doc.Summary)
	return (&HorizonMonitorPipeline{}).ruleArticleAssessment(doc, cls)
}
