# AgentHorde

Общий канонизатор и схемы вынесены в пакет [`agenthorde_common`](../../shared/agenthorde_common/); см. [два рантайма](../../../docs/agents/agenthorde-runtimes.md).

Greenfield **Vertex AI** + **Google ADK** (`google.adk`) experiment: one supervisor loop (cycle cap), workers `supply_chain_analyzer`, `governance_control_analyzer` (ownership and corporate control chains), `sanctions_analyzer`, `critic_agent`, `red_team_agent`, typed session pack and tool envelopes. `apply` accumulates JSON patches into session state (no production graph runtime).

## Текущее состояние (schema v2)

Короткая сводка для агентов/ревьюеров, чтобы не копать по всему файлу:

- **Канонический слой синонимов**: в ядре есть общий словарь `horizon_synonym_pairs` (Go runtime `synonymdict`, Python `DictionaryRuntime.resolve_generic_synonym`). Canonicalizer умеет `strip_legal_suffix` / `strip_parenthetical_alias` (см. `../../shared/agenthorde_common/agenthorde_common/canonicalizer/slug.py` + `assets/legal_suffixes.json`) и гасит внутренние id-шники `eNN` в `_resolve_surface_to_entity`.
- **Санкционный слой**: таксономия несёт `SANCTIONS_REGIMES_CATALOG` (OFAC SDN, EU restrictive measures, EAR/BIS, G7 price cap, CAATSA, …). Phase 3 нормализует `regime` entities к каноническим путям вида `/regime/ofac/sdn`. Специализированный `sanctions_analyzer` (см. `agenthorde/agents/workers.py`) поднимается по `escalationHints` из supply-chain аналитика или supervisor policy.
- **EscalationHint**: `AnalyzerOutput.escalationHints: list[EscalationHint{reason, suggestedAgent, confidence}]`. supervisor router читает подсказки и при `confidence >= 0.6` перенаправляет поток на `sanctions_analyzer` / `governance_control_analyzer` / `red_team_agent` и т.д.
- **Person floor-coverage**: `governance_control_analyzer` обязан для каждого `person` эмитить минимум один из `member_of / directs / owns / controls / incorporated_in`, а в pre-return sanity sweep — удалять persons без структурной привязки.
- **Динамический cycle cap**: supervisor может ставить `extendCapBy` (1–3 раза) в `SupervisorDecision` либо вызвать tool `extend_cycle_cap(reason, uncoveredFamilies)`. Каждое расширение добавляет `ceil(initial_cap * 0.1)` итераций и взводит `extensions_pending_red_team` — следующий цикл принудительно уходит в `red_team_agent`, чтобы расширение бюджета «оплачивалось» критикой.
- **Budget telemetry**: в `SessionPack` появились `initial_supervisor_cap`, `extensions_used`, `extensions_pending_red_team`, `token_spend`, `web_search_calls_used`, `cost_so_far_usd`.
- **Parallel fan-out**: `SupervisorDecision{nextAgent: "parallel_fanout", parallelAgents: [...]}` запускает несколько аналитиков в `asyncio.gather`. Мердж в `reasoning_graph`/`proposed_relations` идемпотентен — при коллизиях побеждает узел с большим числом evidence_refs / более длинным текстом.
- **Canonicalizer follow-up plan**: `CanonicalizerResult.followUpPlan` содержит `aggregateMentions` (orphan-фрагменты), `deferredResearch` (unknown products/relations/transforms/synonyms + dictionary warnings) и `knownAnchors` (топ-фрагменты по инцидентности). `runner_cli.py --followup-from PRIOR.json` вшивает это в следующий запрос как `question.priorFollowUpPlan`, и аналитики используют `knownAnchors` вместо повторного discovery.
- **Schema versioning**: `CanonicalizerResult.schemaVersion=2` и `SessionPack.schema_version=2`; старые блобы без новых полей валидируются (тест `test_session_pack_backward_compat_empty_fields`).
- **Critic OpenRouter fallback** (временный, см. Roadmap ниже): критик пробует Vertex первым, при 429 / невалидном JSON проваливается в OpenRouter-цепочку `openai/o4-mini (reasoning.effort=low) → z-ai/glm-5.1 → deepseek/deepseek-chat-v3.1 → nvidia/nemotron-3-super-120b-a12b` (env `AGENTHORDE_CRITIC_FALLBACKS`). Порядок закреплён бенчем `scripts/bench_critic_models.py` на апрель 2026: o4-mini лидирует по скорости (≈6s), цене (~$0.003/вызов) и строгости schema. Каждая попытка логируется построчно в `AGENTHORDE_CRITIC_AB_LOG` (JSONL) для A/B-сравнения моделей на одном запросе.

## Бизнес-смыслы: какие артефакты цепочек поставок находит система

Речь про **смысловые кирпичики цепочек поставок**, которые система в таком виде (агенты + поиск + граф состояния) **может вытащить и зафиксировать** — не про поля JSON, а про **что именно по бизнес-смыслу** получается на выходе.

### 1. Продукт и границы цепи

- **Что везём / что перерабатываем** (сырьё, полуфабрикат, готовая химия, контейнерный SKU и т.д.).
- **Границы анализа**: от какого этапа «считаем цепь» (рудник / солончак / НПЗ / порт / завод) и до куда (причал в ЕС, переработчик, OEM).
- **Сегменты рынка**: spot vs долгосрок, контракт vs спот, стратегический запас.

*Артефакт по смыслу:* **определение объекта цепи и scope** (без этого нельзя сравнивать альтернативы).

### 2. География и топология (узлы и «ноги»)

- **Географические узлы**: страны, регионы, **конкретные месторождения / солончаки / заводы / хабы**.
- **Транспортные узлы**: **порты**, терминалы, **ICD**, ж/д узлы, трубопроводные точки (если релевантно).
- **Ноги маршрута**: море / река / ж/д / авто; **именованные коридоры** (напр. через Малакку vs обход Ломбок).
- **Направление потока**: who → whom (регион экспорта → регион импорта), без обязательной привязки к одному контракту.

*Артефакт:* **топология цепи** — набор **nodes + legs** (даже если пока без точных объёмов).

### 3. Участники рынка (именованные акторы)

- **Добытчики / переработчики** (NOC, горнодобывающие, химические гиганты).
- **Трейдеры / оффтейкеры** (кто фиксирует объёмы).
- **Перевозчики**: **линии**, **чартереры**, типы судов (VLCC, Valemax и т.д.).
- **Покупатели / OEM** (автопроизводители, металлурги, государственные импортёры).
- **Инфраструктурные операторы** терминалов (напр. выделенный рудный хаб).

*Артефакт:* **ролевая карта цепи** — кто на каком звене «имеет имя» и какую роль играет.

### 4. Физические и инженерные ограничения

- **Ограничения по осадке / габариту** (Malacca-max, обход).
- **Узкие места пропускной способности**: проливы, каналы, **плотность судоходства**.
- **Перегрузка / консолидация**: хаб, где крупное судно → мелкое.
- **Ёмкость и очереди** (время ожидания у причала, волна из другого кризиса — напр. Красное море).

*Артефакт:* **инженерно-логистический constraint layer** поверх карты.

### 5. Коммерция и контрактная логика

- **Инкотермы и типовые схемы** (FOB/CIF/CFR/CIP и т.д.) — кто несёт фрахт, страхование, риск.
- **Форма закупки**: долгосрок с NOC, спот, тендер.
- **Ценовые и количественные якоря** (если модель их назвала — всегда как **кандидат на верификацию**).

*Артефакт:* **коммерческий контур** — как риск и стоимость «ездят» по цепи.

### 6. Регуляторика, комплаенс, ESG

- **Экспортный контроль, санкции, таможенные коды** (если тема чувствительная).
- **Цепочка due diligence** (напр. LkSG, EU Battery Regulation, CRMA — как классы требований).
- **Экологические и социальные ограничения** (вода на солончаке, протесты, квоты).

*Артефакт:* **regulatory overlay** — не замена юристу, а **список применимых режимов и триггеров**.

### 7. Риски и сценарии

- **Геополитика** (зависимость от пролива, «дилемма», альтернативные маршруты).
- **Страхование**: war risk, HRA, кто покупает полис.
- **Операционные сбои** (заторы, аварии, погода).
- **Репутационные / compliance-риски** (теневой флот, спорные юрисдикции).

*Артефакт:* **risk register** с привязкой к звеньям цепи.

### 8. Доказательности и уровень уверенности

- **Утверждения с опорой на поиск** (URL, домен, краткая выжимка).
- **Утверждения «из памяти модели»** без свежего источника.
- **Явно отмеченные пробелы**: «что не знаем», «что устарело», «что надо сверить».

*Артефакт:* **evidence-attributed claims** + **open verification queue**.

### 9. Гипотезы и ветвление смысла

- **Конкурирующие интерпретации** (напр. другой коридор, другой поставщик, политический сценарий).
- **Гипотезы vs факты** (в т.ч. отдельным типом узла или флагом).

*Артефакт:* **hypothesis branches** для сценарного анализа и merge-политики.

### 10. Синтез для решения

- **Сквозное резюме цепи** в связном тексте (executive chain narrative).
- **Список открытых вопросов** к аналитикам, юристам, закупкам.
- **Приёмочное заключение** (достаточно ли ответили на вопрос; что добрать).

*Артефакт:* **brief + backlog + quality verdict** — то, что обычно идёт в заседание или в RFI.

### Сводка

Система в целом находит и может материализовать:

1. **Объект и границы цепи**
2. **Топологию** (узлы, ноги, коридоры, обходы)
3. **Именованных участников** по ролям
4. **Физические ограничения** (осадка, choke points, хабы перегрузки)
5. **Коммерческую обвязку** (инкотермы, кто несёт риск/страхование)
6. **Регуляторный и ESG-слой**
7. **Риски и сценарии**
8. **Доказательную базу** (и очередь верификации)
9. **Гипотезы и альтернативные ветки**
10. **Итоговый бриф + приёмка + пробелы**

Именно это потом ложится на системные артефакты вида: **ProductFlow**, **Leg**, **Actor**, **Constraint**, **ContractPattern**, **Regime**, **RiskItem**, **EvidenceRef**, **HypothesisBranch**, **InvestigationBrief** — как принято в каноне продукта.

## Типы сущностей (онтология «что извлекается»)

Ниже — **классы узлов** (сущностей), которые в принципе всплывают в ответах агентов и в `toolLedger` при прогонах `runner_cli.py`: **supply chain** (фикстура в духе `malacca_strait_resources.json`) и **governance & control** (вопросы в духе структуры собственности G42). Это **инвентарь для маппинга на резолвер / граф**, а не список верифицированных фактов.

### Сущности, подтипы и связи (каркас)

- **Не смешивать** «что за объект» и «как он связан с другим»: **доли, даты сделки, роль в совете, voting vs economic** — это атрибуты **связи** (ребра) или отдельного факта-события, а не повод плодить новый класс сущности под каждую комбинацию. Семейство типов рёбер описано в разделе **«Типы связей в модели (supply chain + governance)»** (в т.ч. п. **9** для капитала и контроля; атрибуты вроде `pct`, `as_of`, `source` на ребре).
- **Каркас онтологии** — узкий набор **базовых классов** в таблице; **нюансы** — через **подтип** узла (`subtype` / уточняющий тег: `listed_issuer`, `swf`, `spv_rsc`, …) и/или через **ребро** («A владеет 42 % B» → два узла `legal_person`, одно ребро `owns` с `pct=42`).

---

### Домен: supply chain и морская логистика (Malacca-style)

#### Гидрография и маршруты

- **Пролив / узкое место:** Малакка, Ломбок, Зонд
- **Смежные коридоры (контекст):** Ормуз, Красное море, Суэц, обход мыса Доброй Надежды
- **Акватории:** SOMS (Singapore and Malacca Straits), Южно-Кинайское море
- **Океаны / макрорегионы:** Индийский океан, Тихий океан
- **Торговые направления:** Intra-Asia, Asia–Europe

#### География политическая и административная

- **Государства:** КНР, Япония, Республика Корея, Индия, Россия, страны Персидского залива (SA, UAE, KW, IQ, QA и др.), Австралия, Бразилия, Индонезия, Малайзия, Сингапур (город-государство), США, Мьянма, Таиланд, Пакистан (в т.ч. через CPEC)

#### Порты, хабы, инфраструктура

- **Порты / хабы:** Сингапур, Port Klang, Tanjung Pelepas, Dumai
- **Стратегическая география:** Андаманские и Никобарские острова (Индия)
- **Крупные проекты (как тренды в тексте):** CPEC, Power of Siberia 2, трубопроводы в Мьянме, Thai Land Bridge

#### Компании и группы компаний

- **Нефть / NOC и мажоры:** Sinopec (Unipec), Chevron, Shell, ExxonMobil, TotalEnergies, Petronas, Indian Oil Corp, Reliance Industries
- **Трейдеры:** Trafigura, Vitol, Glencore (в т.ч. ST Shipping)
- **Контейнерные линии:** MSC, Maersk, CMA CGM, COSCO Shipping, Hapag-Lloyd, ONE, Evergreen
- **Судоходные / bulk-операторы:** MOL, NYK, «K» Line, Winning International Group, CMES Shipping, SK Shipping
- **Горнодобывающие / bulk-заказчики:** Rio Tinto, BHP, Vale
- **Картель / группа:** OPEC

#### Суда и классы флота

- **Танкеры:** VLCC, Aframax
- **Сухогрузы:** Capesize, Panamax, bulk carriers
- **Контейнеровозы:** ULCS и прочие контейнерные классы
- **LNG:** LNG carriers, потоки LNG
- **Прочее:** «shadow fleet» (категория схем/тоннажа в чувствительном контексте)

#### Товары и потоки ресурсов

- **Углеводороды:** сырая нефть, LNG
- **Руды и bulk:** железная руда, бокситы, уголь (thermal coal)
- **Контейнерные грузы:** manufactured goods, electronics, intermediate components

#### Рынки, метрики, страхование

- **Объёмы и доли по рынку трафика:** % торговли/трафика, bpd, TEU, transits, доли стран в руде/угле — как правило **метрика на ребре** «поток / коридор» или атрибут сценария, а не отдельный тип «процента вообще»
- **Фрахт:** SCFI, сравнения с pre-pandemic, квартальная динамика
- **Страхование:** war-risk premium, % hull value, Notice of Cancellation (NOC)
- **Классификация риска:** JWC (Joint War Committee), elevated risk area
- **Концепции:** Malacca dilemma, Red Sea crisis / diversions, naval militarization, major-power competition
- **Политико-экономические рамки:** Belt and Road (контекст торговли)

#### Безопасность и инциденты

- **Кооперация:** совместные патрули Индонезия–Малайзия–Сингапур
- **Техника:** autonomous unmanned surface vessels (надзор)
- **Инциденты:** piracy, armed robbery, opportunistic thefts, boardings
- **Военно-морские акторы:** присутствие ВМС США, Индии, Китая в регионе

---

### Домен: governance и владение (G42-style)

Базовый класс чаще всего **`legal_person`** (юрлицо) или **`natural_person`** (физлицо); «чей это инвестор» — **подтип** организации, а не отдельная сущность «доля».

- **`natural_person` / ключевое физлицо:** UBO, председатель, номинальный контролёр (пример прогона: фигура в роли UBO и chair)
- **`legal_person` (каркас) и типичные подтипы:** `private_holdco`, `listed_issuer`, `operating_subsidiary`, `spv` / `restricted_scope_company` (RSC), `sovereign_wealth_fund`, `private_equity_firm`, `strategic_corporate_investor`, `family_office`, `private_conglomerate`, `state_linked_operating_company` (примеры: холдинг группы, дочки облака/данных, листинги на ADX, SPV владения, Mubadala, Silver Lake, Microsoft, семейный офис, Royal Group-тип)
- **`fund` / инвестиционная программа** — подтип `/org` (`fund`): вроде `state_backed_ai_fund`, `divestment_or_transition_fund` (пример: совместный ИИ-фонд, фонд в сюжете реструктуризации портфеля)
- **`asset_manager` / allocator** — подтип `/org` (`asset_manager`): когда в тексте важен именно **менеджер** активов после передачи фонда (пример сюжета: переход управления с одной структуры на другую при том же UBO-контуре)
- **`listing_venue`** — подтип `/org` (`exchange`); **`security` / тикер** → `/market/ticker/{exchange}/{symbol}` — отдельно от «компании как эмитента»
- **`financial_center_freezone`** — подтип `/geo` (`freezone`): субнациональная зона права (пример: ADGM как юрисдикция инкорпорации холдинга)
- **Совместные предприятия:** по умолчанию моделировать как **два (или более) `legal_person` + ребро** `joint_venture_with` с долями и датой на ребре; отдельный узел «JV» — только если нужен якорь для нескольких связанных событий
- **Сделки и раунды:** сумма инвестиции, выкуп миноритария, слияние — чаще **`corporate_transaction`** как узел-событие с рёбрами к участникам *или* атрибутированное ребро `invested_in` / `acquired_stake` между юрлицами (не дублировать и то и другое без нужды)

---

### Узлы графа рассуждения (`proposedNodes`)

Схема в коде допускает виды: **`hypothesis_anchor`**, **`claim`**, **`question`**, **`tool_evidence`**, **`counter`**, **`mission`**. В README ранее перечислялись не все; для обоих доменов актуально:

- **`hypothesis_anchor`** — рамочная интерпретация / сценарий
- **`claim`** — атомарное утверждение (цепь, порт, доля, UBO, дочка)
- **`question`** — явный запрос на верификацию или дозагрузку
- **`tool_evidence`**, **`counter`**, **`mission`** — привязка к поиску, альтернативная ветка, миссия цикла

### Источники (не узлы предметной онтологии)

- **Supply chain / море:** eia.gov, ballastmarkets.com, freightender.com, seatrade-maritime.com, lloydslist.com, straitstimes.com, insurancejournal.com и т.п. — **EvidenceRef** / publisher
- **Governance / cap table:** сайт эмитента, **биржевые** материалы (в т.ч. ADX), регуляторные раскрытия, **pitchbook / preqin**-класс баз, крупная пресса и отраслевые издания, корпоративные IR-страницы — тот же слой **EvidenceRef**, другой типичный набор доменов

### Сводная таблица классов (каркас + домен прогона)

Столбец **Канон. путь** — первый сегмент из замкнутого enum (11 классов; полный реестр — в разделе «Канонический путь» ниже). **Подтип** передаётся вторым сегментом или атрибутом узла, **не** отдельным первым сегментом.

| Канон. путь | Подтипы (примеры) | Домен | Примеры из прогонов |
|-------------|-------------------|--------|----------------------|
| `/org` | `oil_trader`, `shipping_line`, `miner`, `noc_major` | supply | Unipec, Shell, MSC, Rio Tinto, … |
| `/org` | `private_holdco`, `listed_issuer`, `operating_subsidiary`, `spv`, `swf`, `pe_firm`, `strategic_investor`, `family_office`, `asset_manager` | governance | G42 holdco, Core42, Presight, Mubadala, Silver Lake, Microsoft, … |
| `/org` | `gov_body` | supply / governance | регуляторы, министерства |
| `/org` | `fund` (`state_backed_ai_fund`, `transition_fund`) | governance | совместный ИИ-фонд, фонд в сюжете дайвеста |
| `/org` | `exchange` | governance | ADX, LSE, NASDAQ |
| `/person` | `ubo`, `chair`, `director` | governance | UBO / chair в сюжете владения |
| `/geo` | `strait`, `sea`, `ocean` | supply | Malacca, Lombok, Sunda, Hormuz, Red Sea, SOMS, SCS |
| `/geo` | `canal`, `cape` | supply | Suez, Cape of Good Hope |
| `/geo` | `country`, `city_state` | supply / governance | CN, JP, Gulf states, UAE … |
| `/geo` | `archipelago`, `isle` | supply | Andaman & Nicobar |
| `/geo` | `freezone`, `financial_center` | governance | ADGM, DIFC |
| `/site` | `port` (`container`, `bulk`, `lng`, `mixed`) | supply | Singapore, Port Klang, Tanjung Pelepas, Dumai |
| `/site` | `facility` (`refinery`, `smelter`, `terminal`) | supply | — |
| `/site` | `deposit` | supply | — |
| `/product` | `commodity`, `material`, `intermediate` | supply | crude, iron ore, LNG, containers, … |
| `/vessel_class` | `tanker`, `bulker`, `container`, `lng_carrier` | supply | VLCC, Capesize, ULCS, … |
| `/route` | `corridor` | supply | Asia–Europe, Intra-Asia |
| `/route` | `leg` (`sea`, `rail`, `road`, `pipeline`) | supply | — |
| `/market` | `index`, `benchmark` | supply | bpd, TEU, SCFI, hull % |
| `/market` | `ticker` | governance | PRESIGHT, SPACE42 |
| `/regime` | `regulatory` | supply | JWC, EU CBAM, санкционный список |
| `/regime` | `risk` | supply | Malacca dilemma, piracy, war_risk |
| `/regime` | `incoterm` | supply | FOB, CIF, … |
| `/project` | `infrastructure` | supply | CPEC, Thai Land Bridge, … |
| `/project` | `programme` | supply / governance | BRI, нац. стратегии |
| `/project` | `operation` | supply | совместные патрули ID–MY–SG |
| `/evidence` | `source` | оба | домены из `web_search` grounding |
| `/evidence` | `claim` | оба | реифицированные утверждения |

## Канонический путь (экспериментальная таксономия)

**Канонический путь** — многосегментный идентификатор сущности. **Первый сегмент** задаёт **класс объекта** (замкнутый enum — полный реестр ниже, он же используется в столбце «Канон. путь» сводной таблицы классов выше). **Второй и далее** — уточнение (часто **ISO2** для «привязанных к земле» сущностей; для продуктов — **стадия передела / подвид / спецификация**, а не страна как «вторая буква»).

### Полный реестр первых сегментов (замкнутый enum — 12 классов)

Если объект не ложится ни на один из 12 — это кандидат на расширение enum, а не повод вкладывать его ad hoc в существующий путь.

| # | Префикс | Что покрывает | 2-й сегмент (типичный) | Примеры подтипов |
|---|---------|---------------|------------------------|------------------|
| 1 | `/org` | Любое юрлицо, госорган, фонд, биржа | ISO2 юрисдикции / HQ | `private_holdco`, `listed_issuer`, `spv`, `swf`, `pe_firm`, `family_office`, `noc`, `shipping_line`, `miner`, `gov_body`, `fund`, `exchange`, `asset_manager` |
| 2 | `/person` | Физическое лицо (UBO, director, chair) | ISO2 гражданства / резидентства | `ubo`, `chair`, `director`, `nominee` |
| 3 | `/geo` | География: страна, пролив, море, канал, мыс, freezone | ISO2 или нормализованный slug | `country`, `city_state`, `strait`, `sea`, `ocean`, `canal`, `cape`, `archipelago`, `isle`, `region`, `freezone`, `financial_center` |
| 4 | `/site` | Физическая точка: порт, завод, месторождение | ISO2 → город-slug или LOCODE | `port` (`container`, `bulk`, `lng`, `mixed`), `facility` (`refinery`, `smelter`, `terminal`), `deposit` |
| 5 | `/product` | Товар / материал / номенклатура | `{family}/{slug}` | см. схему ниже |
| 6 | `/vessel_class` | Класс судна | контролируемый словарь / IMO | `tanker`, `bulker`, `container`, `lng_carrier` |
| 7 | `/route` | Маршрут, коридор, нога перевозки | режим или slug коридора | `corridor`, `leg` (`sea`, `rail`, `road`, `air`, `pipeline`) |
| 8 | `/market` | Рыночный инструмент, индекс, тикер | провайдер + код | `index`, `benchmark`, `ticker` |
| 9 | `/regime` | Нормативная рамка: режим, риск, инкотерм | юрисдикция → код | `regulatory`, `risk`, `incoterm` |
| 10 | `/project` | Инициатива: проект, программа, операция | `transnational` или ISO2 → slug | `infrastructure`, `programme`, `operation` |
| 11 | `/evidence` | Мета-слой: источник, утверждение | домен / hash | `source`, `claim` |
| 12 | `/community` | Неформальная социальная сущность без юр.регистрации | ISO2 → подтип → slug | `dynasty` (chaebol-семьи, royal houses), `tribe` (кланы, племена), `industry-association`, `representatives`, `activist`, `professional`, `religious`, `criminal`, `ethnic`, `interest`, `trade`, `key-community` |

#### Зачем `/community` отдельно от `/org` и `/person`

`/org` — юр.лицо с регистрацией (LLC, Inc., Ltd., JSC, агентство). `/person` — один названный человек. `/community` — мульти-индивидная социальная сущность без юр.регистрации, которая *использует* организации и каналы, но сама не является юр.вехилом. Канонический пример: семья Lee коллективно контролирует чеболь Samsung через серию холдингов — `/community/kr/dynasty/lee-family` `controls_via` → `/org/kr/samsung-c-and-t`. Holdco остаётся отдельной `/org`, семья как сущность — `/community/dynasty`. Зеркалирует core canonical control plane (`backend/internal/horizon/scout/canonicalizer.go` slot ordering и `entity_semantics.EntityTypeCommunityDynasty / Tribe`).

Путь `/community/{geo}/{subtype}/{slug}` — три семантических слота после класса. Подтип — это **отдельный сегмент**, не префикс slug.

#### Зачем `/site` отдельно от `/geo`

`/geo` — абстрактная география (страна, пролив, море). `/site` — конкретная физическая точка эксплуатации (порт, завод, месторождение). У одного `/site` может быть несколько операторов (`/org`), а один `/geo` может содержать множество `/site`. Связь между ними — ребро `located_in`.

### Схема для `/product`

`/product/{family}/{slug}`

- **`family`:** `lithium`, `crude`, `coal`, `battery`, `semiconductor`, `steel`, …  
- **`slug`:** конкретный продукт: `spodumene-concentrate`, `lithium-carbonate`, `battery-cell`, `thermal-coal`, …

Стадия передела (raw → intermediate → finished) **не кодируется в пути** — она выводится из позиции продукта в цепочке площадок (см. «Продуктовые трансформации: site-centric модель»). Географию цепи к продукту вешать **ребрами**, а не смешивать с классом продукта.

### Правила целостности пути

1. **Первый сегмент** — строго из **enum выше** (11 классов).  
2. **Второй сегмент** — **подтип или уточнитель**: ISO2 для привязанных к территории; код подвида для остальных.  
3. **Дальше** — только **сужение** (город, slug, код, стадия).  
4. **Не вкладывать** одну сущность в путь другой: юрлицо не в `/site`, физлицо не в `/org` — только **ребро** (`operated_by`, `beneficially_owns`, …).

## Типы связей (6 слоёв)

Связи — **направленные рёбра** между узлами из 11 классов выше. Инверсную связь отдельно не именуем: `subsidiary_of` A→B — это и есть «B parent of A» (направление хранится на ребре). **Доли, суммы, даты, source** — атрибуты ребра, а не отдельные узлы.

### Атрибут `layer` на ребре

Одна и та же связь (например `supplies`) может описывать разные уровни реальности. Вместо отдельных графов — **один граф**, но каждое ребро несёт атрибут **`layer`**:

| `layer` | Что означает | Темп изменений | Пример |
|---------|-------------|----------------|--------|
| `structural` | Возможность / топология: маршрут существует, порт способен принять, компания в принципе торгует | месяцы–годы | `leg_connects` SG→Rotterdam, `operates` PSA→Singapore port |
| `contractual` | Обязательство: offtake, charter party, долгосрочный контракт | недели–годы | `supplies` Trafigura→Sinopec (5-year offtake, 2M t/y) |
| `flow` | Факт: конкретная отгрузка, рейс, инцидент | однократно | `supplies` Trafigura→Sinopec (март 2025, 80k t, VLCC «Xxx») |

Если `layer` не указан, ребро считается `structural`. Аналогия с governance: `subsidiary_of` = `structural`, корпоративное событие (`invested_in`) = `flow`.

### 1. Материальный поток и топология

Физическое движение товара от `/site` к `/site` через `/route`. Трансформация продукта — **свойство узла `/site`**, а не ребро между `/product` (см. раздел «Продуктовые трансформации: site-centric модель»).

| Связь | A → B | Смысл | Допустимые `layer` | Типичные атрибуты ребра |
|--------|-------|--------|-------------------|-------------------------|
| `supplies` | `/site` → `/site` | Поставка **одного** продукта между площадками (атомарное ребро); несколько товаров между той же парой — **несколько** `supplies` | structural, contractual, flow | Обязательно: `product` (путь) и после канонизации `product_fragment_id` → id узла `/product`. Опционально: `incoterm` ∈ Incoterms 2020 (`FOB`, `CIF`, `CFR`, `FCA`, `DAP`, ...). Тот же `incoterm`-атрибут допустим на `charters` |
| `leg_connects` | `/site` → `/site` \| `/route` \| `/geo` или `/route` → `/site` \| `/geo` | Нога перевозки: две площадки, площадка ↔ коридор, коридор ↔ площадка/регион | structural, flow | `mode`, `carrier` → `/org`, `direction` |
| `transships_at` | `/route` → `/site` или `/route` → `/geo` | Смена судна или режима в хабе | structural, flow | — |
| `passes_through` | `/route` → `/geo` \| `/site` \| `/route` или `/vessel_class` → `/route` \| `/site` \| `/geo` | Маршрут или класс судна проходит через пролив, канал, choke point, или другой route-узел | structural | — |
| `alternate_to` | `/route` → `/route` или `/site` → `/site` | Сценарная альтернатива (тот же O–D, другой путь / узел) | structural | `delta_cost`, `delta_days` |
| ~~`co_shipped`~~ *(удалено)* | — | Совместная перевозка: несколько атомарных `supplies` между той же парой site с разным `product` / `product_fragment_id` | flow | — |

Связи `originates_at` / `terminates_at` / `transforms_into` **убраны**: начало и конец цепочки определяются крайними `/site` в графе; transform — атрибут узла `/site`.

#### Проекции supply-chain (`kind`-лейблы)

Одна relation из закрытого словаря может жить на **разных уровнях абстракции** — physical, commercial, actor-level, aggregate. Канонизатор не скрывает и не сливает эти уровни: каждому признанному `(from_class, to_class)` срезу соответствует явный семантический `kind`-лейбл, который стемпится в `CanonicalEdge.attributes.kind` в phase 4.

Пример для `supplies`:

| endpoint-пара | `kind` | что это значит |
|---------------|--------|----------------|
| `/site` → `/site` | `physical_leg` | физическая нога цепочки (Ras Tanura → Qingdao) |
| `/org` → `/org` | `commercial` | контракт seller→buyer (Saudi Aramco → Unipec) |
| `/org` → `/geo` | `actor_to_region` | актор на рынок региона (Saudi Aramco → China) |
| `/org` → `/route` | `actor_via_corridor` | актор оперирует через коридор (Unipec via Malacca) |
| `/geo` → `/geo` | `trade_flow` | макро-поток между регионами (Australia → China iron ore) |

Полный каталог проекций для всех relation закрытого словаря и blacklist нонсенсных пар — в [docs/horizon/architecture/supply-chain-relation-endpoint-kinds.md](../../../docs/horizon/architecture/supply-chain-relation-endpoint-kinds.md).

**Трёх-уровневая endpoint-валидация:**

- **recognized** — пара в каталоге → ребро с `attributes.kind = <label>`.
- **unspecified** — пара **не** в каталоге и **не** в blacklist → ребро с `attributes.kind = "unspecified"` + запись в `candidates.unknown_relations` (reason `endpoint_pair_not_in_catalog`) + soft warning в `validationReport.relationKindWarnings`. Downstream видит факт, не теряя информации; каталог может быть расширен post-hoc.
- **forbidden** — пара в blacklist → ребро **не** строится, запись в `candidates.unknown_relations` (reason `forbidden_endpoint_pair`) + hard error в `validationReport.relationErrors` (code `relation_endpoints_forbidden`).

Агентские промпты ничего не знают об этой системе — весь type-матчинг живёт в канонизаторе (separation principle: «agent reasons — canonicalizer types»).

### 2. Акторы и операции

Кто что делает на цепочке. Субъект — всегда `/org` (или `/org/gov_body`). Ребро `supplies` между площадками (§1) описывает **что** течёт; связи ниже — **кто** стоит за площадкой.

| Связь | A → B | Смысл | Допустимые `layer` | Типичные атрибуты |
|--------|-------|--------|-------------------|-------------------|
| `operates` | `/org` → `/site` | Оператор порта, завода, месторождения | structural | `role` (`terminal_operator`, `concessionaire`, …) |
| `charters` | `/org` → `/vessel_class` | Заказчик фрахтует тоннаж | contractual, flow | `charter_type` (`time`, `voyage`, `coa`), `period` |
| `regulates` | `/org` → `/geo` или `/site` | Госорган к зоне или объекту | structural | `regime` → `/regime` |

### 3. Ограничения, режимы и риски

Всё, что лимитирует, обуславливает или угрожает звеньям цепочки. Почти всегда `structural` — ограничения и режимы меняются медленно.

| Связь | A → B | Смысл | Допустимые `layer` | Типичные атрибуты |
|--------|-------|--------|-------------------|-------------------|
| `constrained_by` | `/site` \| `/vessel_class` \| `/route` → `/geo` \| `/route` \| `/site` | Осадка, габарит, правила пролива, пропускная способность choke point | structural | `parameter`, `limit_value` |
| `requires_capability` | `/product` → `/site` | Тип груза требует оборудования терминала | structural | `capability` (`lng_regas`, `deep_draft`, …) |
| `subject_to` | любой → `/regime` | Узел, сделка или поток под нормативным режимом | structural, flow | `since`, `status` |
| `exposes_to` | любой → `/regime/risk` | Звено подвержено риску | structural | `severity`, `probability` |
| `mitigated_by` | `/regime/risk` → `/route` или `/org` | Мера или альтернатива снижает риск | structural | `residual_risk` |
| `priced_under` | `/route` или leg → `/regime/incoterm` | Сделка / нога под инкотермом | contractual, flow | — |
| `benchmarked_to` | `/product` или leg → `/market` | Поток привязан к индексу | contractual | — |

### 4. Владение и контроль

Кто кем владеет, кто контролирует, где зарегистрирован. Субъекты: `/org`, `/person`. Объекты: `/org`, `/geo/freezone`, `/market/ticker`. По умолчанию `structural`; события — `flow`.

| Связь | A → B | Смысл | Допустимые `layer` | Типичные атрибуты |
|--------|-------|--------|-------------------|-------------------|
| `subsidiary_of` | `/org` → `/org` | Дочка → материнская (холдинг, SPV) | structural | `pct`, `economic_pct`, `voting_pct` |
| `beneficially_owns` | `/person` → `/org` | UBO / конечный бенефициар | structural | `pct`, `mechanism` |
| `controls_via` | `/person` или `/org` → `/org` | Де-факто контроль: `board_chair`, `veto`, `golden_share`, `shareholder_agreement` | structural | `mechanism`, `source` |
| `holds_board_seat` | `/org` или `/person` → `/org` | Место в совете / номинация директора | structural | `role` (`chair`, `independent`, `nominee`) |
| `joint_venture_with` | `/org` → `/org` | СП; доли на ребре | structural | `pct`, `as_of` |
| `listed_on` | `/org` → `/market/ticker` | Эмитент → тикер на площадке | structural | `exchange`, `instrument_class` |
| `incorporated_in` | `/org` → `/geo` | Юрлицо → юрисдикция регистрации | structural | `zone` (`freezone`, `onshore`) |
| `managed_by` | `/org` → `/org` | Фонд / программа → управляющий | structural | — |

**Каталог (слой `layer_section: catalog`):**

| Связь | A → B | Смысл | `layer` | Примечание |
|--------|-------|--------|---------|------------|
| `produces` | `/org` → `/product` | Организация выпускает линейку продукта (class-level, не SKU) | structural | Отдельный разрез от supply-chain; тот же узел `/product` используется в `supplies` как cargo |

#### Корпоративные события (`layer: flow`, порождают / меняют structural-рёбра §4)

Моделируются как ребро с `layer: flow` и `event_type` + `as_of`, либо как узел-событие `/evidence/claim` kind `corporate_event`.

| Событие (`event_type`) | Смысл | Что порождает / меняет |
|---------|--------|---------------|
| `invested_in` | Денежный раунд / стратегическая инвестиция | `subsidiary_of` с `pct` |
| `acquired_stake_from` | Выкуп доли у третьего лица | изменение `pct` на `subsidiary_of` |
| `asset_contribution` | Вклад активов в обмен на долю | новый `subsidiary_of` |
| `merged_into` | Слияние дочерних платформ | удаление одного `/org`, перенос рёбер |
| `spun_out` | Выделение / IPO | новый `/org` + `listed_on` |
| `divested_to` | Передача портфеля / мандата | смена `managed_by` или `subsidiary_of` |

### 5. Доказательность

Мета-слой выведен **из графа** в `EvidenceArchive` (см. «Evidence archive» ниже). В `graph.edges` **нет** больше `asserted_by` / `supported_by` / `contradicts` / `under_hypothesis`, и в `graph.fragments` нет `/evidence/claim`, `/evidence/counter`, `/evidence/hypothesis` анкоров.

Связь claim ↔ бизнес-ребро сохраняется через `CanonicalEdge.sourceClaimIds` (список `n*` id). Связь claim ↔ источник, counter ↔ claim, claim ↔ hypothesis — живут в `evidenceArchive.links` / `evidenceArchive.counters[*].contradicts_claim_id` / `evidenceArchive.claims[*].hypothesis_id`. Имена отношений `asserted_by`, `supported_by`, `contradicts`, `under_hypothesis` остаются в словаре ради обратной совместимости, но phase 7 их не эмитит.

### Сводка

| | Supply chain | Governance | Типичный `layer` |
|-|-------------|------------|------------------|
| §1 Поток и топология | основной | — | structural + flow |
| §2 Акторы и операции | основной | частично (`operates`) | structural + contractual + flow |
| §3 Ограничения и режимы | основной | частично (`subject_to` / санкции) | structural |
| §4 Владение и контроль | частично (концессии) | основной | structural; события = flow |
| §5 Доказательность | оба | оба | — (мета) |

**Принцип:** не смешивать `passes_through` (физика) с `asserted_by` (мета); `structural` ребро и `flow` ребро с одним и тем же именем — это разные утверждения о мире; в governance — не подменять `subsidiary_of` (`structural`) событием `invested_in` (`flow`) без разделения; transform — свойство `/site`, а не ребро между `/product` (canonical transform dictionary — constraint, не элемент графа).

## Архитектура: агенты рассуждают, канонизатор строит граф

### Принцип разделения фаз

AgentHorde работает в **две фазы**, и они не смешиваются.

**Фаза 1 — исследование (агенты).** Supervisor гоняет worker-агентов (`supply_chain_analyzer`, `governance_control_analyzer`, critic, red team). Агенты ищут данные через `web_search` (grounded в Vertex), рассуждают, выдвигают гипотезы, проверяют их. Результат — `reasoningGraph` из свободных `claim` / `hypothesis_anchor` / `question` / `tool_evidence` нод. Агенты **не фокусируются на таксономии**: их цель — рассуждать, а не расставлять canonical paths. «G42», «Group 42», «G42 Ltd» могут появляться как разные упоминания — это нормально.

**Фаза 2 — канонизация (один проход после `finalize`).** Канонизатор получает полный `reasoningGraph` + `finalAnswer` и за один проход:

1. **Дедуплицирует** сущности (G42 = Group 42 = G42 Ltd → один `/org/ae/g42`)
2. **Типизирует** — из свободного текста claim выделяет узлы с canonical paths из 11 классов
3. **Строит граф связей** — из текста «Mubadala owns majority stake in G42» делает `subsidiary_of` ребро с `pct`, `layer: structural`, `source`
4. **Привязывает transform** к `/site` — проверяет по canonical transform dictionary
5. **Структурирует метрики** — «42% stake» → structured metric с confidence и provenance
6. **Валидирует** — canonical paths, relation types, product slugs

Канонизатор — **гибрид LLM + детерминированный код**: LLM для NER и relation extraction из свободного текста (код не поймёт из контекста, что «Sheikh Tahnoon» — UBO); код для дедупликации, canonical path resolution, lookup в словарях синонимов/трансформов и финальной валидации.

### Почему не на лету

- **Полный контекст.** Канонизатор видит весь результат целиком — может дедуплицировать по-настоящему, а не по одному claim.
- **Агент не теряет фокус.** Каждый токен на каноническое форматирование — это токен, не потраченный на рассуждение.
- **Нет петли.** На лету канонизация ×N циклов = N × латентность. Один проход в конце = 1 × латентность.
- **Нет конфликта имён.** Если канонизатор на лету переименовал сущность, а агент в следующем цикле ищет по старому имени — хаос.
- **Отладка.** Сырой output агента и канонизированный граф — две отдельные вещи. Можно сравнить, найти потери.

### Что на выходе канонизатора

Структурированный граф, готовый к записи в ядро: узлы с canonical paths, типизированные рёбра с атрибутами и `layer`, structured metrics с provenance. Это то, что раньше строил детерминированный пайплайн ядра — теперь строит агентская среда + канонизатор.

### Agent contract: `proposedEntities` + `proposedRelations` (по id)

Помимо `proposedNodes` (только reasoning: claim / question / …), каждый analyzer эмитит:

1. **`proposedEntities`** — реестр объектов: `{ "id": "e1", "label": "…" }`.  
   `label` — **дословная** строка из `chainSummary` или `proposedNodes[].text` (одно и то же написание везде). Типы сущностей агент **не** задаёт — их выводит канонизатор (phase 1) из narrative.
2. **`proposedRelations`** — рёбра **только между id из реестра**: `{ "srcId": "e1", "dstId": "e2", "relation": "produces", … }`.  
   Никаких natural-language строк в `srcId`/`dstId`. Supervisor при мерже префиксует id как `analyzer:e1`, `governance:e1`, `sanctions:e1`, чтобы `parallel_fanout` не коллидировал.

Пример строки relation:

```json
{
  "srcId": "e1",
  "dstId": "e2",
  "relation": "produces",
  "attributes": { "as_of": "2024-01-01" },
  "sourceClaimIds": ["n1"]
}
```

- `relation` — закрытый список AH (`supplies`, `produces`, `operates`, `transships_at`, `passes_through`, `listed_on`, `subsidiary_of`, `beneficially_owns`, ...).
- Для `supplies` обязателен `attributes.product` (surface cargo) и опционален `attributes.incoterm` (Incoterms 2020).
- Несколько cargo между одной парой site — **несколько** `supplies`-строк (multi-edge, не массив).

Путь данных:

1. `_merge_analyzer_into_pack` кладёт `proposedEntities` и `proposedRelations` в `SessionPack` с дедупом relations по `(srcId, dstId, relation)` (ids уже namespaced).
2. Phase 0 собирает реестр и relations в `WorkingState.analyzer_entities` / `analyzer_relations`; битые ссылки (`srcId` не из реестра) → `input_issues` с кодом `entity_id_not_in_registry`.
3. Phase 1 получает `analyzer_entities` в payload и для каждой строки реестра эмитит mention с тем же `surface` и полем `agentId` = namespaced id. По умолчанию (`AGENTHORDE_CANONICALIZER_PHASE1_CHUNK_MAX=0`) весь реестр уходит **одним** bound-вызовом LLM (без adaptive halving); при `CHUNK_MAX >= 1` список режется на чанки этого размера и обрабатывается **параллельно**, а пустой ответ чанка делится пополам до `AGENTHORDE_CANONICALIZER_PHASE1_MAX_DEPTH` (обход таймаутов Vertex flash). Дополнительно всегда идёт prose-pass (`analyzer_entities=[]`). Модель phase 1 по умолчанию — `openai/o4-mini` через OpenRouter (`OPENROUTER_API_KEY`); для Vertex задайте `AGENTHORDE_CANONICALIZER_ENTITY_MODEL=gemini-3-flash-preview` и положительный `CHUNK_MAX`.
4. После phase 2 pipeline строит `agent_registry_id_to_entity_id`. Phase 4 **pre-pass** резолвит рёбра по этому маппингу (основной путь); fallback — fuzzy `label` → entity только если маппинг не сработал. Неуспех → `candidates.unknownRelations` с `reason: entity_not_resolved_by_canonicalizer`. Финал = union(pre_pass, llm_extended) с дедупом по `(sourceFragmentId, targetFragmentId, relation, layer_section)` (+ `product_fragment_id` для `supplies`).

Pre-pass-рёбра маркируются `qualifiers.origin = "analyzer_proposed"` для аудита.

### Evidence archive: вне графа

`EvidenceArchive` — аудит-слой, отдельный от `CanonicalGraph`. Содержит пять списков:

- `sources` — `EvidenceSource` по одной записи на URL (`sha16(url)`).
- `links` — `EvidenceLink`: `claim_id → source_id` (`asserted_by` top-1, `supported_by` fan-out с рангом и score).
- `claims` — `EvidenceClaim` (id / text / hypothesis_id) для всех claim-ов, которые где-то использованы (в `edge.sourceClaimIds`, в `metric.provenance.claim_ids` или просто присутствуют в reasoning graph).
- `hypotheses` — `EvidenceHypothesis` (id / text) по всем уникальным `hypothesis_id`.
- `counters` — `EvidenceCounter` (id / text / `contradicts_claim_id`), где `contradicts_claim_id` — ближайший предшествующий claim в порядке `reasoningGraph.nodes`.

Правила связывания claim → source в `links`:

1. Каждый `proposedNodes[i].evidenceRefs` должен содержать URL-ы из `toolLedger[*].payload.sources[*].url` (или уже готовые 16-hex id). Значение `"prior_knowledge"` — явный маркер «чистое prior knowledge, источники не нужны».
2. Если `evidence_refs` непуст и хоть один URL/id резолвится в `archive.sources` — создаём прямые `EvidenceLink` со `score=100` (первый — `asserted_by`, остальные — `supported_by`).
3. Если `evidence_refs` непуст, но ни один не резолвится — **не** подмешиваем fuzzy: уважаем декларацию аналитика.
4. Если `evidence_refs` пуст — fallback в fuzzy. Для первичного matching'а порог `CANONICALIZER_EVIDENCE_FANOUT_MIN_SCORE`; для supported_by fan-out при отсутствии первичного хита — более мягкий `CANONICALIZER_EVIDENCE_FALLBACK_MIN_SCORE` (по умолчанию 45).

Ни один `/evidence/*` фрагмент и ни одно `asserted_by`/`supported_by`/`contradicts`/`under_hypothesis` ребро в `graph` больше не появляются. Это сжимает `graph.edges` до чисто структурной топологии (как правило, в 1.5–2× на реалистичных прогонах) и делает apply в property graph тривиальным: archive маппится в GCS (raw контент) + metadata-store (BigQuery / Firestore) с пятью таблицами.

### Supervisor: pre-finalize gates и trace

Перед `finalize` supervisor проверяет три инварианта. Каждый гейт даёт **один** retry (guard через `pack.gatesRetried`), после чего пишет `gate_action = "skip"` и всё равно финализирует (честный отчёт с gap'ом).

| Gate | Условие retry | Retry agent | Директива |
|------|---------------|-------------|-----------|
| `structural` | `last_intent ∈ {supply_chain, governance, sanctions}` **и** `len(pack.proposedRelations) == 0` | соответствующий analyzer | `STRUCTURAL GAP: emit explicit proposedRelations using AH vocabulary` |
| `evidence` | хотя бы один `proposedNodes[i].evidenceRefs = []` **и** `toolLedger` содержит `web_search.sources` | тот же analyzer | `EVIDENCE GAP: attach source URLs from toolLedger to evidenceRefs` |
| `open_questions` | `analyzerOutput.openQuestions != []` **и** `cycle < cap - 1` | тот же analyzer | `OPEN QUESTIONS: run targeted web_search on <top-3>` |

Каждое решение роутера **и** каждое срабатывание гейта пишет `SupervisorTraceEntry` в `pack.supervisorTrace`. Поле `pack.lastIntent` (`"supply_chain"` / `"governance"` / `"sanctions"` / `""`) фиксирует последнего «домен-аналитика» и используется гейтами, чтобы retry попадал в нужного воркера.

### Реализация канонизатора (код)

В репозитории реализован **9-фазный stateless пайплайн** в пакете [`agenthorde_common/canonicalizer/`](../../shared/agenthorde_common/agenthorde_common/canonicalizer/):

| Фаза | Модуль | Движок | Назначение |
|------|--------|--------|------------|
| 0 | `phases/phase0_input.py` | код | Сборка `WorkingState` из `pack`, `analyzerOutput` / `governanceOutput`, `finalAnswer`, `workingGraph` |
| 1 | `phases/phase1_entities.py` + `prompts/phase1_entities.txt` | LLM | NER / типизация по 11 классам AH |
| 2 | `phases/phase2_dedup.py` | код + Firestore product/gov synonyms | Нормализация поверхности, fuzzy (`rapidfuzz`), словарные merge, **jurisdiction identity gate** |
| 3 | `phases/phase3_paths.py` + `slug.py` | код | Canonical path + ISO2 из контекста, коллизии slug |
| 4 | `phases/phase4_relations.py` + `prompts/phase4_relations.txt` | LLM | Связи между `entity_id` (до маппинга во fragment UUID) |
| 5 | `phases/phase5_transforms.py` + `dictionaries.py` | код + `horizon_product_transform_pairs` (read-only) | Site-centric transform на узлах `/site`; неизвестные пары только в JSON `candidates.unknownTransforms` |
| 6 | `phases/phase6_metrics.py` + `prompts/phase6_metrics.txt` | LLM + код | Метрики на фрагментах / рёбрах |
| 7 | `phases/phase7_evidence.py` | код | `/evidence/source` из `toolLedger`, `/evidence/claim`, `asserted_by` / `supported_by` |
| 8 | `phases/phase8_validate.py` + `validation.py` | код | Пути, endpoints связей, `layer`, unknown transforms → отчёт |
| 9 | `phases/phase9_output.py` + `confidence.py` | код | Noisy-OR confidence по рёбрам, сборка `CanonicalizerResult` |

Публичный API: `from agenthorde_common.canonicalizer import canonicalize, CanonicalizerInput, CANONICALIZER_VERSION`. Вызов **после** прогона агентов встроен в [`runner_cli.py`](runner_cli.py): в ответ добавляется поле `canonicalGraph` (JSON). Флаг CLI `--skip-canonicalizer` отключает пост-обработку.

**Форма графа:** узлы — [`CanonicalFragment`](../../shared/agenthorde_common/agenthorde_common/canonicalizer/schemas.py) (поля в духе `HorizonFragment`: `canonicalPath`, `fragmentClass`, `metrics`, `provenance`, `validFrom`/`validTo`, `siteTransforms` для site-centric transform). Рёбра — [`CanonicalEdge`](../../shared/agenthorde_common/agenthorde_common/canonicalizer/schemas.py) (поля в духе `DependencyEdge`: `sourceFragmentId`, `targetFragmentId`, `relation`, `qualifiers` с `layer` / `layerSection`, `attributes`, `metrics`, `provenance`). Таксономия на этом этапе **только AH**; маппинг enum → ядро — отдельный слой позже.

**Словари:** чтение из Firestore (`horizon_product_synonym_pairs`, `horizon_product_transform_pairs`, `horizon_gov_synonym_pairs`), проект по умолчанию из `HORIZON_FIRESTORE_PROJECT` или `GOOGLE_CLOUD_PROJECT`. Переменные моделей: `AGENTHORDE_CANONICALIZER_ENTITY_MODEL`, `_RELATION_MODEL`, `_METRIC_MODEL`, порог fuzzy `AGENTHORDE_CANONICALIZER_FUZZY_THRESHOLD`.

#### Jurisdiction-as-identity (phase 2 hard gate)

Идентичность фрагмента определяется **полным каноническим путём**, а не сырой поверхностью. Префикс юрисдикции (`/regime/eu/...` vs `/regime/us/...`, `/org/us/apple` vs `/org/hk/apple-daily`) — **онтологический инвариант**: два фрагмента из разных юрисдикций — разные сущности, как бы похожи ни были их названия.

Реализация в [`phases/phase2_dedup.py`](../../shared/agenthorde_common/agenthorde_common/canonicalizer/phases/phase2_dedup.py): для классов, чья идентичность intrinsically привязана к юрисдикции (`regime`, `program`, `project`, `site`, `geo`, `person`, `org`, `community`, `market`), при попытке merge двух mention'ов с **известными и различающимися** `iso2_hint` — merge отвергается **без** оглядки на surface-similarity. Гэп фиксируется в `candidates.mergeCandidates` с `reason=namespace_conflict:{iso_a}_vs_{iso_b}`, чтобы critic / следующий цикл это видели.

ISO2 для гейта берётся из `context_hints.iso2_hint` (если LLM поставил), иначе извлекается из самой поверхности через `extract_iso2_from_text` + `iso2_from_country_names` ([`geo_iso.py`](../../shared/agenthorde_common/agenthorde_common/canonicalizer/geo_iso.py), включая региональные токены `eu` / `gcc` / `int`).

Что это автоматически чинит:
- `EU Chips Act` ≠ `US CHIPS Act` (одинаковые токены, разные регуляторные режимы)
- `Apple Inc.` ≠ `Apple Daily` (один surface "Apple", разные страны + типы)
- `Lee` (US senator) ≠ `Lee family` (KR chaebol dynasty) — тут ещё и тип разный, но gate работает поверх type-фильтра
- Любые `Ministry of Industry` из разных стран

Принцип расширяет AGENTS.md-инвариант для product (`canonical product path` как identity) на ВСЕ jurisdiction-bound классы. Без хардкодов конкретных пар: единственный список — какие классы юрисдикционно-связанные.

#### Endpoint resolution: registry id → entity, затем fuzzy fallback

Pre-pass phase 4 сначала маппит `proposedRelations[].srcId` / `dstId` (namespaced registry id) в `entity_id` через `agent_registry_id_to_entity_id`, построенный после phase 2 из `mentions[].agentRegistryId`.

Если маппинг не сработал, для endpoint'а подставляется `label` из `WorkingState.analyzer_entity_label_by_ns` и вызывается [`_resolve_surface_to_entity`](../../shared/agenthorde_common/agenthorde_common/canonicalizer/phases/phase4_relations.py) — каскад:

1. Reject известные internal-id паттерны (`^e\d+$`).
2. Exact canonical path match (`/org/...`).
3. Exact normalized surface (underscore→space).
4. Singular/plural inflection fallback (vessel_class drift: VLCC ↔ VLCCs).
5. Strip legal suffix + parenthetical alias на обеих сторонах.
6. Generic synonym dictionary (Firestore `horizon_gov_synonym_pairs`).
7. **token_set_ratio fuzzy fallback** ≥ 92, tiebreak по размеру пересечения токенов.

Полный провал → `candidates.unknownRelations` с `reason: entity_not_resolved_by_canonicalizer`.

### Этапы:

- **Этап 1 (текущий):** канонизатор stateless. Каждый прогон — изолированный, один вход → один канонизированный граф. Детерминированно, воспроизводимо, легко отлаживать.
- **Этап 2 (см. ниже):** долгосрочная память через Vertex AI субстраты знаний.

---

## Roadmap: долгосрочная память через Vertex AI субстраты знаний

После того как канонизатор стабилизируется (Этап 1), его выходы начинают не просто возвращаться пользователю, а **записываться в долгосрочную память** — структурированные индексированные субстраты в GCS + Vertex AI Search. Это превращает AH из «stateless исследовательского цикла» в «систему, которая копит и переиспользует знания между прогонами».

### Принцип: дедупликация на записи, не на чтении

Сейчас `ah_knowledge_search` — заглушка, потому что нет базы знаний. С субстратами он становится **реальным первым шагом любого агентского цикла**: «что мы уже знаем?» перед «пойдём искать новое». Когда агент делает `knowledge_search("G42 ownership")`, Vertex возвращает **уже канонизированную** сущность с `/org/ae/g42`, canonical name, всеми известными связями. Агенту не нужно догадываться, что он нашёл — результат уже несёт штамп канонизатора.

Если в следующем прогоне тот же агент через `web_search` находит «Group 42», он может сразу проверить через `knowledge_search` и увидеть, что это та же сущность. **Дедупликация между прогонами получается автоматически** — она произошла на фазе записи канонизатором, а не при каждом чтении.

### Четыре типа субстратов (разные гранулярности запроса)

Канонизатор пишет результаты прогона в **четыре параллельных субстрата** — каждый оптимизирован под свой тип запроса:

| Субстрат | Что индексируется | Оптимален для запроса |
|----------|-------------------|----------------------|
| **Сущности** | canonical path + canonical name + subtype + attributes (sector_path, domains, метрики) | «Кто такой G42?», «Какие площадки под оператором PSA?» |
| **Сущности со связями** | entity-centric подграф на 1–2 ребра: узел + непосредственные структурные связи + атрибуты рёбер | «Что Mubadala владеет?», «С кем G42 в JV?» |
| **Цепочки со смыслами** | полная цепочка site→site с transforms, ключевыми режимами, narrative summary | «Как литий добирается от Atacama до CATL?», «Путь нефти через Малаккский пролив» |
| **Наблюдения** | отдельные claims с provenance, `as_of`, `confidence`, source refs | «Что говорили о санкциях на X в октябре?», «Какие оценки доли Silver Lake в G42?» |

Каждый субстрат — это отдельный **Vertex AI Search data store** в своей GCS bucket'е со своей схемой метаданных. Один и тот же факт может попасть в несколько субстратов в разных разрезах (например, ребро `subsidiary_of` → в «сущности со связями» как атрибут подграфа и в «наблюдения» как отдельный claim с датой).

### Схема метаданных

Помимо семантического эмбеддинга, каждая запись несёт **структурированные фильтры** для точных запросов:

- `canonical_path` — точный lookup по пути
- `subtype` — фильтр по подтипу (`swf`, `port`, `gov_body`, …)
- `domains` — массив доменов (`governance`, `logistics`, …)
- `sector_path` — GICS-подобный сектор
- `layer` — для рёбер и цепочек (structural / contractual / flow)
- `as_of` / `observed_at` — временной диапазон
- `confidence` — порог достоверности
- `run_id` + `canonicalizer_version` — провенанс записи

Это позволяет агенту спрашивать не только «что известно про G42», а «какие **контрактные** связи `/org/ae/g42` известны **на 2025 год** с confidence > 0.7».

### Канонизатор — единственный writer

**Жёсткое правило:** в субстраты пишет **только канонизатор**. Не агент напрямую, не side-channel, не какая-то внешняя система. Это гарантирует, что всё в памяти прошло через:

1. Entity extraction с типизацией по 11 классам
2. Дедупликацию с lookup в operational dictionaries
3. Canonical path resolution
4. Relation extraction по 6 слоям связей
5. Transform attachment с валидацией по canonical dictionary
6. Metric structuring с provenance
7. Финальную валидацию схемы

Если писать в субстрат мимо канонизатора — память начнёт деградировать: появятся alias'ы, противоречивые типы, non-canonical paths. Это тот же принцип, что «канонизатор — единственное место enforcement онтологии», только расширенный на долгосрочную память.

### Конфликты и противоречия

Два прогона могут записать **конфликтующие факты** о G42 (разные доли, разные UBO в разное время). Субстрат не должен их «решать» — он должен **хранить оба** с provenance и `as_of`. Следующий агент сам прочитает оба и примет решение:

- «Последнее раскрытие эмитента (2024-Q4) вытеснило предыдущее (2023-Q2)» — использует свежий факт
- «Есть два независимых источника с разными оценками, confidence 0.6 и 0.8» — формирует гипотезы по обоим

Это естественно ложится на **observations substrate** — каждая запись помечена `observed_at` и `source`; несогласие становится видимым, а не скрытым в процессе merge.

### Почему Vertex AI Search

- **Не нужно городить свой векторный индекс** — Vertex AI Search предоставляет data store + semantic search + structured filters из коробки
- **GCS как bucket** для JSONL записей — дёшево, version-able, легко переиндексировать
- **Метаданные как фильтры** — не нужен отдельный SQL для точных запросов, всё в одном слое
- **Индексация автоматическая** — канонизатор пишет в bucket, Vertex переиндексирует data store
- **Инфраструктура production-ready** — эта архитектура стандартна для RAG-систем в GCP

### Как меняется агентский цикл

С субстратами цикл каждого worker-агента начинается с:

```
1. knowledge_search(query)         # сначала спроси, что система знает
2. анализ результатов              # что известно, что нет, где пробелы
3. web_search(...)                 # идёт искать только то, чего не знает
4. ah_apply(...)                   # дополняет reasoningGraph новым
```

Это **резко сокращает** количество `web_search` вызовов и latency прогонов для повторяющихся сущностей. G42 упомянут в 5 прогонах подряд — его canonical entity и его ownership подграф уже в памяти, web-поиск нужен только для уточнений.

### Что НЕ делаем на Этапе 2

- **Не делаем subscription / listeners** на субстрат (кто-то внешний — нет, только канонизатор пишет)
- **Не делаем automatic rewrite** — старые записи не перезаписываются при новом прогоне; конфликты хранятся как отдельные observations с разными датами
- **Не делаем cross-run reasoning внутри канонизатора** — каждый прогон stateless, память читается только агентами через `knowledge_search`, канонизатор с ней не сверяется
- **Не делаем ручного pruning** в MVP — мусор копится, следим за объёмом; автоматическая очистка — отдельная задача позже

---

## Roadmap: канонизатор — доработать позже

Текущая реализация канонизатора сознательно упрощена; ниже — что вынести в следующие итерации (после стабилизации JSON-выхода и агентского контура).

| Область | Сейчас (MVP) | Сделать позже |
|---------|----------------|---------------|
| **Доказательность (фаза 7)** | Для `asserted_by` все claim’ы привязываются к **одному** источнику из `toolLedger`; нет маппинга **claim → URL** из grounding по каждому claim; нет рёбер `contradicts` (counter) и `under_hypothesis` (hypothesis_anchor) | Построить связь claim↔источник(и) по шагам агента / фрагментам grounding; добавить `contradicts` и `under_hypothesis` по правилам README §5 |
| **Метрики (фаза 6)** | Нет физических **конверсий единиц** (kcf→mcf, баррели→тонны и т.д.); слабая привязка **к конкретному ребру** (эвристика «первое ownership-ребро» для pct) | Словарь/код нормализации единиц; явная привязка метрики к ребру/узлу из LLM + пост-валидация |
| **Словари** | Read-only Firestore **опционально**; без проекта — fuzzy + LLM, граф всё равно в JSON | При необходимости: локальный снапшот словарей, явная политика «нет проекта → предупреждение в отчёте», расширение org/person aliases в ядре |
| **Вызовы LLM (фазы 1, 4, 6)** | Пустой ответ → пустые mentions / relations / metrics **без** fallback из текста; нет ретраев и жёстких таймаутов | Ретраи + таймаут; лёгкие детерминированные fallback’и (например NER-заготовки из claim-текста) при `AGENTHORDE_CANONICALIZER_SKIP` / сбое модели |

---

## Roadmap: перенос критика на backend `/api/v1/ai/critic`

### Контекст

После серии прогонов `gemini-3.1-pro-preview` (наша текущая `CRITIC_MODEL`) регулярно ловит `429 RESOURCE_EXHAUSTED` на поздних циклах супервайзера — именно на критике, потому что он делает тяжёлые структурированные вызовы и проходит поверх уже прогретого пакета с несколькими сотнями узлов.

В репозитории уже есть готовый канал на не-Vertex провайдеров:
- `backend/internal/ai/openrouter_service.go` — OpenAI-совместимый клиент, поддерживает `response_format: json_schema + strict`.
- `backend/internal/ai/model_manager.go` — реестр моделей с OpenRouter-слагами и fallback-логикой.
- `backend/internal/ai/task_executor.go` — выбирает модель по `AIRequirements` и прогоняет через нужный сервис.
- `scripts/deploy` + `scripts/update-openrouter-secret.sh` — `OPENROUTER_API_KEY` уже пробрасывается в Cloud Run из Secret Manager.
- `backend/internal/http/ai_exec.go` — endpoint `POST /api/v1/ai/structured` над `AITaskExecutor` (сейчас с фиксированным task-type и без проброса `PreferredModel`/`ResponseSchema`).

### Временное решение (реализовано в `agenthorde/critic/`)

Критик теперь пробует Vertex первым, а при ошибке или невалидном JSON по очереди ходит в OpenRouter-цепочку моделей напрямую из Python. Порядок зафиксирован после прогона `scripts/bench_critic_models.py` (апрель 2026, промпт TSMC/ASML/CHIPS):
- `openai/o4-mini` (reasoning.effort=low) → `z-ai/glm-5.1` → `deepseek/deepseek-chat-v3.1` → `nvidia/nemotron-3-super-120b-a12b`
- порядок настраивается env: `AGENTHORDE_CRITIC_FALLBACKS`
- per-model overrides (например, `reasoning.effort=low` для o4-mini и `temperature=None` для OpenAI reasoning-моделей) — в `CRITIC_OR_MODEL_OVERRIDES` из `config.py`
- `AGENTHORDE_CRITIC_OR_MAX_OUTPUT_TOKENS` по умолчанию `6144` — headroom под продовые вердикты
- `OPENROUTER_API_KEY` читается из env (тот же ключ, что и в Go-бэкенде)
- каждая попытка (Vertex + каждый OR-rung) пишется в `/tmp/agenthorde_smoke/critic_ab.jsonl` (path в `AGENTHORDE_CRITIC_AB_LOG`) — чтобы одной командой сравнить вердикты разных моделей на одном и том же запросе
- `response_format: json_schema` строится из `CriticVerdict.model_json_schema()` + нормализатор `to_openai_strict_schema` (инлайнит `$ref`, убирает `default`/`pattern`/`minLength`, делает все поля `required`)
- `strict: true` по умолчанию, можно отключить `AGENTHORDE_CRITIC_OR_STRICT=0`
- критик в OR-режиме вызывается без tool-calling (web_search / retrieve) — ему это и не нужно, все доказательства уже в session pack

Почему именно такой порядок (из бенча):
- **o4-mini** — ≈6.2s, ~$0.0026/вызов, 4 blockers + чёткий `missionHint` на 319 output-токенах, strict schema держится идеально. Дешевле и быстрее всех прочих при сопоставимом качестве.
- **GLM 5.1** — второй слой на случай квоты OpenAI; диверсификация провайдеров (Z.ai).
- **DeepSeek chat v3.1** — самый дешёвый reliable рунг (~8s, $0.0004).
- **Nemotron 3 Super** — emergency final (копейки, 60s, 5 blockers — жёсткий).

Дропнуты: DeepSeek R1 (заменён o4-mini), Qwen 3.5 397B (121s слишком медленно), Qwen 3.5 Plus (провайдер Alibaba не enforcит strict json_schema), Nemotron 3 Nano (DeepInfra не держит json_schema вообще).

Это сознательно временное решение: дублирует часть логики AITaskExecutor (retries, cost tracking, telemetry), обходит централизованный биллинг и ходит в OpenRouter напрямую, не через `/api/v1/ai/structured`.

### Целевое состояние

1. **Расширить `/api/v1/ai/structured`** (или ввести парный `/api/v1/ai/critic`), чтобы принимать `preferredModel`, `fallbackModels`, `responseSchema` и возвращать фактически использованную модель + usage. Экзекутор уже всё это умеет внутри — нужно только пробросить через handler.
2. **Добавить в `model_manager.go`** записи OpenRouter-слагов для не-Nemotron моделей: `z-ai/glm-5.1`, `deepseek/deepseek-r1`, `deepseek/deepseek-chat-v3.1`. Провайдер — `AIProviderOpenRouter`, НЕ `AIProviderDeepSeek` (в `backend/internal/ai/` нет `NewDeepSeekService`, эти DeepSeek-модели в реестре на сегодня мёртвые записи).
3. **В Python-стороне** заменить прямой OpenRouter-клиент в `agenthorde/critic/openrouter_client.py` на HTTP-клиент к backend-endpoint. Получаем: единую телеметрию (usage_metrics + horizon_spend_sink), централизованный retry/circuit-breaker, общий rate-limit budgeting.
4. **Ретрофитить AB-логирование** — переключить источник данных с локального JSONL на Horizon spend ledger + usage_metrics (уже есть в экзекуторе).

Когда эти четыре пункта закрыты, модуль `agenthorde/critic/` сжимается до одного тонкого клиента backend-endpoint-а, а настройка моделей критика перемещается в Go-конфиг.

---

## Roadmap: aggregate mentions и followUpPlan

### Проблема

После запуска канонизатора часть фрагментов стабильно остаётся без рёбер (orphan nodes). Разбор реального прогона показывает, что это **не баг**, а три разные корзины:

1. **Aggregate mention** — сущность упомянута как контекст, но источники (web_search-результаты данного прогона) не дали явного relation. Пример: в запросе про Russian seaborne crude «всплывают» Malta / Egypt как транзитная география, Yulong Petrochemical как китайский refinery, Radiating World как shadow-fleet vehicle — без конкретного `supplies` / `operates` / `owns` в пределах нашего исследования. Это **легитимный deferred-факт**.
2. **Analyzer gap** — в исходном тексте есть явный relation, но LLM-агент его не эмитнул (например, Suezmax явно чартерится в тексте, но `charters` не попал в `proposedRelations`). Это баг extraction-слоя.
3. **Vocabulary gap** — relation концептуально нужен, но не входит в закрытый словарь канонизатора. Пример: Argus / Platts публикуют price reference — у нас до этого PR не было relation «org publishes market benchmark». Это дыра в онтологии, закрывается отдельной правкой.

Текущая валидация проходит по всем трём корзинам одинаково (просто пишет `orphan_nodes`). Это неточно и мешает видеть реальные пробелы.

### Целевая модель

**Orphan-корзина «aggregate mention» становится first-class артефактом** — с маркером на фрагменте и планом follow-up исследования.

#### На фрагменте

Добавить:

```python
attributes.aggregation_status: "concrete" | "aggregate_mention"
attributes.aggregation_reason: "no_rel_emitted_in_this_run"
```

`concrete` — фрагмент участвует хотя бы в одном ребре. `aggregate_mention` — упомянут, рёбер нет, но мы **сознательно** сохраняем его как anchor для последующих прогонов. Будущие follow-up запросы, приземляющие concrete-рёбра к существующему каноническому пути, автоматически переведут фрагмент в `concrete` без ручного merge.

#### На canonicalGraph — followUpPlan

Новое поле `canonicalGraph.followUpPlan: list[FollowUpItem]`:

```jsonc
{
  "id": "fup-eg-01",
  "entityPath": "/geo/eg/egypt",
  "fragmentClass": "geo",
  "contextFromRun": "Transit geography in Russian crude rerouting (Suez Canal, SUMED)",
  "suggestedQuestion": "What is Egypt's concrete role in Russian seaborne crude flows post-2022: Suez transit volumes, SUMED pipeline usage, domestic Egyptian receivers, STS around Ceuta/Lakonikos on the Mediterranean leg?",
  "suggestedAgent": "supply_chain_analyzer",
  "priority": "medium",
  "parentRunId": "<this run>"
}
```

Приоритет выводим из `mention_count` + centrality-proxy (сколько соседних concrete-фрагментов упоминают эту сущность в claim-тексте).

#### Трёхуровневая корзинизация

Канонизатор v1 по умолчанию пишет всех orphan'ов в `aggregate_mention`. Отдельная постаудит-фаза (V2, позже) может переклассифицировать:

| корзина | где хранится | что с этим делать |
|---|---|---|
| `aggregate_mention` | `fragment.attributes.aggregation_status` + `followUpPlan` | показывать как deferred, предлагать follow-up |
| `analyzer_gap` (V2) | `ValidationReport.analyzer_gaps` | метрика качества extraction; триггер для промт-retune |
| `vocabulary_gap` (V2) | `ValidationReport.vocabulary_gaps` + `candidates.unknown_relations` | триггер для расширения taxonomy |

Различение `aggregate_mention` vs `analyzer_gap` требует пост-LLM-аудита (маленький вызов, который смотрит на orphan + исходный текст и решает: «в тексте был relation?»). Для MVP-v1 всех orphan'ов считаем `aggregate_mention` — self-correcting: если follow-up стабильно пуст, это был шум либо vocabulary_gap; если приносит рёбра — классификация была верной.

### Почему это ценно

- **Эпистемическая честность**: граф перестаёт терять «упомянули, но не раскопали». Рамка исследователя, не код-валидатора.
- **Идемпотентные follow-up'ы**: повторный запрос с drill-down на конкретный aggregate просто приземляет новые рёбра к существующему canonical path. Никаких merge-конфликтов.
- **Self-correcting quality signal**: стабильно пустые follow-up'ы → vocabulary_gap или шум; наполняющиеся → корректный aggregate.
- **Разгружает валидацию**: `orphan_nodes` перестаёт быть универсальной «плохой метрикой» и разделяется на точные категории.

### Scope первой итерации

1. Схемы: `aggregation_status` на `CanonicalFragment.attributes`, `FollowUpItem` pydantic-модель, `canonicalGraph.followUpPlan`.
2. Phase 9 (или новая phase 10): по `orphan_nodes` генерит `FollowUpItem` с простой эвристикой приоритета и готовым question-шаблоном.
3. Обновить `ValidationReport`: `orphan_nodes` остаётся как техническая метрика, но UI/downstream-слой читает `aggregate_mentions` из фрагментов.
4. Unit-тест + один live-прогон с follow-up по одному из aggregate'ов, убеждаемся что новые concrete-рёбра приземляются к тому же фрагменту.

Вне первой итерации: LLM-аудит классификации (V2), автоматический prompt-retune на основе `analyzer_gaps`, automatic vocabulary extension.

---

## Продуктовые трансформации: site-centric модель

### Проблема текущей модели

В ядре (и в ранней AH-онтологии) transform — это абстрактное ребро между двумя `/product`: `spodumene-concentrate` →`transforms_into`→ `lithium-carbonate`. Но:

- Превращение не происходит само по себе — оно происходит **на конкретной площадке**. Реферинг-завод **и есть** transform.
- Стадия в product path (`/product/{stage}/{family}/{grade}`) не используется для фильтрации — никто не ищет «все продукты стадии `chemical_intermediate`». Ищут конкретный продукт или конкретную цепочку.
- Абстрактный граф трансформаций (product→product) и физический граф (site→site) живут отдельно, и их нужно мысленно сшивать.

### Целевая модель: transform — свойство `/site`, а не ребро между `/product`

Цепочка поставок — это **граф площадок**, связанных ребром `supplies` с продуктом на ребре. Трансформация — свойство принимающего узла.

```
/site/cl/mine-atacama
   ——supplies(product: /product/spodumene-concentrate)——→
/site/cn/refinery-ganfeng
   [transform: spodumene-concentrate → lithium-carbonate]
   ——supplies(product: /product/lithium-carbonate)——→
/site/de/battery-plant-catl
   [transform: [lithium-carbonate, cathode-material, anode, electrolyte, casing] → battery-cell]
   ——supplies(product: /product/battery-cell)——→
/site/de/assembly-plant
```

### Что меняется

**1. Product path упрощается.**

Стадия **выводится** из позиции в цепочке, а не кодируется в пути. Достаточно:

`/product/{family}/{slug}`

Примеры: `/product/lithium/spodumene-concentrate`, `/product/lithium/lithium-carbonate`, `/product/battery/battery-cell`.

**2. `transforms_into` уходит из связей между продуктами.**

Вместо абстрактного ребра `/product/A` →`transforms_into`→ `/product/B` — атрибут на узле `/site`:

```
transform:
  inputs: ["/product/lithium/spodumene-concentrate"]
  output: "/product/lithium/lithium-carbonate"
  kind: transformation      # или assembly для multi-input
```

**3. `supplies` становится единственной связью, двигающей продукт по цепи.**

| Ребро | A → B | Атрибуты |
|-------|-------|----------|
| `supplies` | `/site` → `/site` | `product` → `/product`, `volume`, `layer`, `carrier` → `/org`, ... |

Всё, что течёт между площадками, — одно ребро `supplies` с продуктом на нём. Одна связь вместо двух (`supplies` + `transforms_into`).

**4. Multi-input transform (сборка) ложится естественно.**

Батарейный завод принимает катодный материал, анодный, электролит, корпус — несколько `supplies` рёбер с разными `product` входят в один `/site`. На узле — assembly transform с массивом `inputs` и одним `output`.

**5. Canonical transform dictionary меняет роль.**

Словарь 405 эталонных пар (canonical control plane) не исчезает, а становится **constraint / validation**: если `/site` заявляет transform `X→Y`, словарь проверяет, известна ли такая пара. Неизвестные пары уходят в candidate queue — как и раньше, но проверка привязана к площадке, а не к абстрактному графу.

### Что это даёт

- **Цепочка читается линейно**: site → site → site, как маршрутный лист. Не нужно сшивать два параллельных графа.
- **«Что будет, если завод встанет?»** — сразу видно, какой transform пропадает. Не нужно ходить из физического графа в продуктовый и обратно.
- **«Какие площадки делают этот передел?»** — запрос к графу, а не к абстрактному словарю.
- **Product path компактнее**: 2 сегмента вместо 3–4; стадия не дублирует то, что и так видно по графу.

### Совместимость с ядром

Ядро сейчас хранит transform pairs как абстрактные `from→to`. Site-centric модель **не ломает** этот словарь — она переиспользует его как constraint. Для ядра это значит:

- `TRANSFORMS_INTO` / `ASSEMBLES_INTO` ребро между продуктами можно заменить на атрибут `/site` (или `/fac` в терминах ядра)
- Supply links, которые сейчас — отдельный fragment type, становятся просто `supplies` рёбрами с продуктом
- Product canonical path теряет `{stage}` сегмент — это breaking change для ядра, но эволюционный для AH

Приземлять после стабилизации агентской среды вместе с упрощением таксономии ядра.

## Что планируется заимствовать из ядра (до стабилизации агентской среды не приземляется)

Онтология AgentHorde — экспериментальная, намеренно изолирована от runtime taxonomy ядра (`horizon_taxonomy`). Ядро несёт 20+ namespaces, 4-уровневые product paths, industry aliases, supply-link fragments и прочую инфраструктуру, которая AH-агентам избыточна. Ниже — **только то, что имеет смысл взять**, с указанием формы адаптации.

### 1. Метрики (из ядра: `MetricValue` + `MetricDefinition`)

**Зачем:** сейчас AH не умеет структурированно выразить «доля 42 %, confidence 0.8, раскрытие эмитента, март 2025». Всё ложится в свободный текст `claim`. Без метрик governance-агент не отличает подтверждённую долю от газетной оценки.

**Что берём:**
- Двойка типов: **magnitude** (0..1) и **directional** (−1..1)
- На каждом значении: `value`, `confidence` (0..1), `observed_at`, `provenance` (source ref → `/evidence/source`)
- Опционально: `unit`, `error_margin`

**Чего не берём:** полный каталог стандартизированных метрик ядра (operational_health, water_intensity, …). AH определит свой минимальный набор по мере прогонов.

### 2. Домены (из ядра: sphere paths + domain modes → один тег-слой)

**Зачем:** AH нуждается в кросс-доменной разметке — «это про логистику», «это про геополитику», «это про корпоративное управление». Сейчас такой разметки нет.

**Что берём:** плоский или двухуровневый тег `domain`, объединяющий то, что в ядре разнесено по `/sphere/{L1}/{L2}` и `domainMode`. Примерный начальный список:

- `logistics` / `logistics/maritime`
- `geopolitics`
- `governance`
- `energy` / `energy/markets`
- `finance`
- `policy` / `policy/sanctions`
- `technology`

Формат: массив тегов на узле или ребре (как `spherePaths` в ядре, но без вложенности глубже двух уровней).

### 3. Секторальная таксономия (из ядра: sector taxonomy → лёгкая двухуровневая нарезка)

**Зачем:** когда агент находит Mubadala, Shell, TSMC — полезно иметь сектор для классификации и фильтрации, а не только подтип `/org`.

**Что берём:** GICS-подобная **двухуровневая** схема (sector → industry group). Примеры:

- `Energy/Upstream`, `Energy/Midstream`, `Energy/Downstream`
- `Materials/Mining`, `Materials/Chemicals`
- `Technology/Semiconductors`, `Technology/Software`
- `Financials/Asset_Management`, `Financials/Banking`
- `Industrials/Transportation/Marine`

Поле `sector_path` на узлах `/org`. **Не берём** 4-уровневый NAICS-подобный каталог ядра.

### 4. Модальность ребра: `projected` (из ядра: `edgeModality`)

**Зачем:** `layer` (structural / contractual / flow) не различает «маршрут существует» и «маршрут планируется». Это встречается в обоих доменах: Thai Land Bridge (projected), обсуждаемая сделка Microsoft–G42 (projected).

**Что берём:** одно опциональное поле `status` на structural-ребре со значениями:

- `actual` (default, можно не указывать)
- `projected` (планируется, строится, обсуждается)

**Чего не берём:** полный enum ядра (`historical`, `potential`) — `historical` покрывается отсутствием ребра или корпоративным событием; `potential` слишком размыт.

### Чего НЕ берём из ядра

| Концепция ядра | Почему не берём |
|----------------|-----------------|
| 20+ first-segment namespaces (`/industry`, `/domain`, `/community`, `/segment`, `/media`, `/decision`, `/mechanism`, `/zone`, `/track`, `/supply-link`, …) | AH достаточно 11 классов; остальные служат инфраструктуре ядра |
| `/gov` как отдельный namespace | AH моделирует как подтип `/org/gov_body`; менять обратно — ломать консолидацию |
| 4-уровневый product path (`/product/{category}/{type}/{stage}/{slug}`) | AH-шный 3-уровневый (`{stage}/{family}/{grade}`) проще и ближе к supply chain реальности |
| Lifecycle stages (`concept` → `operational` → `closed`) | Полезно, но это атрибут узла, а не таксономия; агент и так может записать это в claim |
| `ValidFrom` / `ValidTo` на рёбрах | AH использует `as_of` на атрибутах + корпоративные события; полная темпоральная валидность — overhead для экспериментальной среды |
| `LOCATED_IN` как отдельный тип связи | ISO2 в каноническом пути + `incorporated_in` уже покрывают географию; третий способ — избыточен |
| Industry taxonomy aliases, eventTypeSynonyms, signalTypes | Инфраструктура нормализации ядра; AH работает с LLM-выводом, а не с incoming pipeline |
| Полный каталог `allowedEdgeMetrics`, `allowedMetricUnits` | AH определит свой минимальный набор |

### Направление влияния: AH → ядро, не наоборот

AH — не клиент ядра, которому нужен маппинг. AH — эксперимент, который должен **упростить** ядро, заменив монструозный пайплайн агентским подходом и показав, что 11 классов, 6 слоёв связей и компактная секторальная таксономия достаточны для тех же задач, которые ядро решает 20+ namespaces.

Когда AH стабилизируется, ожидаемый результат:
- Таксономия ядра **сжимается** в сторону AH-шной (меньше namespaces, консолидация `/gov`→`/org`, `/fac`+`/deposit`→`/site`, `/loc`→`/geo`)
- Relation types ядра **чистятся** (убираются дублирующие, вводится `layer` вместо нагромождения edge modalities)
- Метрики, домены и секторы из ядра **переходят в AH как есть** (это то немногое, что ядро делает лучше)

## Setup

```bash
cd agents/vertex/agenthorde
python -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
export GOOGLE_CLOUD_PROJECT=your-project
# Optional: AGENTHORDE_MAX_SUPERVISOR_CYCLES, AGENTHORDE_SUPERVISOR_MODEL, AGENTHORDE_WORKER_MODEL, AGENTHORDE_CRITIC_MODEL
```

Use Application Default Credentials (`gcloud auth application-default login`).

## Run

```bash
python runner_cli.py --input-file fixtures/example_input.json
# or
python runner_cli.py --input-json '{"question":"Trace a plausible supply path for X under sanctions risk."}'
```

Validate input only (no API calls):

```bash
python runner_cli.py --validate-only --input-json '{"question":"test"}'
```

Lithium South America → Germany smoke (**5** supervisor cycles, ADC / `gcloud` project):

```bash
./scripts/run_lithium_supply_smoke.sh
# optional: path to another JSON {"question":"..."}
./scripts/run_lithium_supply_smoke.sh /path/to/input.json
```

## Tests

From repository root:

```bash
PYTHONPATH=agents/vertex/agenthorde python -m unittest discover -s agents/vertex/agenthorde/tests -p 'test_*.py' -v
```

## Models

Defaults: supervisor + workers `gemini-3-flash-preview`, critic `gemini-3.1-pro-preview`. Override with env vars listed in `agenthorde/config.py`. No hidden aliasing to other model IDs.

## Прогоны: что было «ошибкой» и как исправлено

На ранних прогонах в stderr встречалось следующее (не обязательно падение процесса):

| Симптом | Причина | Исправление |
|--------|---------|-------------|
| **App name mismatch** (`vertex_agenthorde` vs каталог `agents`) | Эвристика ADK по пути к модулю супервайзера | По умолчанию `APP_NAME=agents` в `agenthorde/config.py` (переопределение: `AGENTHORDE_APP_NAME`). |
| **AFC disabled / tools incompatible** | В списке тулов смешивался встроенный `google_search` из ADK с обычными Python-callable | Свой **`web_search`** на `genai` + `GoogleSearch()`; все тулы — callables (`agenthorde/tools/builtins.py`). |
| **Пустой `toolLedger` / модель без поиска** | Инструкция не требовала вызовов; плюс смесь тулов выше | Жёсткие требования в промпте аналитика + леджер на каждом туле. |
| **`reasoningGraph` пустой при заполненном analyzer** | Устаревший `pack` в памяти / merge не доезжал до финального дампа | Перезагрузка `pack` из `state` по циклам, merge после аналитика, финальный merge перед ответом (`supervisor.py`). |
| **`UserWarning: [EXPERIMENTAL] PLUGGABLE_AUTH`** | ADK при импорте | Подавление известного шума в `runner_cli` через `agenthorde/quiet_vertex_logs.py` (до импорта ADK). |
| **`Warning: non-text parts … function_call`** | `google.genai` логирует при чтении `.text`, когда в ответе есть вызовы функций (норма для AFC) | Фильтр на logger **`google_genai.types`** (имя в SDK) в `quiet_vertex_logs.py`. |

Чтобы снова видеть все предупреждения SDK: **`AGENTHORDE_VERBOSE_VERTEX_LOGS=1`**.
