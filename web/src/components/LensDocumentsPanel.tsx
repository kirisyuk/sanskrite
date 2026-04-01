import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { api } from '../lib/api';
import type { CoreDoc } from '../types/generated';
import { isArtifact, type Artifact } from '../types/artifact';

interface ArtifactTemplate {
  id: string;
  name: string;
  description?: string;
  category?: string;
  graphSourceKinds?: string[];
  defaultStructure?: Array<{
    id: string;
    title: string;
    order: number;
    description?: string;
    required?: boolean;
  }>;
}

interface LensDocumentsPanelProps {
  projectId: string | null;
  lensId: string | null;
  lensName?: string | null;
  graphSourceKind?: string;
}

function matchesLensArtifact(artifact: Artifact, lensId: string | null, graphSourceKind: string): boolean {
  if (!lensId) return false;
  const graphSources = artifact.artifactConfig?.contextSources?.graphSources || [];
  return graphSources.some((item: any) =>
    String(item?.kind || '').trim() === graphSourceKind &&
    String(item?.lensId || '').trim() === lensId,
  );
}

function sortArtifacts(items: Artifact[]): Artifact[] {
  return [...items].sort((a, b) => {
    const aTime = new Date(a.updatedAt || a.createdAt || 0).getTime();
    const bTime = new Date(b.updatedAt || b.createdAt || 0).getTime();
    return bTime - aTime || String(a.filename || '').localeCompare(String(b.filename || ''));
  });
}

function formatDateTime(value?: string): string {
  if (!value) return '';
  try {
    return new Date(value).toLocaleString();
  } catch {
    return '';
  }
}

export const LensDocumentsPanel: React.FC<LensDocumentsPanelProps> = ({
  projectId,
  lensId,
  lensName,
  graphSourceKind = 'lens_supply_chain_report',
}) => {
  const [artifacts, setArtifacts] = React.useState<Artifact[]>([]);
  const [templates, setTemplates] = React.useState<ArtifactTemplate[]>([]);
  const [loadingArtifacts, setLoadingArtifacts] = React.useState(false);
  const [loadingTemplates, setLoadingTemplates] = React.useState(false);
  const [selectedArtifactId, setSelectedArtifactId] = React.useState<string | null>(null);
  const [creatingNew, setCreatingNew] = React.useState(false);
  const [selectedTemplate, setSelectedTemplate] = React.useState<ArtifactTemplate | null>(null);
  const [artifactName, setArtifactName] = React.useState('');
  const [userRequest, setUserRequest] = React.useState('');
  const [creatingArtifact, setCreatingArtifact] = React.useState(false);
  const [activeSessionId, setActiveSessionId] = React.useState<string | null>(null);
  const [activeSessionArtifactId, setActiveSessionArtifactId] = React.useState<string | null>(null);
  const [activeSessionStatus, setActiveSessionStatus] = React.useState<string | null>(null);
  const [shareBusyArtifactId, setShareBusyArtifactId] = React.useState<string | null>(null);
  const [rerunBusyArtifactId, setRerunBusyArtifactId] = React.useState<string | null>(null);
  const [error, setError] = React.useState<string | null>(null);

  const loadArtifacts = React.useCallback(async () => {
    if (!projectId || !lensId) {
      setArtifacts([]);
      return;
    }
    setLoadingArtifacts(true);
    try {
      const docs = await api.docsList(projectId);
      const next = sortArtifacts(
        docs.filter((doc: CoreDoc) => isArtifact(doc))
          .filter((doc): doc is Artifact => isArtifact(doc))
          .filter((doc) => matchesLensArtifact(doc, lensId, graphSourceKind)),
      );
      setArtifacts(next);
      setSelectedArtifactId((current) => {
        if (current && next.some((item) => item.id === current)) {
          return current;
        }
        return next[0]?.id || null;
      });
    } catch (loadError) {
      console.error('[LensDocumentsPanel] Failed to load artifacts', loadError);
      setArtifacts([]);
      setError('Failed to load lens documents.');
    } finally {
      setLoadingArtifacts(false);
    }
  }, [projectId, lensId, graphSourceKind]);

  const loadTemplates = React.useCallback(async () => {
    setLoadingTemplates(true);
    try {
      const items = await api.listArtifactTemplates(undefined, { graphSourceKind });
      setTemplates(Array.isArray(items) ? items : []);
    } catch (loadError) {
      console.error('[LensDocumentsPanel] Failed to load templates', loadError);
      setTemplates([]);
      setError('Failed to load document templates.');
    } finally {
      setLoadingTemplates(false);
    }
  }, [graphSourceKind]);

  React.useEffect(() => {
    void loadArtifacts();
  }, [loadArtifacts]);

  React.useEffect(() => {
    void loadTemplates();
  }, [loadTemplates]);

  React.useEffect(() => {
    const handleContentUpdated = (event: Event) => {
      const customEvent = event as CustomEvent<{ projectId?: string }>;
      if (!projectId) return;
      if (customEvent.detail?.projectId && customEvent.detail.projectId !== projectId) return;
      void loadArtifacts();
    };
    window.addEventListener('content.updated', handleContentUpdated as EventListener);
    window.addEventListener('pages.updated', handleContentUpdated as EventListener);
    return () => {
      window.removeEventListener('content.updated', handleContentUpdated as EventListener);
      window.removeEventListener('pages.updated', handleContentUpdated as EventListener);
    };
  }, [projectId, loadArtifacts]);

  React.useEffect(() => {
    if (!activeSessionId || !projectId) return;
    let cancelled = false;
    const timer = window.setInterval(async () => {
      try {
        const session = await api.getGenerationSession(activeSessionId);
        if (cancelled) return;
        const status = String(session?.status || '').trim();
        setActiveSessionStatus(status || null);
        await loadArtifacts();
        if (status === 'completed' || status === 'failed' || status === 'cancelled') {
          window.clearInterval(timer);
          setActiveSessionId(null);
          setActiveSessionArtifactId(null);
          if (status === 'failed') {
            setError('Document generation failed.');
          }
        }
      } catch (sessionError) {
        console.error('[LensDocumentsPanel] Failed to poll generation session', sessionError);
        if (!cancelled) {
          window.clearInterval(timer);
          setActiveSessionId(null);
          setActiveSessionArtifactId(null);
        }
      }
    }, 2000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [activeSessionId, projectId, loadArtifacts]);

  const selectedArtifact = React.useMemo(
    () => artifacts.find((item) => item.id === selectedArtifactId) || null,
    [artifacts, selectedArtifactId],
  );

  const resetCreateState = React.useCallback(() => {
    setCreatingNew(false);
    setSelectedTemplate(null);
    setArtifactName('');
    setUserRequest('');
    setCreatingArtifact(false);
  }, []);

  const handleTemplateSelect = React.useCallback((template: ArtifactTemplate) => {
    setSelectedTemplate(template);
    const baseLensName = String(lensName || 'Lens').trim();
    const baseTemplateName = String(template.name || 'Document').trim();
    setArtifactName([baseLensName, baseTemplateName].filter(Boolean).join(' - '));
  }, [lensName]);

  const handleCreateArtifact = React.useCallback(async () => {
    if (!projectId || !lensId || !selectedTemplate || !artifactName.trim()) {
      return;
    }
    setCreatingArtifact(true);
    setError(null);
    try {
      const response = await api.createArtifactFromTemplate(projectId, {
        templateId: selectedTemplate.id,
        filename: artifactName.trim(),
        userRequest: userRequest.trim() || undefined,
        preferredLanguage: 'en',
        graphSources: [
          {
            kind: graphSourceKind,
            lensId,
            templateId: selectedTemplate.id,
          },
        ],
        autoGenerate: true,
        generationMode: 'auto',
        maxOutputTokens: 32768,
        thinkingBudget: 32768,
      });
      const artifact = response?.artifact;
      if (!artifact?.id) {
        throw new Error('Artifact was created without an id');
      }
      setSelectedArtifactId(String(artifact.id));
      if (response?.session?.id) {
        setActiveSessionId(String(response.session.id));
        setActiveSessionArtifactId(String(artifact.id));
        setActiveSessionStatus(String(response.session.status || 'pending'));
      }
      if (response?.generationError) {
        setError(String(response.generationError));
      }
      await loadArtifacts();
      resetCreateState();
      window.dispatchEvent(new CustomEvent('content.updated', { detail: { projectId } }));
    } catch (createError) {
      console.error('[LensDocumentsPanel] Failed to create artifact from template', createError);
      setError('Failed to create lens document.');
    } finally {
      setCreatingArtifact(false);
    }
  }, [projectId, lensId, selectedTemplate, artifactName, userRequest, graphSourceKind, loadArtifacts, resetCreateState]);

  const handleRerunArtifact = React.useCallback(async (artifact: Artifact) => {
    if (!projectId) return;
    setRerunBusyArtifactId(artifact.id);
    setError(null);
    try {
      const session = await api.startArtifactGeneration(artifact.id, 'auto', projectId);
      if (session?.id) {
        setSelectedArtifactId(artifact.id);
        setActiveSessionId(String(session.id));
        setActiveSessionArtifactId(artifact.id);
        setActiveSessionStatus(String(session.status || 'pending'));
      }
    } catch (rerunError) {
      console.error('[LensDocumentsPanel] Failed to rerun artifact generation', rerunError);
      setError('Failed to regenerate document.');
    } finally {
      setRerunBusyArtifactId(null);
    }
  }, [projectId]);

  const handleShareArtifact = React.useCallback(async (artifact: Artifact) => {
    if (!projectId) return;
    setShareBusyArtifactId(artifact.id);
    setError(null);
    try {
      const share = await api.shareCreate(projectId);
      const url = `${window.location.origin}/shared/${encodeURIComponent(String(share.shareToken || ''))}?doc=${encodeURIComponent(artifact.id)}`;
      const opened = window.open(url, '_blank', 'noopener,noreferrer');
      if (!opened) {
        window.location.href = url;
      }
    } catch (shareError) {
      console.error('[LensDocumentsPanel] Failed to share artifact', shareError);
      setError('Failed to create share link.');
    } finally {
      setShareBusyArtifactId(null);
    }
  }, [projectId]);

  const handleExportArtifact = React.useCallback(async (artifact: Artifact) => {
    if (!projectId) return;
    try {
      const blob = await api.docsExportToDocx(projectId, artifact.id);
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = `${artifact.filename.replace(/\.[^/.]+$/, '') || 'document'}.docx`;
      document.body.appendChild(anchor);
      anchor.click();
      document.body.removeChild(anchor);
      URL.revokeObjectURL(url);
    } catch (exportError) {
      console.error('[LensDocumentsPanel] Failed to export artifact', exportError);
      setError('Failed to export document.');
    }
  }, [projectId]);

  return (
    <div className="min-h-0 flex flex-col gap-3">
      <div className="flex items-center justify-between gap-2">
        <div>
          <div className="text-sm font-medium" style={{ color: 'var(--text-heading)' }}>
            Lens Documents
          </div>
          <div className="text-[11px]" style={{ color: 'var(--text-tertiary-weak)' }}>
            Dynamic documents bound to the current lens substrate.
          </div>
        </div>
        <button
          type="button"
          onClick={() => {
            setError(null);
            setCreatingNew(true);
          }}
          className="px-3 py-1.5 rounded text-xs transition-colors"
          style={{
            backgroundColor: 'rgba(236, 179, 56, 0.16)',
            color: 'var(--text-heading)',
            border: '1px solid var(--overlay-line-90)',
          }}
        >
          Create new document
        </button>
      </div>

      {error ? (
        <div
          className="rounded-lg px-3 py-2 text-xs"
          style={{
            backgroundColor: 'rgba(220, 38, 38, 0.12)',
            color: 'var(--text-heading)',
            border: '1px solid rgba(220, 38, 38, 0.24)',
          }}
        >
          {error}
        </div>
      ) : null}

      {creatingNew ? (
        <div
          className="rounded-xl p-3 space-y-3"
          style={{
            backgroundColor: 'var(--toolbar-bg)',
            border: '1px solid var(--overlay-line-90)',
          }}
        >
          <div className="flex items-center justify-between gap-2">
            <div>
              <div className="text-sm font-medium" style={{ color: 'var(--text-heading)' }}>
                {selectedTemplate ? selectedTemplate.name : 'Choose a template'}
              </div>
              <div className="text-[11px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                Graph-bound templates only.
              </div>
            </div>
            <button
              type="button"
              onClick={resetCreateState}
              className="px-2.5 py-1 rounded text-xs"
              style={{
                backgroundColor: 'transparent',
                color: 'var(--text-secondary-strong)',
                border: '1px solid var(--overlay-line-90)',
              }}
            >
              Back
            </button>
          </div>

          {!selectedTemplate ? (
            <div className="space-y-2">
              {loadingTemplates ? (
                <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
                  Loading templates...
                </div>
              ) : templates.length === 0 ? (
                <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
                  No graph-bound templates are available for this lens yet.
                </div>
              ) : (
                templates.map((template) => (
                  <button
                    key={template.id}
                    type="button"
                    onClick={() => handleTemplateSelect(template)}
                    className="w-full text-left rounded-lg px-3 py-3 transition-colors"
                    style={{
                      backgroundColor: 'var(--bg-sidebar)',
                      border: '1px solid var(--overlay-line-90)',
                      color: 'var(--text-heading)',
                    }}
                  >
                    <div className="text-sm font-medium">{template.name}</div>
                    {template.description ? (
                      <div className="text-xs mt-1" style={{ color: 'var(--text-secondary-strong)' }}>
                        {template.description}
                      </div>
                    ) : null}
                  </button>
                ))
              )}
            </div>
          ) : (
            <div className="space-y-3">
              {selectedTemplate.description ? (
                <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
                  {selectedTemplate.description}
                </div>
              ) : null}

              <label className="block text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
                Document name
                <input
                  type="text"
                  value={artifactName}
                  onChange={(event) => setArtifactName(event.target.value)}
                  className="mt-1 w-full rounded-md px-3 py-2 text-sm"
                  style={{
                    backgroundColor: 'var(--bg-sidebar)',
                    color: 'var(--text-heading)',
                    border: '1px solid var(--overlay-line-90)',
                  }}
                />
              </label>

              <label className="block text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
                Optional guidance
                <textarea
                  value={userRequest}
                  onChange={(event) => setUserRequest(event.target.value)}
                  rows={4}
                  placeholder="Add a narrow framing note for this document."
                  className="mt-1 w-full rounded-md px-3 py-2 text-sm resize-y"
                  style={{
                    backgroundColor: 'var(--bg-sidebar)',
                    color: 'var(--text-heading)',
                    border: '1px solid var(--overlay-line-90)',
                  }}
                />
              </label>

              <div className="space-y-2">
                <div className="text-xs font-medium" style={{ color: 'var(--text-heading)' }}>
                  Sections
                </div>
                {(selectedTemplate.defaultStructure || [])
                  .slice()
                  .sort((a, b) => a.order - b.order)
                  .map((section) => (
                    <div
                      key={section.id}
                      className="rounded-lg px-3 py-2"
                      style={{
                        backgroundColor: 'var(--bg-sidebar)',
                        border: '1px solid var(--overlay-line-90)',
                      }}
                    >
                      <div className="text-xs font-medium" style={{ color: 'var(--text-heading)' }}>
                        {section.title}
                      </div>
                      {section.description ? (
                        <div className="text-[11px] mt-1" style={{ color: 'var(--text-secondary-strong)' }}>
                          {section.description}
                        </div>
                      ) : null}
                    </div>
                  ))}
              </div>

              <div className="flex justify-end">
                <button
                  type="button"
                  onClick={handleCreateArtifact}
                  disabled={creatingArtifact || !artifactName.trim()}
                  className="px-3 py-1.5 rounded text-xs transition-colors"
                  style={{
                    backgroundColor: creatingArtifact ? 'var(--white-10)' : 'rgba(236, 179, 56, 0.16)',
                    color: 'var(--text-heading)',
                    border: '1px solid var(--overlay-line-90)',
                    cursor: creatingArtifact || !artifactName.trim() ? 'not-allowed' : 'pointer',
                  }}
                >
                  {creatingArtifact ? 'Creating...' : 'Create and generate'}
                </button>
              </div>
            </div>
          )}
        </div>
      ) : null}

      <div
        className="rounded-xl p-3 space-y-3"
        style={{
          backgroundColor: 'var(--toolbar-bg)',
          border: '1px solid var(--overlay-line-90)',
        }}
      >
        <div className="flex items-center justify-between gap-2">
          <div className="text-xs uppercase tracking-wide" style={{ color: 'var(--text-tertiary-weak)' }}>
            Documents ({artifacts.length})
          </div>
          {activeSessionId && activeSessionArtifactId ? (
            <div className="text-[11px]" style={{ color: 'var(--text-secondary-strong)' }}>
              {activeSessionStatus ? `Generating: ${activeSessionStatus}` : 'Generating...'}
            </div>
          ) : null}
        </div>

        {loadingArtifacts ? (
          <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
            Loading documents...
          </div>
        ) : artifacts.length === 0 ? (
          <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
            No documents have been created for this lens yet.
          </div>
        ) : (
          <div className="space-y-1.5">
            {artifacts.map((artifact) => {
              const active = artifact.id === selectedArtifactId;
              const generating = activeSessionArtifactId === artifact.id;
              return (
                <div
                  key={artifact.id}
                  className="rounded-lg px-3 py-2 transition-colors"
                  style={{
                    backgroundColor: active ? 'var(--white-10)' : 'var(--bg-sidebar)',
                    border: '1px solid var(--overlay-line-90)',
                  }}
                >
                  <div className="flex items-start justify-between gap-2">
                    <button
                      type="button"
                      onClick={() => setSelectedArtifactId(artifact.id)}
                      className="flex-1 text-left"
                      style={{ color: 'var(--text-heading)' }}
                    >
                      <div className="text-sm font-medium">{artifact.filename}</div>
                      <div className="text-[11px] mt-1" style={{ color: 'var(--text-tertiary-weak)' }}>
                        {formatDateTime(artifact.updatedAt || artifact.createdAt)}
                        {generating ? ' · generating' : ''}
                      </div>
                    </button>
                    <div className="flex items-center gap-1">
                      <button
                        type="button"
                        onClick={() => void handleShareArtifact(artifact)}
                        disabled={shareBusyArtifactId === artifact.id}
                        className="px-2 py-1 rounded text-[11px]"
                        style={{
                          backgroundColor: 'transparent',
                          color: 'var(--text-secondary-strong)',
                          border: '1px solid var(--overlay-line-90)',
                        }}
                      >
                        {shareBusyArtifactId === artifact.id ? 'Sharing...' : 'Share'}
                      </button>
                      <button
                        type="button"
                        onClick={() => void handleRerunArtifact(artifact)}
                        disabled={rerunBusyArtifactId === artifact.id || generating}
                        className="px-2 py-1 rounded text-[11px]"
                        style={{
                          backgroundColor: 'transparent',
                          color: 'var(--text-secondary-strong)',
                          border: '1px solid var(--overlay-line-90)',
                        }}
                      >
                        {rerunBusyArtifactId === artifact.id ? 'Running...' : 'Regenerate'}
                      </button>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      <div
        className="min-h-0 flex-1 rounded-xl p-3 flex flex-col"
        style={{
          backgroundColor: 'var(--toolbar-bg)',
          border: '1px solid var(--overlay-line-90)',
        }}
      >
        {selectedArtifact ? (
          <>
            <div className="flex items-start justify-between gap-3 mb-3">
              <div>
                <div className="text-sm font-medium" style={{ color: 'var(--text-heading)' }}>
                  {selectedArtifact.filename}
                </div>
                <div className="text-[11px] mt-1" style={{ color: 'var(--text-tertiary-weak)' }}>
                  Preview of the current document version.
                </div>
              </div>
              <div className="flex items-center gap-1">
                <button
                  type="button"
                  onClick={() => void handleShareArtifact(selectedArtifact)}
                  className="px-2 py-1 rounded text-[11px]"
                  style={{
                    backgroundColor: 'transparent',
                    color: 'var(--text-secondary-strong)',
                    border: '1px solid var(--overlay-line-90)',
                  }}
                >
                  Share
                </button>
                <button
                  type="button"
                  onClick={() => void handleExportArtifact(selectedArtifact)}
                  className="px-2 py-1 rounded text-[11px]"
                  style={{
                    backgroundColor: 'transparent',
                    color: 'var(--text-secondary-strong)',
                    border: '1px solid var(--overlay-line-90)',
                  }}
                >
                  Export
                </button>
              </div>
            </div>

            <div className="min-h-0 flex-1 overflow-y-auto space-y-3 pr-1">
              {(selectedArtifact.artifactConfig?.structure || [])
                .slice()
                .sort((a: any, b: any) => a.order - b.order)
                .map((section: any) => (
                  <div
                    key={section.id}
                    className="rounded-lg"
                    style={{
                      backgroundColor: 'var(--bg-sidebar)',
                      border: '1px solid var(--overlay-line-90)',
                    }}
                  >
                    <div
                      className="px-4 py-3 border-b"
                      style={{ borderColor: 'var(--overlay-line-90)' }}
                    >
                      <div className="text-sm font-medium" style={{ color: 'var(--text-heading)' }}>
                        {section.title}
                      </div>
                      {section.description ? (
                        <div className="text-[11px] mt-1" style={{ color: 'var(--text-secondary-strong)' }}>
                          {section.description}
                        </div>
                      ) : null}
                    </div>
                    <div className="px-4 py-3">
                      {section.content ? (
                        <div className="markdown text-xs" style={{ color: 'var(--text-heading)' }}>
                          <ReactMarkdown remarkPlugins={[remarkGfm]}>
                            {String(section.content)}
                          </ReactMarkdown>
                        </div>
                      ) : (
                        <div className="text-xs" style={{ color: 'var(--text-tertiary-weak)' }}>
                          Section not generated yet.
                        </div>
                      )}
                    </div>
                  </div>
                ))}
            </div>
          </>
        ) : (
          <div className="text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
            Select a lens document to preview it here.
          </div>
        )}
      </div>
    </div>
  );
};

export default LensDocumentsPanel;
