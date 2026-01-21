import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { ThemeContext } from './ThemeProvider';
import type { FragmentProfile } from './FragmentProfileModal';

export interface MarketProfileDashboardProps {
  profile: FragmentProfile | null;
}

/**
 * Dashboard component for displaying Market fragment profiles.
 * Shows structured sections: overview, size_metrics, growth_outlook, competition, trends, barriers, regional.
 */
export const MarketProfileDashboard: React.FC<MarketProfileDashboardProps> = ({ profile }) => {
  const { theme } = React.useContext(ThemeContext);

  if (!profile) {
    return (
      <div
        style={{
          padding: '40px 20px',
          textAlign: 'center',
          color: 'var(--text-secondary-strong)',
          fontSize: 13,
        }}
      >
        No profile data available
      </div>
    );
  }

  // Get sections in order
  const sectionKeys = Object.keys(profile.sections || {}).sort((a, b) => {
    const orderA = profile.sections[a]?.order || 100;
    const orderB = profile.sections[b]?.order || 100;
    return orderA - orderB;
  });

  if (sectionKeys.length === 0) {
    return (
      <div
        style={{
          padding: '40px 20px',
          textAlign: 'center',
          color: 'var(--text-secondary-strong)',
          fontSize: 13,
        }}
      >
        Profile has no sections
      </div>
    );
  }

  return (
    <div
      style={{
        padding: '24px',
        display: 'flex',
        flexDirection: 'column',
        gap: 24,
      }}
    >
      {/* Profile metadata */}
      <div
        style={{
          padding: '12px 16px',
          borderRadius: 12,
          border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
          background: theme === 'dark' ? 'rgba(0,0,0,0.2)' : 'rgba(0,0,0,0.02)',
          fontSize: 11,
          color: 'var(--text-secondary-strong)',
          display: 'flex',
          alignItems: 'center',
          gap: 16,
          flexWrap: 'wrap',
        }}
      >
        <div>
          <span style={{ fontWeight: 700 }}>Captured:</span>{' '}
          {new Date(profile.capturedAt).toLocaleDateString('en-US', {
            year: 'numeric',
            month: 'short',
            day: 'numeric',
            hour: '2-digit',
            minute: '2-digit',
          })}
        </div>
        {profile.generation?.groundingUsed && (
          <div>
            <span style={{ fontWeight: 700 }}>Grounding:</span> Enabled
          </div>
        )}
        {profile.generation?.model && (
          <div>
            <span style={{ fontWeight: 700 }}>Model:</span> {profile.generation.model}
          </div>
        )}
        {profile.generation?.graphContext && (
          <div>
            <span style={{ fontWeight: 700 }}>Context:</span>{' '}
            {profile.generation.graphContext.neighborCount} neighbors,{' '}
            {profile.generation.graphContext.edgeCount} edges
          </div>
        )}
      </div>

      {/* Sections */}
      {sectionKeys.map((sectionKey) => {
        const section = profile.sections[sectionKey];
        if (!section) return null;

        const hasContent = Boolean(section.content?.trim());
        const hasStructured = section.structured && Object.keys(section.structured).length > 0;
        const hasSources = section.sources && section.sources.length > 0;

        if (!hasContent && !hasStructured) return null;

        return (
          <div
            key={sectionKey}
            style={{
              display: 'flex',
              flexDirection: 'column',
              gap: 12,
            }}
          >
            {/* Section header */}
            <div
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: 12,
              }}
            >
              <h3
                style={{
                  fontSize: 16,
                  fontWeight: 800,
                  color: 'var(--text-heading)',
                  margin: 0,
                }}
              >
                {section.title || sectionKey}
              </h3>
              {section.confidence !== undefined && (
                <div
                  style={{
                    fontSize: 11,
                    fontWeight: 700,
                    color:
                      section.confidence >= 0.8
                        ? '#10b981'
                        : section.confidence >= 0.6
                        ? '#f59e0b'
                        : '#f87171',
                    padding: '4px 10px',
                    borderRadius: 999,
                    background:
                      theme === 'dark'
                        ? 'rgba(255,255,255,0.06)'
                        : 'rgba(0,0,0,0.04)',
                  }}
                >
                  {Math.round(section.confidence * 100)}%
                </div>
              )}
            </div>

            {/* Structured data */}
            {hasStructured && (
              <div
                style={{
                  padding: '12px 16px',
                  borderRadius: 10,
                  border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
                  background: theme === 'dark' ? 'rgba(0,0,0,0.15)' : 'rgba(0,0,0,0.02)',
                  fontSize: 13,
                  fontFamily: 'monospace',
                }}
              >
                <pre
                  style={{
                    margin: 0,
                    whiteSpace: 'pre-wrap',
                    wordBreak: 'break-word',
                    color: 'var(--text-heading)',
                  }}
                >
                  {JSON.stringify(section.structured, null, 2)}
                </pre>
              </div>
            )}

            {/* Content */}
            {hasContent && (
              <div
                style={{
                  fontSize: 13,
                  color: 'var(--text-heading)',
                  lineHeight: 1.6,
                }}
              >
                <ReactMarkdown remarkPlugins={[remarkGfm]}>
                  {section.content}
                </ReactMarkdown>
              </div>
            )}

            {/* Sources */}
            {hasSources && (
              <div
                style={{
                  marginTop: 4,
                  paddingTop: 12,
                  borderTop: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.06)' : 'rgba(15,23,42,0.06)'}`,
                }}
              >
                <div
                  style={{
                    fontSize: 11,
                    fontWeight: 700,
                    color: 'var(--text-secondary-strong)',
                    marginBottom: 8,
                  }}
                >
                  Sources:
                </div>
                <div
                  style={{
                    display: 'flex',
                    flexDirection: 'column',
                    gap: 6,
                  }}
                >
                  {(section.sources ?? []).map((source, idx) => (
                    <a
                      key={idx}
                      href={source}
                      target="_blank"
                      rel="noopener noreferrer"
                      style={{
                        fontSize: 11,
                        color: 'var(--accent-primary, #3B82F6)',
                        textDecoration: 'none',
                        overflow: 'hidden',
                        textOverflow: 'ellipsis',
                        whiteSpace: 'nowrap',
                        maxWidth: '100%',
                      }}
                      onMouseEnter={(e) => {
                        e.currentTarget.style.textDecoration = 'underline';
                      }}
                      onMouseLeave={(e) => {
                        e.currentTarget.style.textDecoration = 'none';
                      }}
                    >
                      {source}
                    </a>
                  ))}
                </div>
              </div>
            )}
          </div>
        );
      })}

      {/* Generation notes if available */}
      {profile.generation && 'generationNotes' in profile.generation && (
        <div
          style={{
            padding: '12px 16px',
            borderRadius: 10,
            border: `1px solid ${theme === 'dark' ? 'rgba(255,255,255,0.08)' : 'rgba(15,23,42,0.08)'}`,
            background: theme === 'dark' ? 'rgba(0,0,0,0.15)' : 'rgba(0,0,0,0.02)',
            fontSize: 12,
            color: 'var(--text-secondary-strong)',
            fontStyle: 'italic',
          }}
        >
          {(profile.generation as any).generationNotes}
        </div>
      )}
    </div>
  );
};
