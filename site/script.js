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
    metaDescription:
      "Horde.Vision is an AI-native decision intelligence company for complex operating environments.",
    "metaDescription.platform":
      "Horizon Decision Fabric on Horde.Vision turns fragmented entities, signals, dependencies, and exposure into a working decision surface.",
    "metaDescription.agent-mesh":
      "Horizon Agent Horde on Horde.Vision runs governed simulations for testing institutional, market, and operational response.",
    "nav.aria.primary": "Primary",
    "nav.aria.languageSelector": "Language selector",
    "nav.aria.switchEnglish": "Switch to English",
    "nav.aria.switchRussian": "Switch to Russian",
    "nav.aria.switchKazakh": "Switch to Kazakh",
    "nav.menu": "Menu",
    "nav.about": "About",
    "nav.services": "Capabilities",
    "nav.platform": "Architecture",
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
    metaDescription:
      "Horde.Vision — AI-native компания в области decision intelligence для сложной операционной среды.",
    "metaDescription.platform":
      "Horizon Decision Fabric на Horde.Vision превращает разрозненные сущности, сигналы, зависимости и зоны уязвимости в рабочую поверхность для решений.",
    "metaDescription.agent-mesh":
      "Horizon Agent Horde на Horde.Vision запускает управляемые симуляции для проверки институциональной, рыночной и операционной реакции.",
    "nav.aria.primary": "Основная навигация",
    "nav.aria.languageSelector": "Выбор языка",
    "nav.aria.switchEnglish": "Переключить на английский",
    "nav.aria.switchRussian": "Переключить на русский",
    "nav.aria.switchKazakh": "Переключить на казахский",
    "nav.menu": "Меню",
    "nav.about": "О компании",
    "nav.services": "Возможности",
    "nav.platform": "Архитектура",
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
    metaDescription:
      "Horde.Vision — күрделі операциялық ортаға арналған AI-native decision intelligence компаниясы.",
    "metaDescription.platform":
      "Horizon Decision Fabric Horde.Vision аясында бытыраңқы нысандарды, сигналдарды, тәуелділіктерді және осалдықты жұмыс decision surface-іне біріктіреді.",
    "metaDescription.agent-mesh":
      "Horizon Agent Horde Horde.Vision аясында институционалдық, нарықтық және операциялық реакцияны тексеруге арналған governed simulations жүргізеді.",
    "nav.aria.primary": "Негізгі навигация",
    "nav.aria.languageSelector": "Тілді таңдау",
    "nav.aria.switchEnglish": "Ағылшын тіліне ауыстыру",
    "nav.aria.switchRussian": "Орыс тіліне ауыстыру",
    "nav.aria.switchKazakh": "Қазақ тіліне ауыстыру",
    "nav.menu": "Мәзір",
    "nav.about": "Компания",
    "nav.services": "Мүмкіндіктер",
    "nav.platform": "Архитектура",
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
