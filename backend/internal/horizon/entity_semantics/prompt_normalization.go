package entity_semantics

import "strings"

var promptVocabularyReplacer = strings.NewReplacer(
	"ENTITY TYPE: ORGANIZATION", "ENTITY TYPE: ORG",
	"ENTITY TYPE: FACILITY", "ENTITY TYPE: SITE",
	"ENTITY TYPE: GEO_REGION", "ENTITY TYPE: GEO",
	"ENTITY TYPE: PROGRAM", "ENTITY TYPE: PROJECT",
	"ENTITY TYPE: DECISION_POINT", "ENTITY TYPE: DECISION",
	"ENTITY TYPE: COMMUNITY_DYNASTY", "ENTITY TYPE: COMMUNITY dynasty",
	"ENTITY TYPE: COMMUNITY_TRIBE", "ENTITY TYPE: COMMUNITY tribe",
	"FACILITY", "SITE",
	"GEO_REGION", "GEO",
	"PROGRAM", "PROJECT",
	"DECISION_POINT", "DECISION",
	"/program/", "/project/",
	"/event/", "/evidence/",
)

func normalizePromptVocabulary(block string) string {
	if strings.TrimSpace(block) == "" {
		return ""
	}
	return promptVocabularyReplacer.Replace(block)
}
