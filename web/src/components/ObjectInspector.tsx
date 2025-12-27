import React from 'react';
import { api } from '../lib/api';
import type { HorizonNodeData } from './HorizonGraphNodeTypes';

interface ObjectInspectorProps {
  nodeId: string | null;
  nodeData: HorizonNodeData | null;
  edgeId: string | null;
  edgeData: any | null;
  lensId: string | null;
}

export const ObjectInspector: React.FC<ObjectInspectorProps> = ({
  nodeId,
  nodeData,
  edgeId,
  edgeData,
  lensId,
}) => {
  const [fragment, setFragment] = React.useState<any>(null);
  const [entity, setEntity] = React.useState<any>(null);
  const [edge, setEdge] = React.useState<any>(null);
  const [sourceFragment, setSourceFragment] = React.useState<any>(null);
  const [targetFragment, setTargetFragment] = React.useState<any>(null);
  const [edgeExplainLoading, setEdgeExplainLoading] = React.useState(false);
  const [edgeExplainError, setEdgeExplainError] = React.useState<string | null>(null);
  const [edgeExplainText, setEdgeExplainText] = React.useState<string | null>(null);
  const [nodeExplainLoading, setNodeExplainLoading] = React.useState(false);
  const [nodeExplainError, setNodeExplainError] = React.useState<string | null>(null);
  const [nodeExplainText, setNodeExplainText] = React.useState<string | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [ensuring, setEnsuring] = React.useState(false);
  const [enriching, setEnriching] = React.useState(false);
  const [productIOLoading, setProductIOLoading] = React.useState(false);
  const [productIOError, setProductIOError] = React.useState<string | null>(null);
  const [productIOResult, setProductIOResult] = React.useState<{ nodes: number; edges: number } | null>(null);

  // Load fragment and entity data when nodeId changes
  React.useEffect(() => {
    if (!nodeId) {
      setFragment(null);
      setEntity(null);
      setError(null);
      setNodeExplainError(null);
      setNodeExplainText(null);
      return;
    }

    const loadData = async () => {
      setLoading(true);
      setError(null);
      try {
        // Use fragmentId from nodeData, or fallback to nodeId if fragmentId is not available
        const fragmentId = nodeData?.fragmentId || nodeId;
        
        if (!fragmentId) {
          setError('Fragment ID is missing');
          setLoading(false);
          return;
        }
        
        // Load fragment
        const fragmentData = await api.getHorizonFragment(fragmentId);
        setFragment(fragmentData);

        // Try to load linked entity
        try {
          const entityData = await api.getFragmentEntity(fragmentId);
          setEntity(entityData);
        } catch (err) {
          // Entity might not exist, that's okay
          setEntity(null);
        }
      } catch (err: any) {
        console.error('[ObjectInspector] Failed to load data', err);
        setError(err.response?.data?.error || err.message || 'Failed to load data');
      } finally {
        setLoading(false);
      }
    };

    loadData();
  }, [nodeId, nodeData]);

  const handleExplainNode = React.useCallback(async () => {
    if (!nodeId) return;
    setNodeExplainLoading(true);
    setNodeExplainError(null);
    setNodeExplainText(null);

    try {
      const preferredLanguage =
        (typeof navigator !== 'undefined' && navigator.language) ? navigator.language : 'en';

      const fragmentId = nodeData?.fragmentId || nodeId;

      const payload = {
        preferredLanguage,
        node: {
          nodeId,
          fragmentId,
          label:
            nodeData?.label ||
            fragment?.labels?.en ||
            fragment?.canonicalPath?.split('/')?.pop() ||
            null,
          canonicalPath: nodeData?.canonicalPath || fragment?.canonicalPath || null,
          type: nodeData?.type || fragment?.type || null,
          status: nodeData?.status || fragment?.status || null,
        },
        visibleIncomingOutgoingEdges: (nodeData as any)?.visibleIncidentEdges || [],
        visibleNeighborNodes: (nodeData as any)?.visibleNeighborNodes || [],
      };

      const prompt =
        `Explain this node and the visible incoming/outgoing relationships.\n` +
        `- Output language: ${preferredLanguage}\n` +
        `- Use Google Search grounding.\n` +
        `- Output: plain text only.\n` +
        `- Be detailed (but structured): 2-4 short paragraphs.\n` +
        `- If you are unsure about anything, say so explicitly.\n\n` +
        `Graph context (JSON):\n` +
        `${JSON.stringify(payload, null, 2)}\n`;

      const resp: any = await api.genaiComplete({
        prompt,
        options: {
          profile: 'fast',
          modelOverride: 'gemini-3-flash-preview',
          groundingEnabled: true,
          temperature: 0.2,
          maxOutputTokens: 8192,
          systemInstruction:
            'You are a careful analyst. Explain the node and relationships in plain language. Do not invent facts. Prefer grounded statements.',
        },
      });

      const text = String(resp?.content || '').trim();
      if (!text) {
        setNodeExplainError('Empty response from model');
      } else {
        setNodeExplainText(text);
      }
    } catch (err: any) {
      console.error('[ObjectInspector] Explain node failed', err);
      setNodeExplainError(err.response?.data?.error || err.message || 'Failed to explain node');
    } finally {
      setNodeExplainLoading(false);
    }
  }, [nodeId, nodeData, fragment]);

  // Load edge data when edgeId changes
  React.useEffect(() => {
    if (!edgeId) {
      setEdge(null);
      setSourceFragment(null);
      setTargetFragment(null);
      setEdgeExplainError(null);
      setEdgeExplainText(null);
      setError(null);
      return;
    }

    const loadEdgeData = async () => {
      setLoading(true);
      setError(null);
      try {
        let loadedEdge: any = null;
        // Try to load edge from API if we have lensItemId
        if (edgeData?.lensItemId) {
          try {
            const edgeDataFromApi = await api.getHorizonEdge(edgeData.lensItemId);
            loadedEdge = edgeDataFromApi;
            setEdge(edgeDataFromApi);
          } catch (err) {
            // If API call fails, use the data from edgeData
            console.warn('[ObjectInspector] Failed to load edge from API, using provided data', err);
            loadedEdge = edgeData;
            setEdge(edgeData);
          }
        } else {
          // Use the data from edgeData directly
          loadedEdge = edgeData;
          setEdge(edgeData);
        }

        // Load source and target fragments if we have their IDs
        // Try to get from loadedEdge first, then from edgeData (passed from click handler)
        const sourceId = loadedEdge?.sourceFragmentId || edgeData?.sourceFragmentId;
        const targetId = loadedEdge?.targetFragmentId || edgeData?.targetFragmentId;

        if (sourceId) {
          try {
            const sourceFrag = await api.getHorizonFragment(sourceId);
            setSourceFragment(sourceFrag);
          } catch (err) {
            console.warn('[ObjectInspector] Failed to load source fragment', err);
            setSourceFragment(null);
          }
        } else {
          setSourceFragment(null);
        }

        if (targetId) {
          try {
            const targetFrag = await api.getHorizonFragment(targetId);
            setTargetFragment(targetFrag);
          } catch (err) {
            console.warn('[ObjectInspector] Failed to load target fragment', err);
            setTargetFragment(null);
          }
        } else {
          setTargetFragment(null);
        }
      } catch (err: any) {
        console.error('[ObjectInspector] Failed to load edge data', err);
        setError(err.response?.data?.error || err.message || 'Failed to load edge data');
      } finally {
        setLoading(false);
      }
    };

    loadEdgeData();
  }, [edgeId, edgeData]);

  const handleExplainEdge = React.useCallback(async () => {
    if (!edgeId) return;
    setEdgeExplainLoading(true);
    setEdgeExplainError(null);
    setEdgeExplainText(null);

    try {
      const preferredLanguage =
        (typeof navigator !== 'undefined' && navigator.language) ? navigator.language : 'en';

      const edgeType = String(edgeData?.edgeType || edge?.type || 'UNKNOWN');
      const sourceId = String(edgeData?.sourceFragmentId || edge?.sourceFragmentId || '').trim();
      const targetId = String(edgeData?.targetFragmentId || edge?.targetFragmentId || '').trim();

      const payload = {
        preferredLanguage,
        edge: {
          id: edgeId,
          edgeType,
          criticality: edgeData?.criticality ?? edge?.properties?.criticality,
          amplification: edgeData?.amplification ?? edge?.properties?.amplification,
          latencyHours: edgeData?.latencyHours ?? edge?.properties?.latencyHours,
        },
        source: {
          fragmentId: sourceId,
          label:
            sourceFragment?.labels?.en ||
            sourceFragment?.labels?.[Object.keys(sourceFragment?.labels || {})[0]] ||
            sourceFragment?.canonicalPath?.split('/')?.pop() ||
            null,
          canonicalPath: sourceFragment?.canonicalPath || null,
          type: sourceFragment?.type || null,
        },
        target: {
          fragmentId: targetId,
          label:
            targetFragment?.labels?.en ||
            targetFragment?.labels?.[Object.keys(targetFragment?.labels || {})[0]] ||
            targetFragment?.canonicalPath?.split('/')?.pop() ||
            null,
          canonicalPath: targetFragment?.canonicalPath || null,
          type: targetFragment?.type || null,
        },
      };

      const prompt =
        `Explain why this graph edge exists.\n` +
        `- Output language: ${preferredLanguage}\n` +
        `- Use Google Search grounding.\n` +
        `- Keep it concise (3-6 sentences). Plain text only.\n` +
        `- If you are unsure, say so explicitly.\n\n` +
        `Graph edge context (JSON):\n` +
        `${JSON.stringify(payload, null, 2)}\n`;

      const resp: any = await api.genaiComplete({
        prompt,
        options: {
          profile: 'fast',
          modelOverride: 'gemini-3-flash-preview',
          groundingEnabled: true,
          temperature: 0.2,
          maxOutputTokens: 2048,
          systemInstruction:
            'You are a careful analyst. Explain the edge in plain language. Do not invent facts. Prefer grounded statements.',
        },
      });

      const text = String(resp?.content || '').trim();
      if (!text) {
        setEdgeExplainError('Empty response from model');
      } else {
        setEdgeExplainText(text);
      }
    } catch (err: any) {
      console.error('[ObjectInspector] Explain edge failed', err);
      setEdgeExplainError(err.response?.data?.error || err.message || 'Failed to explain edge');
    } finally {
      setEdgeExplainLoading(false);
    }
  }, [edgeId, edgeData, edge, sourceFragment, targetFragment]);

  const handleMaterializeProductIO = React.useCallback(async () => {
    const fragmentId = nodeData?.fragmentId || nodeId;
    if (!lensId) {
      setProductIOError('Lens ID is missing (open this inspector from a Lens cockpit)');
      return;
    }
    if (!fragmentId) {
      setProductIOError('Fragment ID is missing');
      return;
    }

    setProductIOLoading(true);
    setProductIOError(null);
    setProductIOResult(null);

    try {
      const extracted = await api.extractProductIO(fragmentId, { direction: 'both', lang: 'en' });
      const applied = await api.applyProductIO(lensId, {
        objectiveFragmentId: fragmentId,
        produces: extracted.produces || [],
        consumes: extracted.consumes || [],
      });
      const nodesCount = Array.isArray(applied?.delta?.fragmentsUpserted) ? applied.delta.fragmentsUpserted.length : 0;
      const edgesCount = Array.isArray(applied?.delta?.edgesUpserted) ? applied.delta.edgesUpserted.length : 0;
      setProductIOResult({ nodes: nodesCount, edges: edgesCount });

      // Best-effort: expand the selected node to show newly materialized neighbors.
      const expand = (nodeData as any)?.onLoadRelated as undefined | ((id: string) => void);
      if (typeof expand === 'function') {
        expand(fragmentId);
      }
    } catch (err: any) {
      console.error('[ObjectInspector] Product IO materialization failed', err);
      setProductIOError(err.response?.data?.error || err.message || 'Failed to materialize Product IO');
    } finally {
      setProductIOLoading(false);
    }
  }, [lensId, nodeData]);

  if (!nodeId && !edgeId) {
    return (
      <div style={{ padding: '16px', textAlign: 'center', color: 'var(--text-secondary)' }}>
        Select a node or edge to inspect
      </div>
    );
  }

  return (
    <div style={{ padding: '8px 0' }}>
      {/* Loading indicator (do not block rendering; selection info should stay visible) */}
      {loading && (
        <div style={{ padding: '8px 12px', marginBottom: '12px', color: 'var(--text-secondary-strong)', fontSize: '12px' }}>
          Loading…
        </div>
      )}
      {/* Error message */}
      {error && (
        <div style={{ 
          padding: '12px', 
          marginBottom: '16px',
          borderRadius: '6px',
          backgroundColor: 'rgba(239, 68, 68, 0.1)',
          border: '1px solid rgba(239, 68, 68, 0.3)',
          color: '#f87171',
          fontSize: '13px',
        }}>
          {error}
        </div>
      )}
      
      {/* Edge Info */}
      {edgeId && (
        <>
          <div style={{ marginBottom: '24px' }}>
            <h4 style={{ 
              fontSize: '12px', 
              fontWeight: 600, 
              color: 'var(--text-heading)',
              marginBottom: '8px',
              textTransform: 'uppercase',
              letterSpacing: '0.5px',
            }}>
              Edge Information
            </h4>
            <div style={{ 
              padding: '12px',
              borderRadius: '8px',
              backgroundColor: 'var(--bg-elevated)',
              border: '1px solid var(--border-subtle)',
            }}>
              <button
                type="button"
                onClick={handleExplainEdge}
                disabled={edgeExplainLoading}
                style={{
                  padding: '8px 10px',
                  fontSize: '12px',
                  fontWeight: 700,
                  borderRadius: 8,
                  border: '1px solid var(--border-subtle)',
                  background: edgeExplainLoading ? 'var(--bg-secondary)' : 'var(--bg-tertiary)',
                  color: 'var(--text-heading)',
                  cursor: edgeExplainLoading ? 'not-allowed' : 'pointer',
                  opacity: edgeExplainLoading ? 0.7 : 1,
                  width: '100%',
                  marginBottom: 12,
                }}
                title="Explain this edge using Gemini 3 Flash Preview (with Search grounding)"
              >
                {edgeExplainLoading ? 'Explaining…' : 'Explain'}
              </button>
              {edgeExplainError && (
                <div style={{ fontSize: '12px', color: '#f87171', marginBottom: 10 }}>
                  {edgeExplainError}
                </div>
              )}
              {edgeExplainText && (
                <div
                  style={{
                    fontSize: '12px',
                    color: 'var(--text-heading)',
                    lineHeight: 1.45,
                    whiteSpace: 'pre-wrap',
                    padding: 10,
                    borderRadius: 8,
                    backgroundColor: 'var(--bg-secondary)',
                    border: '1px solid var(--border-subtle)',
                    marginBottom: 12,
                  }}
                >
                  {edgeExplainText}
                </div>
              )}
              <div style={{ marginBottom: '8px' }}>
                <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                  Edge ID
                </span>
                <span style={{ fontSize: '13px', color: 'var(--text-heading)', fontFamily: 'monospace' }}>
                  {edgeId}
                </span>
              </div>
              {edgeData?.lensItemId && (
                <div style={{ marginBottom: '8px' }}>
                  <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                    Lens Item ID
                  </span>
                  <span style={{ fontSize: '13px', color: 'var(--text-heading)', fontFamily: 'monospace' }}>
                    {edgeData.lensItemId}
                  </span>
                </div>
              )}
              {edge?.id && (
                <div style={{ marginBottom: '8px' }}>
                  <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                    Dependency Edge ID
                  </span>
                  <span style={{ fontSize: '13px', color: 'var(--text-heading)', fontFamily: 'monospace' }}>
                    {edge.id}
                  </span>
                </div>
              )}
              {(edgeData?.edgeType || edge?.type) && (
                <div style={{ marginBottom: '8px' }}>
                  <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                    Edge Type
                  </span>
                  <span style={{ fontSize: '13px', color: 'var(--text-heading)' }}>
                    {edgeData?.edgeType || edge?.type}
                  </span>
                </div>
              )}
              {(edge?.sourceFragmentId || edgeData?.sourceFragmentId) && (
                <div style={{ marginBottom: '8px' }}>
                  <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                    From (Source)
                  </span>
                  <div style={{ fontSize: '13px', color: 'var(--text-heading)' }}>
                    {sourceFragment ? (
                      <>
                        <div style={{ fontWeight: 500, marginBottom: '2px' }}>
                          {sourceFragment.labels?.en || sourceFragment.labels?.[Object.keys(sourceFragment.labels || {})[0]] || sourceFragment.canonicalPath?.split('/').pop() || 'Unknown'}
                        </div>
                        <div style={{ fontSize: '11px', color: 'var(--text-secondary)', fontFamily: 'monospace' }}>
                          {edge?.sourceFragmentId || edgeData?.sourceFragmentId}
                        </div>
                      </>
                    ) : (
                      <span style={{ fontFamily: 'monospace' }}>{edge?.sourceFragmentId || edgeData?.sourceFragmentId}</span>
                    )}
                  </div>
                </div>
              )}
              {(edge?.targetFragmentId || edgeData?.targetFragmentId) && (
                <div style={{ marginBottom: '8px' }}>
                  <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                    To (Target)
                  </span>
                  <div style={{ fontSize: '13px', color: 'var(--text-heading)' }}>
                    {targetFragment ? (
                      <>
                        <div style={{ fontWeight: 500, marginBottom: '2px' }}>
                          {targetFragment.labels?.en || targetFragment.labels?.[Object.keys(targetFragment.labels || {})[0]] || targetFragment.canonicalPath?.split('/').pop() || 'Unknown'}
                        </div>
                        <div style={{ fontSize: '11px', color: 'var(--text-secondary)', fontFamily: 'monospace' }}>
                          {edge?.targetFragmentId || edgeData?.targetFragmentId}
                        </div>
                      </>
                    ) : (
                      <span style={{ fontFamily: 'monospace' }}>{edge?.targetFragmentId || edgeData?.targetFragmentId}</span>
                    )}
                  </div>
                </div>
              )}
            </div>
          </div>

          {/* Edge Properties */}
          {(edgeData?.criticality !== undefined || edgeData?.amplification !== undefined || edgeData?.latencyHours !== undefined || 
            edge?.properties) && (
            <div style={{ marginBottom: '24px' }}>
              <h4 style={{ 
                fontSize: '12px', 
                fontWeight: 600, 
                color: 'var(--text-heading)',
                marginBottom: '8px',
                textTransform: 'uppercase',
                letterSpacing: '0.5px',
              }}>
                Edge Properties
              </h4>
              <div style={{ 
                padding: '12px',
                borderRadius: '8px',
                backgroundColor: 'var(--bg-elevated)',
                border: '1px solid var(--border-subtle)',
              }}>
                {(edgeData?.criticality !== undefined || edge?.properties?.criticality !== undefined) && (
                  <div style={{ marginBottom: '8px' }}>
                    <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                      Criticality
                    </span>
                    <span style={{ fontSize: '13px', color: 'var(--text-heading)', fontFamily: 'monospace' }}>
                      {edge?.properties?.criticality ?? edgeData?.criticality}
                    </span>
                  </div>
                )}
                {(edgeData?.amplification !== undefined || edge?.properties?.amplification !== undefined) && (
                  <div style={{ marginBottom: '8px' }}>
                    <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                      Amplification
                    </span>
                    <span style={{ fontSize: '13px', color: 'var(--text-heading)', fontFamily: 'monospace' }}>
                      {edge?.properties?.amplification ?? edgeData?.amplification}
                    </span>
                  </div>
                )}
                {(edgeData?.latencyHours !== undefined || edge?.properties?.latencyHours !== undefined) && (
                  <div style={{ marginBottom: '8px' }}>
                    <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                      Latency (hours)
                    </span>
                    <span style={{ fontSize: '13px', color: 'var(--text-heading)', fontFamily: 'monospace' }}>
                      {edge?.properties?.latencyHours ?? edgeData?.latencyHours}
                    </span>
                  </div>
                )}
                {edge?.direction && (
                  <div style={{ marginBottom: '8px' }}>
                    <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                      Direction
                    </span>
                    <span style={{ fontSize: '13px', color: 'var(--text-heading)' }}>
                      {edge.direction}
                    </span>
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Edge Qualifiers */}
          {edge?.qualifiers && Object.keys(edge.qualifiers).length > 0 && (
            <div style={{ marginBottom: '24px' }}>
              <h4 style={{ 
                fontSize: '12px', 
                fontWeight: 600, 
                color: 'var(--text-heading)',
                marginBottom: '8px',
                textTransform: 'uppercase',
                letterSpacing: '0.5px',
              }}>
                Qualifiers
              </h4>
              <div style={{ 
                padding: '12px',
                borderRadius: '8px',
                backgroundColor: 'var(--bg-elevated)',
                border: '1px solid var(--border-subtle)',
              }}>
                {Object.entries(edge.qualifiers).map(([key, value]) => (
                  <div key={key} style={{ marginBottom: '8px' }}>
                    <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                      {key}
                    </span>
                    <span style={{ fontSize: '13px', color: 'var(--text-heading)' }}>
                      {String(value)}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Edge Metadata */}
          {edge?.metadata && (
            <div style={{ marginBottom: '24px' }}>
              <h4 style={{ 
                fontSize: '12px', 
                fontWeight: 600, 
                color: 'var(--text-heading)',
                marginBottom: '8px',
                textTransform: 'uppercase',
                letterSpacing: '0.5px',
              }}>
                Metadata
              </h4>
              <div style={{ 
                padding: '12px',
                borderRadius: '8px',
                backgroundColor: 'var(--bg-elevated)',
                border: '1px solid var(--border-subtle)',
              }}>
                {edge.metadata.sourceFrameId && (
                  <div style={{ marginBottom: '8px' }}>
                    <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                      Source Frame ID
                    </span>
                    <span style={{ fontSize: '13px', color: 'var(--text-heading)', fontFamily: 'monospace' }}>
                      {edge.metadata.sourceFrameId}
                    </span>
                  </div>
                )}
                {edge.metadata.createdAt && (
                  <div style={{ marginBottom: '8px' }}>
                    <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                      Created At
                    </span>
                    <span style={{ fontSize: '13px', color: 'var(--text-heading)' }}>
                      {new Date(edge.metadata.createdAt).toLocaleString()}
                    </span>
                  </div>
                )}
                {edge.metadata.lastUpdatedAt && (
                  <div style={{ marginBottom: '8px' }}>
                    <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                      Last Updated At
                    </span>
                    <span style={{ fontSize: '13px', color: 'var(--text-heading)' }}>
                      {new Date(edge.metadata.lastUpdatedAt).toLocaleString()}
                    </span>
                  </div>
                )}
              </div>
            </div>
          )}
        </>
      )}
      
      {/* Node Info */}
      {nodeId && (
      <div style={{ marginBottom: '24px' }}>
        <h4 style={{ 
          fontSize: '12px', 
          fontWeight: 600, 
          color: 'var(--text-heading)',
          marginBottom: '8px',
          textTransform: 'uppercase',
          letterSpacing: '0.5px',
        }}>
          Node Information
        </h4>
        <div style={{ 
          padding: '12px',
          borderRadius: '8px',
          backgroundColor: 'var(--bg-elevated)',
          border: '1px solid var(--border-subtle)',
        }}>
          <button
            type="button"
            onClick={handleExplainNode}
            disabled={nodeExplainLoading}
            style={{
              padding: '8px 10px',
              fontSize: '12px',
              fontWeight: 700,
              borderRadius: 8,
              border: '1px solid var(--border-subtle)',
              background: nodeExplainLoading ? 'var(--bg-secondary)' : 'var(--bg-tertiary)',
              color: 'var(--text-heading)',
              cursor: nodeExplainLoading ? 'not-allowed' : 'pointer',
              opacity: nodeExplainLoading ? 0.7 : 1,
              width: '100%',
              marginBottom: 12,
            }}
            title="Explain this node using Gemini 3 Flash Preview (with Search grounding)"
          >
            {nodeExplainLoading ? 'Explaining…' : 'Explain'}
          </button>
          {nodeExplainError && (
            <div style={{ fontSize: '12px', color: '#f87171', marginBottom: 10 }}>
              {nodeExplainError}
            </div>
          )}
          {nodeExplainText && (
            <div
              style={{
                fontSize: '12px',
                color: 'var(--text-heading)',
                lineHeight: 1.45,
                whiteSpace: 'pre-wrap',
                padding: 10,
                borderRadius: 8,
                backgroundColor: 'var(--bg-secondary)',
                border: '1px solid var(--border-subtle)',
                marginBottom: 12,
              }}
            >
              {nodeExplainText}
            </div>
          )}
          <div style={{ marginBottom: '8px' }}>
            <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
              Node ID
            </span>
            <span style={{ fontSize: '13px', color: 'var(--text-heading)', fontFamily: 'monospace' }}>
              {nodeId}
            </span>
          </div>
          {(nodeData?.fragmentId || nodeId) && (
            <div style={{ marginBottom: '8px' }}>
              <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                Fragment ID
              </span>
              <span style={{ fontSize: '13px', color: 'var(--text-heading)', fontFamily: 'monospace' }}>
                {nodeData?.fragmentId || nodeId}
              </span>
            </div>
          )}
          {nodeData?.type && (
            <div style={{ marginBottom: '8px' }}>
              <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                Type
              </span>
              <span style={{ fontSize: '13px', color: 'var(--text-heading)' }}>
                {nodeData.type}
              </span>
            </div>
          )}
          {nodeData?.label && (
            <div>
              <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                Label
              </span>
              <span style={{ fontSize: '13px', color: 'var(--text-heading)' }}>
                {nodeData.label}
              </span>
            </div>
          )}
        </div>
      </div>
      )}

      {/* Fragment Info */}
      {nodeId && nodeData && fragment && (
        <div style={{ marginBottom: '24px' }}>
          <h4 style={{ 
            fontSize: '12px', 
            fontWeight: 600, 
            color: 'var(--text-heading)',
            marginBottom: '8px',
            textTransform: 'uppercase',
            letterSpacing: '0.5px',
          }}>
            Fragment Details
          </h4>
          <div style={{ 
            padding: '12px',
            borderRadius: '8px',
            backgroundColor: 'var(--bg-elevated)',
            border: '1px solid var(--border-subtle)',
          }}>
            {fragment.labels && Object.keys(fragment.labels).length > 0 && (
              <div style={{ marginBottom: '8px' }}>
                <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                  Labels
                </span>
                {Object.entries(fragment.labels).map(([lang, label]) => (
                  <div key={lang} style={{ fontSize: '13px', color: 'var(--text-heading)', marginBottom: '4px' }}>
                    <span style={{ fontSize: '11px', color: 'var(--text-secondary)', marginRight: '8px' }}>
                      {lang}:
                    </span>
                    {String(label)}
                  </div>
                ))}
              </div>
            )}
            {fragment.canonicalPath && (
              <div style={{ marginBottom: '8px' }}>
                <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                  Canonical Path
                </span>
                <span style={{ fontSize: '13px', color: 'var(--text-heading)', fontFamily: 'monospace', wordBreak: 'break-all' }}>
                  {fragment.canonicalPath}
                </span>
              </div>
            )}
            {fragment.type && (
              <div style={{ marginBottom: '8px' }}>
                <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                  Fragment Type
                </span>
                <span style={{ fontSize: '13px', color: 'var(--text-heading)' }}>
                  {fragment.type}
                </span>
              </div>
            )}
            {fragment.identifiers && Object.keys(fragment.identifiers).length > 0 && (
              <div>
                <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                  Identifiers
                </span>
                {Object.entries(fragment.identifiers).map(([key, value]) => (
                  <div key={key} style={{ fontSize: '13px', color: 'var(--text-heading)', marginBottom: '4px' }}>
                    <span style={{ fontSize: '11px', color: 'var(--text-secondary)', marginRight: '8px' }}>
                      {key}:
                    </span>
                    {String(value)}
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Hypothesis: Product IO (v1) */}
      {nodeData?.fragmentId && (
        <div style={{ marginBottom: '24px' }}>
          <h4 style={{ 
            fontSize: '12px', 
            fontWeight: 600, 
            color: 'var(--text-heading)',
            marginBottom: '8px',
            textTransform: 'uppercase',
            letterSpacing: '0.5px',
          }}>
            Hypothesis: Product IO (v1)
          </h4>
          <div style={{ 
            padding: '12px',
            borderRadius: '8px',
            backgroundColor: 'var(--bg-elevated)',
            border: '1px solid var(--border-subtle)',
          }}>
            <div style={{ fontSize: '12px', color: 'var(--text-secondary-strong)', marginBottom: 10, lineHeight: 1.35 }}>
              Extract products this object <b>produces</b> and <b>consumes</b>, then materialize them into the global Horizon graph.
              <div style={{ marginTop: 6, fontSize: '11px', color: 'var(--text-tertiary-weak)' }}>
                v1: PRODUCT-only. Fragment is primary; entity link is enrichment.
              </div>
            </div>
            {productIOError && (
              <div style={{ fontSize: '12px', color: '#f87171', marginBottom: 8 }}>
                {productIOError}
              </div>
            )}
            {productIOResult && (
              <div style={{ fontSize: '12px', color: 'var(--text-heading)', marginBottom: 8 }}>
                Materialized: <b>{productIOResult.nodes}</b> nodes, <b>{productIOResult.edges}</b> edges
              </div>
            )}
            <button
              type="button"
              onClick={handleMaterializeProductIO}
              disabled={productIOLoading || !lensId}
              style={{
                padding: '8px 10px',
                fontSize: '12px',
                fontWeight: 600,
                borderRadius: 8,
                border: '1px solid var(--border-subtle)',
                background: productIOLoading ? 'var(--bg-secondary)' : 'var(--bg-tertiary)',
                color: 'var(--text-heading)',
                cursor: productIOLoading || !lensId ? 'not-allowed' : 'pointer',
                opacity: productIOLoading || !lensId ? 0.7 : 1,
                width: '100%',
              }}
            >
              {productIOLoading ? 'Materializing…' : 'Materialize Product IO'}
            </button>
          </div>
        </div>
      )}

      {/* Entity Info */}
      {nodeId && nodeData && entity && (
        <div style={{ marginBottom: '24px' }}>
          <h4 style={{ 
            fontSize: '12px', 
            fontWeight: 600, 
            color: 'var(--text-heading)',
            marginBottom: '8px',
            textTransform: 'uppercase',
            letterSpacing: '0.5px',
          }}>
            Linked Entity
          </h4>
          <div style={{ 
            padding: '12px',
            borderRadius: '8px',
            backgroundColor: 'var(--bg-elevated)',
            border: '1px solid var(--border-subtle)',
          }}>
            {entity.canonicalName && (
              <div style={{ marginBottom: '8px' }}>
                <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                  Canonical Name
                </span>
                <span style={{ fontSize: '13px', color: 'var(--text-heading)' }}>
                  {entity.canonicalName}
                </span>
              </div>
            )}
            {entity.type && (
              <div style={{ marginBottom: '8px' }}>
                <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                  Entity Type
                </span>
                <span style={{ fontSize: '13px', color: 'var(--text-heading)' }}>
                  {entity.type}
                </span>
              </div>
            )}
            {entity.labels && Object.keys(entity.labels).length > 0 && (
              <div style={{ marginBottom: '8px' }}>
                <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                  Labels
                </span>
                {Object.entries(entity.labels).map(([lang, label]) => (
                  <div key={lang} style={{ fontSize: '13px', color: 'var(--text-heading)', marginBottom: '4px' }}>
                    <span style={{ fontSize: '11px', color: 'var(--text-secondary)', marginRight: '8px' }}>
                      {lang}:
                    </span>
                    {String(label)}
                  </div>
                ))}
              </div>
            )}
            {entity.homeLocation && (
              <div style={{ marginBottom: '8px' }}>
                <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                  Location
                </span>
                <div style={{ fontSize: '13px', color: 'var(--text-heading)' }}>
                  {entity.homeLocation.name && (
                    <div style={{ marginBottom: '4px', fontWeight: 500 }}>
                      {entity.homeLocation.name}
                    </div>
                  )}
                  <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
                    {entity.homeLocation.city && (
                      <div>
                        <span style={{ fontSize: '11px', color: 'var(--text-secondary)', marginRight: '4px' }}>
                          City:
                        </span>
                        {entity.homeLocation.city}
                      </div>
                    )}
                    {entity.homeLocation.admin1 && (
                      <div>
                        <span style={{ fontSize: '11px', color: 'var(--text-secondary)', marginRight: '4px' }}>
                          Region:
                        </span>
                        {entity.homeLocation.admin1}
                      </div>
                    )}
                    {entity.homeLocation.countryAlpha2 && (
                      <div>
                        <span style={{ fontSize: '11px', color: 'var(--text-secondary)', marginRight: '4px' }}>
                          Country:
                        </span>
                        {entity.homeLocation.countryAlpha2.toUpperCase()}
                      </div>
                    )}
                    {entity.homeLocation.iso6709 && (
                      <div style={{ fontFamily: 'monospace', fontSize: '12px', color: 'var(--text-secondary-strong)' }}>
                        <span style={{ fontSize: '11px', color: 'var(--text-secondary)', marginRight: '4px' }}>
                          Coordinates:
                        </span>
                        {entity.homeLocation.iso6709}
                      </div>
                    )}
                  </div>
                </div>
              </div>
            )}
            {entity.locationPath && (
              <div style={{ marginBottom: '8px' }}>
                <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                  Location Path
                </span>
                <span style={{ fontSize: '13px', color: 'var(--text-heading)', fontFamily: 'monospace', wordBreak: 'break-all' }}>
                  {entity.locationPath}
                </span>
              </div>
            )}
            {entity.kbRefs && entity.kbRefs.length > 0 && (
              <div>
                <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                  Knowledge Base References
                </span>
                {entity.kbRefs.map((ref: any, idx: number) => (
                  <div key={idx} style={{ fontSize: '13px', color: 'var(--text-heading)', marginBottom: '4px' }}>
                    <span style={{ fontSize: '11px', color: 'var(--text-secondary)', marginRight: '8px' }}>
                      {ref.kb}:
                    </span>
                    {ref.externalId || 'N/A'}
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}

      {/* Actions */}
      {nodeId && nodeData && fragment && (
        <div>
          <h4 style={{ 
            fontSize: '12px', 
            fontWeight: 600, 
            color: 'var(--text-heading)',
            marginBottom: '8px',
            textTransform: 'uppercase',
            letterSpacing: '0.5px',
          }}>
            Actions
          </h4>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
            <button
              onClick={async () => {
                if (!fragment.id || ensuring) return;
                setEnsuring(true);
                setError(null);
                try {
                  const result = await api.ensureFragmentEntity(fragment.id);
                  // Reload fragment and entity after ensure
                  const fragmentData = await api.getHorizonFragment(fragment.id);
                  setFragment(fragmentData);
                  try {
                    const entityData = await api.getFragmentEntity(fragment.id);
                    setEntity(entityData);
                  } catch (err) {
                    // Entity might not exist yet
                    setEntity(null);
                  }
                } catch (err: any) {
                  console.error('[ObjectInspector] Failed to ensure entity', err);
                  setError(err.response?.data?.error || err.message || 'Failed to ensure entity');
                } finally {
                  setEnsuring(false);
                }
              }}
              disabled={ensuring || enriching}
              style={{
                padding: '8px 12px',
                borderRadius: '6px',
                backgroundColor: ensuring ? 'var(--bg-secondary)' : 'var(--accent-primary)',
                color: 'var(--text-on-accent)',
                border: 'none',
                cursor: ensuring || enriching ? 'not-allowed' : 'pointer',
                fontSize: '13px',
                fontWeight: 500,
                opacity: ensuring || enriching ? 0.6 : 1,
              }}
            >
              {ensuring ? 'Updating...' : 'Ensure Entity'}
            </button>
            {entity && (
              <button
                onClick={async () => {
                  if (!fragment.id || enriching) return;
                  setEnriching(true);
                  setError(null);
                  try {
                    await api.enrichFragmentEntity(fragment.id, true);
                    // Reload entity after enrich
                    const entityData = await api.getFragmentEntity(fragment.id);
                    setEntity(entityData);
                    // Also reload fragment in case it was updated
                    const fragmentData = await api.getHorizonFragment(fragment.id);
                    setFragment(fragmentData);
                  } catch (err: any) {
                    console.error('[ObjectInspector] Failed to enrich entity', err);
                    setError(err.response?.data?.error || err.message || 'Failed to enrich entity');
                  } finally {
                    setEnriching(false);
                  }
                }}
                disabled={ensuring || enriching}
                style={{
                  padding: '8px 12px',
                  borderRadius: '6px',
                  backgroundColor: enriching ? 'var(--bg-secondary)' : 'var(--bg-elevated)',
                  color: 'var(--text-heading)',
                  border: '1px solid var(--border-subtle)',
                  cursor: ensuring || enriching ? 'not-allowed' : 'pointer',
                  fontSize: '13px',
                  fontWeight: 500,
                  opacity: ensuring || enriching ? 0.6 : 1,
                }}
              >
                {enriching ? 'Updating...' : 'Enrich Entity'}
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

