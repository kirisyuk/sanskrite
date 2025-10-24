import React from 'react';
import { api } from '../lib/api';

const AdminAccess: React.FC = () => {
  const [tab, setTab] = React.useState<'waitlist' | 'other'>('waitlist');
  const [statusFilter, setStatusFilter] = React.useState<'pending' | 'approved' | 'rejected' | 'all'>('pending');
  const [items, setItems] = React.useState<Array<{ id: string; email: string; status: string; note?: string; invitedBy?: string; createdAt: string; updatedAt: string }>>([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [approvingEmail, setApprovingEmail] = React.useState<string | null>(null);

  const load = React.useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const status = statusFilter === 'all' ? undefined : statusFilter;
      const data = await api.waitlistList(status as any);
      setItems(data);
    } catch (e) {
      setError('Failed to load waitlist');
    } finally {
      setLoading(false);
    }
  }, [statusFilter]);

  React.useEffect(() => {
    if (tab === 'waitlist') load();
  }, [tab, statusFilter, load]);

  const approve = async (email: string) => {
    try {
      setApprovingEmail(email);
      await api.waitlistApprove(email);
      await load();
    } catch {
      setError('Failed to approve');
    } finally {
      setApprovingEmail(null);
    }
  };

  return (
    <div className="rounded-xl" style={{ backgroundColor: 'transparent', padding: '0' }}>
      <div className="flex items-center gap-2 mb-3">
        <button
          className="px-3 py-1 rounded text-12"
          style={{ backgroundColor: tab === 'waitlist' ? 'var(--white-10)' : 'transparent', color: 'var(--text-heading)' }}
          onClick={() => setTab('waitlist')}
        >
          Waitlist
        </button>
        <button
          className="px-3 py-1 rounded text-12"
          style={{ backgroundColor: tab === 'other' ? 'var(--white-10)' : 'transparent', color: 'var(--text-heading)' }}
          onClick={() => setTab('other')}
        >
          Other
        </button>
      </div>

      {tab === 'waitlist' && (
        <div>
          <div className="flex items-center justify-between mb-2">
            <div className="text-13" style={{ color: 'var(--text-heading)' }}>Manage access requests</div>
            <div className="flex items-center gap-2">
              <label className="text-12" style={{ color: 'var(--text-secondary)' }}>Status</label>
              <select
                className="text-12 px-2 py-1 rounded"
                style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)', border: 'none' }}
                value={statusFilter}
                onChange={(e) => setStatusFilter(e.target.value as any)}
              >
                <option value="pending">Pending</option>
                <option value="approved">Approved</option>
                <option value="rejected">Rejected</option>
                <option value="all">All</option>
              </select>
            </div>
          </div>

          <div className="rounded-xl border" style={{ borderColor: 'var(--overlay-line-90)' }}>
            {loading ? (
              <div className="p-3 text-12" style={{ color: 'var(--text-secondary)' }}>Loading…</div>
            ) : error ? (
              <div className="p-3 text-12" style={{ color: 'var(--text-secondary)' }}>{error}</div>
            ) : items.length === 0 ? (
              <div className="p-3 text-12" style={{ color: 'var(--text-secondary)' }}>No entries</div>
            ) : (
              <div className="divide-y" style={{ borderColor: 'var(--overlay-line-90)' }}>
                {items.map((it) => (
                  <div key={it.id} className="flex items-center justify-between p-3" style={{ backgroundColor: 'var(--bg-elevated)' }}>
                    <div>
                      <div className="text-13" style={{ color: 'var(--text-heading)' }}>{it.email}</div>
                      <div className="text-12" style={{ color: 'var(--text-secondary)' }}>
                        {(it.status || '').toUpperCase()} · {new Date(it.createdAt).toLocaleString()}
                        {it.note ? ` · ${it.note}` : ''}
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      {(it.status || '').toLowerCase() !== 'approved' && (
                        <button
                          className="btn btn-primary"
                          disabled={approvingEmail === it.email}
                          onClick={() => approve(it.email)}
                        >
                          {approvingEmail === it.email ? 'Approving…' : 'Approve'}
                        </button>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

export default AdminAccess;


