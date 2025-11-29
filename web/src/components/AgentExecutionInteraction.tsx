import React from 'react';

interface AgentExecutionInteractionProps {
  interaction: {
    id: string;
    content: string;
    [key: string]: any;
  };
}

interface AgentExecutionData {
  jobId: string;
  agentId: string;
  title?: string;
  userQuery: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'partial';
  progress: number;
  steps: Array<{
    id: string;
    capability: string;
    status: string;
    message?: string;
    error?: string;
    output?: Record<string, any>;
    startedAt?: number;
    completedAt?: number;
  }>;
  error?: string;
  result?: Record<string, any>;
}

export const AgentExecutionInteraction: React.FC<AgentExecutionInteractionProps> = ({
  interaction,
}) => {
  let executionData: AgentExecutionData | null = null;
  
  try {
    executionData = JSON.parse(interaction.content) as AgentExecutionData;
  } catch (e) {
    console.error('[AgentExecutionInteraction] Failed to parse content', e);
    return null;
  }

  if (!executionData) {
    return null;
  }

  const getStatusIcon = (status: string) => {
    if (status === 'running' || status === 'pending') {
      return (
        <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ color: 'var(--text-secondary-strong)' }}>
          <circle cx="12" cy="12" r="10" strokeWidth="2" strokeDasharray="32" strokeDashoffset="32" strokeLinecap="round">
            <animate attributeName="stroke-dasharray" dur="1.5s" values="0 32;16 16;0 32;0 32" repeatCount="indefinite" />
            <animate attributeName="stroke-dashoffset" dur="1.5s" values="0;-16;-32;-32" repeatCount="indefinite" />
          </circle>
        </svg>
      );
    } else if (status === 'completed') {
      return (
        <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ color: 'var(--text-secondary-strong)' }}>
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
        </svg>
      );
    } else if (status === 'failed') {
      return (
        <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ color: '#f87171' }}>
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
        </svg>
      );
    } else if (status === 'partial') {
      return (
        <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ color: 'var(--text-secondary-strong)' }}>
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
        </svg>
      );
    }
    return null;
  };

  const getStepStatusIcon = (status: string) => {
    if (status === 'running' || status === 'pending') {
      return (
        <svg className="w-2.5 h-2.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ color: 'var(--text-secondary-strong)' }}>
          <circle cx="12" cy="12" r="10" strokeWidth="2" strokeDasharray="32" strokeDashoffset="32" strokeLinecap="round">
            <animate attributeName="stroke-dasharray" dur="1.5s" values="0 32;16 16;0 32;0 32" repeatCount="indefinite" />
            <animate attributeName="stroke-dashoffset" dur="1.5s" values="0;-16;-32;-32" repeatCount="indefinite" />
          </circle>
        </svg>
      );
    } else if (status === 'completed') {
      return (
        <svg className="w-2.5 h-2.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ color: 'var(--text-secondary-strong)' }}>
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
        </svg>
      );
    } else if (status === 'failed') {
      return (
        <svg className="w-2.5 h-2.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ color: '#f87171' }}>
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
        </svg>
      );
    } else if (status === 'skipped') {
      return (
        <svg className="w-2.5 h-2.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" style={{ color: 'var(--text-tertiary-weak)' }}>
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 12h14" />
        </svg>
      );
    }
    return null;
  };

  const title = executionData.title || executionData.userQuery || 'Agent Execution';
  const isRunning = executionData.status === 'running' || executionData.status === 'pending';
  const isCompleted = executionData.status === 'completed';
  const isFailed = executionData.status === 'failed';

  return (
    <div
      className="rounded-lg px-3 py-2 mb-2"
      style={{
        backgroundColor: 'var(--bg-input)',
        border: '1px solid var(--white-10)',
      }}
    >
      {/* Header */}
      <div className="flex items-center gap-2 mb-2">
        {getStatusIcon(executionData.status)}
        <div className="flex-1">
          <div className="text-xs font-medium" style={{ color: 'var(--text-heading)' }}>
            {title}
          </div>
          {executionData.agentId && (
            <div className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
              {executionData.agentId}
            </div>
          )}
        </div>
        {executionData.progress > 0 && (
          <div className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
            {Math.round(executionData.progress * 100)}%
          </div>
        )}
      </div>

      {/* Steps */}
      {executionData.steps && executionData.steps.length > 0 && (
        <div className="mt-2 space-y-1">
          {executionData.steps.map((step) => (
            <div
              key={step.id}
              className="flex items-start gap-2 text-xs"
              style={{ color: 'var(--text-secondary-strong)' }}
            >
              <div className="flex-shrink-0 mt-0.5">
                {getStepStatusIcon(step.status)}
              </div>
              <div className="flex-1 min-w-0">
                <div className="font-medium">{step.capability}</div>
                {step.message && (
                  <div className="text-[10px] mt-0.5" style={{ color: 'var(--text-tertiary-weak)' }}>
                    {step.message}
                  </div>
                )}
                {step.error && (
                  <div className="text-[10px] mt-0.5" style={{ color: '#f87171' }}>
                    {step.error}
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Error message */}
      {isFailed && executionData.error && (
        <div className="mt-2 text-xs" style={{ color: '#f87171' }}>
          {executionData.error}
        </div>
      )}

      {/* Result */}
      {isCompleted && executionData.result && Object.keys(executionData.result).length > 0 && (
        <div className="mt-2 pt-2 border-t" style={{ borderColor: 'var(--white-10)' }}>
          <div className="text-[10px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>
            Result:
          </div>
          <div className="text-xs" style={{ color: 'var(--text-heading)' }}>
            <pre className="whitespace-pre-wrap break-words">
              {JSON.stringify(executionData.result, null, 2)}
            </pre>
          </div>
        </div>
      )}
    </div>
  );
};

