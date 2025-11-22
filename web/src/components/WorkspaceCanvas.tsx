/**
 * Компонент Workspace Canvas
 * Показывает документы всех чатов, сгруппированные по проектам
 */

import React from 'react';
import { useTranslation } from 'react-i18next';
import { ModalWithHeader } from './ModalWithHeader';
import { useProjectsStore } from '../stores/projectsStore';
import { useWorkspaceStore } from '../stores/workspaceStore';
import { api } from '../lib/api';
import type { CoreDoc } from '../types/generated';
import type { Project } from '../types';

export interface WorkspaceCanvasProps {
  isOpen: boolean;
  onClose: () => void;
  onOpenDocument?: (doc: { id: string; filename: string; contentType?: string; sizeBytes?: number; parseStatus?: string; docType?: string; artifactConfig?: any }) => void;
}

// Мемоизированный компонент документа
const DocumentItem = React.memo<{
  doc: CoreDoc;
  indexedStatus: { status: 'indexed' | 'not_indexed' | 'unknown' | 'loading'; text: string };
  onOpenDocument?: (doc: { id: string; filename: string; contentType?: string; sizeBytes?: number; parseStatus?: string; docType?: string; artifactConfig?: any }) => void;
}>(({ doc, indexedStatus, onOpenDocument }) => {
  const getDocumentIcon = () => (
    <svg className="w-3 h-3" viewBox="0 0 24 24" fill="none" stroke="currentColor">
      <path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M14 2v6h6M16 13H8M16 17H8M10 9H8" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );

  return (
    <div
      className="px-2 py-1.5 rounded text-xs flex items-start gap-2 group cursor-pointer"
      style={{
        backgroundColor: 'transparent',
      }}
      onMouseEnter={(e) => {
        (e.currentTarget as HTMLDivElement).style.backgroundColor =
          'var(--white-10)';
      }}
      onMouseLeave={(e) => {
        (e.currentTarget as HTMLDivElement).style.backgroundColor =
          'transparent';
      }}
      onClick={() => {
        if (onOpenDocument) {
          onOpenDocument({
            id: doc.id,
            filename: doc.filename,
            contentType: doc.contentType,
            sizeBytes: doc.sizeBytes,
            parseStatus: doc.parseStatus,
            docType: doc.docType,
            artifactConfig: doc.artifactConfig,
          });
        }
      }}
    >
      <div className="flex-shrink-0 mt-0.5" style={{ color: 'var(--text-secondary)' }}>
        {getDocumentIcon()}
      </div>
      <div className="flex-1 min-w-0">
        <div
          className="font-medium truncate"
          style={{ color: 'var(--text-heading)' }}
          title={doc.filename}
        >
          {doc.filename}
        </div>
        <div className="flex items-center gap-2 mt-1">
          <span
            className="text-[10px] px-1.5 py-0.5 rounded"
            style={{
              color: indexedStatus.status === 'indexed' 
                ? 'var(--accent-primary)' 
                : indexedStatus.status === 'loading'
                ? 'var(--text-secondary-strong)'
                : 'var(--text-tertiary-weak)',
              backgroundColor: indexedStatus.status === 'indexed'
                ? 'var(--white-10)'
                : indexedStatus.status === 'loading'
                ? 'var(--white-06)'
                : 'transparent',
            }}
          >
            {indexedStatus.text}
          </span>
          {doc.sizeBytes && (
            <span
              className="text-[10px]"
              style={{ color: 'var(--text-tertiary-weak)' }}
            >
              {doc.sizeBytes < 1024 
                ? `${doc.sizeBytes}B` 
                : doc.sizeBytes < 1024 * 1024
                ? `${(doc.sizeBytes / 1024).toFixed(1)}KB`
                : `${(doc.sizeBytes / (1024 * 1024)).toFixed(1)}MB`}
            </span>
          )}
        </div>
      </div>
    </div>
  );
});
DocumentItem.displayName = 'DocumentItem';

// Мемоизированный компонент проекта
const ProjectSection = React.memo<{
  project: Project;
  documents: CoreDoc[];
  discussionFrame?: string;
  indexStatuses: Record<string, { indexed: boolean; loading: boolean; lastChecked: number }>;
  getIndexedStatus: (doc: CoreDoc) => { status: 'indexed' | 'not_indexed' | 'unknown' | 'loading'; text: string };
  onOpenDocument?: (doc: { id: string; filename: string; contentType?: string; sizeBytes?: number; parseStatus?: string; docType?: string; artifactConfig?: any }) => void;
}>(({ project, documents, discussionFrame, getIndexedStatus, onOpenDocument }) => {
  return (
    <div className="mb-6">
      {/* Project header */}
      <div className="mb-3">
        <div
          className="text-13 font-medium mb-1"
          style={{ color: 'var(--text-heading)' }}
        >
          {project.displayName}
        </div>
        {discussionFrame && (
          <div
            className="text-xs"
            style={{ 
              color: 'var(--text-secondary-strong)',
              lineHeight: '1.5',
              wordBreak: 'normal',
              overflowWrap: 'normal',
            }}
          >
            {discussionFrame}
          </div>
        )}
      </div>

      {/* Documents list */}
      {documents.length > 0 ? (
        <div 
          className="grid gap-1"
          style={{
            gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))',
          }}
        >
          {documents.map((doc) => (
            <DocumentItem
              key={doc.id}
              doc={doc}
              indexedStatus={getIndexedStatus(doc)}
              onOpenDocument={onOpenDocument}
            />
          ))}
        </div>
      ) : (
        <div
          className="text-xs px-2 py-1"
          style={{ color: 'var(--text-tertiary-weak)' }}
        >
          No documents
        </div>
      )}
    </div>
  );
});
ProjectSection.displayName = 'ProjectSection';

export const WorkspaceCanvas: React.FC<WorkspaceCanvasProps> = ({
  isOpen,
  onClose,
  onOpenDocument,
}) => {
  const { t } = useTranslation();
  // Оптимизация: подписываемся только на длину массива проектов, не на весь массив
  const projectsLength = useProjectsStore((s) => s.projects?.length || 0);
  const projectsIds = useProjectsStore((s) => s.projects?.map((p) => p.id).join(',') || '');
  // Получаем проекты только когда нужно через getState()
  const getProjects = React.useCallback(() => useProjectsStore.getState().projects || [], []);
  
  // Оптимизация: подписываемся только на длину и ключи, не на весь массив
  const projectsWithDocsLength = useWorkspaceStore((s) => s.projectsWithDocs.length);
  const projectsWithDocsKeys = useWorkspaceStore((s) => s.projectsWithDocs.map((p) => `${p.project.id}:${p.lastUpdated}`).join(','));
  const loading = useWorkspaceStore((s) => s.loading);
  const setLoading = useWorkspaceStore((s) => s.setLoading);
  const updateProjectDocs = useWorkspaceStore((s) => s.updateProjectDocs);
  
  // Получаем данные только когда нужно
  const getProjectsWithDocs = React.useCallback(() => useWorkspaceStore.getState().projectsWithDocs, []);
  
  // Мемоизируем текущие данные
  const [currentProjectsWithDocs, setCurrentProjectsWithDocs] = React.useState(() => getProjectsWithDocs());
  
  // Обновляем только при реальных изменениях
  React.useEffect(() => {
    const newData = getProjectsWithDocs();
    const currentKeys = currentProjectsWithDocs.map((p) => `${p.project.id}:${p.lastUpdated}`).join(',');
    const newKeys = newData.map((p) => `${p.project.id}:${p.lastUpdated}`).join(',');
    if (currentKeys !== newKeys) {
      setCurrentProjectsWithDocs(newData);
    }
  }, [projectsWithDocsKeys, getProjectsWithDocs]);

  // Load documents for all projects - show cached data immediately, update in background
  React.useEffect(() => {
    if (!isOpen) return;

    // Show cached data immediately - data is already in store from previous opens
    const hasCachedData = currentProjectsWithDocs.length > 0;

    const loadAllProjectsData = async () => {
      // Only show loading if we don't have cached data
      if (!hasCachedData) {
        setLoading(true);
      }

      try {
        const projects = getProjects() || [];
        // Load data for all projects in parallel
        const loadPromises = projects
          .filter((p) => p && p.id !== 'local-unfold')
          .map(async (project) => {
            try {
              // Load documents and interactions in parallel
              const [docs, interactions] = await Promise.all([
                api.docsList(project.id).catch(() => [] as CoreDoc[]),
                api.getProjectInteractions(project.id).catch(() => [] as any[]),
              ]);
              
              // Extract Discussion Frame
              let discussionFrame: string | undefined;
              const analysisInteraction = interactions.find(
                (it: any) => it.subkind === 'discussion.flow.analysis'
              );
              
              if (analysisInteraction?.content) {
                try {
                  const data = JSON.parse(analysisInteraction.content);
                  if (data.frame && typeof data.frame === 'string') {
                    discussionFrame = data.frame;
                  }
                } catch {
                  // Ignore parse errors
                }
              }

              // Update store incrementally for each project as data arrives
              if (docs.length > 0 || discussionFrame) {
                updateProjectDocs(project, docs, discussionFrame);
              }
            } catch (err) {
              console.error(`[WorkspaceCanvas] Failed to load data for project ${project.id}`, err);
            }
          });

        await Promise.all(loadPromises);
      } catch (err) {
        console.error('[WorkspaceCanvas] Failed to load projects data', err);
      } finally {
        setLoading(false);
      }
    };

    // Load in background - cached data is already shown
    loadAllProjectsData();
  }, [isOpen, projectsLength, projectsIds, setLoading, updateProjectDocs, getProjects, currentProjectsWithDocs.length]);

  // Оптимизация: подписываемся только на ключи статусов, не на весь объект
  const indexStatusesKeys = useWorkspaceStore((s) => Object.keys(s.indexStatuses).join(','));
  const setDocumentIndexStatus = useWorkspaceStore((s) => s.setDocumentIndexStatus);
  
  // Получаем статусы только когда нужно
  const getIndexStatuses = React.useCallback(() => useWorkspaceStore.getState().indexStatuses, []);
  
  // Мемоизируем текущие статусы
  const [currentIndexStatuses, setCurrentIndexStatuses] = React.useState(() => getIndexStatuses());
  
  // Обновляем статусы только при реальных изменениях
  React.useEffect(() => {
    const newStatuses = getIndexStatuses();
    setCurrentIndexStatuses((prevStatuses) => {
      const currentKeys = Object.keys(prevStatuses).join(',');
      const newKeys = Object.keys(newStatuses).join(',');
      if (currentKeys !== newKeys) {
        return newStatuses;
      }
      // Проверяем изменения в значениях
      const hasChanges = Object.keys(newStatuses).some(
        (key) => prevStatuses[key]?.indexed !== newStatuses[key]?.indexed ||
                 prevStatuses[key]?.loading !== newStatuses[key]?.loading
      );
      return hasChanges ? newStatuses : prevStatuses;
    });
  }, [indexStatusesKeys, getIndexStatuses]);

  // Check index status for documents - update in background, use cached if available
  React.useEffect(() => {
    if (!isOpen || currentProjectsWithDocs.length === 0) return;

    const checkIndexStatuses = async () => {
      const indexStatuses = getIndexStatuses();
      // Collect all documents that need status check
      const docsToCheck: Array<{ projectId: string; docId: string }> = [];
      
      for (const { project, documents } of currentProjectsWithDocs) {
        for (const doc of documents) {
          if (!doc.storagePath || doc.id === 'local-unfold') continue;
          
          const existingStatus = indexStatuses[doc.id];
          // Check if we need to refresh (no status or status is older than 5 minutes)
          const needsCheck = !existingStatus || 
            (Date.now() - existingStatus.lastChecked > 5 * 60 * 1000);
          
          if (needsCheck) {
            docsToCheck.push({ projectId: project.id, docId: doc.id });
            // Set loading state immediately
            setDocumentIndexStatus(doc.id, false, true);
          }
        }
      }

      // Check statuses in parallel
      const checkPromises = docsToCheck.map(async ({ projectId, docId }) => {
        try {
          const status = await api.docsCheckIndexStatus(projectId, docId);
          setDocumentIndexStatus(docId, status.indexed, false);
        } catch (err) {
          console.error(`[WorkspaceCanvas] Failed to check index status for ${docId}`, err);
          setDocumentIndexStatus(docId, false, false);
        }
      });

      await Promise.all(checkPromises);
    };

    checkIndexStatuses();
  }, [isOpen, currentProjectsWithDocs, indexStatusesKeys, setDocumentIndexStatus, getIndexStatuses]);

  const getIndexedStatus = React.useCallback((doc: CoreDoc): { status: 'indexed' | 'not_indexed' | 'unknown' | 'loading'; text: string } => {
    if (!doc.storagePath) {
      return { status: 'not_indexed', text: 'Not indexed' };
    }
    
    const status = currentIndexStatuses[doc.id];
    if (!status) {
      return { status: 'unknown', text: 'Unknown' };
    }
    
    if (status.loading) {
      return { status: 'loading', text: 'Checking...' };
    }
    
    return status.indexed 
      ? { status: 'indexed', text: 'Indexed' }
      : { status: 'not_indexed', text: 'Not indexed' };
  }, [currentIndexStatuses]);

  const getProjectSortTimestamp = React.useCallback((project: Project, fallback: number) => {
    const parseDate = (value?: string) => (value ? Date.parse(value) : NaN);
    const created = parseDate(project.createdAt);
    const updated = parseDate(project.updatedAt);
    const lastInteraction = parseDate(project.lastInteractionAt);

    if (!Number.isNaN(created)) return created;
    if (!Number.isNaN(updated)) return updated;
    if (!Number.isNaN(lastInteraction)) return lastInteraction;
    return fallback;
  }, []);

  // Memoize projects list to avoid unnecessary re-renders (newest -> oldest)
  const sortedProjectsWithDocs = React.useMemo(() => {
    return [...currentProjectsWithDocs].sort((a, b) => {
      const fallbackA = a.lastUpdated || 0;
      const fallbackB = b.lastUpdated || 0;
      const tsA = getProjectSortTimestamp(a.project, fallbackA);
      const tsB = getProjectSortTimestamp(b.project, fallbackB);

      if (tsA !== tsB) {
        return tsB - tsA; // Newest first
      }
      return a.project.displayName.localeCompare(b.project.displayName);
    });
  }, [currentProjectsWithDocs, getProjectSortTimestamp]);

  if (!isOpen) return null;

  return (
    <ModalWithHeader
      title={t('unfold.sections.workspace')}
      onClose={onClose}
    >
      <div className="space-y-6 py-2">
        {loading && currentProjectsWithDocs.length === 0 ? (
          <div className="flex items-center justify-center py-8">
            <div className="animate-pulse text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
              Loading workspace...
            </div>
          </div>
        ) : currentProjectsWithDocs.length === 0 ? (
          <div className="flex items-center justify-center py-8">
            <div className="text-xs text-center" style={{ color: 'var(--text-secondary-strong)' }}>
              No documents found in any chats
            </div>
          </div>
        ) : (
          sortedProjectsWithDocs.map(({ project, documents, discussionFrame }) => (
            <ProjectSection
              key={project.id}
              project={project}
              documents={documents}
              discussionFrame={discussionFrame}
              indexStatuses={currentIndexStatuses}
              getIndexedStatus={getIndexedStatus}
              onOpenDocument={onOpenDocument}
            />
          ))
        )}
      </div>
    </ModalWithHeader>
  );
};

