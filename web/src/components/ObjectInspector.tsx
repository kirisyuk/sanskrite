import React from 'react';
import { api } from '../lib/api';
import type { HorizonNodeData } from './HorizonGraphNodeTypes';

interface ObjectInspectorProps {
  nodeId: string | null;
  nodeData: HorizonNodeData | null;
}

export const ObjectInspector: React.FC<ObjectInspectorProps> = ({
  nodeId,
  nodeData,
}) => {
  const [fragment, setFragment] = React.useState<any>(null);
  const [entity, setEntity] = React.useState<any>(null);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [ensuring, setEnsuring] = React.useState(false);
  const [enriching, setEnriching] = React.useState(false);

  // Load fragment and entity data when nodeId changes
  React.useEffect(() => {
    if (!nodeId || !nodeData?.fragmentId) {
      setFragment(null);
      setEntity(null);
      setError(null);
      return;
    }

    const loadData = async () => {
      setLoading(true);
      setError(null);
      try {
        const fragmentId = nodeData.fragmentId;
        
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
  }, [nodeId, nodeData?.fragmentId]);

  if (!nodeId || !nodeData) {
    return (
      <div style={{ padding: '16px', textAlign: 'center', color: 'var(--text-secondary)' }}>
        Select a node to inspect
      </div>
    );
  }

  if (loading) {
    return (
      <div style={{ padding: '16px', textAlign: 'center', color: 'var(--text-secondary)' }}>
        Loading...
      </div>
    );
  }

  return (
    <div style={{ padding: '8px 0' }}>
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
      
      {/* Node Info */}
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
          <div style={{ marginBottom: '8px' }}>
            <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
              Node ID
            </span>
            <span style={{ fontSize: '13px', color: 'var(--text-heading)', fontFamily: 'monospace' }}>
              {nodeId}
            </span>
          </div>
          {nodeData.fragmentId && (
            <div style={{ marginBottom: '8px' }}>
              <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                Fragment ID
              </span>
              <span style={{ fontSize: '13px', color: 'var(--text-heading)', fontFamily: 'monospace' }}>
                {nodeData.fragmentId}
              </span>
            </div>
          )}
          {nodeData.type && (
            <div style={{ marginBottom: '8px' }}>
              <span style={{ fontSize: '11px', color: 'var(--text-secondary)', display: 'block', marginBottom: '4px' }}>
                Type
              </span>
              <span style={{ fontSize: '13px', color: 'var(--text-heading)' }}>
                {nodeData.type}
              </span>
            </div>
          )}
          {nodeData.label && (
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

      {/* Fragment Info */}
      {fragment && (
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

      {/* Entity Info */}
      {entity && (
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
      {fragment && (
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

