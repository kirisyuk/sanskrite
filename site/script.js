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
    "pageTitle.platform": "Etigram Platform Cases",
    metaDescription:
      "Etigram is a strategic intelligence firm focused on geopolitical risk review, deep monitoring, and scenario planning.",
    "metaDescription.platform":
      "Etigram Platform case views covering connected maps, entity graphs, affected areas, and industrial detail for exposed operations.",
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
    "platform.cta": "Open Platform",
    "platform.detailLink": "Explore platform cases",
    "platform.alt": "Abstract image for platform access",
    "platformPage.eyebrow": "Platform cases",
    "platformPage.title":
      "Working views for mapped exposure and decision context.",
    "platformPage.text":
      "The Etigram Platform combines mapped dependencies, relational structures, affected-area analysis, and industrial detail in one analytical surface for teams operating across exposed regions and supply chains.",
    "platformPage.ctaPrimary": "Open Platform",
    "platformPage.ctaSecondary": "Back to Services",
    "platformPage.hero.alt": "Platform surface overview",
    "platformPage.story.label": "Platform cases",
    "platformPage.story.title":
      "Four working views of operational exposure.",
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
    "pageTitle.platform": "Платформа Etigram",
    metaDescription:
      "Etigram — компания стратегической аналитики, которая помогает видеть геополитические риски, отслеживать изменения и готовить рабочие сценарии.",
    "metaDescription.platform":
      "Платформа Etigram: карты связей, графы зависимостей, зоны воздействия и детальная картина промышленных контуров.",
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
    "platform.cta": "Открыть платформу",
    "platform.detailLink": "Смотреть кейсы платформы",
    "platform.alt": "Абстрактное изображение для доступа к платформе",
    "platformPage.eyebrow": "Кейсы платформы",
    "platformPage.title":
      "Рабочие представления, которые помогают увидеть уязвимости и принять решение.",
    "platformPage.text":
      "Платформа Etigram собирает зависимости, графы связей, зоны воздействия и промышленную детализацию в одном рабочем пространстве для команд, которые работают с чувствительными регионами и цепочками поставок.",
    "platformPage.ctaPrimary": "Открыть платформу",
    "platformPage.ctaSecondary": "Назад к сервисам",
    "platformPage.hero.alt": "Общий вид рабочей поверхности платформы",
    "platformPage.story.label": "Кейсы платформы",
    "platformPage.story.title":
      "Четыре рабочих представления операционного риска.",
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
    "pageTitle.platform": "Etigram платформасы",
    metaDescription:
      "Etigram геосаяси тәуекелдерді көруге, өзгерістерді бақылауға және жұмыс сценарийлерін дайындауға көмектесетін стратегиялық талдау компаниясы.",
    "metaDescription.platform":
      "Etigram платформасы: байланысқан карталар, тәуелділік графтары, әсер ету аймақтары және өндірістік көріністер.",
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
    "platform.cta": "Платформаға өту",
    "platform.detailLink": "Платформа кейстерін көру",
    "platform.alt": "Платформаға қолжетімділікке арналған абстрактілі сурет",
    "platformPage.eyebrow": "Платформа кейстері",
    "platformPage.title":
      "Тәуекелді көріп, шешім қабылдауға көмектесетін жұмыс көріністері.",
    "platformPage.text":
      "Etigram платформасы тәуелділіктерді, байланыс графтарын, әсер ету аймақтарын және өндірістік детальды сезімтал өңірлер мен жеткізу тізбектерімен жұмыс істейтін командалар үшін бір жұмыс кеңістігіне жинайды.",
    "platformPage.ctaPrimary": "Платформаға өту",
    "platformPage.ctaSecondary": "Қызметтерге оралу",
    "platformPage.hero.alt": "Платформаның жұмыс бетінің жалпы көрінісі",
    "platformPage.story.label": "Платформа кейстері",
    "platformPage.story.title":
      "Операциялық тәуекелді көрсететін төрт жұмыс көрінісі.",
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
