import React from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';

interface ArtifactsPanelToolbarProps {
  onAnalyzeEntities: () => Promise<void>;
  onAnalyzeFacts: () => Promise<void>;
  onAnalyzeEvents: () => Promise<void>;
  onAnalyzeMetrics: () => Promise<void>;
  onAnalyzeDecisions: () => Promise<void>;
}

export const ArtifactsPanelToolbar: React.FC<ArtifactsPanelToolbarProps> = ({
  onAnalyzeEntities,
  onAnalyzeFacts,
  onAnalyzeEvents,
  onAnalyzeMetrics,
  onAnalyzeDecisions,
}) => {
  return (
    <div
      className="mb-3 px-3 py-2 rounded-lg"
      style={{ backgroundColor: 'var(--toolbar-bg)' }}
    >
      <div
        style={{
          display: 'flex',
          justifyContent: 'flex-start',
          gap: 6,
        }}
      >
        <button
          type="button"
          aria-label="Analyze entities"
          title="Analyze entities"
          className="p-2 rounded"
          style={{
            background: 'transparent',
            color: 'var(--text-secondary-strong)',
            cursor: 'pointer',
          }}
          onMouseEnter={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'var(--white-10)';
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'transparent';
          }}
          onClick={onAnalyzeEntities}
        >
          <svg
            className="w-4 h-4"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
          >
            <circle cx="5" cy="12" r="2" strokeWidth="2" />
            <circle cx="19" cy="7" r="2" strokeWidth="2" />
            <circle cx="19" cy="17" r="2" strokeWidth="2" />
            <path
              d="M7 12h8M17 8l-4 4 4 4"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
        </button>
        <button
          type="button"
          aria-label="Analyze facts"
          title="Analyze facts"
          className="p-2 rounded"
          style={{
            background: 'transparent',
            color: 'var(--text-secondary-strong)',
            cursor: 'pointer',
          }}
          onMouseEnter={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'var(--white-10)';
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'transparent';
          }}
          onClick={onAnalyzeFacts}
        >
          <svg
            className="w-4 h-4"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
          >
            <path d="M3 4h18v4H3zM3 10h18v10H3z" strokeWidth="2" />
            <path
              d="M6 13h6M6 17h10"
              strokeWidth="2"
              strokeLinecap="round"
            />
          </svg>
        </button>
        <button
          type="button"
          aria-label="Analyze events"
          title="Analyze events"
          className="p-2 rounded"
          style={{
            background: 'transparent',
            color: 'var(--text-secondary-strong)',
            cursor: 'pointer',
          }}
          onMouseEnter={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'var(--white-10)';
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'transparent';
          }}
          onClick={onAnalyzeEvents}
        >
          <svg
            className="w-4 h-4"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
          >
            <rect
              x="3"
              y="4"
              width="18"
              height="16"
              rx="2"
              strokeWidth="2"
            />
            <path
              d="M3 8h18M7 2v4M17 2v4M8 12h4"
              strokeWidth="2"
              strokeLinecap="round"
            />
          </svg>
        </button>
        <button
          type="button"
          aria-label="Analyze metrics"
          title="Analyze metrics"
          className="p-2 rounded"
          style={{
            background: 'transparent',
            color: 'var(--text-secondary-strong)',
            cursor: 'pointer',
          }}
          onMouseEnter={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'var(--white-10)';
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'transparent';
          }}
          onClick={onAnalyzeMetrics}
        >
          <svg
            className="w-4 h-4"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
          >
            <path
              d="M4 20V10M10 20V4M16 20v-8M22 20H2"
              strokeWidth="2"
              strokeLinecap="round"
            />
          </svg>
        </button>
        <button
          type="button"
          aria-label="Analyze decisions"
          title="Analyze decisions"
          className="p-2 rounded"
          style={{
            background: 'transparent',
            color: 'var(--text-secondary-strong)',
            cursor: 'pointer',
          }}
          onMouseEnter={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'var(--white-10)';
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'transparent';
          }}
          onClick={onAnalyzeDecisions}
        >
          <svg
            className="w-4 h-4"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
          >
            <path
              d="M4 12h8M12 12l4-4M12 12l4 4"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
            <circle cx="4" cy="12" r="2" strokeWidth="2" />
          </svg>
        </button>
        <button
          type="button"
          aria-label="Create artifact"
          title="Create artifact"
          className="p-2 rounded"
          style={{
            background: 'transparent',
            color: 'var(--text-secondary-strong)',
            cursor: 'pointer',
          }}
          onMouseEnter={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'var(--white-10)';
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'transparent';
          }}
          onClick={() => {
            /* no-op for now */
          }}
        >
          <svg
            className="w-4 h-4"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
          >
            <path
              d="M12 5v14M5 12h14"
              strokeWidth="2"
              strokeLinecap="round"
            />
          </svg>
        </button>
      </div>
    </div>
  );
};

