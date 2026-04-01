import React, { useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { HorizonEntityInspector } from './HorizonEntityInspector';
import { MapObjectDetail, type MapObjectInspectorPayload } from './MapObjectDetail';
import { LensDocumentsPanel } from './LensDocumentsPanel';
import { ThemeContext } from './ThemeProvider';

export type MapDrawerPanelType = 'view' | 'simulation' | 'export-tools';

export type HorizonPanelType = 'object-inspector' | 'documents' | 'map-drawer' | null;

interface HorizonDrawersProps {
  panelType: HorizonPanelType;
  isMobile: boolean;
  drawerWidth: number;
  setDrawerWidth: (width: number) => void;
  closeAllPanels: () => void;
  getPanelVisibilityStyles: (visible: boolean) => React.CSSProperties;
  // Object Inspector props
  projectId?: string | null;
  lensId?: string | null;
  lensName?: string | null;
  selectedNodeId: string | null;
  selectedNodeData: any;
  selectedEdgeId: string | null;
  selectedEdgeData: any;
  mapInspectorState?: MapObjectInspectorPayload | null;
  mapDrawerTitle?: string | null;
  setMapDrawerHost?: ((el: HTMLDivElement | null) => void) | null;
}

export const HorizonDrawers: React.FC<HorizonDrawersProps> = ({
  panelType,
  isMobile,
  drawerWidth,
  setDrawerWidth,
  closeAllPanels,
  getPanelVisibilityStyles,
  projectId,
  lensId,
  lensName,
  selectedNodeId,
  selectedNodeData,
  selectedEdgeId,
  selectedEdgeData,
  mapInspectorState = null,
  mapDrawerTitle = null,
  setMapDrawerHost = null,
}) => {
  const { t } = useTranslation();
  const { theme } = React.useContext(ThemeContext);
  const drawerRef = useRef<HTMLDivElement>(null);

  // Sync drawer width when resized natively
  React.useEffect(() => {
    const drawer = drawerRef.current;
    if (!drawer) return;
    
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        const width = entry.contentRect.width;
        if (Math.abs(width - drawerWidth) > 1) {
          // Avoid infinite loops
          setDrawerWidth(width);
        }
      }
    });
    
    observer.observe(drawer);
    return () => observer.disconnect();
  }, [drawerWidth, setDrawerWidth]);

  if (panelType === null) {
    return null;
  }

  return (
    <>
      {/* Mobile overlay backdrop */}
      {isMobile && (
        <div
          onClick={closeAllPanels}
          style={{
            position: 'fixed',
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            backgroundColor: 'rgba(0, 0, 0, 0.5)',
            zIndex: 40,
          }}
        />
      )}
      <div
        ref={drawerRef}
        className="min-h-0 flex flex-col drawer-resizable"
        style={{
          width: `${drawerWidth}px`,
          minWidth: '300px',
          maxWidth: '800px',
          flexShrink: 0,
          flexGrow: 0,
          position: isMobile ? 'fixed' : 'relative',
          top: isMobile ? 0 : 'auto',
          right: isMobile ? 0 : 'auto',
          bottom: isMobile ? 0 : 'auto',
          height: '100%',
          maxHeight: '100%',
          overflow: 'hidden',
          zIndex: isMobile ? 50 : 'auto',
          // Keep the drawer as a consistent "surface" on desktop too,
          // otherwise the transparent gutter reads like wasted empty space over the canvas.
          backgroundColor: 'var(--bg-sidebar)',
          borderLeft: '1px solid var(--border-subtle)',
          ...(isMobile
            ? {
                maxWidth: '85vw',
                borderRadius: 0,
              }
            : {}),
        }}
      >
        {/* Resize handle on left edge */}
        {!isMobile && (
          <div
            onMouseDown={(e) => {
              e.preventDefault();
              const startX = e.clientX;
              const startWidth =
                drawerRef.current?.clientWidth || drawerWidth;
              
              const onMove = (ev: MouseEvent) => {
                const delta = startX - ev.clientX; // Handle on left: moving right (ev.clientX > startX) decreases width
                const newWidth = Math.max(
                  300,
                  Math.min(800, startWidth + delta),
                );
                if (drawerRef.current) {
                  drawerRef.current.style.width = `${newWidth}px`;
                  setDrawerWidth(newWidth);
                }
              };
              
              const onUp = () => {
                window.removeEventListener('mousemove', onMove);
                window.removeEventListener('mouseup', onUp);
              };
              
              window.addEventListener('mousemove', onMove);
              window.addEventListener('mouseup', onUp);
            }}
            style={{
              position: 'absolute',
              left: -4,
              top: 0,
              bottom: 0,
              width: '8px',
              cursor: 'col-resize',
              zIndex: 10,
            }}
          />
        )}
        {/* Drawer content */}
        <div
          className="min-h-0 flex flex-col"
          style={{
            background: 'transparent',
            height: '100%',
            overflow: 'hidden',
          }}
        >
          {/* Panel container - ALWAYS in DOM, never removed */}
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              flex: panelType !== null ? 1 : 0,
              minWidth: panelType !== null ? 'auto' : 0,
              visibility:
                panelType !== null ? 'visible' : 'hidden',
              pointerEvents: panelType !== null ? 'auto' : 'none',
              height: '100%',
              minHeight: 0,
              overflow: 'hidden',
            }}
          >
            <div
              className="mt-3 p-3"
              style={{
                display: 'flex',
                flexDirection: 'column',
                flex: 1,
                minHeight: 0,
                overflowY: 'auto',
              }}
            >
              <div
                className="flex items-center justify-between"
                style={{
                  position: 'sticky',
                  top: 0,
                  paddingBottom: 8,
                  marginBottom: 8,
                  zIndex: 1,
                }}
              >
                <div className="flex items-center gap-3 flex-1 min-w-0">
                  <h3
                    className="text-13 font-medium"
                    style={{ color: 'var(--text-heading)' }}
                  >
                    {panelType === 'object-inspector'
                      ? (mapInspectorState?.object ? 'Object' : 'Object Inspector')
                      : panelType === 'documents'
                        ? 'Lens Documents'
                        : panelType === 'map-drawer'
                          ? (mapDrawerTitle || 'Map Panel')
                        : 'Horizon'}
                  </h3>
                </div>
                
                <button
                  type="button"
                  aria-label="Close panel"
                  title="Close"
                  className="p-2 rounded-lg transition-colors flex-shrink-0"
                  style={{
                    backgroundColor: 'transparent',
                    color: 'var(--text-secondary-strong)',
                  }}
                  onMouseEnter={(e) => {
                    (
                      e.currentTarget as HTMLButtonElement
                    ).style.backgroundColor = 'var(--white-10)';
                  }}
                  onMouseLeave={(e) => {
                    (
                      e.currentTarget as HTMLButtonElement
                    ).style.backgroundColor = 'transparent';
                  }}
                  onClick={closeAllPanels}
                >
                  <svg
                    className="w-4 h-4"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                  >
                    <path
                      strokeLinecap="round"
                      strokeWidth={2}
                      d="M6 6l12 12M18 6L6 18"
                    />
                  </svg>
                </button>
              </div>
              
              {/* Panel content - always render all panels, hide inactive ones */}
              <div
                style={{
                  flex: 1,
                  minHeight: 0,
                  overflowY: 'auto',
                  paddingRight: 4,
                }}
              >
                {/* Object Inspector Panel */}
                {panelType === 'object-inspector' && (
                  <div style={getPanelVisibilityStyles(true)}>
                    {mapInspectorState?.object ? (
                      <MapObjectDetail
                        {...mapInspectorState}
                        isDark={theme === 'dark'}
                        paletteTheme={theme === 'dark' ? 'dark' : 'light'}
                        layout="panel"
                      />
                    ) : (
                      <HorizonEntityInspector
                        nodeId={selectedNodeId}
                        nodeData={selectedNodeData}
                        edgeId={selectedEdgeId}
                        edgeData={selectedEdgeData}
                        lensId={lensId || null}
                      />
                    )}
                  </div>
                )}
                {panelType === 'documents' && (
                  <div style={getPanelVisibilityStyles(true)}>
                    <LensDocumentsPanel
                      projectId={projectId || null}
                      lensId={lensId || null}
                      lensName={lensName || null}
                    />
                  </div>
                )}
                {panelType === 'map-drawer' && (
                  <div style={getPanelVisibilityStyles(true)}>
                    <div
                      ref={setMapDrawerHost || undefined}
                      style={{
                        minHeight: 0,
                        display: 'flex',
                        flexDirection: 'column',
                      }}
                    />
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      </div>
    </>
  );
};
