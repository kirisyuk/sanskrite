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
    pageTitle: "Horizon",
    "pageTitle.platform": "Horizon Decision Fabric | Horizon",
    "pageTitle.agent-mesh": "Horizon Agent Horde | Horizon",
    metaDescription:
      "Horizon is a strategic intelligence firm focused on geopolitical risk review, deep monitoring, and scenario planning.",
    "metaDescription.platform":
      "Horizon Decision Fabric structures objects, dependencies, signals, and operating cases into decision-ready context.",
    "metaDescription.agent-mesh":
      "Horizon Agent Horde simulates real-world response to scenario requests across governed multi-agent worlds.",
    "nav.aria.primary": "Primary",
    "nav.aria.languageSelector": "Language selector",
    "nav.aria.switchEnglish": "Switch to English",
    "nav.aria.switchRussian": "Switch to Russian",
    "nav.aria.switchKazakh": "Switch to Kazakh",
    "nav.menu": "Menu",
    "nav.about": "About",
    "nav.services": "Services",
    "nav.platform": "Platforms",
    "nav.contact": "Contact",
    "hero.eyebrow": "Strategic intelligence for exposed operating environments",
    "hero.title": "Measured intelligence for geopolitical exposure.",
    "hero.text":
      "Horizon provides concise geopolitical review, deep monitoring, and scenario planning for organizations exposed to policy shifts, regional instability, and strategic dependencies.",
    "hero.cta": "View Services",
    "intro.label": "Company",
    "intro.title": "Horizon structures geopolitical complexity into decision-ready briefs.",
    "intro.paragraph1":
      "We produce restrained intelligence outputs for management teams that require signal, sequence, and consequence without presentation noise.",
    "intro.paragraph2":
      "Our work is built for teams that need a clear picture of exposure, dependencies, and plausible developments before they become operational losses.",
    "services.label": "Services",
    "services.title": "Core service lines.",
    "services.geo.title": "Geopolitical Risks Overview",
    "services.geo.subtitle": "Executive risk framing",
    "services.geo.text":
      "Structured overviews of regions, corridors, policy moves, and strategic dependencies where leadership requires a high-level risk picture.",
    "services.geo.alt": "Abstract image for geopolitical risks overview",
    "services.monitoring.title": "Deep Monitoring",
    "services.monitoring.subtitle": "Persistent intelligence watch",
    "services.monitoring.text":
      "Persistent tracking across selected topics, jurisdictions, entities, and weak signals that need disciplined review over time.",
    "services.monitoring.alt": "Abstract image for deep monitoring",
    "services.scenario.title": "Scenario Planning",
    "services.scenario.subtitle": "Contingency modeling",
    "services.scenario.text":
      "Structured scenario models covering escalation, disruption, and second-order effects for planning cycles and board-level discussion.",
    "services.scenario.alt": "Abstract image for scenario planning",
    "platforms.label": "Platforms",
    "platforms.title": "Two platform layers.",
    "platforms.text":
      "Horizon is structured around a decision surface for exposed operations and a separate simulation layer for scenario requests and real-world response.",
    "platforms.fabric.title": "Horizon Decision Fabric",
    "platforms.fabric.subtitle": "Structured intelligence surface",
    "platforms.fabric.text":
      "Decision Fabric organizes objects, dependencies, signals, and cases into a disciplined working surface for exposed operating environments.",
    "platforms.fabric.cta": "Discover Platform",
    "platforms.fabric.alt": "Horizon Decision Fabric overview",
    "platforms.mesh.title": "Horizon Agent Horde",
    "platforms.mesh.subtitle": "Multi-agent simulation layer",
    "platforms.mesh.text":
      "Agent Horde simulates how institutions, companies, markets, and narratives may respond to scenario-driven requests across governed multi-agent worlds.",
    "platforms.mesh.cta": "Discover Platform",
    "platforms.mesh.alt": "Horizon Agent Horde overview",
    "platformPage.eyebrow": "Horizon Decision Fabric",
    "platformPage.title": "Structured intelligence for exposed operating environments.",
    "platformPage.text":
      "Decision Fabric organizes objects, dependencies, signals, and analytical cases into a disciplined working surface for teams managing geopolitical and operational exposure.",
    "platformPage.ctaPrimary": "View Core Structure",
    "platformPage.ctaSecondary": "View Applications",
    "platformPage.hero.alt": "Horizon Decision Fabric overview",
    "platformPage.foundation.label": "Decision Fabric Core",
    "platformPage.foundation.title": "A strict analytical structure, not a loose data collage.",
    "platformPage.foundation.taxonomy.title": "Object Taxonomy and Topology",
    "platformPage.foundation.taxonomy.subtitle":
      "Structured entities, controlled relationships, spatial logic",
    "platformPage.foundation.taxonomy.text":
      "Every object sits inside a strict taxonomy with explicit relation types, topology, hierarchy, and operational context. The platform is designed to preserve structure as complexity increases.",
    "platformPage.foundation.taxonomy.alt": "Object topology and relationship structure",
    "platformPage.foundation.signals.title": "Signals Processing and Distribution",
    "platformPage.foundation.signals.subtitle":
      "Ingestion, routing, propagation, operational relevance",
    "platformPage.foundation.signals.text":
      "Signals are not stored as disconnected updates. They are processed, attached to objects, distributed through dependencies, and surfaced where they alter current exposure or planning assumptions.",
    "platformPage.foundation.signals.alt":
      "Connected signal propagation across mapped dependencies",
    "platformPage.foundation.ai.title": "AI and Analytical Workflows",
    "platformPage.foundation.ai.subtitle":
      "Assisted structuring, pattern support, analytical acceleration",
    "platformPage.foundation.ai.text":
      "AI and ML support object structuring, signal interpretation, pattern detection, and analytical draft work, while the platform remains grounded in explicit models, governed workflows, and human review.",
    "platformPage.foundation.ai.alt":
      "Platform analytical surface with AI-supported workflows",
    "platformPage.cases.label": "Decision Fabric Applications",
    "platformPage.cases.title":
      "Representative operating problems the analytical surface is designed to frame.",
    "platformPage.cases.supply.title": "Supply Chain and Disruption",
    "platformPage.cases.supply.subtitle":
      "Exposure mapping across assets, routes, suppliers, bottlenecks",
    "platformPage.cases.supply.text":
      "Trace how disruptions move through suppliers, logistics corridors, industrial clusters, and strategic nodes to identify operational concentration before it becomes a supply event.",
    "platformPage.cases.supply.alt": "Supply chain and disruption case",
    "platformPage.cases.governance.title": "Complex Governance",
    "platformPage.cases.governance.subtitle":
      "Ownership chains, affiliated entities, control surfaces",
    "platformPage.cases.governance.text":
      "Clarify who controls what, through which entities, under which layers of ownership, influence, or delegated authority, when governance structure is itself part of the operating risk.",
    "platformPage.cases.governance.alt": "Complex governance case",
    "platformPage.cases.regulatory.title": "Regulatory Impact",
    "platformPage.cases.regulatory.subtitle":
      "Policy shifts, sanctions logic, downstream consequences",
    "platformPage.cases.regulatory.text":
      "Connect regulatory decisions to jurisdictions, entities, corridors, and production footprints so teams can see where legal or policy shifts become operational constraints.",
    "platformPage.cases.regulatory.alt": "Regulatory impact case",
    "platformPage.story.label": "Representative Views",
    "platformPage.story.title": "Working views already used across the analytical surface.",
    "platformPage.caseMap.title": "Connected Maps",
    "platformPage.caseMap.subtitle": "Routes, nodes, concentration",
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
      "A working analytical surface for signals, dependencies, and consequence.",
    "platformPage.summary.text":
      "Decision Fabric is built to keep entities, signals, and operational exposure in one governed structure that teams can inspect and act on.",
    "platformPage.summary.cta": "Open Decision Fabric",
    "agentMeshPage.eyebrow": "Horizon Agent Horde",
    "agentMeshPage.title":
      "Multi-agent simulation of real-world response to scenario requests.",
    "agentMeshPage.text":
      "Agent Horde runs governed simulation worlds to observe how institutions, companies, markets, and narratives may move in response to scenario-driven requests.",
    "agentMeshPage.ctaPrimary": "View Operating Model",
    "agentMeshPage.ctaSecondary": "View Simulation Cases",
    "agentMeshPage.hero.alt": "Horizon Agent Horde overview",
    "agentMeshPage.operation.label": "Agent Horde Operation",
    "agentMeshPage.operation.title":
      "Signal injection, governed world simulation, and traceable review.",
    "agentMeshPage.operation.alt": "Agent Horde simulation layer",
    "agentMeshPage.operation.signal.title": "Signal Injection",
    "agentMeshPage.operation.signal.subtitle": "Scenario request framing",
    "agentMeshPage.operation.signal.text":
      "A simulation run begins with a scenario request: a regulatory move, market disruption, access restriction, industrial event, or narrative shift.",
    "agentMeshPage.operation.worlds.title": "World Simulation",
    "agentMeshPage.operation.worlds.subtitle":
      "Agent populations, reaction paths, scenario variation",
    "agentMeshPage.operation.worlds.text":
      "Multiple governed worlds can be explored in parallel so competing institutional, commercial, and narrative reactions are tested rather than assumed.",
    "agentMeshPage.operation.trace.title": "Traceable Outcomes",
    "agentMeshPage.operation.trace.subtitle": "Artifacts, lineage, review stages",
    "agentMeshPage.operation.trace.text":
      "Each run preserves artifacts, task lineage, review checkpoints, and aggregate output so alternative reactions can be compared and challenged.",
    "agentMeshPage.cases.label": "Simulation Cases",
    "agentMeshPage.cases.title":
      "Representative scenario requests for governed multi-agent runs.",
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
      "A governed environment for testing reaction before reality forces it.",
    "agentMeshPage.summary.text":
      "Agent Horde is designed to simulate plausible institutional and market response paths without reducing complex reaction to a single forecast.",
    "agentMeshPage.summary.cta": "Learn More About Access",
    "footer.label": "Horizon",
    "footer.title": "Decision Factory",
    "footer.note": "Strategic intelligence, monitoring, and scenario work.",
    "footer.location.title": "Location",
    "footer.location.line1": "Dostyk, 16",
    "footer.location.line2": "Astana, Kazakhstan",
    "footer.contact.title": "Contact",
    "footer.contact.line1": "igor@nordicvision.org",
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
    pageTitle: "Horizon",
    "pageTitle.platform": "Horizon Decision Fabric | Horizon",
    "pageTitle.agent-mesh": "Horizon Agent Horde | Horizon",
    metaDescription:
      "Horizon — компания стратегической аналитики, которая помогает видеть геополитические риски, отслеживать изменения и готовить рабочие сценарии.",
    "metaDescription.platform":
      "Horizon Decision Fabric структурирует объекты, зависимости, сигналы и операционные кейсы в рабочий контур для принятия решений.",
    "metaDescription.agent-mesh":
      "Horizon Agent Horde моделирует реакцию реального мира на сценарные запросы в управляемых мультиагентных мирах.",
    "nav.aria.primary": "Основная навигация",
    "nav.aria.languageSelector": "Выбор языка",
    "nav.aria.switchEnglish": "Переключить на английский",
    "nav.aria.switchRussian": "Переключить на русский",
    "nav.aria.switchKazakh": "Переключить на казахский",
    "nav.menu": "Меню",
    "nav.about": "О компании",
    "nav.services": "Сервисы",
    "nav.platform": "Платформы",
    "nav.contact": "Контакты",
    "hero.eyebrow": "Стратегическая аналитика для уязвимой операционной среды",
    "hero.title": "Спокойная аналитика для сложной геополитической среды.",
    "hero.text":
      "Horizon помогает командам видеть, как политические решения, региональная нестабильность и стратегические зависимости превращаются в операционный риск.",
    "hero.cta": "Смотреть сервисы",
    "intro.label": "Компания",
    "intro.title": "Horizon превращает геополитическую сложность в рабочий контекст для решений.",
    "intro.paragraph1":
      "Мы делаем сдержанные аналитические материалы для команд, которым важны сигнал, причинно-следственная цепочка и возможные последствия, а не шум вокруг них.",
    "intro.paragraph2":
      "Мы работаем для команд, которым важно заранее видеть уязвимости, зависимости и вероятные сценарии до того, как они превращаются в операционные потери.",
    "services.label": "Сервисы",
    "services.title": "Ключевые сервисные линии.",
    "services.geo.title": "Обзор геополитических рисков",
    "services.geo.subtitle": "Рамка рисков для руководства",
    "services.geo.text":
      "Структурированный обзор регионов, коридоров, политических решений и стратегических зависимостей там, где руководству нужна ясная картина рисков.",
    "services.geo.alt": "Абстрактное изображение для обзора геополитических рисков",
    "services.monitoring.title": "Глубокий мониторинг",
    "services.monitoring.subtitle": "Постоянное наблюдение за средой",
    "services.monitoring.text":
      "Постоянное отслеживание тем, юрисдикций, компаний и слабых сигналов, которые нельзя упускать из виду в течение времени.",
    "services.monitoring.alt": "Абстрактное изображение для глубокого мониторинга",
    "services.scenario.title": "Сценарное планирование",
    "services.scenario.subtitle": "Подготовка к развилкам и сбоям",
    "services.scenario.text":
      "Структурированные сценарии эскалации, перебоев и эффектов второго порядка для планирования и обсуждения на уровне руководства.",
    "services.scenario.alt": "Абстрактное изображение для сценарного планирования",
    "platforms.label": "Платформы",
    "platforms.title": "Две платформенные линии.",
    "platforms.text":
      "Horizon построен вокруг рабочей аналитической поверхности для уязвимой операционной среды и отдельного слоя моделирования для сценарных запросов и реакции реального мира.",
    "platforms.fabric.title": "Horizon Decision Fabric",
    "platforms.fabric.subtitle": "Структурированная аналитическая поверхность",
    "platforms.fabric.text":
      "Decision Fabric собирает объекты, зависимости, сигналы и кейсы в дисциплинированную рабочую поверхность для уязвимой операционной среды.",
    "platforms.fabric.cta": "Изучить платформу",
    "platforms.fabric.alt": "Обзор Horizon Decision Fabric",
    "platforms.mesh.title": "Horizon Agent Horde",
    "platforms.mesh.subtitle": "Мультиагентный слой моделирования",
    "platforms.mesh.text":
      "Agent Horde моделирует, как институты, компании, рынки и нарративные акторы могут отреагировать на сценарный запрос в управляемых мультиагентных мирах.",
    "platforms.mesh.cta": "Изучить платформу",
    "platforms.mesh.alt": "Обзор Horizon Agent Horde",
    "platformPage.eyebrow": "Horizon Decision Fabric",
    "platformPage.title": "Структурированная аналитика для уязвимой операционной среды.",
    "platformPage.text":
      "Decision Fabric организует объекты, зависимости, сигналы и аналитические кейсы в дисциплинированную рабочую поверхность для команд, работающих с геополитической и операционной уязвимостью.",
    "platformPage.ctaPrimary": "Смотреть ядро платформы",
    "platformPage.ctaSecondary": "Смотреть применения",
    "platformPage.hero.alt": "Обзор Horizon Decision Fabric",
    "platformPage.foundation.label": "Ядро Decision Fabric",
    "platformPage.foundation.title": "Строгая аналитическая структура, а не рыхлая витрина из данных.",
    "platformPage.foundation.taxonomy.title": "Таксономия и топология объектов",
    "platformPage.foundation.taxonomy.subtitle":
      "Структурированные сущности, типы связей, пространственная логика",
    "platformPage.foundation.taxonomy.text":
      "Каждый объект находится внутри строгой таксономии с явными типами связей, топологией, иерархией и операционным контекстом. Платформа сохраняет структуру даже при росте сложности.",
    "platformPage.foundation.taxonomy.alt": "Топология объектов и структура связей",
    "platformPage.foundation.signals.title": "Обработка и распространение сигналов",
    "platformPage.foundation.signals.subtitle":
      "Приём, маршрутизация, распространение, операционная значимость",
    "platformPage.foundation.signals.text":
      "Сигналы не хранятся как разрозненные обновления. Они обрабатываются, привязываются к объектам, распространяются по зависимостям и поднимаются там, где меняют текущую уязвимость или планирование.",
    "platformPage.foundation.signals.alt":
      "Распространение сигналов по связанным операционным зависимостям",
    "platformPage.foundation.ai.title": "AI и аналитические процессы",
    "platformPage.foundation.ai.subtitle":
      "Поддержка структурирования, поиск паттернов, ускорение анализа",
    "platformPage.foundation.ai.text":
      "AI и ML помогают в структурировании объектов, интерпретации сигналов, поиске паттернов и подготовке аналитических черновиков, но платформа остаётся опёртой на явные модели, управляемые процессы и human review.",
    "platformPage.foundation.ai.alt": "Аналитическая поверхность платформы с AI-поддержкой",
    "platformPage.cases.label": "Применения Decision Fabric",
    "platformPage.cases.title":
      "Типовые операционные задачи, под которые спроектирована аналитическая поверхность.",
    "platformPage.cases.supply.title": "Supply Chain and Disruption",
    "platformPage.cases.supply.subtitle":
      "Карта уязвимостей по активам, маршрутам, поставщикам и узким местам",
    "platformPage.cases.supply.text":
      "Прослеживайте, как перебои проходят через поставщиков, логистические коридоры, промышленные кластеры и стратегические узлы, чтобы увидеть концентрацию риска до того, как она станет сбоем поставок.",
    "platformPage.cases.supply.alt": "Кейс по цепочке поставок и сбоям",
    "platformPage.cases.governance.title": "Complex Governance",
    "platformPage.cases.governance.subtitle":
      "Контуры владения, аффилированные структуры, поверхности контроля",
    "platformPage.cases.governance.text":
      "Понимайте, кто чем управляет, через какие сущности, на каких слоях владения, влияния или делегированной ответственности, когда именно структура управления становится частью операционного риска.",
    "platformPage.cases.governance.alt": "Кейс по сложной структуре управления",
    "platformPage.cases.regulatory.title": "Regulatory Impact",
    "platformPage.cases.regulatory.subtitle":
      "Политические сдвиги, санкционная логика, downstream-последствия",
    "platformPage.cases.regulatory.text":
      "Связывайте регуляторные решения с юрисдикциями, сущностями, коридорами и производственным контуром, чтобы видеть, где правовые и политические изменения превращаются в операционные ограничения.",
    "platformPage.cases.regulatory.alt": "Кейс по регуляторному воздействию",
    "platformPage.story.label": "Рабочие представления",
    "platformPage.story.title": "Представления, уже используемые внутри аналитической поверхности.",
    "platformPage.caseMap.title": "Связанные карты",
    "platformPage.caseMap.subtitle": "Маршруты, узлы, концентрация",
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
      "Рабочий аналитический контур для сигналов, зависимостей и последствий.",
    "platformPage.summary.text":
      "Decision Fabric удерживает сущности, сигналы и операционную уязвимость внутри одного управляемого контура, который можно разбирать и использовать в работе.",
    "platformPage.summary.cta": "Открыть Decision Fabric",
    "agentMeshPage.eyebrow": "Horizon Agent Horde",
    "agentMeshPage.title":
      "Мультиагентное моделирование реакции реального мира на сценарные запросы.",
    "agentMeshPage.text":
      "Agent Horde запускает управляемые симуляционные миры, чтобы увидеть, как институты, компании, рынки и нарративы могут сдвинуться в ответ на сценарный запрос.",
    "agentMeshPage.ctaPrimary": "Смотреть операционную модель",
    "agentMeshPage.ctaSecondary": "Смотреть симуляционные кейсы",
    "agentMeshPage.hero.alt": "Обзор Horizon Agent Horde",
    "agentMeshPage.operation.label": "Работа Agent Horde",
    "agentMeshPage.operation.title":
      "Ввод сигнала, управляемое моделирование миров и прослеживаемый review.",
    "agentMeshPage.operation.alt": "Слой моделирования Agent Horde",
    "agentMeshPage.operation.signal.title": "Ввод сигнала",
    "agentMeshPage.operation.signal.subtitle": "Рамка сценарного запроса",
    "agentMeshPage.operation.signal.text":
      "Симуляционный запуск начинается со сценарного запроса: регуляторный шаг, рыночный сбой, ограничение доступа, промышленное событие или нарративный сдвиг.",
    "agentMeshPage.operation.worlds.title": "Моделирование миров",
    "agentMeshPage.operation.worlds.subtitle":
      "Агентные популяции, траектории реакции, вариативность сценариев",
    "agentMeshPage.operation.worlds.text":
      "Несколько управляемых миров можно исследовать параллельно, чтобы проверять конкурирующие институциональные, коммерческие и нарративные реакции, а не предполагать их.",
    "agentMeshPage.operation.trace.title": "Прослеживаемый результат",
    "agentMeshPage.operation.trace.subtitle": "Артефакты, lineage, этапы review",
    "agentMeshPage.operation.trace.text":
      "Каждый запуск сохраняет артефакты, lineage задач, контрольные точки review и агрегированный вывод, чтобы альтернативные реакции можно было сравнивать и оспаривать.",
    "agentMeshPage.cases.label": "Симуляционные кейсы",
    "agentMeshPage.cases.title":
      "Типовые сценарные запросы для управляемых мультиагентных запусков.",
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
      "Управляемая среда для проверки реакции до того, как это навяжет реальность.",
    "agentMeshPage.summary.text":
      "Agent Horde спроектирован так, чтобы моделировать правдоподобные институциональные и рыночные траектории реакции, не сводя сложность к одному прогнозу.",
    "agentMeshPage.summary.cta": "Узнать про доступ",
    "footer.label": "Horizon",
    "footer.title": "Decision Factory",
    "footer.note": "Стратегическая аналитика, мониторинг и сценарная работа.",
    "footer.location.title": "Адрес",
    "footer.location.line1": "Dostyk, 16",
    "footer.location.line2": "Astana, Kazakhstan",
    "footer.contact.title": "Контакты",
    "footer.contact.line1": "igor@nordicvision.org",
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
    pageTitle: "Horizon",
    "pageTitle.platform": "Horizon Decision Fabric | Horizon",
    "pageTitle.agent-mesh": "Horizon Agent Horde | Horizon",
    metaDescription:
      "Horizon геосаяси тәуекелдерді көруге, өзгерістерді бақылауға және жұмыс сценарийлерін дайындауға көмектесетін стратегиялық талдау компаниясы.",
    "metaDescription.platform":
      "Horizon Decision Fabric объектілерді, тәуелділіктерді, сигналдарды және операциялық кейстерді шешімге дайын контекстке біріктіреді.",
    "metaDescription.agent-mesh":
      "Horizon Agent Horde сценарийлік сұрауларға нақты әлемнің реакциясын басқарылатын мультиагентті әлемдерде модельдейді.",
    "nav.aria.primary": "Негізгі навигация",
    "nav.aria.languageSelector": "Тілді таңдау",
    "nav.aria.switchEnglish": "Ағылшын тіліне ауыстыру",
    "nav.aria.switchRussian": "Орыс тіліне ауыстыру",
    "nav.aria.switchKazakh": "Қазақ тіліне ауыстыру",
    "nav.menu": "Мәзір",
    "nav.about": "Компания",
    "nav.services": "Қызметтер",
    "nav.platform": "Платформалар",
    "nav.contact": "Байланыс",
    "hero.eyebrow": "Осал операциялық ортаға арналған стратегиялық талдау",
    "hero.title": "Күрделі геосаяси ортаға арналған байыпты талдау.",
    "hero.text":
      "Horizon командаларға саяси өзгерістер, өңірлік тұрақсыздық және стратегиялық тәуелділіктер операциялық тәуекелге қалай айналатынын түсінуге көмектеседі.",
    "hero.cta": "Қызметтерді көру",
    "intro.label": "Компания",
    "intro.title": "Horizon геосаяси күрделілікті шешім қабылдауға қажет жұмыс контексіне айналдырады.",
    "intro.paragraph1":
      "Біз басқарушы командаларға артық әшекейсіз, тек сигналды, себеп-салдарды және ықтимал нәтижені көрсететін ұстамды аналитикалық материалдар дайындаймыз.",
    "intro.paragraph2":
      "Біздің жұмыс операциялық шығынға айналмай тұрып осалдықты, тәуелділікті және ықтимал сценарийді алдын ала көруі керек командаларға арналған.",
    "services.label": "Қызметтер",
    "services.title": "Негізгі қызмет желілері.",
    "services.geo.title": "Геосаяси тәуекелдерге шолу",
    "services.geo.subtitle": "Басшылыққа арналған тәуекел жақтауы",
    "services.geo.text":
      "Басшылыққа тәуекелдің анық көрінісі керек болатын аймақтар, дәліздер, саяси қадамдар және стратегиялық тәуелділіктер бойынша құрылымдалған шолу.",
    "services.geo.alt": "Геосаяси тәуекелдер шолуына арналған абстрактілі сурет",
    "services.monitoring.title": "Терең мониторинг",
    "services.monitoring.subtitle": "Ортадағы өзгерісті тұрақты бақылау",
    "services.monitoring.text":
      "Ұзақ уақыт назардан тыс қалдыруға болмайтын тақырыптар, юрисдикциялар, компаниялар және әлсіз сигналдар бойынша тұрақты бақылау.",
    "services.monitoring.alt": "Терең мониторингке арналған абстрактілі сурет",
    "services.scenario.title": "Сценарийлік жоспарлау",
    "services.scenario.subtitle": "Бұрылыс пен үзіліске дайындық",
    "services.scenario.text":
      "Эскалацияға, үзіліске және екінші реттік әсерлерге арналған құрылымдалған сценарийлер жоспарлау циклдары мен басқарушылық талқылауларға көмектеседі.",
    "services.scenario.alt": "Сценарийлік жоспарлауға арналған абстрактілі сурет",
    "platforms.label": "Платформалар",
    "platforms.title": "Екі платформалық қабат.",
    "platforms.text":
      "Horizon осал операциялық ортаға арналған аналитикалық бет пен сценарийлік сұраулар мен нақты әлем реакциясын модельдейтін бөлек қабаттың айналасында құрылған.",
    "platforms.fabric.title": "Horizon Decision Fabric",
    "platforms.fabric.subtitle": "Құрылымдалған аналитикалық бет",
    "platforms.fabric.text":
      "Decision Fabric объектілерді, тәуелділіктерді, сигналдарды және кейстерді осал операциялық ортаға арналған тәртіпті жұмыс бетіне жинайды.",
    "platforms.fabric.cta": "Платформаны шолу",
    "platforms.fabric.alt": "Horizon Decision Fabric шолуы",
    "platforms.mesh.title": "Horizon Agent Horde",
    "platforms.mesh.subtitle": "Мультиагентті модельдеу қабаты",
    "platforms.mesh.text":
      "Agent Horde институттар, компаниялар, нарықтар және нарративті акторлар сценарийлік сұрауға басқарылатын мультиагентті әлемдерде қалай жауап беруі мүмкін екенін модельдейді.",
    "platforms.mesh.cta": "Платформаны шолу",
    "platforms.mesh.alt": "Horizon Agent Horde шолуы",
    "platformPage.eyebrow": "Horizon Decision Fabric",
    "platformPage.title": "Осал операциялық ортаға арналған құрылымдалған талдау.",
    "platformPage.text":
      "Decision Fabric объектілерді, тәуелділіктерді, сигналдарды және аналитикалық кейстерді геосаяси және операциялық осалдықпен жұмыс істейтін командаларға арналған тәртіпті жұмыс бетіне біріктіреді.",
    "platformPage.ctaPrimary": "Платформаның өзегін көру",
    "platformPage.ctaSecondary": "Қолданылуын көру",
    "platformPage.hero.alt": "Horizon Decision Fabric шолуы",
    "platformPage.foundation.label": "Decision Fabric өзегі",
    "platformPage.foundation.title": "Бос деректер коллажы емес, қатаң аналитикалық құрылым.",
    "platformPage.foundation.taxonomy.title": "Объект таксономиясы мен топологиясы",
    "platformPage.foundation.taxonomy.subtitle":
      "Құрылымдалған нысандар, бақыланатын байланыстар, кеңістік логикасы",
    "platformPage.foundation.taxonomy.text":
      "Әр объект қатаң таксономия ішінде, айқын байланыс типтерімен, топологиямен, иерархиямен және операциялық контекспен орналасады. Күрделілік артқан сайын платформа құрылымды сақтап қалады.",
    "platformPage.foundation.taxonomy.alt": "Объект топологиясы мен байланыстар құрылымы",
    "platformPage.foundation.signals.title": "Сигналдарды өңдеу және тарату",
    "platformPage.foundation.signals.subtitle":
      "Қабылдау, маршруттау, тарату, операциялық мән",
    "platformPage.foundation.signals.text":
      "Сигналдар бөлек жаңартулар ретінде сақталмайды. Олар өңделеді, объектілерге бекітіледі, тәуелділіктер арқылы таралады және ағымдағы осалдықты не жоспарлау болжамдарын өзгертетін жерде көрінеді.",
    "platformPage.foundation.signals.alt":
      "Байланысқан операциялық тәуелділіктер арқылы сигналдардың таралуы",
    "platformPage.foundation.ai.title": "AI және аналитикалық жұмыс ағындары",
    "platformPage.foundation.ai.subtitle":
      "Құрылымдауға көмек, үлгі табу, талдауды жеделдету",
    "platformPage.foundation.ai.text":
      "AI мен ML объектілерді құрылымдауға, сигналдарды түсіндіруге, үлгілерді табуға және аналитикалық нобай дайындауға көмектеседі, бірақ платформаның негізі айқын модельдерге, басқарылатын жұмыс ағындарына және адам тексеруіне сүйенеді.",
    "platformPage.foundation.ai.alt": "AI қолдауы бар платформаның аналитикалық беті",
    "platformPage.cases.label": "Decision Fabric қолданылуы",
    "platformPage.cases.title": "Аналитикалық бет жобаланған типтік операциялық міндеттер.",
    "platformPage.cases.supply.title": "Supply Chain and Disruption",
    "platformPage.cases.supply.subtitle":
      "Активтер, маршруттар, жеткізушілер және тар орындар бойынша осалдық картасы",
    "platformPage.cases.supply.text":
      "Іркілістердің жеткізушілер, логистикалық дәліздер, өндірістік кластерлер және стратегиялық тораптар арқылы қалай өтетінін бақылап, тәуекел шоғырлануын жеткізу оқиғасына айналмай тұрып анықтаңыз.",
    "platformPage.cases.supply.alt": "Жеткізу тізбегі мен іркіліс кейсі",
    "platformPage.cases.governance.title": "Complex Governance",
    "platformPage.cases.governance.subtitle":
      "Меншік тізбектері, аффилирленген құрылымдар, бақылау контурлары",
    "platformPage.cases.governance.text":
      "Басқару құрылымының өзі операциялық тәуекелге айналғанда, кім нені, қай құрылым арқылы, меншік, ықпал немесе делегирленген өкілеттіктің қай қабатында басқаратынын анық көріңіз.",
    "platformPage.cases.governance.alt": "Күрделі басқару құрылымы кейсі",
    "platformPage.cases.regulatory.title": "Regulatory Impact",
    "platformPage.cases.regulatory.subtitle":
      "Саяси өзгерістер, санкциялық логика, төмен қарай әсерлер",
    "platformPage.cases.regulatory.text":
      "Құқықтық немесе саяси өзгерістердің операциялық шектеуге қай жерде айналатынын көру үшін реттеуші шешімдерді юрисдикциялармен, субъектілермен, дәліздермен және өндірістік контурмен байланыстырыңыз.",
    "platformPage.cases.regulatory.alt": "Реттеуші әсер кейсі",
    "platformPage.story.label": "Жұмыс көріністері",
    "platformPage.story.title": "Аналитикалық бетте қолданылып жатқан көріністер.",
    "platformPage.caseMap.title": "Байланысқан карталар",
    "platformPage.caseMap.subtitle": "Маршруттар, түйіндер, шоғырлану",
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
      "Сигналдар, тәуелділіктер және салдар үшін жұмыс аналитикалық контуры.",
    "platformPage.summary.text":
      "Decision Fabric субъектілерді, сигналдарды және операциялық осалдықты бір басқарылатын құрылымда ұстап, командаларға оны талдап, қолдануға мүмкіндік береді.",
    "platformPage.summary.cta": "Decision Fabric ашу",
    "agentMeshPage.eyebrow": "Horizon Agent Horde",
    "agentMeshPage.title": "Сценарийлік сұрауларға нақты әлем реакциясын мультиагентті модельдеу.",
    "agentMeshPage.text":
      "Agent Horde сценарийлік сұрауға жауап ретінде институттар, компаниялар, нарықтар және нарративтер қалай қозғала алатынын көру үшін басқарылатын симуляциялық әлемдерді іске қосады.",
    "agentMeshPage.ctaPrimary": "Операциялық модельді көру",
    "agentMeshPage.ctaSecondary": "Симуляция кейстерін көру",
    "agentMeshPage.hero.alt": "Horizon Agent Horde шолуы",
    "agentMeshPage.operation.label": "Agent Horde жұмысы",
    "agentMeshPage.operation.title":
      "Сигнал енгізу, басқарылатын әлемдерді модельдеу және қадағаланатын review.",
    "agentMeshPage.operation.alt": "Agent Horde модельдеу қабаты",
    "agentMeshPage.operation.signal.title": "Сигнал енгізу",
    "agentMeshPage.operation.signal.subtitle": "Сценарийлік сұрауды рамкалау",
    "agentMeshPage.operation.signal.text":
      "Симуляция іске қосылуы сценарийлік сұраудан басталады: реттеуші қадам, нарықтық үзіліс, қолжетімділік шектеуі, өндірістік оқиға немесе нарративтік ығысым.",
    "agentMeshPage.operation.worlds.title": "Әлемдерді модельдеу",
    "agentMeshPage.operation.worlds.subtitle":
      "Агент популяциялары, реакция жолдары, сценарий вариативтілігі",
    "agentMeshPage.operation.worlds.text":
      "Бірнеше басқарылатын әлемді қатар зерттеуге болады, сонда бәсекелес институционалдық, коммерциялық және нарративтік реакциялар тексеріледі, жай болжанбайды.",
    "agentMeshPage.operation.trace.title": "Қадағаланатын нәтиже",
    "agentMeshPage.operation.trace.subtitle": "Артефактілер, lineage, review кезеңдері",
    "agentMeshPage.operation.trace.text":
      "Әр іске қосу артефактілерді, тапсырма lineage-ын, review бақылау нүктелерін және жиынтық нәтижені сақтайды, сондықтан балама реакцияларды салыстыруға және талқылауға болады.",
    "agentMeshPage.cases.label": "Симуляция кейстері",
    "agentMeshPage.cases.title":
      "Басқарылатын мультиагентті іске қосудың типтік сценарийлік сұраулары.",
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
      "Реалдылық мәжбүрлемей тұрып реакцияны тексеруге арналған басқарылатын орта.",
    "agentMeshPage.summary.text":
      "Agent Horde күрделі реакцияны бір ғана болжамға түсірмей, институционалдық және нарықтық жауаптың шынайы траекторияларын модельдеуге арналған.",
    "agentMeshPage.summary.cta": "Қолжетімділік туралы білу",
    "footer.label": "Horizon",
    "footer.title": "Decision Factory",
    "footer.note": "Стратегиялық талдау, мониторинг және сценарийлік жұмыс.",
    "footer.location.title": "Мекенжай",
    "footer.location.line1": "Dostyk, 16",
    "footer.location.line2": "Astana, Kazakhstan",
    "footer.contact.title": "Байланыс",
    "footer.contact.line1": "igor@nordicvision.org",
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
