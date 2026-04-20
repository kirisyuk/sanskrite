const navToggle = document.querySelector(".nav-toggle");
const navLinks = document.querySelector(".nav-links");
const languageButtons = document.querySelectorAll(".language-button");
const metaDescription = document.querySelector("#meta-description");
const revealElements = document.querySelectorAll(".reveal, .reveal-card");

async function cleanupLegacyAppRuntime() {
  if (typeof window === "undefined") return;

  if ("serviceWorker" in navigator) {
    try {
      const registrations = await navigator.serviceWorker.getRegistrations();
      await Promise.all(registrations.map((registration) => registration.unregister()));
    } catch (_error) {
      // Ignore cleanup failures on the marketing site.
    }
  }

  if ("caches" in window) {
    try {
      const cacheNames = await caches.keys();
      await Promise.all(cacheNames.map((name) => caches.delete(name)));
    } catch (_error) {
      // Ignore cleanup failures on the marketing site.
    }
  }
}

void cleanupLegacyAppRuntime();

const translations = {
  en: {
    locale: "en",
    pageTitle: "Horde.Vision",
    "pageTitle.platform": "Horizon Decision Fabric | Horde.Vision",
    "pageTitle.agent-mesh": "Horizon Agent Horde | Horde.Vision",
    "pageTitle.insights": "Insights | Horde.Vision",
    "pageTitle.insights-elephant-routing-alias-2026-04":
      "Elephant Alpha is a routing alias | Horde.Vision Insights",
    "pageTitle.insights-industrial-depth-elephant-2026-04":
      "Industrial depth on a free tier | Horde.Vision Insights",
    "pageTitle.insights-frontier-vs-elephant-2026-04":
      "$1.20 vs $0: paid frontier vs free elephant on industrial depth | Horde.Vision Insights",
    "pageTitle.insights-three-verticals-2026-04":
      "FMCG, banking, pharma: same six models, three different leaders | Horde.Vision Insights",
    "pageTitle.insights-how-to-read":
      "How to read these reports | Horde.Vision Insights",
    metaDescription:
      "Horde.Vision is an AI-native decision intelligence company for complex operating environments.",
    "metaDescription.platform":
      "Horizon Decision Fabric on Horde.Vision turns fragmented entities, signals, dependencies, and exposure into a working decision surface.",
    "metaDescription.agent-mesh":
      "Horizon Agent Horde on Horde.Vision runs governed simulations for testing institutional, market, and operational response.",
    "metaDescription.insights":
      "Open-lab field reports and benchmarks from Horde.Vision: how frontier and stealth models behave in real industrial reasoning roles.",
    "metaDescription.insights-elephant-routing-alias-2026-04":
      "A stealth model on OpenRouter is actually a routing alias over multiple backends. And at frontier scale, models diverge in judgment, they do not converge.",
    "metaDescription.insights-industrial-depth-elephant-2026-04":
      "We ran 15 free OpenRouter models across four industrial verticals. Most disappear after ~30 calls when the daily free quota fires. Then we ran the one survivor — openrouter/elephant-alpha, labelled provider 'Stealth' — across 12 industrial verticals. Field report on cost, latency and depth at $0.",
    "metaDescription.insights-frontier-vs-elephant-2026-04":
      "Six models on two industrial packs for $1.20 total. Leadership flips by vertical: glm-5.1 wins steel, gpt-5.4 wins fertilizer. Opus consumes 83% of budget without leading either pack. The union of all five paid frontiers covers only 41% of the steel rubric and 24% of the fertilizer rubric — no model owns a vertical.",
    "metaDescription.insights-three-verticals-2026-04":
      "Six models on three industries you actually live in: FMCG, European banking under Basel III endgame, US commercial pharma 2026. Three different leaders. Opus is first on FMCG and last on pharma. The free elephant takes third place on pharma, beating GPT-5.4 and Opus. Single-flagship model procurement does not work across business-relevant verticals.",
    "metaDescription.insights-how-to-read":
      "Reader's guide to the Horde.Vision Insights series — what the series is, terminology (tier-1/2/3, pack, marker, rubric, stealth, frontier), the chronological arc of published reports, and how to request the reproduction bundle.",
    "nav.aria.primary": "Primary",
    "nav.aria.languageSelector": "Language selector",
    "nav.aria.switchEnglish": "Switch to English",
    "nav.aria.switchRussian": "Switch to Russian",
    "nav.aria.switchKazakh": "Switch to Kazakh",
    "nav.menu": "Menu",
    "nav.about": "About",
    "nav.services": "Capabilities",
    "nav.platform": "Architecture",
    "nav.insights": "Insights",
    "nav.contact": "Contact",
    "hero.eyebrow": "Executive decision intelligence",
    "hero.title": "Clarity for decisions made under pressure.",
    "hero.text":
      "Horde.Vision helps leadership teams understand how geopolitical shifts, regulatory pressure, supply disruption, and second-order effects become operational consequence.",
    "hero.cta": "See the System",
    "intro.label": "Company",
    "intro.title": "Most organizations do not lack information. They lack structured understanding of consequence.",
    "intro.paragraph1":
      "Horde.Vision is built for teams that need to see how signals, dependencies, and exposure connect before disruption becomes an operational problem.",
    "intro.paragraph2":
      "Horizon provides the decision surface; governed research and simulation workflows help leadership teams test what matters next before commitment is forced by reality.",
    "services.label": "Capabilities",
    "services.title": "Core decision intelligence capabilities.",
    "services.geo.title": "Decision Intelligence Briefing",
    "services.geo.subtitle": "Executive framing under uncertainty",
    "services.geo.text":
      "Structured assessments of geopolitical, regulatory, supply, and operational exposure for teams making high-consequence decisions.",
    "services.geo.alt": "Abstract image for geopolitical risks overview",
    "services.monitoring.title": "Signal Monitoring and Grounded Research",
    "services.monitoring.subtitle": "Persistent signal watch and retrieval",
    "services.monitoring.text":
      "Continuous monitoring across entities, jurisdictions, corridors, and weak signals, with grounded evidence that can be reviewed, compared, and escalated.",
    "services.monitoring.alt": "Abstract image for deep monitoring",
    "services.scenario.title": "Scenario Response and Simulation",
    "services.scenario.subtitle": "Governed testing before commitment",
    "services.scenario.text":
      "Scenario workflows that explore disruption, response paths, and second-order effects before reality forces action.",
    "services.scenario.alt": "Abstract image for scenario planning",
    "platforms.label": "System",
    "platforms.title": "One architecture for decision intelligence and scenario response.",
    "platforms.text":
      "Horde.Vision combines a decision surface for consequence mapping with a governed simulation layer for testing response under uncertainty.",
    "platforms.fabric.title": "Horizon Decision Fabric",
    "platforms.fabric.subtitle": "Decision surface for consequence, exposure, and action",
    "platforms.fabric.text":
      "Horizon brings signals, entities, dependencies, and exposure into one governed surface for leadership review, monitoring, and action.",
    "platforms.fabric.cta": "See Horizon",
    "platforms.fabric.alt": "Horizon Decision Fabric overview",
    "platforms.mesh.title": "Horizon Agent Horde",
    "platforms.mesh.subtitle": "Governed simulation for strategic response",
    "platforms.mesh.text":
      "Agent Horde tests plausible institutional, market, and operational response paths before leadership is forced to commit.",
    "platforms.mesh.cta": "See Agent Horde",
    "platforms.mesh.alt": "Horizon Agent Horde overview",
    "platformPage.eyebrow": "Horizon Decision Surface",
    "platformPage.title": "A decision surface for complex operating environments.",
    "platformPage.text":
      "Horizon turns fragmented entities, signals, dependencies, and exposure into a structured analytical surface for monitoring, investigation, and action.",
    "platformPage.ctaPrimary": "View Core System",
    "platformPage.ctaSecondary": "View Applications",
    "platformPage.hero.alt": "Horizon Decision Fabric overview",
    "platformPage.foundation.label": "Decision Intelligence Core",
    "platformPage.foundation.title": "A governed analytical system, not a loose collection of reports.",
    "platformPage.foundation.taxonomy.title": "Structured World Model",
    "platformPage.foundation.taxonomy.subtitle":
      "Entities, dependencies, topology, operational context",
    "platformPage.foundation.taxonomy.text":
      "Horizon organizes organizations, assets, corridors, products, and events inside a governed world model so consequence can be traced rather than guessed.",
    "platformPage.foundation.taxonomy.alt": "Object topology and relationship structure",
    "platformPage.foundation.signals.title": "Signal Grounding and Propagation",
    "platformPage.foundation.signals.subtitle":
      "Ingestion, retrieval, routing, consequence",
    "platformPage.foundation.signals.text":
      "Signals are grounded against structured entities and dependencies, then surfaced where they change exposure, relevance, and decision priority.",
    "platformPage.foundation.signals.alt":
      "Connected signal propagation across mapped dependencies",
    "platformPage.foundation.ai.title": "AI-Native Analytical Runtime",
    "platformPage.foundation.ai.subtitle":
      "Retrieval, synthesis, governed workflows",
    "platformPage.foundation.ai.text":
      "AI helps expand research, structure evidence, compare patterns, and synthesize working hypotheses while keeping outputs reviewable, traceable, and operationally useful.",
    "platformPage.foundation.ai.alt":
      "Platform analytical surface with AI-supported workflows",
    "platformPage.cases.label": "Decision Intelligence Applications",
    "platformPage.cases.title":
      "Representative problems Horizon is built to frame and operationalize.",
    "platformPage.cases.supply.title": "Supply Chain Exposure",
    "platformPage.cases.supply.subtitle":
      "Assets, routes, bottlenecks, propagation",
    "platformPage.cases.supply.text":
      "Trace how disruptions move through suppliers, logistics corridors, industrial clusters, and strategic nodes to identify operational concentration before it becomes a supply event.",
    "platformPage.cases.supply.alt": "Supply chain and disruption case",
    "platformPage.cases.governance.title": "Control and Governance Mapping",
    "platformPage.cases.governance.subtitle":
      "Ownership chains, affiliates, decision authority",
    "platformPage.cases.governance.text":
      "Clarify who controls what, through which entities, under which layers of ownership, influence, or delegated authority, when governance structure is itself part of the operating risk.",
    "platformPage.cases.governance.alt": "Complex governance case",
    "platformPage.cases.regulatory.title": "Regulatory and Sanctions Impact",
    "platformPage.cases.regulatory.subtitle":
      "Policy shifts, jurisdictional pressure, downstream consequence",
    "platformPage.cases.regulatory.text":
      "Connect regulatory decisions to jurisdictions, entities, corridors, and production footprints so teams can see where legal or policy shifts become operational constraints.",
    "platformPage.cases.regulatory.alt": "Regulatory impact case",
    "platformPage.story.label": "Representative Views",
    "platformPage.story.title": "Views that move analysis from fragmented signal to operational consequence.",
    "platformPage.caseMap.title": "Connected Maps",
    "platformPage.caseMap.subtitle": "Routes, nodes, exposure",
    "platformPage.caseMap.text":
      "Connected map views place facilities, transit routes, suppliers, and operating dependencies in one frame so exposure is visible before disruption becomes operational.",
    "platformPage.caseMap.alt": "Connected map view showing operational nodes and routes",
    "platformPage.caseGraph.title": "Relational Graphs",
    "platformPage.caseGraph.subtitle": "Control, ownership, influence",
    "platformPage.caseGraph.text":
      "Graph views surface control chains, ownership links, affiliated entities, and indirect proximity that are easy to miss in linear reporting.",
    "platformPage.caseGraph.alt":
      "Relationship graph showing connected entities and dependencies",
    "platformPage.caseScenario.title": "Affected Areas",
    "platformPage.caseScenario.subtitle": "Impact zones, spillovers, proximity",
    "platformPage.caseScenario.text":
      "Affected-area views isolate disruption zones, overlay them with assets and logistics, and clarify where second-order effects start to matter.",
    "platformPage.caseScenario.alt":
      "Affected areas map highlighting impact zones and exposed assets",
    "platformPage.caseDetailed.title": "Industrial Detail",
    "platformPage.caseDetailed.subtitle": "Industrial zones, adjacency, operating density",
    "platformPage.caseDetailed.text":
      "Detailed industrial views help teams inspect plant adjacency, utility dependence, and where localized disruption can propagate across clustered assets.",
    "platformPage.caseDetailed.alt":
      "Detailed industrial zones view showing concentrated assets",
    "platformPage.summary.label": "Decision surface",
    "platformPage.summary.title":
      "One working surface for signals, dependencies, and operational consequence.",
    "platformPage.summary.text":
      "Horizon is built for teams that need structured decision intelligence rather than disconnected monitoring, static reporting, or unguided search.",
    "platformPage.summary.cta": "Request Platform Access",
    "agentMeshPage.eyebrow": "Horizon Agent Horde",
    "agentMeshPage.title":
      "Governed simulation for scenario response and strategic testing.",
    "agentMeshPage.text":
      "Agent Horde extends Horde.Vision with multi-agent simulation workflows that test how institutions, companies, markets, and narratives may move after a scenario signal is introduced.",
    "agentMeshPage.ctaPrimary": "View Simulation Runtime",
    "agentMeshPage.ctaSecondary": "View Applications",
    "agentMeshPage.hero.alt": "Horizon Agent Horde overview",
    "agentMeshPage.operation.label": "Governed Simulation Runtime",
    "agentMeshPage.operation.title":
      "Scenario injection, parallel worlds, and traceable review.",
    "agentMeshPage.operation.alt": "Agent Horde simulation layer",
    "agentMeshPage.operation.signal.title": "Signal Injection",
    "agentMeshPage.operation.signal.subtitle": "Decision questions framed as executable inputs",
    "agentMeshPage.operation.signal.text":
      "Each run begins with a structured scenario request: a regulatory move, market disruption, access restriction, industrial event, or narrative shift that must be tested before commitment.",
    "agentMeshPage.operation.worlds.title": "Governed World Simulation",
    "agentMeshPage.operation.worlds.subtitle":
      "Parallel populations, reaction paths, controlled variation",
    "agentMeshPage.operation.worlds.text":
      "Multiple governed worlds can be explored in parallel so competing institutional, commercial, and operational responses are examined rather than assumed.",
    "agentMeshPage.operation.trace.title": "Traceable Outcomes",
    "agentMeshPage.operation.trace.subtitle": "Artifacts, lineage, review checkpoints",
    "agentMeshPage.operation.trace.text":
      "Simulation runs preserve artifacts, reasoning lineage, review stages, and aggregate outputs so alternative response paths can be compared, challenged, and reused.",
    "agentMeshPage.cases.label": "Scenario Applications",
    "agentMeshPage.cases.title":
      "Representative scenario classes for governed response testing.",
    "agentMeshPage.cases.supply.title": "Supply Shock and Escalation",
    "agentMeshPage.cases.supply.subtitle":
      "Facilities, corridors, substitute paths, reaction timing",
    "agentMeshPage.cases.supply.text":
      "Test how firms, regulators, logistics actors, and adjacent operators may react when a disruption signal is introduced into a concentrated supply chain.",
    "agentMeshPage.cases.supply.alt": "Supply shock simulation case",
    "agentMeshPage.cases.regulatory.title": "Regulatory Move and Response",
    "agentMeshPage.cases.regulatory.subtitle":
      "Jurisdictions, enforcement paths, downstream adaptation",
    "agentMeshPage.cases.regulatory.text":
      "Simulate how legal, administrative, and market participants may respond when a rule change, sanctions signal, or licensing constraint enters the system.",
    "agentMeshPage.cases.regulatory.alt": "Regulatory response simulation case",
    "agentMeshPage.cases.narrative.title": "Narrative Cascade and Alignment",
    "agentMeshPage.cases.narrative.subtitle":
      "Institutional messaging, market perception, secondary coordination",
    "agentMeshPage.cases.narrative.text":
      "Observe how narrative actors, institutions, and commercial entities may align, diverge, or reinforce each other after a scenario-driven signal enters public space.",
    "agentMeshPage.cases.narrative.alt": "Narrative cascade simulation case",
    "agentMeshPage.summary.label": "Simulation layer",
    "agentMeshPage.summary.title":
      "A governed environment for testing response before reality forces commitment.",
    "agentMeshPage.summary.text":
      "Agent Horde helps teams compare plausible reaction paths without reducing complex institutions and markets to a single forecast.",
    "agentMeshPage.summary.cta": "Request Access",
    "insightsPage.eyebrow": "Open Lab · Field Reports",
    "insightsPage.title": "No best LLM. Best model for the role, in a vertical.",
    "insightsPage.lead":
      "Reproducible benchmarks and field notes from a working multi-agent decision-intelligence system. Not a leaderboard. Not synthetic tests. Real industrial reasoning packs, real role contracts, published artifacts.",
    "insightsPage.chip.cadence": "Cadence",
    "insightsPage.chip.cadenceValue": "Quarterly + ad-hoc",
    "insightsPage.chip.scope": "Scope",
    "insightsPage.chip.scopeValue": "Role × Vertical × Multi-agent",
    "insightsPage.chip.published": "Published",
    "insightsPage.intro.label": "Why this section exists",
    "insightsPage.intro.title":
      "Public benchmarks answer \"which model is smarter.\" We answer \"which model in which role on which vertical, and where it breaks.\"",
    "insightsPage.intro.p1":
      "Every report here is a short field test. We pick a real industrial scenario, write it out as a plain prompt plus a rubric of things a deep answer should name, and run the same prompt across a shortlist of paid and free LLMs. Then we publish the scores, the cost per call, the raw outputs, and a note on what broke. No synthetic benchmarks, no leaderboards, no \"we think\" — only \"data shows / data does not show\".",
    "insightsPage.intro.p2":
      "New reader? Start with How to read these reports — a 3-minute primer with the glossary (tier-1/2/3, pack, marker, rubric, stealth, frontier), the order the reports were written in, and how to request the reproduction bundle. Everything below assumes that shared vocabulary.",
    "insightsPage.intro.howToReadLink": "How to read these reports",
    "insightsPage.card.industrialDepthElephant.category":
      "Report #2 · Industrial Depth on a Free Tier",
    "insightsPage.card.industrialDepthElephant.title":
      "Industrial depth on a free tier: 30 calls, then one model survives the day.",
    "insightsPage.card.industrialDepthElephant.lead":
      "15 free OpenRouter models × 4 industrial packs = 60 cells. Only 17 succeeded. After ~30 calls a global daily quota fires and 14 of 15 models go dark. We then ran the survivor — openrouter/elephant-alpha, branded by OpenRouter as provider Stealth — across 12 industrial verticals. 32 minutes, $0.",
    "insightsPage.card.industrialDepthElephant.metaDate": "2026 · April",
    "insightsPage.card.industrialDepthElephant.metaVertical": "Verticals: 12",
    "insightsPage.card.industrialDepthElephant.metaRole": "Open-ended prose",
    "insightsPage.card.industrialDepthElephant.metaModels": "15 models tried",
    "insightsPage.card.frontierVsElephant.category":
      "Report #3 · Frontier Ceiling",
    "insightsPage.card.frontierVsElephant.title":
      "$1.20 vs $0. Five paid frontier models against one free elephant on industrial depth.",
    "insightsPage.card.frontierVsElephant.lead":
      "Six models, two industrial packs, $1.20 total. Leadership flips by vertical (glm-5.1 on steel, gpt-5.4 on fertilizer). Opus burns 83% of budget without leading either. And even the union of all five paid frontiers covers only 41% of the steel rubric and 24% of the fertilizer rubric — no model owns a vertical.",
    "insightsPage.card.frontierVsElephant.metaDate": "2026 · April",
    "insightsPage.card.frontierVsElephant.metaVertical":
      "Verticals: 2 (steel, fertilizer)",
    "insightsPage.card.frontierVsElephant.metaRole": "Open-ended prose",
    "insightsPage.card.frontierVsElephant.metaModels":
      "6 models · $1.20 total",
    "insightsPage.card.threeVerticals.category":
      "Report #4 · Three Recognizable Verticals",
    "insightsPage.card.threeVerticals.title":
      "FMCG, banking, pharma. Same six models, three different leaders. Opus is first on FMCG and last on pharma.",
    "insightsPage.card.threeVerticals.lead":
      "Three industries you actually live in: FMCG procurement under EU PPWR, a European bank facing Basel III endgame, a top-15 pharma planning 2027 commercial under IRA. Same six models, $1.58 total. Three different leaders. The free elephant takes third place on pharma at $0, beating GPT-5.4 and Opus. Opus comes last on pharma at $0.41.",
    "insightsPage.card.threeVerticals.metaDate": "2026 · April",
    "insightsPage.card.threeVerticals.metaVertical":
      "Verticals: 3 (FMCG, banking, pharma)",
    "insightsPage.card.threeVerticals.metaRole": "Open-ended prose",
    "insightsPage.card.threeVerticals.metaModels":
      "6 models · $1.58 total",
    "insightsPage.card.elephantAlias.category": "Report #1 · Model Provenance",
    "insightsPage.card.elephantAlias.title":
      "A \"stealth\" OpenRouter model is actually a routing alias over multiple backends.",
    "insightsPage.card.elephantAlias.lead":
      "Seven runs of the same prompt, one stealth alias. Latency ranges 15× across runs (2.0s → 30.3s). Composition flips between stop, conditional, and go. Two structurally different output styles. And the larger story: at frontier scale, models diverge in judgment, they do not converge.",
    "insightsPage.card.elephantAlias.metaDate": "2026 · April",
    "insightsPage.card.elephantAlias.metaVertical": "Vertical: semiconductor",
    "insightsPage.card.elephantAlias.metaRole": "Role: critic",
    "insightsPage.card.elephantAlias.metaModels": "11 models",
    "insightsPage.card.cta": "Read the report →",
    "insightsPage.empty":
      "More reports land here every quarter. Next in the queue: a critic-role contract fidelity head-to-head on the same model pool, then an explorer-role probe across three new verticals. See the reader's guide for what those terms mean.",
    "preamble.label.about": "What this report is",
    "preamble.label.read": "How to read this",
    "preamble.speed.fast": "2 min",
    "preamble.speed.mid": "10 min",
    "preamble.speed.full": "30 min",
    "preamble.footer":
      "New reader? Start with How to read these reports — a 3-minute primer, glossary of terms used across the series (tier-1/2/3, pack, marker, rubric, stealth, frontier), and the order the reports were written in.",
    "howToRead.eyebrow": "Reader's guide · Start here",
    "howToRead.title": "How to read these reports.",
    "howToRead.lead":
      "This page is a 3-minute primer for the Horde.Vision Insights series. It explains what we publish, the vocabulary we use, the order in which the reports were written, and how to get the raw artifacts if you want to reproduce anything. Read this once and the rest of the series becomes much easier to navigate.",
    "howToRead.series.heading": "What this series is",
    "howToRead.series.p1":
      "Horde.Vision is a decision-intelligence company. We build a multi-agent operating layer for institutional and industrial reasoning — the kind of work where a single LLM call is never the whole answer, and where \"which model is best?\" is the wrong question. The right question is \"which model, in which role, on which vertical, and where does it break?\"",
    "howToRead.series.p2":
      "These reports come out of the same harness we run internally to evaluate the models we put into production. When an evaluation produces a finding we think operators outside our team would care about, we write it up and publish it here — with the scoring rubric, the cost numbers, the raw outputs, and reproduction instructions. No synthetic benchmarks. No leaderboards. No \"model X scored 87%.\" Real industrial scenarios, real cost ledgers, real failure modes.",
    "howToRead.series.p3":
      "The intended reader is an operator: a head of strategy, a CIO, a procurement lead, a chief of staff, a research director. Someone who has to make a decision next quarter about which models to put into their workflows and cannot rely on headline benchmark scores to make it. These reports are written to that reader.",
    "howToRead.speeds.heading": "How to read a report at three speeds",
    "howToRead.speeds.intro":
      "Each report is structured so you can extract the headline in two minutes or the full methodology in thirty. Pick your speed up front.",
    "howToRead.speeds.two.time": "2 minutes",
    "howToRead.speeds.two.title": "The headline",
    "howToRead.speeds.two.body":
      "Read the hero block at the top of the page (eyebrow, title, lead) and the TL;DR box right under it. That is the full argument of the report in bullet form. If the TL;DR doesn't surprise you, you can stop here — the body is the evidence, not the thesis.",
    "howToRead.speeds.ten.time": "10 minutes",
    "howToRead.speeds.ten.title": "The findings",
    "howToRead.speeds.ten.body":
      "Add the \"Finding 1 / 2 / 3\" sections and the charts. These show how we got each bullet in the TL;DR — the rank tables, the cost ledger, the coverage-ceiling calculation. At this speed you should leave with both the headline and the evidence for it.",
    "howToRead.speeds.thirty.time": "30 minutes",
    "howToRead.speeds.thirty.title": "The methodology",
    "howToRead.speeds.thirty.body":
      "Add the Methodology, Reproduction, and Artifacts sections at the bottom. These tell you exactly how the run was configured, what we did and did not measure, and how to get the reproduction bundle if you want to rerun the same matrix on your own prompts or model shortlist.",
    "howToRead.terms.heading": "Terminology",
    "howToRead.terms.intro":
      "Twelve terms show up across the series. If one of them is unfamiliar in a report, come back here.",
    "howToRead.terms.pack.term": "Pack",
    "howToRead.terms.pack.def":
      "A self-contained industrial scenario we send to a model. Usually a 3-paragraph setup (a fictional company, a real regulatory and market context, and a 2027 decision to prepare for) plus six escalating questions in plain prose. No schema, no system prompt, no tools. Every pack lives in a directory with a prompt.md and a markers.yaml.",
    "howToRead.terms.rubric.term": "Rubric",
    "howToRead.terms.rubric.def":
      "The full set of things we expect a \"deep\" answer to name or quantify — company names, regulation numbers, dates, prices, sites, people. Written before the run, never shown to the model, stored as markers.yaml alongside the pack.",
    "howToRead.terms.marker.term": "Marker",
    "howToRead.terms.marker.def":
      "A single item in the rubric — e.g. \"Kalundborg\", \"72.5%\", \"Article 22\", \"Wezlana\". Scoring is a conservative case-insensitive substring match: the marker is either present in the response or not.",
    "howToRead.terms.tiers.term": "Tier 1 / Tier 2 / Tier 3",
    "howToRead.terms.tiers.def":
      "Difficulty bands inside a rubric. Tier 1 — things any large LLM with general coverage of the vertical should know. Tier 2 — competent practitioner knowledge. Tier 3 — expert, recent, or event-specific knowledge (2024-2026 regulatory dates, specific brand names of 2025 biosimilar launches, quantified discount ranges, named supervisors). Tier 3 is where the gap between models shows up.",
    "howToRead.terms.harness.term": "Harness",
    "howToRead.terms.harness.def":
      "The Python script we run to execute a pack against a list of models. Sends the prompt, records wall latency, captures input/output tokens from the provider, preserves the raw text, and scores it against the rubric. Deliberately boring — no judging LLM, no subjective scoring.",
    "howToRead.terms.frontier.term": "Frontier model",
    "howToRead.terms.frontier.def":
      "The current top-tier paid flagships from the big labs. In this series that means claude-opus-4.7, gpt-5.4, gemini-3.1-pro-preview, glm-5.1, and kimi-k2.5. The pool changes as new generations release.",
    "howToRead.terms.stealth.term": "Stealth model · Elephant-alpha",
    "howToRead.terms.stealth.def":
      "OpenRouter periodically runs anonymized early-preview slots under names like openrouter/elephant-alpha and the literal provider label Stealth. The model behind the alias is deliberately not disclosed. elephant-alpha is the slot we have been probing across this series; as report #1 shows, it is actually a routing alias over multiple backends, not a single model.",
    "howToRead.terms.roles.term":
      "Role (explorer / critic / canonicalizer / supervisor)",
    "howToRead.terms.roles.def":
      "Roles inside our own multi-agent system. An explorer drafts free-form prose and enumerates entities; a critic evaluates a draft against a strict contract; a canonicalizer maps findings to our system's entity types; a supervisor routes work between roles. Most published reports focus on explorer or critic fitness.",
    "howToRead.terms.contract.term": "Strict-JSON contract",
    "howToRead.terms.contract.def":
      "A rigid output schema enforced on a role (e.g. the critic role). Contract-fidelity reports measure how often a model returns output that is both valid JSON and conforms to the expected fields — a separate axis from prose depth.",
    "howToRead.terms.coverage.term": "Coverage ceiling",
    "howToRead.terms.coverage.def":
      "The union of tier-3 markers caught by any model in the pool, divided by the size of the rubric. A useful upper bound: \"pool all these models and the best you can do is this fraction of the rubric.\" In our reports so far it sits at 32-48% on real industrial verticals — most of the rubric goes uncaught by everyone.",
    "howToRead.terms.costmarker.term": "Cost-per-marker",
    "howToRead.terms.costmarker.def":
      "Total USD spent on a model's response divided by the number of tier-3 markers it caught. Produces apples-to-apples comparisons across models with very different price sheets. A 53× spread between the cheapest and most expensive paid model on the same prompt is not unusual.",
    "howToRead.terms.inversion.term": "Leadership inversion",
    "howToRead.terms.inversion.def":
      "A single model moving from first place on one vertical to last on another, with the same prompt format and the same budget. The core finding of report #4: three of six models in the pool do this across just three verticals. No single \"best model\" exists across business-relevant industries.",
    "howToRead.arc.heading": "The reports, in order",
    "howToRead.arc.intro":
      "Each report in the series stands on its own, but they were written in this order and each one leans slightly on the last. If you read them in sequence the arc is: what is this free model? → does free tier hold at all? → do paid flagships beat it? → does any of this hold across verticals you actually live in?",
    "howToRead.arc.r1.title":
      "A stealth model is a routing alias. And at frontier scale, models diverge.",
    "howToRead.arc.r1.body":
      "Eleven models on the same semiconductor / CHIPS Act session pack, under a strict-JSON critic contract. Two findings: elephant-alpha is a routing alias over multiple backends, not a single model; and at frontier scale, models diverge in judgment, they do not converge on consensus truth.",
    "howToRead.arc.r1.link": "Read #1 →",
    "howToRead.arc.r2.title":
      "Industrial depth on a free tier: 30 calls, then one model holds the day.",
    "howToRead.arc.r2.body":
      "Fifteen free OpenRouter models against four industrial packs. After about thirty calls a global daily quota fires and fourteen of fifteen models go dark. The one survivor — elephant-alpha — gets extended to twelve verticals. Field report on what you can extract from a free tier when everything else breaks.",
    "howToRead.arc.r2.link": "Read #2 →",
    "howToRead.arc.r3.title":
      "$1.20 vs $0 — paid frontier vs free elephant on industrial depth.",
    "howToRead.arc.r3.body":
      "Six models, two heavy industrial packs (steel & CBAM, fertilizers & ag inputs), $1.20 total. The leader changes by vertical. Opus burns 83% of the budget and leads neither pack. The union of all five paid frontiers covers only 41% of the steel rubric and 24% of the fertilizer rubric — no model owns a vertical.",
    "howToRead.arc.r3.link": "Read #3 →",
    "howToRead.arc.r4.title":
      "FMCG, banking, pharma. Same six models, three different leaders.",
    "howToRead.arc.r4.body":
      "Three industries the reader actually lives in: FMCG procurement under EU PPWR, a European bank under Basel III endgame, a top-15 pharma planning 2027 commercial under the IRA. Opus is first on FMCG and last on pharma. The free elephant takes third place on pharma at $0, beating GPT-5.4 and Opus. This is leadership inversion across three recognizable verticals.",
    "howToRead.arc.r4.link": "Read #4 →",
    "howToRead.arc.next":
      "Next in the queue: a critic-role contract-fidelity head-to-head on the same model pool, then explorer-role fitness across three new verticals.",
    "howToRead.artifacts.heading": "Artifacts and reproduction",
    "howToRead.artifacts.p1":
      "Every report ends with a Reproduction block showing the exact command we ran. The packs, rubrics, harness, and raw JSONL outputs live in a private research repo we do not publish publicly — partly because they change every week as new models come out, partly because they include pricing structures and ranking choices that are genuinely a product.",
    "howToRead.artifacts.p2": "We are happy to share them on request. Two flavors:",
    "howToRead.artifacts.li1":
      "Reproduction bundle — a tarball with the prompt.md and markers.yaml files for every pack cited in a given report, the raw JSONL from the run, the harness script, and a one-line command. Enough to rerun the same matrix yourself. Free, sent by email to a real operator address.",
    "howToRead.artifacts.li2":
      "Custom run — we design packs for your verticals, point the harness at your model shortlist (including anything you cannot easily test in public — internal fine-tunes, VPC endpoints, rate-limited previews), and deliver the full rubric, charts, cost ledger, and interpretation memo. This is a paid engagement; a typical run is a week.",
    "howToRead.artifacts.p3":
      "Write us at hello@horde.vision and tell us which report you read and what you want to do with it — that is usually enough for us to know what to send back.",
    "howToRead.conventions.heading": "Conventions",
    "howToRead.conventions.li1":
      "Single shot per cell unless we explicitly label a report as a variance probe. One (model × pack) run in, one JSONL row out. We note in methodology when we repeat.",
    "howToRead.conventions.li2":
      "Open-ended prose unless we explicitly label the report as strict-JSON. Most reports are free-form; contract-fidelity reports use a separate axis.",
    "howToRead.conventions.li3":
      "Cost numbers are batch-observed. We record the exact input_tokens and output_tokens returned by the provider and multiply by the list price at publication date. We show the ledger so you can recompute with your own pricing.",
    "howToRead.conventions.li4":
      "Rubric scoring is conservative. Case-insensitive substring match only — no fuzzy match, no synonym expansion, no LLM judge. False negatives are therefore slightly inflated and false positives are very rare. We note this tradeoff when it matters.",
    "howToRead.conventions.li5":
      "We do not hide failed runs. Every FAIL or empty_content in a published table is a real outcome. Some of the most interesting findings are about failure modes.",
    "howToRead.cta.title":
      "Now you are ready to read any report in the series.",
    "howToRead.cta.text":
      "Go to the Insights index and pick a report — or jump straight to #4 if you want the most recent field test, to #1 if you want the methodology origin, or to the most relevant vertical for your operation.",
    "howToRead.cta.button": "See all reports",
    "elephantAlias.preamble.about":
      "Report #1 of our Horde.Vision Insights series — short field reports where we run the same prompt harness across paid and free LLMs on real industrial scenarios and publish the rubric-scored depth, cost, and per-model quirks. This one runs the same prompt seven times against an anonymized \"stealth\" slot on OpenRouter, then against ten named frontier and fast flagships. Two findings: a stealth model is not always a model (it can be a routing alias over several backends), and at frontier scale models diverge in judgment rather than converging on one answer.",
    "elephantAlias.preamble.speed.fast": "hero + TL;DR",
    "elephantAlias.preamble.speed.mid":
      "+ Finding 1 (stealth alias) and Finding 2 (frontier divergence)",
    "elephantAlias.preamble.speed.full":
      "+ methodology, reproduction, request the bundle",
    "industrialDepthElephant.preamble.about":
      "Report #2 of our Horde.Vision Insights series. We ran fifteen free models on OpenRouter against four industrial packs (battery, pharma biologics, nuclear, aerospace). After about thirty calls a global daily quota fired and fourteen of fifteen went dark — one survived, so we extended it to twelve verticals. This is a field report on what a free tier actually buys you, where it breaks, and where one non-labeled stealth slot holds up.",
    "industrialDepthElephant.preamble.speed.fast": "hero + TL;DR",
    "industrialDepthElephant.preamble.speed.mid":
      "+ the cliff chart, the 12-vertical depth table, variance probe",
    "industrialDepthElephant.preamble.speed.full":
      "+ methodology, reproduction, bundle on request",
    "frontierVsElephant.preamble.about":
      "Report #3 of our Horde.Vision Insights series. Six models — Claude Opus 4.7, GPT-5.4, Gemini 3.1 Pro, GLM-5.1, Kimi K2.5, and the free stealth slot elephant-alpha — run the same prompt harness against two heavy industrial packs (EU steel under CBAM, and fertilizer / ag-inputs under sanctions and export controls). Total bill for the paid models: $1.20. Core finding: no model leads both verticals, and paid flagships do not clearly dominate the free slot on knowledge depth.",
    "frontierVsElephant.preamble.speed.fast": "hero + TL;DR",
    "frontierVsElephant.preamble.speed.mid":
      "+ tier-3 chart, cost ledger, per-vertical ranks",
    "frontierVsElephant.preamble.speed.full":
      "+ methodology, reproduction, bundle on request",
    "threeVerticals.preamble.about":
      "Report #4 of our Horde.Vision Insights series. Same six models as the previous report — Claude Opus 4.7, GPT-5.4, Gemini 3.1 Pro, GLM-5.1, Kimi K2.5, and the free stealth slot elephant-alpha — across three industries the reader actually lives in: FMCG procurement under EU PPWR, a European bank under Basel III endgame, and a top-15 pharma planning its 2027 commercial strategy under the IRA. Core finding: three different leaders across three verticals. Opus leads FMCG and comes last on pharma. The free slot takes third on pharma at $0. No model owns the field.",
    "threeVerticals.preamble.speed.fast": "hero + TL;DR",
    "threeVerticals.preamble.speed.mid":
      "+ rank inversion table, coverage ceiling, cost-per-marker spread",
    "threeVerticals.preamble.speed.full":
      "+ methodology, reproduction, bundle on request",
    "elephantAlias.eyebrow": "Field Report · Model Provenance · 2026-04-18",
    "elephantAlias.title":
      "A stealth model is a routing alias. And at frontier scale, models diverge.",
    "elephantAlias.lead":
      "Eleven models on the same TSMC / ASML / CHIPS Act session pack, under a strict-JSON critic contract. Two findings worth publishing — both counter to the standard \"scale → consensus → truth\" narrative — emerged from one afternoon of runs.",
    "elephantAlias.back": "← Back to Insights",
    "elephantAlias.cta.title": "Same harness, your vertical.",
    "elephantAlias.cta.text":
      "If you operate in semiconductor, energy, maritime, lithium, or REE supply chains and you are deciding which model to put into a critic role on your own pipeline — we can run this same harness against your prompts. Reproducibility is the whole product.",
    "elephantAlias.cta.button": "Request a custom run",
    "industrialDepthElephant.eyebrow":
      "Field Report · Industrial Depth · 2026-04-19",
    "industrialDepthElephant.title":
      "Industrial depth on a free tier: 30 calls, then one model survives the day.",
    "industrialDepthElephant.lead":
      "Fifteen free OpenRouter models, four industrial verticals, open-ended prose answers. Most models disappear after the daily free quota fires. We then ran the one survivor — openrouter/elephant-alpha, branded by OpenRouter under the literal provider name Stealth — across twelve industrial verticals. This is what we found.",
    "industrialDepthElephant.back": "← Back to Insights",
    "industrialDepthElephant.cta.title": "Same harness, your vertical.",
    "industrialDepthElephant.cta.text":
      "If your operations sit in any of the verticals above — or in an adjacent one — and you are weighing free-tier or stealth-aliased models for first-pass industrial orientation, we can run this same harness against your prompts on your model pool. Reproducibility is the whole product.",
    "industrialDepthElephant.cta.button": "Request a custom run",
    "frontierVsElephant.eyebrow":
      "Field Report · Frontier Ceiling · 2026-04-19",
    "frontierVsElephant.title":
      "$1.20 vs $0. Frontier flagships versus a free stealth model on industrial depth.",
    "frontierVsElephant.lead":
      "Six models on the same two industrial packs: openrouter/elephant-alpha (free, provider Stealth) against five paid frontier flagships — claude-opus-4.7, gpt-5.4, gemini-3.1-pro, glm-5.1, kimi-k2.5. Total spend: $1.20. Total wall time: 23 minutes. Three findings worth publishing — and one benchmarking trap that almost killed two of the five paid models.",
    "frontierVsElephant.back": "← Back to Insights",
    "frontierVsElephant.cta.title":
      "Same harness, your vertical, your model pool.",
    "frontierVsElephant.cta.text":
      "If you are weighing a single-flagship procurement decision against a multi-model strategy in your operating vertical, we can run this same matrix against your prompts on your shortlist. The 41% / 24% coverage ceiling shown here is a starting point — your vertical will have its own.",
    "frontierVsElephant.cta.button": "Request a custom run",
    "threeVerticals.eyebrow":
      "Field Report · Three Verticals You Actually Live In · 2026-04-19",
    "threeVerticals.title":
      "Three industries, same six models, three different leaders. Opus comes first on FMCG and last on pharma.",
    "threeVerticals.lead":
      "FMCG procurement under EU PPWR. A European universal bank preparing for Basel III endgame. A top-15 pharma planning 2027 commercial under Medicare price negotiation. Same six models, same harness, same 8 192-token budget. The leader changes on every vertical. claude-opus-4.7 wins FMCG and finishes last on pharma. The free openrouter/elephant-alpha takes third place on pharma at $0, beating GPT-5.4 and Opus. This is what \"no best LLM, best model for the role in the vertical\" looks like in three recognizable industries.",
    "threeVerticals.back": "← Back to Insights",
    "threeVerticals.cta.title":
      "Your vertical, your model pool, your prompts.",
    "threeVerticals.cta.text":
      "If you are standing up an internal AI program and deciding which model to put against your real operating reality — across the verticals you actually live in — we can run this same matrix on your prompts and your shortlist. The 78% Opus cost share, the pharma rank inversion, the 53× cost-per-marker spread: each of those is a starting point. Your vertical will produce its own.",
    "threeVerticals.cta.button": "Request a custom run",
    "footer.label": "Horde.Vision",
    "footer.title": "Built for leadership under uncertainty.",
    "footer.note": "Structured decision intelligence, grounded research, and governed simulation for complex operating environments.",
    "footer.location.title": "Location",
    "footer.location.line1": "Dostyk, 16",
    "footer.location.line2": "Astana, Kazakhstan",
    "footer.contact.title": "Contact",
    "footer.contact.line1": "hello@horde.vision",
    "footer.availability.title": "Availability",
    "footer.availability.line1": "Meetings by request",
    "footer.availability.line2": "Additional details coming soon",
    "footer.legal.title": "Legal",
    "footer.legal.privacy": "Privacy Notice",
    "footer.legal.terms": "Terms of Usage",
    "footer.legal.technical": "Technical Disclosure",
  },
  ru: {
    locale: "ru",
    pageTitle: "Horde.Vision",
    "pageTitle.platform": "Horizon Decision Fabric | Horde.Vision",
    "pageTitle.agent-mesh": "Horizon Agent Horde | Horde.Vision",
    "pageTitle.insights": "Аналитика | Horde.Vision",
    "pageTitle.insights-elephant-routing-alias-2026-04":
      "Elephant Alpha — это routing alias | Horde.Vision Аналитика",
    "pageTitle.insights-industrial-depth-elephant-2026-04":
      "Индустриальная глубина на free-tier | Horde.Vision Аналитика",
    "pageTitle.insights-frontier-vs-elephant-2026-04":
      "$1.20 vs $0: платный frontier против бесплатного elephant | Horde.Vision Аналитика",
    "pageTitle.insights-three-verticals-2026-04":
      "FMCG, банки, фарма: те же шесть моделей, три разных лидера | Horde.Vision Аналитика",
    "pageTitle.insights-how-to-read":
      "Как читать эти отчёты | Horde.Vision Аналитика",
    metaDescription:
      "Horde.Vision — AI-native компания в области decision intelligence для сложной операционной среды.",
    "metaDescription.platform":
      "Horizon Decision Fabric на Horde.Vision превращает разрозненные сущности, сигналы, зависимости и зоны уязвимости в рабочую поверхность для решений.",
    "metaDescription.agent-mesh":
      "Horizon Agent Horde на Horde.Vision запускает управляемые симуляции для проверки институциональной, рыночной и операционной реакции.",
    "metaDescription.insights":
      "Открытая лаборатория Horde.Vision: воспроизводимые бенчмарки и field notes о том, как frontier и stealth модели ведут себя в реальных индустриальных reasoning-ролях.",
    "metaDescription.insights-elephant-routing-alias-2026-04":
      "Stealth-модель на OpenRouter оказывается routing alias-ом над несколькими бэкендами. И на frontier-масштабе модели расходятся в суждениях, а не сходятся.",
    "metaDescription.insights-industrial-depth-elephant-2026-04":
      "15 free-моделей OpenRouter на 4 индустриальных вертикалях. Большинство выпадают после ~30 вызовов из-за дневной квоты. Затем мы прогнали единственного выжившего — openrouter/elephant-alpha, под брендом провайдера 'Stealth' — на 12 индустриальных вертикалях. Field report: цена, латентность, глубина за $0.",
    "metaDescription.insights-frontier-vs-elephant-2026-04":
      "Шесть моделей на двух индустриальных пакетах за $1.20. Лидерство переключается по вертикали: glm-5.1 берёт сталь, gpt-5.4 — удобрения. Opus съедает 83% бюджета и не лидирует ни на одном пакете. Объединение всех 5 платных flagships покрывает только 41% steel rubric и 24% fertilizer rubric — ни одна модель не владеет вертикалью.",
    "metaDescription.insights-three-verticals-2026-04":
      "Шесть моделей на трёх индустриях, в которых вы реально живёте: FMCG, европейский банк под Basel III endgame, US commercial pharma 2026. Три разных лидера. Opus — первый на FMCG и последний на фарме. Бесплатный elephant занимает третье место на фарме, обогнав GPT-5.4 и Opus. Закупка одного flagship-а не работает на бизнес-релевантных вертикалях.",
    "metaDescription.insights-how-to-read":
      "Гид для читателя серии Horde.Vision Insights — что это за серия, терминология (tier-1/2/3, pack, marker, rubric, stealth, frontier), хронологический порядок отчётов и как запросить reproduction bundle.",
    "nav.aria.primary": "Основная навигация",
    "nav.aria.languageSelector": "Выбор языка",
    "nav.aria.switchEnglish": "Переключить на английский",
    "nav.aria.switchRussian": "Переключить на русский",
    "nav.aria.switchKazakh": "Переключить на казахский",
    "nav.menu": "Меню",
    "nav.about": "О компании",
    "nav.services": "Возможности",
    "nav.platform": "Архитектура",
    "nav.insights": "Аналитика",
    "nav.contact": "Контакты",
    "hero.eyebrow": "Executive decision intelligence",
    "hero.title": "Ясность для решений, принимаемых под давлением.",
    "hero.text":
      "Horde.Vision помогает руководителям понимать, как геополитические сдвиги, регуляторное давление, supply disruption и эффекты второго порядка превращаются в операционные последствия.",
    "hero.cta": "Смотреть систему",
    "intro.label": "Компания",
    "intro.title": "Большинству организаций не не хватает информации. Им не хватает структурированного понимания последствий.",
    "intro.paragraph1":
      "Horde.Vision создан для команд, которым нужно видеть, как сигналы, зависимости и зоны уязвимости соединяются между собой до того, как disruption становится операционной проблемой.",
    "intro.paragraph2":
      "Horizon даёт decision surface; управляемые research и simulation workflows помогают leadership-командам проверять, что действительно важно дальше, до того как обязательства навяжет сама реальность.",
    "services.label": "Возможности",
    "services.title": "Ключевые возможности decision intelligence.",
    "services.geo.title": "Decision Intelligence Briefing",
    "services.geo.subtitle": "Executive framing under uncertainty",
    "services.geo.text":
      "Структурированная оценка геополитической, регуляторной, supply и операционной уязвимости для команд, принимающих high-consequence decisions.",
    "services.geo.alt": "Абстрактное изображение для обзора геополитических рисков",
    "services.monitoring.title": "Signal Monitoring and Grounded Research",
    "services.monitoring.subtitle": "Persistent signal watch and retrieval",
    "services.monitoring.text":
      "Непрерывный мониторинг сущностей, юрисдикций, коридоров и слабых сигналов с grounded evidence, который можно review, compare и escalate.",
    "services.monitoring.alt": "Абстрактное изображение для глубокого мониторинга",
    "services.scenario.title": "Scenario Response and Simulation",
    "services.scenario.subtitle": "Governed testing before commitment",
    "services.scenario.text":
      "Scenario workflows для исследования disruption, response paths и эффектов второго порядка до того, как действовать начнёт сама реальность.",
    "services.scenario.alt": "Абстрактное изображение для сценарного планирования",
    "platforms.label": "Система",
    "platforms.title": "Единая архитектура для decision intelligence и scenario response.",
    "platforms.text":
      "Horde.Vision соединяет decision surface для картирования последствий с управляемым simulation layer для проверки реакции в условиях неопределённости.",
    "platforms.fabric.title": "Horizon Decision Fabric",
    "platforms.fabric.subtitle": "Decision surface для последствий, уязвимости и действия",
    "platforms.fabric.text":
      "Horizon собирает сигналы, сущности, зависимости и зоны уязвимости в единую управляемую поверхность для leadership-review, мониторинга и действия.",
    "platforms.fabric.cta": "Смотреть Horizon",
    "platforms.fabric.alt": "Обзор Horizon Decision Fabric",
    "platforms.mesh.title": "Horizon Agent Horde",
    "platforms.mesh.subtitle": "Управляемое моделирование для стратегической реакции",
    "platforms.mesh.text":
      "Agent Horde помогает проверять правдоподобные институциональные, рыночные и операционные response paths до того, как leadership будет вынужден принимать обязательства.",
    "platforms.mesh.cta": "Смотреть Agent Horde",
    "platforms.mesh.alt": "Обзор Horizon Agent Horde",
    "platformPage.eyebrow": "Horizon Decision Surface",
    "platformPage.title": "Decision surface для сложной операционной среды.",
    "platformPage.text":
      "Horizon превращает разрозненные сущности, сигналы, зависимости и зоны уязвимости в структурированную аналитическую поверхность для мониторинга, расследования и действия.",
    "platformPage.ctaPrimary": "Смотреть ядро системы",
    "platformPage.ctaSecondary": "Смотреть применения",
    "platformPage.hero.alt": "Обзор Horizon Decision Fabric",
    "platformPage.foundation.label": "Ядро decision intelligence",
    "platformPage.foundation.title": "Управляемая аналитическая система, а не россыпь отчётов.",
    "platformPage.foundation.taxonomy.title": "Структурированная модель мира",
    "platformPage.foundation.taxonomy.subtitle":
      "Сущности, зависимости, топология, операционный контекст",
    "platformPage.foundation.taxonomy.text":
      "Horizon организует организации, активы, коридоры, продукты и события внутри управляемой модели мира, чтобы последствия можно было прослеживать, а не угадывать.",
    "platformPage.foundation.taxonomy.alt": "Топология объектов и структура связей",
    "platformPage.foundation.signals.title": "Grounding и распространение сигналов",
    "platformPage.foundation.signals.subtitle":
      "Приём, retrieval, маршрутизация, последствия",
    "platformPage.foundation.signals.text":
      "Сигналы привязываются к структурированным сущностям и зависимостям, а затем поднимаются там, где меняют уязвимость, релевантность и приоритет принятия решений.",
    "platformPage.foundation.signals.alt":
      "Распространение сигналов по связанным операционным зависимостям",
    "platformPage.foundation.ai.title": "AI-native analytical runtime",
    "platformPage.foundation.ai.subtitle":
      "Retrieval, synthesis, governed workflows",
    "platformPage.foundation.ai.text":
      "AI помогает расширять research, структурировать evidence, сравнивать паттерны и собирать рабочие гипотезы, сохраняя outputs reviewable, traceable и полезными для операционной работы.",
    "platformPage.foundation.ai.alt": "Аналитическая поверхность платформы с AI-поддержкой",
    "platformPage.cases.label": "Применения decision intelligence",
    "platformPage.cases.title":
      "Типовые задачи, которые Horizon помогает рамочно понять и перевести в действие.",
    "platformPage.cases.supply.title": "Supply Chain Exposure",
    "platformPage.cases.supply.subtitle":
      "Активы, маршруты, bottlenecks, propagation",
    "platformPage.cases.supply.text":
      "Прослеживайте, как перебои проходят через поставщиков, логистические коридоры, промышленные кластеры и стратегические узлы, чтобы увидеть концентрацию риска до того, как она станет сбоем поставок.",
    "platformPage.cases.supply.alt": "Кейс по цепочке поставок и сбоям",
    "platformPage.cases.governance.title": "Control and Governance Mapping",
    "platformPage.cases.governance.subtitle":
      "Цепочки владения, affiliates, decision authority",
    "platformPage.cases.governance.text":
      "Понимайте, кто чем управляет, через какие сущности, на каких слоях владения, влияния или делегированной ответственности, когда именно структура управления становится частью операционного риска.",
    "platformPage.cases.governance.alt": "Кейс по сложной структуре управления",
    "platformPage.cases.regulatory.title": "Regulatory and Sanctions Impact",
    "platformPage.cases.regulatory.subtitle":
      "Policy shifts, jurisdictional pressure, downstream consequence",
    "platformPage.cases.regulatory.text":
      "Связывайте регуляторные решения с юрисдикциями, сущностями, коридорами и производственным контуром, чтобы видеть, где правовые и политические изменения превращаются в операционные ограничения.",
    "platformPage.cases.regulatory.alt": "Кейс по регуляторному воздействию",
    "platformPage.story.label": "Рабочие представления",
    "platformPage.story.title": "Представления, которые переводят анализ от фрагментированного сигнала к операционному последствию.",
    "platformPage.caseMap.title": "Связанные карты",
    "platformPage.caseMap.subtitle": "Маршруты, узлы, уязвимость",
    "platformPage.caseMap.text":
      "Связанные карты собирают площадки, маршруты, поставщиков и зависимости в одном поле зрения, чтобы уязвимость была заметна ещё до сбоя.",
    "platformPage.caseMap.alt": "Связанная карта с операционными узлами и маршрутами",
    "platformPage.caseGraph.title": "Графы связей",
    "platformPage.caseGraph.subtitle": "Контроль, владение, влияние",
    "platformPage.caseGraph.text":
      "Графы связей показывают контуры контроля, владения, аффилированные структуры и косвенные связи, которые теряются в линейной отчётности.",
    "platformPage.caseGraph.alt": "Граф связей между сущностями и зависимостями",
    "platformPage.caseScenario.title": "Зоны воздействия",
    "platformPage.caseScenario.subtitle": "Очаги риска, перенос эффекта, близость",
    "platformPage.caseScenario.text":
      "Зоны воздействия показывают, где возникло нарушение, как оно накладывается на активы и логистику и где начинают проявляться эффекты второго порядка.",
    "platformPage.caseScenario.alt": "Карта зон воздействия с выделенными участками риска",
    "platformPage.caseDetailed.title": "Промышленная детализация",
    "platformPage.caseDetailed.subtitle": "Промышленные зоны, соседство, плотность",
    "platformPage.caseDetailed.text":
      "Детализированные промышленные виды помогают разбирать соседство площадок, зависимость от инфраструктуры и точки, где локальный сбой способен разойтись по кластеру активов.",
    "platformPage.caseDetailed.alt":
      "Детализированный вид промышленных зон с плотной концентрацией активов",
    "platformPage.summary.label": "Поверхность решений",
    "platformPage.summary.title":
      "Единая рабочая поверхность для сигналов, зависимостей и операционных последствий.",
    "platformPage.summary.text":
      "Horizon создан для команд, которым нужна structured decision intelligence, а не disconnected monitoring, статичная отчётность или unguided search.",
    "platformPage.summary.cta": "Запросить доступ",
    "agentMeshPage.eyebrow": "Horizon Agent Horde",
    "agentMeshPage.title":
      "Управляемое моделирование для проверки сценарной реакции и стратегических гипотез.",
    "agentMeshPage.text":
      "Agent Horde расширяет Horde.Vision мультиагентными simulation workflows, которые проверяют, как институты, компании, рынки и нарративы могут сдвинуться после ввода сценарного сигнала.",
    "agentMeshPage.ctaPrimary": "Смотреть simulation runtime",
    "agentMeshPage.ctaSecondary": "Смотреть применения",
    "agentMeshPage.hero.alt": "Обзор Horizon Agent Horde",
    "agentMeshPage.operation.label": "Governed simulation runtime",
    "agentMeshPage.operation.title":
      "Ввод сценария, параллельные миры и прослеживаемый review.",
    "agentMeshPage.operation.alt": "Слой моделирования Agent Horde",
    "agentMeshPage.operation.signal.title": "Ввод сигнала",
    "agentMeshPage.operation.signal.subtitle": "Decision questions as executable inputs",
    "agentMeshPage.operation.signal.text":
      "Каждый запуск начинается со структурированного сценарного запроса: регуляторный шаг, рыночный сбой, ограничение доступа, промышленное событие или нарративный сдвиг, который нужно проверить до принятия обязательств.",
    "agentMeshPage.operation.worlds.title": "Управляемое моделирование миров",
    "agentMeshPage.operation.worlds.subtitle":
      "Параллельные популяции, траектории реакции, контролируемая вариативность",
    "agentMeshPage.operation.worlds.text":
      "Несколько управляемых миров можно исследовать параллельно, чтобы разбирать конкурирующие институциональные, коммерческие и операционные реакции, а не просто предполагать их.",
    "agentMeshPage.operation.trace.title": "Прослеживаемый результат",
    "agentMeshPage.operation.trace.subtitle": "Артефакты, lineage, checkpoints review",
    "agentMeshPage.operation.trace.text":
      "Simulation runs сохраняют артефакты, reasoning lineage, этапы review и агрегированные outputs, чтобы альтернативные response paths можно было сравнивать, оспаривать и использовать повторно.",
    "agentMeshPage.cases.label": "Сценарные применения",
    "agentMeshPage.cases.title":
      "Типовые классы сценариев для проверки управляемой реакции.",
    "agentMeshPage.cases.supply.title": "Supply Shock and Escalation",
    "agentMeshPage.cases.supply.subtitle":
      "Площадки, коридоры, пути замещения, тайминг реакции",
    "agentMeshPage.cases.supply.text":
      "Проверяйте, как фирмы, регуляторы, логистические акторы и соседние операторы могут отреагировать, когда сигнал о сбое вводится в концентрированную цепочку поставок.",
    "agentMeshPage.cases.supply.alt": "Кейс моделирования supply shock",
    "agentMeshPage.cases.regulatory.title": "Regulatory Move and Response",
    "agentMeshPage.cases.regulatory.subtitle":
      "Юрисдикции, контуры enforcement, downstream-адаптация",
    "agentMeshPage.cases.regulatory.text":
      "Моделируйте, как правовые, административные и рыночные участники могут отреагировать, когда в систему входит изменение правил, санкционный сигнал или лицензионное ограничение.",
    "agentMeshPage.cases.regulatory.alt": "Кейс моделирования регуляторной реакции",
    "agentMeshPage.cases.narrative.title": "Narrative Cascade and Alignment",
    "agentMeshPage.cases.narrative.subtitle":
      "Институциональные сообщения, восприятие рынком, вторичная координация",
    "agentMeshPage.cases.narrative.text":
      "Наблюдайте, как нарративные акторы, институты и коммерческие структуры могут выравниваться, расходиться или усиливать друг друга после выхода сценарного сигнала в публичное пространство.",
    "agentMeshPage.cases.narrative.alt": "Кейс моделирования нарративного каскада",
    "agentMeshPage.summary.label": "Слой моделирования",
    "agentMeshPage.summary.title":
      "Управляемая среда для проверки реакции до того, как реальность навяжет обязательства.",
    "agentMeshPage.summary.text":
      "Agent Horde помогает командам сравнивать правдоподобные траектории реакции, не сводя сложные институты и рынки к одному прогнозу.",
    "agentMeshPage.summary.cta": "Запросить доступ",
    "insightsPage.eyebrow": "Открытая лаборатория · Field Reports",
    "insightsPage.title": "Лучшей LLM не существует. Лучшая модель — для роли в вертикали.",
    "insightsPage.lead":
      "Воспроизводимые бенчмарки и field notes из работающей multi-agent системы decision intelligence. Не leaderboard. Не синтетические тесты. Реальные индустриальные reasoning-паки, реальные контракты ролей, опубликованные артефакты.",
    "insightsPage.chip.cadence": "Каденция",
    "insightsPage.chip.cadenceValue": "Ежеквартально + ad-hoc",
    "insightsPage.chip.scope": "Охват",
    "insightsPage.chip.scopeValue": "Роль × Вертикаль × Multi-agent",
    "insightsPage.chip.published": "Опубликовано",
    "insightsPage.intro.label": "Зачем этот раздел",
    "insightsPage.intro.title":
      "Публичные бенчмарки отвечают «какая модель умнее». Мы отвечаем «какая модель в какой роли на какой вертикали и где она ломается».",
    "insightsPage.intro.p1":
      "Каждый отчёт здесь — это короткий полевой тест. Мы берём реальный индустриальный сценарий, расписываем его обычным prompt-ом плюс rubric-ом того, что глубокий ответ должен называть, и прогоняем тот же prompt на шортлисте платных и бесплатных LLM. Затем публикуем оценки, стоимость на вызов, сырые ответы и заметку о том, что сломалось. Никаких синтетических бенчмарков, никаких leaderboard-ов, никаких «нам кажется» — только «данные показывают / данные не показывают».",
    "insightsPage.intro.p2":
      "Новый читатель? Начните с «Как читать эти отчёты» — трёхминутный гид с глоссарием (tier-1/2/3, pack, marker, rubric, stealth, frontier), порядком, в котором написаны отчёты, и тем, как запросить reproduction bundle. Всё, что ниже, исходит из этого общего словаря.",
    "insightsPage.intro.howToReadLink": "Как читать эти отчёты",
    "insightsPage.card.industrialDepthElephant.category":
      "Отчёт #2 · Индустриальная глубина на free-tier",
    "insightsPage.card.industrialDepthElephant.title":
      "Индустриальная глубина на free-tier: 30 вызовов — и день держит одна модель.",
    "insightsPage.card.industrialDepthElephant.lead":
      "15 free-моделей OpenRouter × 4 индустриальных пака = 60 ячеек. Успешно отработали 17. После ~30 вызовов срабатывает глобальная дневная квота — 14 из 15 моделей уходят в 429 до конца дня. Дальше мы прогнали выжившего — openrouter/elephant-alpha, под брендом провайдера Stealth — на 12 индустриальных вертикалях. 32 минуты, $0.",
    "insightsPage.card.industrialDepthElephant.metaDate": "2026 · апрель",
    "insightsPage.card.industrialDepthElephant.metaVertical": "Вертикалей: 12",
    "insightsPage.card.industrialDepthElephant.metaRole": "Open-ended проза",
    "insightsPage.card.industrialDepthElephant.metaModels": "15 моделей в попытке",
    "insightsPage.card.frontierVsElephant.category":
      "Отчёт #3 · Потолок frontier",
    "insightsPage.card.frontierVsElephant.title":
      "$1.20 vs $0. Пять платных flagship-моделей против одного бесплатного elephant на индустриальной глубине.",
    "insightsPage.card.frontierVsElephant.lead":
      "Шесть моделей, два индустриальных пака, $1.20 в сумме. Лидерство переключается по вертикали (glm-5.1 на стали, gpt-5.4 на удобрениях). Opus съедает 83% бюджета и не лидирует ни на одном паке. И даже объединение всех 5 платных flagships покрывает только 41% steel rubric и 24% fertilizer rubric — ни одна модель не владеет вертикалью.",
    "insightsPage.card.frontierVsElephant.metaDate": "2026 · апрель",
    "insightsPage.card.frontierVsElephant.metaVertical":
      "Вертикалей: 2 (сталь, удобрения)",
    "insightsPage.card.frontierVsElephant.metaRole": "Open-ended проза",
    "insightsPage.card.frontierVsElephant.metaModels":
      "6 моделей · $1.20 в сумме",
    "insightsPage.card.threeVerticals.category":
      "Отчёт #4 · Три узнаваемые вертикали",
    "insightsPage.card.threeVerticals.title":
      "FMCG, банки, фарма. Те же шесть моделей, три разных лидера. Opus — первый на FMCG и последний на фарме.",
    "insightsPage.card.threeVerticals.lead":
      "Три индустрии, в которых вы реально живёте: FMCG-закупки под EU PPWR, европейский банк под Basel III endgame, top-15 фарма с коммерческим планом 2027 под Medicare price negotiation. Те же шесть моделей, $1.58 в сумме. Три разных лидера. Бесплатный elephant занимает третье место на фарме за $0, обогнав GPT-5.4 и Opus. Opus — последний на фарме за $0.41.",
    "insightsPage.card.threeVerticals.metaDate": "2026 · апрель",
    "insightsPage.card.threeVerticals.metaVertical":
      "Вертикалей: 3 (FMCG, банки, фарма)",
    "insightsPage.card.threeVerticals.metaRole": "Open-ended проза",
    "insightsPage.card.threeVerticals.metaModels":
      "6 моделей · $1.58 в сумме",
    "insightsPage.card.elephantAlias.category": "Отчёт #1 · Происхождение модели",
    "insightsPage.card.elephantAlias.title":
      "«Stealth»-модель на OpenRouter — это routing alias над несколькими бэкендами.",
    "insightsPage.card.elephantAlias.lead":
      "Семь прогонов одного и того же prompt, один stealth alias. Latency расходится в 15× (2.0с → 30.3с). Composition меняется между stop, conditional и go. Два структурно разных стиля вывода. И более крупный сюжет: на frontier-масштабе модели расходятся в суждениях, а не сходятся.",
    "insightsPage.card.elephantAlias.metaDate": "2026 · апрель",
    "insightsPage.card.elephantAlias.metaVertical": "Вертикаль: полупроводники",
    "insightsPage.card.elephantAlias.metaRole": "Роль: critic",
    "insightsPage.card.elephantAlias.metaModels": "11 моделей",
    "insightsPage.card.cta": "Читать отчёт →",
    "insightsPage.empty":
      "Каждый квартал здесь будут появляться новые отчёты. Следующее в очереди: head-to-head по contract-fidelity в роли критика на том же пуле моделей, затем explorer-роль на трёх новых вертикалях. Что означают эти термины — см. в гиде читателя.",
    "preamble.label.about": "О чём этот отчёт",
    "preamble.label.read": "Как его читать",
    "preamble.speed.fast": "2 мин",
    "preamble.speed.mid": "10 мин",
    "preamble.speed.full": "30 мин",
    "preamble.footer":
      "Новый читатель? Начните с «Как читать эти отчёты» — трёхминутный гид с глоссарием терминов серии (tier-1/2/3, pack, marker, rubric, stealth, frontier) и хронологическим порядком отчётов.",
    "howToRead.eyebrow": "Гид читателя · Начать здесь",
    "howToRead.title": "Как читать эти отчёты.",
    "howToRead.lead":
      "Эта страница — трёхминутный primer по серии Horde.Vision Insights. Она объясняет, что мы публикуем, какой используем словарь, в каком порядке писались отчёты и как получить сырые артефакты, если вы хотите что-то воспроизвести. Прочтите её один раз — и вся дальнейшая серия станет гораздо легче для навигации.",
    "howToRead.series.heading": "Что это за серия",
    "howToRead.series.p1":
      "Horde.Vision — компания в области decision intelligence. Мы строим multi-agent операционный слой для институционального и индустриального reasoning — работу, где один вызов LLM никогда не является полным ответом, и где «какая модель лучше?» — неправильный вопрос. Правильный вопрос: «какая модель, в какой роли, на какой вертикали и где она ломается?»",
    "howToRead.series.p2":
      "Эти отчёты выходят из того же стенда, который мы используем внутренне для оценки моделей, идущих в production. Когда оценка даёт наблюдение, которое, как нам кажется, заинтересует операторов за пределами нашей команды — мы оформляем его и публикуем здесь, вместе со scoring rubric, цифрами стоимости, сырыми ответами и инструкциями по воспроизведению. Никаких синтетических бенчмарков. Никаких leaderboard-ов. Никаких «модель X набрала 87%». Реальные индустриальные сценарии, реальные cost ledger-ы, реальные failure modes.",
    "howToRead.series.p3":
      "Целевой читатель — оператор: глава стратегии, CIO, закупки, chief of staff, директор по research. Тот, кому в ближайшем квартале придётся решать, какие модели ставить в свои workflow, и кто не может опираться на заголовочные benchmark-оценки. Отчёты написаны под такого читателя.",
    "howToRead.speeds.heading": "Как читать отчёт на трёх скоростях",
    "howToRead.speeds.intro":
      "Каждый отчёт построен так, чтобы заголовок можно было вытащить за две минуты, а всю методологию — за тридцать. Выбирайте скорость заранее.",
    "howToRead.speeds.two.time": "2 минуты",
    "howToRead.speeds.two.title": "Заголовок",
    "howToRead.speeds.two.body":
      "Прочитайте hero-блок вверху страницы (eyebrow, заголовок, lead) и TL;DR прямо под ним. Это весь аргумент отчёта в bullet-форме. Если TL;DR вас не удивил — можно остановиться здесь, тело отчёта — это доказательства, а не тезис.",
    "howToRead.speeds.ten.time": "10 минут",
    "howToRead.speeds.ten.title": "Находки",
    "howToRead.speeds.ten.body":
      "Добавьте секции «Finding 1 / 2 / 3» и графики. Они показывают, как мы получили каждый bullet в TL;DR — rank-таблицы, cost ledger, расчёт coverage ceiling. На этой скорости вы должны уйти и с заголовком, и с доказательствами под него.",
    "howToRead.speeds.thirty.time": "30 минут",
    "howToRead.speeds.thirty.title": "Методология",
    "howToRead.speeds.thirty.body":
      "Добавьте секции Methodology, Reproduction и Artifacts внизу. Они точно говорят, как был сконфигурирован прогон, что мы измеряли и не измеряли, и как получить reproduction bundle, если вы хотите прогнать ту же матрицу на своих prompt-ах и своём шортлисте моделей.",
    "howToRead.terms.heading": "Терминология",
    "howToRead.terms.intro":
      "Двенадцать терминов всплывают по всей серии. Если какой-то из них в отчёте вам не знаком — возвращайтесь сюда.",
    "howToRead.terms.pack.term": "Pack",
    "howToRead.terms.pack.def":
      "Самодостаточный индустриальный сценарий, который мы отправляем модели. Обычно это трёхабзацная вводная (вымышленная компания, реальный регуляторный и рыночный контекст, решение на 2027 год, к которому нужно подготовиться) плюс шесть нарастающих вопросов обычной прозой. Никакой схемы, никакого system prompt, никаких инструментов. Каждый pack лежит в папке с prompt.md и markers.yaml.",
    "howToRead.terms.rubric.term": "Rubric",
    "howToRead.terms.rubric.def":
      "Полный набор того, что «глубокий» ответ должен назвать или количественно оценить — имена компаний, номера нормативных актов, даты, цены, площадки, фамилии. Пишется до прогона, никогда не показывается модели, хранится как markers.yaml рядом с pack-ом.",
    "howToRead.terms.marker.term": "Marker",
    "howToRead.terms.marker.def":
      "Один пункт в rubric-е — например «Kalundborg», «72.5%», «Article 22», «Wezlana». Scoring — консервативный case-insensitive substring match: marker либо присутствует в ответе, либо нет.",
    "howToRead.terms.tiers.term": "Tier 1 / Tier 2 / Tier 3",
    "howToRead.terms.tiers.def":
      "Уровни сложности внутри rubric-а. Tier 1 — то, что должна знать любая крупная LLM с общим покрытием вертикали. Tier 2 — знания компетентного практика. Tier 3 — экспертное, свежее или событийное знание (регуляторные даты 2024–2026, конкретные бренды биосимиляров 2025, количественные диапазоны скидок, имена супервизоров). Tier 3 — это то место, где разрыв между моделями становится виден.",
    "howToRead.terms.harness.term": "Harness",
    "howToRead.terms.harness.def":
      "Python-скрипт, которым мы запускаем pack на списке моделей. Отправляет prompt, пишет wall-latency, фиксирует input/output tokens от провайдера, сохраняет сырой текст и сверяет его с rubric-ом. Сознательно скучный — никакого judging LLM, никакого субъективного scoring.",
    "howToRead.terms.frontier.term": "Frontier-модель",
    "howToRead.terms.frontier.def":
      "Актуальные top-tier платные flagship-и крупных лабораторий. В этой серии это claude-opus-4.7, gpt-5.4, gemini-3.1-pro-preview, glm-5.1 и kimi-k2.5. Пул меняется по мере выхода новых поколений.",
    "howToRead.terms.stealth.term": "Stealth-модель · Elephant-alpha",
    "howToRead.terms.stealth.def":
      "OpenRouter периодически запускает анонимные early-preview слоты под именами типа openrouter/elephant-alpha и буквальным provider-label «Stealth». Модель за алиасом сознательно не раскрывается. elephant-alpha — слот, который мы зондируем во всей этой серии; как показывает отчёт #1, это на самом деле routing alias над несколькими бэкендами, а не одна модель.",
    "howToRead.terms.roles.term":
      "Роль (explorer / critic / canonicalizer / supervisor)",
    "howToRead.terms.roles.def":
      "Роли внутри нашей multi-agent системы. Explorer пишет свободную прозу и перечисляет сущности; critic оценивает draft против жёсткого контракта; canonicalizer мапит находки на типы сущностей нашей системы; supervisor маршрутизирует работу между ролями. Большинство публикуемых отчётов фокусируются на fitness explorer-а или critic-а.",
    "howToRead.terms.contract.term": "Strict-JSON contract",
    "howToRead.terms.contract.def":
      "Жёсткая выходная схема, которой обязана следовать роль (например critic). Отчёты по contract-fidelity измеряют, как часто модель возвращает вывод, который одновременно валидный JSON и соответствует ожидаемым полям — это отдельная ось от глубины прозы.",
    "howToRead.terms.coverage.term": "Coverage ceiling",
    "howToRead.terms.coverage.def":
      "Объединение tier-3 markers, пойманных любой моделью из пула, поделённое на размер rubric-а. Полезная верхняя оценка: «соберите все эти модели вместе — и лучшее, что вы получите, это такая-то доля rubric-а». В наших отчётах пока это 32–48% на реальных индустриальных вертикалях — большая часть rubric-а не ловится никем.",
    "howToRead.terms.costmarker.term": "Cost-per-marker",
    "howToRead.terms.costmarker.def":
      "Общая сумма USD, потраченная на ответ модели, делённая на количество tier-3 markers, которые она поймала. Даёт apples-to-apples сравнение между моделями с очень разными прайс-листами. Разрыв в 53× между самой дешёвой и самой дорогой платной моделью на одном и том же prompt-е — не редкость.",
    "howToRead.terms.inversion.term": "Leadership inversion",
    "howToRead.terms.inversion.def":
      "Когда одна и та же модель на одной вертикали — первая, а на другой — последняя, при том же формате prompt-а и том же бюджете. Ключевая находка отчёта #4: три из шести моделей пула делают это всего на трёх вертикалях. Одной «лучшей модели» через бизнес-релевантные индустрии не существует.",
    "howToRead.arc.heading": "Отчёты, в порядке написания",
    "howToRead.arc.intro":
      "Каждый отчёт серии самодостаточен, но они писались в этом порядке и каждый слегка опирается на предыдущий. Если читать по порядку, arc такой: что это за бесплатная модель? → держится ли free-tier вообще? → бьют ли её платные flagships? → работает ли это хоть как-то на вертикалях, в которых вы реально живёте?",
    "howToRead.arc.r1.title":
      "Stealth-модель — это routing alias. И на frontier-масштабе модели расходятся.",
    "howToRead.arc.r1.body":
      "Одиннадцать моделей на одном и том же semiconductor / CHIPS Act session pack, под strict-JSON critic contract. Две находки: elephant-alpha — это routing alias над несколькими бэкендами, а не одна модель; и на frontier-масштабе модели расходятся в суждениях, а не сходятся к consensus truth.",
    "howToRead.arc.r1.link": "Читать #1 →",
    "howToRead.arc.r2.title":
      "Индустриальная глубина на free-tier: 30 вызовов — и день держит одна модель.",
    "howToRead.arc.r2.body":
      "Пятнадцать бесплатных моделей OpenRouter против четырёх индустриальных пакетов. После примерно тридцати вызовов срабатывает глобальная дневная квота и четырнадцать из пятнадцати уходят в 429. Единственного выжившего — elephant-alpha — мы расширяем на двенадцать вертикалей. Field-отчёт о том, что можно вытащить из free-tier, когда всё остальное ломается.",
    "howToRead.arc.r2.link": "Читать #2 →",
    "howToRead.arc.r3.title":
      "$1.20 vs $0 — платный frontier против бесплатного elephant на индустриальной глубине.",
    "howToRead.arc.r3.body":
      "Шесть моделей, два тяжёлых индустриальных пака (сталь и CBAM, удобрения и ag inputs), $1.20 в сумме. Лидер меняется по вертикали. Opus сжигает 83% бюджета и не лидирует ни на одном паке. Объединение всех пяти платных frontier покрывает только 41% steel rubric и 24% fertilizer rubric — ни одна модель не владеет вертикалью.",
    "howToRead.arc.r3.link": "Читать #3 →",
    "howToRead.arc.r4.title":
      "FMCG, банки, фарма. Те же шесть моделей, три разных лидера.",
    "howToRead.arc.r4.body":
      "Три индустрии, в которых вы реально живёте: FMCG-закупки под EU PPWR, европейский банк под Basel III endgame, top-15 фарма с коммерческим планом 2027 под IRA. Opus — первый на FMCG и последний на фарме. Бесплатный elephant занимает третье место на фарме за $0, обогнав GPT-5.4 и Opus. Это и есть leadership inversion на трёх узнаваемых вертикалях.",
    "howToRead.arc.r4.link": "Читать #4 →",
    "howToRead.arc.next":
      "Следующее в очереди: head-to-head по contract-fidelity в роли критика на том же пуле моделей, затем explorer-роль на трёх новых вертикалях.",
    "howToRead.artifacts.heading": "Артефакты и воспроизведение",
    "howToRead.artifacts.p1":
      "Каждый отчёт заканчивается блоком Reproduction, показывающим точную команду, которую мы запускали. Паки, rubric-и, harness и сырые JSONL живут в приватном research-репо, который мы не публикуем открыто — частично потому что они меняются каждую неделю с выходом новых моделей, частично потому что там структуры pricing и ranking-choice, которые по сути являются продуктом.",
    "howToRead.artifacts.p2": "Мы рады делиться ими по запросу. Два формата:",
    "howToRead.artifacts.li1":
      "Reproduction bundle — tarball с prompt.md и markers.yaml для каждого pack-а, упомянутого в конкретном отчёте, сырой JSONL с прогона, скрипт harness и однострочная команда. Достаточно, чтобы прогнать ту же матрицу самостоятельно. Бесплатно, отправляется по email на настоящий операторский адрес.",
    "howToRead.artifacts.li2":
      "Custom run — мы проектируем паки под ВАШИ вертикали, направляем harness на ВАШ шортлист моделей (включая то, что нельзя легко протестировать публично — внутренние fine-tune, VPC endpoint-ы, rate-limited preview), и отдаём полный rubric, графики, cost ledger и интерпретационную записку. Это платный проект; типичный прогон — неделя.",
    "howToRead.artifacts.p3":
      "Напишите нам на hello@horde.vision и скажите, какой отчёт вы читали и что хотите с ним сделать — этого обычно достаточно, чтобы мы поняли, что отправлять в ответ.",
    "howToRead.conventions.heading": "Соглашения",
    "howToRead.conventions.li1":
      "Single shot per cell, если отчёт явно не помечен как variance-probe. Один прогон (модель × pack) — одна JSONL-строка. В методологии мы отдельно отмечаем, когда повторяем.",
    "howToRead.conventions.li2":
      "Открытая проза, если отчёт явно не помечен как strict-JSON. Большинство отчётов — свободная форма; отчёты по contract-fidelity используют отдельную ось.",
    "howToRead.conventions.li3":
      "Цифры стоимости фиксируются в рантайме. Мы записываем точные input_tokens и output_tokens, возвращённые провайдером, и умножаем на list-цену на дату публикации. Показываем ledger — чтобы вы могли пересчитать со своим pricing.",
    "howToRead.conventions.li4":
      "Scoring rubric-а консервативен. Только case-insensitive substring match — никакого fuzzy-матча, никакого synonym expansion, никакого LLM-судьи. False negatives из-за этого слегка раздуты, false positives почти не бывает. Мы отмечаем этот trade-off там, где он важен.",
    "howToRead.conventions.li5":
      "Мы не прячем fail-прогоны. Каждый FAIL или empty_content в опубликованной таблице — это реальный исход. Часть самых интересных находок — именно про failure modes.",
    "howToRead.cta.title":
      "Теперь вы готовы читать любой отчёт серии.",
    "howToRead.cta.text":
      "Перейдите в индекс Insights и выберите отчёт — или прыгните сразу к #4, если хотите самый свежий полевой тест, к #1, если хотите origin методологии, или к самой релевантной для вас вертикали.",
    "howToRead.cta.button": "Все отчёты",
    "elephantAlias.preamble.about":
      "Отчёт #1 нашей серии Horde.Vision Insights — короткие полевые отчёты, в которых мы прогоняем один и тот же prompt harness на платных и бесплатных LLM на реальных индустриальных сценариях и публикуем rubric-оценки, стоимость и поведенческие особенности моделей. В этом отчёте мы семь раз прогоняем один и тот же prompt на анонимном stealth-слоте OpenRouter, а затем против десяти именованных frontier и fast flagships. Две находки: stealth-модель — это не всегда модель (это может быть routing alias над несколькими бэкендами), и на frontier-масштабе модели расходятся в суждениях, а не сходятся к одному ответу.",
    "elephantAlias.preamble.speed.fast": "hero + TL;DR",
    "elephantAlias.preamble.speed.mid":
      "+ Finding 1 (stealth alias) и Finding 2 (frontier divergence)",
    "elephantAlias.preamble.speed.full":
      "+ методология, воспроизведение, bundle по запросу",
    "industrialDepthElephant.preamble.about":
      "Отчёт #2 нашей серии Horde.Vision Insights. Мы прогнали пятнадцать бесплатных моделей на OpenRouter против четырёх индустриальных пакетов (батарея, фарма biologics, ядерная энергетика, аэрокосмос). После примерно тридцати вызовов срабатывает глобальная дневная квота и четырнадцать из пятнадцати уходят в 429 — одна выжила, поэтому мы расширили её на двенадцать вертикалей. Это полевой отчёт о том, что на самом деле даёт free-tier, где он ломается и где один stealth-слот без бренда держит линию.",
    "industrialDepthElephant.preamble.speed.fast": "hero + TL;DR",
    "industrialDepthElephant.preamble.speed.mid":
      "+ график обрыва, таблица по 12 вертикалям, variance probe",
    "industrialDepthElephant.preamble.speed.full":
      "+ методология, воспроизведение, bundle по запросу",
    "frontierVsElephant.preamble.about":
      "Отчёт #3 нашей серии Horde.Vision Insights. Шесть моделей — Claude Opus 4.7, GPT-5.4, Gemini 3.1 Pro, GLM-5.1, Kimi K2.5 и бесплатный stealth-слот elephant-alpha — прогоняют один и тот же prompt harness против двух тяжёлых индустриальных пакетов (европейская сталь под CBAM и удобрения / ag-inputs под санкциями и экспортным контролем). Общий счёт за платные модели: $1.20. Основная находка: ни одна модель не лидирует на обеих вертикалях, и платные flagships не доминируют над бесплатным слотом на глубине знаний.",
    "frontierVsElephant.preamble.speed.fast": "hero + TL;DR",
    "frontierVsElephant.preamble.speed.mid":
      "+ график tier-3, cost ledger, ранжирование по вертикалям",
    "frontierVsElephant.preamble.speed.full":
      "+ методология, воспроизведение, bundle по запросу",
    "threeVerticals.preamble.about":
      "Отчёт #4 нашей серии Horde.Vision Insights. Те же шесть моделей, что и в предыдущем отчёте — Claude Opus 4.7, GPT-5.4, Gemini 3.1 Pro, GLM-5.1, Kimi K2.5 и бесплатный stealth-слот elephant-alpha — на трёх индустриях, в которых вы реально живёте: FMCG-закупки под EU PPWR, европейский банк под Basel III endgame и top-15 фарма с коммерческим планом 2027 под IRA. Ключевая находка: три разных лидера на трёх вертикалях. Opus — первый на FMCG и последний на фарме. Бесплатный слот занимает третье место на фарме за $0. Ни одна модель не владеет полем.",
    "threeVerticals.preamble.speed.fast": "hero + TL;DR",
    "threeVerticals.preamble.speed.mid":
      "+ таблица rank inversion, coverage ceiling, разброс cost-per-marker",
    "threeVerticals.preamble.speed.full":
      "+ методология, воспроизведение, bundle по запросу",
    "elephantAlias.eyebrow": "Field Report · Происхождение модели · 2026-04-18",
    "elephantAlias.title":
      "Stealth-модель — это routing alias. И на frontier-масштабе модели расходятся.",
    "elephantAlias.lead":
      "Одиннадцать моделей на одном и том же session pack по TSMC / ASML / CHIPS Act, под strict-JSON контрактом критика. Две находки уровня публикации — обе против стандартного нарратива «scale → consensus → truth» — выросли из одного дня прогонов.",
    "elephantAlias.back": "← Назад в Аналитику",
    "elephantAlias.cta.title": "Тот же стенд, ваша вертикаль.",
    "elephantAlias.cta.text":
      "Если вы работаете в полупроводниках, энергетике, морской логистике, литии или REE supply chains и решаете, какую модель ставить в роль критика на собственном пайплайне — мы можем прогнать тот же стенд против ваших prompt'ов. Воспроизводимость — это и есть продукт.",
    "elephantAlias.cta.button": "Запросить кастомный прогон",
    "industrialDepthElephant.eyebrow":
      "Field Report · Индустриальная глубина · 2026-04-19",
    "industrialDepthElephant.title":
      "Индустриальная глубина на free-tier: 30 вызовов — и день держит одна модель.",
    "industrialDepthElephant.lead":
      "Пятнадцать free-моделей OpenRouter, четыре индустриальных вертикали, открытые prose-ответы. Большинство моделей выпадают после срабатывания дневной free-квоты. Дальше мы прогнали единственного выжившего — openrouter/elephant-alpha, под буквальным брендом провайдера Stealth — на двенадцати индустриальных вертикалях. Вот что мы увидели.",
    "industrialDepthElephant.back": "← Назад в Аналитику",
    "industrialDepthElephant.cta.title": "Тот же стенд, ваша вертикаль.",
    "industrialDepthElephant.cta.text":
      "Если вы работаете в одной из вертикалей выше — или в смежной — и оцениваете free-tier или stealth-aliased модели для первого прохода по индустриальной разметке, мы можем прогнать тот же стенд против ваших prompt'ов и на вашем пуле моделей. Воспроизводимость — это и есть продукт.",
    "industrialDepthElephant.cta.button": "Запросить кастомный прогон",
    "frontierVsElephant.eyebrow":
      "Field Report · Потолок frontier · 2026-04-19",
    "frontierVsElephant.title":
      "$1.20 vs $0. Frontier-flagships против бесплатной stealth-модели на индустриальной глубине.",
    "frontierVsElephant.lead":
      "Шесть моделей на одних и тех же двух индустриальных паках: openrouter/elephant-alpha (бесплатно, провайдер Stealth) против пяти платных flagship — claude-opus-4.7, gpt-5.4, gemini-3.1-pro, glm-5.1, kimi-k2.5. Суммарные расходы: $1.20. Wall time: 23 минуты. Три находки уровня публикации — и одна benchmarking-ловушка, которая чуть не убила два из пяти платных моделей.",
    "frontierVsElephant.back": "← Назад в Аналитику",
    "frontierVsElephant.cta.title":
      "Тот же стенд, ваша вертикаль, ваш пул моделей.",
    "frontierVsElephant.cta.text":
      "Если вы выбираете между ставкой на один flagship и multi-model стратегией в своей операционной вертикали — мы можем прогнать ту же матрицу против ваших prompt'ов на вашем shortlist. Потолок 41% / 24% из этого отчёта — это стартовая точка; у вашей вертикали будет своя.",
    "frontierVsElephant.cta.button": "Запросить кастомный прогон",
    "threeVerticals.eyebrow":
      "Field Report · Три вертикали, в которых вы реально живёте · 2026-04-19",
    "threeVerticals.title":
      "Три индустрии, те же шесть моделей, три разных лидера. Opus — первый на FMCG и последний на фарме.",
    "threeVerticals.lead":
      "FMCG-закупки под EU PPWR. Европейский универсальный банк перед Basel III endgame. Top-15 фарма с коммерческим планом 2027 под Medicare price negotiation. Те же шесть моделей, тот же стенд, тот же бюджет в 8 192 токена. Лидер меняется на каждой вертикали. claude-opus-4.7 побеждает FMCG и финиширует последним на фарме. Бесплатный openrouter/elephant-alpha занимает третье место на фарме за $0, обгоняя GPT-5.4 и Opus. Это и есть «нет лучшего LLM, есть лучшая модель для роли в вертикали» в трёх узнаваемых индустриях.",
    "threeVerticals.back": "← Назад в Аналитику",
    "threeVerticals.cta.title":
      "Ваша вертикаль, ваш пул моделей, ваши prompt'ы.",
    "threeVerticals.cta.text":
      "Если вы поднимаете внутреннюю AI-программу и решаете, какую модель ставить против вашей реальной операционной реальности — на вертикалях, в которых вы реально живёте — мы можем прогнать ту же матрицу против ваших prompt'ов на вашем shortlist. 78% доли Opus в costs, инверсия рангов на фарме, разрыв 53× по cost-per-marker — каждое из этого стартовая точка. У вашей вертикали будут свои.",
    "threeVerticals.cta.button": "Запросить кастомный прогон",
    "footer.label": "Horde.Vision",
    "footer.title": "Для leadership в условиях неопределённости.",
    "footer.note": "Structured decision intelligence, grounded research и governed simulation для сложной операционной среды.",
    "footer.location.title": "Адрес",
    "footer.location.line1": "Dostyk, 16",
    "footer.location.line2": "Astana, Kazakhstan",
    "footer.contact.title": "Контакты",
    "footer.contact.line1": "hello@horde.vision",
    "footer.availability.title": "Доступность",
    "footer.availability.line1": "Встречи по запросу",
    "footer.availability.line2": "Дополнительные детали будут добавлены позже",
    "footer.legal.title": "Правовая информация",
    "footer.legal.privacy": "Уведомление о конфиденциальности",
    "footer.legal.terms": "Условия использования",
    "footer.legal.technical": "Техническое раскрытие",
  },
  kz: {
    locale: "kk",
    pageTitle: "Horde.Vision",
    "pageTitle.platform": "Horizon Decision Fabric | Horde.Vision",
    "pageTitle.agent-mesh": "Horizon Agent Horde | Horde.Vision",
    "pageTitle.insights": "Талдау | Horde.Vision",
    "pageTitle.insights-elephant-routing-alias-2026-04":
      "Elephant Alpha — routing alias | Horde.Vision Талдауы",
    "pageTitle.insights-industrial-depth-elephant-2026-04":
      "Free-tier-дегі индустриалды тереңдік | Horde.Vision Талдауы",
    "pageTitle.insights-frontier-vs-elephant-2026-04":
      "$1.20 vs $0: ақылы frontier бесплатный elephant-қа қарсы | Horde.Vision Талдауы",
    "pageTitle.insights-three-verticals-2026-04":
      "FMCG, банктер, фарма: сол алты модель, үш әртүрлі көшбасшы | Horde.Vision Талдауы",
    "pageTitle.insights-how-to-read":
      "Осы есептерді қалай оқу керек | Horde.Vision Талдауы",
    metaDescription:
      "Horde.Vision — күрделі операциялық ортаға арналған AI-native decision intelligence компаниясы.",
    "metaDescription.platform":
      "Horizon Decision Fabric Horde.Vision аясында бытыраңқы нысандарды, сигналдарды, тәуелділіктерді және осалдықты жұмыс decision surface-іне біріктіреді.",
    "metaDescription.agent-mesh":
      "Horizon Agent Horde Horde.Vision аясында институционалдық, нарықтық және операциялық реакцияны тексеруге арналған governed simulations жүргізеді.",
    "metaDescription.insights":
      "Horde.Vision-нің ашық зертханасы: frontier және stealth модельдердің нақты индустриалды reasoning-рөлдерінде қалай жұмыс істейтіні туралы қайталанатын бенчмарктер мен field notes.",
    "metaDescription.insights-elephant-routing-alias-2026-04":
      "OpenRouter-дегі stealth-модель іс жүзінде бірнеше бэкендтің үстіндегі routing alias болып шықты. Frontier-масштабта модельдер пікірде жинақталмайды — алшақтайды.",
    "metaDescription.insights-industrial-depth-elephant-2026-04":
      "OpenRouter-тің 15 free-моделін 4 индустриалды вертикальда қостық. Күндік квота іске қосылған соң ~30 шақырудан кейін көпшілігі шығып қалады. Кейін жалғыз тірі қалғанды — провайдер 'Stealth' брендімен openrouter/elephant-alpha — 12 индустриалды вертикальда қостық. $0-ге құн, латенттілік және тереңдік туралы field report.",
    "metaDescription.insights-frontier-vs-elephant-2026-04":
      "Алты модель екі индустриалды пакетте, барлығы $1.20. Көшбасшылық вертикаль бойынша ауысады: glm-5.1 болатта, gpt-5.4 тыңайтқыштарда жетекші. Opus бюджеттің 83%-ын жейді әрі ешбір пакетте жетекшіл бола алмайды. Тіпті бес ақылы flagship біріккен жауаптары steel rubric-тің 41%-ын және fertilizer rubric-тің 24%-ын ғана қамтиды — ешбір модель вертикальды толық иеленбейді.",
    "metaDescription.insights-three-verticals-2026-04":
      "Алты модель сіз шынайы өмір сүретін үш индустрияда: FMCG, Basel III endgame алдындағы еуропалық банк, US commercial pharma 2026. Үш әртүрлі көшбасшы. Opus FMCG-де бірінші, фармада соңғы. Бесплатный elephant фармада үшінші орынды алып, GPT-5.4 және Opus-ты артта қалдырады. Жалғыз flagship-ке ставка бизнес-релевантты вертикальдарда жұмыс істемейді.",
    "metaDescription.insights-how-to-read":
      "Horde.Vision Insights сериясына арналған оқырман гиді — серия не, терминология (tier-1/2/3, pack, marker, rubric, stealth, frontier), есептердің хронологиялық реті және reproduction bundle-ды қалай сұрау керектігі.",
    "nav.aria.primary": "Негізгі навигация",
    "nav.aria.languageSelector": "Тілді таңдау",
    "nav.aria.switchEnglish": "Ағылшын тіліне ауыстыру",
    "nav.aria.switchRussian": "Орыс тіліне ауыстыру",
    "nav.aria.switchKazakh": "Қазақ тіліне ауыстыру",
    "nav.menu": "Мәзір",
    "nav.about": "Компания",
    "nav.services": "Мүмкіндіктер",
    "nav.platform": "Архитектура",
    "nav.insights": "Талдау",
    "nav.contact": "Байланыс",
    "hero.eyebrow": "Executive decision intelligence",
    "hero.title": "Қысым жағдайында қабылданатын шешімдерге арналған айқындық.",
    "hero.text":
      "Horde.Vision басшылық командаларға геосаяси ығысулар, реттеуші қысым, supply disruption және екінші реттік әсерлердің операциялық салдарға қалай айналатынын түсінуге көмектеседі.",
    "hero.cta": "Жүйені көру",
    "intro.label": "Компания",
    "intro.title": "Көп ұйымдарға ақпарат жетіспейді емес. Оларға салдарды құрылымдап түсіну жетіспейді.",
    "intro.paragraph1":
      "Horde.Vision disruption операциялық мәселеге айналмай тұрып сигналдар, тәуелділіктер және осалдық қалай байланысатынын көруі тиіс командаларға арналған.",
    "intro.paragraph2":
      "Horizon decision surface береді; governed research және simulation workflows leadership командаларына шынайы міндеттеме сырттан таңылғанға дейін келесі маңыздыны тексеруге көмектеседі.",
    "services.label": "Мүмкіндіктер",
    "services.title": "Decision intelligence-тің негізгі мүмкіндіктері.",
    "services.geo.title": "Decision Intelligence Briefing",
    "services.geo.subtitle": "Белгісіздік жағдайындағы executive framing",
    "services.geo.text":
      "High-consequence decisions қабылдайтын командаларға арналған геосаяси, реттеуші, supply және операциялық осалдықтың құрылымдалған бағасы.",
    "services.geo.alt": "Геосаяси тәуекелдер шолуына арналған абстрактілі сурет",
    "services.monitoring.title": "Signal Monitoring and Grounded Research",
    "services.monitoring.subtitle": "Тұрақты signal watch және retrieval",
    "services.monitoring.text":
      "Review, compare және escalate жасауға болатын grounded evidence-пен бірге нысандарды, юрисдикцияларды, дәліздерді және әлсіз сигналдарды үздіксіз бақылау.",
    "services.monitoring.alt": "Терең мониторингке арналған абстрактілі сурет",
    "services.scenario.title": "Scenario Response and Simulation",
    "services.scenario.subtitle": "Commitment алдында governed testing",
    "services.scenario.text":
      "Шынайы әрекет мәжбүрлемей тұрып disruption, response paths және екінші реттік әсерлерді зерттейтін scenario workflows.",
    "services.scenario.alt": "Сценарийлік жоспарлауға арналған абстрактілі сурет",
    "platforms.label": "Жүйе",
    "platforms.title": "Decision intelligence пен scenario response үшін біртұтас архитектура.",
    "platforms.text":
      "Horde.Vision consequence mapping жасайтын decision surface пен белгісіздік жағдайындағы реакцияны тексеретін governed simulation layer-ді біріктіреді.",
    "platforms.fabric.title": "Horizon Decision Fabric",
    "platforms.fabric.subtitle": "Салдар, осалдық және әрекетке арналған decision surface",
    "platforms.fabric.text":
      "Horizon сигналдарды, нысандарды, тәуелділіктерді және осалдықты leadership review, мониторинг және әрекетке арналған бір governed surface-ке жинайды.",
    "platforms.fabric.cta": "Horizon-ды көру",
    "platforms.fabric.alt": "Horizon Decision Fabric шолуы",
    "platforms.mesh.title": "Horizon Agent Horde",
    "platforms.mesh.subtitle": "Стратегиялық реакцияға арналған governed simulation",
    "platforms.mesh.text":
      "Agent Horde leadership нақты міндеттеме алуға мәжбүр болмай тұрып ықтимал институционалдық, нарықтық және операциялық response paths-ті тексереді.",
    "platforms.mesh.cta": "Agent Horde-ты көру",
    "platforms.mesh.alt": "Horizon Agent Horde шолуы",
    "platformPage.eyebrow": "Horizon Decision Surface",
    "platformPage.title": "Күрделі операциялық ортаға арналған decision surface.",
    "platformPage.text":
      "Horizon бытыраңқы нысандарды, сигналдарды, тәуелділіктерді және осалдықты мониторингке, зерттеуге және әрекетке арналған құрылымдалған аналитикалық бетке айналдырады.",
    "platformPage.ctaPrimary": "Жүйенің өзегін көру",
    "platformPage.ctaSecondary": "Қолданылуын көру",
    "platformPage.hero.alt": "Horizon Decision Fabric шолуы",
    "platformPage.foundation.label": "Decision intelligence өзегі",
    "platformPage.foundation.title": "Босаң есептер жиынтығы емес, governed analytical system.",
    "platformPage.foundation.taxonomy.title": "Құрылымдалған әлем моделі",
    "platformPage.foundation.taxonomy.subtitle":
      "Нысандар, тәуелділіктер, топология, операциялық контекст",
    "platformPage.foundation.taxonomy.text":
      "Horizon ұйымдарды, активтерді, дәліздерді, өнімдерді және оқиғаларды governed world model ішінде ұйымдастырады, сондықтан салдарды болжау емес, қадағалау мүмкін болады.",
    "platformPage.foundation.taxonomy.alt": "Объект топологиясы мен байланыстар құрылымы",
    "platformPage.foundation.signals.title": "Signal grounding және propagation",
    "platformPage.foundation.signals.subtitle":
      "Ingestion, retrieval, routing, consequence",
    "platformPage.foundation.signals.text":
      "Сигналдар құрылымдалған нысандар мен тәуелділіктерге бекітіледі, содан кейін осалдықты, релеванттықты және decision priority-ді өзгертетін жерде көтеріледі.",
    "platformPage.foundation.signals.alt":
      "Байланысқан операциялық тәуелділіктер арқылы сигналдардың таралуы",
    "platformPage.foundation.ai.title": "AI-native analytical runtime",
    "platformPage.foundation.ai.subtitle":
      "Retrieval, synthesis, governed workflows",
    "platformPage.foundation.ai.text":
      "AI research-ті кеңейтуге, evidence-ті құрылымдауға, паттерндерді салыстыруға және working hypotheses жинауға көмектеседі, ал outputs reviewable, traceable және операциялық пайдалы болып қалады.",
    "platformPage.foundation.ai.alt": "AI қолдауы бар платформаның аналитикалық беті",
    "platformPage.cases.label": "Decision intelligence қолданылуы",
    "platformPage.cases.title": "Horizon шеңберлеп түсіндіріп, әрекетке айналдыратын типтік міндеттер.",
    "platformPage.cases.supply.title": "Supply Chain Exposure",
    "platformPage.cases.supply.subtitle":
      "Активтер, маршруттар, bottlenecks, propagation",
    "platformPage.cases.supply.text":
      "Іркілістердің жеткізушілер, логистикалық дәліздер, өндірістік кластерлер және стратегиялық тораптар арқылы қалай өтетінін бақылап, тәуекел шоғырлануын жеткізу оқиғасына айналмай тұрып анықтаңыз.",
    "platformPage.cases.supply.alt": "Жеткізу тізбегі мен іркіліс кейсі",
    "platformPage.cases.governance.title": "Control and Governance Mapping",
    "platformPage.cases.governance.subtitle":
      "Меншік тізбектері, affiliates, decision authority",
    "platformPage.cases.governance.text":
      "Басқару құрылымының өзі операциялық тәуекелге айналғанда, кім нені, қай құрылым арқылы, меншік, ықпал немесе делегирленген өкілеттіктің қай қабатында басқаратынын анық көріңіз.",
    "platformPage.cases.governance.alt": "Күрделі басқару құрылымы кейсі",
    "platformPage.cases.regulatory.title": "Regulatory and Sanctions Impact",
    "platformPage.cases.regulatory.subtitle":
      "Policy shifts, jurisdictional pressure, downstream consequence",
    "platformPage.cases.regulatory.text":
      "Құқықтық немесе саяси өзгерістердің операциялық шектеуге қай жерде айналатынын көру үшін реттеуші шешімдерді юрисдикциялармен, субъектілермен, дәліздермен және өндірістік контурмен байланыстырыңыз.",
    "platformPage.cases.regulatory.alt": "Реттеуші әсер кейсі",
    "platformPage.story.label": "Жұмыс көріністері",
    "platformPage.story.title": "Талдауды бытыраңқы сигналдан операциялық салдарға жеткізетін көріністер.",
    "platformPage.caseMap.title": "Байланысқан карталар",
    "platformPage.caseMap.subtitle": "Маршруттар, түйіндер, осалдық",
    "platformPage.caseMap.text":
      "Байланысқан карталар нысандарды, маршруттарды, жеткізушілерді және тәуелділіктерді бір көрініске жинап, осалдықты үзіліс болғанға дейін көрсетеді.",
    "platformPage.caseMap.alt":
      "Операциялық түйіндер мен маршруттарды көрсететін байланысқан карта",
    "platformPage.caseGraph.title": "Байланыс графтары",
    "platformPage.caseGraph.subtitle": "Бақылау, меншік, ықпал",
    "platformPage.caseGraph.text":
      "Байланыс графтары бақылау тізбектерін, меншік байланыстарын, аффилирленген құрылымдарды және кәдімгі есепте көзден таса болатын жанама жақындықты көрсетеді.",
    "platformPage.caseGraph.alt": "Субъектілер мен тәуелділіктер арасындағы байланыс графы",
    "platformPage.caseScenario.title": "Әсер ету аймақтары",
    "platformPage.caseScenario.subtitle": "Тәуекел аймағы, әсердің таралуы, жақындық",
    "platformPage.caseScenario.text":
      "Әсер ету аймақтары бұзылу қай жерде басталғанын, оның активтер мен логистикаға қалай қабаттасатынын және екінші реттік әсер қай жерде байқалатынын көрсетеді.",
    "platformPage.caseScenario.alt": "Тәуекел аймақтары белгіленген әсер ету картасы",
    "platformPage.caseDetailed.title": "Өндірістік деталь",
    "platformPage.caseDetailed.subtitle": "Өндірістік аймақтар, іргелестік, тығыздық",
    "platformPage.caseDetailed.text":
      "Өндірістік детальды көріністер алаңдардың іргелестігін, инфрақұрылымға тәуелділікті және жергілікті үзіліс активтер шоғырына қай тұстан тарай алатынын түсінуге көмектеседі.",
    "platformPage.caseDetailed.alt":
      "Активтер тығыз орналасқан өндірістік аймақтардың детальды көрінісі",
    "platformPage.summary.label": "Шешім беті",
    "platformPage.summary.title":
      "Сигналдар, тәуелділіктер және операциялық салдар үшін бір жұмыс беті.",
    "platformPage.summary.text":
      "Horizon disconnected monitoring, static reporting немесе unguided search емес, structured decision intelligence қажет командаларға арналған.",
    "platformPage.summary.cta": "Қолжетімділік сұрау",
    "agentMeshPage.eyebrow": "Horizon Agent Horde",
    "agentMeshPage.title": "Сценарийлік реакция мен стратегиялық тестілеуге арналған governed simulation.",
    "agentMeshPage.text":
      "Agent Horde Horde.Vision-ды көпагентті simulation workflows-пен кеңейтеді; олар сценарий сигналы енгізілгеннен кейін институттар, компаниялар, нарықтар және нарративтердің қалай қозғалуын тексереді.",
    "agentMeshPage.ctaPrimary": "Simulation runtime-ты көру",
    "agentMeshPage.ctaSecondary": "Қолданылуын көру",
    "agentMeshPage.hero.alt": "Horizon Agent Horde шолуы",
    "agentMeshPage.operation.label": "Governed simulation runtime",
    "agentMeshPage.operation.title":
      "Сценарий енгізу, параллель әлемдер және қадағаланатын review.",
    "agentMeshPage.operation.alt": "Agent Horde модельдеу қабаты",
    "agentMeshPage.operation.signal.title": "Сигнал енгізу",
    "agentMeshPage.operation.signal.subtitle": "Decision questions as executable inputs",
    "agentMeshPage.operation.signal.text":
      "Әр run commitment-ке дейін тексерілуі тиіс құрылымдалған сценарийлік сұраудан басталады: реттеуші қадам, нарықтық үзіліс, қолжетімділік шектеуі, өндірістік оқиға немесе нарративтік ығысым.",
    "agentMeshPage.operation.worlds.title": "Governed world simulation",
    "agentMeshPage.operation.worlds.subtitle":
      "Параллель популяциялар, реакция жолдары, controlled variation",
    "agentMeshPage.operation.worlds.text":
      "Бірнеше governed world қатар зерттеледі, сондықтан бәсекелес институционалдық, коммерциялық және операциялық реакциялар жай болжанбай, нақты талданады.",
    "agentMeshPage.operation.trace.title": "Қадағаланатын нәтиже",
    "agentMeshPage.operation.trace.subtitle": "Артефактілер, lineage, review checkpoints",
    "agentMeshPage.operation.trace.text":
      "Simulation runs артефактілерді, reasoning lineage-ті, review кезеңдерін және aggregate outputs-ты сақтайды, сондықтан балама response paths салыстырылып, сыналып, қайта қолданылуы мүмкін.",
    "agentMeshPage.cases.label": "Сценарийлік қолданулар",
    "agentMeshPage.cases.title":
      "Governed response testing үшін типтік сценарий кластары.",
    "agentMeshPage.cases.supply.title": "Supply Shock and Escalation",
    "agentMeshPage.cases.supply.subtitle":
      "Алаңдар, дәліздер, алмастыру жолдары, реакция уақыты",
    "agentMeshPage.cases.supply.text":
      "Іркіліс сигналы шоғырланған жеткізу тізбегіне енгізілгенде фирмалар, реттеушілер, логистикалық акторлар және көршілес операторлар қалай жауап беруі мүмкін екенін тексеріңіз.",
    "agentMeshPage.cases.supply.alt": "Supply shock модельдеу кейсі",
    "agentMeshPage.cases.regulatory.title": "Regulatory Move and Response",
    "agentMeshPage.cases.regulatory.subtitle":
      "Юрисдикциялар, enforcement контурлары, төмен қарай бейімделу",
    "agentMeshPage.cases.regulatory.text":
      "Ереже өзгерісі, санкциялық сигнал немесе лицензиялық шектеу жүйеге енгенде құқықтық, әкімшілік және нарық қатысушылары қалай әрекет етуі мүмкін екенін модельдеңіз.",
    "agentMeshPage.cases.regulatory.alt": "Реттеуші реакцияны модельдеу кейсі",
    "agentMeshPage.cases.narrative.title": "Narrative Cascade and Alignment",
    "agentMeshPage.cases.narrative.subtitle":
      "Институционалдық хабарлама, нарық қабылдауы, екінші реттік үйлесу",
    "agentMeshPage.cases.narrative.text":
      "Сценарийлік сигнал қоғамдық кеңістікке шыққаннан кейін нарративті акторлар, институттар және коммерциялық құрылымдар қалай теңесуі, алшақтауы немесе бірін-бірі күшейтуі мүмкін екенін бақылаңыз.",
    "agentMeshPage.cases.narrative.alt": "Нарративтік каскадты модельдеу кейсі",
    "agentMeshPage.summary.label": "Модельдеу қабаты",
    "agentMeshPage.summary.title":
      "Шындық міндеттемені таңғанға дейін реакцияны тексеретін governed орта.",
    "agentMeshPage.summary.text":
      "Agent Horde күрделі институттар мен нарықтарды бір болжамға қысқартпай, ықтимал реакция траекторияларын салыстыруға көмектеседі.",
    "agentMeshPage.summary.cta": "Қолжетімділік сұрау",
    "insightsPage.eyebrow": "Ашық зертхана · Field Reports",
    "insightsPage.title": "Ең үздік LLM деген жоқ. Ең үздік модель — рөлге арналған, вертикальдағы.",
    "insightsPage.lead":
      "Жұмыс істеп тұрған multi-agent decision-intelligence жүйесінен қайталанатын бенчмарктер мен field notes. Бұл leaderboard емес. Синтетикалық тесттер емес. Нақты индустриалды reasoning-пактар, нақты рөл контракттары, жарияланған артефактілер.",
    "insightsPage.chip.cadence": "Кезеңділік",
    "insightsPage.chip.cadenceValue": "Тоқсан сайын + ad-hoc",
    "insightsPage.chip.scope": "Ауқым",
    "insightsPage.chip.scopeValue": "Рөл × Вертикаль × Multi-agent",
    "insightsPage.chip.published": "Жарияланды",
    "insightsPage.intro.label": "Бұл бөлім не үшін",
    "insightsPage.intro.title":
      "Жалпыға ашық бенчмарктер «қай модель ақылдырақ» деген сұраққа жауап береді. Біз «қай модель, қай рөлде, қай вертикальде және қайда сынады» деген сұраққа жауап береміз.",
    "insightsPage.intro.p1":
      "Мұндағы әр есеп — қысқа далалық сынақ. Біз нақты индустриалды сценарийді таңдаймыз, оны қарапайым prompt түрінде және тереңдік жауабы қандай мәліметтерді атау керектігінің rubric-імен бірге жазамыз, содан соң осы prompt-ты ақылы және тегін LLM тізімінде қоса жібереміз. Одан кейін бағаларды, әр шақырудың құнын, шикі жауаптарды және не сынғаны туралы ескертпені жариялаймыз. Ешқандай синтетикалық бенчмарк, ешқандай leaderboard, ешқандай «біз ойлаймыз» — тек «деректер көрсетеді / көрсетпейді».",
    "insightsPage.intro.p2":
      "Жаңа оқырман? «Осы есептерді қалай оқу керек» парақшасынан бастаңыз — үш минуттық гид глоссариймен (tier-1/2/3, pack, marker, rubric, stealth, frontier), есептердің жазылу ретімен және reproduction bundle-ды қалай сұрауға болатындығымен. Бұдан төмендегінің бәрі осы ортақ сөздікке сүйенеді.",
    "insightsPage.intro.howToReadLink": "Осы есептерді қалай оқу керек",
    "insightsPage.card.industrialDepthElephant.category":
      "Есеп #2 · Free-tier-дегі индустриалды тереңдік",
    "insightsPage.card.industrialDepthElephant.title":
      "Free-tier-дегі индустриалды тереңдік: 30 шақыру, кейін күнді бір модель ұстайды.",
    "insightsPage.card.industrialDepthElephant.lead":
      "OpenRouter-тің 15 free-моделі × 4 индустриалды пак = 60 ұяшық. Тек 17-сі сәтті орындалды. ~30 шақырудан кейін күндік жаһандық квота іске қосылып, 15 модельдің 14-і күн соңына дейін 429 алады. Кейін тірі қалғанды — провайдер Stealth брендімен openrouter/elephant-alpha — 12 индустриалды вертикальда қостық. 32 минут, $0.",
    "insightsPage.card.industrialDepthElephant.metaDate": "2026 · сәуір",
    "insightsPage.card.industrialDepthElephant.metaVertical": "Вертикалі: 12",
    "insightsPage.card.industrialDepthElephant.metaRole": "Open-ended проза",
    "insightsPage.card.industrialDepthElephant.metaModels": "15 модель сыналды",
    "insightsPage.card.frontierVsElephant.category":
      "Есеп #3 · Frontier шегі",
    "insightsPage.card.frontierVsElephant.title":
      "$1.20 vs $0. Бес ақылы flagship-модель индустриалды тереңдікте бір бесплатный elephant-қа қарсы.",
    "insightsPage.card.frontierVsElephant.lead":
      "Алты модель, екі индустриалды пак, барлығы $1.20. Көшбасшылық вертикаль бойынша ауысады (glm-5.1 болатта, gpt-5.4 тыңайтқыштарда). Opus бюджеттің 83%-ын жейді әрі ешбір пакетте жетекшіл бола алмайды. Бес ақылы flagship біріккенде steel rubric-тің тек 41%-ын және fertilizer rubric-тің 24%-ын ғана қамтиды — ешбір модель вертикальды иеленбейді.",
    "insightsPage.card.frontierVsElephant.metaDate": "2026 · сәуір",
    "insightsPage.card.frontierVsElephant.metaVertical":
      "Вертикалі: 2 (болат, тыңайтқыштар)",
    "insightsPage.card.frontierVsElephant.metaRole": "Open-ended проза",
    "insightsPage.card.frontierVsElephant.metaModels":
      "6 модель · барлығы $1.20",
    "insightsPage.card.threeVerticals.category":
      "Есеп #4 · Үш танылатын вертикаль",
    "insightsPage.card.threeVerticals.title":
      "FMCG, банктер, фарма. Сол алты модель, үш әртүрлі көшбасшы. Opus FMCG-де бірінші, фармада соңғы.",
    "insightsPage.card.threeVerticals.lead":
      "Сіз шынайы өмір сүретін үш индустрия: EU PPWR-дегі FMCG сатып алу, Basel III endgame-дегі еуропалық банк, IRA Medicare price negotiation-дағы top-15 фарма. Сол алты модель, барлығы $1.58. Үш әртүрлі көшбасшы. Бесплатный elephant фармада үшінші орынды $0-ге алып, GPT-5.4 және Opus-ты артта қалдырады. Opus фармада соңғы — $0.41-ге.",
    "insightsPage.card.threeVerticals.metaDate": "2026 · сәуір",
    "insightsPage.card.threeVerticals.metaVertical":
      "Вертикалі: 3 (FMCG, банктер, фарма)",
    "insightsPage.card.threeVerticals.metaRole": "Open-ended проза",
    "insightsPage.card.threeVerticals.metaModels":
      "6 модель · барлығы $1.58",
    "insightsPage.card.elephantAlias.category": "Есеп #1 · Модель шығу тегі",
    "insightsPage.card.elephantAlias.title":
      "OpenRouter-дегі «stealth»-модель іс жүзінде бірнеше бэкендтің үстіндегі routing alias.",
    "insightsPage.card.elephantAlias.lead":
      "Бір prompt-тың жеті прогоны, бір stealth alias. Latency 15× ауқымда (2.0с → 30.3с). Composition stop, conditional және go арасында ауысады. Екі құрылымы әртүрлі шығыс стилі. Және үлкенірек оқиға: frontier-масштабта модельдер пікірде жинақталмайды — алшақтайды.",
    "insightsPage.card.elephantAlias.metaDate": "2026 · сәуір",
    "insightsPage.card.elephantAlias.metaVertical": "Вертикаль: жартылай өткізгіштер",
    "insightsPage.card.elephantAlias.metaRole": "Рөл: critic",
    "insightsPage.card.elephantAlias.metaModels": "11 модель",
    "insightsPage.card.cta": "Есепті оқу →",
    "insightsPage.empty":
      "Әр тоқсанда мұнда жаңа есептер пайда болады. Кезекте: сол модель пулында contract-fidelity критик рөлінің head-to-head салыстыруы, кейін үш жаңа вертикальдағы explorer-рөлі. Бұл терминдер нені білдіретінін оқырман гидінен қараңыз.",
    "preamble.label.about": "Бұл есеп не туралы",
    "preamble.label.read": "Оны қалай оқу керек",
    "preamble.speed.fast": "2 мин",
    "preamble.speed.mid": "10 мин",
    "preamble.speed.full": "30 мин",
    "preamble.footer":
      "Жаңа оқырман? «Осы есептерді қалай оқу керек» парақшасынан бастаңыз — үш минуттық гид серияда қолданылатын терминдер глоссариімен (tier-1/2/3, pack, marker, rubric, stealth, frontier) және есептер жазылған ретімен.",
    "howToRead.eyebrow": "Оқырман гиді · Осы жерден бастаңыз",
    "howToRead.title": "Осы есептерді қалай оқу керек.",
    "howToRead.lead":
      "Бұл бет — Horde.Vision Insights сериясына арналған үш минуттық primer. Ол біз не жариялайтынымызды, қандай сөздікті қолданатынымызды, есептердің жазылу ретін және егер бірдеңені қайталап шығарғыңыз келсе — шикі артефактілерді қалай алуға болатынын түсіндіреді. Оны бір рет оқыңыз — серияның қалған бөлігінде бағдарлау әлдеқайда оңай болады.",
    "howToRead.series.heading": "Бұл серия не туралы",
    "howToRead.series.p1":
      "Horde.Vision — decision intelligence саласындағы компания. Біз институционалдық және индустриалды reasoning үшін multi-agent операциялық қабатын құрамыз — мұндай жұмыста бір ғана LLM шақыруы ешқашан толық жауап бола алмайды, ал «қай модель ең жақсы?» деген дұрыс сұрақ емес. Дұрыс сұрақ: «қай модель, қай рөлде, қай вертикальда және қайда сынады?»",
    "howToRead.series.p2":
      "Осы есептер production-ға қоятын модельдерді бағалайтын сол ішкі стендтен шығады. Бағалау командамыздан тыс операторларды қызықтыратын тұжырым берген кезде, біз оны scoring rubric-пен, құн сандарымен, шикі жауаптармен және қайталау нұсқауларымен бірге осы жерде жариялаймыз. Ешқандай синтетикалық бенчмарк. Ешқандай leaderboard. Ешқандай «Х моделі 87% алды». Нақты индустриалды сценарийлер, нақты cost ledger-лер, нақты failure modes.",
    "howToRead.series.p3":
      "Мақсатты оқырман — оператор: стратегия жетекшісі, CIO, сатып алу басшысы, chief of staff, зерттеу директоры. Келер тоқсанда қандай модельдерді жұмыс процестеріне қою керектігі туралы шешім қабылдауы керек және headline-бенчмарк ұпайларына сүйене алмайтын адам. Есептер осындай оқырманға жазылған.",
    "howToRead.speeds.heading": "Есепті үш жылдамдықпен қалай оқу керек",
    "howToRead.speeds.intro":
      "Әр есеп тақырыпты екі минутта немесе толық методологияны отыз минутта шығара алатындай құрылған. Жылдамдықты алдын ала таңдаңыз.",
    "howToRead.speeds.two.time": "2 минут",
    "howToRead.speeds.two.title": "Тақырып",
    "howToRead.speeds.two.body":
      "Беттің жоғарғы жағындағы hero-блокты (eyebrow, тақырып, lead) және оның астындағы TL;DR блогын оқыңыз. Бұл есептің толық аргументі bullet-формасында. Егер TL;DR сізді таңғалдырмаса — осы жерде тоқтауға болады. Бет негізі — дәлелдер, тезис емес.",
    "howToRead.speeds.ten.time": "10 минут",
    "howToRead.speeds.ten.title": "Тұжырымдар",
    "howToRead.speeds.ten.body":
      "«Finding 1 / 2 / 3» бөлімдерін және графиктерді қосыңыз. Олар TL;DR-дегі әр bullet-ті қалай алғанымызды көрсетеді — rank-кестелер, cost ledger, coverage ceiling есебі. Бұл жылдамдықта сіз тақырыппен де, оған дәлелмен де кетесіз.",
    "howToRead.speeds.thirty.time": "30 минут",
    "howToRead.speeds.thirty.title": "Методология",
    "howToRead.speeds.thirty.body":
      "Төмендегі Methodology, Reproduction және Artifacts бөлімдерін қосыңыз. Олар прогон қалай конфигурацияланғанын, нені өлшегенімізді және нені өлшемегенімізді, және өз prompt-тарыңыз бен модель шортлистіңізде сол матрицаны қайта жүгіртуді қаласаңыз reproduction bundle-ды қалай алатыныңызды дәл айтады.",
    "howToRead.terms.heading": "Терминология",
    "howToRead.terms.intro":
      "Серия бойы он екі термин кездеседі. Егер олардың бірі есепте бейтаныс болса — осы жерге оралыңыз.",
    "howToRead.terms.pack.term": "Pack",
    "howToRead.terms.pack.def":
      "Модельге жіберетін өзіндік индустриалды сценарий. Әдетте үш абзацты кіріспе (ойдан құрылған компания, нақты реттеу және нарықтық контекст, 2027 жылға дайындалатын шешім) және қарапайым прозада алты өсіп бара жатқан сұрақ. Ешқандай схема, system prompt, құрал жоқ. Әр pack prompt.md және markers.yaml бар қалтада тұрады.",
    "howToRead.terms.rubric.term": "Rubric",
    "howToRead.terms.rubric.def":
      "«Терең» жауап атайтын немесе санмен сипаттайтын нәрселердің толық тізімі — компания аттары, нормативтік құжат нөмірлері, даталар, бағалар, алаңдар, адамдар. Прогонға дейін жазылады, модельге ешқашан көрсетілмейді, pack қасында markers.yaml ретінде сақталады.",
    "howToRead.terms.marker.term": "Marker",
    "howToRead.terms.marker.def":
      "Rubric-тегі бір элемент — мысалы «Kalundborg», «72.5%», «Article 22», «Wezlana». Scoring консервативті case-insensitive substring сәйкестігі: marker жауапта не бар, не жоқ.",
    "howToRead.terms.tiers.term": "Tier 1 / Tier 2 / Tier 3",
    "howToRead.terms.tiers.def":
      "Rubric ішіндегі қиындық жолақтары. Tier 1 — вертикальды жалпы қамтуы бар кез келген ірі LLM білуі тиіс дүниелер. Tier 2 — білікті практик білімі. Tier 3 — сараптамалық, жаңа немесе оқиғаға қатысты білім (2024–2026 реттеу даталары, 2025 биосимиляр шығарылымдарының нақты бренд атаулары, сандық жеңілдік диапазондары, супервайзерлердің аттары). Tier 3 — модельдер арасындағы алшақтық көрінетін жер.",
    "howToRead.terms.harness.term": "Harness",
    "howToRead.terms.harness.def":
      "Модельдер тізіміне pack-ты жіберетін Python-скрипт. Prompt-ты жібереді, wall-latency-ді жазады, провайдердің input/output tokens-ын ұстайды, шикі мәтінді сақтайды және оны rubric-пен салыстырады. Әдейі қарапайым — судья LLM жоқ, субъективті scoring жоқ.",
    "howToRead.terms.frontier.term": "Frontier-модель",
    "howToRead.terms.frontier.def":
      "Ірі зертханалардың қазіргі top-tier ақылы flagship-тары. Бұл серияда ол claude-opus-4.7, gpt-5.4, gemini-3.1-pro-preview, glm-5.1 және kimi-k2.5. Пул жаңа ұрпақтар шыққанда өзгереді.",
    "howToRead.terms.stealth.term": "Stealth-модель · Elephant-alpha",
    "howToRead.terms.stealth.def":
      "OpenRouter мезгіл-мезгіл openrouter/elephant-alpha сияқты аттармен және буквалды «Stealth» provider-label-імен жасырын early-preview слоттарын қосады. Алиастың артындағы модель әдейі ашылмайды. elephant-alpha — осы серия бойы зерттеп жатқан слотымыз; #1 есепте көрсетілгендей, ол іс жүзінде бірнеше бэкендтің үстіндегі routing alias, бір модель емес.",
    "howToRead.terms.roles.term":
      "Рөл (explorer / critic / canonicalizer / supervisor)",
    "howToRead.terms.roles.def":
      "Біздің multi-agent жүйеміз ішіндегі рөлдер. Explorer еркін проза жазады және сущностарды тізеді; critic draft-ты қатаң контрактке қарсы бағалайды; canonicalizer табылған нәрселерді жүйеміздің сущность түрлеріне картаға түсіреді; supervisor рөлдер арасында жұмысты бағыттайды. Жарияланған есептердің көбі explorer немесе critic fitness-іне бағытталған.",
    "howToRead.terms.contract.term": "Strict-JSON contract",
    "howToRead.terms.contract.def":
      "Рөлге мәжбүрлі қатаң шығыс схемасы (мысалы critic рөліне). Contract-fidelity есептері модельдің қаншалықты жиі валидті JSON әрі күтілетін өрістерге сәйкес шығыс беретінін өлшейді — бұл проза тереңдігінен бөлек ось.",
    "howToRead.terms.coverage.term": "Coverage ceiling",
    "howToRead.terms.coverage.def":
      "Пулдағы кез келген модель ұстаған tier-3 markers-тердің біріктірілген саны rubric өлшеміне бөлінген. Пайдалы жоғарғы шек: «осы модельдердің барлығын біріктіріп, rubric-тің осы бөлігін ғана аласыз». Бүгінгі есептерде бұл нақты индустриалды вертикальдарда 32–48% — rubric-тің көбі ешкіммен ұсталмайды.",
    "howToRead.terms.costmarker.term": "Cost-per-marker",
    "howToRead.terms.costmarker.def":
      "Модельдің жауабына жұмсалған жалпы USD — оның ұстаған tier-3 markers санына бөлінген. Прайс-парақтары әртүрлі модельдерді apples-to-apples салыстыруға мүмкіндік береді. Бір және сол prompt-те ең арзан және ең қымбат ақылы модельдер арасындағы 53× алшақтық — сирек жағдай емес.",
    "howToRead.terms.inversion.term": "Leadership inversion",
    "howToRead.terms.inversion.def":
      "Бір модель бір вертикальда бірінші, ал келесіде соңғы болатын жағдай, сол prompt форматы мен сол бюджетте. #4 есептің негізгі тұжырымы: пулдағы алты модельдің үшеуі мұны үш ғана вертикальда жасайды. Бизнес-релевантты индустриялар арасындағы жалғыз «ең жақсы модель» жоқ.",
    "howToRead.arc.heading": "Есептер, жазылу ретімен",
    "howToRead.arc.intro":
      "Серияның әр есебі өздігінен тұра алады, бірақ олар осы ретпен жазылды және әрқайсысы алдыңғысына сүйенеді. Рет-ретімен оқыған жағдайда arc мынадай: бұл бесплатный модель не? → free-tier жалпы шыдай ма? → ақылы flagship-тар оны жеңе ала ма? → мұның бәрі шынайы өмір сүретін вертикальдарда жұмыс істей ме?",
    "howToRead.arc.r1.title":
      "Stealth-модель — routing alias. Frontier-масштабта модельдер алшақтайды.",
    "howToRead.arc.r1.body":
      "Сол semiconductor / CHIPS Act session pack-те, strict-JSON critic контракты аясында он бір модель. Екі тұжырым: elephant-alpha — бірнеше бэкендтің үстіндегі routing alias, бір модель емес; және frontier-масштабта модельдер пікірде алшақтайды, consensus truth-қа жинақталмайды.",
    "howToRead.arc.r1.link": "#1 оқу →",
    "howToRead.arc.r2.title":
      "Free-tier-дегі индустриалды тереңдік: 30 шақыру, кейін бір модель күнді ұстайды.",
    "howToRead.arc.r2.body":
      "Он бес бесплатный OpenRouter моделі төрт индустриалды пакетке қарсы. Шамамен отыз шақырудан кейін күндік жаһандық квота іске қосылады, он бестің он төртеуі 429 алады. Тірі қалған жалғыз — elephant-alpha — он екі вертикальға дейін кеңейеді. Бәрі сынған кезде free-tier-ден не шығаруға болатыны туралы далалық есеп.",
    "howToRead.arc.r2.link": "#2 оқу →",
    "howToRead.arc.r3.title":
      "$1.20 vs $0 — ақылы frontier бесплатный elephant-қа қарсы индустриалды тереңдікте.",
    "howToRead.arc.r3.body":
      "Алты модель, екі ауыр индустриалды пак (болат және CBAM, тыңайтқыштар және ag inputs), барлығы $1.20. Көшбасшы вертикаль бойынша өзгереді. Opus бюджеттің 83%-ын жейді және ешбір пакетте жетекшіл бола алмайды. Бес ақылы frontier-дің жиыны steel rubric-тің тек 41%-ын және fertilizer rubric-тің 24%-ын қамтиды — ешбір модель вертикальды иеленбейді.",
    "howToRead.arc.r3.link": "#3 оқу →",
    "howToRead.arc.r4.title":
      "FMCG, банктер, фарма. Сол алты модель, үш әртүрлі көшбасшы.",
    "howToRead.arc.r4.body":
      "Оқырман шынайы өмір сүретін үш индустрия: EU PPWR-дегі FMCG сатып алу, Basel III endgame аясындағы еуропалық банк, IRA аясында 2027 коммерциялық жоспарын дайындайтын top-15 фарма. Opus FMCG-де бірінші және фармада соңғы. Бесплатный elephant фармада үшінші орын алады ($0-ге), GPT-5.4 және Opus-ты артта қалдырады. Үш танылатын вертикальда leadership inversion дәл осылай көрінеді.",
    "howToRead.arc.r4.link": "#4 оқу →",
    "howToRead.arc.next":
      "Кезекте: сол модель пулында contract-fidelity критик рөлінің head-to-head салыстыруы, кейін үш жаңа вертикальдағы explorer-рөл.",
    "howToRead.artifacts.heading": "Артефактілер және қайталау",
    "howToRead.artifacts.p1":
      "Әр есеп Reproduction блогымен аяқталады, ол біз жүгірткен нақты команданы көрсетеді. Pack-тар, rubric-тар, harness және шикі JSONL шығыстары біз ашық түрде жарияламайтын жеке research-репода тұрады — ішінара себебі олар жаңа модельдер шыққан сайын апта сайын өзгереді, ішінара pricing құрылымдары мен ranking-таңдаулары шынайы түрде продуктыны білдіреді.",
    "howToRead.artifacts.p2":
      "Біз оларды сұраныс бойынша бөлісуге дайынбыз. Екі формат:",
    "howToRead.artifacts.li1":
      "Reproduction bundle — нақты есепте аталған әр pack-тің prompt.md және markers.yaml файлдары, прогон шикі JSONL, harness скрипті және бір-жолдық команда бар tarball. Осы матрицаны өз бетіңізше қайта жүгірту үшін жеткілікті. Тегін, шынайы оператор email-іне жіберіледі.",
    "howToRead.artifacts.li2":
      "Custom run — СІЗДІҢ вертикальдарыңызға арнап pack-тар жасаймыз, harness-ты СІЗДІҢ модель шортлистіңізге бағыттаймыз (жалпыға оңай тесттелмейтін дүниелерді қоса — ішкі fine-tune, VPC endpoints, rate-limited preview), және толық rubric-ты, графиктерді, cost ledger-ді және интерпретациялық жазбаны жеткіземіз. Бұл ақылы жоба; әдеттегі прогон — апта.",
    "howToRead.artifacts.p3":
      "Бізге hello@horde.vision мекенжайына жазыңыз және қай есепті оқып, онымен не істегіңіз келетінін айтыңыз — әдетте бұл жауапта не жіберу керектігін түсіну үшін жеткілікті.",
    "howToRead.conventions.heading": "Келісімдер",
    "howToRead.conventions.li1":
      "Single shot per cell, егер есеп variance-probe ретінде ашық белгіленбесе. Бір (модель × pack) прогон — бір JSONL-жол. Қайталанған жағдайда методологияда айтамыз.",
    "howToRead.conventions.li2":
      "Ашық проза, егер есеп strict-JSON деп ашық белгіленбесе. Есептердің көбі еркін форматта; contract-fidelity есептері бөлек осьті қолданады.",
    "howToRead.conventions.li3":
      "Құн сандары прогонда тіркеледі. Біз провайдер қайтарған нақты input_tokens және output_tokens жазып, жариялау күнінің list-бағасына көбейтеміз. Сіз өзіңіздің pricing-іңізбен қайта есептей алуыңыз үшін ledger-ді көрсетеміз.",
    "howToRead.conventions.li4":
      "Rubric scoring консервативті. Тек case-insensitive substring сәйкестігі — ешқандай fuzzy-сәйкестік, ешқандай synonym expansion, ешқандай LLM-судья. Сол себепті false negatives аз-кем ұлғаяды, false positives сирек кездеседі. Бұл trade-off маңызды жерде оны атаймыз.",
    "howToRead.conventions.li5":
      "Сәтсіз прогондарды жасырмаймыз. Жарияланған кестеде әр FAIL немесе empty_content — шынайы нәтиже. Ең қызықты тұжырымдардың бір бөлігі дәл failure modes туралы.",
    "howToRead.cta.title":
      "Енді сіз серияның кез келген есебін оқуға дайынсыз.",
    "howToRead.cta.text":
      "Insights индексіне өтіп, есеп таңдаңыз — немесе ең жаңа далалық сынақты қаласаңыз бірден #4-ке, методология бастауын қаласаңыз #1-ге, немесе операцияңыз үшін ең өзекті вертикальға өтіңіз.",
    "howToRead.cta.button": "Барлық есептер",
    "elephantAlias.preamble.about":
      "Horde.Vision Insights сериясының #1 есебі — біз ақылы және тегін LLM-дерде нақты индустриалды сценарийлерде бір ғана prompt harness-ты жіберіп, rubric-бағаларын, құнын және модель ерекшеліктерін жариялайтын қысқа далалық есептер. Бұл есепте біз OpenRouter-дегі анонимды «stealth»-слотқа қарсы бір prompt-ты жеті рет жібереміз, кейін он атаулы frontier және fast flagship-қа қарсы. Екі тұжырым: stealth-модель әрқашан модель емес (ол бірнеше бэкендтің үстіндегі routing alias бола алады), және frontier-масштабта модельдер бір жауапқа жинақталмай, пікірде алшақтайды.",
    "elephantAlias.preamble.speed.fast": "hero + TL;DR",
    "elephantAlias.preamble.speed.mid":
      "+ Finding 1 (stealth alias) және Finding 2 (frontier divergence)",
    "elephantAlias.preamble.speed.full":
      "+ методология, қайталау, bundle сұраныс бойынша",
    "industrialDepthElephant.preamble.about":
      "Horde.Vision Insights сериясының #2 есебі. OpenRouter-дегі он бес бесплатный модельді төрт индустриалды пакетке (батарея, фарма biologics, атом энергетикасы, аэрокосмос) жібердік. Шамамен отыз шақырудан кейін күндік жаһандық квота іске қосылып, он бестің он төртеуі 429 алды — біреуі шыдады, сондықтан оны он екі вертикальға кеңейттік. Бұл free-tier шынымен не беретіні, қайда сынатыны және бір бренді жоқ stealth-слоттың қайда шыдайтыны туралы далалық есеп.",
    "industrialDepthElephant.preamble.speed.fast": "hero + TL;DR",
    "industrialDepthElephant.preamble.speed.mid":
      "+ құлау графигі, 12-вертикальды тереңдік кестесі, variance probe",
    "industrialDepthElephant.preamble.speed.full":
      "+ методология, қайталау, bundle сұраныс бойынша",
    "frontierVsElephant.preamble.about":
      "Horde.Vision Insights сериясының #3 есебі. Алты модель — Claude Opus 4.7, GPT-5.4, Gemini 3.1 Pro, GLM-5.1, Kimi K2.5 және бесплатный stealth-слот elephant-alpha — екі ауыр индустриалды пакетке (CBAM аясындағы еуропалық болат және санкциялар мен экспорттық бақылау аясындағы тыңайтқыштар / ag-inputs) қарсы бір prompt harness жүргізеді. Ақылы модельдерге жалпы шот: $1.20. Негізгі тұжырым: ешбір модель екі вертикальда да жетекшіл емес, ал ақылы flagship-тар білім тереңдігінде бесплатный слотты айқын басым алмайды.",
    "frontierVsElephant.preamble.speed.fast": "hero + TL;DR",
    "frontierVsElephant.preamble.speed.mid":
      "+ tier-3 графигі, cost ledger, вертикаль бойынша рангтер",
    "frontierVsElephant.preamble.speed.full":
      "+ методология, қайталау, bundle сұраныс бойынша",
    "threeVerticals.preamble.about":
      "Horde.Vision Insights сериясының #4 есебі. Алдыңғы есеппен бірдей алты модель — Claude Opus 4.7, GPT-5.4, Gemini 3.1 Pro, GLM-5.1, Kimi K2.5 және бесплатный stealth-слот elephant-alpha — оқырман шынайы өмір сүретін үш индустрияда: EU PPWR аясындағы FMCG сатып алу, Basel III endgame аясындағы еуропалық банк және IRA аясында 2027 коммерциялық стратегиясын жоспарлайтын top-15 фарма. Негізгі тұжырым: үш вертикальда үш әртүрлі көшбасшы. Opus FMCG-де жетекшіл және фармада соңғы. Бесплатный слот фармада үшінші орын алады ($0-ге). Ешбір модель алаңды иеленбейді.",
    "threeVerticals.preamble.speed.fast": "hero + TL;DR",
    "threeVerticals.preamble.speed.mid":
      "+ rank inversion кестесі, coverage ceiling, cost-per-marker алшақтығы",
    "threeVerticals.preamble.speed.full":
      "+ методология, қайталау, bundle сұраныс бойынша",
    "elephantAlias.eyebrow": "Field Report · Модель шығу тегі · 2026-04-18",
    "elephantAlias.title":
      "Stealth-модель — routing alias. Frontier-масштабта модельдер алшақтайды.",
    "elephantAlias.lead":
      "Бір TSMC / ASML / CHIPS Act session pack-тегі он бір модель, strict-JSON critic контракты бойынша. Жариялауға тұрарлық екі олжа — екеуі де стандартты «scale → consensus → truth» нарративіне қарсы — бір күндік прогондардан шықты.",
    "elephantAlias.back": "← Талдауға қайту",
    "elephantAlias.cta.title": "Сол стенд, сіздің вертикаль.",
    "elephantAlias.cta.text":
      "Егер сіз жартылай өткізгіштерде, энергетикада, теңіз логистикасында, литий немесе REE supply chain-де жұмыс істесеңіз және өз пайплайныңыздағы critic рөліне қандай модель қою керек екенін шешіп жатсаңыз — біз сол стендті сіздің prompt-тарыңызға қарсы іске қоса аламыз. Қайталанатындық — толық өнім.",
    "elephantAlias.cta.button": "Жеке прогон сұрау",
    "industrialDepthElephant.eyebrow":
      "Field Report · Индустриалды тереңдік · 2026-04-19",
    "industrialDepthElephant.title":
      "Free-tier-дегі индустриалды тереңдік: 30 шақыру, кейін күнді бір модель ұстайды.",
    "industrialDepthElephant.lead":
      "OpenRouter-тің он бес free-моделі, төрт индустриалды вертикаль, ашық prose жауаптар. Күндік free-квота іске қосылған соң модельдердің көпшілігі шығып қалады. Кейін біз жалғыз тірі қалғанды — провайдердің сөзбе-сөз бренді Stealth астындағы openrouter/elephant-alpha — он екі индустриалды вертикальда қостық. Не байқағанымыз төменде.",
    "industrialDepthElephant.back": "← Талдауға қайту",
    "industrialDepthElephant.cta.title": "Сол стенд, сіздің вертикаль.",
    "industrialDepthElephant.cta.text":
      "Егер сіздің операцияңыз жоғарыдағы вертикальдардың бірінде немесе оларға жақын саладамса, әрі бірінші индустриалды бағдар үшін free-tier немесе stealth-aliased модельдерді өлшеп жатсаңыз — біз сол стендті сіздің prompt-тарыңызға және сіздің модельдер пуліңізге қарсы іске қоса аламыз. Қайталанатындық — толық өнім.",
    "industrialDepthElephant.cta.button": "Жеке прогон сұрау",
    "frontierVsElephant.eyebrow":
      "Field Report · Frontier шегі · 2026-04-19",
    "frontierVsElephant.title":
      "$1.20 vs $0. Frontier-flagships бір бесплатный stealth-модельге қарсы индустриалды тереңдікте.",
    "frontierVsElephant.lead":
      "Алты модель сол бір екі индустриалды пакта: openrouter/elephant-alpha (бесплатно, провайдер Stealth) бес ақылы flagship-ке қарсы — claude-opus-4.7, gpt-5.4, gemini-3.1-pro, glm-5.1, kimi-k2.5. Жалпы шығын: $1.20. Жалпы wall time: 23 минут. Жариялауға тұратын үш олжа — және бес ақылы модельдің екеуін шамамен өлтіріп жіберетін бір benchmarking-қақпан.",
    "frontierVsElephant.back": "← Талдауға қайту",
    "frontierVsElephant.cta.title":
      "Сол стенд, сіздің вертикаль, сіздің модель пулыңыз.",
    "frontierVsElephant.cta.text":
      "Егер сіз операциялық вертикаліңізде жалғыз flagship-ке ставка қою мен multi-model стратегиясы арасында таңдау жасап жатсаңыз — біз сол матрицаны сіздің prompt-тарыңызға және shortlist-іңізге қарсы жүгіртіп бере аламыз. Осы есепте көрсетілген 41% / 24% потолок — бастау нүктесі ғана; сіздің вертикаліңіздің өз потологі болады.",
    "frontierVsElephant.cta.button": "Жеке прогон сұрау",
    "threeVerticals.eyebrow":
      "Field Report · Сіз шынайы өмір сүретін үш вертикаль · 2026-04-19",
    "threeVerticals.title":
      "Үш индустрия, сол алты модель, үш әртүрлі көшбасшы. Opus FMCG-де бірінші, фармада соңғы.",
    "threeVerticals.lead":
      "EU PPWR-дегі FMCG сатып алу. Basel III endgame алдындағы еуропалық универсалды банк. Medicare price negotiation-дағы 2027 коммерциялық жоспардағы top-15 фарма. Сол алты модель, сол стенд, сол 8 192 токендік бюджет. Әр вертикальда көшбасшы өзгереді. claude-opus-4.7 FMCG-де жеңеді әрі фармада соңғы орынға түседі. Бесплатный openrouter/elephant-alpha фармада $0-ге үшінші орынды алып, GPT-5.4 және Opus-ты артта қалдырады. «Ең үздік LLM деген жоқ, ең үздік модель — рөлге арналған, вертикальдағы» дегеннің үш танылатын индустриядағы көрінісі осындай.",
    "threeVerticals.back": "← Талдауға қайту",
    "threeVerticals.cta.title":
      "Сіздің вертикаль, сіздің модель пулыңыз, сіздің prompt-тарыңыз.",
    "threeVerticals.cta.text":
      "Егер сіз ішкі AI-бағдарламаны көтеріп жатсаңыз әрі шынайы операциялық шындығыңызға қарсы қандай модель қою керек екенін шешіп жатсаңыз — өз вертикальдарыңызда — біз сол матрицаны сіздің prompt-тарыңыз бен shortlist-іңізге қарсы жүгіртіп бере аламыз. Opus-тың costs-тағы 78% үлесі, фармадағы рангтер инверсиясы, cost-per-marker бойынша 53× алшақтық — әрқайсысы бастау нүктесі. Сіздің вертикаліңізде өзіңіздікі болады.",
    "threeVerticals.cta.button": "Жеке прогон сұрау",
    "footer.label": "Horde.Vision",
    "footer.title": "Белгісіздік жағдайындағы leadership үшін.",
    "footer.note": "Күрделі операциялық ортаға арналған structured decision intelligence, grounded research және governed simulation.",
    "footer.location.title": "Мекенжай",
    "footer.location.line1": "Dostyk, 16",
    "footer.location.line2": "Astana, Kazakhstan",
    "footer.contact.title": "Байланыс",
    "footer.contact.line1": "hello@horde.vision",
    "footer.availability.title": "Қолжетімділік",
    "footer.availability.line1": "Кездесулер сұраныс бойынша",
    "footer.availability.line2": "Қосымша мәліметтер кейінірек қосылады",
    "footer.legal.title": "Құқықтық ақпарат",
    "footer.legal.privacy": "Құпиялылық туралы хабарлама",
    "footer.legal.terms": "Пайдалану шарттары",
    "footer.legal.technical": "Техникалық ашып көрсету",
  },
};

const applyLanguage = (language) => {
  const dictionary = translations[language] || translations.en;
  const pageKey = document.body.dataset.page;
  const pageTitleKey = pageKey ? `pageTitle.${pageKey}` : "";
  const pageMetaKey = pageKey ? `metaDescription.${pageKey}` : "";

  document.documentElement.lang = dictionary.locale;
  document.title = dictionary[pageTitleKey] || dictionary.pageTitle;

  if (metaDescription) {
    metaDescription.setAttribute(
      "content",
      dictionary[pageMetaKey] || dictionary.metaDescription
    );
  }

  document.querySelectorAll("[data-i18n]").forEach((element) => {
    const key = element.getAttribute("data-i18n");
    if (key && dictionary[key]) {
      element.textContent = dictionary[key];
    }
  });

  document.querySelectorAll("[data-i18n-alt]").forEach((element) => {
    const key = element.getAttribute("data-i18n-alt");
    if (key && dictionary[key]) {
      element.setAttribute("alt", dictionary[key]);
    }
  });

  document.querySelectorAll("[data-i18n-aria-label]").forEach((element) => {
    const key = element.getAttribute("data-i18n-aria-label");
    if (key && dictionary[key]) {
      element.setAttribute("aria-label", dictionary[key]);
    }
  });

  languageButtons.forEach((button) => {
    button.classList.toggle("is-active", button.dataset.lang === language);
  });

  window.localStorage.setItem("site-language", language);
};

const detectBrowserLanguage = () => {
  const candidates = [window.navigator.language, ...(window.navigator.languages || [])]
    .filter(Boolean)
    .map((value) => value.toLowerCase());

  for (const candidate of candidates) {
    if (candidate.startsWith("ru")) return "ru";
    if (candidate.startsWith("kk") || candidate.startsWith("kz")) return "kz";
    if (candidate.startsWith("en")) return "en";
  }

  return "en";
};

if (navToggle && navLinks) {
  navToggle.addEventListener("click", () => {
    const isOpen = navLinks.classList.toggle("is-open");
    navToggle.setAttribute("aria-expanded", String(isOpen));
  });

  navLinks.querySelectorAll("a").forEach((link) => {
    link.addEventListener("click", () => {
      navLinks.classList.remove("is-open");
      navToggle.setAttribute("aria-expanded", "false");
    });
  });
}

languageButtons.forEach((button) => {
  button.addEventListener("click", () => {
    applyLanguage(button.dataset.lang || "en");
  });
});

const savedLanguage = window.localStorage.getItem("site-language");
applyLanguage(savedLanguage && translations[savedLanguage] ? savedLanguage : detectBrowserLanguage());

const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");

if (revealElements.length > 0) {
  if (reducedMotion.matches || !("IntersectionObserver" in window)) {
    revealElements.forEach((element) => element.classList.add("is-visible"));
  } else {
    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (entry.isIntersecting) {
            entry.target.classList.add("is-visible");
            observer.unobserve(entry.target);
          }
        });
      },
      {
        threshold: 0.16,
        rootMargin: "0px 0px -8% 0px",
      }
    );

    revealElements.forEach((element) => observer.observe(element));
  }
}
