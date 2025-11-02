import React from 'react';
import { useParams } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';
import ChatGraphMessage from '../components/ChatGraphMessage';
import ChatMessage from '../components/ChatMessage';
import { api } from '../lib/api';

export default function ShareViewPage() {
  const { token } = useParams<{ token: string }>();
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const [data, setData] = React.useState<any | null>(null);
  const [theme, setTheme] = React.useState<'light' | 'dark'>('dark');

  React.useEffect(() => {
    let mounted = true;
    (async () => {
      try {
        if (!token) { setError('Missing token'); setLoading(false); return; }
        const res = await api.shareGetPublic(token);
        if (mounted) { setData(res); setLoading(false); }
      } catch (e: any) {
        if (mounted) { setError('Unavailable or expired'); setLoading(false); }
      }
    })();
    return () => { mounted = false; };
  }, [token]);

  // Load theme from localStorage on mount and apply to document
  React.useEffect(() => {
    const savedTheme = localStorage.getItem('theme') as 'light' | 'dark' | null;
    if (savedTheme) {
      setTheme(savedTheme);
      document.documentElement.setAttribute('data-theme', savedTheme);
    } else {
      document.documentElement.setAttribute('data-theme', 'dark');
    }
  }, []);

  // Apply theme changes to document element
  React.useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
  }, [theme]);

  const toggleTheme = () => {
    const newTheme = theme === 'dark' ? 'light' : 'dark';
    setTheme(newTheme);
    localStorage.setItem('theme', newTheme);
  };

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center" style={{ background: 'var(--bg-root)', color: 'var(--text-secondary-strong)' }}>
        Loading…
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="min-h-screen flex flex-col items-center justify-center p-6" style={{ background: 'var(--bg-root)' }}>
        <div className="w-full max-w-lg text-center">
          <h1 className="text-2xl font-semibold mb-3" style={{ color: 'var(--text-heading)' }}>
            Link Unavailable or Expired
          </h1>
          <p className="text-base mb-2" style={{ color: 'var(--text-secondary-strong)' }}>
            The public link to this project is no longer valid or has expired.
          </p>
          <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
            Please contact the project owner to request an updated link.
          </p>
        </div>
      </div>
    );
  }

  const proj = data.project || {};
  const items = Array.isArray(data.interactions) ? data.interactions : [];

  return (
    <div className="min-h-screen" style={{ background: 'var(--bg-root)' }}>
      <div className="max-w-3xl mx-auto px-4 py-4">
        {/* Header with theme toggle */}
        <div className="mb-3 flex items-start justify-between">
          <div className="flex-1">
            <div className="text-[13px] font-semibold" style={{ color: 'var(--text-heading)' }}>
              {proj.displayName || 'Shared chat'}
            </div>
            {proj.description && (
              <div className="text-[12px]" style={{ color: 'var(--text-secondary-strong)' }}>
                {proj.description}
              </div>
            )}
            <div className="text-[11px] mt-1" style={{ color: 'var(--text-tertiary-weak)' }}>
              Public read‑only · available until {new Date(data.share.expiresAt).toLocaleString()}
            </div>
          </div>
          <button
            onClick={toggleTheme}
            className="ml-3 rounded-lg p-2"
            style={{ 
              backgroundColor: 'var(--bg-elevated)', 
              border: '1px solid var(--overlay-line-90)',
              color: 'var(--text-secondary-strong)',
              cursor: 'pointer'
            }}
            title={`Switch to ${theme === 'dark' ? 'light' : 'dark'} theme`}
          >
            {theme === 'dark' ? (
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                <circle cx="12" cy="12" r="5" strokeWidth="2"/>
                <path d="M12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42" strokeWidth="2" strokeLinecap="round"/>
              </svg>
            ) : (
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"/>
              </svg>
            )}
          </button>
        </div>

        {/* Messages */}
        <div className="space-y-2">
          {items.map((it: any) => {
            const isUser = it.kind === 'user';
            const content = (it.contentSanitized || it.content || '').toString();
            const gd = it.graphData;

            // Special rendering for action.bring_in indicator messages
            if (isUser && it.subkind === 'action.bring_in') {
              return (
                <div key={it.id} className="my-1.5 flex justify-end">
                  <div className="px-3 py-1.5 rounded-lg text-[12px] inline-flex items-center gap-2" style={{ 
                    background: 'var(--accent-research-10)', 
                    color: 'var(--accent-research)',
                    border: '1px solid var(--accent-research-20)',
                    maxWidth: '80%'
                  }}>
                    <svg className="w-3.5 h-3.5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                      <path d="M12 7a3 3 0 1 1 0 6a3 3 0 0 1 0-6" strokeWidth="2" />
                      <path d="M5 20c0-3.314 3.134-6 7-6s7 2.686 7 6" strokeWidth="2" strokeLinecap="round" />
                      <path d="M18 7v4M16 9h4" strokeWidth="2" strokeLinecap="round" />
                    </svg>
                    <span>{content}</span>
                  </div>
                </div>
              );
            }

            // Determine display name
            let displayName = isUser ? 'you' : 'assistant';
            if (it.author?.displayName) {
              displayName = it.author.displayName;
            } else if (it.subkind === 'agent.bring_in' || it.subkind === 'agent.ask') {
              displayName = 'agent';
            }

            // Agent accent (if agent contribution)
            const isAgent = it.subkind?.startsWith('agent.');

            return (
              <ChatMessage
                key={it.id}
                role={isUser ? 'user' : 'assistant'}
                align={isUser ? 'right' : 'left'}
                headerText={displayName}
                leftAccent={isAgent}
              >
                {gd ? (
                  <ChatGraphMessage
                    chartTitle={gd.chartTitle}
                    chartType={gd.chartType || 'line'}
                    table={gd.table}
                    sources={gd.sources}
                  />
                ) : (
                  <div className="markdown">
                    <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[rehypeRaw]}>
                      {content}
                    </ReactMarkdown>
                  </div>
                )}
              </ChatMessage>
            );
          })}
        </div>
      </div>
    </div>
  );
}


