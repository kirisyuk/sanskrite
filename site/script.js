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
      // Ignore cache cleanup failures on the marketing site.
    }
  }
}

void cleanupLegacyAppRuntime();

const translations = {
  en: {
    locale: "en",
    pageTitle: "Etigram",
    "pageTitle.platform": "Etigram Discover Platform",
    metaDescription:
      "Etigram is a strategic intelligence firm focused on geopolitical risk review, deep monitoring, and scenario planning.",
    "metaDescription.platform":
      "Etigram Platform: structured object topology, AI-supported signal workflows, operational use cases, and analytical views for exposed environments.",
    "nav.aria.primary": "Primary",
    "nav.aria.languageSelector": "Language selector",
    "nav.aria.switchEnglish": "Switch to English",
    "nav.aria.switchRussian": "Switch to Russian",
    "nav.aria.switchKazakh": "Switch to Kazakh",
    "nav.menu": "Menu",
    "nav.about": "About",
    "nav.services": "Services",
    "nav.platform": "Platform",
    "nav.contact": "Contact",
    "hero.eyebrow": "Strategic intelligence for exposed operating environments",
    "hero.title": "Measured intelligence for geopolitical exposure.",
    "hero.text":
      "Etigram provides concise geopolitical review, deep monitoring, and scenario planning for organizations exposed to policy shifts, regional instability, and strategic dependencies.",
    "hero.cta": "View Services",
    "intro.label": "Company",
    "intro.title":
      "Etigram structures geopolitical complexity into decision-ready briefs.",
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
    "platform.title": "Access to the working platform.",
    "platform.text":
      "Platform access is provided separately for approved workflows and client mandates, with working views configured around the client's operating footprint.",
    "platform.cta": "Discover Platform",
    "platform.detailLink": "Discover Platform",
    "platform.alt": "Abstract image for platform access",
    "platformPage.eyebrow": "Discover Platform",
    "platformPage.title":
      "A structured working surface for exposed operations.",
    "platformPage.text":
      "The Etigram Platform is built for teams that need disciplined object structure, connected signals, and decision-ready context across sensitive operating environments.",
    "platformPage.ctaPrimary": "Open Platform",
    "platformPage.ctaSecondary": "View Platform Cases",
    "platformPage.hero.alt": "Platform surface overview",
    "platformPage.foundation.label": "Platform Design",
    "platformPage.foundation.title":
      "A strict analytical structure, not a loose data collage.",
    "platformPage.foundation.taxonomy.title": "Object Taxonomy and Topology",
    "platformPage.foundation.taxonomy.subtitle":
      "Structured entities, controlled relationships, spatial logic",
    "platformPage.foundation.taxonomy.text":
      "Every object sits inside a strict taxonomy with explicit relation types, topology, hierarchy, and operational context. The platform is designed to preserve structure as complexity increases.",
    "platformPage.foundation.taxonomy.alt":
      "Object topology and relationship structure",
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
    "platformPage.cases.label": "Use Cases",
    "platformPage.cases.title":
      "Representative operating problems the platform is designed to frame.",
    "platformPage.cases.supply.title": "Supply Chain and Disruption",
    "platformPage.cases.supply.subtitle":
      "Exposure mapping across assets, routes, suppliers, bottlenecks",
    "platformPage.cases.supply.text":
      "Trace how disruptions move through suppliers, logistics corridors, industrial clusters, and strategic nodes to identify operational concentration before it becomes a supply event.",
    "platformPage.cases.supply.alt":
      "Supply chain and disruption case",
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
    "platformPage.story.label": "Analytical Views",
    "platformPage.story.title":
      "Working views already in use across the platform surface.",
    "platformPage.caseMap.title": "Connected Maps",
    "platformPage.caseMap.subtitle": "Routes, nodes, concentration",
    "platformPage.caseMap.text":
      "Connected map views place facilities, transit routes, suppliers, and operating dependencies in one frame so exposure is visible before disruption becomes operational.",
    "platformPage.caseMap.alt":
      "Connected map view showing operational nodes and routes",
    "platformPage.caseGraph.title": "Relational Graphs",
    "platformPage.caseGraph.subtitle": "Control, ownership, influence",
    "platformPage.caseGraph.text":
      "Graph views surface control chains, ownership links, affiliated entities, and indirect proximity that are easy to miss in linear reporting.",
    "platformPage.caseGraph.alt":
      "Relationship graph showing connected entities and dependencies",
    "platformPage.caseScenario.title": "Affected Areas",
    "platformPage.caseScenario.subtitle":
      "Impact zones, spillovers, proximity",
    "platformPage.caseScenario.text":
      "Affected-area views isolate disruption zones, overlay them with assets and logistics, and clarify where second-order effects start to matter.",
    "platformPage.caseScenario.alt":
      "Affected areas map highlighting impact zones and exposed assets",
    "platformPage.caseDetailed.title": "Industrial Detail",
    "platformPage.caseDetailed.subtitle":
      "Industrial zones, adjacency, operating density",
    "platformPage.caseDetailed.text":
      "Detailed industrial views help teams inspect plant adjacency, utility dependence, and where localized disruption can propagate across clustered assets.",
    "platformPage.caseDetailed.alt":
      "Detailed industrial zones view showing concentrated assets",
    "platformPage.summary.label": "Why it matters",
    "platformPage.summary.title":
      "From separate signals to a working analytical surface.",
    "platformPage.summary.text":
      "Instead of shifting between map layers, entity lists, and fragmented notes, teams can work across geography, networks, and affected areas in one decision-ready context.",
    "platformPage.summary.cta": "Go to Platform",
    "footer.label": "Etigram",
    "footer.title": "Decision Factory",
    "footer.note": "Strategic intelligence, monitoring, and scenario work.",
    "footer.location.title": "Location",
    "footer.location.line1": "Dostyk, 16",
    "footer.location.line2": "Astana, Kazakhstan",
    "footer.contact.title": "Contact",
    "footer.contact.line1": "hi@etigram.pro",
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
    pageTitle: "Etigram",
    "pageTitle.platform": "Etigram Платформа",
    metaDescription:
      "Etigram — компания стратегической аналитики, которая помогает видеть геополитические риски, отслеживать изменения и готовить рабочие сценарии.",
    "metaDescription.platform":
      "Платформа Etigram: строгая топология объектов, ИИ-поддержка сигналов, операционные кейсы и аналитические представления для уязвимой среды.",
    "nav.aria.primary": "Основная навигация",
    "nav.aria.languageSelector": "Выбор языка",
    "nav.aria.switchEnglish": "Переключить на английский",
    "nav.aria.switchRussian": "Переключить на русский",
    "nav.aria.switchKazakh": "Переключить на казахский",
    "nav.menu": "Меню",
    "nav.about": "О компании",
    "nav.services": "Сервисы",
    "nav.platform": "Платформа",
    "nav.contact": "Контакты",
    "hero.eyebrow": "Стратегическая аналитика для уязвимой операционной среды",
    "hero.title": "Спокойная аналитика для сложной геополитической среды.",
    "hero.text":
      "Etigram помогает командам видеть, как политические решения, региональная нестабильность и стратегические зависимости превращаются в операционный риск.",
    "hero.cta": "Смотреть сервисы",
    "intro.label": "Компания",
    "intro.title":
      "Etigram превращает геополитическую сложность в рабочий контекст для решений.",
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
    "platform.title": "Доступ к платформе.",
    "platform.text":
      "Доступ к платформе открывается отдельно для согласованных рабочих процессов и клиентских задач, а рабочие представления настраиваются вокруг реального операционного контура клиента.",
    "platform.cta": "Изучить платформу",
    "platform.detailLink": "Изучить платформу",
    "platform.alt": "Абстрактное изображение для доступа к платформе",
    "platformPage.eyebrow": "Изучить платформу",
    "platformPage.title":
      "Структурированная рабочая поверхность для уязвимых операционных контуров.",
    "platformPage.text":
      "Платформа Etigram создана для команд, которым нужен строгий объектный контур, связанные сигналы и рабочий контекст для решений в чувствительной операционной среде.",
    "platformPage.ctaPrimary": "Открыть платформу",
    "platformPage.ctaSecondary": "Смотреть кейсы платформы",
    "platformPage.hero.alt": "Общий вид рабочей поверхности платформы",
    "platformPage.foundation.label": "Архитектура платформы",
    "platformPage.foundation.title":
      "Строгая аналитическая структура, а не рыхлая витрина из данных.",
    "platformPage.foundation.taxonomy.title": "Таксономия и топология объектов",
    "platformPage.foundation.taxonomy.subtitle":
      "Структурированные сущности, типы связей, пространственная логика",
    "platformPage.foundation.taxonomy.text":
      "Каждый объект находится внутри строгой таксономии с явными типами связей, топологией, иерархией и операционным контекстом. Платформа сохраняет структуру даже при росте сложности.",
    "platformPage.foundation.taxonomy.alt":
      "Топология объектов и структура связей",
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
    "platformPage.foundation.ai.alt":
      "Аналитическая поверхность платформы с AI-поддержкой",
    "platformPage.cases.label": "Кейсы",
    "platformPage.cases.title":
      "Типовые операционные задачи, под которые спроектирована платформа.",
    "platformPage.cases.supply.title": "Supply Chain and Disruption",
    "platformPage.cases.supply.subtitle":
      "Карта уязвимостей по активам, маршрутам, поставщикам и узким местам",
    "platformPage.cases.supply.text":
      "Прослеживайте, как перебои проходят через поставщиков, логистические коридоры, промышленные кластеры и стратегические узлы, чтобы увидеть концентрацию риска до того, как она станет сбоем поставок.",
    "platformPage.cases.supply.alt":
      "Кейс по цепочке поставок и сбоям",
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
    "platformPage.story.label": "Аналитические представления",
    "platformPage.story.title":
      "Рабочие представления, уже используемые внутри платформенной поверхности.",
    "platformPage.caseMap.title": "Связанные карты",
    "platformPage.caseMap.subtitle": "Маршруты, узлы, концентрация",
    "platformPage.caseMap.text":
      "Связанные карты собирают площадки, маршруты, поставщиков и зависимости в одном поле зрения, чтобы уязвимость была заметна ещё до сбоя.",
    "platformPage.caseMap.alt":
      "Связанная карта с операционными узлами и маршрутами",
    "platformPage.caseGraph.title": "Графы связей",
    "platformPage.caseGraph.subtitle": "Контроль, владение, влияние",
    "platformPage.caseGraph.text":
      "Графы связей показывают контуры контроля, владения, аффилированные структуры и косвенные связи, которые теряются в линейной отчётности.",
    "platformPage.caseGraph.alt":
      "Граф связей между сущностями и зависимостями",
    "platformPage.caseScenario.title": "Зоны воздействия",
    "platformPage.caseScenario.subtitle":
      "Очаги риска, перенос эффекта, близость",
    "platformPage.caseScenario.text":
      "Зоны воздействия показывают, где возникло нарушение, как оно накладывается на активы и логистику и где начинают проявляться эффекты второго порядка.",
    "platformPage.caseScenario.alt":
      "Карта зон воздействия с выделенными участками риска",
    "platformPage.caseDetailed.title": "Промышленная детализация",
    "platformPage.caseDetailed.subtitle":
      "Промышленные зоны, соседство, плотность",
    "platformPage.caseDetailed.text":
      "Детализированные промышленные виды помогают разбирать соседство площадок, зависимость от инфраструктуры и точки, где локальный сбой способен разойтись по кластеру активов.",
    "platformPage.caseDetailed.alt":
      "Детализированный вид промышленных зон с плотной концентрацией активов",
    "platformPage.summary.label": "Почему это важно",
    "platformPage.summary.title":
      "От разрозненных сигналов к рабочей картине происходящего.",
    "platformPage.summary.text":
      "Вместо переключения между картами, списками сущностей и разрозненными заметками команда получает единый контекст по географии, связям и зонам воздействия.",
    "platformPage.summary.cta": "Перейти в платформу",
    "footer.label": "Etigram",
    "footer.title": "Decision Factory",
    "footer.note": "Стратегическая аналитика, мониторинг и сценарная работа.",
    "footer.location.title": "Адрес",
    "footer.location.line1": "Dostyk, 16",
    "footer.location.line2": "Astana, Kazakhstan",
    "footer.contact.title": "Контакты",
    "footer.contact.line1": "hi@etigram.pro",
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
    pageTitle: "Etigram",
    "pageTitle.platform": "Etigram платформасын шолу",
    metaDescription:
      "Etigram геосаяси тәуекелдерді көруге, өзгерістерді бақылауға және жұмыс сценарийлерін дайындауға көмектесетін стратегиялық талдау компаниясы.",
    "metaDescription.platform":
      "Etigram платформасы: объектілердің қатаң топологиясы, AI-қолдаулы сигнал ағындары, операциялық кейстер және осал ортаға арналған аналитикалық көріністер.",
    "nav.aria.primary": "Негізгі навигация",
    "nav.aria.languageSelector": "Тілді таңдау",
    "nav.aria.switchEnglish": "Ағылшын тіліне ауыстыру",
    "nav.aria.switchRussian": "Орыс тіліне ауыстыру",
    "nav.aria.switchKazakh": "Қазақ тіліне ауыстыру",
    "nav.menu": "Мәзір",
    "nav.about": "Компания",
    "nav.services": "Қызметтер",
    "nav.platform": "Платформа",
    "nav.contact": "Байланыс",
    "hero.eyebrow": "Осал операциялық ортаға арналған стратегиялық талдау",
    "hero.title": "Күрделі геосаяси ортаға арналған байыпты талдау.",
    "hero.text":
      "Etigram командаларға саяси өзгерістер, өңірлік тұрақсыздық және стратегиялық тәуелділіктер операциялық тәуекелге қалай айналатынын түсінуге көмектеседі.",
    "hero.cta": "Қызметтерді көру",
    "intro.label": "Компания",
    "intro.title":
      "Etigram геосаяси күрделілікті шешім қабылдауға қажет жұмыс контексіне айналдырады.",
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
    "platform.title": "Платформаға қолжетімділік.",
    "platform.text":
      "Платформаға қолжетімділік келісілген жұмыс процесі мен клиенттік міндеттер бойынша бөлек беріледі, ал жұмыс көріністері клиенттің нақты операциялық контурының айналасында бапталады.",
    "platform.cta": "Платформаны шолу",
    "platform.detailLink": "Платформаны шолу",
    "platform.alt": "Платформаға қолжетімділікке арналған абстрактілі сурет",
    "platformPage.eyebrow": "Платформаны шолу",
    "platformPage.title":
      "Осал операциялық ортаға арналған құрылымдалған жұмыс беті.",
    "platformPage.text":
      "Etigram платформасы қатаң объектілік құрылым, байланысқан сигналдар және сезімтал операциялық ортаға арналған шешім қабылдау контексі керек командалар үшін жасалған.",
    "platformPage.ctaPrimary": "Платформаға өту",
    "platformPage.ctaSecondary": "Платформа кейстерін көру",
    "platformPage.hero.alt": "Платформаның жұмыс бетінің жалпы көрінісі",
    "platformPage.foundation.label": "Платформа архитектурасы",
    "platformPage.foundation.title":
      "Бос деректер коллажы емес, қатаң аналитикалық құрылым.",
    "platformPage.foundation.taxonomy.title": "Объект таксономиясы мен топологиясы",
    "platformPage.foundation.taxonomy.subtitle":
      "Құрылымдалған нысандар, бақыланатын байланыстар, кеңістік логикасы",
    "platformPage.foundation.taxonomy.text":
      "Әр объект қатаң таксономия ішінде, айқын байланыс типтерімен, топологиямен, иерархиямен және операциялық контекспен орналасады. Күрделілік артқан сайын платформа құрылымды сақтап қалады.",
    "platformPage.foundation.taxonomy.alt":
      "Объект топологиясы мен байланыстар құрылымы",
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
    "platformPage.foundation.ai.alt":
      "AI қолдауы бар платформаның аналитикалық беті",
    "platformPage.cases.label": "Кейстер",
    "platformPage.cases.title":
      "Платформа жобаланған типтік операциялық міндеттер.",
    "platformPage.cases.supply.title": "Supply Chain and Disruption",
    "platformPage.cases.supply.subtitle":
      "Активтер, маршруттар, жеткізушілер және тар орындар бойынша осалдық картасы",
    "platformPage.cases.supply.text":
      "Іркілістердің жеткізушілер, логистикалық дәліздер, өндірістік кластерлер және стратегиялық тораптар арқылы қалай өтетінін бақылап, тәуекел шоғырлануын жеткізу оқиғасына айналмай тұрып анықтаңыз.",
    "platformPage.cases.supply.alt":
      "Жеткізу тізбегі мен іркіліс кейсі",
    "platformPage.cases.governance.title": "Complex Governance",
    "platformPage.cases.governance.subtitle":
      "Меншік тізбектері, аффилирленген құрылымдар, бақылау контурлары",
    "platformPage.cases.governance.text":
      "Басқару құрылымының өзі операциялық тәуекелге айналғанда, кім нені, қай құрылым арқылы, меншік, ықпал немесе делегирленген өкілеттіктің қай қабатында басқаратынын анық көріңіз.",
    "platformPage.cases.governance.alt":
      "Күрделі басқару құрылымы кейсі",
    "platformPage.cases.regulatory.title": "Regulatory Impact",
    "platformPage.cases.regulatory.subtitle":
      "Саяси өзгерістер, санкциялық логика, төмен қарай әсерлер",
    "platformPage.cases.regulatory.text":
      "Құқықтық немесе саяси өзгерістердің операциялық шектеуге қай жерде айналатынын көру үшін реттеуші шешімдерді юрисдикциялармен, субъектілермен, дәліздермен және өндірістік контурмен байланыстырыңыз.",
    "platformPage.cases.regulatory.alt": "Реттеуші әсер кейсі",
    "platformPage.story.label": "Аналитикалық көріністер",
    "platformPage.story.title":
      "Платформалық жұмыс бетінде қолданылып жатқан көріністер.",
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
    "platformPage.caseGraph.alt":
      "Субъектілер мен тәуелділіктер арасындағы байланыс графы",
    "platformPage.caseScenario.title": "Әсер ету аймақтары",
    "platformPage.caseScenario.subtitle":
      "Тәуекел аймағы, әсердің таралуы, жақындық",
    "platformPage.caseScenario.text":
      "Әсер ету аймақтары бұзылу қай жерде басталғанын, оның активтер мен логистикаға қалай қабаттасатынын және екінші реттік әсер қай жерде байқалатынын көрсетеді.",
    "platformPage.caseScenario.alt":
      "Тәуекел аймақтары белгіленген әсер ету картасы",
    "platformPage.caseDetailed.title": "Өндірістік деталь",
    "platformPage.caseDetailed.subtitle":
      "Өндірістік аймақтар, іргелестік, тығыздық",
    "platformPage.caseDetailed.text":
      "Өндірістік детальды көріністер алаңдардың іргелестігін, инфрақұрылымға тәуелділікті және жергілікті үзіліс активтер шоғырына қай тұстан тарай алатынын түсінуге көмектеседі.",
    "platformPage.caseDetailed.alt":
      "Активтер тығыз орналасқан өндірістік аймақтардың детальды көрінісі",
    "platformPage.summary.label": "Неге бұл маңызды",
    "platformPage.summary.title":
      "Бөлек сигналдардан тұтас жұмыс көрінісіне дейін.",
    "platformPage.summary.text":
      "Команда карта қабаттары, субъектілер тізімі мен шашыраңқы жазбалар арасында секірмей, географияны, байланыстарды және әсер ету аймақтарын бір контексте қарай алады.",
    "platformPage.summary.cta": "Платформаға өту",
    "footer.label": "Etigram",
    "footer.title": "Decision Factory",
    "footer.note": "Стратегиялық талдау, мониторинг және сценарийлік жұмыс.",
    "footer.location.title": "Мекенжай",
    "footer.location.line1": "Dostyk, 16",
    "footer.location.line2": "Astana, Kazakhstan",
    "footer.contact.title": "Байланыс",
    "footer.contact.line1": "hi@etigram.pro",
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
  const candidates = [
    window.navigator.language,
    ...(window.navigator.languages || []),
  ]
    .filter(Boolean)
    .map((value) => value.toLowerCase());

  for (const candidate of candidates) {
    if (candidate.startsWith("ru")) {
      return "ru";
    }
    if (candidate.startsWith("kk") || candidate.startsWith("kz")) {
      return "kz";
    }
    if (candidate.startsWith("en")) {
      return "en";
    }
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

    revealElements.forEach((element, index) => {
      element.style.transitionDelay = `${Math.min(index * 60, 220)}ms`;
      observer.observe(element);
    });
  }
}
