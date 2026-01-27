# Risk Shield — 8-секционный визуализатор рисков

> Концепция визуализации рисков вокруг сущностей в графе Horizon, вдохновлённая игровыми интерфейсами.

## Концепция

При клике на сущность в графе вокруг неё появляется **"энергетический щит"** — октагональная обводка, где каждый из 8 сегментов отвечает за определённую категорию рисков.

```
                    ┌───────────┐
                   ╱ GEOPOLITICAL╲
           ┌──────╱   🌍          ╲──────┐
          ╱ REGULATORY            SUPPLY ╲
         ╱   ⚖️                     🔗    ╲
        │                                  │
        │ KEY PERSON  ┌──────────┐  FINANCIAL
        │    👤       │  NODE    │    💰   │
        │             └──────────┘         │
         ╲                                ╱
          ╲ OPERATIONAL          ESG     ╱
           ╲   🏭               🌱      ╱
            └──────╲          ╱───────┘
                    ╲ CYBER  ╱
                     └──⚡──┘
```

## 8 сегментов риска

| # | Сегмент | Иконка (Lucide) | Цвет | HEX | Описание |
|---|---------|-----------------|------|-----|----------|
| 1 | **Geopolitical** | `Globe` | Amber | `#F59E0B` | Санкции, политические связи, trade restrictions, юрисдикционные риски |
| 2 | **Supply Chain** | `Link` | Teal | `#14B8A6` | Зависимости от поставщиков, single points of failure, tier-2/3 suppliers |
| 3 | **Financial** | `Coins` | Emerald | `#10B981` | Кредиторы, инвесторы, долговая нагрузка, контрагентные риски |
| 4 | **Cyber/Tech** | `Zap` | Electric Blue | `#3B82F6` | Технологические зависимости, data exposure, IP риски, cyber threats |
| 5 | **ESG** | `Leaf` | Lime | `#84CC16` | Environmental violations, labor issues, governance red flags |
| 6 | **Operational** | `Factory` | Slate | `#64748B` | Facilities, производственные риски, географическая концентрация |
| 7 | **Key Person** | `UserCircle` | Violet | `#8B5CF6` | Топ-менеджмент, бенефициары, PEP-статусы, перекрёстные директорства |
| 8 | **Regulatory** | `Scale` | Rose | `#F43F5E` | Лицензии, compliance-статусы, pending regulations по индустрии |

## Канонизация: RiskShield как метрики/субметрики (Horizon-native)

В текущей системе Horizon уже есть всё необходимое, чтобы **риски были “оцифрованы, понятны, трассируемы”** и в будущем стали кандидатами на propagation:

- **`HorizonFragment.state`** хранит метрики как `map[string]MetricValue` (со временем, confidence и provenance).
- **`Metric Registry`** (Firestore `horizon_metric_specs`) задаёт контракт на метрики: kind/unit/aggregation/decay/threshold/derivedFrom.
- **`Signals`** (facts → synthesis → signal facts с `metricUpdates`) дают воспроизводимые “дельты”.
- **`RollupService`** умеет агрегировать сигналы и выдавать explain trace (top contributors, rules).
- **`PropagationEngine`** уже распространяет **дельты метрик** по графу (threshold + damping + policy hooks).

Отсюда предложение: **сегменты RiskShield — это просто “rollup-метрики” (derived metrics)**, поверх базовых risk-метрик и/или сигналов.

### 1) Нейминг метрик (рекомендуемый)

Используем канонический префикс, который уже применяется Scout’ом:

- **Базовые риск-метрики**: `/metric/risk/<metric_name>` (пример: `/metric/risk/sanction_exposure`)
- **Сегмент-роллапы щита** (8 штук): `/metric/risk/risk_segment_<segment_id>`

Список segment metric keys:
- `/metric/risk/risk_segment_geopolitical`
- `/metric/risk/risk_segment_supply`
- `/metric/risk/risk_segment_financial`
- `/metric/risk/risk_segment_cyber`
- `/metric/risk/risk_segment_esg`
- `/metric/risk/risk_segment_operational`
- `/metric/risk/risk_segment_keyperson`
- `/metric/risk/risk_segment_regulatory`

Примечание: base-name специально включает `risk_segment_…`, чтобы избежать коллизий при `metricBaseName()` (в пропагации оно берётся как последний path segment).

### 2) Привязка сегментов к сферам (spherePaths)

Сферы уже посеяны (см. `horizon_seed_taxonomy`). Их можно использовать как “каноническую привязку смысла” для фактов/сигналов и explain.

Рекомендуемая карта (v1):

| Segment | spherePaths (patterns) |
|--------|-------------------------|
| Geopolitical | `/sphere/geopolitics/*`, `/sphere/policy/sanctions`, `/sphere/policy/export-controls`, `/sphere/policy/trade` |
| Regulatory | `/sphere/policy/regulation`, `/sphere/policy/licensing`, `/sphere/policy/customs`, `/sphere/policy/tax`, `/sphere/legal/*`, `/sphere/compliance/*` |
| Supply Chain | `/sphere/logistics/*`, `/sphere/operations/procurement`, `/sphere/operations/continuity` |
| Financial | `/sphere/finance/*`, `/sphere/finance/credit`, `/sphere/finance/liquidity`, `/sphere/finance/fx`, `/sphere/finance/insurance` |
| Cyber/Tech | `/sphere/cyber/*`, `/sphere/digital`, `/sphere/infrastructure/cloud`, `/sphere/infrastructure/telecom`, `/sphere/legal/ip` |
| ESG | `/sphere/esg/*`, `/sphere/environment/*`, `/sphere/labor/*` |
| Operational | `/sphere/operations/*`, `/sphere/infrastructure/*`, `/sphere/security/physical` |
| Key Person | `/sphere/governance/*`, `/sphere/workforce`, `/sphere/social/information/*` |

### 3) Привязка сегментов к метрикам (metric groups)

На старте можно опираться на уже существующие “стандартизированные” risk-метрики (встречаются в таксономии/Scout):

| Segment | базовые метрики (v1 candidates) |
|--------|----------------------------------|
| Geopolitical | `/metric/risk/sanction_exposure`, `/metric/risk/geopolitical_alignment`, `/metric/risk/stability_index`, `/metric/risk/corruption_vulnerability` |
| Regulatory | `/metric/risk/sanction_exposure` (как прокси), далее расширить на `license_*`, `compliance_*`, `litigation_*` |
| Supply Chain | `/metric/risk/supply_chain_criticality` (+ будущие: `supplier_concentration`, `single_source_ratio`) |
| Financial | `/metric/risk/market_sentiment` (directional как сигнал), далее расширить на `credit_risk`, `liquidity_risk` |
| Cyber/Tech | (v1: через signals по `/sphere/cyber/*`, далее добавить `/metric/risk/cyber_incident_severity`) |
| ESG | (v1: через signals по `/sphere/esg/*`, + `water_intensity`, `energy_intensity`, `labor_intensity`) |
| Operational | `/metric/risk/operational_health`, `/metric/risk/capacity_utilization` |
| Key Person | `/metric/risk/public_perception` (directional), далее добавить `pep_exposure`, `adverse_media` |

### 4) Как вычислять сегмент-роллап

Варианты (оба совместимы и могут сосуществовать):

- **A. Client-side rollup (быстро, без миграций)**: UI вычисляет сегменты из `fragment.state` по карте выше. Traceability берём из `MetricValue.provenance` (ограниченно) или через отдельный запрос explain.
- **B. Server-side rollup (правильнее, propagation-ready)**:
  - `RollupService` агрегирует **signals** (и/или базовые метрики) в 8 segment-metrics.
  - Пишем segment-metrics в `HorizonFragment.state` как обычные `MetricValue` (с observedAt/confidence/provenance).
  - Propagation течёт уже “по сегментам” с помощью propagation policies (metricPattern × relationType).

Рекомендация: начать с A для UX, быстро перейти на B для трассируемости и propagation.

## Расположение сегментов

Сегменты располагаются по часовой стрелке, начиная с 12:00 (top):

```typescript
const RISK_SEGMENTS = [
  { id: 'geopolitical', angle: 0,    icon: 'Globe',      color: '#F59E0B' },
  { id: 'supply',       angle: 45,   icon: 'Link',       color: '#14B8A6' },
  { id: 'financial',    angle: 90,   icon: 'Coins',      color: '#10B981' },
  { id: 'cyber',        angle: 135,  icon: 'Zap',        color: '#3B82F6' },
  { id: 'esg',          angle: 180,  icon: 'Leaf',       color: '#84CC16' },
  { id: 'operational',  angle: 225,  icon: 'Factory',    color: '#64748B' },
  { id: 'keyPerson',    angle: 270,  icon: 'UserCircle', color: '#8B5CF6' },
  { id: 'regulatory',   angle: 315,  icon: 'Scale',      color: '#F43F5E' },
] as const;

type RiskSegmentId = typeof RISK_SEGMENTS[number]['id'];
```

## Визуальные состояния

### Уровни риска

Каждый сегмент отображает уровень риска через opacity и glow-эффекты:

| Уровень | Значение | Визуализация |
|---------|----------|--------------|
| `none` | 0 | 20% opacity, без glow |
| `low` | 1 | 40% opacity |
| `medium` | 2 | 70% opacity |
| `high` | 3 | 100% opacity, subtle glow |
| `critical` | 4 | 100% + pulse animation + outer glow |

### CSS-переменные

```css
:root {
  /* Risk Shield */
  --risk-shield-gap: 2px;
  --risk-shield-thickness: 4px;
  --risk-shield-radius: 48px;
  
  /* Segment colors */
  --risk-geopolitical: #F59E0B;
  --risk-supply: #14B8A6;
  --risk-financial: #10B981;
  --risk-cyber: #3B82F6;
  --risk-esg: #84CC16;
  --risk-operational: #64748B;
  --risk-keyperson: #8B5CF6;
  --risk-regulatory: #F43F5E;
  
  /* Glow */
  --risk-glow-blur: 8px;
  --risk-glow-spread: 2px;
}
```

## Интерактивность

### Hover на сегменте

При наведении на сегмент:
1. Сегмент увеличивается (scale: 1.1)
2. Появляется tooltip с названием категории и summary
3. Подсвечиваются связанные ноды в графе

### Клик на сегменте

При клике:
1. Открывается боковая панель с детальным анализом по категории
2. В графе раскрываются связанные сущности (expand from segment)
3. Edge'ы фильтруются по релевантности к выбранной категории риска

### Пример: клик на "Geopolitical" для Nexperia B.V.

```
Nexperia B.V. [Geopolitical 🌍]
├── Wingtech Technology (Owner) → 🇨🇳 China jurisdiction
│   └── ⚠️ CFIUS scrutiny (semiconductor sector)
├── Zhang Xuezheng (Manager) → PEP check pending
└── Facilities in:
    ├── 🇩🇪 Germany — EU regulations apply
    ├── 🇬🇧 UK — Post-Brexit trade rules
    └── 🇵🇭 Philippines — Emerging market risk
```

## Модель данных

### RiskAssessment

```typescript
interface RiskAssessment {
  entityId: string;
  entityType: 'organization' | 'person' | 'facility';
  
  segments: {
    geopolitical: RiskLevel;
    supply: RiskLevel;
    financial: RiskLevel;
    cyber: RiskLevel;
    esg: RiskLevel;
    operational: RiskLevel;
    keyPerson: RiskLevel;
    regulatory: RiskLevel;
  };
  
  // Детали по каждому сегменту
  details: Record<RiskSegmentId, RiskDetail[]>;
  
  computedAt: Timestamp;
  sources: string[]; // откуда получены данные
}

type RiskLevel = 'none' | 'low' | 'medium' | 'high' | 'critical';

interface RiskDetail {
  id: string;
  title: string;
  description: string;
  severity: RiskLevel;
  relatedEntityIds: string[]; // для раскрытия в графе
  source: string;
  timestamp: Timestamp;
}
```

> Примечание (важно): в Horizon уже есть нативная модель метрик `HorizonFragment.state` и registry/propagation для них. Поэтому `RiskAssessment` стоит рассматривать как **UI view-model** (кэш/материализация по желанию), а не как первичную “истину”.

## Компонент React

```typescript
// web/src/components/graph/RiskShield.tsx

interface RiskShieldProps {
  entityId: string;
  assessment: RiskAssessment;
  isVisible: boolean;
  onSegmentClick: (segmentId: RiskSegmentId) => void;
  onSegmentHover: (segmentId: RiskSegmentId | null) => void;
}
```

## Интеграция с графом

Risk Shield появляется:
1. При клике на ноду (toggle)
2. При длительном hover (>500ms)
3. Программно через `graphStore.showRiskShield(entityId)`

Risk Shield скрывается:
1. При клике вне ноды
2. При перемещении графа (pan/zoom)
3. Программно через `graphStore.hideRiskShield()`

## Источники данных для сегментов

| Сегмент | Источники данных |
|---------|------------------|
| Geopolitical | Sanctions lists (OFAC, EU, UN), jurisdiction mapping, ownership chain analysis |
| Supply Chain | Horizon graph relationships, supplier dependencies, concentration analysis |
| Financial | Credit ratings, financial statements, debt analysis, investor network |
| Cyber/Tech | CVE databases, tech stack analysis, data breach history |
| ESG | ESG ratings (MSCI, Sustainalytics), violation databases, news monitoring |
| Operational | Facility mapping, geographic risk indices, operational incidents |
| Key Person | PEP databases, adverse media, directorship networks, UBO analysis |
| Regulatory | License registries, regulatory filings, enforcement actions |

## Приоритеты реализации

### Milestone 1: Visual MVP
- [ ] Компонент `RiskShield` с 8 сегментами
- [ ] Статические цвета и иконки
- [ ] Hover tooltips
- [ ] Интеграция с ReactFlow

### Milestone 2: Data Integration
- [ ] `RiskAssessment` модель в Firestore
- [ ] API endpoint `/api/horizon/entities/{id}/risk-assessment`
- [ ] Базовый scoring по доступным данным

### Milestone 3: Segment Expansion
- [ ] Клик на сегмент → раскрытие подграфа
- [ ] Фильтрация edges по категории риска
- [ ] Боковая панель с деталями

### Milestone 4: Advanced Features
- [ ] Real-time risk updates
- [ ] Historical risk trends
- [ ] Risk propagation по графу (каскадные риски)
- [ ] Export risk reports

---

*Последнее обновление: 2026-01-09*

