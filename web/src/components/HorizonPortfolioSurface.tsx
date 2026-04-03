import React from 'react';
import { api } from '../lib/api';
import type {
  HorizonMonitoringDigest,
  HorizonWatchPortfolio,
  HorizonWatchPortfolioEntryView,
  HorizonWatchPortfolioView,
} from '../types/horizonWatch';

type HorizonPortfolioSurfaceProps = {
  projectName?: string | null;
  onOpenWorldMap?: () => void;
  onOpenWatch?: () => void;
};

const palette = {
  pageBg: 'var(--bg-content)',
  panelBg: 'var(--bg-elevated)',
  panelAltBg: 'var(--bg-sidebar)',
  panelBorder: 'var(--overlay-line-90)',
  borderSubtle: 'var(--border-subtle)',
  text: 'var(--text-heading)',
  textMuted: 'var(--text-secondary-strong)',
  textTertiary: 'var(--text-secondary)',
  chipBg: 'var(--white-06)',
  hoverBg: 'var(--white-03)',
  activeBg: 'var(--white-04)',
  brandBg: 'var(--brand-10)',
  brandBorder: 'var(--brand-20)',
  brandText: 'var(--blue-200)',
  dangerBg: 'var(--danger-10)',
  dangerBorder: 'var(--danger-20)',
  dangerText: 'var(--danger-300)',
  successBg: 'var(--success-20)',
  successBorder: 'var(--success-20)',
  successText: 'var(--accent-success)',
  warningBg: 'var(--warning-10)',
  warningBorder: 'var(--warning-20)',
  warningText: 'var(--warning-300)',
  accentText: 'var(--accent-primary)',
};

const panelStyle: React.CSSProperties = {
  borderRadius: 24,
  border: `1px solid ${palette.panelBorder}`,
  background: palette.panelBg,
  boxShadow: '0 24px 80px rgba(0,0,0,0.18)',
  backdropFilter: 'blur(12px)',
};

function formatDate(value?: string): string {
  if (!value) return 'n/a';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return 'n/a';
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date);
}

function formatPct(value?: number): string {
  return `${Math.round((value || 0) * 100)}%`;
}

function numericPositionValue(
  entry: HorizonWatchPortfolioEntryView | null | undefined,
  key: string,
): number {
  const raw = entry?.monitoringSnapshot?.currentPosition?.[key];
  return typeof raw === 'number' ? raw : 0;
}

function threatCount(
  entry: HorizonWatchPortfolioEntryView | null | undefined,
): number {
  return entry?.monitoringSnapshot?.threats?.length || 0;
}

function tailwindCount(
  entry: HorizonWatchPortfolioEntryView | null | undefined,
): number {
  return entry?.monitoringSnapshot?.tailwinds?.length || 0;
}

function postureLabel(
  entry: HorizonWatchPortfolioEntryView | null | undefined,
): string {
  const risk = numericPositionValue(entry, 'riskPressure');
  const opportunity = numericPositionValue(entry, 'opportunityPressure');
  if (risk >= opportunity + 0.08 || threatCount(entry) > 0)
    return 'Under pressure';
  if (opportunity >= risk + 0.08 || tailwindCount(entry) > 0)
    return 'Supported';
  return 'Balanced';
}

function postureTone(
  entry: HorizonWatchPortfolioEntryView | null | undefined,
): {
  bg: string;
  border: string;
  fg: string;
} {
  const label = postureLabel(entry);
  if (label === 'Under pressure') {
    return {
      bg: palette.dangerBg,
      border: palette.dangerBorder,
      fg: palette.dangerText,
    };
  }
  if (label === 'Supported') {
    return {
      bg: palette.successBg,
      border: palette.successBorder,
      fg: palette.successText,
    };
  }
  return {
    bg: palette.brandBg,
    border: palette.brandBorder,
    fg: palette.brandText,
  };
}

function firstThreat(
  entry: HorizonWatchPortfolioEntryView | null | undefined,
): any | null {
  return entry?.monitoringSnapshot?.threats?.[0] || null;
}

function firstTailwind(
  entry: HorizonWatchPortfolioEntryView | null | undefined,
): any | null {
  return entry?.monitoringSnapshot?.tailwinds?.[0] || null;
}

function entrySummary(entry: HorizonWatchPortfolioEntryView): string {
  if (entry.resolutionError) return entry.resolutionError;
  const threat = firstThreat(entry);
  if (threat?.headline) return threat.headline;
  const tailwind = firstTailwind(entry);
  if (tailwind?.headline) return tailwind.headline;
  const escalation = entry.monitoringSnapshot?.recommendedEscalation;
  if (escalation) return `Escalation: ${escalation}`;
  return 'No strong interpreted movement yet.';
}

function endpointLabels(
  entry: HorizonWatchPortfolioEntryView | null | undefined,
): string[] {
  const hits = entry?.monitoringSnapshot?.propagatedExposure || [];
  return hits
    .slice(0, 4)
    .map((hit: any) => String(hit?.label || hit?.canonicalPath || '').trim())
    .filter(Boolean);
}

function digestItems(digest: HorizonMonitoringDigest | null): Array<{
  id: string;
  title: string;
  summary?: string;
  canonicalPath?: string;
  hotness?: number;
}> {
  if (!Array.isArray(digest?.priorityPoints)) return [];
  return digest.priorityPoints
    .map((point, index) => ({
      id: String(point.id || `digest-point-${index}`),
      title: String(point.headline || point.label || '').trim(),
      summary: String(point.summary || point.why || '').trim() || undefined,
      canonicalPath: String(point.canonicalPath || '').trim() || undefined,
      hotness: typeof point.hotness === 'number' ? point.hotness : undefined,
    }))
    .filter((point) => point.title);
}

function StatCard({
  label,
  value,
  hint,
}: {
  label: string;
  value: string;
  hint?: string;
}) {
  return (
    <div style={{ ...panelStyle, padding: 16 }}>
      <div
        style={{
          fontSize: 11,
          textTransform: 'uppercase',
          letterSpacing: '0.12em',
          color: palette.textTertiary,
        }}
      >
        {label}
      </div>
      <div
        style={{
          marginTop: 8,
          fontSize: 30,
          fontWeight: 850,
          color: palette.text,
        }}
      >
        {value}
      </div>
      {hint ? (
        <div
          style={{
            marginTop: 6,
            fontSize: 12,
            lineHeight: 1.6,
            color: palette.textMuted,
          }}
        >
          {hint}
        </div>
      ) : null}
    </div>
  );
}

export const HorizonPortfolioSurface: React.FC<
  HorizonPortfolioSurfaceProps
> = ({ projectName, onOpenWorldMap, onOpenWatch }) => {
  const [isCompact, setIsCompact] = React.useState(
    () => window.innerWidth < 1080,
  );
  const [portfolios, setPortfolios] = React.useState<HorizonWatchPortfolio[]>(
    [],
  );
  const [selectedPortfolioId, setSelectedPortfolioId] = React.useState<
    string | null
  >(null);
  const [selectedEntryId, setSelectedEntryId] = React.useState<string | null>(
    null,
  );
  const [portfolioView, setPortfolioView] =
    React.useState<HorizonWatchPortfolioView | null>(null);
  const [digest, setDigest] = React.useState<HorizonMonitoringDigest | null>(
    null,
  );
  const [loadingList, setLoadingList] = React.useState(true);
  const [loadingDetail, setLoadingDetail] = React.useState(false);
  const [listError, setListError] = React.useState<string | null>(null);
  const [detailError, setDetailError] = React.useState<string | null>(null);

  const loadPortfolios = React.useCallback(async () => {
    setLoadingList(true);
    setListError(null);
    try {
      const next = await api.getHorizonWatchPortfolios();
      setPortfolios(next);
      setSelectedPortfolioId((current) => {
        if (current && next.some((item) => item.id === current)) return current;
        return next[0]?.id || null;
      });
    } catch (err: any) {
      console.error('[HorizonPortfolioSurface] Failed to load portfolios', err);
      setListError(String(err?.message || 'Failed to load portfolios.'));
      setPortfolios([]);
      setSelectedPortfolioId(null);
    } finally {
      setLoadingList(false);
    }
  }, []);

  React.useEffect(() => {
    void loadPortfolios();
  }, [loadPortfolios]);

  React.useEffect(() => {
    const updateCompact = () => setIsCompact(window.innerWidth < 1080);
    updateCompact();
    window.addEventListener('resize', updateCompact);
    return () => window.removeEventListener('resize', updateCompact);
  }, []);

  React.useEffect(() => {
    if (!selectedPortfolioId) {
      setPortfolioView(null);
      setDigest(null);
      setDetailError(null);
      return;
    }
    let cancelled = false;
    const run = async () => {
      setLoadingDetail(true);
      setDetailError(null);
      try {
        const view = await api.getHorizonWatchPortfolio(selectedPortfolioId);
        if (!cancelled) setPortfolioView(view);
      } catch (err: any) {
        if (cancelled) return;
        console.error(
          '[HorizonPortfolioSurface] Failed to load portfolio view',
          err,
        );
        setPortfolioView(null);
        setDetailError(String(err?.message || 'Failed to load portfolio.'));
      } finally {
        if (!cancelled) setLoadingDetail(false);
      }
    };
    const runDigest = async () => {
      try {
        const nextDigest =
          await api.getHorizonWatchPortfolioDigest(selectedPortfolioId);
        if (!cancelled) setDigest(nextDigest);
      } catch (err) {
        if (!cancelled) {
          console.warn(
            '[HorizonPortfolioSurface] Portfolio digest unavailable',
            err,
          );
          setDigest(null);
        }
      }
    };
    void run();
    void runDigest();
    return () => {
      cancelled = true;
    };
  }, [selectedPortfolioId]);

  React.useEffect(() => {
    const entries = portfolioView?.entries || [];
    setSelectedEntryId((current) => {
      if (current && entries.some((entry) => entry.entryId === current))
        return current;
      return entries[0]?.entryId || null;
    });
  }, [portfolioView]);

  const selectedPortfolio =
    portfolioView?.portfolio ||
    portfolios.find((item) => item.id === selectedPortfolioId) ||
    null;
  const entries = portfolioView?.entries || [];
  const selectedEntry =
    entries.find((entry) => entry.entryId === selectedEntryId) ||
    entries[0] ||
    null;
  const todayItems = digestItems(digest).slice(0, 3);
  const pressuredEntries = entries.filter(
    (entry) => postureLabel(entry) === 'Under pressure',
  ).length;
  const supportedEntries = entries.filter(
    (entry) => postureLabel(entry) === 'Supported',
  ).length;
  const reviewEntries = entries.filter((entry) => {
    if (entry.resolutionError) return true;
    return (entry.monitoringSnapshot?.openQuestions || []).length > 0;
  }).length;

  return (
    <div
      style={{
        minHeight: 0,
        flex: 1,
        display: 'grid',
        gridTemplateColumns: isCompact
          ? 'minmax(0, 1fr)'
          : '320px minmax(0, 1fr)',
        gap: 20,
        padding: isCompact ? 14 : 20,
        background: palette.pageBg,
        color: palette.text,
        overflow: 'hidden',
      }}
    >
      <aside
        style={{
          ...panelStyle,
          padding: 18,
          display: 'flex',
          flexDirection: 'column',
          minHeight: 0,
          overflow: 'hidden',
        }}
      >
        <div style={{ marginBottom: 16 }}>
          <div
            style={{
              fontSize: 11,
              textTransform: 'uppercase',
              letterSpacing: '0.16em',
              color: palette.textTertiary,
            }}
          >
            Portfolio
          </div>
          <div
            style={{
              marginTop: 10,
              fontSize: 24,
              fontWeight: 850,
              color: palette.text,
            }}
          >
            Books
          </div>
          <div
            style={{
              marginTop: 8,
              fontSize: 13,
              lineHeight: 1.7,
              color: palette.textMuted,
            }}
          >
            A portfolio starts with books: client accounts, prospects, or
            internal watchbooks. Each book is then filled with business entries
            that resolve into focus objects.
          </div>
          {projectName ? (
            <div
              style={{
                marginTop: 10,
                fontSize: 12,
                color: palette.textTertiary,
              }}
            >
              Current project context: {projectName}
            </div>
          ) : null}
        </div>

        <div style={{ display: 'flex', gap: 8, marginBottom: 14 }}>
          <button
            type="button"
            onClick={() => void loadPortfolios()}
            style={{
              borderRadius: 14,
              border: `1px solid ${palette.panelBorder}`,
              background: palette.panelAltBg,
              color: palette.text,
              padding: '10px 12px',
              fontSize: 12,
              fontWeight: 700,
            }}
          >
            Refresh
          </button>
          {onOpenWatch ? (
            <button
              type="button"
              onClick={onOpenWatch}
              style={{
                borderRadius: 14,
                border: `1px solid ${palette.panelBorder}`,
                background: 'transparent',
                color: palette.textMuted,
                padding: '10px 12px',
                fontSize: 12,
                fontWeight: 700,
              }}
            >
              Watch
            </button>
          ) : null}
          {onOpenWorldMap ? (
            <button
              type="button"
              onClick={onOpenWorldMap}
              style={{
                borderRadius: 14,
                border: `1px solid ${palette.panelBorder}`,
                background: 'transparent',
                color: palette.textMuted,
                padding: '10px 12px',
                fontSize: 12,
                fontWeight: 700,
              }}
            >
              World map
            </button>
          ) : null}
        </div>

        <div
          style={{
            fontSize: 11,
            textTransform: 'uppercase',
            letterSpacing: '0.12em',
            color: palette.textTertiary,
            marginBottom: 10,
          }}
        >
          Tenant books
        </div>

        <div
          style={{
            flex: 1,
            minHeight: 0,
            overflowY: 'auto',
            paddingRight: 4,
            display: 'flex',
            flexDirection: 'column',
            gap: 8,
          }}
        >
          {loadingList ? (
            <div style={{ fontSize: 13, color: palette.textMuted }}>
              Loading books…
            </div>
          ) : listError ? (
            <div
              style={{
                borderRadius: 18,
                border: `1px solid ${palette.dangerBorder}`,
                background: palette.dangerBg,
                padding: 14,
                fontSize: 13,
                lineHeight: 1.7,
                color: palette.dangerText,
              }}
            >
              {listError}
            </div>
          ) : portfolios.length === 0 ? (
            <div
              style={{
                borderRadius: 18,
                border: `1px dashed ${palette.panelBorder}`,
                padding: 16,
                fontSize: 13,
                lineHeight: 1.7,
                color: palette.textMuted,
              }}
            >
              No books yet.
              <br />A portfolio starts with one book and a handful of entries
              such as client accounts, prospects or internal names to watch.
            </div>
          ) : (
            portfolios.map((portfolio) => {
              const isActive = portfolio.id === selectedPortfolioId;
              return (
                <button
                  key={portfolio.id}
                  type="button"
                  onClick={() => setSelectedPortfolioId(portfolio.id)}
                  style={{
                    textAlign: 'left',
                    borderRadius: 18,
                    padding: 14,
                    border: `1px solid ${isActive ? palette.brandBorder : palette.panelBorder}`,
                    background: isActive ? palette.brandBg : palette.panelAltBg,
                    color: palette.text,
                  }}
                >
                  <div
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'space-between',
                      gap: 8,
                    }}
                  >
                    <div
                      style={{
                        fontSize: 15,
                        fontWeight: 750,
                        color: palette.text,
                      }}
                    >
                      {portfolio.name}
                    </div>
                    <span
                      style={{
                        borderRadius: 999,
                        padding: '4px 8px',
                        fontSize: 10,
                        fontWeight: 800,
                        letterSpacing: '0.08em',
                        textTransform: 'uppercase',
                        background: palette.chipBg,
                        color: palette.textMuted,
                      }}
                    >
                      {portfolio.kind || 'mixed'}
                    </span>
                  </div>
                  {portfolio.description ? (
                    <div
                      style={{
                        marginTop: 8,
                        fontSize: 12,
                        lineHeight: 1.6,
                        color: palette.textMuted,
                      }}
                    >
                      {portfolio.description}
                    </div>
                  ) : null}
                  <div
                    style={{
                      marginTop: 10,
                      fontSize: 11,
                      color: palette.textTertiary,
                    }}
                  >
                    {(portfolio.entries || []).length} entries
                  </div>
                </button>
              );
            })
          )}
        </div>
      </aside>

      <main
        style={{
          minWidth: 0,
          minHeight: 0,
          overflowY: 'auto',
          paddingRight: 4,
        }}
      >
        {!selectedPortfolio ? (
          <div style={{ ...panelStyle, padding: 24 }}>
            <div
              style={{
                fontSize: 12,
                textTransform: 'uppercase',
                letterSpacing: '0.14em',
                color: palette.textTertiary,
              }}
            >
              Empty state
            </div>
            <div
              style={{
                marginTop: 10,
                fontSize: 30,
                fontWeight: 850,
                color: palette.text,
              }}
            >
              Start with a book, not with signals
            </div>
            <div
              style={{
                marginTop: 12,
                maxWidth: 860,
                fontSize: 15,
                lineHeight: 1.8,
                color: palette.textMuted,
              }}
            >
              Portfolio is the tenant’s business watchbook. It starts with a
              book such as <strong>Client Accounts</strong>,{' '}
              <strong>Prospects</strong> or{' '}
              <strong>Internal Strategic Watch</strong>. Then it fills with
              entries like Equinor, Northvolt or Euro Mobility. Each entry can
              resolve into a focus object and begin monitoring.
            </div>
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
            <section style={{ ...panelStyle, padding: 24 }}>
              <div
                style={{
                  display: 'flex',
                  alignItems: 'flex-start',
                  justifyContent: 'space-between',
                  gap: 16,
                  flexWrap: 'wrap',
                }}
              >
                <div style={{ maxWidth: 920 }}>
                  <div
                    style={{
                      fontSize: 11,
                      textTransform: 'uppercase',
                      letterSpacing: '0.16em',
                      color: palette.textTertiary,
                    }}
                  >
                    Portfolio book
                  </div>
                  <div
                    style={{
                      marginTop: 10,
                      fontSize: 32,
                      lineHeight: 1.15,
                      fontWeight: 850,
                      color: palette.text,
                    }}
                  >
                    {selectedPortfolio.name}
                  </div>
                  {selectedPortfolio.description ? (
                    <div
                      style={{
                        marginTop: 10,
                        fontSize: 15,
                        lineHeight: 1.8,
                        color: palette.textMuted,
                      }}
                    >
                      {selectedPortfolio.description}
                    </div>
                  ) : null}
                  <div
                    style={{
                      display: 'flex',
                      gap: 8,
                      flexWrap: 'wrap',
                      marginTop: 14,
                    }}
                  >
                    <span
                      style={{
                        borderRadius: 999,
                        padding: '6px 10px',
                        background: palette.brandBg,
                        color: palette.brandText,
                        fontSize: 11,
                        fontWeight: 800,
                        textTransform: 'uppercase',
                        letterSpacing: '0.08em',
                      }}
                    >
                      {selectedPortfolio.kind || 'mixed'}
                    </span>
                    <span
                      style={{
                        borderRadius: 999,
                        padding: '6px 10px',
                        background: palette.chipBg,
                        color: palette.textMuted,
                        fontSize: 11,
                        fontWeight: 800,
                        textTransform: 'uppercase',
                        letterSpacing: '0.08em',
                      }}
                    >
                      {selectedPortfolio.status || 'active'}
                    </span>
                  </div>
                </div>

                <div
                  style={{
                    minWidth: 220,
                    textAlign: isCompact ? 'left' : 'right',
                  }}
                >
                  <div style={{ fontSize: 12, color: palette.textTertiary }}>
                    Updated{' '}
                    {formatDate(
                      portfolioView?.generatedAt || digest?.updatedAt,
                    )}
                  </div>
                  {loadingDetail ? (
                    <div
                      style={{
                        marginTop: 8,
                        fontSize: 12,
                        color: palette.textTertiary,
                      }}
                    >
                      Refreshing monitoring view…
                    </div>
                  ) : null}
                </div>
              </div>
            </section>

            <section
              style={{
                display: 'grid',
                gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
                gap: 14,
              }}
            >
              <StatCard
                label="Entries"
                value={String(entries.length)}
                hint="Business objects in this book"
              />
              <StatCard
                label="Under Pressure"
                value={String(pressuredEntries)}
                hint="Entries where risk dominates"
              />
              <StatCard
                label="Supported"
                value={String(supportedEntries)}
                hint="Entries where tailwinds dominate"
              />
              <StatCard
                label="Needs Review"
                value={String(reviewEntries)}
                hint="Entries with open questions or resolution gaps"
              />
            </section>

            <section style={{ ...panelStyle, padding: 20 }}>
              <div
                style={{
                  fontSize: 11,
                  textTransform: 'uppercase',
                  letterSpacing: '0.12em',
                  color: palette.textTertiary,
                }}
              >
                Today
              </div>
              <div
                style={{
                  marginTop: 6,
                  fontSize: 20,
                  fontWeight: 800,
                  color: palette.text,
                }}
              >
                {digest?.headline || 'No daily digest yet'}
              </div>
              <div
                style={{
                  marginTop: 10,
                  fontSize: 14,
                  lineHeight: 1.8,
                  color: palette.textMuted,
                }}
              >
                {digest?.summary ||
                  'This book has not yet produced a daily portfolio digest.'}
              </div>
              {todayItems.length > 0 ? (
                <div
                  style={{
                    marginTop: 18,
                    display: 'grid',
                    gridTemplateColumns: isCompact
                      ? 'minmax(0, 1fr)'
                      : 'repeat(3, minmax(0, 1fr))',
                    gap: 12,
                  }}
                >
                  {todayItems.map((item) => (
                    <div
                      key={item.id}
                      style={{
                        borderRadius: 18,
                        padding: 14,
                        border: `1px solid ${palette.panelBorder}`,
                        background: palette.panelAltBg,
                      }}
                    >
                      <div
                        style={{
                          fontSize: 14,
                          fontWeight: 750,
                          color: palette.text,
                        }}
                      >
                        {item.title}
                      </div>
                      {item.summary ? (
                        <div
                          style={{
                            marginTop: 8,
                            fontSize: 12,
                            lineHeight: 1.65,
                            color: palette.textMuted,
                          }}
                        >
                          {item.summary}
                        </div>
                      ) : null}
                      <div
                        style={{
                          marginTop: 10,
                          display: 'flex',
                          gap: 10,
                          flexWrap: 'wrap',
                          fontSize: 11,
                          color: palette.textTertiary,
                        }}
                      >
                        {item.canonicalPath ? (
                          <span>{item.canonicalPath}</span>
                        ) : null}
                        {typeof item.hotness === 'number' ? (
                          <span>{formatPct(item.hotness)}</span>
                        ) : null}
                      </div>
                    </div>
                  ))}
                </div>
              ) : null}
            </section>

            {detailError ? (
              <section
                style={{
                  ...panelStyle,
                  padding: 16,
                  borderColor: palette.dangerBorder,
                }}
              >
                <div
                  style={{
                    fontSize: 14,
                    fontWeight: 750,
                    color: palette.dangerText,
                  }}
                >
                  Portfolio view unavailable
                </div>
                <div
                  style={{
                    marginTop: 8,
                    fontSize: 13,
                    lineHeight: 1.7,
                    color: palette.textMuted,
                  }}
                >
                  {detailError}
                </div>
              </section>
            ) : null}

            <section
              style={{
                display: 'grid',
                gridTemplateColumns: isCompact
                  ? 'minmax(0, 1fr)'
                  : 'minmax(0, 1.15fr) minmax(320px, 0.85fr)',
                gap: 20,
                alignItems: 'start',
              }}
            >
              <div style={{ ...panelStyle, padding: 20 }}>
                <div
                  style={{
                    fontSize: 11,
                    textTransform: 'uppercase',
                    letterSpacing: '0.12em',
                    color: palette.textTertiary,
                  }}
                >
                  Entries
                </div>
                <div
                  style={{
                    marginTop: 6,
                    fontSize: 20,
                    fontWeight: 800,
                    color: palette.text,
                  }}
                >
                  Who this book is about
                </div>

                <div
                  style={{
                    marginTop: 16,
                    display: 'flex',
                    flexDirection: 'column',
                    gap: 12,
                  }}
                >
                  {entries.length === 0 ? (
                    <div
                      style={{
                        borderRadius: 18,
                        border: `1px dashed ${palette.panelBorder}`,
                        padding: 16,
                        fontSize: 13,
                        lineHeight: 1.7,
                        color: palette.textMuted,
                      }}
                    >
                      This book has no entries yet.
                      <br />
                      It should first fill with accounts, prospects or internal
                      names that can resolve into focus objects.
                    </div>
                  ) : (
                    entries.map((entry) => {
                      const active = entry.entryId === selectedEntry?.entryId;
                      const tone = postureTone(entry);
                      return (
                        <button
                          key={entry.entryId}
                          type="button"
                          onClick={() => setSelectedEntryId(entry.entryId)}
                          style={{
                            textAlign: 'left',
                            borderRadius: 18,
                            padding: 16,
                            border: `1px solid ${active ? tone.border : palette.panelBorder}`,
                            background: active ? tone.bg : palette.panelAltBg,
                            color: palette.text,
                          }}
                        >
                          <div
                            style={{
                              display: 'flex',
                              alignItems: 'flex-start',
                              justifyContent: 'space-between',
                              gap: 12,
                            }}
                          >
                            <div>
                              <div
                                style={{
                                  fontSize: 15,
                                  fontWeight: 760,
                                  color: palette.text,
                                }}
                              >
                                {entry.label}
                              </div>
                              <div
                                style={{
                                  marginTop: 4,
                                  fontSize: 12,
                                  color: palette.textTertiary,
                                }}
                              >
                                {entry.canonicalPath ||
                                  entry.monitoringSnapshot
                                    ?.anchorCanonicalPath ||
                                  'No canonical path'}
                              </div>
                            </div>
                            <span
                              style={{
                                borderRadius: 999,
                                padding: '5px 10px',
                                background: active ? tone.bg : palette.chipBg,
                                color: active ? tone.fg : palette.textMuted,
                                border: `1px solid ${active ? tone.border : palette.panelBorder}`,
                                fontSize: 11,
                                fontWeight: 800,
                              }}
                            >
                              {postureLabel(entry)}
                            </span>
                          </div>

                          <div
                            style={{
                              marginTop: 10,
                              fontSize: 13,
                              lineHeight: 1.7,
                              color: palette.textMuted,
                            }}
                          >
                            {entrySummary(entry)}
                          </div>

                          <div
                            style={{
                              display: 'flex',
                              gap: 10,
                              flexWrap: 'wrap',
                              marginTop: 12,
                            }}
                          >
                            <span
                              style={{
                                fontSize: 11,
                                color: palette.dangerText,
                              }}
                            >
                              {threatCount(entry)} threats
                            </span>
                            <span
                              style={{
                                fontSize: 11,
                                color: palette.successText,
                              }}
                            >
                              {tailwindCount(entry)} tailwinds
                            </span>
                            {entry.monitoringSnapshot?.recommendedEscalation ? (
                              <span
                                style={{
                                  fontSize: 11,
                                  color: palette.accentText,
                                }}
                              >
                                {entry.monitoringSnapshot.recommendedEscalation}
                              </span>
                            ) : null}
                            {entry.sourceTenantId &&
                            selectedPortfolio?.tenantId &&
                            entry.sourceTenantId !==
                              selectedPortfolio.tenantId ? (
                              <span
                                style={{
                                  fontSize: 11,
                                  color: palette.warningText,
                                }}
                              >
                                from {entry.sourceTenantId}
                              </span>
                            ) : null}
                          </div>
                        </button>
                      );
                    })
                  )}
                </div>
              </div>

              <div style={{ ...panelStyle, padding: 20 }}>
                <div
                  style={{
                    fontSize: 11,
                    textTransform: 'uppercase',
                    letterSpacing: '0.12em',
                    color: palette.textTertiary,
                  }}
                >
                  Entry posture
                </div>
                <div
                  style={{
                    marginTop: 6,
                    fontSize: 20,
                    fontWeight: 800,
                    color: palette.text,
                  }}
                >
                  {selectedEntry?.label || 'Select an entry'}
                </div>

                {selectedEntry ? (
                  <div
                    style={{
                      display: 'flex',
                      flexDirection: 'column',
                      gap: 16,
                      marginTop: 16,
                    }}
                  >
                    <div
                      style={{
                        borderRadius: 18,
                        padding: 16,
                        border: `1px solid ${postureTone(selectedEntry).border}`,
                        background: postureTone(selectedEntry).bg,
                      }}
                    >
                      <div
                        style={{
                          display: 'flex',
                          alignItems: 'center',
                          justifyContent: 'space-between',
                          gap: 10,
                          flexWrap: 'wrap',
                        }}
                      >
                        <div
                          style={{
                            fontSize: 16,
                            fontWeight: 760,
                            color: palette.text,
                          }}
                        >
                          {postureLabel(selectedEntry)}
                        </div>
                        <span
                          style={{
                            fontSize: 12,
                            fontWeight: 800,
                            color: postureTone(selectedEntry).fg,
                          }}
                        >
                          {selectedEntry.monitoringSnapshot
                            ?.recommendedEscalation || 'warm_only'}
                        </span>
                      </div>
                      <div
                        style={{
                          marginTop: 8,
                          fontSize: 12,
                          color: palette.textMuted,
                        }}
                      >
                        {selectedEntry.canonicalPath ||
                          selectedEntry.monitoringSnapshot?.anchorCanonicalPath}
                      </div>
                      <div
                        style={{
                          marginTop: 14,
                          display: 'grid',
                          gridTemplateColumns: 'repeat(3, minmax(0, 1fr))',
                          gap: 10,
                        }}
                      >
                        <div>
                          <div
                            style={{
                              fontSize: 10,
                              textTransform: 'uppercase',
                              letterSpacing: '0.08em',
                              color: palette.textTertiary,
                            }}
                          >
                            Risk
                          </div>
                          <div
                            style={{
                              marginTop: 6,
                              fontSize: 18,
                              fontWeight: 800,
                              color: palette.dangerText,
                            }}
                          >
                            {formatPct(
                              numericPositionValue(
                                selectedEntry,
                                'riskPressure',
                              ),
                            )}
                          </div>
                        </div>
                        <div>
                          <div
                            style={{
                              fontSize: 10,
                              textTransform: 'uppercase',
                              letterSpacing: '0.08em',
                              color: palette.textTertiary,
                            }}
                          >
                            Opportunity
                          </div>
                          <div
                            style={{
                              marginTop: 6,
                              fontSize: 18,
                              fontWeight: 800,
                              color: palette.successText,
                            }}
                          >
                            {formatPct(
                              numericPositionValue(
                                selectedEntry,
                                'opportunityPressure',
                              ),
                            )}
                          </div>
                        </div>
                        <div>
                          <div
                            style={{
                              fontSize: 10,
                              textTransform: 'uppercase',
                              letterSpacing: '0.08em',
                              color: palette.textTertiary,
                            }}
                          >
                            Readiness
                          </div>
                          <div
                            style={{
                              marginTop: 6,
                              fontSize: 18,
                              fontWeight: 800,
                              color: palette.accentText,
                            }}
                          >
                            {formatPct(
                              numericPositionValue(
                                selectedEntry,
                                'monitoringReadiness',
                              ),
                            )}
                          </div>
                        </div>
                      </div>
                    </div>

                    <div>
                      <div
                        style={{
                          fontSize: 11,
                          textTransform: 'uppercase',
                          letterSpacing: '0.12em',
                          color: palette.textTertiary,
                        }}
                      >
                        What threatens it
                      </div>
                      <div
                        style={{
                          marginTop: 8,
                          fontSize: 13,
                          lineHeight: 1.7,
                          color: palette.textMuted,
                        }}
                      >
                        {firstThreat(selectedEntry)?.headline ||
                          'No dominant threat has been surfaced yet.'}
                      </div>
                    </div>

                    <div>
                      <div
                        style={{
                          fontSize: 11,
                          textTransform: 'uppercase',
                          letterSpacing: '0.12em',
                          color: palette.textTertiary,
                        }}
                      >
                        What helps it
                      </div>
                      <div
                        style={{
                          marginTop: 8,
                          fontSize: 13,
                          lineHeight: 1.7,
                          color: palette.textMuted,
                        }}
                      >
                        {firstTailwind(selectedEntry)?.headline ||
                          'No dominant tailwind has been surfaced yet.'}
                      </div>
                    </div>

                    <div>
                      <div
                        style={{
                          fontSize: 11,
                          textTransform: 'uppercase',
                          letterSpacing: '0.12em',
                          color: palette.textTertiary,
                        }}
                      >
                        Affected contour
                      </div>
                      <div
                        style={{
                          marginTop: 10,
                          display: 'flex',
                          flexWrap: 'wrap',
                          gap: 8,
                        }}
                      >
                        {endpointLabels(selectedEntry).length > 0 ? (
                          endpointLabels(selectedEntry).map((label) => (
                            <span
                              key={label}
                              style={{
                                borderRadius: 999,
                                padding: '6px 10px',
                                background: palette.chipBg,
                                border: `1px solid ${palette.panelBorder}`,
                                fontSize: 11,
                                color: palette.textMuted,
                              }}
                            >
                              {label}
                            </span>
                          ))
                        ) : (
                          <div
                            style={{
                              fontSize: 13,
                              color: palette.textTertiary,
                            }}
                          >
                            No propagated contour has been highlighted yet.
                          </div>
                        )}
                      </div>
                    </div>

                    <div>
                      <div
                        style={{
                          fontSize: 11,
                          textTransform: 'uppercase',
                          letterSpacing: '0.12em',
                          color: palette.textTertiary,
                        }}
                      >
                        Open questions
                      </div>
                      <div
                        style={{
                          marginTop: 10,
                          display: 'flex',
                          flexDirection: 'column',
                          gap: 8,
                        }}
                      >
                        {(selectedEntry.monitoringSnapshot?.openQuestions || [])
                          .length > 0 ? (
                          (
                            selectedEntry.monitoringSnapshot?.openQuestions ||
                            []
                          )
                            .slice(0, 4)
                            .map((question) => (
                              <div
                                key={question}
                                style={{
                                  borderRadius: 14,
                                  padding: '10px 12px',
                                  background: palette.warningBg,
                                  border: `1px solid ${palette.warningBorder}`,
                                  fontSize: 12,
                                  lineHeight: 1.6,
                                  color: palette.warningText,
                                }}
                              >
                                {question}
                              </div>
                            ))
                        ) : (
                          <div
                            style={{
                              fontSize: 13,
                              color: palette.textTertiary,
                            }}
                          >
                            No explicit open questions on this entry right now.
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                ) : (
                  <div
                    style={{
                      marginTop: 16,
                      fontSize: 13,
                      color: palette.textTertiary,
                    }}
                  >
                    Choose an entry to see its current posture.
                  </div>
                )}
              </div>
            </section>
          </div>
        )}
      </main>
    </div>
  );
};

export default HorizonPortfolioSurface;
