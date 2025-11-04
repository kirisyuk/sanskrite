import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { api } from '../lib/api';
import { useAuth } from '../contexts/AuthContext';

export default function VerifiedPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { user } = useAuth();
  const [url, setUrl] = useState('');
  const [model, setModel] = useState('gemini_fast'); // default model
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<{ code: string; url: string } | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!url.trim()) {
      setError(t('verified.form.urlRequired'));
      return;
    }

    // Basic URL validation
    try {
      new URL(url.trim());
    } catch {
      setError(t('verified.form.invalidUrl'));
      return;
    }

    setLoading(true);
    setError(null);
    setResult(null);

    try {
      // Pass model only if user is authenticated
      const selectedModel = user ? model : undefined;
      const res = await api.verifiedCreate(url.trim(), selectedModel);
      setResult({ code: res.code, url: res.url });
      // Auto-navigate after 2 seconds
      setTimeout(() => {
        navigate(`/verified/${res.code}`);
      }, 2000);
    } catch (e: any) {
      setError(e?.response?.data?.message || t('verified.form.error'));
      setLoading(false);
    }
  };

  const handleCopyUrl = async () => {
    if (!result) return;
    const fullUrl = `${window.location.origin}/verified/${result.code}`;
    try {
      await navigator.clipboard.writeText(fullUrl);
      // Show feedback (could use toast library if available)
    } catch (err) {
      // Fallback
      const textArea = document.createElement('textarea');
      textArea.value = fullUrl;
      document.body.appendChild(textArea);
      textArea.select();
      document.execCommand('copy');
      document.body.removeChild(textArea);
    }
  };

  return (
    <div className="content-area" style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', minHeight: 'calc(100vh - 88px)' }}>
      <div className="w-full max-w-2xl px-4">
        <div className="mb-6">
          <h1 className="text-2xl font-semibold mb-2" style={{ color: 'var(--text-heading)' }}>
            {t('verified.title')}
          </h1>
          <p className="text-13" style={{ color: 'var(--text-secondary)' }}>
            {t('verified.description')}
          </p>
        </div>

        {!result ? (
          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label htmlFor="url-input" className="block text-13 mb-2" style={{ color: 'var(--text-primary)' }}>
                {t('verified.form.urlLabel')}
              </label>
              <input
                id="url-input"
                type="url"
                value={url}
                onChange={(e) => {
                  setUrl(e.target.value);
                  setError(null);
                }}
                placeholder={t('verified.form.urlPlaceholder')}
                disabled={loading}
                className="w-full px-4 py-3 rounded-lg text-13"
                style={{
                  backgroundColor: 'var(--bg-elevated)',
                  border: error ? '1px solid var(--accent-danger)' : '1px solid var(--border-subtle)',
                  color: 'var(--text-primary)',
                  outline: 'none',
                }}
                onFocus={(e) => {
                  e.currentTarget.style.borderColor = 'var(--accent-primary)';
                }}
                onBlur={(e) => {
                  e.currentTarget.style.borderColor = 'var(--border-subtle)';
                }}
              />
              {error && (
                <p className="text-12 mt-2" style={{ color: 'var(--accent-danger)' }}>
                  {error}
                </p>
              )}
            </div>

            {user && (
              <div>
                <label htmlFor="model-select" className="block text-13 mb-2" style={{ color: 'var(--text-primary)' }}>
                  {t('verified.form.modelLabel', 'AI Model')}
                </label>
                <select
                  id="model-select"
                  value={model}
                  onChange={(e) => setModel(e.target.value)}
                  disabled={loading}
                  className="w-full px-4 py-3 rounded-lg text-13"
                  style={{
                    backgroundColor: 'var(--bg-elevated)',
                    border: '1px solid var(--border-subtle)',
                    color: 'var(--text-primary)',
                    outline: 'none',
                    cursor: loading ? 'not-allowed' : 'pointer',
                  }}
                >
                  <option value="gemini_fast">Gemini Fast</option>
                  <option value="gemini_quality">Gemini Quality</option>
                  <option value="grok_fast">Grok Fast</option>
                  <option value="grok_quality">Grok Quality</option>
                  <option value="gpt_fast">GPT Fast</option>
                  <option value="gpt_quality">GPT Quality</option>
                  <option value="claude_fast">Claude Fast</option>
                  <option value="claude_balanced">Claude Balanced</option>
                </select>
              </div>
            )}

            <button
              type="submit"
              disabled={loading || !url.trim()}
              className="w-full py-3 rounded-lg text-13 font-medium transition-colors"
              style={{
                backgroundColor: loading || !url.trim() ? 'var(--bg-disabled)' : 'var(--accent-primary)',
                color: loading || !url.trim() ? 'var(--text-disabled)' : 'var(--text-on-accent)',
                cursor: loading || !url.trim() ? 'not-allowed' : 'pointer',
              }}
            >
              {loading ? t('verified.form.processing') : t('verified.form.submit')}
            </button>
          </form>
        ) : (
          <div className="space-y-4">
            <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--accent-success-10)', border: '1px solid var(--accent-success-20)' }}>
              <div className="text-13 font-semibold mb-2" style={{ color: 'var(--accent-success)' }}>
                {t('verified.success.title')}
              </div>
              <div className="text-12 mb-4" style={{ color: 'var(--text-secondary)' }}>
                {t('verified.success.description')}
              </div>
              <div className="flex items-center gap-2">
                <input
                  type="text"
                  readOnly
                  value={`${window.location.origin}/verified/${result.code}`}
                  className="flex-1 px-3 py-2 rounded text-12"
                  style={{
                    backgroundColor: 'var(--bg-body)',
                    border: '1px solid var(--border-subtle)',
                    color: 'var(--text-primary)',
                  }}
                />
                <button
                  onClick={handleCopyUrl}
                  className="px-4 py-2 rounded text-12 font-medium"
                  style={{
                    backgroundColor: 'var(--accent-primary)',
                    color: 'var(--text-on-accent)',
                    border: 'none',
                    cursor: 'pointer',
                  }}
                >
                  {t('verified.success.copy')}
                </button>
              </div>
            </div>
            <div className="text-12 text-center" style={{ color: 'var(--text-secondary)' }}>
              {t('verified.success.redirecting')}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

