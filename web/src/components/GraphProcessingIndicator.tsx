import React from 'react';
import { useGenaiChatStore } from '../stores/genaiChatStore';

interface GraphProcessingIndicatorProps {
  variant?: 'empty-state' | 'overlay' | 'status-bar';
}

export const GraphProcessingIndicator: React.FC<GraphProcessingIndicatorProps> = ({ 
  variant = 'empty-state' 
}) => {
  const processingCommands = useGenaiChatStore((s) => s.processingCommands);
  const commandsProgress = useGenaiChatStore((s) => s.commandsProgress);
  
  if (!processingCommands && !commandsProgress) {
    return null;
  }
  
  const progress = commandsProgress || {
    stage: 'parsing',
    processed: 0,
    total: 0,
    currentOperation: 'Processing...',
  };
  
  const getStageLabel = (stage: string): string => {
    switch (stage) {
      case 'parsing': return 'Parsing commands...';
      case 'validating': return 'Validating commands...';
      case 'creating': return 'Creating entities...';
      case 'updating': return 'Updating entities...';
      case 'deleting': return 'Deleting entities...';
      case 'linking': return 'Linking entities...';
      case 'complete': return 'Complete';
      default: return 'Processing...';
    }
  };
  
  const progressPercent = progress.total > 0 
    ? Math.round((progress.processed / progress.total) * 100) 
    : 0;
  
  if (variant === 'empty-state') {
    return (
      <div className="flex flex-col items-center justify-center gap-4">
        <div className="relative w-16 h-16">
          <div
            className="absolute inset-0 rounded-full border-4 border-transparent"
            style={{
              borderTopColor: 'var(--text-heading)',
              animation: 'spin 1s linear infinite',
            }}
          />
        </div>
        <div className="text-center">
          <div
            className="text-16 mb-2"
            style={{
              color: 'var(--text-heading)',
              fontSize: 'clamp(18px, 2vw, 24px)',
              fontWeight: 500,
            }}
          >
            {getStageLabel(progress.stage)}
          </div>
          {progress.currentOperation && (
            <div
              className="text-12"
              style={{
                color: 'var(--text-secondary-strong)',
              }}
            >
              {progress.currentOperation}
            </div>
          )}
          {progress.total > 0 && (
            <div
              className="text-12 mt-2"
              style={{
                color: 'var(--text-secondary-strong)',
              }}
            >
              {progress.processed} / {progress.total} commands
            </div>
          )}
        </div>
      </div>
    );
  }
  
  if (variant === 'overlay') {
    return (
      <div
        className="absolute inset-0 flex items-center justify-center z-50"
        style={{
          backgroundColor: 'rgba(0, 0, 0, 0.5)',
          backdropFilter: 'blur(4px)',
        }}
      >
        <div
          className="bg-elevated rounded-lg p-6 shadow-lg max-w-md mx-4"
          style={{
            backgroundColor: 'var(--bg-elevated)',
            border: '1px solid var(--border-subtle)',
          }}
        >
          <div className="flex items-center gap-4 mb-4">
            <div className="relative w-8 h-8">
              <div
                className="absolute inset-0 rounded-full border-2 border-transparent"
                style={{
                  borderTopColor: 'var(--text-heading)',
                  animation: 'spin 1s linear infinite',
                }}
              />
            </div>
            <div className="flex-1">
              <div
                className="text-14 font-medium"
                style={{
                  color: 'var(--text-heading)',
                }}
              >
                {getStageLabel(progress.stage)}
              </div>
              {progress.currentOperation && (
                <div
                  className="text-12 mt-1"
                  style={{
                    color: 'var(--text-secondary-strong)',
                  }}
                >
                  {progress.currentOperation}
                </div>
              )}
            </div>
          </div>
          {progress.total > 0 && (
            <div className="mt-4">
              <div className="flex justify-between text-11 mb-1" style={{ color: 'var(--text-secondary-strong)' }}>
                <span>{progress.processed} / {progress.total} commands</span>
                <span>{progressPercent}%</span>
              </div>
              <div
                className="h-2 rounded-full overflow-hidden"
                style={{
                  backgroundColor: 'var(--white-06)',
                }}
              >
                <div
                  className="h-full transition-all duration-300"
                  style={{
                    width: `${progressPercent}%`,
                    backgroundColor: 'var(--text-heading)',
                  }}
                />
              </div>
            </div>
          )}
        </div>
      </div>
    );
  }
  
  // status-bar variant
  return (
    <div
      className="absolute bottom-0 left-0 right-0 z-40 p-3"
      style={{
        backgroundColor: 'var(--bg-elevated)',
        borderTop: '1px solid var(--border-subtle)',
      }}
    >
      <div className="flex items-center gap-3">
        <div className="relative w-4 h-4">
          <div
            className="absolute inset-0 rounded-full border-2 border-transparent"
            style={{
              borderTopColor: 'var(--text-heading)',
              animation: 'spin 1s linear infinite',
            }}
          />
        </div>
        <div className="flex-1 min-w-0">
          <div
            className="text-12 font-medium truncate"
            style={{
              color: 'var(--text-heading)',
            }}
          >
            {getStageLabel(progress.stage)}
          </div>
          {progress.currentOperation && (
            <div
              className="text-11 truncate mt-0.5"
              style={{
                color: 'var(--text-secondary-strong)',
              }}
            >
              {progress.currentOperation}
            </div>
          )}
        </div>
        {progress.total > 0 && (
          <div
            className="text-11 whitespace-nowrap"
            style={{
              color: 'var(--text-secondary-strong)',
            }}
          >
            {progress.processed}/{progress.total}
          </div>
        )}
      </div>
      {progress.total > 0 && (
        <div
          className="h-1 rounded-full overflow-hidden mt-2"
          style={{
            backgroundColor: 'var(--white-06)',
          }}
        >
          <div
            className="h-full transition-all duration-300"
            style={{
              width: `${progressPercent}%`,
              backgroundColor: 'var(--text-heading)',
            }}
          />
        </div>
      )}
    </div>
  );
};

