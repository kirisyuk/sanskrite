## State and Pages

### Stores
- Discovery store: `web/src/stores/discoveryStore.ts`
- Horizon realtime / lens UI state: `web/src/stores/horizonRealtimeStore.ts`
- User/auth state: `web/src/contexts/AuthContext.tsx`

### Pages
- Discovery: `web/src/pages/DiscoveryPage.tsx` (и связанные экраны в `web/src/pages/*`)
- Horizon (граф / линзы): `web/src/pages/HorizonPage.tsx`

### Components
- AuthGuard: `web/src/components/AuthGuard.tsx`
- UserProfile: `web/src/components/UserProfile.tsx`
- Horizon canvas / прогресс: `web/src/components/LensCanvas.tsx`, `LensCanvasProgress.tsx`, `WorldMapPanel.tsx` (и др. по префиксу/импортам в `HorizonPage`)


