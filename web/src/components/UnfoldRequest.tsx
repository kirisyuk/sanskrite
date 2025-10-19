import React from 'react';
import { useGenAISettings, modelCapabilities } from '../stores/genaiSettingsStore';
import { TelegramFetchUpdatesTool } from '../lib/genaiTools';
import { ThemeContext } from './ThemeProvider';

interface UnfoldRequestProps {
  value: string;
  onChange: (v: string) => void;
  onSubmit: () => void;
  disabled?: boolean;
}

const UnfoldRequest: React.FC<UnfoldRequestProps> = ({ value, onChange, onSubmit, disabled }) => {
  const { theme } = React.useContext(ThemeContext);
  const [showSettings, setShowSettings] = React.useState(false);
  const settings = useGenAISettings();
  const containerStyle: React.CSSProperties = {
    borderColor: 'var(--overlay-line-90)',
    backgroundColor: 'var(--bg-elevated)',
    boxShadow: theme === 'light' ? '0 2px 10px rgba(0,0,0,0.06)' : 'none',
  };

  return (
    <div className="rounded-xl border overflow-hidden mt-2" style={containerStyle}>
      <div className="p-3">
        <div className="flex items-end gap-2">
          <div className="flex-1">
            <textarea
              rows={3}
              value={value}
              onChange={(e) => onChange(e.target.value)}
              placeholder="Unfold the story — drop links or @handles to reveal hidden connections…"
              className="w-full resize-none bg-transparent outline-none"
              style={{ backgroundColor: 'transparent', border: 'none', outline: 'none', color: 'var(--text-heading)' }}
              aria-label="Unfold query"
              onKeyDown={(e) => {
                if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                  e.preventDefault();
                  onSubmit();
                }
              }}
            />
          </div>
          {/* Settings button placed next to Send, matching sidebar icon */}
          <button
            type="button"
            aria-label="Settings"
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
            aria-label="Send"
            className="flex items-center justify-center rounded-full"
            onClick={() => { if (!disabled) onSubmit(); }}
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
      {showSettings && (
        <div className="fixed inset-0 z-50 flex items-center justify-center" role="dialog" aria-modal="true">
          <div className="absolute inset-0 bg-black/30" onClick={() => setShowSettings(false)} />
          <div className="relative w-full max-w-lg rounded-xl border p-4" style={{ backgroundColor: 'var(--bg-elevated)', borderColor: 'var(--overlay-line-90)' }}>
            <div className="flex items-center justify-between mb-3">
              <div className="text-lg" style={{ color: 'var(--text-heading)' }}>GenAI Settings</div>
              <button onClick={() => setShowSettings(false)} aria-label="Close" className="rounded-full" style={{ width: 32, height: 32 }}>
                <svg viewBox="0 0 24 24" className="w-6 h-6"><path d="M6 6l12 12M18 6L6 18" stroke="var(--text-secondary-strong)" strokeWidth="2"/></svg>
              </button>
            </div>
            <div className="space-y-3">
              <div>
                <label className="block text-sm mb-1" style={{ color: 'var(--text-secondary-strong)' }}>Model</label>
                <select value={settings.model} onChange={(e) => settings.setModel(e.target.value as any)} className="w-full">
                  <option value="gemini_quality">gemini_quality (gemini-2.5-pro)</option>
                  <option value="gemini_fast">gemini_fast (gemini-2.5-flash-lite)</option>
                  <option value="gpt_quality">gpt_quality (gpt-5)</option>
                  <option value="gpt_balanced">gpt_balanced (gpt-5-mini)</option>
                  <option value="gpt_fast">gpt_fast (gpt-5-nano)</option>
                </select>
              </div>
              <div className="flex items-center justify-between">
                <div>
                  <div className="text-sm" style={{ color: 'var(--text-secondary-strong)' }}>Search Grounding</div>
                  <div className="text-xs" style={{ color: 'var(--text-secondary)' }}>Включить Google Search grounding</div>
                </div>
                <input type="checkbox" checked={settings.groundingEnabled} onChange={(e) => settings.setGrounding(e.target.checked)} disabled={!modelCapabilities[settings.model].grounding} />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block text-sm mb-1" style={{ color: 'var(--text-secondary-strong)' }}>Temperature</label>
                  <input type="number" step={0.1} min={0} max={2} value={settings.temperature} onChange={(e) => settings.setTemperature(parseFloat(e.target.value))} className="w-full" />
                </div>
                <div>
                  <label className="block text-sm mb-1" style={{ color: 'var(--text-secondary-strong)' }}>Max tokens</label>
                  <input type="number" min={16} max={8192} value={settings.maxOutputTokens} onChange={(e) => settings.setMaxOutputTokens(parseInt(e.target.value || '0', 10))} className="w-full" />
                </div>
              </div>
              <div>
                <label className="block text-sm mb-1" style={{ color: 'var(--text-secondary-strong)' }}>System instruction</label>
                <textarea rows={3} value={settings.systemInstruction} onChange={(e) => settings.setSystemInstruction(e.target.value)} className="w-full" />
              </div>
              <div className="flex items-center justify-between">
                <div>
                  <div className="text-sm" style={{ color: 'var(--text-secondary-strong)' }}>Enable Telegram tool</div>
                  <div className="text-xs" style={{ color: 'var(--text-secondary)' }}>Добавить telegram_fetch_updates в tools</div>
                </div>
                <input type="checkbox" checked={settings.enableTelegramTool} onChange={(e) => settings.setEnableTelegramTool(e.target.checked)} disabled={!modelCapabilities[settings.model].tools} />
              </div>
            </div>
            <div className="mt-4 text-right">
              <button
                className="px-3 py-2 rounded"
                onClick={() => { settings.save(); setShowSettings(false); }}
                style={{ background: 'var(--btn-primary-bg)', color: 'var(--btn-primary-fg)' }}
              >
                Done
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export default UnfoldRequest;


