import React from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '../lib/api';
import { useGenAISettings, providerCapabilities } from '../stores/genaiSettingsStore';
import { TelegramFetchUpdatesTool } from '../lib/genaiTools';
import { useGenaiChatStore } from '../stores/genaiChatStore';
import { useDiscoveryStore } from '../stores/discoveryStore';
import { ThemeContext } from './ThemeProvider';
import AttachedDocuments from './AttachedDocuments';

interface AttachedDoc {
  id: string;
  filename: string;
  sizeBytes?: number;
  status: 'uploading' | 'parsing' | 'parsed' | 'failed';
  error?: string;
}

interface StatusMessage {
  id: string;
  type: 'warning' | 'error' | 'info';
  text: string;
  fadingOut: boolean;
}

interface UnfoldRequestProps {
  value: string;
  onChange: (v: string) => void;
  onSubmit: () => void;
  disabled?: boolean;
  dangerTint?: boolean;
  showHighRiskTag?: boolean;
  onHighRiskTagClick?: () => void;
}

const UnfoldRequest: React.FC<UnfoldRequestProps> = ({ value, onChange, onSubmit, disabled, dangerTint, showHighRiskTag, onHighRiskTagClick }) => {
  const { theme } = React.useContext(ThemeContext);
  const [showSettings, setShowSettings] = React.useState(false);
  const settings = useGenAISettings();
  const hasHistory = useGenaiChatStore((s) => (s.messages && s.messages.length > 0) || (s.interactions && s.interactions.length > 0));
  const { t } = useTranslation();
  const [attachedDocs, setAttachedDocs] = React.useState<AttachedDoc[]>([]);
  const lastDocEvent = useDiscoveryStore((s: any) => s.lastDocEvent);
  const [isDragging, setIsDragging] = React.useState(false);
  const [statusMessages, setStatusMessages] = React.useState<StatusMessage[]>([]);

  const initialPlaceholders = React.useMemo(() => {
    const arr = t('unfold.request.placeholders.initial', { returnObjects: true }) as string[] | undefined;
    return Array.isArray(arr) && arr.length ? arr : [
      t('unfold.request.placeholders.begin'),
    ];
  }, [t]);

  const ongoingPlaceholders = React.useMemo(() => {
    const arr = t('unfold.request.placeholders.ongoing', { returnObjects: true }) as string[] | undefined;
    return Array.isArray(arr) && arr.length ? arr : [
      t('unfold.request.placeholders.continue'),
    ];
  }, [t]);

  const [placeholderText, setPlaceholderText] = React.useState<string>(t('unfold.request.placeholders.begin'));

  React.useEffect(() => {
    if (hasHistory) {
      const idx = Math.floor(Math.random() * ongoingPlaceholders.length);
      setPlaceholderText(ongoingPlaceholders[idx] || t('unfold.request.placeholders.continue'));
    } else {
      const idx = Math.floor(Math.random() * initialPlaceholders.length);
      setPlaceholderText(initialPlaceholders[idx] || t('unfold.request.placeholders.helpToday'));
    }
    // We intentionally do not depend on value to avoid re-randomizing while typing
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hasHistory, ongoingPlaceholders, initialPlaceholders]);

  // Listen for document processing events from WebSocket
  React.useEffect(() => {
    if (!lastDocEvent) return;
    
    const { docId, status, error } = lastDocEvent;
    if (!docId) return;

    console.log('[Attach] WebSocket doc event', { docId, status, error });

    setAttachedDocs(prev => {
      const doc = prev.find(d => d.id === docId);
      if (!doc) return prev;

      return prev.map(d => {
        if (d.id !== docId) return d;
        
        // Map backend status to UI status
        let uiStatus: AttachedDoc['status'] = d.status;
        if (status === 'uploaded') uiStatus = 'parsing';
        else if (status === 'parsing') uiStatus = 'parsing';
        else if (status === 'parsed') uiStatus = 'parsed';
        else if (status === 'failed' || error) uiStatus = 'failed';
        
        return {
          ...d,
          status: uiStatus,
          error: error || d.error,
        };
      });
    });
  }, [lastDocEvent]);
  const containerStyle: React.CSSProperties = React.useMemo(() => ({
    borderColor: dangerTint ? 'rgba(220, 20, 60, 0.35)' : 'var(--overlay-line-90)',
    backgroundColor: 'var(--bg-elevated)',
    boxShadow: theme === 'light' ? '0 2px 10px rgba(0,0,0,0.06)' : 'none',
    backgroundImage: dangerTint ? 'linear-gradient(0deg, rgba(255,0,0,0.04), rgba(255,0,0,0.04))' : undefined,
  }), [theme, dangerTint]);

  // Supported file types (same as input accept attribute)
  const SUPPORTED_TYPES = React.useMemo(() => [
    'text/plain',
    'text/markdown',
    'text/csv',
    'text/tab-separated-values',
    'application/pdf',
    'application/msword',
    'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  ], []);

  const SUPPORTED_EXTENSIONS = React.useMemo(() => [
    '.txt', '.md', '.csv', '.tsv', '.pdf', '.doc', '.docx'
  ], []);

  const isFileSupported = React.useCallback((file: File): boolean => {
    if (SUPPORTED_TYPES.includes(file.type)) return true;
    // Check by extension as fallback
    const ext = '.' + file.name.split('.').pop()?.toLowerCase();
    return SUPPORTED_EXTENSIONS.includes(ext);
  }, [SUPPORTED_TYPES, SUPPORTED_EXTENSIONS]);

  const addStatusMessage = React.useCallback((type: StatusMessage['type'], text: string) => {
    const id = `status-${Date.now()}-${Math.random()}`;
    const message: StatusMessage = { id, type, text, fadingOut: false };
    
    setStatusMessages(prev => [...prev, message]);
    
    // Start fade out after 4.5 seconds
    setTimeout(() => {
      setStatusMessages(prev => prev.map(m => m.id === id ? { ...m, fadingOut: true } : m));
    }, 4500);
    
    // Remove after 5 seconds (fade out duration: 500ms)
    setTimeout(() => {
      setStatusMessages(prev => prev.filter(m => m.id !== id));
    }, 5000);
  }, []);

  const handleFileUpload = React.useCallback(async (file: File) => {
    if (!isFileSupported(file)) {
      console.warn('[Attach] Unsupported file type', { name: file.name, type: file.type });
      addStatusMessage('warning', `File type not supported: ${file.name}`);
      return;
    }

    // Create temporary doc entry with uploading status
    const tempId = `temp-${Date.now()}`;
    const tempDoc: AttachedDoc = {
      id: tempId,
      filename: file.name,
      sizeBytes: file.size,
      status: 'uploading',
    };
    setAttachedDocs(prev => [...prev, tempDoc]);
    
    try {
      // project id from chat store
      const { useGenaiChatStore } = await import('../stores/genaiChatStore');
      const store = useGenaiChatStore.getState();
      const pid = await store.ensureProject();
      console.log('[Attach] uploading', { name: file.name, size: file.size, type: file.type, projectId: pid });
      
      const doc = await api.docsUpload(pid, file);
      console.log('[Attach] uploaded', doc);
      
      // Update with real doc ID and parsing status
      setAttachedDocs(prev => prev.map(d => 
        d.id === tempId 
          ? { ...d, id: doc.id, status: 'parsing' as const }
          : d
      ));
      
      // Update store with real IDs
      const currentIds = store.pendingAttachedDocIds.filter(id => !id.startsWith('temp-'));
      store.setPendingAttachedDocIds([...currentIds, doc.id]);
      
      // Trigger parse and wait for completion
      try {
        await api.docsParse(pid, doc.id);
        setAttachedDocs(prev => prev.map(d => 
          d.id === doc.id 
            ? { ...d, status: 'parsed' as const }
            : d
        ));
      } catch (parseErr) {
        console.warn('[Attach] parse failed, but doc uploaded', parseErr);
        // Keep as parsing - will check status later
      }
    } catch (err) {
      console.error('[Attach] failed', err);
      // Mark as failed
      setAttachedDocs(prev => prev.map(d => 
        d.id === tempId 
          ? { ...d, status: 'failed' as const, error: 'Upload failed' }
          : d
      ));
    }
  }, [isFileSupported, addStatusMessage]);

  const handleRemoveDoc = React.useCallback(async (docId: string) => {
    // Remove from UI immediately
    setAttachedDocs(prev => prev.filter(d => d.id !== docId));
    
    // Update store
    const { useGenaiChatStore } = await import('../stores/genaiChatStore');
    const store = useGenaiChatStore.getState();
    const currentIds = store.pendingAttachedDocIds.filter(id => id !== docId);
    store.setPendingAttachedDocIds(currentIds);
    
    // Delete from server if not a temp ID
    if (!docId.startsWith('temp-')) {
      try {
        const pid = store.projectId;
        if (pid && pid !== 'local-unfold') {
          await api.docsDelete(pid, docId);
          console.log('[Attach] deleted from server', { docId });
        }
      } catch (err) {
        console.error('[Attach] failed to delete from server', err);
      }
    }
  }, []);

  return (
    <>
      {/* Status messages banner */}
      {statusMessages.length > 0 && (
        <div className="mb-2 space-y-1">
          {statusMessages.map((msg) => (
            <div
              key={msg.id}
              className="rounded-lg px-3 py-2 flex items-start gap-2 transition-opacity duration-500"
              style={{
                backgroundColor: msg.type === 'error' ? 'rgba(248, 113, 113, 0.1)' : 
                                msg.type === 'warning' ? 'rgba(251, 191, 36, 0.1)' : 
                                'rgba(96, 165, 250, 0.1)',
                border: `1px solid ${msg.type === 'error' ? 'rgba(248, 113, 113, 0.3)' : 
                                      msg.type === 'warning' ? 'rgba(251, 191, 36, 0.3)' : 
                                      'rgba(96, 165, 250, 0.3)'}`,
                opacity: msg.fadingOut ? 0 : 1,
              }}
            >
              {/* Icon */}
              <div className="flex-shrink-0 mt-0.5" style={{ 
                color: msg.type === 'error' ? '#f87171' : 
                       msg.type === 'warning' ? '#fbbf24' : 
                       '#60a5fa' 
              }}>
                {msg.type === 'error' && (
                  <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                    <circle cx="12" cy="12" r="10" strokeWidth="2" />
                    <path d="M12 8v4M12 16h.01" strokeWidth="2" strokeLinecap="round" />
                  </svg>
                )}
                {msg.type === 'warning' && (
                  <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                    <path d="M12 9v4M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0zM12 17h.01" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                )}
                {msg.type === 'info' && (
                  <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                    <circle cx="12" cy="12" r="10" strokeWidth="2" />
                    <path d="M12 16v-4M12 8h.01" strokeWidth="2" strokeLinecap="round" />
                  </svg>
                )}
              </div>
              
              {/* Message text */}
              <div className="flex-1 text-xs" style={{ 
                color: msg.type === 'error' ? '#f87171' : 
                       msg.type === 'warning' ? '#fbbf24' : 
                       '#60a5fa' 
              }}>
                {msg.text}
              </div>
            </div>
          ))}
        </div>
      )}
      
      {/* Attached documents list (shown above input) */}
      <AttachedDocuments docs={attachedDocs} onRemove={handleRemoveDoc} />
      
      <div 
        id="unfold-request-container" 
        className="rounded-xl border overflow-hidden mt-2 relative" 
        style={{
          ...containerStyle,
          transition: 'all 0.2s ease',
          ...(isDragging ? {
            borderColor: 'var(--text-secondary)',
            backgroundColor: 'var(--white-10)',
            boxShadow: '0 0 0 2px var(--white-06)',
          } : {}),
        }}
        onDragOver={(e) => {
          e.preventDefault();
          e.stopPropagation();
          if (!isDragging) setIsDragging(true);
        }}
        onDragEnter={(e) => {
          e.preventDefault();
          e.stopPropagation();
          setIsDragging(true);
        }}
        onDragLeave={(e) => {
          e.preventDefault();
          e.stopPropagation();
          // Only set to false if leaving the container itself (not child elements)
          if (e.currentTarget === e.target) {
            setIsDragging(false);
          }
        }}
        onDrop={(e) => {
          e.preventDefault();
          e.stopPropagation();
          setIsDragging(false);
          
          const files = Array.from(e.dataTransfer.files);
          console.log('[Attach] Drop files', { count: files.length });
          
          // Upload each supported file
          files.forEach(file => {
            handleFileUpload(file); // handleFileUpload will show warning if unsupported
          });
        }}
      >
        {/* Drag overlay */}
        {isDragging && (
          <div 
            className="absolute inset-0 flex items-center justify-center pointer-events-none z-10"
            style={{
              backgroundColor: 'var(--white-06)',
              backdropFilter: 'blur(2px)',
            }}
          >
            <div 
              className="flex flex-col items-center gap-2 px-4 py-3 rounded-lg"
              style={{
                backgroundColor: 'var(--bg-elevated)',
                border: '2px dashed var(--text-secondary)',
                color: 'var(--text-heading)',
              }}
            >
              <svg className="w-8 h-8" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                <path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4M17 8l-5-5-5 5M12 3v12" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
              <span className="text-sm font-medium">Drop files to attach</span>
            </div>
          </div>
        )}
        
        <div className="p-3">
        {/* Top: input field only */}
        <div className="mb-2">
          <textarea
            rows={3}
            value={value}
            onChange={(e) => onChange(e.target.value)}
            placeholder={placeholderText}
            className="w-full resize-none bg-transparent outline-none"
            style={{ backgroundColor: 'transparent', border: 'none', outline: 'none', color: 'var(--text-heading)' }}
            aria-label={t('unfold.request.ariaLabel')}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                e.preventDefault();
                onSubmit();
                // Clear attached docs after sending message
                setAttachedDocs([]);
              }
            }}
          />
        </div>
        {/* Bottom: controls row (icons) */}
        <div className="flex items-center justify-between gap-2">
          {/* Strength selector (synced with settings) — left side */}
          <div className="flex items-center gap-2">
            {/* Attach button */}
            <label className="flex items-center justify-center rounded-full cursor-pointer" style={{ width: 32, height: 32 }} title={t('unfold.request.attachTitle')}>
              <input
                type="file"
                accept=".txt,.md,.csv,.tsv,application/pdf,application/msword,application/vnd.openxmlformats-officedocument.wordprocessingml.document,text/plain,text/markdown,text/csv,text/tab-separated-values"
                style={{ display: 'none' }}
                onChange={async (e) => {
                  const file = e.target.files?.[0];
                  if (!file) return;
                  
                  await handleFileUpload(file);
                  e.currentTarget.value = '';
                }}
              />
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" aria-hidden="true">
                <path d="M21 15V7a4 4 0 00-4-4h-3a4 4 0 00-4 4v10a3 3 0 106 0V8" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
              </svg>
            </label>
            <label className="sr-only">{t('unfold.request.modelStrengthLabel')}</label>
            <select
              aria-label={t('unfold.request.modelStrengthLabel')}
              value={settings.strength}
              onChange={(e) => settings.setStrength(e.target.value as any)}
              className="text-xs"
              style={{ background: 'transparent', color: 'var(--text-heading)', border: '1px solid var(--overlay-line-90)', borderRadius: 8, padding: '4px 8px' }}
            >
              <option value="fast">{t('unfold.request.strengthFast')}</option>
              <option value="balanced" disabled={settings.provider === 'grok'}>{t('unfold.request.strengthBalanced')}</option>
              <option value="quality">{t('unfold.request.strengthQuality')}</option>
            </select>
            {/* Web search toggle (synced with modal) */}
            <button
              type="button"
              aria-label={t('unfold.request.webSearch')}
              aria-pressed={settings.groundingEnabled}
              disabled={!providerCapabilities[settings.provider].grounding}
              onClick={() => settings.setGrounding(!settings.groundingEnabled)}
              className="text-xs rounded flex items-center gap-1"
              style={{
                padding: '4px 8px',
                border: '1px solid var(--overlay-line-90)',
                background: settings.groundingEnabled ? 'var(--white-10)' : 'transparent',
                color: 'var(--text-heading)',
                opacity: !providerCapabilities[settings.provider].grounding ? 0.5 : 1,
              }}
            >
              <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" aria-hidden="true">
                <circle cx="12" cy="12" r="9" strokeWidth={2} />
                <path d="M3 12h18" strokeWidth={2} />
                <path d="M12 3a15 15 0 010 18M12 3a15 15 0 000 18" strokeWidth={2} />
              </svg>
              <span>{t('unfold.request.webSearch')}</span>
            </button>
          </div>
          <div className="flex items-center gap-2">
            {showHighRiskTag && (
              <button
                type="button"
                aria-label={t('unfold.request.highRiskConversation')}
                title={t('unfold.request.highRiskConversation')}
                onClick={() => { onHighRiskTagClick && onHighRiskTagClick(); }}
                className="flex items-center gap-1 px-2 py-1 rounded-full"
                style={{
                  background: 'rgba(255,0,0,0.08)',
                  border: '1px solid rgba(220,20,60,0.45)',
                  color: '#cc3344',
                  fontSize: 11,
                  fontWeight: 600,
                }}
              >
                <span style={{ display: 'inline-block', width: 6, height: 6, borderRadius: 9999, backgroundColor: '#cc3344' }} />
                <span>{t('unfold.request.highRiskTag')}</span>
              </button>
            )}
            <button
              type="button"
              aria-label={t('unfold.request.settings')}
              className="flex items-center justify-center rounded-full"
              onClick={() => setShowSettings(true)}
              style={{ width: '32px', height: '32px', backgroundColor: 'transparent' }}
            >
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                <circle cx="12" cy="12" r="4.5" strokeWidth={2} />
                <path strokeLinecap="round" strokeWidth={2} d="M12 3v2M12 19v2M3 12h2M19 12h2M5.636 5.636l1.414 1.414M16.95 7.05l1.414-1.414M18.364 18.364l-1.414-1.414M7.05 16.95l-1.414 1.414" />
              </svg>
            </button>
            <button
              type="button"
              aria-label={t('unfold.request.send')}
              className="flex items-center justify-center rounded-full"
              onClick={() => { 
                if (!disabled) {
                  onSubmit();
                  // Clear attached docs after sending message
                  setAttachedDocs([]);
                }
              }}
              disabled={disabled}
              style={{
                width: '32px',
                height: '32px',
                backgroundColor: 'transparent',
                opacity: disabled ? 0.6 : 1,
              }}
            >
              <svg className="w-6 h-6" viewBox="0 0 24 24" aria-hidden="true">
                <path d="M8 5v14l11-7z" fill="var(--text-secondary-strong)" />
              </svg>
            </button>
          </div>
        </div>
      </div>
      {showSettings && (
        <div className="fixed inset-0 z-50 flex items-center justify-center" role="dialog" aria-modal="true">
          <div className="absolute inset-0 bg-black/30" onClick={() => setShowSettings(false)} />
          <div className="relative w-full max-w-lg rounded-xl border p-4" style={{ backgroundColor: 'var(--bg-elevated)', borderColor: 'var(--overlay-line-90)' }}>
            <div className="flex items-center justify-between mb-3">
              <div className="text-lg" style={{ color: 'var(--text-heading)' }}>{t('unfold.request.settingsModal.title')}</div>
              <button onClick={() => setShowSettings(false)} aria-label="Close" className="rounded-full" style={{ width: 32, height: 32 }}>
                <svg viewBox="0 0 24 24" className="w-6 h-6"><path d="M6 6l12 12M18 6L6 18" stroke="var(--text-secondary-strong)" strokeWidth="2"/></svg>
              </button>
            </div>
            <div className="space-y-3">
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-sm mb-1" style={{ color: 'var(--text-secondary-strong)' }}>{t('unfold.request.settingsModal.provider')}</label>
                  <select value={settings.provider} onChange={(e) => settings.setProvider(e.target.value as any)} className="w-full">
                    <option value="openai">OpenAI</option>
                    <option value="gemini">Gemini</option>
                    <option value="grok">Grok</option>
                  </select>
                </div>
                <div>
                  <label className="block text-sm mb-1" style={{ color: 'var(--text-secondary-strong)' }}>{t('unfold.request.settingsModal.strength')}</label>
            <select value={settings.strength} onChange={(e) => settings.setStrength(e.target.value as any)} className="w-full">
              <option value="fast">{t('unfold.request.strengthFast')}</option>
              <option value="balanced">{t('unfold.request.strengthBalanced')}</option>
              <option value="quality">{t('unfold.request.strengthQuality')}</option>
                  </select>
                </div>
              </div>
              {/* Stance evaluation model */}
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-sm mb-1" style={{ color: 'var(--text-secondary-strong)' }}>{t('unfold.request.settingsModal.stanceProvider')}</label>
                  <select value={settings.stanceProvider || 'gemini'} onChange={(e) => settings.setStanceProvider(e.target.value as any)} className="w-full">
                    <option value="openai">OpenAI</option>
                    <option value="gemini">Gemini</option>
                    <option value="grok">Grok</option>
                  </select>
                </div>
                <div>
                  <label className="block text-sm mb-1" style={{ color: 'var(--text-secondary-strong)' }}>{t('unfold.request.settingsModal.stanceStrength')}</label>
                  <select value={settings.stanceStrength || 'fast'} onChange={(e) => settings.setStanceStrength(e.target.value as any)} className="w-full">
                    <option value="fast">{t('unfold.request.strengthFast')}</option>
                    <option value="balanced" disabled={(settings.stanceProvider || 'gemini') === 'grok'}>{t('unfold.request.strengthBalanced')}</option>
                    <option value="quality">{t('unfold.request.strengthQuality')}</option>
                  </select>
                </div>
              </div>
              <div>
                <label className="block text-sm mb-1" style={{ color: 'var(--text-secondary-strong)' }}>{t('unfold.request.settingsModal.stanceModel')}</label>
                <input value={settings.stanceModel || 'gemini_fast'} onChange={(e) => settings.setStanceModel(e.target.value as any)} className="w-full" />
                <div className="text-[11px] mt-1" style={{ color: 'var(--text-secondary)' }}>{t('unfold.request.settingsModal.stanceModelHelp')}</div>
              </div>
              <div className="flex items-center justify-between">
                <div>
                  <div className="text-sm" style={{ color: 'var(--text-secondary-strong)' }}>{t('unfold.request.settingsModal.groundingTitle')}</div>
                  <div className="text-xs" style={{ color: 'var(--text-secondary)' }}>{t('unfold.request.settingsModal.groundingDesc')}</div>
                </div>
                <input type="checkbox" checked={settings.groundingEnabled} onChange={(e) => settings.setGrounding(e.target.checked)} disabled={!providerCapabilities[settings.provider].grounding} />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-sm mb-1" style={{ color: 'var(--text-secondary-strong)' }}>{t('unfold.request.settingsModal.temperature')}</label>
                  <input type="number" step={0.1} min={0} max={2} value={settings.temperature} onChange={(e) => settings.setTemperature(parseFloat(e.target.value))} className="w-full" />
                </div>
                <div>
                  <label className="block text-sm mb-1" style={{ color: 'var(--text-secondary-strong)' }}>{t('unfold.request.settingsModal.maxTokens')}</label>
                  <input type="number" min={16} max={8192} value={settings.maxOutputTokens} onChange={(e) => settings.setMaxOutputTokens(parseInt(e.target.value || '0', 10))} className="w-full" />
                </div>
              </div>
              <div>
                <label className="block text-sm mb-1" style={{ color: 'var(--text-secondary-strong)' }}>{t('unfold.request.settingsModal.systemInstruction')}</label>
                <textarea rows={3} value={settings.systemInstruction} onChange={(e) => settings.setSystemInstruction(e.target.value)} className="w-full" />
              </div>
              <div className="flex items-center justify-between">
                <div>
                  <div className="text-sm" style={{ color: 'var(--text-secondary-strong)' }}>{t('unfold.request.settingsModal.enableTelegramTool')}</div>
                  <div className="text-xs" style={{ color: 'var(--text-secondary)' }}>{t('unfold.request.settingsModal.enableTelegramToolDesc')}</div>
                </div>
                <input type="checkbox" checked={settings.enableTelegramTool} onChange={(e) => settings.setEnableTelegramTool(e.target.checked)} disabled={!providerCapabilities[settings.provider].tools} />
              </div>
            </div>
            <div className="mt-4 text-right">
              <button
                className="px-3 py-2 rounded"
                onClick={() => { settings.save(); setShowSettings(false); }}
                style={{ background: 'var(--btn-primary-bg)', color: 'var(--btn-primary-fg)' }}
              >
                {t('unfold.request.settingsModal.done')}
              </button>
            </div>
          </div>
        </div>
      )}
      </div>
    </>
  );
};

export default UnfoldRequest;


