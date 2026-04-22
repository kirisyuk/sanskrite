package agents

import "strings"

var livePromptTaxonomyReplacer = strings.NewReplacer(
	`/program/`, `/project/`,
	`/event/`, `/evidence/`,
	`ORGANIZATION`, `org`,
	`FACILITY`, `site`,
	`GEO_REGION`, `geo`,
	`PROGRAM`, `project`,
	`DECISION_POINT`, `decision`,
	`EVENT`, `evidence`,
	`PERSON`, `person`,
	`PRODUCT`, `product`,
	`MARKET`, `market`,
	`COMMUNITY`, `community`,
	`MECHANISM`, `mechanism`,
	`MEDIA_CHANNEL`, `media_channel`,
	`GOV`, `gov`,
	`SUPPLIES_TO`, `supplies`,
	`PRODUCES`, `produces`,
	`OWNS`, `beneficially_owns`,
	`CONTROLS`, `controls_via`,
	`INVESTS_IN`, `invested_in`,
	`BOARD_MEMBER_OF`, `holds_board_seat`,
	`PARTNERS_WITH`, `joint_venture_with`,
	`TRADED_IN`, `listed_on`,
	`ADMINISTERS`, `operates`,
	`MANAGES`, `managed_by`,
	`ASSURES`, `assures`,
	`PARTICIPATES_IN`, `participates_in`,
	`REGULATES`, `regulates`,
	`GATEKEEPS_DECISION`, `gatekeeps`,
	`USES_MECHANISM`, `wields`,
)

func normalizeLivePromptTaxonomy(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return prompt
	}
	return livePromptTaxonomyReplacer.Replace(prompt)
}
