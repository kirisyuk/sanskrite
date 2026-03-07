package discovery

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type ahoNode struct {
	next map[byte]int
	fail int
	out  []int
}

type ahoMatcher struct {
	nodes    []ahoNode
	patterns []string
}

func newAhoMatcher(patterns []string) *ahoMatcher {
	normalized := make([]string, 0, len(patterns))
	seen := make(map[string]struct{}, len(patterns))
	for _, p := range patterns {
		p = normalizeKeywordText(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		normalized = append(normalized, p)
	}

	m := &ahoMatcher{
		nodes: []ahoNode{{next: map[byte]int{}}},
	}
	if len(normalized) == 0 {
		return m
	}
	m.patterns = normalized

	for idx, pattern := range normalized {
		state := 0
		for i := 0; i < len(pattern); i++ {
			ch := pattern[i]
			next, ok := m.nodes[state].next[ch]
			if !ok {
				next = len(m.nodes)
				m.nodes[state].next[ch] = next
				m.nodes = append(m.nodes, ahoNode{next: map[byte]int{}})
			}
			state = next
		}
		m.nodes[state].out = append(m.nodes[state].out, idx)
	}

	queue := make([]int, 0, len(m.nodes))
	for _, s := range m.nodes[0].next {
		queue = append(queue, s)
		m.nodes[s].fail = 0
	}

	for q := 0; q < len(queue); q++ {
		state := queue[q]
		for ch, next := range m.nodes[state].next {
			queue = append(queue, next)
			fail := m.nodes[state].fail
			for fail != 0 {
				if _, ok := m.nodes[fail].next[ch]; ok {
					break
				}
				fail = m.nodes[fail].fail
			}
			if fnext, ok := m.nodes[fail].next[ch]; ok {
				m.nodes[next].fail = fnext
			} else {
				m.nodes[next].fail = 0
			}
			if out := m.nodes[m.nodes[next].fail].out; len(out) > 0 {
				m.nodes[next].out = append(m.nodes[next].out, out...)
			}
		}
	}

	return m
}

func (m *ahoMatcher) MatchCounts(text string) map[string]int {
	text = normalizeKeywordText(text)
	if m == nil || len(m.patterns) == 0 || text == "" {
		return nil
	}
	buf := []byte(text)
	out := map[string]int{}
	state := 0

	for i := 0; i < len(buf); i++ {
		ch := buf[i]
		for state != 0 {
			if _, ok := m.nodes[state].next[ch]; ok {
				break
			}
			state = m.nodes[state].fail
		}
		if next, ok := m.nodes[state].next[ch]; ok {
			state = next
		} else {
			state = 0
		}
		if len(m.nodes[state].out) == 0 {
			continue
		}
		for _, pid := range m.nodes[state].out {
			p := m.patterns[pid]
			start := i - len(p) + 1
			if start < 0 {
				continue
			}
			if !hasTokenBoundaries(buf, start, i) {
				continue
			}
			out[p]++
		}
	}
	return out
}

func hasTokenBoundaries(buf []byte, start, end int) bool {
	if start < 0 || end >= len(buf) || start > end {
		return false
	}
	if start > 0 && isTokenByte(buf[start-1]) {
		return false
	}
	if end+1 < len(buf) && isTokenByte(buf[end+1]) {
		return false
	}
	return true
}

func isTokenByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

type wmWeightedKeyword struct {
	Pattern string
	Weight  float64
}

type wmWeightedLabel struct {
	Label    string
	Keywords []wmWeightedKeyword
}

type wmWeightedMatcher struct {
	matcher *ahoMatcher
	labels  []wmWeightedLabel
}

func newWMWeightedMatcher(labels []wmWeightedLabel) *wmWeightedMatcher {
	all := make([]string, 0, 128)
	norm := make([]wmWeightedLabel, 0, len(labels))

	for _, label := range labels {
		lbl := strings.TrimSpace(label.Label)
		if lbl == "" {
			continue
		}
		kwMap := map[string]float64{}
		for _, kw := range label.Keywords {
			p := normalizeKeywordText(kw.Pattern)
			if p == "" {
				continue
			}
			w := kw.Weight
			if w <= 0 {
				w = 1.0
			}
			kwMap[p] += w
		}
		if len(kwMap) == 0 {
			continue
		}
		compiled := wmWeightedLabel{Label: lbl, Keywords: make([]wmWeightedKeyword, 0, len(kwMap))}
		for p, w := range kwMap {
			compiled.Keywords = append(compiled.Keywords, wmWeightedKeyword{Pattern: p, Weight: w})
			all = append(all, p)
		}
		sort.Slice(compiled.Keywords, func(i, j int) bool {
			return compiled.Keywords[i].Pattern < compiled.Keywords[j].Pattern
		})
		norm = append(norm, compiled)
	}

	return &wmWeightedMatcher{
		matcher: newAhoMatcher(all),
		labels:  norm,
	}
}

func (m *wmWeightedMatcher) Score(text string) map[string]float64 {
	if m == nil || m.matcher == nil || len(m.labels) == 0 {
		return nil
	}
	counts := m.matcher.MatchCounts(text)
	if len(counts) == 0 {
		return nil
	}
	out := map[string]float64{}
	for _, label := range m.labels {
		score := 0.0
		for _, kw := range label.Keywords {
			c := counts[kw.Pattern]
			if c <= 0 {
				continue
			}
			mult := 1.0 + 0.22*float64(c-1)
			if mult > 1.75 {
				mult = 1.75
			}
			score += kw.Weight * mult
		}
		if score > 0 {
			out[label.Label] = score
		}
	}
	return out
}

type wmLevelCategory struct {
	Level    string
	Category string
	BaseConf float64
	Rank     int
}

type wmZoneClass struct {
	ConditionType string
	ZoneType      string
	ZoneSubType   string
	Rank          int
}

type wmHeuristicEngine struct {
	nonEvent       *wmWeightedMatcher
	strongEvent    *wmWeightedMatcher
	classifier     *wmWeightedMatcher
	zoneClassifier *wmWeightedMatcher
	classByLabel   map[string]wmLevelCategory
	zoneByLabel    map[string]wmZoneClass
}

var (
	wmHeuristicsOnce  sync.Once
	wmHeuristicsInst  *wmHeuristicEngine
	reSeismicMagShort = regexp.MustCompile(`\bm(?:w|ww|l|b)?\s*([0-9]{1,2})(?:\s+([0-9]{1,2}))?\+?\b`)
	reSeismicMagLong  = regexp.MustCompile(`\bmagnitude\s+([0-9]{1,2})(?:\s+([0-9]{1,2}))?\+?\b`)
)

func wmHeuristics() *wmHeuristicEngine {
	wmHeuristicsOnce.Do(func() {
		wmHeuristicsInst = buildWMHeuristicEngine()
	})
	return wmHeuristicsInst
}

func buildWMHeuristicEngine() *wmHeuristicEngine {
	engine := &wmHeuristicEngine{
		classByLabel: map[string]wmLevelCategory{},
		zoneByLabel:  map[string]wmZoneClass{},
	}

	engine.nonEvent = newWMWeightedMatcher([]wmWeightedLabel{
		{
			Label: "noise",
			Keywords: []wmWeightedKeyword{
				{"departments submenu", 2.2}, {"regions submenu", 2.2}, {"topics submenu", 2.2}, {"primary menu", 1.8},
				{"open filter submenu", 2.4}, {"all analysis", 1.6}, {"bookstore", 1.8}, {"frequently asked questions", 1.8},
				{"program manager", 2.0}, {"project manager", 2.0}, {"business partner", 1.7}, {"vacancy", 1.8}, {"hiring", 1.8},
				{"career", 1.3}, {"consultant", 1.6}, {"fellowship", 1.4}, {"conference", 1.2}, {"workday support", 2.0},
				{"legal counsel", 1.6}, {"staff report", 1.3}, {"article iv mission", 1.4}, {"job opening", 1.8},
				{"job vacancy", 1.8}, {"press office", 1.4}, {"opinion", 1.2}, {"newsletter", 1.2},
			},
		},
	})
	engine.strongEvent = newWMWeightedMatcher([]wmWeightedLabel{
		{
			Label: "event",
			Keywords: []wmWeightedKeyword{
				{"attack", 1.1}, {"attacks", 1.1}, {"airstrike", 2.0}, {"missile strike", 2.0}, {"drone strike", 1.8},
				{"shelling", 1.8}, {"bombed", 1.8}, {"torpedo", 1.8}, {"sunk", 1.7}, {"warship", 1.6}, {"destroyer", 1.5},
				{"frigate", 1.5}, {"intercepts", 1.4}, {"counterstrike", 1.8}, {"invasion", 2.1}, {"troops deployed", 1.8},
				{"strike", 1.3}, {"strikes", 1.4}, {"launched", 1.3}, {"launches", 1.3}, {"killed", 1.5},
				{"casualties", 1.5}, {"explosion", 1.4}, {"blast", 1.4}, {"intercepted", 1.4}, {"downed", 1.5},
				{"clash", 1.4}, {"riot", 1.2}, {"outage", 1.2}, {"blackout", 1.5}, {"earthquake", 1.7}, {"aftershock", 1.6},
				{"flood", 1.5}, {"wildfire", 1.5}, {"hurricane", 1.6}, {"typhoon", 1.6}, {"cyclone", 1.6}, {"landslide", 1.4},
				{"tsunami", 1.7}, {"seismic", 1.6}, {"epicenter", 1.4}, {"quake", 1.5}, {"quake swarm", 1.8}, {"magnitude", 1.2},
				{"volcano", 1.5}, {"volcanic eruption", 1.8}, {"ash plume", 1.6}, {"lava flow", 1.6},
				{"evacuation", 1.3}, {"evacuate", 1.3}, {"evacuated", 1.3}, {"displaced", 1.4}, {"refugee", 1.2},
				{"famine", 1.5}, {"cholera", 1.2}, {"breach", 1.3}, {"hacked", 1.5}, {"ransomware", 1.6},
				{"malware", 1.5}, {"exploit kit", 1.4}, {"zero day", 1.5}, {"vulnerability", 1.3}, {"exploited", 1.3},
				{"terror attack", 1.8}, {"terrorist", 1.7}, {"hostage", 1.7}, {"suicide bombing", 1.9},
				{"pandemic", 1.7}, {"epidemic", 1.6}, {"outbreak", 1.5}, {"ebola", 1.7}, {"mpox", 1.5}, {"h5n1", 1.5},
				{"oil spill", 1.6}, {"chemical leak", 1.6}, {"radiation leak", 1.7}, {"toxic spill", 1.7},
			},
		},
	})

	classLabels := []struct {
		label string
		meta  wmLevelCategory
		words []wmWeightedKeyword
	}{
		{
			label: "critical|military",
			meta:  wmLevelCategory{Level: "critical", Category: "military", BaseConf: 0.88, Rank: 4},
			words: []wmWeightedKeyword{
				{"nuclear strike", 3.8}, {"nuclear attack", 3.7}, {"ballistic missile strike", 3.6},
				{"chemical attack", 3.4}, {"full scale invasion", 3.5},
			},
		},
		{
			label: "critical|conflict",
			meta:  wmLevelCategory{Level: "critical", Category: "conflict", BaseConf: 0.87, Rank: 4},
			words: []wmWeightedKeyword{
				{"invasion", 3.0}, {"genocide", 3.7}, {"martial law", 3.4},
			},
		},
		{
			label: "critical|infrastructure",
			meta:  wmLevelCategory{Level: "critical", Category: "infrastructure", BaseConf: 0.87, Rank: 4},
			words: []wmWeightedKeyword{
				{"strait of hormuz closed", 3.6},
			},
		},
		{
			label: "critical|disaster",
			meta:  wmLevelCategory{Level: "critical", Category: "disaster", BaseConf: 0.86, Rank: 4},
			words: []wmWeightedKeyword{
				{"major earthquake", 3.5}, {"catastrophic flood", 3.4},
			},
		},
		{
			label: "high|military",
			meta:  wmLevelCategory{Level: "high", Category: "military", BaseConf: 0.79, Rank: 3},
			words: []wmWeightedKeyword{
				{"missile strike", 2.7}, {"missile attack", 2.6}, {"troops deployed", 2.5}, {"warship", 2.2},
				{"frigate", 2.0}, {"destroyer", 2.0}, {"submarine", 2.1}, {"torpedo", 2.2}, {"sunk", 2.1},
				{"intercepts", 2.0}, {"fighter jet", 2.0}, {"interceptor", 1.8}, {"air defense", 1.7},
				{"launched missiles", 2.4}, {"launches missiles", 2.3}, {"navy", 1.8}, {"air force", 1.8},
				{"shoots down", 2.2}, {"downed", 2.1}, {"torpedoes", 2.2},
			},
		},
		{
			label: "high|conflict",
			meta:  wmLevelCategory{Level: "high", Category: "conflict", BaseConf: 0.79, Rank: 3},
			words: []wmWeightedKeyword{
				{"airstrike", 2.8}, {"drone strike", 2.7}, {"ground invasion", 2.8}, {"counterstrike", 2.6},
				{"war", 1.3}, {"conflict", 1.2}, {"strike", 1.9}, {"strikes", 2.0}, {"killed", 2.0},
				{"casualties", 1.8}, {"explosion", 1.7}, {"clashes", 1.7}, {"hostilities", 1.7},
			},
		},
		{
			label: "high|cyber",
			meta:  wmLevelCategory{Level: "high", Category: "cyber", BaseConf: 0.78, Rank: 3},
			words: []wmWeightedKeyword{
				{"cyber attack", 2.8}, {"cyber breach", 2.6}, {"data breach", 2.4}, {"hacked", 2.2},
				{"malware", 2.2}, {"ransomware", 2.4}, {"botnet", 2.1}, {"phishing", 1.8},
				{"vulnerability", 1.9}, {"zero day", 2.2}, {"exploited", 2.0}, {"backdoor", 2.0},
				{"kev", 1.7}, {"advisory", 1.6}, {"cve", 1.7},
			},
		},
		{
			label: "high|infrastructure",
			meta:  wmLevelCategory{Level: "high", Category: "infrastructure", BaseConf: 0.78, Rank: 3},
			words: []wmWeightedKeyword{
				{"blackout", 2.6}, {"internet outage", 2.6}, {"cable cut", 2.4}, {"pipeline disruption", 2.4},
				{"port closure", 2.2}, {"strait of hormuz", 2.5}, {"suez canal", 2.5}, {"canal traffic", 2.1},
				{"shipping lane", 2.0}, {"service outage", 2.2},
			},
		},
		{
			label: "high|disaster",
			meta:  wmLevelCategory{Level: "high", Category: "disaster", BaseConf: 0.78, Rank: 3},
			words: []wmWeightedKeyword{
				{"earthquake", 2.5}, {"hurricane", 2.4}, {"wildfire", 2.3}, {"flood", 2.3},
				{"famine", 2.2}, {"drought", 2.0}, {"aftershock", 2.2}, {"tsunami", 2.3},
				{"landslide", 2.0}, {"eruption", 2.2}, {"evacuation", 1.8}, {"seismic", 2.1},
				{"epicenter", 1.9}, {"quake swarm", 2.2}, {"richter", 1.8}, {"volcanic eruption", 2.3},
			},
		},
		{
			label: "high|terrorism",
			meta:  wmLevelCategory{Level: "high", Category: "terrorism", BaseConf: 0.79, Rank: 3},
			words: []wmWeightedKeyword{
				{"terror attack", 2.7}, {"terrorist", 2.4}, {"hostage", 2.2}, {"suicide bombing", 2.7},
				{"bomb blast", 2.2}, {"isis", 2.0}, {"al qaeda", 2.0},
			},
		},
		{
			label: "high|health",
			meta:  wmLevelCategory{Level: "high", Category: "health", BaseConf: 0.77, Rank: 3},
			words: []wmWeightedKeyword{
				{"pandemic", 2.6}, {"epidemic", 2.3}, {"outbreak", 2.1}, {"ebola", 2.5},
				{"cholera", 2.1}, {"mpox", 2.1}, {"avian flu", 2.2}, {"h5n1", 2.2},
			},
		},
		{
			label: "high|environmental",
			meta:  wmLevelCategory{Level: "high", Category: "environmental", BaseConf: 0.76, Rank: 3},
			words: []wmWeightedKeyword{
				{"oil spill", 2.4}, {"toxic spill", 2.4}, {"chemical leak", 2.3}, {"radiation leak", 2.5},
				{"contamination", 2.1}, {"hazmat", 2.0},
			},
		},
		{
			label: "medium|protest",
			meta:  wmLevelCategory{Level: "medium", Category: "protest", BaseConf: 0.69, Rank: 2},
			words: []wmWeightedKeyword{
				{"protest", 1.9}, {"riot", 2.0}, {"unrest", 1.9},
			},
		},
		{
			label: "medium|diplomatic",
			meta:  wmLevelCategory{Level: "medium", Category: "diplomatic", BaseConf: 0.67, Rank: 2},
			words: []wmWeightedKeyword{
				{"ceasefire", 1.9}, {"summit", 1.3}, {"treaty", 1.4}, {"agreement", 1.2}, {"dialogue", 1.1}, {"negotiation", 1.2},
			},
		},
		{
			label: "medium|economic",
			meta:  wmLevelCategory{Level: "medium", Category: "economic", BaseConf: 0.66, Rank: 2},
			words: []wmWeightedKeyword{
				{"sanctions", 1.9}, {"embargo", 1.8}, {"tariff", 1.7}, {"trade war", 1.8}, {"food price", 1.7},
				{"hunger", 1.7}, {"energy crisis", 1.8}, {"oil price", 1.6}, {"gas price", 1.5}, {"inflation", 1.2}, {"gdp", 1.0},
			},
		},
		{
			label: "medium|infrastructure",
			meta:  wmLevelCategory{Level: "medium", Category: "infrastructure", BaseConf: 0.67, Rank: 2},
			words: []wmWeightedKeyword{
				{"supply chain", 1.8}, {"maritime security", 1.7}, {"logistics corridor", 1.7},
				{"shipping disruption", 1.8}, {"port congestion", 1.6}, {"canal transit", 1.6},
				{"air crash", 1.9}, {"plane crash", 1.9}, {"train derailment", 1.8}, {"bridge collapse", 1.9},
			},
		},
		{
			label: "medium|military",
			meta:  wmLevelCategory{Level: "medium", Category: "military", BaseConf: 0.68, Rank: 2},
			words: []wmWeightedKeyword{
				{"military exercise", 1.9}, {"deterrence", 1.6}, {"arms deal", 1.5}, {"nuclear doctrine", 1.7},
				{"deployment", 1.6}, {"warships", 1.7}, {"missile defense", 1.7},
			},
		},
		{
			label: "medium|health",
			meta:  wmLevelCategory{Level: "medium", Category: "health", BaseConf: 0.66, Rank: 2},
			words: []wmWeightedKeyword{
				{"infection", 1.6}, {"infected", 1.5}, {"public health", 1.5}, {"hospitalized", 1.5},
				{"health emergency", 1.7},
			},
		},
		{
			label: "medium|environmental",
			meta:  wmLevelCategory{Level: "medium", Category: "environmental", BaseConf: 0.66, Rank: 2},
			words: []wmWeightedKeyword{
				{"heatwave", 1.9}, {"smog", 1.5}, {"air quality alert", 1.7}, {"water contamination", 1.8},
				{"drought", 1.7},
			},
		},
		{
			label: "medium|crime",
			meta:  wmLevelCategory{Level: "medium", Category: "crime", BaseConf: 0.65, Rank: 2},
			words: []wmWeightedKeyword{
				{"assassination", 2.0}, {"kidnapping", 1.9}, {"piracy", 1.8}, {"armed robbery", 1.8},
			},
		},
		{
			label: "low|diplomatic",
			meta:  wmLevelCategory{Level: "low", Category: "diplomatic", BaseConf: 0.56, Rank: 1},
			words: []wmWeightedKeyword{
				{"election", 1.0}, {"policy", 0.95}, {"strategy", 0.9},
			},
		},
		{
			label: "low|economic",
			meta:  wmLevelCategory{Level: "low", Category: "economic", BaseConf: 0.55, Rank: 1},
			words: []wmWeightedKeyword{
				{"regulation", 0.95},
			},
		},
	}
	classRules := make([]wmWeightedLabel, 0, len(classLabels))
	for _, item := range classLabels {
		engine.classByLabel[item.label] = item.meta
		classRules = append(classRules, wmWeightedLabel{Label: item.label, Keywords: item.words})
	}
	engine.classifier = newWMWeightedMatcher(classRules)

	zoneLabels := []struct {
		label string
		meta  wmZoneClass
		words []wmWeightedKeyword
	}{
		{
			label: "earthquake_affected|natural_disaster|earthquake",
			meta:  wmZoneClass{ConditionType: "earthquake_affected", ZoneType: "natural_disaster", ZoneSubType: "earthquake", Rank: 6},
			words: []wmWeightedKeyword{{"earthquake", 2.6}, {"aftershock", 2.2}, {"seismic", 2.1}},
		},
		{
			label: "flood_zone|natural_disaster|flooding",
			meta:  wmZoneClass{ConditionType: "flood_zone", ZoneType: "natural_disaster", ZoneSubType: "flooding", Rank: 6},
			words: []wmWeightedKeyword{{"flood", 2.5}, {"flash flood", 2.5}, {"inundation", 2.2}, {"river overflow", 2.2}},
		},
		{
			label: "wildfire_zone|natural_disaster|wildfire",
			meta:  wmZoneClass{ConditionType: "wildfire_zone", ZoneType: "natural_disaster", ZoneSubType: "wildfire", Rank: 5},
			words: []wmWeightedKeyword{{"wildfire", 2.4}, {"forest fire", 2.2}, {"brush fire", 2.1}},
		},
		{
			label: "natural_disaster|natural_disaster|storm_system",
			meta:  wmZoneClass{ConditionType: "natural_disaster", ZoneType: "natural_disaster", ZoneSubType: "storm_system", Rank: 4},
			words: []wmWeightedKeyword{{"hurricane", 2.2}, {"typhoon", 2.2}, {"cyclone", 2.2}, {"storm surge", 2.1}},
		},
		{
			label: "tsunami_affected|natural_disaster|tsunami",
			meta:  wmZoneClass{ConditionType: "tsunami_affected", ZoneType: "natural_disaster", ZoneSubType: "tsunami", Rank: 6},
			words: []wmWeightedKeyword{{"tsunami", 2.7}, {"seismic sea wave", 2.2}},
		},
		{
			label: "volcanic_eruption|natural_disaster|volcanic_eruption",
			meta:  wmZoneClass{ConditionType: "volcanic_eruption", ZoneType: "natural_disaster", ZoneSubType: "volcanic_eruption", Rank: 6},
			words: []wmWeightedKeyword{{"volcano", 2.4}, {"volcanic eruption", 2.6}, {"ash plume", 2.2}, {"lava flow", 2.2}},
		},
		{
			label: "humanitarian_crisis|humanitarian_crisis|mass_displacement",
			meta:  wmZoneClass{ConditionType: "humanitarian_crisis", ZoneType: "humanitarian_crisis", ZoneSubType: "mass_displacement", Rank: 4},
			words: []wmWeightedKeyword{{"refugee", 2.0}, {"displaced", 2.1}, {"famine", 2.2}, {"cholera", 1.8}, {"humanitarian corridor", 2.0}},
		},
		{
			label: "disease_outbreak|humanitarian_crisis|epidemic",
			meta:  wmZoneClass{ConditionType: "disease_outbreak", ZoneType: "humanitarian_crisis", ZoneSubType: "epidemic", Rank: 5},
			words: []wmWeightedKeyword{{"outbreak", 2.3}, {"epidemic", 2.4}, {"pandemic", 2.5}, {"ebola", 2.5}, {"mpox", 2.1}, {"h5n1", 2.2}},
		},
		{
			label: "environmental_hazard|natural_disaster|toxic_pollution",
			meta:  wmZoneClass{ConditionType: "environmental_hazard", ZoneType: "natural_disaster", ZoneSubType: "toxic_pollution", Rank: 5},
			words: []wmWeightedKeyword{{"oil spill", 2.4}, {"toxic spill", 2.5}, {"chemical leak", 2.3}, {"radiation leak", 2.6}, {"contamination", 2.0}},
		},
		{
			label: "political_tension|political_tension|civil_unrest",
			meta:  wmZoneClass{ConditionType: "political_tension", ZoneType: "political_tension", ZoneSubType: "civil_unrest", Rank: 3},
			words: []wmWeightedKeyword{{"riot", 1.9}, {"protest", 1.8}, {"unrest", 1.8}, {"clash with police", 1.7}, {"state of emergency", 1.8}},
		},
		{
			label: "cyber_disruption|infrastructure_disruption|cyber_outage",
			meta:  wmZoneClass{ConditionType: "cyber_disruption", ZoneType: "infrastructure_disruption", ZoneSubType: "cyber_outage", Rank: 5},
			words: []wmWeightedKeyword{
				{"cyber attack", 2.3}, {"ransomware", 2.2}, {"ddos", 2.1}, {"internet outage", 2.0},
				{"cable cut", 1.9}, {"cyber breach", 2.1}, {"data breach", 1.9}, {"hacker", 1.8}, {"hacked", 1.9},
				{"malware", 1.9}, {"exploit campaign", 1.8}, {"zero day", 2.0}, {"vulnerability", 1.7},
				{"backdoor", 1.8}, {"exploited", 1.7},
			},
		},
		{
			label: "infrastructure_disruption|infrastructure_disruption|critical_infra",
			meta:  wmZoneClass{ConditionType: "infrastructure_disruption", ZoneType: "infrastructure_disruption", ZoneSubType: "critical_infra", Rank: 4},
			words: []wmWeightedKeyword{
				{"pipeline sabotage", 2.2}, {"power blackout", 2.2}, {"grid outage", 2.1}, {"port closure", 2.0},
				{"shipping lane", 1.8}, {"strait of hormuz", 2.3}, {"suez canal", 2.2},
			},
		},
		{
			label: "military_conflict|military_conflict|kinetic_operations",
			meta:  wmZoneClass{ConditionType: "military_conflict", ZoneType: "military_conflict", ZoneSubType: "kinetic_operations", Rank: 7},
			words: []wmWeightedKeyword{
				{"airstrike", 2.5}, {"missile", 2.3}, {"drone strike", 2.3}, {"shelling", 2.2}, {"ground offensive", 2.3},
				{"troops", 2.0}, {"warship", 2.1}, {"torpedo", 2.2}, {"frigate", 2.0}, {"destroyer", 2.0},
				{"counterstrike", 2.2}, {"bombed", 2.1}, {"sunk", 2.0}, {"strike", 1.8}, {"strikes", 1.9},
				{"killed", 1.8}, {"casualties", 1.7}, {"war in", 1.6}, {"war with", 1.6}, {"hostilities", 1.7},
			},
		},
	}
	zoneRules := make([]wmWeightedLabel, 0, len(zoneLabels))
	for _, item := range zoneLabels {
		engine.zoneByLabel[item.label] = item.meta
		zoneRules = append(zoneRules, wmWeightedLabel{Label: item.label, Keywords: item.words})
	}
	engine.zoneClassifier = newWMWeightedMatcher(zoneRules)

	return engine
}

func (e *wmHeuristicEngine) nonEventScore(text string) float64 {
	if e == nil || e.nonEvent == nil {
		return 0
	}
	return e.nonEvent.Score(text)["noise"]
}

func (e *wmHeuristicEngine) strongEventScore(text string) float64 {
	if e == nil || e.strongEvent == nil {
		return 0
	}
	score := e.strongEvent.Score(text)["event"]
	if magScore, _, ok := e.seismicMagnitudeSignal(text); ok {
		if score < magScore {
			score = magScore
		} else {
			score += 0.25 * magScore
		}
	}
	return score
}

func (e *wmHeuristicEngine) IsLikelyNonEvent(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return true
	}
	noise := e.nonEventScore(text)
	if noise <= 0 {
		return false
	}
	return noise >= 1.8 && e.strongEventScore(text) < 1.05
}

func (e *wmHeuristicEngine) HasStrongEvent(text string) bool {
	text = normalizeKeywordText(text)
	if text == "" {
		return false
	}
	return e.strongEventScore(text) >= 1.15
}

func (e *wmHeuristicEngine) Classify(text string) horizonMonitorNewsClassification {
	text = normalizeKeywordText(text)
	if text == "" {
		return horizonMonitorNewsClassification{Level: "info", Category: "general", Confidence: 0.35, Source: "keyword_ac"}
	}
	if e.IsLikelyNonEvent(text) {
		return horizonMonitorNewsClassification{Level: "info", Category: "general", Confidence: 0.25, Source: "keyword_ac"}
	}

	scores := e.classifier.Score(text)
	if len(scores) == 0 {
		if magCls, ok := e.classifySeismicByMagnitude(text); ok {
			return magCls
		}
		return horizonMonitorNewsClassification{Level: "info", Category: "general", Confidence: 0.35, Source: "keyword_ac"}
	}

	bestLabel := ""
	bestScore := 0.0
	bestRank := -1
	labels := make([]string, 0, len(scores))
	for label := range scores {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		score := scores[label]
		meta, ok := e.classByLabel[label]
		rank := 0
		if ok {
			rank = meta.Rank
		}
		if score > bestScore || (score == bestScore && rank > bestRank) {
			bestScore = score
			bestLabel = label
			bestRank = rank
		}
	}
	meta, ok := e.classByLabel[bestLabel]
	if !ok {
		return horizonMonitorNewsClassification{Level: "info", Category: "general", Confidence: 0.35, Source: "keyword_ac"}
	}

	conf := meta.BaseConf + bestScore*0.025
	if conf > 0.96 {
		conf = 0.96
	}

	if magCls, ok := e.classifySeismicByMagnitude(text); ok {
		bestRank := levelRank(meta.Level)
		magRank := levelRank(magCls.Level)
		if magRank > bestRank && meta.Category != "military" && meta.Category != "conflict" && meta.Category != "cyber" {
			return magCls
		}
	}
	return horizonMonitorNewsClassification{
		Level:      meta.Level,
		Category:   meta.Category,
		Confidence: conf,
		Source:     "keyword_ac",
	}
}

func (e *wmHeuristicEngine) InferZoneCondition(text string) (conditionType, zoneType, zoneSubType string, score float64) {
	text = normalizeKeywordText(text)
	if text == "" || e == nil || e.zoneClassifier == nil {
		return "", "", "", 0
	}
	if e.IsLikelyNonEvent(text) {
		return "", "", "", 0
	}

	scores := e.zoneClassifier.Score(text)
	if len(scores) == 0 {
		if cond, ztyp, sub, magScore, ok := e.inferSeismicZoneByMagnitude(text); ok {
			return cond, ztyp, sub, magScore
		}
		return "", "", "", 0
	}

	bestLabel := ""
	bestScore := 0.0
	bestRank := -1
	labels := make([]string, 0, len(scores))
	for label := range scores {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		s := scores[label]
		meta := e.zoneByLabel[label]
		if s > bestScore || (s == bestScore && meta.Rank > bestRank) {
			bestScore = s
			bestLabel = label
			bestRank = meta.Rank
		}
	}
	if bestScore < 1.15 {
		if cond, ztyp, sub, magScore, ok := e.inferSeismicZoneByMagnitude(text); ok {
			return cond, ztyp, sub, magScore
		}
		return "", "", "", bestScore
	}
	meta, ok := e.zoneByLabel[bestLabel]
	if !ok {
		return "", "", "", bestScore
	}
	return meta.ConditionType, meta.ZoneType, meta.ZoneSubType, bestScore
}

func levelRank(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "critical":
		return 5
	case "high":
		return 4
	case "medium":
		return 3
	case "low":
		return 2
	default:
		return 1
	}
}

func (e *wmHeuristicEngine) classifySeismicByMagnitude(text string) (horizonMonitorNewsClassification, bool) {
	magScore, mag, ok := e.seismicMagnitudeSignal(text)
	if !ok {
		return horizonMonitorNewsClassification{}, false
	}
	level := "medium"
	conf := 0.72
	switch {
	case mag >= 7.0:
		level, conf = "critical", 0.90
	case mag >= 6.0:
		level, conf = "high", 0.84
	case mag >= 5.0:
		level, conf = "high", 0.79
	case mag >= 4.5:
		level, conf = "medium", 0.73
	}
	conf += 0.012 * (magScore - 1.6)
	if conf > 0.96 {
		conf = 0.96
	}
	return horizonMonitorNewsClassification{
		Level:      level,
		Category:   "disaster",
		Confidence: conf,
		Source:     "keyword_ac+seismic",
	}, true
}

func (e *wmHeuristicEngine) inferSeismicZoneByMagnitude(text string) (conditionType, zoneType, zoneSubType string, score float64, ok bool) {
	magScore, mag, hasMag := e.seismicMagnitudeSignal(text)
	if !hasMag || mag < 4.5 {
		return "", "", "", 0, false
	}
	score = magScore
	if score < 1.35 {
		score = 1.35
	}
	return "earthquake_affected", "natural_disaster", "earthquake", score, true
}

func (e *wmHeuristicEngine) seismicMagnitudeSignal(text string) (score float64, magnitude float64, ok bool) {
	mag, ok := extractSeismicMagnitude(text)
	if !ok || mag < 4.5 {
		return 0, 0, false
	}
	switch {
	case mag >= 8.0:
		score = 3.30
	case mag >= 7.0:
		score = 2.95
	case mag >= 6.0:
		score = 2.55
	case mag >= 5.5:
		score = 2.25
	case mag >= 5.0:
		score = 1.98
	default:
		score = 1.70
	}
	if hasSeismicLexeme(text) {
		score += 0.24
	}
	if score > 3.45 {
		score = 3.45
	}
	return score, mag, true
}

func hasSeismicLexeme(text string) bool {
	lower := normalizeKeywordText(text)
	if lower == "" {
		return false
	}
	lexemes := []string{
		"earthquake", "aftershock", "seismic", "epicenter", "usgs", "richter",
		"quake", "foreshock", "hypocenter",
	}
	for _, lx := range lexemes {
		if strings.Contains(" "+lower+" ", " "+lx+" ") {
			return true
		}
	}
	return false
}

func extractSeismicMagnitude(text string) (float64, bool) {
	text = normalizeKeywordText(text)
	if text == "" {
		return 0, false
	}
	best := 0.0
	parseMatches := func(re *regexp.Regexp) {
		if re == nil {
			return
		}
		matches := re.FindAllStringSubmatch(text, -1)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			base, err := strconv.Atoi(strings.TrimSpace(m[1]))
			if err != nil || base <= 0 {
				continue
			}
			val := float64(base)
			if len(m) > 2 {
				fracToken := strings.TrimSpace(m[2])
				if fracToken != "" {
					if frac, err := strconv.Atoi(fracToken); err == nil && frac >= 0 {
						div := 10.0
						if len(fracToken) > 1 {
							div = 100.0
						}
						val += float64(frac) / div
					}
				}
			}
			if val > best {
				best = val
			}
		}
	}
	parseMatches(reSeismicMagShort)
	parseMatches(reSeismicMagLong)
	if best >= 4.0 {
		return best, true
	}
	return 0, false
}
