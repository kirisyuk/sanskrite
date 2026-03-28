import React, { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { ThemeContext } from '../../../../components/ThemeProvider';
import { ProductScopeComposer, type ProductScopeSelection } from '../../../../components/ProductScopeComposer';
import { api, type HorizonProductPrefetchSuggestion } from '../../../../lib/api';
import { useProjectsStore } from '../../../../stores/projectsStore';
import {
  defaultScoutingModeForCanonicalPath,
  recommendedDepthForScoutingMode,
  specialtiesForScoutingMode,
  type ScoutingMode,
} from '../../lib/scoutingMode';

interface AssembleLensRequestProps {
  projectId: string;
  onAssemble: (request: AssembleLensRequest) => void;
  disabled?: boolean;
  pinnedBottom?: boolean;
  placeholderOverride?: string;
  hideProjectIndicator?: boolean;
  hideSuggestions?: boolean;
}

/** Lens mode determines the focus of scouting and research agents */
export type LensMode = ScoutingMode;

export interface AssembleLensRequest {
  projectId: string;
  query: string;
  name?: string;
  /** Explicit empty-lens creation path: create lens without preflight/research. */
  createEmptyLens?: boolean;
  /** Optional: UI-selected canonical paths to be merged into lens scope. */
  prefetchScope?: string[];
  filters: {
    maxRecursionDepth: number;
    /** Lens mode: general (default), supply_chain, or governance */
    mode: LensMode;
    /** Optional specialties derived from mode */
    specialties?: string[];
  };
}

type EntryMode = 'freeform' | 'product' | 'entity';

const PLACEHOLDER_VARIANTS = [
  "What are you analyzing?",
  "What are you investigating?",
  "What would you like to explore?",
  "What is your focus area?",
  "What are you looking at?",
];

const SUGGESTIONS = [
  "Tesla supply chain risks",
  "Nvidia AI chips competitors",
  "Apple smartphone suppliers",
  "BYD vs Tesla battery tech",
  "OpenAI corporate structure",
  "Microsoft cloud market share",
  "SpaceX regulatory challenges",
  "TSMC manufacturing capacity",
  "Volkswagen EV strategy",
  "Amazon logistics network",
];

const entryModeIcon = (mode: EntryMode) => {
  switch (mode) {
    case 'product':
      return (
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M16.5 9.4 7.55 4.24" />
          <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
          <polyline points="3.29 7 12 12 20.71 7" />
          <line x1="12" y1="22" x2="12" y2="12" />
        </svg>
      );
    case 'entity':
      return (
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M3 21h18" />
          <path d="M5 21V7l7-4 7 4v14" />
          <path d="M9 11h.01" />
          <path d="M15 11h.01" />
          <path d="M9 15h.01" />
          <path d="M15 15h.01" />
        </svg>
      );
    default:
      return (
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M4 7h16" />
          <path d="M7 12h10" />
          <path d="M10 17h4" />
        </svg>
      );
  }
};

const AssembleLensRequestComponent: React.FC<AssembleLensRequestProps> = ({ 
  projectId, 
  onAssemble, 
  disabled = false,
  pinnedBottom = false,
  placeholderOverride,
  hideProjectIndicator = false,
  hideSuggestions = false,
}) => {
  const { theme } = React.useContext(ThemeContext);
  const currentProject = useProjectsStore((s) => s.currentProject);
  const [inputValue, setInputValue] = useState('');
  type PrefetchEntity = { id: string; canonicalPath: string; name: string; type: string };
  const [entryMode, setEntryMode] = useState<EntryMode>('freeform');
  const [selectedEntities, setSelectedEntities] = useState<PrefetchEntity[]>([]);
  const [selectedProducts, setSelectedProducts] = useState<ProductScopeSelection[]>([]);
  const [activeProductPath, setActiveProductPath] = useState<string | null>(null);
  const [entityLoading, setEntityLoading] = useState(false);
  const [entitySuggestions, setEntitySuggestions] = useState<PrefetchEntity[]>([]);
  const [showEntityDropdown, setShowEntityDropdown] = useState(false);
  const [productLoading, setProductLoading] = useState(false);
  const [productSuggestions, setProductSuggestions] = useState<HorizonProductPrefetchSuggestion[]>([]);
  const [showProductDropdown, setShowProductDropdown] = useState(false);
  const [showModeDropdown, setShowModeDropdown] = useState(false);
  const [filters, setFilters] = useState<{
    maxRecursionDepth: number;
    mode: LensMode;
    specialties?: string[];
  }>({
    maxRecursionDepth: 1,
    mode: 'general',
  });
  const [modeTouched, setModeTouched] = useState(false);
  const [showSettings, setShowSettings] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const settingsDialogRef = useRef<HTMLDivElement>(null);
  const settingsCloseBtnRef = useRef<HTMLButtonElement>(null);
  const lastActiveElRef = useRef<HTMLElement | null>(null);
  const settingsTitleId = React.useId();
  const settingsDescId = React.useId();
  const entityAbortRef = useRef<AbortController | null>(null);
  const productAbortRef = useRef<AbortController | null>(null);
  const entityDebounceRef = useRef<number | null>(null);
  const productDebounceRef = useRef<number | null>(null);
  const [placeholder] = useState(() => 
    PLACEHOLDER_VARIANTS[Math.floor(Math.random() * PLACEHOLDER_VARIANTS.length)]
  );
  const [suggestions] = useState(() =>
    [...SUGGESTIONS].sort(() => 0.5 - Math.random()).slice(0, 2)
  );

  // Focus input on mount
  useEffect(() => {
    if (inputRef.current) {
      inputRef.current.focus();
    }
  }, []);

  // Settings dialog: a11y + focus trap + ESC + scroll lock
  useEffect(() => {
    if (!showSettings) return;

    lastActiveElRef.current = (document.activeElement as HTMLElement) || null;
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    const getFocusables = (root: HTMLElement | null) => {
      if (!root) return [] as HTMLElement[];
      const nodes = root.querySelectorAll<HTMLElement>(
        'a[href],button:not([disabled]),textarea:not([disabled]),input:not([disabled]):not([type="hidden"]),select:not([disabled]),[tabindex]:not([tabindex="-1"])'
      );
      return Array.from(nodes).filter((el) => {
        const style = window.getComputedStyle(el);
        return style.display !== 'none' && style.visibility !== 'hidden';
      });
    };

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        setShowSettings(false);
        return;
      }
      if (e.key !== 'Tab') return;

      const focusables = getFocusables(settingsDialogRef.current);
      if (focusables.length === 0) return;

      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      const active = document.activeElement as HTMLElement | null;

      if (e.shiftKey) {
        if (!active || active === first || !settingsDialogRef.current?.contains(active)) {
          e.preventDefault();
          last.focus();
        }
      } else {
        if (!active || !settingsDialogRef.current?.contains(active)) {
          e.preventDefault();
          first.focus();
        } else if (active === last) {
          e.preventDefault();
          first.focus();
        }
      }
    };

    document.addEventListener('keydown', onKeyDown);
    window.setTimeout(() => settingsCloseBtnRef.current?.focus(), 0);

    return () => {
      document.removeEventListener('keydown', onKeyDown);
      document.body.style.overflow = prevOverflow;
      window.setTimeout(() => lastActiveElRef.current?.focus?.(), 0);
    };
  }, [showSettings]);

  useEffect(() => {
    if (modeTouched) return;

    const selectedPaths = selectedEntities
      .map((entity) => String(entity.canonicalPath || '').trim())
      .filter(Boolean);

    const suggestedMode =
      selectedPaths.length > 0 && selectedPaths.every((path) => defaultScoutingModeForCanonicalPath(path) === 'governance')
        ? 'governance'
        : 'general';

    setFilters((prev) => {
      const nextDepth = recommendedDepthForScoutingMode(suggestedMode);
      const nextSpecialties = specialtiesForScoutingMode(suggestedMode);
      const prevSpecialties = prev.specialties || [];
      const sameSpecialties =
        prevSpecialties.length === nextSpecialties.length &&
        prevSpecialties.every((value, index) => value === nextSpecialties[index]);
      if (prev.mode === suggestedMode && prev.maxRecursionDepth === nextDepth && sameSpecialties) {
        return prev;
      }
      return {
        ...prev,
        mode: suggestedMode,
        specialties: nextSpecialties,
        maxRecursionDepth: nextDepth,
      };
    });
  }, [modeTouched, selectedEntities]);

  useEffect(() => {
    if (selectedProducts.length === 0) {
      if (activeProductPath !== null) setActiveProductPath(null);
      return;
    }
    if (!activeProductPath || !selectedProducts.some((item) => item.canonicalPath === activeProductPath)) {
      setActiveProductPath(selectedProducts[selectedProducts.length - 1].canonicalPath);
    }
  }, [activeProductPath, selectedProducts]);

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setInputValue(e.target.value);
  };

  // Prefetch entity suggestions (existing objects) for chip selection.
  useEffect(() => {
    if (entryMode !== 'freeform') {
      setEntitySuggestions([]);
      setEntityLoading(false);
      setShowEntityDropdown(false);
      try { entityAbortRef.current?.abort(); } catch {}
      return;
    }
    // Keep UX natural:
    // - chips live "inside" the query input
    // - we only show entity suggestions when the user is typing a single token (no spaces)
    //   so we don't spam suggestions while writing a long sentence.
    const raw = inputValue.trim();
    const token = raw.startsWith('@') ? raw.slice(1) : raw;
    const canSuggest = token.length >= 2 && !/\s/.test(raw);
    if (!canSuggest) {
      setEntitySuggestions([]);
      setEntityLoading(false);
      setShowEntityDropdown(false);
      try { entityAbortRef.current?.abort(); } catch {}
      return;
    }

    if (entityDebounceRef.current) {
      window.clearTimeout(entityDebounceRef.current);
    }

    setEntityLoading(true);
    entityDebounceRef.current = window.setTimeout(async () => {
      try {
        try { entityAbortRef.current?.abort(); } catch {}
        const ac = new AbortController();
        entityAbortRef.current = ac;
        const res = await api.searchHorizonEntities(token, 10, ac.signal);
        const selected = new Set(selectedEntities.map((e) => e.canonicalPath));
        const list: PrefetchEntity[] = (res?.entities || [])
          .map((e: any) => ({
            id: String(e.id || '').trim(),
            canonicalPath: String(e.canonicalPath || '').trim(),
            name: String(e.name || '').trim(),
            type: String(e.type || '').trim(),
          }))
          .filter((e) => e.canonicalPath && e.name && !selected.has(e.canonicalPath));
        setEntitySuggestions(list);
        setShowEntityDropdown(true);
      } catch (err: any) {
        // Ignore aborts
        if (err?.name === 'AbortError' || err?.code === 'ERR_CANCELED') return;
        setEntitySuggestions([]);
        setShowEntityDropdown(false);
      } finally {
        setEntityLoading(false);
      }
    }, 180) as unknown as number;

    return () => {
      if (entityDebounceRef.current) window.clearTimeout(entityDebounceRef.current);
    };
  }, [inputValue, selectedEntities]);

  useEffect(() => {
    if (entryMode !== 'product') {
      setProductSuggestions([]);
      setProductLoading(false);
      setShowProductDropdown(false);
      try { productAbortRef.current?.abort(); } catch {}
      return;
    }

    const raw = inputValue.trim();
    if (raw.length < 2) {
      setProductSuggestions([]);
      setProductLoading(false);
      setShowProductDropdown(false);
      try { productAbortRef.current?.abort(); } catch {}
      return;
    }

    if (productDebounceRef.current) {
      window.clearTimeout(productDebounceRef.current);
    }

    setProductLoading(true);
    productDebounceRef.current = window.setTimeout(async () => {
      try {
        try { productAbortRef.current?.abort(); } catch {}
        const ac = new AbortController();
        productAbortRef.current = ac;
        const res = await api.prefetchHorizonProducts({ query: raw, limit: 8 }, ac.signal);
        const selected = new Set(selectedProducts.map((e) => e.canonicalPath));
        const list = (res?.suggestions || []).filter((item) => !selected.has(String(item.canonicalPath || '').trim()));
        setProductSuggestions(list);
        setShowProductDropdown(list.length > 0);
      } catch (err: any) {
        if (err?.name === 'AbortError' || err?.code === 'ERR_CANCELED') return;
        setProductSuggestions([]);
        setShowProductDropdown(false);
      } finally {
        setProductLoading(false);
      }
    }, 160) as unknown as number;

    return () => {
      if (productDebounceRef.current) window.clearTimeout(productDebounceRef.current);
    };
  }, [entryMode, inputValue, selectedProducts]);

  // Close dropdown on outside click.
  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      const el = rootRef.current;
      if (!el) return;
      if (el.contains(e.target as any)) return;
      setShowEntityDropdown(false);
      setShowProductDropdown(false);
      setShowModeDropdown(false);
    };
    window.addEventListener('mousedown', onDown);
    return () => window.removeEventListener('mousedown', onDown);
  }, []);

  const addEntity = (ent: PrefetchEntity) => {
    const cp = String(ent.canonicalPath || '').trim();
    if (!cp) return;
    setSelectedEntities((prev) => {
      if (prev.some((x) => x.canonicalPath === cp)) return prev;
      return [...prev, ent];
    });
    // Remove the current token (user was likely typing an entity name).
    setInputValue('');
    setShowEntityDropdown(false);
    // Keep focus in the query input for fast workflow.
    window.setTimeout(() => inputRef.current?.focus(), 0);
  };

  const removeEntity = (canonicalPath: string) => {
    const cp = String(canonicalPath || '').trim();
    if (!cp) return;
    setSelectedEntities((prev) => prev.filter((x) => x.canonicalPath !== cp));
  };

  const addProduct = (suggestion: HorizonProductPrefetchSuggestion) => {
    const canonicalPath = String(suggestion.canonicalPath || '').trim();
    if (!canonicalPath) return;
    setSelectedProducts((prev) => {
      if (prev.some((item) => item.canonicalPath === canonicalPath)) return prev;
      return [
        ...prev,
        {
          canonicalPath,
          label: suggestion.label || suggestion.matchedLabel || canonicalPath.split('/').filter(Boolean).slice(-1)[0] || canonicalPath,
          sourceScope: suggestion.sourceScope,
        },
      ];
    });
    setActiveProductPath(canonicalPath);
    setInputValue('');
    setShowProductDropdown(false);
    window.setTimeout(() => inputRef.current?.focus(), 0);
  };

  const removeProduct = (canonicalPath: string) => {
    const cp = String(canonicalPath || '').trim();
    if (!cp) return;
    setSelectedProducts((prev) => prev.filter((item) => item.canonicalPath !== cp));
  };

  const handleAssemble = () => {
    const q = entryMode === 'freeform' ? inputValue.trim() : '';
    const prefetchScope =
      entryMode === 'product'
        ? selectedProducts.map((e) => e.canonicalPath).filter(Boolean)
        : selectedEntities.map((e) => e.canonicalPath).filter(Boolean);
    if (!q && prefetchScope.length === 0) return;

    const buildPrefetchLensName = () => {
      if (q) return '';
      const names = (entryMode === 'product' ? selectedProducts.map((e) => e.label) : selectedEntities.map((e) => String(e?.name || '').trim()))
        .filter(Boolean);
      if (names.length === 0) return '';
      const first = names[0];
      const second = names.length > 1 ? names[1] : '';
      const suffix = names.length > 2 ? ` (+${names.length - 2})` : '';
      return second ? `${first} · ${second}${suffix}` : `${first}${suffix}`;
    };

    const request: AssembleLensRequest = {
      projectId,
      query: q,
      name: buildPrefetchLensName() || undefined,
      prefetchScope,
      filters,
    };

    onAssemble(request);
  };

  const clampInt = (v: number, min: number, max: number) => {
    const n = Number.isFinite(v) ? Math.trunc(v) : min;
    return Math.max(min, Math.min(max, n));
  };

  const setMode = (mode: LensMode) => {
    const specialties = specialtiesForScoutingMode(mode);
    setFilters((prev) => {
      if (prev.mode === mode) return prev;
      return {
        ...prev,
        mode,
        specialties,
        maxRecursionDepth: clampInt(recommendedDepthForScoutingMode(mode), 0, 5),
      };
    });
    setModeTouched(true);
  };

  const setMaxDepth = (v: number) => {
    setFilters((prev) => ({ ...prev, maxRecursionDepth: clampInt(v, 0, 5) }));
  };

  const containerStyle: React.CSSProperties = {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: pinnedBottom ? 'flex-end' : 'center',
    // When pinned at the bottom, this block must NOT take full height,
    // otherwise it will push the realtime feed outside the viewport.
    height: pinnedBottom ? 'auto' : '100%',
    flexShrink: pinnedBottom ? 0 : undefined,
    width: '100%',
    padding: pinnedBottom ? '0 24px 24px 24px' : '2rem',
  };

  const inputContainerStyle: React.CSSProperties = {
    width: '100%',
    maxWidth: '960px',
    position: 'relative',
  };

  const containerStyleForInput: React.CSSProperties = {
    width: '100%',
    maxWidth: '960px',
    borderRadius: '16px',
    overflow: 'visible',
    transition: 'all 0.3s ease',
    background: theme === 'dark' ? 'var(--bg-elevated)' : 'var(--bg-elevated)',
    border: '1px solid var(--border-subtle)',
    boxShadow: pinnedBottom 
      ? (theme === 'dark' ? '0 -4px 20px rgba(0,0,0,0.2)' : '0 -4px 20px rgba(0,0,0,0.05)')
      : (theme === 'dark' ? '0 20px 60px rgba(0,0,0,0.4)' : '0 20px 60px rgba(0,0,0,0.15)'),
    display: 'flex',
    flexDirection: 'column',
    position: 'relative',
  };

  const inputRowStyle: React.CSSProperties = {
    width: '100%',
    display: 'flex',
    alignItems: 'center',
  };

  const tokenFieldStyle: React.CSSProperties = {
    flex: 1,
    minWidth: 0,
    display: 'flex',
    flexWrap: 'wrap',
    alignItems: 'center',
    gap: 8,
    padding: '10px 10px 10px 12px',
    cursor: 'text',
  };
  const chipStyle: React.CSSProperties = {
    display: 'inline-flex',
    alignItems: 'center',
    gap: 8,
    padding: '6px 10px',
    borderRadius: 999,
    border: '1px solid var(--border-subtle)',
    background: theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.04)',
    color: 'var(--text-heading)',
    cursor: 'pointer',
    maxWidth: '100%',
  };
  const queryInputStyle: React.CSSProperties = {
    flex: 1,
    minWidth: 180,
    padding: '10px 10px',
    fontSize: 16,
    fontWeight: 500,
    backgroundColor: 'transparent',
    border: 'none',
    outline: 'none',
    color: 'var(--text-heading)',
  };

  const activeSelections = entryMode === 'product' ? selectedProducts : selectedEntities;
  const hasSelectedObjects = selectedProducts.length > 0 || selectedEntities.length > 0;
  const sendBtnDisabled = disabled || (entryMode === 'freeform'
    ? (!inputValue.trim() && selectedEntities.length === 0)
    : selectedProducts.length === 0);
  const modeLabel =
    entryMode === 'product' ? 'By Product' : entryMode === 'entity' ? 'By Entity' : 'Research';

  return (
    <div style={containerStyle}>
      {/* Project indicator */}
      {!hideProjectIndicator && currentProject && currentProject.id === projectId && (
        <div
          style={{
            marginBottom: 12,
            fontSize: 13,
            color: 'var(--text-secondary-strong)',
            textAlign: 'center',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            gap: 6,
          }}
        >
          <span style={{ color: 'var(--text-tertiary)' }}>You're in</span>
          <span style={{ fontWeight: 500, color: 'var(--text-heading)' }}>
            {currentProject.displayName || 'Untitled'}
          </span>
          <span style={{ color: 'var(--text-tertiary)' }}>project</span>
        </div>
      )}
      <div style={inputContainerStyle} ref={rootRef}>
        <div style={containerStyleForInput}>
          <div style={inputRowStyle}>
            {/* Left Icon */}
            <div style={{ paddingLeft: 16, display: 'flex', alignItems: 'center', color: 'var(--text-secondary-strong)' }}>
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="m12 3-1.912 5.813a2 2 0 0 1-1.275 1.275L3 12l5.813 1.912a2 2 0 0 1 1.275 1.275L12 21l1.912-5.813a2 2 0 0 1 1.275-1.275L21 12l-5.813-1.912a2 2 0 0 1-1.275-1.275L12 3Z"/>
              </svg>
            </div>

            {/* Tokenized input: chips + free text in one "natural" field */}
            <div
              style={tokenFieldStyle}
              onMouseDown={(e) => {
                e.preventDefault();
                inputRef.current?.focus();
              }}
            >
              {entryMode === 'product'
                ? selectedProducts.map((p) => (
                    <button
                      key={p.canonicalPath}
                      type="button"
                      onClick={() => setActiveProductPath(p.canonicalPath)}
                      title={p.label}
                      style={{
                        ...chipStyle,
                        border: activeProductPath === p.canonicalPath ? '1px solid var(--accent-primary)' : chipStyle.border,
                      }}
                    >
                      <span style={{ fontSize: 12, fontWeight: 750, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {p.label}
                      </span>
                      <span
                        onClick={(event) => {
                          event.stopPropagation();
                          removeProduct(p.canonicalPath);
                        }}
                        style={{ fontSize: 11, color: 'var(--text-tertiary)', flexShrink: 0 }}
                      >
                        ×
                      </span>
                    </button>
                  ))
                : selectedEntities.map((e) => (
                    <button
                      key={e.canonicalPath}
                      type="button"
                      onClick={() => removeEntity(e.canonicalPath)}
                      title={`Remove ${e.name}`}
                      style={chipStyle}
                    >
                      <span style={{ fontSize: 12, fontWeight: 750, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {e.name}
                      </span>
                      <span style={{ fontSize: 11, color: 'var(--text-tertiary)', flexShrink: 0 }}>
                        ×
                      </span>
                    </button>
                  ))}

              <input
                ref={inputRef}
                type="text"
                value={inputValue}
                onChange={handleInputChange}
                placeholder={
                  activeSelections.length === 0
                    ? (
                        entryMode === 'product'
                          ? 'Search products by canonical name or synonym'
                          : (String(placeholderOverride || '').trim() || placeholder)
                      )
                    : ''
                }
                style={queryInputStyle}
                disabled={disabled}
                onFocus={() => {
                  if (entryMode === 'product') {
                    if (productSuggestions.length > 0) setShowProductDropdown(true);
                  } else if (entitySuggestions.length > 0) {
                    setShowEntityDropdown(true);
                  }
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Escape') {
                    setShowModeDropdown(false);
                    setShowEntityDropdown(false);
                    setShowProductDropdown(false);
                    return;
                  }
                  if (e.key === 'Enter' && !disabled) {
                    e.preventDefault();
                    if (entryMode === 'product' && productSuggestions.length > 0) {
                      addProduct(productSuggestions[0]);
                      return;
                    }
                    handleAssemble();
                  }
                }}
              />

              {(entryMode === 'product' ? productLoading : entityLoading) && (
                <span style={{ fontSize: 11, color: 'var(--text-tertiary)' }}>
                  Searching…
                </span>
              )}
            </div>

            {/* Right Actions */}
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, paddingRight: 12 }}>
              <button
                type="button"
                aria-label="Settings"
                onClick={() => setShowSettings(true)}
                style={{
                  width: 32,
                  height: 32,
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  borderRadius: 8,
                  background: 'transparent',
                  color: 'var(--text-secondary)',
                  border: 'none',
                  cursor: 'pointer',
                  transition: 'background 0.2s',
                }}
                onMouseEnter={(e) => (e.currentTarget.style.background = 'var(--white-10)')}
                onMouseLeave={(e) => (e.currentTarget.style.background = 'transparent')}
              >
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <circle cx="12" cy="12" r="3" />
                  <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1Z" />
                </svg>
              </button>

              <button
                type="button"
                aria-label="Start"
                onClick={handleAssemble}
                disabled={sendBtnDisabled}
                style={{
                  width: 32,
                  height: 32,
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  borderRadius: 8,
                  background: sendBtnDisabled ? 'transparent' : 'var(--accent-primary)',
                  color: sendBtnDisabled ? 'var(--text-disabled)' : '#ffffff',
                  border: 'none',
                  cursor: sendBtnDisabled ? 'not-allowed' : 'pointer',
                  transition: 'all 0.2s',
                  opacity: sendBtnDisabled ? 0.5 : 1,
                }}
              >
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M5 12h14" />
                  <path d="m12 5 7 7-7 7" />
                </svg>
              </button>
            </div>
          </div>

          <div
            style={{
              width: '100%',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'flex-start',
              padding: '10px 14px 12px',
              borderTop: '1px solid var(--border-subtle)',
            }}
          >
            <div style={{ position: 'relative' }}>
              <button
                type="button"
                onClick={() => setShowModeDropdown((prev) => !prev)}
                style={{
                  display: 'inline-flex',
                  alignItems: 'center',
                  gap: 8,
                  borderRadius: 999,
                  border: 'none',
                  background: 'transparent',
                  color: 'var(--text-secondary-strong)',
                  padding: '4px 8px',
                  fontSize: 12,
                  fontWeight: 700,
                  cursor: 'pointer',
                }}
              >
                <span style={{ display: 'inline-flex' }}>{entryModeIcon(entryMode)}</span>
                <span>{modeLabel}</span>
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                  <path d="m6 9 6 6 6-6" />
                </svg>
              </button>

              {showModeDropdown && (
                <div
                  style={{
                  position: 'absolute',
                  top: '100%',
                  left: 0,
                  marginTop: 10,
                  minWidth: 196,
                  borderRadius: 14,
                    border: '1px solid var(--border-subtle)',
                    background: 'var(--bg-elevated)',
                    boxShadow: theme === 'dark' ? '0 22px 60px rgba(0,0,0,0.45)' : '0 22px 60px rgba(0,0,0,0.14)',
                    overflow: 'hidden',
                    zIndex: 90,
                  }}
                >
                  {([
                    { key: 'freeform', label: 'Research', disabled: false },
                    { key: 'product', label: 'By Product', disabled: false },
                    { key: 'entity', label: 'By Entity', disabled: true },
                  ] as { key: EntryMode; label: string; disabled: boolean }[]).map((option) => {
                    const active = entryMode === option.key;
                    return (
                      <button
                        key={option.key}
                        type="button"
                        disabled={option.disabled}
                        onClick={() => {
                          if (option.disabled) return;
                          setEntryMode(option.key);
                          setInputValue('');
                          setShowModeDropdown(false);
                          setShowEntityDropdown(false);
                          setShowProductDropdown(false);
                          window.setTimeout(() => inputRef.current?.focus(), 0);
                        }}
                        style={{
                          width: '100%',
                          textAlign: 'left',
                          padding: '10px 12px',
                          border: 'none',
                          background: active ? 'var(--white-06)' : 'transparent',
                          color: option.disabled ? 'var(--text-disabled)' : 'var(--text-heading)',
                          cursor: option.disabled ? 'not-allowed' : 'pointer',
                          fontSize: 12,
                          fontWeight: active ? 800 : 650,
                          opacity: option.disabled ? 0.56 : 1,
                          display: 'flex',
                          alignItems: 'center',
                          gap: 8,
                        }}
                      >
                        <span style={{ display: 'inline-flex', color: option.disabled ? 'var(--text-disabled)' : 'var(--text-secondary-strong)' }}>
                          {entryModeIcon(option.key)}
                        </span>
                        <span>{option.label}</span>
                      </button>
                    );
                  })}
                </div>
              )}
            </div>
          </div>
        </div>

        {/* Entity dropdown (prefetch) */}
        {entryMode === 'freeform' && showEntityDropdown && entitySuggestions.length > 0 && (
          <div
            style={{
              position: 'absolute',
              left: 0,
              right: 0,
              top: '100%',
              marginTop: 10,
              borderRadius: 14,
              border: '1px solid var(--border-subtle)',
              background: 'var(--bg-elevated)',
              boxShadow: theme === 'dark' ? '0 22px 60px rgba(0,0,0,0.45)' : '0 22px 60px rgba(0,0,0,0.14)',
              overflow: 'hidden',
              zIndex: 90,
            }}
          >
            {entitySuggestions.slice(0, 10).map((e) => (
              <button
                key={e.canonicalPath}
                type="button"
                onClick={() => addEntity(e)}
                style={{
                  width: '100%',
                  textAlign: 'left',
                  padding: '10px 12px',
                  border: 'none',
                  background: 'transparent',
                  cursor: 'pointer',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: 12,
                }}
                onMouseEnter={(ev) => (ev.currentTarget.style.background = 'var(--white-06)')}
                onMouseLeave={(ev) => (ev.currentTarget.style.background = 'transparent')}
              >
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: 12, fontWeight: 750, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {e.name}
                  </div>
                  <div style={{ marginTop: 2, fontSize: 11, color: 'var(--text-tertiary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {e.canonicalPath}
                  </div>
                </div>
                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', flexShrink: 0 }}>
                  {e.type}
                </div>
              </button>
            ))}
          </div>
        )}
        {entryMode === 'product' && showProductDropdown && productSuggestions.length > 0 && (
          <div
            style={{
              position: 'absolute',
              left: 0,
              right: 0,
              top: '100%',
              marginTop: 56,
              borderRadius: 14,
              border: '1px solid var(--border-subtle)',
              background: 'var(--bg-elevated)',
              boxShadow: theme === 'dark' ? '0 22px 60px rgba(0,0,0,0.45)' : '0 22px 60px rgba(0,0,0,0.14)',
              overflow: 'hidden',
              zIndex: 80,
            }}
          >
            {productSuggestions.slice(0, 8).map((item) => (
              <button
                key={item.canonicalPath}
                type="button"
                onClick={() => addProduct(item)}
                style={{
                  width: '100%',
                  textAlign: 'left',
                  padding: '10px 12px',
                  border: 'none',
                  background: 'transparent',
                  cursor: 'pointer',
                  display: 'flex',
                  alignItems: 'flex-start',
                  justifyContent: 'space-between',
                  gap: 12,
                }}
                onMouseEnter={(ev) => (ev.currentTarget.style.background = 'var(--white-06)')}
                onMouseLeave={(ev) => (ev.currentTarget.style.background = 'transparent')}
              >
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: 12, fontWeight: 750, color: 'var(--text-heading)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {item.label}
                  </div>
                  <div style={{ marginTop: 2, fontSize: 11, color: 'var(--text-tertiary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {item.canonicalPath}
                  </div>
                </div>
                <div style={{ fontSize: 11, color: 'var(--text-secondary-strong)', flexShrink: 0 }}>
                  {item.sourceScope || 'global'}
                </div>
              </button>
            ))}
          </div>
        )}
      </div>

      {entryMode === 'product' && selectedProducts.length > 0 && (
        <div
          style={{
            width: '100%',
            maxWidth: '1280px',
            marginTop: 24,
            minHeight: 440,
            flex: pinnedBottom ? 1 : undefined,
          }}
        >
          <ProductScopeComposer
            selectedProducts={selectedProducts}
            activePath={activeProductPath}
            onSelectProduct={setActiveProductPath}
            maxHops={3}
            maxPerHop={7}
            minHeight={440}
          />
        </div>
      )}

      {!pinnedBottom && entryMode === 'product' && selectedProducts.length === 0 && (
        <div style={{ marginTop: 18, fontSize: 13, color: 'var(--text-tertiary)', textAlign: 'center', maxWidth: 520, lineHeight: 1.5 }}>
          Pick a product to explore its chain.
        </div>
      )}

      {/* Suggestions (only when centered) */}
      {!pinnedBottom && !hideSuggestions && entryMode === 'freeform' && !hasSelectedObjects && (
        <div style={{ 
          marginTop: 16, 
          fontSize: 13, 
          color: 'var(--text-tertiary)', 
          lineHeight: 1.6,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          flexWrap: 'nowrap',
          whiteSpace: 'nowrap',
          overflowX: 'auto',
          maxWidth: '100%',
        }}>
          <span style={{ marginRight: 6 }}>Try:</span>
          {suggestions.map((s, i) => (
            <React.Fragment key={s}>
              <span 
                style={{ 
                  cursor: 'pointer', 
                  color: 'var(--text-secondary)',
                  borderBottom: '1px dashed var(--border-subtle, rgba(255,255,255,0.2))',
                  transition: 'color 0.2s',
                }}
                onClick={() => {
                  setInputValue(s);
                  if (inputRef.current) inputRef.current.focus();
                }}
                onMouseEnter={(e) => e.currentTarget.style.color = 'var(--text-heading)'}
                onMouseLeave={(e) => e.currentTarget.style.color = 'var(--text-secondary)'}
              >
                {s}
              </span>
              {i < suggestions.length - 1 && <span style={{ margin: '0 8px', color: 'var(--text-disabled, rgba(255,255,255,0.1))' }}>|</span>}
            </React.Fragment>
          ))}
        </div>
      )}

      {/* Settings Modal */}
      {showSettings && createPortal(
        <div
          className="fixed inset-0 z-[80] flex items-center justify-center"
          role="dialog"
          aria-modal="true"
          aria-labelledby={settingsTitleId}
          aria-describedby={settingsDescId}
          onClick={(e) => {
            if (e.target === e.currentTarget) setShowSettings(false);
          }}
          style={{
            background: 'rgba(0,0,0,0.35)',
            backdropFilter: 'blur(6px)',
            padding: 16,
          }}
        >
          <div
            ref={settingsDialogRef}
            className="w-full max-w-lg rounded-2xl border"
            style={{
              backgroundColor: 'var(--bg-elevated)',
              borderColor: 'var(--overlay-line-90)',
              boxShadow: '0 18px 60px rgba(0, 0, 0, 0.40)',
              overflow: 'hidden',
            }}
            onClick={(e) => e.stopPropagation()}
          >
            <div
              className="px-5 py-4 flex items-start justify-between gap-4"
              style={{ borderBottom: '1px solid var(--overlay-line-90)' }}
            >
              <div style={{ minWidth: 0 }}>
                <div
                  id={settingsTitleId}
                  className="text-[15px] font-semibold"
                  style={{ color: 'var(--text-heading)', letterSpacing: '-0.01em' }}
                >
                  Settings
                </div>
                <div
                  id={settingsDescId}
                  className="text-[12px] mt-0.5"
                  style={{ color: 'var(--text-secondary-strong)', lineHeight: 1.4 }}
                >
                  Choose the lens and control how deep recursive expansion goes. Press Esc to close.
                </div>
              </div>
              <button
                ref={settingsCloseBtnRef}
                type="button"
                onClick={() => setShowSettings(false)}
                aria-label="Close settings"
                className="rounded-lg p-2 transition-colors"
                style={{
                  color: 'var(--text-secondary-strong)',
                  backgroundColor: 'transparent',
                  border: '1px solid var(--border-subtle)',
                  flexShrink: 0,
                }}
              >
                <svg viewBox="0 0 24 24" className="w-5 h-5" fill="none" stroke="currentColor">
                  <path d="M6 6l12 12M18 6L6 18" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                </svg>
              </button>
            </div>

            <div className="px-5 py-4 space-y-4">
              {/* Analysis mode */}
              <fieldset className="rounded-xl p-3" style={{ backgroundColor: 'transparent' }}>
                <legend className="text-[12px] px-1" style={{ color: 'var(--text-secondary-strong)' }}>
                  Analysis mode
                </legend>
                <div className="mt-2 grid grid-cols-3 gap-2" role="radiogroup" aria-label="Analysis mode">
                  {([
                    { value: 'general' as const, label: 'General', desc: 'Broad exploration' },
                    { value: 'supply_chain' as const, label: 'Supply chain', desc: 'Material & product flows' },
                    { value: 'governance' as const, label: 'Governance', desc: 'Ownership, control, leadership' },
                  ]).map((opt) => {
                    const active = filters.mode === opt.value;
                    return (
                      <label
                        key={opt.value}
                        className="rounded-xl border px-3 py-2 cursor-pointer select-none"
                        style={{
                          borderColor: active ? 'var(--overlay-line-80)' : 'var(--border-subtle)',
                          background: active ? 'var(--white-10)' : 'transparent',
                        }}
                      >
                        <input
                          type="radio"
                          name="analysis-mode"
                          className="sr-only"
                          checked={active}
                          onChange={() => setMode(opt.value)}
                        />
                        <div className="text-[12px] font-semibold" style={{ color: 'var(--text-heading)' }}>
                          {opt.label}
                        </div>
                        <div className="text-[11px] mt-0.5" style={{ color: 'var(--text-tertiary-weak)', lineHeight: 1.35 }}>
                          {opt.desc}
                        </div>
                      </label>
                    );
                  })}
                </div>
              </fieldset>

              {/* Recursion depth */}
              <div className="rounded-xl p-3" style={{ backgroundColor: 'var(--white-06)' }}>
                <div className="flex items-start justify-between gap-3">
                  <div style={{ minWidth: 0 }}>
                    <div className="text-[12px] font-semibold" style={{ color: 'var(--text-heading)' }}>
                      Recursion depth
                    </div>
                    <div className="text-[11px] mt-0.5" style={{ color: 'var(--text-secondary-strong)', lineHeight: 1.35 }}>
                      Controls how far we expand linked fragments. Range: 0–5.
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <button
                      type="button"
                      aria-label="Decrease recursion depth"
                      onClick={() => setMaxDepth(filters.maxRecursionDepth - 1)}
                      disabled={filters.maxRecursionDepth <= 0}
                      className="rounded-lg px-2 py-1 border text-[12px]"
                      style={{
                        borderColor: 'var(--border-subtle)',
                        color: 'var(--text-heading)',
                        background: 'transparent',
                        opacity: filters.maxRecursionDepth <= 0 ? 0.45 : 1,
                        cursor: filters.maxRecursionDepth <= 0 ? 'not-allowed' : 'pointer',
                      }}
                    >
                      −
                    </button>
                    <input
                      type="number"
                      inputMode="numeric"
                      min={0}
                      max={5}
                      step={1}
                      value={filters.maxRecursionDepth}
                      onChange={(e) => setMaxDepth(Number(e.target.value))}
                      className="text-[12px] rounded-lg border px-2 py-1 w-[64px]"
                      style={{
                        backgroundColor: 'var(--bg-input)',
                        color: 'var(--text-heading)',
                        borderColor: 'var(--border-input, rgba(255, 255, 255, 0.1))',
                        outline: 'none',
                      }}
                    />
                    <button
                      type="button"
                      aria-label="Increase recursion depth"
                      onClick={() => setMaxDepth(filters.maxRecursionDepth + 1)}
                      disabled={filters.maxRecursionDepth >= 5}
                      className="rounded-lg px-2 py-1 border text-[12px]"
                      style={{
                        borderColor: 'var(--border-subtle)',
                        color: 'var(--text-heading)',
                        background: 'transparent',
                        opacity: filters.maxRecursionDepth >= 5 ? 0.45 : 1,
                        cursor: filters.maxRecursionDepth >= 5 ? 'not-allowed' : 'pointer',
                      }}
                    >
                      +
                    </button>
                  </div>
                </div>

                <div className="mt-3">
                  <input
                    type="range"
                    min={0}
                    max={5}
                    step={1}
                    value={filters.maxRecursionDepth}
                    onChange={(e) => setMaxDepth(Number(e.target.value))}
                    aria-label="Recursion depth"
                    className="w-full"
                    style={{
                      accentColor: 'var(--text-heading)',
                    }}
                  />
                  <div className="mt-1 flex justify-between text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                    {Array.from({ length: 6 }).map((_, i) => (
                      <span key={i} style={{ width: '16px', textAlign: 'center' }}>{i}</span>
                    ))}
                  </div>
                </div>
              </div>
            </div>

            <div
              className="px-5 py-3 flex items-center justify-end gap-2"
              style={{ borderTop: '1px solid var(--overlay-line-90)' }}
            >
              <button
                type="button"
                className="px-3 py-2 rounded-lg border text-[12px] transition-colors"
                onClick={() => setShowSettings(false)}
                style={{
                  background: 'transparent',
                  color: 'var(--text-heading)',
                  borderColor: 'var(--border-subtle)',
                }}
              >
                Done
              </button>
            </div>
          </div>
        </div>,
        document.body
      )}
    </div>
  );
};

export default AssembleLensRequestComponent;
