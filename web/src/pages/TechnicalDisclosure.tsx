import { useEffect, useMemo } from 'react';
import { useTranslation } from 'react-i18next';

type DisclosureLocale = 'en' | 'ru' | 'kz';

type DisclosureSection = {
  heading: string;
  paragraphs: string[];
  bullets?: string[];
};

type DisclosureContent = {
  title: string;
  metaTitle: string;
  metaDescription: string;
  lastUpdatedLabel: string;
  lastUpdatedValue: string;
  intro: string[];
  sections: DisclosureSection[];
  referencesTitle: string;
  references: Array<{ label: string; href: string }>;
  contactTitle: string;
  contactText: string;
};

const content: Record<DisclosureLocale, DisclosureContent> = {
  en: {
    title: 'Technical Disclosure',
    metaTitle: 'Technical Disclosure - Horizon',
    metaDescription:
      'Technical disclosure for the Horizon platform covering proprietary technology, AI-assisted analytical outputs, governance, oversight, and transparency controls.',
    lastUpdatedLabel: 'Last Updated:',
    lastUpdatedValue: 'March 16, 2026',
    intro: [
      'This Technical Disclosure explains how the Horizon platform operates at a high level for authorized users, clients, counterparties, and reviewers.',
      'The platform is built on proprietary workflows, data structures, and analytical interfaces. It also uses external and internal AI components to assist with certain analytical inferences, summaries, prioritization steps, and scenario-oriented outputs.',
      'This document is intended to support transparency expectations reflected in Regulation (EU) 2024/1689 (EU AI Act) and the current artificial intelligence governance framework of the Republic of Kazakhstan. It is not, by itself, a conformity assessment, legal classification, or regulatory filing.',
    ],
    sections: [
      {
        heading: '1. Scope and Intended Use',
        paragraphs: [
          'Horizon is a restricted-access analytical platform intended for professional users and approved organizations operating in geopolitically exposed, operationally sensitive, or dependency-heavy environments.',
          'Its intended use is to support structured intelligence work, mapping of exposure, monitoring of entities and events, relationship analysis, and scenario preparation. It is not offered as a general-purpose public chatbot or as a consumer-facing automated decision service.',
        ],
      },
      {
        heading: '2. Nature of the Technology Stack',
        paragraphs: [
          'The platform relies on proprietary software components, internal orchestration logic, custom ontologies, analytical workflows, visualization layers, and controlled data-processing pipelines developed or configured by Horizon.',
          'Certain platform capabilities may call external AI or machine learning services, but the overall working environment, interface logic, workflow control, and analytical assembly remain proprietary to Horizon.',
        ],
        bullets: [
          'proprietary data modeling and entity structures',
          'proprietary graph, map, and scenario workflows',
          'internal orchestration and review logic',
          'integrated third-party AI services where appropriate for specific tasks',
        ],
      },
      {
        heading: '3. Where AI Is Used',
        paragraphs: [
          'AI is used to assist with selected analytical functions, which may include extraction support, summarization, clustering, pattern suggestion, prioritization, language transformation, or scenario drafting.',
          'AI-assisted outputs may help surface possible relationships, propose analytical framing, or accelerate structured review. They do not replace final human judgment and should not be interpreted as autonomous conclusions.',
        ],
        bullets: [
          'draft analytical text and summarization',
          'signal triage and prioritization support',
          'relationship and dependency exploration',
          'scenario branch generation and comparison support',
          'translation or multilingual rendering support',
        ],
      },
      {
        heading: '4. Human Oversight and Decision Responsibility',
        paragraphs: [
          'Horizon is designed as a human-supervised working environment. The platform is intended to assist analysts, operators, and decision-makers, not to substitute for accountable human review.',
          'Users and client teams remain responsible for validating evidence, interpreting outputs, deciding whether an output is fit for use, and determining whether further escalation, corroboration, or professional review is required.',
        ],
        bullets: [
          'human review remains required before action',
          'AI-assisted outputs can be incomplete, wrong, stale, or misleading',
          'platform outputs should be treated as analytical support, not automatic determinations',
        ],
      },
      {
        heading: '5. Output Characteristics and Limitations',
        paragraphs: [
          'Platform outputs may contain uncertainty, assumptions, inferred relationships, or incomplete context. Some outputs are generated probabilistically through AI components and may reflect gaps in source data, model limitations, or ambiguous evidence.',
          'Users should expect that certain outputs require corroboration, source checking, and contextual judgment. Horizon does not represent that every output is exhaustive, error-free, or suitable for direct operational execution without review.',
        ],
      },
      {
        heading: '6. Data Inputs, Logging, and Traceability',
        paragraphs: [
          'The platform may process user prompts, uploaded materials, analytical notes, structured objects, system events, and operational metadata as part of its functioning and security posture.',
          'Where supported by the relevant workflow, the system maintains logging, traceability, and workflow records to support troubleshooting, accountability, quality review, and incident investigation.',
        ],
        bullets: [
          'user and system interaction logs',
          'workflow and processing metadata',
          'model-call and service orchestration records where available',
          'configuration and operational event records',
        ],
      },
      {
        heading: '7. Third-Party AI and Service Providers',
        paragraphs: [
          'Some functions may depend on third-party cloud, authentication, storage, mapping, search, or AI providers. These providers are used as infrastructure or component services within a broader Horizon-controlled workflow.',
          'When third-party AI is used, Horizon seeks to limit exposure of sensitive information, apply controlled integration patterns, and maintain review responsibility within the platform’s governed operating model. Provider-side terms and data-handling practices may still apply.',
        ],
      },
      {
        heading: '8. Risk Management, Testing, and Operational Controls',
        paragraphs: [
          'Horizon applies operational controls intended to reduce foreseeable misuse, unreliable output propagation, unauthorized access, and workflow drift. These measures may include restricted access, role controls, model and provider configuration controls, testing, monitoring, and staged deployment practices.',
          'No AI-enabled platform can guarantee complete absence of error, bias, outage, hallucination, or misuse. Controls reduce risk; they do not eliminate it.',
        ],
        bullets: [
          'authorized-access model and account controls',
          'environment and integration configuration management',
          'operational monitoring and logging',
          'review and adjustment of prompts, providers, and workflows',
          'incident response and corrective action where needed',
        ],
      },
      {
        heading: '9. Transparency Positioning',
        paragraphs: [
          'This document is meant to provide plain-language transparency regarding the presence of AI-assisted analytical functionality, the role of proprietary systems, and the continued need for human oversight.',
          'Nothing in this document should be read as a claim that the platform falls into a specific legal risk category under the EU AI Act or Kazakhstan law without a separate legal and factual assessment for the relevant deployment context.',
        ],
      },
      {
        heading: '10. User and Client Responsibilities',
        paragraphs: [
          'Authorized users and client organizations are expected to operate the platform within applicable law, internal governance rules, and sector-specific controls. They should evaluate whether a given use case triggers additional documentation, validation, or regulatory obligations.',
        ],
        bullets: [
          'do not rely on AI-assisted outputs as sole grounds for sensitive decisions',
          'perform contextual review before escalation or action',
          'avoid entering information that should not be processed under the relevant mandate or policy',
          'raise incidents, anomalies, or suspected harmful outputs for review',
        ],
      },
      {
        heading: '11. Alignment with EU and Kazakhstan AI Governance Expectations',
        paragraphs: [
          'This disclosure is structured to align, at a practical level, with recurring governance themes present in the EU AI Act and Kazakhstan’s AI legal framework: transparency to users, internal accountability, human oversight, logging, technical documentation, risk controls, and controlled deployment.',
          'Implementation duties may vary depending on the exact deployment context, customer sector, risk classification, territorial nexus, and future secondary regulation. Compliance must therefore be assessed on a deployment-specific basis.',
        ],
      },
    ],
    referencesTitle: 'Reference Frameworks',
    references: [
      {
        label: 'Regulation (EU) 2024/1689 (EU AI Act) - EUR-Lex',
        href: 'https://eur-lex.europa.eu/eli/reg/2024/1689/oj/eng',
      },
      {
        label: 'Official publication on the Law of the Republic of Kazakhstan on Artificial Intelligence - Akorda',
        href: 'https://www.akorda.kz/en/the-head-of-state-signed-the-law-of-the-republic-of-kazakhstan-on-artificial-intelligence-and-the-law-of-the-republic-of-kazakhstan-on-amendments-and-additions-to-some-legislative-acts-of-the-republic-of-kazakhstan-on-artificial-intelligence-1511243',
      },
      {
        label: 'Ministry summary on Kazakhstan AI regulation and implementation framework',
        href: 'https://www.gov.kz/memleket/entities/mdai/press/news/details/1056600?lang=en',
      },
    ],
    contactTitle: 'Contact',
    contactText:
      'Questions regarding this disclosure, governance posture, or deployment-specific use constraints may be directed to igor@nordicvision.org.',
  },
  ru: {
    title: 'Technical Disclosure',
    metaTitle: 'Technical Disclosure - Horizon',
    metaDescription:
      'Техническое раскрытие по платформе Horizon: проприетарные технологии, ИИ-поддержка аналитических выводов, контроль, надзор и прозрачность.',
    lastUpdatedLabel: 'Последнее обновление:',
    lastUpdatedValue: '16 марта 2026',
    intro: [
      'Этот документ объясняет на высоком уровне, как устроена платформа Horizon, для авторизованных пользователей, клиентов, контрагентов и проверяющих сторон.',
      'Платформа построена на проприетарных рабочих контурах, структурах данных и аналитических интерфейсах. Одновременно она использует внутренние и внешние ИИ-компоненты для формирования отдельных аналитических предположений, сводок, приоритизации и сценарных заготовок.',
      'Документ подготовлен в целях прозрачности с учетом ожиданий Regulation (EU) 2024/1689 (EU AI Act) и действующей рамки регулирования искусственного интеллекта в Республике Казахстан. Сам по себе он не является заключением о соответствии, юридической классификацией или регуляторной регистрацией.',
    ],
    sections: [
      {
        heading: '1. Область применения и назначение',
        paragraphs: [
          'Horizon представляет собой платформу с ограниченным доступом для профессиональных пользователей и одобренных организаций, работающих в геополитически чувствительной, операционно уязвимой или зависимой среде.',
          'Ее назначение — поддержка структурированной аналитики, картирования экспозиции, мониторинга сущностей и событий, анализа связей и подготовки сценариев. Это не публичный массовый чат-бот и не потребительский сервис автоматического принятия решений.',
        ],
      },
      {
        heading: '2. Характер технологического стека',
        paragraphs: [
          'Платформа опирается на проприетарные программные компоненты, внутреннюю логику оркестрации, собственные онтологии, аналитические рабочие контуры, визуальные слои и управляемые пайплайны обработки данных, разработанные или сконфигурированные Horizon.',
          'Отдельные функции могут вызывать внешние ИИ- или ML-сервисы, однако сама рабочая среда, логика интерфейса, управление процессом и аналитическая сборка остаются проприетарными компонентами Horizon.',
        ],
        bullets: [
          'проприетарное моделирование данных и сущностей',
          'собственные графовые, картографические и сценарные рабочие контуры',
          'внутренняя логика оркестрации и review',
          'интеграция сторонних ИИ-сервисов только для отдельных задач',
        ],
      },
      {
        heading: '3. Где используется ИИ',
        paragraphs: [
          'ИИ используется для поддержки отдельных аналитических функций, включая вспомогательное извлечение данных, суммаризацию, кластеризацию, подсказки по паттернам, приоритизацию, языковую трансформацию и сценарные черновики.',
          'ИИ-поддерживаемые результаты могут помогать выявлять вероятные связи, предлагать аналитическую рамку или ускорять структурированный обзор. Они не заменяют финальное суждение человека и не должны трактоваться как автономные выводы.',
        ],
        bullets: [
          'черновой аналитический текст и суммаризация',
          'поддержка triage и приоритизации сигналов',
          'исследование связей и зависимостей',
          'генерация и сравнение ветвей сценариев',
          'перевод и мультиязычное представление',
        ],
      },
      {
        heading: '4. Human oversight и ответственность за решения',
        paragraphs: [
          'Horizon спроектирован как рабочая среда под контролем человека. Платформа должна помогать аналитикам, операторам и decision-makers, а не подменять ответственный человеческий review.',
          'Пользователи и клиентские команды сохраняют ответственность за проверку фактуры, интерпретацию результата, решение о пригодности output к использованию и определение необходимости дополнительной верификации, эскалации или профессионального заключения.',
        ],
        bullets: [
          'перед действием требуется человеческая проверка',
          'ИИ-результаты могут быть неполными, ошибочными, устаревшими или вводящими в заблуждение',
          'output платформы — это аналитическая поддержка, а не автоматическое определение',
        ],
      },
      {
        heading: '5. Свойства outputs и ограничения',
        paragraphs: [
          'Результаты платформы могут содержать неопределенность, предположения, выводы по косвенным признакам и неполный контекст. Часть результатов формируется вероятностно через ИИ-компоненты и может отражать пробелы в исходных данных, ограничения моделей или неоднозначность evidence.',
          'Пользователь должен исходить из того, что отдельные outputs требуют сверки с источниками, дополнительной валидации и контекстного суждения. Horizon не заявляет, что каждый результат является исчерпывающим, безошибочным или пригодным для прямого операционного исполнения без review.',
        ],
      },
      {
        heading: '6. Входные данные, логирование и прослеживаемость',
        paragraphs: [
          'Платформа может обрабатывать пользовательские запросы, загруженные материалы, аналитические заметки, структурированные объекты, системные события и операционные метаданные в рамках своей функциональности и модели безопасности.',
          'Там, где это поддерживается конкретным рабочим контуром, система сохраняет логирование, прослеживаемость и workflow-записи для целей troubleshooting, accountability, quality review и расследования инцидентов.',
        ],
        bullets: [
          'логи пользовательских и системных взаимодействий',
          'workflow- и processing-метаданные',
          'записи вызовов моделей и orchestration-сервисов, где это доступно',
          'конфигурационные и операционные event-записи',
        ],
      },
      {
        heading: '7. Сторонние ИИ- и сервис-провайдеры',
        paragraphs: [
          'Отдельные функции могут зависеть от сторонних облачных, аутентификационных, storage-, mapping-, search- или AI-провайдеров. Эти провайдеры используются как инфраструктурные или компонентные сервисы внутри более широкого управляемого контура Horizon.',
          'Когда применяется сторонний ИИ, Horizon стремится ограничивать передачу чувствительной информации, использовать контролируемые схемы интеграции и сохранять ответственность за review внутри управляемой модели платформы. При этом собственные условия и практики таких провайдеров также могут применяться.',
        ],
      },
      {
        heading: '8. Risk management, тестирование и операционные контроли',
        paragraphs: [
          'Horizon применяет операционные меры, направленные на снижение предсказуемого misuse, распространения ненадежных outputs, несанкционированного доступа и дрейфа рабочих контуров. Такие меры могут включать ограничение доступа, ролевые контроли, управление конфигурациями моделей и провайдеров, тестирование, мониторинг и staged deployment.',
          'Ни одна AI-enabled платформа не может гарантировать полное отсутствие ошибки, bias, outage, hallucination или misuse. Контроли снижают риск, но не устраняют его полностью.',
        ],
        bullets: [
          'модель авторизованного доступа и account controls',
          'управление конфигурациями сред и интеграций',
          'операционный мониторинг и логирование',
          'review и корректировка prompts, провайдеров и workflows',
          'incident response и corrective actions при необходимости',
        ],
      },
      {
        heading: '9. Позиция по прозрачности',
        paragraphs: [
          'Этот документ предназначен для объяснения понятным языком наличия ИИ-поддерживаемой аналитической функциональности, роли проприетарных систем и сохраняющейся необходимости human oversight.',
          'Ничто в этом документе не должно толковаться как утверждение о том, что платформа относится к конкретной юридической категории риска по EU AI Act или казахстанскому законодательству без отдельной правовой и фактической оценки конкретного deployment-контекста.',
        ],
      },
      {
        heading: '10. Обязанности пользователей и клиентов',
        paragraphs: [
          'Авторизованные пользователи и клиентские организации должны использовать платформу в рамках применимого права, внутренних правил управления и отраслевых ограничений. Они также должны оценивать, требует ли конкретный use case дополнительной документации, валидации или регуляторных процедур.',
        ],
        bullets: [
          'не полагаться на ИИ-output как на единственное основание для чувствительных решений',
          'проводить контекстную проверку перед эскалацией или действием',
          'не вводить информацию, обработка которой выходит за пределы мандата или политики',
          'поднимать на review инциденты, аномалии и потенциально вредоносные outputs',
        ],
      },
      {
        heading: '11. Приближение к ожиданиям ЕС и Казахстана по AI governance',
        paragraphs: [
          'Структура этого disclosure приближена к повторяющимся требованиям governance, просматривающимся в EU AI Act и казахстанской AI-рамке: прозрачность для пользователей, внутренняя accountability, human oversight, логирование, техническая документация, риск-контроли и controlled deployment.',
          'Конкретные обязанности могут различаться в зависимости от deployment-контекста, сектора клиента, категории риска, территориальной привязки и последующего вторичного регулирования. Поэтому вопрос compliance должен оцениваться отдельно по каждой фактической конфигурации использования.',
        ],
      },
    ],
    referencesTitle: 'Нормативные и официальные источники',
    references: [
      {
        label: 'Regulation (EU) 2024/1689 (EU AI Act) - EUR-Lex',
        href: 'https://eur-lex.europa.eu/eli/reg/2024/1689/oj/eng',
      },
      {
        label: 'Официальная публикация о Законе Республики Казахстан об искусственном интеллекте - Akorda',
        href: 'https://www.akorda.kz/en/the-head-of-state-signed-the-law-of-the-republic-of-kazakhstan-on-artificial-intelligence-and-the-law-of-the-republic-of-kazakhstan-on-amendments-and-additions-to-some-legislative-acts-of-the-republic-of-kazakhstan-on-artificial-intelligence-1511243',
      },
      {
        label: 'Разъяснение профильного министерства по рамке регулирования ИИ в Казахстане',
        href: 'https://www.gov.kz/memleket/entities/mdai/press/news/details/1056600?lang=en',
      },
    ],
    contactTitle: 'Контакт',
    contactText:
      'Вопросы по этому disclosure, governance-подходу или ограничениям конкретного deployment могут направляться на igor@nordicvision.org.',
  },
  kz: {
    title: 'Technical Disclosure',
    metaTitle: 'Technical Disclosure - Horizon',
    metaDescription:
      'Horizon платформасы бойынша техникалық ашып көрсету: проприетарлық технологиялар, ИИ көмегімен жасалатын аналитикалық тұжырымдар, бақылау және ашықтық.',
    lastUpdatedLabel: 'Соңғы жаңарту:',
    lastUpdatedValue: '2026 жылғы 16 наурыз',
    intro: [
      'Бұл Technical Disclosure құжаты Horizon платформасының жоғары деңгейдегі жұмыс логикасын авторизацияланған пайдаланушыларға, клиенттерге, контрагенттерге және шолушы тараптарға түсіндіреді.',
      'Платформа проприетарлық жұмыс контурларына, деректер құрылымдарына және аналитикалық интерфейстерге сүйенеді. Сонымен бірге ол жекелеген аналитикалық қорытындыларды, қысқаша мазмұндарды, басымдық беруді және сценарийлік нобайларды құрастыру үшін ішкі және сыртқы ИИ компоненттерін пайдаланады.',
      'Бұл құжат ашықтық мақсатында Regulation (EU) 2024/1689 (EU AI Act) талаптарындағы күтулерге және Қазақстан Республикасындағы жасанды интеллектті реттеудің қолданыстағы шеңберіне жақындатылып дайындалды. Ол өздігінен сәйкестік туралы қорытынды, заңдық жіктеу немесе реттеуші тіркеу болып табылмайды.',
    ],
    sections: [
      {
        heading: '1. Қолданылу аясы және мақсаты',
        paragraphs: [
          'Horizon — геосаяси сезімтал, операциялық тұрғыда осал немесе тәуелділігі жоғары ортада жұмыс істейтін кәсіби пайдаланушылар мен мақұлданған ұйымдарға арналған шектеулі қолжетімді аналитикалық платформа.',
          'Оның мақсаты — құрылымдалған intelligence жұмысына, экспозицияны картаға түсіруге, субъектілер мен оқиғаларды мониторингтеуге, байланыстарды талдауға және сценарий дайындауға қолдау көрсету. Бұл жаппай ашық чат-бот та, тұтынушыға арналған автоматтандырылған шешім сервисі де емес.',
        ],
      },
      {
        heading: '2. Технологиялық стек сипаты',
        paragraphs: [
          'Платформа Horizon әзірлеген немесе баптаған проприетарлық бағдарламалық компоненттерге, ішкі orchestration логикасына, меншікті онтологияларға, аналитикалық жұмыс контурларына, визуал қабаттарға және бақыланатын деректер өңдеу pipeline-дарына сүйенеді.',
          'Кейбір функциялар сыртқы ИИ немесе ML сервистерін шақыра алады, бірақ жұмыс ортасының өзі, интерфейс логикасы, процесс басқаруы және аналитикалық жинақтау Horizon-ның проприетарлық қабаттары болып қалады.',
        ],
        bullets: [
          'проприетарлық деректер модельдеуі мен entity құрылымдары',
          'өзінің graph, map және scenario workflow-лары',
          'ішкі orchestration және review логикасы',
          'нақты міндеттер үшін ғана үшінші тарап ИИ сервистерімен интеграция',
        ],
      },
      {
        heading: '3. ИИ қай жерде қолданылады',
        paragraphs: [
          'ИИ жекелеген аналитикалық функцияларды қолдау үшін қолданылады. Бұған деректерді көмекші түрде шығару, summarization, clustering, pattern suggestion, prioritization, тілдік трансформация және scenario draft қалыптастыру кіруі мүмкін.',
          'ИИ көмегімен жасалған нәтижелер ықтимал байланыстарды көрсетуге, аналитикалық рамка ұсынуға немесе structured review процесін жылдамдатуға көмектесуі мүмкін. Олар адамның соңғы judgment-ын алмастырмайды және автономды қорытынды ретінде қаралмауы керек.',
        ],
        bullets: [
          'аналитикалық мәтіннің черновигі мен summarization',
          'сигналдарды triage және prioritization қолдауы',
          'байланыстар мен тәуелділіктерді зерттеу',
          'scenario тармақтарын құрастыру және салыстыру',
          'аударма және көптілді көрсету',
        ],
      },
      {
        heading: '4. Human oversight және шешім үшін жауапкершілік',
        paragraphs: [
          'Horizon адам бақылауындағы жұмыс ортасы ретінде жасалған. Платформа талдаушыларға, операторларға және decision-makers тобына көмектесуге тиіс, бірақ жауапты адамдық review-ді алмастырмайды.',
          'Пайдаланушылар мен клиенттік командалар evidence-ті тексеруге, нәтижені түсіндіруге, output-тың қолдануға жарамдылығын бағалауға және қосымша верификация, escalation немесе кәсіби қорытынды қажет пе екенін анықтауға жауапты болып қалады.',
        ],
        bullets: [
          'әрекетке дейін адамдық тексеру қажет',
          'ИИ нәтижелері толық емес, қате, ескірген немесе жаңылыстырушы болуы мүмкін',
          'платформа output-тары — автоматты determination емес, аналитикалық қолдау',
        ],
      },
      {
        heading: '5. Output сипаты және шектеулері',
        paragraphs: [
          'Платформа нәтижелері белгісіздік, болжам, жанама белгілерге негізделген inference және толық емес контекст қамтуы мүмкін. Нәтижелердің бір бөлігі ИИ компоненттері арқылы ықтималдық негізінде жасалады және source data кемшіліктерін, model limitations немесе ambiguity-ді көрсетуі мүмкін.',
          'Пайдаланушы кейбір output-тар source check, қосымша validation және контекстік judgment талап ететінін ескеруі тиіс. Horizon әрбір нәтиже толық, қатесіз немесе review-сіз тікелей операциялық орындауға жарамды деп мәлімдемейді.',
        ],
      },
      {
        heading: '6. Кіріс деректер, logging және traceability',
        paragraphs: [
          'Платформа өз жұмысы мен қауіпсіздік моделінің шеңберінде пайдаланушы сұрауларын, жүктелген материалдарды, аналитикалық жазбаларды, құрылымдалған объектілерді, жүйелік оқиғаларды және операциялық метадеректерді өңдеуі мүмкін.',
          'Тиісті workflow қолдаған жағдайда, troubleshooting, accountability, quality review және incident investigation мақсаттары үшін logging, traceability және workflow жазбалары сақталады.',
        ],
        bullets: [
          'пайдаланушы және жүйе өзара әрекеттесу логтары',
          'workflow және processing метадеректері',
          'қолжетімді жерде model-call және orchestration жазбалары',
          'конфигурациялық және операциялық event жазбалары',
        ],
      },
      {
        heading: '7. Үшінші тарап ИИ және service provider-лер',
        paragraphs: [
          'Кейбір функциялар cloud, authentication, storage, mapping, search немесе AI provider-лерге тәуелді болуы мүмкін. Олар Horizon бақылайтын кеңірек жұмыс контурының ішінде инфрақұрылымдық немесе компоненттік сервис ретінде қолданылады.',
          'Үшінші тарап ИИ қолданылғанда, Horizon сезімтал ақпараттың берілуін шектеуге, бақыланатын integration схемаларын қолдануға және review жауапкершілігін платформаның басқарылатын моделінде ұстауға тырысады. Дегенмен, мұндай provider-лердің өз шарттары мен data handling тәжірибелері де қолданыла алады.',
        ],
      },
      {
        heading: '8. Risk management, тестілеу және операциялық бақылаулар',
        paragraphs: [
          'Horizon болжанатын misuse-ті, сенімсіз output-тардың таралуын, рұқсатсыз қолжетімділікті және workflow drift-ті азайтуға бағытталған операциялық бақылауларды қолданады. Оларға шектеулі қолжетімділік, рөлдік бақылаулар, model/provider configuration management, testing, monitoring және staged deployment кіруі мүмкін.',
          'ИИ қосылған ешбір платформа error, bias, outage, hallucination немесе misuse толық болмайды деп кепілдік бере алмайды. Бақылаулар тәуекелді азайтады, бірақ оны толық жоймайды.',
        ],
        bullets: [
          'авторизацияланған қолжетімділік моделі және аккаунт бақылауы',
          'орта мен интеграция конфигурацияларын басқару',
          'операциялық мониторинг және logging',
          'prompts, provider және workflow-ларды review және түзету',
          'қажет болғанда incident response және corrective action',
        ],
      },
      {
        heading: '9. Ашықтық ұстанымы',
        paragraphs: [
          'Бұл құжат ИИ көмегімен жұмыс істейтін аналитикалық функциялардың бар екенін, проприетарлық жүйелердің рөлін және human oversight қажеттілігінің сақталатынын түсінікті тілмен түсіндіруге арналған.',
          'Бұл құжаттағы ешбір тұжырым platformаның EU AI Act немесе Қазақстан заңнамасы бойынша нақты бір тәуекел санатына жататыны туралы мәлімдеме ретінде жеке deployment жағдайына арналған бөлек құқықтық және фактілік бағалаусыз түсіндірілмеуі тиіс.',
        ],
      },
      {
        heading: '10. Пайдаланушылар мен клиенттердің міндеттері',
        paragraphs: [
          'Авторизацияланған пайдаланушылар мен клиенттік ұйымдар платформаны қолданылатын құқық, ішкі governance ережелері және салалық шектеулер шеңберінде қолдануы тиіс. Сондай-ақ олар нақты use case үшін қосымша documentation, validation немесе regulatory рәсімдер қажет пе екенін бағалауы керек.',
        ],
        bullets: [
          'ИИ output-тарына сезімтал шешімдердің жалғыз негізі ретінде сүйенбеу',
          'escalation немесе әрекет алдында контекстік review жүргізу',
          'мандаттан немесе саясаттан тыс өңделмеуі тиіс ақпаратты енгізбеу',
          'incident, anomaly немесе ықтимал harmful output-тарды review-ге шығару',
        ],
      },
      {
        heading: '11. ЕО және Қазақстан AI governance күтулеріне жақындату',
        paragraphs: [
          'Бұл disclosure құрылымы EU AI Act пен Қазақстандағы AI-құқықтық шеңберде қайталанатын governance тақырыптарына практикалық түрде жақындатылған: пайдаланушыға ашықтық, ішкі accountability, human oversight, logging, technical documentation, risk control және controlled deployment.',
          'Нақты міндеттер deployment контексіне, клиент саласына, risk classification-ға, аумақтық байланысқа және кейінгі secondary regulation-ға қарай өзгеруі мүмкін. Сондықтан compliance мәселесі әрбір нақты пайдалану конфигурациясы үшін бөлек бағалануы тиіс.',
        ],
      },
    ],
    referencesTitle: 'Нормативтік және ресми дереккөздер',
    references: [
      {
        label: 'Regulation (EU) 2024/1689 (EU AI Act) - EUR-Lex',
        href: 'https://eur-lex.europa.eu/eli/reg/2024/1689/oj/eng',
      },
      {
        label: 'Қазақстан Республикасының Жасанды интеллект туралы Заңы жөніндегі ресми жарияланым - Akorda',
        href: 'https://www.akorda.kz/en/the-head-of-state-signed-the-law-of-the-republic-of-kazakhstan-on-artificial-intelligence-and-the-law-of-the-republic-of-kazakhstan-on-amendments-and-additions-to-some-legislative-acts-of-the-republic-of-kazakhstan-on-artificial-intelligence-1511243',
      },
      {
        label: 'Қазақстандағы ИИ реттеу және енгізу шеңбері туралы министрлік түсіндірмесі',
        href: 'https://www.gov.kz/memleket/entities/mdai/press/news/details/1056600?lang=en',
      },
    ],
    contactTitle: 'Байланыс',
    contactText:
      'Осы disclosure, governance тәсілі немесе нақты deployment шектеулері бойынша сұрақтарды igor@nordicvision.org мекенжайына жіберуге болады.',
  },
};

function resolveLocale(language: string): DisclosureLocale {
  const normalized = String(language || 'en').toLowerCase();
  if (normalized.startsWith('ru')) return 'ru';
  if (normalized.startsWith('kk') || normalized.startsWith('kz')) return 'kz';
  return 'en';
}

export default function TechnicalDisclosure() {
  const { i18n } = useTranslation();
  const locale = resolveLocale(i18n.language);
  const page = useMemo(() => content[locale], [locale]);

  useEffect(() => {
    document.title = page.metaTitle;
    const metaDescription = document.querySelector('meta[name="description"]');
    if (metaDescription) {
      metaDescription.setAttribute('content', page.metaDescription);
    } else {
      const meta = document.createElement('meta');
      meta.name = 'description';
      meta.content = page.metaDescription;
      document.head.appendChild(meta);
    }
  }, [page]);

  return (
    <div
      className="max-w-4xl mx-auto px-6 py-8"
      style={{ color: 'var(--text-primary)', backgroundColor: 'var(--bg-elevated)', borderRadius: 12 }}
    >
      <h1 className="text-24 font-semibold mb-6" style={{ color: 'var(--text-heading)' }}>
        {page.title}
      </h1>

      <div className="text-13 leading-relaxed space-y-6" style={{ color: 'var(--text-secondary-strong)' }}>
        <p className="text-11" style={{ color: 'var(--text-tertiary)' }}>
          <strong>{page.lastUpdatedLabel}</strong> {page.lastUpdatedValue}
        </p>

        {page.intro.map((paragraph) => (
          <p key={paragraph}>{paragraph}</p>
        ))}

        {page.sections.map((section) => (
          <section key={section.heading}>
            <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>
              {section.heading}
            </h2>
            {section.paragraphs.map((paragraph) => (
              <p key={paragraph} className="mt-2 first:mt-0">
                {paragraph}
              </p>
            ))}
            {section.bullets && section.bullets.length > 0 && (
              <ul className="list-disc ml-6 mt-3 space-y-1">
                {section.bullets.map((bullet) => (
                  <li key={bullet}>{bullet}</li>
                ))}
              </ul>
            )}
          </section>
        ))}

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>
            {page.referencesTitle}
          </h2>
          <ul className="list-disc ml-6 mt-2 space-y-1">
            {page.references.map((reference) => (
              <li key={reference.href}>
                <a href={reference.href} className="underline" target="_blank" rel="noopener noreferrer">
                  {reference.label}
                </a>
              </li>
            ))}
          </ul>
        </section>

        <section>
          <h2 className="text-16 font-semibold mb-3 mt-6" style={{ color: 'var(--text-heading)' }}>
            {page.contactTitle}
          </h2>
          <p>{page.contactText}</p>
        </section>
      </div>
    </div>
  );
}
