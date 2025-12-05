/**
 * Компонент Workspace Canvas
 * Показывает документы всех чатов, сгруппированные по проектам
 */

import React from 'react';
import { useTranslation } from 'react-i18next';
import { ModalWithHeader } from './ModalWithHeader';
import { useProjectsStore } from '../stores/projectsStore';
import { useWorkspaceStore } from '../stores/workspaceStore';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { TelegramAddModal } from './TelegramAddModal';
import { RSSAddModal } from './RSSAddModal';
import { WorkspaceToolbar } from './WorkspaceToolbar';
import { SourceItemWithActions } from './SourceItemWithActions';
import SourcePreview from './SourcePreview';
import { SourceDocumentsViewer } from './SourceDocumentsViewer';
import { api } from '../lib/api';
import type { CoreDoc, DataSource } from '../types/generated';
import type { Project } from '../types';

export interface WorkspaceCanvasProps {
  isOpen: boolean;
  projectId?: string | null; // Optional: filter by project, null means all projects
  onClose: () => void;
  onOpenDocument?: (doc: { id: string; filename: string; contentType?: string; sizeBytes?: number; parseStatus?: string; docType?: string; artifactConfig?: any; extractedText?: string; projectId?: string }) => void;
}

// Мемоизированный компонент документа
const DocumentItem = React.memo<{
  doc: CoreDoc;
  indexedStatus: { status: 'indexed' | 'not_indexed' | 'unknown' | 'loading'; text: string };
  bigqueryStatus: { status: 'available' | 'not_available' | 'unknown' | 'loading'; text: string };
  vertexSearchAvailabilityStatus: { status: 'available' | 'not_available' | 'unknown' | 'loading'; text: string };
  onOpenDocument?: (doc: { id: string; filename: string; contentType?: string; sizeBytes?: number; parseStatus?: string; docType?: string; artifactConfig?: any; extractedText?: string; projectId?: string }) => void;
}>(({ doc, indexedStatus, bigqueryStatus, vertexSearchAvailabilityStatus, onOpenDocument }) => {
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
        maxWidth: 'fit-content',
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
            extractedText: doc.extractedText,
            projectId: doc.projectId,
          });
        }
      }}
    >
      <div className="flex-shrink-0 mt-0.5" style={{ color: 'var(--text-secondary)' }}>
        {getDocumentIcon()}
      </div>
      <div className="min-w-0" style={{ maxWidth: '300px' }}>
        <div
          className="font-medium truncate"
          style={{ color: 'var(--text-heading)' }}
          title={doc.filename}
        >
          {doc.filename && doc.filename.length > 50 
            ? `${doc.filename.substring(0, 47)}...` 
            : doc.filename}
        </div>
        <div className="flex items-center gap-1.5 mt-1 flex-wrap">
          {/* Indexed status - I */}
          {(indexedStatus.status === 'indexed' || indexedStatus.status === 'not_indexed' || indexedStatus.status === 'loading') && (
            <span
              className="text-[10px] px-1.5 py-0.5 rounded font-medium"
              title={indexedStatus.text}
              style={{
                color: indexedStatus.status === 'indexed' 
                  ? 'var(--accent-success)' 
                  : indexedStatus.status === 'not_indexed'
                  ? 'rgba(255, 169, 77, 1)'
                  : 'var(--text-secondary-strong)',
                backgroundColor: indexedStatus.status === 'indexed'
                  ? 'var(--success-20)'
                  : indexedStatus.status === 'not_indexed'
                  ? 'rgba(255, 169, 77, 0.20)'
                  : 'var(--white-06)',
              }}
            >
              I
            </span>
          )}
          {/* Analytics available - A */}
          {(bigqueryStatus.status === 'available' || bigqueryStatus.status === 'not_available' || bigqueryStatus.status === 'loading') && (
            <span
              className="text-[10px] px-1.5 py-0.5 rounded font-medium"
              title={bigqueryStatus.text}
              style={{
                color: bigqueryStatus.status === 'available' 
                  ? 'var(--accent-success)' 
                  : bigqueryStatus.status === 'not_available'
                  ? 'rgba(255, 169, 77, 1)'
                  : 'var(--text-secondary-strong)',
                backgroundColor: bigqueryStatus.status === 'available'
                  ? 'var(--success-20)'
                  : bigqueryStatus.status === 'not_available'
                  ? 'rgba(255, 169, 77, 0.20)'
                  : 'var(--white-06)',
              }}
            >
              A
            </span>
          )}
          {/* Vertex AI Search - S */}
          {(vertexSearchAvailabilityStatus.status === 'available' || vertexSearchAvailabilityStatus.status === 'not_available' || vertexSearchAvailabilityStatus.status === 'loading') && (
            <span
              className="text-[10px] px-1.5 py-0.5 rounded font-medium"
              title={vertexSearchAvailabilityStatus.text}
              style={{
                color: vertexSearchAvailabilityStatus.status === 'available' 
                  ? 'var(--accent-success)' 
                  : vertexSearchAvailabilityStatus.status === 'not_available'
                  ? 'rgba(255, 169, 77, 1)'
                  : 'var(--text-secondary-strong)',
                backgroundColor: vertexSearchAvailabilityStatus.status === 'available'
                  ? 'var(--success-20)'
                  : vertexSearchAvailabilityStatus.status === 'not_available'
                  ? 'rgba(255, 169, 77, 0.20)'
                  : 'var(--white-06)',
              }}
            >
              S
            </span>
          )}
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
  sources?: DataSource[];
  discussionFrame?: string;
  indexStatuses: Record<string, { indexed: boolean; loading: boolean; lastChecked: number }>;
  getIndexedStatus: (doc: CoreDoc) => { status: 'indexed' | 'not_indexed' | 'unknown' | 'loading'; text: string };
  getBigQueryStatus: (doc: CoreDoc) => { status: 'available' | 'not_available' | 'unknown' | 'loading'; text: string };
  getVertexSearchAvailabilityStatus: (doc: CoreDoc) => { status: 'available' | 'not_available' | 'unknown' | 'loading'; text: string };
  onOpenDocument?: (doc: { id: string; filename: string; contentType?: string; sizeBytes?: number; parseStatus?: string; docType?: string; artifactConfig?: any; projectId?: string }) => void;
  onSourceStatusChange?: (sourceId: string, newStatus: 'active' | 'paused') => void;
  onSourceDelete?: (sourceId: string) => void;
  onSourcePreview?: (source: DataSource) => void;
  onViewDocuments?: (source: DataSource) => void;
}>(({ project, documents, sources, discussionFrame, getIndexedStatus, getBigQueryStatus, getVertexSearchAvailabilityStatus, onOpenDocument, onSourceStatusChange, onSourceDelete, onSourcePreview, onViewDocuments }) => {
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

      {/* Sources section */}
      {sources && sources.length > 0 && (
        <div className="mb-4">
          <h5 className="text-xs font-medium mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
            Sources ({sources.length})
          </h5>
          <div 
            className="grid gap-1"
            style={{
              gridTemplateColumns: 'repeat(auto-fill, minmax(200px, max-content))',
            }}
          >
            {sources.map((source) => (
              <SourceItemWithActions
                key={source.id}
                source={{
                  id: source.id,
                  displayName: source.displayName || source.kind || 'Unknown',
                  kind: source.kind,
                  status: source.status,
                  lastPolledAt: source.lastPolledAt,
                  nextPollAt: source.nextPollAt,
                }}
                projectId={project.id}
                onStatusChange={onSourceStatusChange}
                onDelete={onSourceDelete}
                onPreview={() => onSourcePreview?.(source)}
                onViewDocuments={(sourceId) => {
                  // Find source in current sources list
                  const source = sources?.find((s) => s.id === sourceId);
                  if (source && onViewDocuments) {
                    onViewDocuments(source);
                  }
                }}
              />
            ))}
          </div>
        </div>
      )}

      {/* Documents list */}
      {documents.length > 0 ? (
        <div 
          className="grid gap-1"
          style={{
            gridTemplateColumns: 'repeat(auto-fill, minmax(200px, max-content))',
          }}
        >
          {documents.map((doc) => (
            <DocumentItem
              key={doc.id}
              doc={doc}
              indexedStatus={getIndexedStatus(doc)}
              bigqueryStatus={getBigQueryStatus(doc)}
              vertexSearchAvailabilityStatus={getVertexSearchAvailabilityStatus(doc)}
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
  projectId,
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
  
  // State for project sources - sync with workspace store
  const storeProjectSources = useWorkspaceStore((s) => s.projectSources);
  const setProjectSourcesStore = useWorkspaceStore((s) => s.setProjectSources);
  const updateSourceStatus = useWorkspaceStore((s) => s.updateSourceStatus);
  const sourceStatusUpdates = useWorkspaceStore((s) => s.sourceStatusUpdates);
  const [projectSources, setProjectSources] = React.useState<Record<string, DataSource[]>>({});
  
  // State for source preview
  const [previewSource, setPreviewSource] = React.useState<DataSource | null>(null);
  const [documentsViewSource, setDocumentsViewSource] = React.useState<DataSource | null>(null);
  
  // Sync local state with store
  React.useEffect(() => {
    setProjectSources(storeProjectSources);
  }, [storeProjectSources, sourceStatusUpdates]);
  
  // State for Telegram modal (only when projectId is provided)
  const [showAddTelegramModal, setShowAddTelegramModal] = React.useState(false);
  const [tgInput, setTgInput] = React.useState('');
  const [tgSubmitting, setTgSubmitting] = React.useState(false);
  const [tgError, setTgError] = React.useState<string | null>(null);

  // State for RSS modal
  const [showAddRSSModal, setShowAddRSSModal] = React.useState(false);
  const [rssInput, setRssInput] = React.useState('');
  const [rssSubmitting, setRssSubmitting] = React.useState(false);
  const [rssError, setRssError] = React.useState<string | null>(null);
  
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
        // Filter by projectId if specified
        const projectsToLoad = projectId
          ? projects.filter((p) => p && p.id === projectId)
          : projects.filter((p) => p && p.id !== 'local-unfold');
        
        // Load data for projects in batches to avoid ERR_INSUFFICIENT_RESOURCES
        // Process 5 projects at a time to limit concurrent HTTP requests
        const BATCH_SIZE = 5;
        for (let i = 0; i < projectsToLoad.length; i += BATCH_SIZE) {
          const batch = projectsToLoad.slice(i, i + BATCH_SIZE);
          const loadPromises = batch.map(async (project) => {
            try {
              // Load documents, interactions, and sources in parallel
              const [docs, interactions, sources] = await Promise.all([
                api.docsList(project.id).catch(() => [] as CoreDoc[]),
                api.getProjectInteractions(project.id).catch(() => [] as any[]),
                api.getProjectSources(project.id).catch(() => [] as DataSource[]),
              ]);
              
              // Store sources for this project
              // Ensure sources is an array (handle null/undefined cases)
              const sourcesArray = Array.isArray(sources) ? sources : [];
              if (sourcesArray.length > 0) {
                // Update both local state and store for synchronization
                setProjectSources((prev) => ({
                  ...prev,
                  [project.id]: sourcesArray,
                }));
                setProjectSourcesStore(project.id, sourcesArray);
              }
              
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

          // Wait for current batch to complete before starting next batch
          await Promise.all(loadPromises);
        }
      } catch (err) {
        console.error('[WorkspaceCanvas] Failed to load projects data', err);
      } finally {
        setLoading(false);
      }
    };

    // Load in background - cached data is already shown
    loadAllProjectsData();
  }, [isOpen, projectId, projectsLength, projectsIds, setLoading, updateProjectDocs, getProjects, currentProjectsWithDocs.length]);

  // Оптимизация: подписываемся только на ключи статусов, не на весь объект
  const indexStatusesKeys = useWorkspaceStore((s) => Object.keys(s.indexStatuses).join(','));
  const setDocumentIndexStatus = useWorkspaceStore((s) => s.setDocumentIndexStatus);
  
  // Получаем статусы только когда нужно
  const getIndexStatuses = React.useCallback(() => useWorkspaceStore.getState().indexStatuses, []);
  
  // Мемоизируем текущие статусы
  const [currentIndexStatuses, setCurrentIndexStatuses] = React.useState(() => getIndexStatuses());

  // BigQuery statuses
  const bigqueryStatusesKeys = useWorkspaceStore((s) => Object.keys(s.bigqueryStatuses).join(','));
  const setDocumentBigQueryStatus = useWorkspaceStore((s) => s.setDocumentBigQueryStatus);
  
  const getBigQueryStatuses = React.useCallback(() => useWorkspaceStore.getState().bigqueryStatuses, []);
  
  const [currentBigQueryStatuses, setCurrentBigQueryStatuses] = React.useState(() => getBigQueryStatuses());

  // Vertex Search Availability statuses
  const vertexSearchAvailabilityStatusesKeys = useWorkspaceStore((s) => Object.keys(s.vertexSearchAvailabilityStatuses).join(','));
  const setDocumentVertexSearchAvailabilityStatus = useWorkspaceStore((s) => s.setDocumentVertexSearchAvailabilityStatus);
  
  const getVertexSearchAvailabilityStatuses = React.useCallback(() => useWorkspaceStore.getState().vertexSearchAvailabilityStatuses, []);
  
  const [currentVertexSearchAvailabilityStatuses, setCurrentVertexSearchAvailabilityStatuses] = React.useState(() => getVertexSearchAvailabilityStatuses());
  
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

  // Обновляем BigQuery статусы только при реальных изменениях
  React.useEffect(() => {
    const newStatuses = getBigQueryStatuses();
    setCurrentBigQueryStatuses((prevStatuses) => {
      const currentKeys = Object.keys(prevStatuses).join(',');
      const newKeys = Object.keys(newStatuses).join(',');
      if (currentKeys !== newKeys) {
        return newStatuses;
      }
      // Проверяем изменения в значениях
      const hasChanges = Object.keys(newStatuses).some(
        (key) => prevStatuses[key]?.available !== newStatuses[key]?.available ||
                 prevStatuses[key]?.loading !== newStatuses[key]?.loading
      );
      return hasChanges ? newStatuses : prevStatuses;
    });
  }, [bigqueryStatusesKeys, getBigQueryStatuses]);

  // Обновляем Vertex Search Availability статусы только при реальных изменениях
  React.useEffect(() => {
    const newStatuses = getVertexSearchAvailabilityStatuses();
    setCurrentVertexSearchAvailabilityStatuses((prevStatuses) => {
      const currentKeys = Object.keys(prevStatuses).join(',');
      const newKeys = Object.keys(newStatuses).join(',');
      if (currentKeys !== newKeys) {
        return newStatuses;
      }
      // Проверяем изменения в значениях
      const hasChanges = Object.keys(newStatuses).some(
        (key) => prevStatuses[key]?.available !== newStatuses[key]?.available ||
                 prevStatuses[key]?.loading !== newStatuses[key]?.loading
      );
      return hasChanges ? newStatuses : prevStatuses;
    });
  }, [vertexSearchAvailabilityStatusesKeys, getVertexSearchAvailabilityStatuses]);

  // Check index status for documents - update in background, use cached if available
  React.useEffect(() => {
    if (!isOpen || currentProjectsWithDocs.length === 0) return;

    const checkIndexStatuses = async () => {
      const indexStatuses = getIndexStatuses();
      // Collect all documents that need status check
      const docsToCheck: Array<{ projectId: string; docId: string }> = [];
      
      let totalDocs = 0;
      let docsWithoutStoragePath = 0;
      
      for (const { project, documents } of currentProjectsWithDocs) {
        for (const doc of documents) {
          totalDocs++;
          if (doc.id === 'local-unfold') {
            continue;
          }
          if (!doc.storagePath) docsWithoutStoragePath++;
          
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

      console.log(`[WorkspaceCanvas] Index status check: total docs=${totalDocs}, without storagePath=${docsWithoutStoragePath}, to check=${docsToCheck.length}`);

      // Check statuses in batches to avoid ERR_INSUFFICIENT_RESOURCES
      // Process 10 checks at a time
      const CHECK_BATCH_SIZE = 10;
      for (let i = 0; i < docsToCheck.length; i += CHECK_BATCH_SIZE) {
        const batch = docsToCheck.slice(i, i + CHECK_BATCH_SIZE);
        const checkPromises = batch.map(async ({ projectId, docId }) => {
          try {
            const status = await api.docsCheckIndexStatus(projectId, docId);
            console.log(`[WorkspaceCanvas] API response for ${docId}:`, status);
            setDocumentIndexStatus(docId, status.indexed, false);
            // Log reason if document is not indexed
            if (!status.indexed) {
              console.log(`[WorkspaceCanvas] Document ${docId} not indexed: reason=${status.reason || 'unknown'}${status.error ? `, error=${status.error}` : ''}`);
            } else {
              console.log(`[WorkspaceCanvas] Document ${docId} is indexed`);
            }
          } catch (err) {
            console.error(`[WorkspaceCanvas] Failed to check index status for ${docId}`, err);
            setDocumentIndexStatus(docId, false, false);
          }
        });

        await Promise.all(checkPromises);
      }
    };

    checkIndexStatuses();
  }, [isOpen, currentProjectsWithDocs, indexStatusesKeys, setDocumentIndexStatus, getIndexStatuses]);

  // Check BigQuery status for documents - update in background, use cached if available
  React.useEffect(() => {
    if (!isOpen || currentProjectsWithDocs.length === 0) return;

    const checkBigQueryStatuses = async () => {
      const bigqueryStatuses = getBigQueryStatuses();
      // Collect all documents that need status check
      const docsToCheck: Array<{ projectId: string; docId: string }> = [];
      
      for (const { project, documents } of currentProjectsWithDocs) {
        for (const doc of documents) {
          if (doc.id === 'local-unfold') {
            continue;
          }
          
          const existingStatus = bigqueryStatuses[doc.id];
          // Check if we need to refresh (no status or status is older than 5 minutes)
          const needsCheck = !existingStatus || 
            (Date.now() - existingStatus.lastChecked > 5 * 60 * 1000);
          
          if (needsCheck) {
            docsToCheck.push({ projectId: project.id, docId: doc.id });
            // Set loading state immediately
            setDocumentBigQueryStatus(doc.id, false, true);
          }
        }
      }

      console.log(`[WorkspaceCanvas] BigQuery status check: to check=${docsToCheck.length}`);

      // Check statuses in batches to avoid ERR_INSUFFICIENT_RESOURCES
      // Process 10 checks at a time
      const CHECK_BATCH_SIZE = 10;
      for (let i = 0; i < docsToCheck.length; i += CHECK_BATCH_SIZE) {
        const batch = docsToCheck.slice(i, i + CHECK_BATCH_SIZE);
        const checkPromises = batch.map(async ({ projectId, docId }) => {
          try {
            const status = await api.docsCheckBigQueryStatus(projectId, docId);
            console.log(`[WorkspaceCanvas] BigQuery API response for ${docId}:`, status);
            setDocumentBigQueryStatus(docId, status.available, false);
            // Log reason if document is not available
            if (!status.available) {
              console.log(`[WorkspaceCanvas] Document ${docId} not available in BigQuery: reason=${status.reason || 'unknown'}${status.error ? `, error=${status.error}` : ''}`);
            } else {
              console.log(`[WorkspaceCanvas] Document ${docId} is available in BigQuery`);
            }
          } catch (err) {
            console.error(`[WorkspaceCanvas] Failed to check BigQuery status for ${docId}`, err);
            setDocumentBigQueryStatus(docId, false, false);
          }
        });

        await Promise.all(checkPromises);
      }
    };

    checkBigQueryStatuses();
  }, [isOpen, currentProjectsWithDocs, bigqueryStatusesKeys, setDocumentBigQueryStatus, getBigQueryStatuses]);

  // Check Vertex Search Availability status for documents - update in background, use cached if available
  React.useEffect(() => {
    if (!isOpen || currentProjectsWithDocs.length === 0) return;

    const checkVertexSearchAvailabilityStatuses = async () => {
      const vertexStatuses = getVertexSearchAvailabilityStatuses();
      // Collect all documents that need status check
      const docsToCheck: Array<{ projectId: string; docId: string }> = [];
      
      for (const { project, documents } of currentProjectsWithDocs) {
        for (const doc of documents) {
          if (doc.id === 'local-unfold') {
            continue;
          }
          if (!doc.storagePath) continue; // Skip docs without storagePath (they can't be in Vertex Search)
          
          const existingStatus = vertexStatuses[doc.id];
          // Check if we need to refresh (no status or status is older than 5 minutes)
          const needsCheck = !existingStatus || 
            (Date.now() - existingStatus.lastChecked > 5 * 60 * 1000);
          
          if (needsCheck) {
            docsToCheck.push({ projectId: project.id, docId: doc.id });
            // Set loading state immediately
            setDocumentVertexSearchAvailabilityStatus(doc.id, false, true);
          }
        }
      }

      if (docsToCheck.length > 0) {
        console.log(`[WorkspaceCanvas] Vertex Search status check: to check=${docsToCheck.length}`);
      }

      // Check statuses in batches to avoid ERR_INSUFFICIENT_RESOURCES
      // Process 10 checks at a time
      const CHECK_BATCH_SIZE = 10;
      for (let i = 0; i < docsToCheck.length; i += CHECK_BATCH_SIZE) {
        const batch = docsToCheck.slice(i, i + CHECK_BATCH_SIZE);
        const checkPromises = batch.map(async ({ projectId, docId }) => {
          try {
            const status = await api.docsCheckVertexSearchAvailability(projectId, docId);
            setDocumentVertexSearchAvailabilityStatus(docId, status.available, false);
          } catch (err) {
            console.error(`[WorkspaceCanvas] Failed to check Vertex Search status for ${docId}`, err);
            setDocumentVertexSearchAvailabilityStatus(docId, false, false);
          }
        });

        await Promise.all(checkPromises);
      }
    };

    checkVertexSearchAvailabilityStatuses();
  }, [isOpen, currentProjectsWithDocs, vertexSearchAvailabilityStatusesKeys, setDocumentVertexSearchAvailabilityStatus, getVertexSearchAvailabilityStatuses]);

  const getIndexedStatus = React.useCallback((doc: CoreDoc): { status: 'indexed' | 'not_indexed' | 'unknown' | 'loading'; text: string } => {
    const status = currentIndexStatuses[doc.id];
    if (!status) {
      return { status: 'unknown', text: t('unfold.workspace.unknown') };
    }
    
    if (status.loading) {
      return { status: 'loading', text: t('unfold.workspace.checking') };
    }
    
    return status.indexed 
      ? { status: 'indexed', text: t('unfold.workspace.indexed') }
      : { status: 'not_indexed', text: t('unfold.workspace.notIndexed') };
  }, [currentIndexStatuses, t]);

  const getBigQueryStatus = React.useCallback((doc: CoreDoc): { status: 'available' | 'not_available' | 'unknown' | 'loading'; text: string } => {
    // Check status from store - don't assume based on storagePath
    // Metadata can exist in BigQuery even without storagePath
    
    const status = currentBigQueryStatuses[doc.id];
    if (!status) {
      return { status: 'unknown', text: t('unfold.workspace.unknown') };
    }
    
    if (status.loading) {
      return { status: 'loading', text: t('unfold.workspace.checking') };
    }
    
    return status.available 
      ? { status: 'available', text: t('unfold.workspace.available') }
      : { status: 'not_available', text: t('unfold.workspace.notAvailable') };
  }, [currentBigQueryStatuses, t]);

  const getVertexSearchAvailabilityStatus = React.useCallback((doc: CoreDoc): { status: 'available' | 'not_available' | 'unknown' | 'loading'; text: string } => {
    if (!doc.storagePath) {
      return { status: 'not_available', text: t('unfold.workspace.notAvailable') };
    }
    
    const status = currentVertexSearchAvailabilityStatuses[doc.id];
    if (!status) {
      return { status: 'unknown', text: t('unfold.workspace.unknown') };
    }
    
    if (status.loading) {
      return { status: 'loading', text: t('unfold.workspace.checking') };
    }
    
    return status.available 
      ? { status: 'available', text: t('unfold.workspace.searchable') }
      : { status: 'not_available', text: t('unfold.workspace.notSearchable') };
  }, [currentVertexSearchAvailabilityStatuses, t]);

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
  // Filter by projectId if specified
  const sortedProjectsWithDocs = React.useMemo(() => {
    const filtered = projectId
      ? currentProjectsWithDocs.filter((p) => p.project.id === projectId)
      : currentProjectsWithDocs;
    
    return [...filtered].sort((a, b) => {
      const fallbackA = a.lastUpdated || 0;
      const fallbackB = b.lastUpdated || 0;
      const tsA = getProjectSortTimestamp(a.project, fallbackA);
      const tsB = getProjectSortTimestamp(b.project, fallbackB);

      if (tsA !== tsB) {
        return tsB - tsA; // Newest first
      }
      return a.project.displayName.localeCompare(b.project.displayName);
    });
  }, [currentProjectsWithDocs, projectId, getProjectSortTimestamp]);

  // Handler for document added callback
  const handleDocumentAdded = React.useCallback(() => {
    if (!projectId) return;
    // Reload documents
    const loadDocs = async () => {
      try {
        const docs = await api.docsList(projectId);
        const projects = getProjects() || [];
        const project = projects.find((p) => p.id === projectId);
        if (project) {
          updateProjectDocs(project, docs || []);
        }
      } catch (err) {
        console.error('[WorkspaceCanvas] Failed to reload documents', err);
      }
    };
    loadDocs();
  }, [projectId, getProjects, updateProjectDocs]);

  // Handler for source added callback
  const handleSourceAdded = React.useCallback(() => {
    if (!projectId) return;
    // Reload sources with retry logic (since source creation is async)
    const loadSources = async (retryCount = 0) => {
      try {
        const sources = await api.getProjectSources(projectId);
        const sourcesArray = sources || [];
        // Update both local state and store for synchronization
        setProjectSources((prev) => ({
          ...prev,
          [projectId]: sourcesArray,
        }));
        setProjectSourcesStore(projectId, sourcesArray);
      } catch (err) {
        console.error('[WorkspaceCanvas] Failed to reload sources', err);
        // Retry up to 3 times with exponential backoff
        if (retryCount < 3) {
          const delay = Math.pow(2, retryCount) * 500; // 500ms, 1000ms, 2000ms
          setTimeout(() => loadSources(retryCount + 1), delay);
        }
      }
    };
    // Wait a bit for source creation to complete, then load
    setTimeout(() => loadSources(), 500);
  }, [projectId, setProjectSourcesStore]);

  // Handler for adding Telegram source
  const handleAddTelegramSource = async () => {
    if (!projectId) return;
    setTgError(null);
    const handle = tgInput.trim().replace(/^@/, '').replace(/^t\.me\//, '').replace(/^telegram\.me\//, '');
    if (!handle) {
      setTgError('Enter @username or t.me/username');
      return;
    }
    
    setTgSubmitting(true);
    try {
      // Use executeDiscovery to add Telegram source
      // Note: This is fire-and-forget on backend, so source creation happens asynchronously
      await api.executeDiscovery({
        projectId,
        queryText: '', // Empty query for source-only addition
        language: 'EN',
        sources: { web: [], telegram: [handle] },
      });
      setShowAddTelegramModal(false);
      setTgInput('');
      // Reload sources for the project (with retry logic to handle async creation)
      handleSourceAdded();
    } catch (e: any) {
      setTgError(e?.message || 'Failed to add channel');
    } finally {
      setTgSubmitting(false);
    }
  };

  // Handler for adding RSS source
  const handleAddRSSSource = async () => {
    if (!projectId) return;
    setRssError(null);
    const url = rssInput.trim();
    if (!url) {
      setRssError('Enter RSS feed URL');
      return;
    }
    
    setRssSubmitting(true);
    try {
      // Use executeDiscovery to add RSS source
      await api.executeDiscovery({
        projectId,
        queryText: '', // Empty query for source-only addition
        language: 'EN',
        sources: { web: [], telegram: [], rss: [url] },
      });
      setShowAddRSSModal(false);
      setRssInput('');
      // Reload sources for the project (with retry logic to handle async creation)
      handleSourceAdded();
    } catch (e: any) {
      setRssError(e?.message || 'Failed to add RSS feed');
    } finally {
      setRssSubmitting(false);
    }
  };

  if (!isOpen) return null;

  return (
    <>
      <ModalWithHeader
        title={t('unfold.sections.workspace')}
        onClose={onClose}
      >
        <div className="space-y-6 py-2">
          {/* Toolbar - only show when projectId is provided */}
          {projectId && (
            <WorkspaceToolbar
              projectId={projectId}
              onDocumentAdded={handleDocumentAdded}
              onSourceAdded={handleSourceAdded}
              onShowTelegramModal={() => setShowAddTelegramModal(true)}
              onShowRSSModal={() => setShowAddRSSModal(true)}
            />
          )}
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
              sources={projectSources[project.id]}
              discussionFrame={discussionFrame}
              indexStatuses={currentIndexStatuses}
              getIndexedStatus={getIndexedStatus}
              getBigQueryStatus={getBigQueryStatus}
              getVertexSearchAvailabilityStatus={getVertexSearchAvailabilityStatus}
              onOpenDocument={onOpenDocument}
              onSourceStatusChange={(sourceId, newStatus) => {
                // Update local state immediately
                setProjectSources(prev => {
                  const updated = { ...prev };
                  if (updated[project.id]) {
                    updated[project.id] = updated[project.id].map(s =>
                      s.id === sourceId ? { ...s, status: newStatus } : s
                    );
                  }
                  return updated;
                });
                // Update store for synchronization with WorkspaceLibrary
                updateSourceStatus(sourceId, newStatus);
                // Also update store sources for this project
                if (projectSources[project.id]) {
                  const updated = projectSources[project.id].map(s =>
                    s.id === sourceId ? { ...s, status: newStatus } : s
                  );
                  setProjectSourcesStore(project.id, updated);
                }
                // Reload from server to ensure consistency
                handleSourceAdded();
              }}
              onSourceDelete={(sourceId) => {
                // Update local state immediately
                setProjectSources(prev => {
                  const updated = { ...prev };
                  if (updated[project.id]) {
                    updated[project.id] = updated[project.id].filter(s => s.id !== sourceId);
                  }
                  return updated;
                });
                // Update store for synchronization with WorkspaceLibrary
                if (projectSources[project.id]) {
                  const updated = projectSources[project.id].filter(s => s.id !== sourceId);
                  setProjectSourcesStore(project.id, updated);
                }
                // Reload from server to ensure consistency
                handleSourceAdded();
              }}
              onSourcePreview={(source) => {
                setPreviewSource(source);
              }}
              onViewDocuments={async (source) => {
                // Load full source data for documents view
                try {
                  const allSources = await api.getProjectSources(project.id);
                  const fullSource = allSources.find((s: DataSource) => s.id === source.id);
                  if (fullSource) {
                    setDocumentsViewSource(fullSource);
                  }
                } catch (err) {
                  console.error('[WorkspaceCanvas] Failed to load source for documents view', err);
                }
              }}
            />
          ))
        )}
        </div>
      </ModalWithHeader>

      {/* Source Preview */}
      {previewSource && (
        <SourcePreview
          source={previewSource}
          onClose={() => setPreviewSource(null)}
        />
      )}

      {/* Source Documents Viewer */}
      {documentsViewSource && (
        <SourceDocumentsViewer
          source={documentsViewSource}
          onClose={() => setDocumentsViewSource(null)}
        />
      )}

      {/* Telegram Add Modal */}
      {projectId && (
        <>
          <TelegramAddModal
            isOpen={showAddTelegramModal}
            onClose={() => {
              setShowAddTelegramModal(false);
              setTgInput('');
              setTgError(null);
            }}
            input={tgInput}
            onInputChange={setTgInput}
            onSubmit={handleAddTelegramSource}
            isSubmitting={tgSubmitting}
            error={tgError}
          />

          <RSSAddModal
            isOpen={showAddRSSModal}
            onClose={() => {
              setShowAddRSSModal(false);
              setRssInput('');
              setRssError(null);
            }}
            input={rssInput}
            onInputChange={setRssInput}
            onSubmit={handleAddRSSSource}
            isSubmitting={rssSubmitting}
            error={rssError}
          />
        </>
      )}
    </>
  );
};

