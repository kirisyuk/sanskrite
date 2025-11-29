/**
 * Компонент Agent Job Details
 * Показывает детали задания агента при нажатии на job
 */

import React from 'react';
import { useTranslation } from 'react-i18next';
import { ModalWithHeader } from './ModalWithHeader';
import { useAgentsStore } from '../stores/agentsStore';
import { useProjectsStore } from '../stores/projectsStore';
import { api } from '../lib/api';

// AgentStep type matching backend model
type AgentStep = {
  id: string;
  capability: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'skipped';
  input?: Record<string, any>;
  output?: Record<string, any>;
  error?: string;
  retryCount: number;
  startedAt?: string;
  completedAt?: string;
  dependsOn?: string[];
  nextSteps?: string[];
  message?: string;
  isCritical: boolean;
  executionStatus?: 'success' | 'partial' | 'failed' | 'retry_needed';
  confidence?: number;
  retryable: boolean;
  alternativeCapabilities?: string[];
  reason?: string;
};

// AgentJob type matching backend model
type AgentJob = {
  id: string;
  tenantId: string;
  projectId: string;
  userId: string;
  agentId: string;
  title?: string;
  status: 'pending' | 'running' | 'completed' | 'failed' | 'partial' | 'paused';
  progress: number;
  task: Record<string, any>;
  result?: Record<string, any>;
  steps: AgentStep[];
  createdAt: string;
  updatedAt: string;
  completedAt?: string;
  autoMode: boolean;
  userQuery: string;
  intent?: Record<string, any>;
  goal?: Record<string, any>;
  planMode: 'auto' | 'manual' | 'hybrid';
  replanTriggered: boolean;
  replanCount: number;
  lastReplanAt?: string;
  interactionId?: string;
};

// Local type definition matching AgentTaskState from agentsStore
type AgentTaskState = {
  taskId: string;
  agentId: string;
  agentName: string;
  status: 'idle' | 'running' | 'completed' | 'error';
  progress: number; // 0.0 - 1.0
  stage?: 'searching' | 'analyzing' | 'planning' | 'executing' | 'saving' | 'completed' | 'error';
  message?: string;
  error?: string;
  result?: any;
  startedAt?: number;
  completedAt?: number;
};

export interface AgentJobDetailsProps {
  isOpen: boolean;
  taskId: string | null;
  onClose: () => void;
}

// Компонент для отображения статуса задания
const StatusBadge: React.FC<{ status: AgentTaskState['status'] }> = ({ status }) => {
  const getStatusColor = () => {
    switch (status) {
      case 'completed':
        return { bg: 'var(--success-20)', color: 'var(--accent-success)' };
      case 'running':
        return { bg: 'rgba(96, 165, 250, 0.20)', color: '#60a5fa' };
      case 'error':
        return { bg: 'rgba(248, 113, 113, 0.20)', color: '#f87171' };
      case 'idle':
      default:
        return { bg: 'var(--white-06)', color: 'var(--text-secondary-strong)' };
    }
  };

  const colors = getStatusColor();

  return (
    <span
      className="text-xs px-2 py-1 rounded"
      style={{
        backgroundColor: colors.bg,
        color: colors.color,
        fontWeight: 500,
      }}
    >
      {status === 'completed' ? 'Completed' : 
       status === 'running' ? 'Running' : 
       status === 'error' ? 'Error' : 
       'Idle'}
    </span>
  );
};

// Компонент для отображения прогресса
const ProgressBar: React.FC<{ progress: number }> = ({ progress }) => {
  const percentage = Math.round(progress * 100);
  
  return (
    <div className="w-full">
      <div className="flex items-center justify-between mb-1">
        <span className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
          Progress
        </span>
        <span className="text-xs font-medium" style={{ color: 'var(--text-heading)' }}>
          {percentage}%
        </span>
      </div>
      <div
        className="w-full rounded-full overflow-hidden"
        style={{
          height: 6,
          backgroundColor: 'var(--white-06)',
        }}
      >
        <div
          className="h-full transition-all duration-300 ease-out"
          style={{
            width: `${percentage}%`,
            backgroundColor: progress === 1 
              ? 'var(--accent-success)' 
              : progress > 0.5 
              ? '#60a5fa' 
              : '#f59e0b',
          }}
        />
      </div>
    </div>
  );
};

// Компонент для отображения информации о задании
const JobInfoSection: React.FC<{ label: string; value: string | number | undefined }> = ({ label, value }) => {
  if (value === undefined || value === null) return null;
  
  return (
    <div className="mb-3">
      <div className="text-xs mb-1" style={{ color: 'var(--text-secondary-strong)' }}>
        {label}
      </div>
      <div className="text-sm" style={{ color: 'var(--text-heading)' }}>
        {typeof value === 'number' ? value.toLocaleString() : String(value)}
      </div>
    </div>
  );
};

// Компонент для отображения результата
const ResultSection: React.FC<{ result: any }> = ({ result }) => {
  if (!result) return null;

  return (
    <div className="mt-4">
      <div className="text-xs font-medium mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
        Result
      </div>
      <div
        className="rounded-lg p-3 text-xs"
        style={{
          backgroundColor: 'var(--white-06)',
          color: 'var(--text-heading)',
          fontFamily: 'monospace',
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
          maxHeight: '400px',
          overflowY: 'auto',
        }}
      >
        {JSON.stringify(result, null, 2)}
      </div>
    </div>
  );
};

// Компонент для отображения ошибки
const ErrorSection: React.FC<{ error: string }> = ({ error }) => {
  return (
    <div className="mt-4">
      <div className="text-xs font-medium mb-2" style={{ color: '#f87171' }}>
        Error
      </div>
      <div
        className="rounded-lg p-3 text-xs"
        style={{
          backgroundColor: 'rgba(248, 113, 113, 0.10)',
          border: '1px solid rgba(248, 113, 113, 0.30)',
          color: '#f87171',
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
        }}
      >
        {error}
      </div>
    </div>
  );
};

// Компонент для отображения шага
const StepItem: React.FC<{ step: AgentStep; index: number }> = ({ step, index }) => {
  const getStatusColor = () => {
    switch (step.status) {
      case 'completed':
        return { bg: 'var(--success-20)', color: 'var(--accent-success)', icon: '✓' };
      case 'running':
        return { bg: 'rgba(96, 165, 250, 0.20)', color: '#60a5fa', icon: '⟳' };
      case 'failed':
        return { bg: 'rgba(248, 113, 113, 0.20)', color: '#f87171', icon: '✗' };
      case 'skipped':
        return { bg: 'var(--white-06)', color: 'var(--text-secondary)', icon: '⊘' };
      case 'pending':
      default:
        return { bg: 'var(--white-06)', color: 'var(--text-secondary-strong)', icon: '○' };
    }
  };

  const colors = getStatusColor();
  const [isExpanded, setIsExpanded] = React.useState(false);

  return (
    <div
      className="rounded-lg border mb-2"
      style={{
        borderColor: 'var(--white-10)',
        backgroundColor: 'var(--white-03)',
      }}
    >
      <div
        className="p-3 cursor-pointer"
        onClick={() => setIsExpanded(!isExpanded)}
      >
        <div className="flex items-start gap-3">
          <div
            className="w-6 h-6 rounded flex items-center justify-center text-xs font-medium flex-shrink-0"
            style={{
              backgroundColor: colors.bg,
              color: colors.color,
            }}
          >
            {index + 1}
          </div>
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 mb-1">
              <span className="text-sm font-medium" style={{ color: 'var(--text-heading)' }}>
                {step.capability}
              </span>
              <span
                className="text-xs px-2 py-0.5 rounded"
                style={{
                  backgroundColor: colors.bg,
                  color: colors.color,
                }}
              >
                {step.status}
              </span>
              {step.isCritical && (
                <span className="text-xs px-2 py-0.5 rounded" style={{ backgroundColor: 'rgba(248, 113, 113, 0.20)', color: '#f87171' }}>
                  Critical
                </span>
              )}
            </div>
            {step.message && (
              <div className="text-xs mt-1" style={{ color: 'var(--text-secondary-strong)' }}>
                {step.message}
              </div>
            )}
            {step.reason && (
              <div className="text-xs mt-1" style={{ color: 'var(--text-secondary)' }}>
                {step.reason}
              </div>
            )}
          </div>
          <div className="text-xs" style={{ color: 'var(--text-secondary)' }}>
            {isExpanded ? '▼' : '▶'}
          </div>
        </div>
      </div>
      
      {isExpanded && (
        <div className="px-3 pb-3 pt-0 border-t" style={{ borderColor: 'var(--white-10)' }}>
          <div className="space-y-2 mt-2">
            {step.error && (
              <div className="text-xs p-2 rounded" style={{ backgroundColor: 'rgba(248, 113, 113, 0.10)', color: '#f87171' }}>
                <strong>Error:</strong> {step.error}
              </div>
            )}
            {step.input && Object.keys(step.input).length > 0 && (
              <div>
                <div className="text-xs font-medium mb-1" style={{ color: 'var(--text-secondary-strong)' }}>
                  Input:
                </div>
                <div
                  className="text-xs p-2 rounded font-mono"
                  style={{
                    backgroundColor: 'var(--white-06)',
                    color: 'var(--text-heading)',
                    whiteSpace: 'pre-wrap',
                    wordBreak: 'break-word',
                    maxHeight: '200px',
                    overflowY: 'auto',
                  }}
                >
                  {JSON.stringify(step.input, null, 2)}
                </div>
              </div>
            )}
            {step.output && Object.keys(step.output).length > 0 && (
              <div>
                <div className="text-xs font-medium mb-1" style={{ color: 'var(--text-secondary-strong)' }}>
                  Output:
                </div>
                <div
                  className="text-xs p-2 rounded font-mono"
                  style={{
                    backgroundColor: 'var(--white-06)',
                    color: 'var(--text-heading)',
                    whiteSpace: 'pre-wrap',
                    wordBreak: 'break-word',
                    maxHeight: '200px',
                    overflowY: 'auto',
                  }}
                >
                  {JSON.stringify(step.output, null, 2)}
                </div>
              </div>
            )}
            <div className="flex gap-4 text-xs" style={{ color: 'var(--text-secondary)' }}>
              {step.startedAt && (
                <span>Started: {new Date(step.startedAt).toLocaleString()}</span>
              )}
              {step.completedAt && (
                <span>Completed: {new Date(step.completedAt).toLocaleString()}</span>
              )}
              {step.retryCount > 0 && (
                <span>Retries: {step.retryCount}</span>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

// Компонент для отображения списка шагов
const StepsSection: React.FC<{ steps: AgentStep[] }> = ({ steps }) => {
  if (!steps || steps.length === 0) return null;

  const completedCount = steps.filter(s => s.status === 'completed').length;
  const failedCount = steps.filter(s => s.status === 'failed').length;
  const runningCount = steps.filter(s => s.status === 'running').length;
  const pendingCount = steps.filter(s => s.status === 'pending').length;

  return (
    <div className="mt-4">
      <div className="flex items-center justify-between mb-3">
        <div className="text-xs font-medium" style={{ color: 'var(--text-secondary-strong)' }}>
          Steps ({steps.length} total)
        </div>
        <div className="flex gap-2 text-xs" style={{ color: 'var(--text-secondary)' }}>
          {completedCount > 0 && <span>✓ {completedCount}</span>}
          {runningCount > 0 && <span>⟳ {runningCount}</span>}
          {failedCount > 0 && <span>✗ {failedCount}</span>}
          {pendingCount > 0 && <span>○ {pendingCount}</span>}
        </div>
      </div>
      <div className="space-y-2">
        {steps.map((step, index) => (
          <StepItem 
            key={`step-${step.id || step.capability || 'unknown'}-${index}`} 
            step={step} 
            index={index} 
          />
        ))}
      </div>
    </div>
  );
};

// Компонент для отображения JSON данных
const JSONSection: React.FC<{ title: string; data: any }> = ({ title, data }) => {
  if (!data || (typeof data === 'object' && Object.keys(data).length === 0)) return null;

  return (
    <div className="mt-4">
      <div className="text-xs font-medium mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
        {title}
      </div>
      <div
        className="rounded-lg p-3 text-xs font-mono"
        style={{
          backgroundColor: 'var(--white-06)',
          color: 'var(--text-heading)',
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
          maxHeight: '300px',
          overflowY: 'auto',
        }}
      >
        {JSON.stringify(data, null, 2)}
      </div>
    </div>
  );
};

export const AgentJobDetails: React.FC<AgentJobDetailsProps> = ({
  isOpen,
  taskId,
  onClose,
}) => {
  const { t } = useTranslation();
  const getTaskState = useAgentsStore((s) => s.getTaskState);
  const loadAgents = useAgentsStore((s) => s.loadAgents);
  const agents = useAgentsStore((s) => s.agents);
  const clearTask = useAgentsStore((s) => s.clearTask);
  const currentProject = useProjectsStore((s) => s.currentProject);
  const [apiJobData, setApiJobData] = React.useState<any>(null);
  const [loadingApiJob, setLoadingApiJob] = React.useState(false);
  const [actionLoading, setActionLoading] = React.useState(false);
  
  // Load agents list if not loaded
  React.useEffect(() => {
    if (agents.length === 0) {
      loadAgents();
    }
  }, [agents.length, loadAgents]);
  
  // Helper to get agent name by agentId
  const getAgentName = React.useCallback((agentId: string): string => {
    if (!agentId) return 'Unknown Agent';
    const agent = agents.find((a) => a.id === agentId);
    if (agent) return agent.displayName || agent.name || agentId;
    // Fallback: map common agent IDs to readable names
    const agentNameMap: Record<string, string> = {
      'data': 'Data Collection Agent',
      'entities': 'Entity Research Agent',
      'document_preparation': 'Document Preparation Agent',
      'document_preparation_agent': 'Document Preparation Agent',
    };
    return agentNameMap[agentId] || agentId;
  }, [agents]);
  
  // Try to get task state from store first
  const storeTaskState = React.useMemo(() => {
    if (!taskId) return null;
    return getTaskState(taskId);
  }, [taskId, getTaskState]);
  
  // Full job data from API (includes steps, task, goal, etc.)
  const [fullJobData, setFullJobData] = React.useState<AgentJob | null>(null);
  
  // Load full job data from API (for both active and completed jobs)
  React.useEffect(() => {
    if (!isOpen || !taskId) {
      setApiJobData(null);
      setFullJobData(null);
      return;
    }
    
    // Always try to load full job data from API, even if we have storeTaskState
    // This ensures we get steps, task, goal, etc.
    const loadFullJobData = async () => {
      try {
        const currentProject = useProjectsStore.getState().currentProject;
        if (!currentProject || currentProject.id === 'local-unfold') {
          return;
        }
        
        // Try to get full job details using getAgentJob
        try {
          const result = await api.getAgentJob(currentProject.id, taskId);
          if (result.job) {
            const job = result.job as AgentJob;
            setFullJobData(job);
            
            // If we don't have storeTaskState, also set apiJobData for compatibility
            if (!storeTaskState) {
              const agentId = job.agentId || 'unknown';
              const agentName = getAgentName(agentId);
              
              setApiJobData({
                taskId: job.id,
                agentId: agentId,
                agentName: agentName,
                status: job.status === 'completed' ? 'completed' : 
                       job.status === 'failed' ? 'error' : 
                       job.status === 'running' || job.status === 'pending' ? 'running' : 'idle',
                progress: job.progress || 0,
                stage: undefined,
                message: job.userQuery || job.title || '',
                error: undefined,
                result: job.result,
                startedAt: job.createdAt ? new Date(job.createdAt).getTime() : undefined,
                completedAt: job.completedAt ? new Date(job.completedAt).getTime() : undefined,
              });
            }
          }
        } catch (getJobErr) {
          // If getAgentJob fails, try listAgentJobs as fallback
          console.warn('[AgentJobDetails] getAgentJob failed, trying listAgentJobs', getJobErr);
          const result = await api.listAgentJobs(currentProject.id, { limit: 100 });
          const job = result.jobs?.find((j: any) => (j.taskId || j.id) === taskId);
          if (job && job.steps && Array.isArray(job.steps)) {
            setFullJobData(job as AgentJob);
          }
        }
      } catch (err) {
        console.error('[AgentJobDetails] Failed to load full job data from API', err);
      }
    };
    
    loadFullJobData();
  }, [isOpen, taskId, getAgentName, storeTaskState]);
  
  // If not found in store, try to load from API (for completed/archived jobs)
  React.useEffect(() => {
    if (!isOpen || !taskId || storeTaskState) {
      if (!storeTaskState) {
        setApiJobData(null);
      }
      return;
    }
    
    // Try to load job from API if not in store
    const loadJobFromApi = async () => {
      setLoadingApiJob(true);
      try {
        const currentProject = useProjectsStore.getState().currentProject;
        if (!currentProject || currentProject.id === 'local-unfold') {
          setLoadingApiJob(false);
          return;
        }
        
        // Try to get full job details using getAgentJob
        try {
          const result = await api.getAgentJob(currentProject.id, taskId);
          if (result.job) {
            const job = result.job as AgentJob;
            setFullJobData(job);
            
            const agentId = job.agentId || 'unknown';
            const agentName = getAgentName(agentId);
            
            // Convert API job format to AgentTaskState format for compatibility
            setApiJobData({
              taskId: job.id,
              agentId: agentId,
              agentName: agentName,
              status: job.status === 'completed' ? 'completed' : 
                     job.status === 'failed' ? 'error' : 
                     job.status === 'running' || job.status === 'pending' ? 'running' : 'idle',
              progress: job.progress || 0,
              stage: undefined,
              message: job.userQuery || job.title || '',
              error: undefined,
              result: job.result,
              startedAt: job.createdAt ? new Date(job.createdAt).getTime() : undefined,
              completedAt: job.completedAt ? new Date(job.completedAt).getTime() : undefined,
            });
          }
        } catch (getJobErr) {
          // Fallback to listAgentJobs if getAgentJob fails
          console.warn('[AgentJobDetails] getAgentJob failed, trying listAgentJobs', getJobErr);
          const result = await api.listAgentJobs(currentProject.id, { limit: 100 });
          const job = result.jobs?.find((j: any) => (j.taskId || j.id) === taskId);
          if (job) {
            const agentId = job.agentId || 'unknown';
            const agentName = getAgentName(agentId);
            
            setApiJobData({
              taskId: job.id,
              agentId: agentId,
              agentName: agentName,
              status: job.status === 'completed' ? 'completed' : 
                     job.status === 'failed' ? 'error' : 
                     job.status === 'running' || job.status === 'pending' ? 'running' : 'idle',
              progress: job.progress || 0,
              stage: job.stage,
              message: job.userQuery || job.title || '',
              error: job.error,
              result: job.result,
              startedAt: job.createdAt ? new Date(job.createdAt).getTime() : undefined,
              completedAt: job.completedAt ? new Date(job.completedAt).getTime() : undefined,
            });
            
            // If job has steps, set fullJobData
            if (job.steps && Array.isArray(job.steps)) {
              setFullJobData(job as AgentJob);
            }
          }
        }
      } catch (err) {
        console.error('[AgentJobDetails] Failed to load job from API', err);
      } finally {
        setLoadingApiJob(false);
      }
    };
    
    loadJobFromApi();
  }, [isOpen, taskId, storeTaskState, getAgentName]);
  
  // Use store task state if available, otherwise use API data
  const taskState = storeTaskState || apiJobData;

  if (!isOpen) return null;

  // Show skeleton loader while loading from API
  if (!taskState && loadingApiJob) {
    return (
      <ModalWithHeader
        title="Loading Job Details..."
        onClose={onClose}
      >
        <div className="space-y-4 py-2">
          {/* Header skeleton */}
          <div className="flex items-start justify-between mb-4">
            <div className="flex-1">
              <div className="flex items-center gap-2 mb-2">
                <div 
                  className="h-5 w-32 rounded animate-pulse" 
                  style={{ backgroundColor: 'var(--white-10)' }}
                />
                <div 
                  className="h-6 w-20 rounded animate-pulse" 
                  style={{ backgroundColor: 'var(--white-10)' }}
                />
              </div>
              <div 
                className="h-4 w-48 rounded animate-pulse" 
                style={{ backgroundColor: 'var(--white-06)' }}
              />
            </div>
          </div>

          {/* Progress bar skeleton */}
          <div className="mb-4">
            <div className="flex items-center justify-between mb-1">
              <div 
                className="h-3 w-16 rounded animate-pulse" 
                style={{ backgroundColor: 'var(--white-06)' }}
              />
              <div 
                className="h-3 w-12 rounded animate-pulse" 
                style={{ backgroundColor: 'var(--white-06)' }}
              />
            </div>
            <div 
              className="h-6 w-full rounded-full animate-pulse" 
              style={{ backgroundColor: 'var(--white-06)' }}
            />
          </div>

          {/* Info sections skeleton */}
          <div className="space-y-2">
            {[1, 2, 3, 4, 5].map((i) => (
              <div key={i} className="mb-3">
                <div 
                  className="h-3 w-20 rounded animate-pulse mb-1" 
                  style={{ backgroundColor: 'var(--white-06)' }}
                />
                <div 
                  className="h-4 w-40 rounded animate-pulse" 
                  style={{ backgroundColor: 'var(--white-10)' }}
                />
              </div>
            ))}
          </div>
        </div>
      </ModalWithHeader>
    );
  }

  if (!taskState) return null;

  // Map store status to API status format
  const storeStatus = taskState.status;
  const apiStatus = fullJobData?.status;
  const jobStatus = apiStatus || (storeStatus === 'running' ? 'running' : 
                                  storeStatus === 'completed' ? 'completed' : 
                                  storeStatus === 'error' ? 'failed' : 
                                  'pending');
  const isRunning = jobStatus === 'running' || jobStatus === 'pending';
  const isPaused = jobStatus === 'paused';
  const isCompleted = jobStatus === 'completed' || jobStatus === 'failed' || jobStatus === 'partial';

  const handlePause = async () => {
    if (!taskId || !currentProject || currentProject.id === 'local-unfold') return;
    setActionLoading(true);
    try {
      await api.pauseAgentJob(currentProject.id, taskId);
      // Reload job data
      if (isOpen && taskId) {
        const result = await api.getAgentJob(currentProject.id, taskId);
        if (result.job) {
          setFullJobData(result.job as AgentJob);
        }
      }
    } catch (err) {
      console.error('[AgentJobDetails] Failed to pause job', err);
      alert('Failed to pause job');
    } finally {
      setActionLoading(false);
    }
  };

  const handleResume = async () => {
    if (!taskId || !currentProject || currentProject.id === 'local-unfold') return;
    setActionLoading(true);
    try {
      await api.resumeAgentJob(currentProject.id, taskId);
      // Reload job data
      if (isOpen && taskId) {
        const result = await api.getAgentJob(currentProject.id, taskId);
        if (result.job) {
          setFullJobData(result.job as AgentJob);
        }
      }
    } catch (err) {
      console.error('[AgentJobDetails] Failed to resume job', err);
      alert('Failed to resume job');
    } finally {
      setActionLoading(false);
    }
  };

  const handleCancel = async () => {
    if (!taskId || !currentProject || currentProject.id === 'local-unfold') return;
    if (!confirm('Are you sure you want to cancel this job?')) return;
    setActionLoading(true);
    try {
      await api.cancelAgentJob(currentProject.id, taskId);
      clearTask(taskId);
      // Reload job data
      if (isOpen && taskId) {
        const result = await api.getAgentJob(currentProject.id, taskId);
        if (result.job) {
          setFullJobData(result.job as AgentJob);
        }
      }
    } catch (err) {
      console.error('[AgentJobDetails] Failed to cancel job', err);
      alert('Failed to cancel job');
    } finally {
      setActionLoading(false);
    }
  };

  const handleDelete = async () => {
    if (!taskId || !currentProject || currentProject.id === 'local-unfold') return;
    if (!confirm('Are you sure you want to delete this job? This action cannot be undone.')) return;
    setActionLoading(true);
    try {
      await api.deleteAgentJob(currentProject.id, taskId);
      clearTask(taskId);
      onClose();
    } catch (err) {
      console.error('[AgentJobDetails] Failed to delete job', err);
      alert('Failed to delete job');
    } finally {
      setActionLoading(false);
    }
  };

  const formatTimestamp = (timestamp?: number) => {
    if (!timestamp) return undefined;
    return new Date(timestamp).toLocaleString();
  };

  const formatDuration = (startedAt?: number, completedAt?: number) => {
    if (!startedAt) return undefined;
    const end = completedAt || Date.now();
    const durationMs = end - startedAt;
    const seconds = Math.floor(durationMs / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    
    if (hours > 0) {
      return `${hours}h ${minutes % 60}m ${seconds % 60}s`;
    } else if (minutes > 0) {
      return `${minutes}m ${seconds % 60}s`;
    } else {
      return `${seconds}s`;
    }
  };

  return (
    <ModalWithHeader
      title={`${taskState.agentName} - Job Details`}
      onClose={onClose}
    >
      <div className="space-y-4 py-2">
        {/* Header section */}
        <div className="flex items-start justify-between mb-4">
          <div className="flex-1">
            <div className="flex items-center gap-2 mb-2">
              <h2 className="text-sm font-medium" style={{ color: 'var(--text-heading)' }}>
                {taskState.agentName}
              </h2>
              <StatusBadge status={taskState.status} />
            </div>
            {taskState.message && (
              <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
                {taskState.message}
              </div>
            )}
          </div>
        </div>

        {/* Action buttons */}
        <div className="flex gap-2 mb-4">
          {isRunning && (
            <button
              onClick={handlePause}
              disabled={actionLoading}
              className="px-3 py-1.5 text-xs rounded"
              style={{
                backgroundColor: 'var(--accent-brand)',
                color: 'white',
                border: 'none',
                cursor: actionLoading ? 'not-allowed' : 'pointer',
                opacity: actionLoading ? 0.6 : 1,
              }}
            >
              {actionLoading ? '...' : 'Pause'}
            </button>
          )}
          {isPaused && (
            <button
              onClick={handleResume}
              disabled={actionLoading}
              className="px-3 py-1.5 text-xs rounded"
              style={{
                backgroundColor: 'var(--accent-success)',
                color: 'white',
                border: 'none',
                cursor: actionLoading ? 'not-allowed' : 'pointer',
                opacity: actionLoading ? 0.6 : 1,
              }}
            >
              {actionLoading ? '...' : 'Resume'}
            </button>
          )}
          {(isRunning || isPaused) && (
            <button
              onClick={handleCancel}
              disabled={actionLoading}
              className="px-3 py-1.5 text-xs rounded"
              style={{
                backgroundColor: '#f59e0b',
                color: 'white',
                border: 'none',
                cursor: actionLoading ? 'not-allowed' : 'pointer',
                opacity: actionLoading ? 0.6 : 1,
              }}
            >
              {actionLoading ? '...' : 'Cancel'}
            </button>
          )}
          <button
            onClick={handleDelete}
            disabled={actionLoading}
            className="px-3 py-1.5 text-xs rounded ml-auto"
            style={{
              backgroundColor: '#f87171',
              color: 'white',
              border: 'none',
              cursor: actionLoading ? 'not-allowed' : 'pointer',
              opacity: actionLoading ? 0.6 : 1,
            }}
          >
            {actionLoading ? '...' : 'Delete'}
          </button>
        </div>

        {/* Progress bar */}
        {taskState.status === 'running' && (
          <div className="mb-4">
            <ProgressBar progress={taskState.progress} />
          </div>
        )}

        {/* Job information */}
        <div className="space-y-2">
          <JobInfoSection label="Task ID" value={taskState.taskId} />
          <JobInfoSection label="Agent ID" value={taskState.agentId} />
          {taskState.stage && (
            <JobInfoSection label="Stage" value={taskState.stage} />
          )}
          <JobInfoSection label="Started At" value={formatTimestamp(taskState.startedAt)} />
          {taskState.completedAt && (
            <JobInfoSection label="Completed At" value={formatTimestamp(taskState.completedAt)} />
          )}
          {taskState.startedAt && (
            <JobInfoSection 
              label="Duration" 
              value={formatDuration(taskState.startedAt, taskState.completedAt)} 
            />
          )}
        </div>

        {/* Error section */}
        {taskState.error && (
          <ErrorSection error={taskState.error} />
        )}

        {/* User Query */}
        {fullJobData?.userQuery && (
          <div className="mt-4">
            <div className="text-xs font-medium mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
              User Query
            </div>
            <div
              className="rounded-lg p-3 text-sm"
              style={{
                backgroundColor: 'var(--white-06)',
                color: 'var(--text-heading)',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
              }}
            >
              {fullJobData.userQuery}
            </div>
          </div>
        )}

        {/* Task */}
        {fullJobData?.task && (
          <JSONSection title="Task" data={fullJobData.task} />
        )}

        {/* Goal */}
        {fullJobData?.goal && (
          <JSONSection title="Goal" data={fullJobData.goal} />
        )}

        {/* Intent */}
        {fullJobData?.intent && (
          <JSONSection title="Intent" data={fullJobData.intent} />
        )}

        {/* Steps */}
        {fullJobData?.steps && fullJobData.steps.length > 0 && (
          <StepsSection steps={fullJobData.steps} />
        )}

        {/* Result section */}
        {taskState.result && (
          <ResultSection result={taskState.result} />
        )}

        {/* Empty state for completed tasks without result */}
        {taskState.status === 'completed' && !taskState.result && !taskState.error && (
          <div className="text-xs text-center py-4" style={{ color: 'var(--text-tertiary-weak)' }}>
            Task completed successfully
          </div>
        )}
      </div>
    </ModalWithHeader>
  );
};

