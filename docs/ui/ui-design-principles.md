## Horizon Web UI Design Principles

This document captures the current design system and principles implemented in the web application. It describes layout structure, typography, color tokens, components (buttons, inputs, tabs, modals), overlays, and responsive behavior, with guidance for building new UI consistently.

### Design Philosophy

- **Dark-first, light-ready**: The interface defaults to dark mode with full light theme overrides via `data-theme` CSS variables.
- **Calm surfaces, minimal borders**: Elevation is conveyed mostly by subtle surface background shifts, not heavy shadows or borders.
- **Tight, information-dense UI**: Base font size is compact, spacing follows an 8px grid with condensed vertical rhythm.
- **Token-driven styling**: Colors and elevations use CSS custom properties; avoid hard-coded hex values in components.
- **Accessible contrast**: Text and key UI affordances use strong contrast; muted content uses calibrated secondary/tertiary tokens.

## Theming and Tokens

Theme is toggled by setting `document.documentElement` attribute `data-theme` to `"dark"` or `"light"` (persisted in `localStorage` under `etg.theme`).

### Token Namespaces

- **Backgrounds**: `--bg-body`, `--bg-canvas-deep`, `--bg-sidebar`, `--bg-elevated`, `--bg-surface`, `--bg-content`
- **Borders**: `--border-none`, `--border-subtle`
- **Text**: `--text-primary`, `--text-secondary`, `--text-muted`, `--text-disabled`, `--text-on-light`, `--text-heading`, `--text-secondary-strong`, `--text-dim`, `--text-tertiary-weak`
- **Accents**: `--accent-brand`, `--accent-badge`, `--accent-success`, `--accent-danger`, `--accent-warning`, `--accent-research`, `--accent-workspace`
- **Surfaces & neutrals**: `--surface-700`, `--surface-750`, `--surface-770`, `--neutral-50`
- **Precomputed alphas**: `--brand-10/20/30/35`, `--danger-10/15/20/30`, `--success-20`, `--warning-10/20`, `--blue600-20/30`, etc.
- **Overlays**: `--overlay-dark-95`, `--overlay-deep-95`, `--overlay-line-90`, `--overlay-30/40/50/60/80`
- **Misc**: Chart palette `--chart-1..10`, additional hues (violet/amber/pink/green/gray), avatar backgrounds.

Tokens have complete dark and light definitions in `src/index.css` so components can rely solely on variable usage for theme correctness.

### Usage Guidance

- Use `var(--accent-brand)` for primary actions; on hover use a slightly stronger brand (`--brand-hover` or alpha variant `--brand-10/20`).
- Use `--surface-700` for active tab/chip backgrounds; `--surface-770` for light theme borders.
- Avoid `border` for elevation; prefer `background-color: var(--bg-elevated)` and spacing.
- Never hardcode hex; map all fills, strokes, and text colors to tokens.

## Typography

- **Font family**: Inter (`@fontsource/inter` weights 400/500/600).
- **Base**: Body at 13px (`0.8125rem`), line-height 1.5, weight 400.

Scale:
- `h1`: 18px, 600, line-height 26px
- `h2`: 16px, 600, line-height 24px
- `h3–h6`: 13px, 500, line-height 20px
- `p`: 13px, line-height 22px

Utility classes:
- `.text-caption` (12px, secondary)
- `.text-metric` (24px, 600)
- `.text-secondary`, `.text-heading`, `.text-disabled`

Guidelines:
- Keep headings concise; avoid oversized typography.
- Prefer semantic HTML (`h1..h3`, `p`) with tokenized colors.

## Layout

Top-level structure uses CSS utility classes defined in `src/index.css` and Tailwind utilities:

- `.layout-main`: Flex row, full-viewport height, gradient background on `html`.
- `.sidebar`: 240px min width (expandable/collapsible), `--bg-sidebar`, scrollable content.
- `.topbar`: 56px height, compact tabs and controls.
- `.main-content` → `.content-area`: Column with `overflow-y: auto` and min-height guards.
- `.card`: `--bg-elevated`, radius 8px, padding 20px, borderless.

Responsive:
- ≤768px: narrower sidebar, additional padding; height constraints preserved.
- ≤640px: sidebar hidden, topbar gains horizontal padding, content rounding/padding adjusts.

Spacing & rhythm:
- Primary gaps at 8/16/24px (`.gap-2/4/6`).
- Rounded corners are 6–8px (`.rounded`, `.rounded-lg`, `.rounded-full`).

## Components

### Buttons

Base class:
- `.btn`: inline-flex center alignment, 8px vertical × 16px horizontal padding, 13px/500 type, radius 6px, 150ms transitions.

Variants:
- `.btn-primary`: `background: var(--accent-brand)`, `color: var(--text-on-light)`; hover to brand-hover.
- `.btn-secondary`: `background: var(--bg-elevated)`, `border: var(--border-subtle)`, `color: var(--text-primary)`.
- `.btn-ghost`: transparent background, brand-colored text; hover uses `brand-10`.
- `.btn-danger`: `background: var(--accent-danger)`, white text.
- `.btn-active`: active/selected state with brand alphas; can be combined with `.btn-secondary`.

Notes:
- Some legacy areas use `btn-gemini` variants. Prefer migrating to `.btn` tokens on new/updated components.

### Inputs

- `.input`: Full width, 8×12px padding, 14px (`0.875rem`) text, `--bg-elevated` background, `--border-subtle` border, radius 6px, focus ring via brand alpha.
- Placeholder text uses `--text-muted`.

### Badges / Pills

- `.pill` base (uppercase 12px, subtle letter spacing), with variants `.pill-success`, `.pill-danger`, `.pill-warning` using alpha surfaces and corresponding accent text tokens.

### Status Indicators

- `.status-dot` 8×8px circular; `.status-success|danger|warning|disabled` map to accent tokens.

### Tabs

- `.tabs` arrange tab buttons with 16px gap.
- `.tab` compact 13px/500, subtle text color.
- `.tab-active` uses `--surface-700` background, primary text, 600 weight.

### Modals

Overlay:
- Fullscreen `fixed inset-0`, dark scrim `bg-black bg-opacity-50`, centered flex, `z-index: 50` or higher if needed.

Containers:
- Dark context (e.g., Inquiry): `background: #` via `--bg-elevated`/`gray-800`, radius 8px, tokenized border using `--border-subtle`, comfortable padding (`p-6`), constrained width (`max-w-xl` with responsive margins).
- Light context (e.g., destructive confirmation): White container, soft border/shadow and rounded corners.

Actions:
- Right-aligned buttons, compact 12–13px text, tokenized hover states using brand/danger alphas.

Accessibility:
- Provide `aria-label` for close buttons, ensure focus is placed inside modal on open and returned on close, and that Escape and backdrop click behavior are consistent. Use role="dialog" (or `alertdialog` for destructive) with `aria-modal="true"` when implementing focus trapping.

### Navigation

- Side navigation collapses to icons-only at narrow widths (toggle button in header).
- Section headers use `.text-caption .text-heading` with tokenized dividers (`--border-subtle`).
- Action buttons in nav use `.btn` with variant switching when collapsed.
- Search uses `.input`.

### Notifications Dropdown

- Uses overlay container (`--overlay-deep-95` background with `--overlay-line-90` borders) and brand-tinted hover states (`--brand-10`).
- Unread indicator uses small blue dot; items show title, entities summary, and timestamps.

### Scrollbars

- Thin scrollbar styling with `--border-subtle` thumb; stable gutters to prevent layout shift.

## Patterns and Do/Don’t

Do:
- Use CSS variables for all colors, including SVG `stroke`/`fill`.
- Prefer elevation via `--bg-elevated` and surface tokens instead of borders.
- Keep spacing tight and consistent with 8px grid; use provided gap utilities.
- Respect theme: never hardcode light-only colors.

Don’t:
- Don’t introduce new arbitrary color hexes.
- Don’t rely on heavy box-shadows to convey hierarchy.
- Don’t increase font sizes ad hoc; use the documented scale.

## Example Snippets

Button:

```tsx
<button className="btn btn-primary">
  Save changes
</button>
```

Input with label:

```tsx
<label className="text-caption text-secondary">Project name</label>
<input className="input" placeholder="Enter a name" />
```

Modal shell:

```tsx
<div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50" role="dialog" aria-modal="true">
  <div className="rounded-lg p-6 w-full max-w-xl mx-4 border" style={{ backgroundColor: 'var(--bg-elevated)', borderColor: 'var(--border-subtle)' }}>
    ...
  </div>
  {/* Implement focus trap in React when wiring up */}
  
</div>
```

Active tab:

```tsx
<button className="tab tab-active">Discovery</button>
```

## Implementation Notes

- Tailwind config is minimal; tokens and global utilities live in `src/index.css`.
- Icons and charts in light theme get explicit tokenized `color`/`stroke` overrides to maintain contrast.
- Skeleton loaders use a global `nv-shimmer` animation with light-theme adjustments.

## Checklist for New Components

- Uses semantic HTML and ARIA roles.
- Colors come from tokens only; supports both themes out of the box.
- Spacing follows 8px grid; corners 6–8px; font sizes from the documented scale.
- Focus states visible; keyboard and screen-reader accessible.
- No hard-coded borders unless for information hierarchy; prefer surface tokens.
- Modals implement overlay, focus trap, Escape/backdrop semantics, and labeled actions.


