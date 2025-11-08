import React from 'react';
import { createPortal } from 'react-dom';

type Mode = 'ask' | 'bring_in' | 'edit' | 'opponent';

export interface AgentPresetItem {
  id: string;
  displayName: string;
  description?: string;
  expectedOutcome?: string;
  useCases?: string[];
  provider: string;
  modelAlias: string;
  capabilities?: { tools?: boolean; webSearch?: boolean; reasoning?: boolean };
  params?: { temperature?: number; systemInstruction?: string };
}

interface AgentPaletteProps {
  mode: Mode;
  visible: boolean;
  title?: string;
  subtitle?: string;
  searchPlaceholder?: string;
  askPlaceholder?: string;
  presets?: AgentPresetItem[];
  initialAskText?: string;
  anchorRect?: { top: number; left: number; width: number; height: number } | null;
  onClose: () => void;
  onSubmitAsk?: (text: string) => void;
  onSelectPreset?: (preset: AgentPresetItem) => void;
  contextNode?: React.ReactNode;
  submitLabel?: string;
}

export const AgentPalette: React.FC<AgentPaletteProps> = ({ mode, visible, title, subtitle, searchPlaceholder, askPlaceholder, presets, initialAskText, anchorRect, onClose, onSubmitAsk, onSelectPreset, contextNode, submitLabel }) => {
  const containerRef = React.useRef<HTMLDivElement | null>(null);
  const [search, setSearch] = React.useState('');
  const [askText, setAskText] = React.useState(initialAskText || '');
  const [hovered, setHovered] = React.useState<AgentPresetItem | null>(null);

  React.useEffect(() => { setAskText(initialAskText || ''); }, [initialAskText]);

  React.useEffect(() => {
    if (!visible) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
      if (mode === 'ask' && e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault();
        onSubmitAsk && onSubmitAsk(askText.trim());
      }
    };
    const onDown = (e: MouseEvent) => {
      const el = containerRef.current;
      if (el && !el.contains(e.target as Node)) onClose();
    };
    document.addEventListener('keydown', onKey);
    document.addEventListener('mousedown', onDown, { capture: true });
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('mousedown', onDown, { capture: true } as any);
    };
  }, [visible, mode, askText, onClose, onSubmitAsk]);

  if (!visible) return null;

  const widthPx = anchorRect ? Math.max(360, Math.round(anchorRect.width * 0.7)) : 640;
  const leftPx = anchorRect ? Math.round(anchorRect.left + (anchorRect.width - widthPx) / 2) : undefined;
  const bottomPx = anchorRect ? Math.max(8, (typeof window !== 'undefined' ? (window.innerHeight - anchorRect.top + 8) : 80)) : undefined;
  const body = (
    <div className="fixed inset-0" style={{ zIndex: 60 }}>
      <div className="absolute inset-0" style={{ backgroundColor: 'rgba(0,0,0,0.35)' }} />
      <div className="absolute inset-0">
        <div
          ref={containerRef}
          className="rounded-xl"
          style={{
            position: 'fixed',
            width: widthPx,
            left: leftPx ?? '50%',
            transform: leftPx == null ? 'translateX(-50%)' : undefined,
            bottom: bottomPx ?? undefined,
            top: bottomPx ? undefined : 60,
            backgroundColor: 'var(--bg-elevated)',
            border: '1px solid var(--overlay-line-90)',
            boxShadow: '0 8px 32px rgba(0, 0, 0, 0.35)'  /* Enhanced shadow for depth */
          }}
        >
          <div className="px-4 py-3" style={{ borderBottom: '1px solid var(--overlay-line-90)' }}>
            <div className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>{title || (mode === 'ask' ? 'Ask this agent' : mode === 'edit' ? 'Edit selected fragment' : mode === 'opponent' ? 'Bring opponent to discussion' : 'Bring in an agent')}</div>
            {subtitle && <div className="text-[11px] mt-0.5" style={{ color: 'var(--text-tertiary-weak)' }}>{subtitle}</div>}
          </div>
          <div className="p-4 space-y-3">
            {(mode === 'bring_in' || mode === 'opponent') && (
              <div className="grid" style={{ gridTemplateColumns: '1fr 1fr', gap: 12 }}>
                <div className="rounded-lg" style={{ height: 360, overflowY: 'auto' }}>
                  {(presets || []).map((p) => (
                    <button key={p.id} type="button" className="w-full text-left px-3 py-2 text-[12px] flex items-center gap-2" style={{ background: hovered?.id === p.id ? 'var(--white-10)' : 'transparent', color: 'var(--text-heading)' }}
                      onMouseEnter={() => setHovered(p)}
                      onFocus={() => setHovered(p)}
                      onClick={() => onSelectPreset && onSelectPreset(p)}
                    >
                      <div className="flex flex-col">
                        <span>{p.displayName}</span>
                        <span className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>{p.provider}</span>
                      </div>
                    </button>
                  ))}
                </div>
                <div className="rounded-lg" style={{ height: 360, overflowY: 'auto', border: '1px solid var(--overlay-line-90)', padding: 12, display: 'flex', flexDirection: 'column' }}>
                  {hovered ? (
                    <div className="text-[12px]" style={{ color: 'var(--text-secondary-strong)', display: 'flex', flexDirection: 'column', gap: 8, minHeight: 0 }}>
                      <div className="font-medium" style={{ color: 'var(--text-heading)' }}>{hovered.displayName}</div>
                      <div style={{ maxHeight: '80px', overflowY: 'auto', minHeight: 0 }}>{hovered.description || 'No description provided.'}</div>
                      {hovered.expectedOutcome && (
                        <div style={{ maxHeight: '60px', overflowY: 'auto', minHeight: 0 }}><span style={{ color: 'var(--text-tertiary-weak)' }}>Expected result: </span>{hovered.expectedOutcome}</div>
                      )}
                      {Array.isArray(hovered.useCases) && hovered.useCases.length > 0 && (
                        <div style={{ flex: 1, minHeight: 0, overflowY: 'auto' }}>
                          <div style={{ color: 'var(--text-tertiary-weak)' }}>Typical use‑cases:</div>
                          <ul className="list-disc ml-4">
                            {hovered.useCases.slice(0, 6).map((u, i) => (<li key={i}>{u}</li>))}
                          </ul>
                        </div>
                      )}
                    </div>
                  ) : (
                    <div className="text-[12px]" style={{ color: 'var(--text-tertiary-weak)', display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%' }}>Hover an agent on the left to see details</div>
                  )}
                </div>
              </div>
            )}
            {(mode === 'ask' || mode === 'edit') && (
              <>
                {contextNode && (
                  <div className="text-[11px] p-2 rounded" style={{ border: '1px solid var(--overlay-line-90)', color: 'var(--text-secondary-strong)' }}>
                    {contextNode}
                  </div>
                )}
                <textarea
                  rows={5}
                  value={askText}
                  onChange={(e) => setAskText(e.target.value)}
                  placeholder={askPlaceholder || (mode === 'edit' ? 'Describe the requested change to the selected fragment' : 'Type your follow-up question')}
                  className="w-full"
                  style={{ background: 'transparent', color: 'var(--text-heading)', border: '1px solid var(--overlay-line-90)', borderRadius: 8, padding: 10 }}
                  autoFocus
                />
                <div className="flex items-center justify-end gap-2">
                  <span className="text-[11px] mr-auto" style={{ color: 'var(--text-tertiary-weak)' }}>{askText.length} chars</span>
                  <button className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }} onClick={onClose}>Cancel</button>
                  <button className="text-[11px] rounded px-3 py-1" style={{ color: 'var(--text-heading)', background: 'var(--white-10)', border: '1px solid var(--overlay-line-90)' }} onClick={() => onSubmitAsk && onSubmitAsk(askText.trim())}>
                    {submitLabel || (mode === 'edit' ? 'Apply' : 'Send')}
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  );

  return (typeof document !== 'undefined' ? (createPortal(body, document.body)) : body);
};

export default AgentPalette;


