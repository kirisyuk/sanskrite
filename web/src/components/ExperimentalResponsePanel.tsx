import React from 'react';
import { ExperimentalResponse } from '../lib/experimentalResponse';

interface ExperimentalResponsePanelProps {
  response: ExperimentalResponse | null;
  loading: boolean;
}

export const ExperimentalResponsePanel: React.FC<ExperimentalResponsePanelProps> = ({ 
  response, 
  loading 
}) => {
  if (loading) {
    return (
      <div className="flex items-center justify-center py-8" style={{ color: 'var(--text-secondary)' }}>
        <div className="flex flex-col items-center gap-2">
          <div className="animate-pulse text-xs">Generating experimental response...</div>
          <div className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
            Step 1: Creating reasoning chain with Gemini + Web Search
          </div>
        </div>
      </div>
    );
  }

  if (!response) {
    return (
      <div className="flex items-center justify-center py-8 text-center" style={{ color: 'var(--text-secondary)' }}>
        <span className="text-xs">No experimental response yet.<br />Send a message to see how the AI thinks.</span>
      </div>
    );
  }

  return (
    <div className="space-y-4" style={{ padding: '12px' }}>
      {/* User Query */}
      <div>
        <div className="text-[11px] uppercase tracking-wide mb-1.5" style={{ color: 'var(--text-tertiary-weak)' }}>
          User Query
        </div>
        <div className="text-12 p-2 rounded" style={{ 
          background: 'var(--white-06)', 
          border: '1px solid var(--overlay-line-90)',
          color: 'var(--text-heading)'
        }}>
          {response.userMessage}
        </div>
      </div>

      {/* Reasoning Chain Section */}
      <div>
        <div className="text-[11px] uppercase tracking-wide mb-1.5" style={{ color: 'var(--text-tertiary-weak)' }}>
          Reasoning Chain (Gemini Fast + Web Search)
        </div>
        <div 
          className="text-12 p-3 rounded whitespace-pre-wrap" 
          style={{ 
            background: 'var(--white-06)', 
            border: '1px solid var(--overlay-line-90)',
            color: 'var(--text-heading)',
            maxHeight: '400px',
            overflowY: 'auto'
          }}
        >
          {response.reasoningChain}
        </div>
      </div>

      {/* Sources */}
      {response.sources.length > 0 && (
        <div>
          <div className="text-[11px] uppercase tracking-wide mb-1.5" style={{ color: 'var(--text-tertiary-weak)' }}>
            Sources ({response.sources.length})
          </div>
          <div className="space-y-1">
            {response.sources.map((source, idx) => (
              <a
                key={idx}
                href={source.url}
                target="_blank"
                rel="noopener noreferrer"
                className="block p-2 rounded transition-colors"
                style={{
                  background: 'var(--white-06)',
                  border: '1px solid var(--overlay-line-90)',
                  color: 'var(--accent-primary)',
                  textDecoration: 'none'
                }}
                onMouseEnter={(e) => {
                  (e.currentTarget as HTMLElement).style.backgroundColor = 'var(--white-10)';
                }}
                onMouseLeave={(e) => {
                  (e.currentTarget as HTMLElement).style.backgroundColor = 'var(--white-06)';
                }}
              >
                <div className="text-[10px] truncate mb-0.5" style={{ color: 'var(--text-tertiary-weak)' }}>
                  {source.url}
                </div>
                <div className="text-12" style={{ color: 'var(--text-heading)' }}>
                  {source.snippet}
                </div>
              </a>
            ))}
          </div>
        </div>
      )}

      {/* Full Response */}
      <div>
        <div className="text-[11px] uppercase tracking-wide mb-1.5" style={{ color: 'var(--text-tertiary-weak)' }}>
          Full Response (Grok Fast)
        </div>
        <div 
          className="text-12 p-3 rounded whitespace-pre-wrap" 
          style={{ 
            background: 'var(--white-06)', 
            border: '1px solid var(--overlay-line-90)',
            color: 'var(--text-heading)',
            maxHeight: '500px',
            overflowY: 'auto'
          }}
        >
          {response.fullResponse}
        </div>
      </div>
    </div>
  );
};

