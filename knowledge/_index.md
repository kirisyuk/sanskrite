# Knowledge Base — Igor Kirisyuk

Last updated: 2026-03-20

## Structure

### geopolitics/
Regional and global political analysis through socioeconomic and sociocultural lenses.
- `middle-east/` — Iran, Gulf, Israel, regional economy
- `central-asia/` — Kazakhstan and neighbors
- `global/` — everything else

### ai-epistemology/
AI through the lens of epistemology, cognition theory, and paradigm shifts.
- `paradigm-shifts/` — baseline assumption changes in the AI era
- `cognition/` — theory of knowledge as applied to AI

### social-dynamics/
Sociocultural anthropology angle on politics, behavior, culture.

### etigram/
Work on Etigram — Decision Intel / Supply Chain / Forecasting / Risk.
- `product/` — product thinking, specs, roadmap
- `research/` — market, competitors, landscape
- `clients/` — client profiles and notes

### essays/
Long-form writing on any topic. Cross-linked with domain folders via tags.
Format: each essay has frontmatter with `tags:` and `related:` fields.

## Tag conventions
- `#geopolitics` `#middle-east` `#iran` `#gulf`
- `#ai` `#epistemology` `#paradigm-shift` `#cognition`
- `#anthropology` `#social-dynamics`
- `#etigram` `#supply-chain` `#forecasting`
- `#essay`

## Routing rules (for agents)
- Iran/Gulf reports → `geopolitics/middle-east/`
- Kazakhstan research → `geopolitics/central-asia/`
- AI/epistemology essays → `ai-epistemology/` + copy ref in `essays/`
- Client materials → `etigram/clients/`
- Standalone essays → `essays/`
