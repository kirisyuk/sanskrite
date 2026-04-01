## Horizon Frontend Architecture Principles

This document outlines the architectural principles that guide the web frontend. It reflects the current implementation and provides guidance for evolving the codebase. Any Azure-specific details are treated as replaceable integration points.

### Goals

- Predictable state and flow with minimal coupling
- Clear separation of concerns between view, state, and side effects
- Tokenized design and themeable UI
- Resilient networking and real-time updates
- Strong typing for all API boundaries

## High-Level Structure

- Entrypoint: `main.tsx` renders `App` inside React `StrictMode`.
- Application shell: `App.tsx` coordinates auth gating, top-level section routing (via `useUIStore`), and websocket providers.
- UI is feature-oriented under `src/components/*` and cross-cutting concerns under `src/hooks`, `src/services`, `src/store`, and `src/types`.
- Styling is driven by `index.css` CSS variables and Tailwind utilities.

## State Management

We use `zustand` for local and global app state.

- UI state (`useUIStore`): navigation, current section/view, language, drawer state, selection. Persist only minimal UI session state when useful.
- Domain state:
  - Research (`useResearchStore`): list, active research, CRUD orchestration
  - Session (`useSessionStore`): sessions grouped by research, active session, legacy compatibility helpers (expiry), persisted subset
  - Notifications (`useNotificationsStore`): in-app signals feed with hydration from storage and WS
  - Additional stores: graph, filters, user cache, etc.

Principles:
- Keep stores small and focused; colocate domain logic near the store and hooks using it.
- Expose computed getters for frequently needed derived state.
- Persist only serializable, minimal slices (via zustand `persist` partialize).

## Data Access Layer

- `services/api.ts` encapsulates REST calls via a typed `HorizonAPI` wrapper around Axios.
- All HTTP logic (headers, retries, error normalization) lives inside the API client. Components do not set headers directly.
- DTOs and entities are typed via `src/types/*`. Prefer importing types from the API layer to avoid duplication.
- Base URL is derived from `VITE_API_URL`; auth and tenant headers are injected by interceptors. These integration points are replaceable.

Principles:
- Treat network as untrusted; normalize shapes and support variant casing when necessary.
- Coalesce compatibility logic in the API client (not in components/stores).
- Re-export the common types from the API layer to make imports ergonomic.

## Async Orchestration and Hooks

- React Query is used for data fetching hooks (`useHorizonApi.ts` integrates `useQuery`/`useMutation`).
- Custom hooks encapsulate non-trivial flows and real-time behavior:
  - WebSockets: `useNotificationsWebSocket`, `useConsoleWebSocket`, `useDiscoveryWebSocket`, etc.
  - Auth: `useAuth` bridges Firebase auth to `useUserStore` and gates the app in `App.tsx`.
  - Analytics/Reality/SituationBoard specific flows: dedicated hooks per feature.

Principles:
- Components should not call the API client directly for recurring queries; use a hook that defines cache keys and enables flags.
- Hooks own their side effects and lifecycles (subscribe/unsubscribe, negotiate WS URLs, hydrate from backend).
- Prefer returning typed results and helpers over exposing internal details (e.g., sockets or raw axios instances) from hooks.

## View Composition and Navigation

- No external router; top-level section switching is controlled by `useUIStore.activeSection` and flags in `App.tsx`.
- Feature views are composed from small, testable UI components; heavy logic is kept in hooks/stores.
- Providers (e.g., `ConsoleWebSocketProvider`) wrap relevant views to scope context and lifecycles.

Principles:
- Keep `App.tsx` logic minimal: auth gate, section selection, and providers only.
- Avoid prop drilling through deep trees; prefer context or stores for cross-cutting state.

## Real-Time and Offline Resilience

- WebSocket usage negotiates URLs via the API layer when available; falls back to configured endpoints.
- Notification feed hydrates from local storage first, then merges backend recent signals.
- Reconnect policies are conservative: retry with backoff and high tolerance for intermittent connectivity.

Principles:
- Always support graceful degradation: if WS fails, the app should still function with periodic HTTP fetches.
- Avoid duplicate events by normalizing IDs and performing idempotent upserts in stores.

## Error Handling and Observability

- API client interceptors log and normalize errors; 401 triggers token refresh and/or auth clear.
- Validation and missing header issues surface with actionable logs and store inspection hints.
- UI favors small, contextual error messaging over global blockers; critical failures render a simple splash.

Principles:
- Fail fast in API layer; bubble structured errors to hooks/components for user-friendly rendering.
- Keep logs informative in development; avoid PII in production logs.

## Theming and Accessibility

- Theme toggled via `data-theme` attribute; CSS variables define color tokens for dark/light.
- All colors and fills must come from tokens; SVGs use tokenized `color`/`stroke`.
- Modals and overlays include `aria-label`, `role="dialog"`/`aria-modal="true"`; implement focus management and Escape/backdrop semantics.

Principles:
- Keep components theme-agnostic by relying on tokens only.
- Provide keyboard and screen-reader support for interactive components.

## Testing Strategy

- Unit tests for stores and hooks validate behavior contract (`*.test.ts(x)` colocated).
- Component tests use Testing Library and MSW for API mocking.

Principles:
- Test public contracts (store actions, selector outputs, hook behavior), not implementation details.
- Prefer deterministic tests; mock time and network where necessary.

## Build and Tooling

- Vite + TypeScript drive fast builds and dev server.
- ESLint + Prettier enforce consistent code style.
- Tailwind v4 and `index.css` provide tokens/utilities.

Principles:
- Keep `tailwind.config.js` minimal; centralize tokens in CSS variables for theme control.
- Avoid custom webpack/Vite magic; prefer conventional configuration.

## Replaceable Integrations

The following integration points are intentionally abstracted and can be replaced without changing component code:

- Auth provider: `firebase.ts` encapsulates initialization and exposes safe handles; `useAuth` adapts to `useUserStore`.
- API backend: `HorizonAPI` encapsulates base URL, headers, and endpoint shapes; swapping the backend means updating this client.
- WebSocket negotiation: API client provides negotiate endpoints; fallbacks read from env.

Migration guidance:
- Introduce new backend endpoints inside `HorizonAPI`; adapt DTOs and normalization there.
- Keep stores and hooks stable by preserving their public signatures and semantics.

## Example Patterns

React Query data hook:

```ts
export function useSessionData(researchId: string, sessionId: string, options?: UseQueryOptions<SessionDataDto, Error>) {
  return useQuery({
    queryKey: ['sessionData', researchId, sessionId],
    queryFn: () => apiClient.getSessionData(researchId, sessionId),
    enabled: !!researchId && !!sessionId && (options?.enabled ?? true),
    ...options,
  });
}
```

Zustand derived getter:

```ts
getActiveResearch: () => {
  const { researches, activeResearchId } = get();
  return activeResearchId ? researches.find(r => r.id === activeResearchId) || null : null;
}
```

WebSocket negotiation with fallback:

```ts
const { url } = await apiClient.negotiateConsoleWebSocket().catch(() => ({ url: import.meta.env.VITE_WEBSOCKET_URL }));
setSocketUrl(url || null);
```

## Checklist for New Features

- State: Add or extend a focused store; define clear actions and selectors.
- Data: Add API methods to `HorizonAPI` and wrap in a `useQuery`/`useMutation` hook.
- UI: Compose small components; rely on tokens and utility classes.
- Real-time: Consider WS subscribe/update flows; ensure idempotency.
- Errors: Normalize in API; show contextual feedback in UI.
- Tests: Add store/hook tests; use MSW for network.


