import React from 'react';
import { api } from '../lib/api';
import { useWsStore } from '../stores/wsStore';

type Voucher = {
  id: string;
  code: string;
  tier: 'pro' | 'max';
  validFrom: string;
  validTo?: string;
  durationMonths: number;
  maxUses?: number;
  usedCount: number;
  isActive: boolean;
  createdBy: string;
  note?: string;
  createdAt: string;
  updatedAt: string;
};

type TelemetryItem = {
  component: string;
  metric: string;
  windowStart: string;
  windowSizeSec: number;
  dimensions?: Record<string, string>;
  counters: Record<string, number>;
  updatedAt: string;
};

const AdminAccess: React.FC = () => {
  const [tab, setTab] = React.useState<'waitlist' | 'vouchers' | 'telemetry' | 'ws'>('waitlist');
  const [statusFilter, setStatusFilter] = React.useState<'pending' | 'approved' | 'rejected' | 'all'>('pending');
  const [items, setItems] = React.useState<Array<{ id: string; email: string; status: string; note?: string; invitedBy?: string; createdAt: string; updatedAt: string }>>([]);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState<string | null>(null);
  const [approvingEmail, setApprovingEmail] = React.useState<string | null>(null);
  
  // Vouchers state
  const [vouchers, setVouchers] = React.useState<Voucher[]>([]);
  const [vouchersLoading, setVouchersLoading] = React.useState(false);
  const [vouchersError, setVouchersError] = React.useState<string | null>(null);
  const [showCreateForm, setShowCreateForm] = React.useState(false);
  const [editingVoucher, setEditingVoucher] = React.useState<Voucher | null>(null);
  const [voucherFilter, setVoucherFilter] = React.useState<'all' | 'pro' | 'max'>('all');
  const [voucherActiveFilter, setVoucherActiveFilter] = React.useState<'all' | 'active' | 'inactive'>('all');

  // Telemetry state
  const [telemetryItems, setTelemetryItems] = React.useState<TelemetryItem[]>([]);
  const [telemetrySummary, setTelemetrySummary] = React.useState<Record<string, number>>({});
  const [telemetryHitRate, setTelemetryHitRate] = React.useState<number>(0);
  const [telemetryLoading, setTelemetryLoading] = React.useState(false);
  const [telemetryError, setTelemetryError] = React.useState<string | null>(null);
  const [telemetryLimit, setTelemetryLimit] = React.useState(48);
  const telemetryComponent = 'canonical_path_resolver';
  const telemetryMetric = 'redirect_cache';

  const load = React.useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const status = statusFilter === 'all' ? undefined : statusFilter;
      const data = await api.waitlistList(status as any);
      setItems(Array.isArray(data) ? data : []);
    } catch (e) {
      setError('Failed to load waitlist');
    } finally {
      setLoading(false);
    }
  }, [statusFilter]);

  const loadVouchers = React.useCallback(async () => {
    setVouchersLoading(true);
    setVouchersError(null);
    try {
      const params: { tier?: 'pro' | 'max'; isActive?: boolean } = {};
      if (voucherFilter !== 'all') {
        params.tier = voucherFilter;
      }
      if (voucherActiveFilter !== 'all') {
        params.isActive = voucherActiveFilter === 'active';
      }
      const data = await api.listVouchers(params);
      setVouchers(Array.isArray(data) ? data : []);
    } catch (e) {
      setVouchersError('Failed to load vouchers');
    } finally {
      setVouchersLoading(false);
    }
  }, [voucherFilter, voucherActiveFilter]);

  const loadTelemetry = React.useCallback(async () => {
    setTelemetryLoading(true);
    setTelemetryError(null);
    try {
      const data = await api.getHorizonTelemetry({
        component: telemetryComponent,
        metric: telemetryMetric,
        limit: telemetryLimit,
      });
      setTelemetryItems(Array.isArray(data?.items) ? data.items : []);
      setTelemetrySummary(data?.summary || {});
      setTelemetryHitRate(typeof data?.hitRate === 'number' ? data.hitRate : 0);
    } catch (e) {
      setTelemetryError('Failed to load telemetry');
    } finally {
      setTelemetryLoading(false);
    }
  }, [telemetryLimit, telemetryComponent, telemetryMetric]);

  React.useEffect(() => {
    if (tab === 'waitlist') load();
    if (tab === 'vouchers') loadVouchers();
    if (tab === 'telemetry') loadTelemetry();
  }, [tab, statusFilter, voucherFilter, voucherActiveFilter, load, loadVouchers, loadTelemetry]);

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
          style={{ backgroundColor: tab === 'vouchers' ? 'var(--white-10)' : 'transparent', color: 'var(--text-heading)' }}
          onClick={() => setTab('vouchers')}
        >
          Vouchers
        </button>
        <button
          className="px-3 py-1 rounded text-12"
          style={{ backgroundColor: tab === 'ws' ? 'var(--white-10)' : 'transparent', color: 'var(--text-heading)' }}
          onClick={() => setTab('ws')}
        >
          WebSocket
        </button>
        <button
          className="px-3 py-1 rounded text-12"
          style={{ backgroundColor: tab === 'telemetry' ? 'var(--white-10)' : 'transparent', color: 'var(--text-heading)' }}
          onClick={() => setTab('telemetry')}
        >
          Telemetry
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

      {tab === 'vouchers' && (
        <VouchersTab
          vouchers={vouchers}
          loading={vouchersLoading}
          error={vouchersError}
          filter={voucherFilter}
          activeFilter={voucherActiveFilter}
          onFilterChange={setVoucherFilter}
          onActiveFilterChange={setVoucherActiveFilter}
          onRefresh={loadVouchers}
          showCreateForm={showCreateForm}
          onShowCreateForm={setShowCreateForm}
          editingVoucher={editingVoucher}
          onEdit={setEditingVoucher}
        />
      )}

      {tab === 'telemetry' && (
        <TelemetryTab
          items={telemetryItems}
          summary={telemetrySummary}
          hitRate={telemetryHitRate}
          loading={telemetryLoading}
          error={telemetryError}
          limit={telemetryLimit}
          onLimitChange={setTelemetryLimit}
          onRefresh={loadTelemetry}
        />
      )}

      {tab === 'ws' && <WsDiagnosticsTab />}
    </div>
  );
};

const VouchersTab: React.FC<{
  vouchers: Voucher[];
  loading: boolean;
  error: string | null;
  filter: 'all' | 'pro' | 'max';
  activeFilter: 'all' | 'active' | 'inactive';
  onFilterChange: (f: 'all' | 'pro' | 'max') => void;
  onActiveFilterChange: (f: 'all' | 'active' | 'inactive') => void;
  onRefresh: () => void;
  showCreateForm: boolean;
  onShowCreateForm: (show: boolean) => void;
  editingVoucher: Voucher | null;
  onEdit: (v: Voucher | null) => void;
}> = ({
  vouchers,
  loading,
  error,
  filter,
  activeFilter,
  onFilterChange,
  onActiveFilterChange,
  onRefresh,
  showCreateForm,
  onShowCreateForm,
  editingVoucher,
  onEdit,
}) => {
  const [creating, setCreating] = React.useState(false);
  const [deletingId, setDeletingId] = React.useState<string | null>(null);
  const [createdVoucher, setCreatedVoucher] = React.useState<Voucher | null>(null);
  const [formData, setFormData] = React.useState({
    code: '',
    tier: 'pro' as 'pro' | 'max',
    validFrom: new Date().toISOString().split('T')[0],
    validTo: '',
    durationMonths: 1,
    maxUses: '',
    isActive: true,
    note: '',
  });

  React.useEffect(() => {
    if (editingVoucher) {
      setFormData({
        code: editingVoucher.code,
        tier: editingVoucher.tier,
        validFrom: editingVoucher.validFrom.split('T')[0],
        validTo: editingVoucher.validTo ? editingVoucher.validTo.split('T')[0] : '',
        durationMonths: editingVoucher.durationMonths,
        maxUses: editingVoucher.maxUses?.toString() || '',
        isActive: editingVoucher.isActive,
        note: editingVoucher.note || '',
      });
      onShowCreateForm(true);
    }
  }, [editingVoucher, onShowCreateForm]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setCreating(true);
    try {
      const data: any = {
        tier: formData.tier,
        validFrom: new Date(formData.validFrom).toISOString(),
        durationMonths: formData.durationMonths,
        isActive: formData.isActive,
      };
      // Code is auto-generated by backend, only include if editing
      if (editingVoucher && formData.code.trim()) {
        data.code = formData.code.toUpperCase().trim();
      }
      if (formData.validTo) {
        data.validTo = new Date(formData.validTo).toISOString();
      }
      if (formData.maxUses) {
        data.maxUses = parseInt(formData.maxUses, 10);
      }
      if (formData.note) {
        data.note = formData.note;
      }

      if (editingVoucher) {
        await api.updateVoucher(editingVoucher.id, data);
        onShowCreateForm(false);
        onEdit(null);
        setFormData({
          code: '',
          tier: 'pro',
          validFrom: new Date().toISOString().split('T')[0],
          validTo: '',
          durationMonths: 1,
          maxUses: '',
          isActive: true,
          note: '',
        });
        onRefresh();
      } else {
        const created = await api.createVoucher(data);
        setCreatedVoucher(created);
        onShowCreateForm(false);
        setFormData({
          code: '',
          tier: 'pro',
          validFrom: new Date().toISOString().split('T')[0],
          validTo: '',
          durationMonths: 1,
          maxUses: '',
          isActive: true,
          note: '',
        });
        onRefresh();
      }
    } catch (e: any) {
      alert(e?.response?.data?.error?.message || 'Failed to save voucher');
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Are you sure you want to delete this voucher?')) return;
    setDeletingId(id);
    try {
      await api.deleteVoucher(id);
      onRefresh();
    } catch (e) {
      alert('Failed to delete voucher');
    } finally {
      setDeletingId(null);
    }
  };

  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <div className="text-13" style={{ color: 'var(--text-heading)' }}>Manage vouchers</div>
        <div className="flex items-center gap-2">
          <select
            className="text-12 px-2 py-1 rounded"
            style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)', border: 'none' }}
            value={filter}
            onChange={(e) => onFilterChange(e.target.value as any)}
          >
            <option value="all">All tiers</option>
            <option value="pro">Pro</option>
            <option value="max">Max</option>
          </select>
          <select
            className="text-12 px-2 py-1 rounded"
            style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)', border: 'none' }}
            value={activeFilter}
            onChange={(e) => onActiveFilterChange(e.target.value as any)}
          >
            <option value="all">All</option>
            <option value="active">Active</option>
            <option value="inactive">Inactive</option>
          </select>
          <button
            className="btn btn-primary text-12"
            onClick={() => {
              onEdit(null);
              setCreatedVoucher(null);
              setFormData({
                code: '',
                tier: 'pro',
                validFrom: new Date().toISOString().split('T')[0],
                validTo: '',
                durationMonths: 1,
                maxUses: '',
                isActive: true,
                note: '',
              });
              onShowCreateForm(true);
            }}
          >
            + Create
          </button>
        </div>
      </div>

      {showCreateForm && (
        <div className="mb-3 p-3 rounded-xl border" style={{ borderColor: 'var(--overlay-line-90)', backgroundColor: 'var(--bg-elevated)' }}>
          <form onSubmit={handleSubmit} className="space-y-3">
            <div className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>
              {editingVoucher ? 'Edit Voucher' : 'Create New Voucher'}
            </div>
            {editingVoucher && (
              <div>
                <label className="text-12 block mb-1" style={{ color: 'var(--text-secondary)' }}>Code</label>
                <input
                  type="text"
                  className="w-full px-2 py-1 rounded text-12"
                  style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)', border: '1px solid var(--overlay-line-90)' }}
                  value={formData.code}
                  disabled
                  placeholder="PRO2024Q1"
                />
              </div>
            )}
            <div className="grid grid-cols-2 gap-2">
              <div>
                <label className="text-12 block mb-1" style={{ color: 'var(--text-secondary)' }}>Tier *</label>
                <select
                  className="w-full px-2 py-1 rounded text-12"
                  style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)', border: '1px solid var(--overlay-line-90)' }}
                  value={formData.tier}
                  onChange={(e) => setFormData({ ...formData, tier: e.target.value as 'pro' | 'max' })}
                  required
                >
                  <option value="pro">Pro</option>
                  <option value="max">Max</option>
                </select>
              </div>
              <div>
                <label className="text-12 block mb-1" style={{ color: 'var(--text-secondary)' }}>Valid From *</label>
                <input
                  type="date"
                  className="w-full px-2 py-1 rounded text-12"
                  style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)', border: '1px solid var(--overlay-line-90)' }}
                  value={formData.validFrom}
                  onChange={(e) => setFormData({ ...formData, validFrom: e.target.value })}
                  required
                />
              </div>
              <div>
                <label className="text-12 block mb-1" style={{ color: 'var(--text-secondary)' }}>Valid To (optional)</label>
                <input
                  type="date"
                  className="w-full px-2 py-1 rounded text-12"
                  style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)', border: '1px solid var(--overlay-line-90)' }}
                  value={formData.validTo}
                  onChange={(e) => setFormData({ ...formData, validTo: e.target.value })}
                />
              </div>
              <div>
                <label className="text-12 block mb-1" style={{ color: 'var(--text-secondary)' }}>Duration (months) *</label>
                <input
                  type="number"
                  min="1"
                  className="w-full px-2 py-1 rounded text-12"
                  style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)', border: '1px solid var(--overlay-line-90)' }}
                  value={formData.durationMonths}
                  onChange={(e) => setFormData({ ...formData, durationMonths: parseInt(e.target.value, 10) || 1 })}
                  required
                />
              </div>
              <div>
                <label className="text-12 block mb-1" style={{ color: 'var(--text-secondary)' }}>Max Uses (optional)</label>
                <input
                  type="number"
                  min="1"
                  className="w-full px-2 py-1 rounded text-12"
                  style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)', border: '1px solid var(--overlay-line-90)' }}
                  value={formData.maxUses}
                  onChange={(e) => setFormData({ ...formData, maxUses: e.target.value })}
                  placeholder="Unlimited if empty"
                />
              </div>
            </div>
            <div>
              <label className="text-12 block mb-1" style={{ color: 'var(--text-secondary)' }}>Note (optional)</label>
              <textarea
                className="w-full px-2 py-1 rounded text-12"
                style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)', border: '1px solid var(--overlay-line-90)' }}
                value={formData.note}
                onChange={(e) => setFormData({ ...formData, note: e.target.value })}
                rows={2}
                placeholder="Internal note"
              />
            </div>
            <div className="flex items-center gap-2">
              <label className="text-12 flex items-center gap-2" style={{ color: 'var(--text-secondary)' }}>
                <input
                  type="checkbox"
                  checked={formData.isActive}
                  onChange={(e) => setFormData({ ...formData, isActive: e.target.checked })}
                />
                Active
              </label>
            </div>
            <div className="flex items-center gap-2">
              <button
                type="submit"
                className="btn btn-primary text-12"
                disabled={creating}
              >
                {creating ? 'Saving...' : editingVoucher ? 'Update' : 'Create'}
              </button>
              <button
                type="button"
                className="btn text-12"
                style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)' }}
                onClick={() => {
                  onShowCreateForm(false);
                  onEdit(null);
                }}
              >
                Cancel
              </button>
            </div>
          </form>
        </div>
      )}

      {createdVoucher && (
        <div className="mb-3 p-4 rounded-xl border" style={{ borderColor: 'var(--accent-primary)', backgroundColor: 'var(--bg-elevated)' }}>
          <div className="text-13 font-medium mb-2" style={{ color: 'var(--text-heading)' }}>
            ✓ Voucher Created Successfully!
          </div>
          <div className="mb-3">
            <div className="text-12 mb-1" style={{ color: 'var(--text-secondary)' }}>Voucher Code:</div>
            <div className="flex items-center gap-2">
              <div 
                className="px-3 py-2 rounded font-mono text-14 font-bold"
                style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)', border: '1px solid var(--overlay-line-90)' }}
              >
                {createdVoucher.code}
              </div>
              <button
                className="btn btn-primary text-12"
                onClick={() => {
                  navigator.clipboard.writeText(createdVoucher.code);
                  alert('Code copied to clipboard!');
                }}
              >
                Copy
              </button>
            </div>
          </div>
          <div className="text-12 mb-2" style={{ color: 'var(--text-secondary)' }}>
            Tier: <strong style={{ color: 'var(--text-heading)' }}>{createdVoucher.tier.toUpperCase()}</strong> · 
            Duration: <strong style={{ color: 'var(--text-heading)' }}>{createdVoucher.durationMonths} month{createdVoucher.durationMonths !== 1 ? 's' : ''}</strong>
          </div>
          <button
            className="btn text-12"
            style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)' }}
            onClick={() => setCreatedVoucher(null)}
          >
            Close
          </button>
        </div>
      )}

      <div className="rounded-xl border" style={{ borderColor: 'var(--overlay-line-90)' }}>
        {loading ? (
          <div className="p-3 text-12" style={{ color: 'var(--text-secondary)' }}>Loading…</div>
        ) : error ? (
          <div className="p-3 text-12" style={{ color: 'var(--text-secondary)' }}>{error}</div>
        ) : vouchers.length === 0 ? (
          <div className="p-3 text-12" style={{ color: 'var(--text-secondary)' }}>No vouchers</div>
        ) : (
          <div className="divide-y" style={{ borderColor: 'var(--overlay-line-90)' }}>
            {vouchers.map((v) => (
              <div key={v.id} className="p-3" style={{ backgroundColor: 'var(--bg-elevated)' }}>
                <div className="flex items-start justify-between">
                  <div className="flex-1">
                    <div className="flex items-center gap-2 mb-1">
                      <div className="text-13 font-medium" style={{ color: 'var(--text-heading)' }}>{v.code}</div>
                      <span
                        className="px-2 py-0.5 rounded text-11"
                        style={{
                          backgroundColor: v.tier === 'max' ? 'var(--accent-primary)' : 'var(--accent-secondary)',
                          color: 'white',
                        }}
                      >
                        {v.tier.toUpperCase()}
                      </span>
                      {!v.isActive && (
                        <span className="px-2 py-0.5 rounded text-11" style={{ backgroundColor: 'var(--overlay-line-90)', color: 'var(--text-secondary)' }}>
                          INACTIVE
                        </span>
                      )}
                    </div>
                    <div className="text-12" style={{ color: 'var(--text-secondary)' }}>
                      Duration: {v.durationMonths} month{v.durationMonths !== 1 ? 's' : ''} · 
                      Used: {v.usedCount}{v.maxUses ? ` / ${v.maxUses}` : ' / ∞'} · 
                      Valid: {new Date(v.validFrom).toLocaleDateString()}
                      {v.validTo ? ` - ${new Date(v.validTo).toLocaleDateString()}` : ' - No expiry'}
                    </div>
                    {v.note && (
                      <div className="text-12 mt-1" style={{ color: 'var(--text-secondary)' }}>Note: {v.note}</div>
                    )}
                    <div className="text-11 mt-1" style={{ color: 'var(--text-tertiary)' }}>
                      Created: {new Date(v.createdAt).toLocaleString()}
                    </div>
                  </div>
                  <div className="flex items-center gap-2 ml-4">
                    <button
                      className="text-12 px-2 py-1 rounded"
                      style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)' }}
                      onClick={() => onEdit(v)}
                    >
                      Edit
                    </button>
                    <button
                      className="text-12 px-2 py-1 rounded"
                      style={{ backgroundColor: 'var(--accent-danger)', color: 'white' }}
                      onClick={() => handleDelete(v.id)}
                      disabled={deletingId === v.id}
                    >
                      {deletingId === v.id ? 'Deleting...' : 'Delete'}
                    </button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};

const WsDiagnosticsTab: React.FC = () => {
  const { health, getDiagnostics } = useWsStore();

  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <div className="text-13" style={{ color: 'var(--text-heading)' }}>WebSocket diagnostics</div>
        <div className="flex items-center gap-2">
          <div
            style={{
              width: '8px',
              height: '8px',
              borderRadius: '50%',
              backgroundColor:
                health === 'ok'
                  ? 'var(--accent-primary)'
                  : health === 'degraded'
                    ? 'var(--accent-warning)'
                    : 'var(--accent-danger)',
            }}
            title={health === 'ok' ? 'Connected' : health === 'degraded' ? 'Degraded' : 'Disconnected'}
          />
          <span className="text-12" style={{ color: 'var(--text-secondary)' }}>
            {health === 'ok' ? 'Connected' : health === 'degraded' ? 'Degraded' : 'Disconnected'}
          </span>
        </div>
      </div>

      <div className="rounded-xl border" style={{ borderColor: 'var(--overlay-line-90)' }}>
        <div className="p-3">
          <label htmlFor="ws-diagnostics-textarea" className="sr-only">WebSocket diagnostics information</label>
          <textarea 
            id="ws-diagnostics-textarea"
            name="ws-diagnostics"
            readOnly 
            value={getDiagnostics()} 
            style={{ 
              width: '100%', 
              height: 400, 
              fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace', 
              fontSize: 12, 
              lineHeight: 1.4, 
              background: 'var(--bg)', 
              color: 'var(--text-primary)', 
              border: '1px solid var(--border)', 
              borderRadius: 6, 
              padding: 8 
            }} 
          />
          <div className="flex items-center justify-end gap-2" style={{ marginTop: 8 }}>
            <button 
              className="btn btn-primary text-12"
              onClick={() => { 
                try { 
                  navigator.clipboard.writeText(getDiagnostics()); 
                } catch {} 
              }}
            >
              Copy
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};

const TelemetryTab: React.FC<{
  items: TelemetryItem[];
  summary: Record<string, number>;
  hitRate: number;
  loading: boolean;
  error: string | null;
  limit: number;
  onLimitChange: (value: number) => void;
  onRefresh: () => void;
}> = ({
  items,
  summary,
  hitRate,
  loading,
  error,
  limit,
  onLimitChange,
  onRefresh,
}) => {
  const resolveCalls = summary?.resolve_calls || 0;
  const redirectHits = summary?.redirect_hits || 0;
  const redirectWrites = summary?.redirect_writes || 0;
  const redirectStable = summary?.redirect_stable || 0;
  const redirectVariability = summary?.redirect_variability || 0;

  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <div className="text-13" style={{ color: 'var(--text-heading)' }}>Canonical path redirect cache</div>
        <div className="flex items-center gap-2">
          <select
            className="text-12 px-2 py-1 rounded"
            style={{ backgroundColor: 'var(--white-10)', color: 'var(--text-heading)', border: 'none' }}
            value={limit}
            onChange={(e) => onLimitChange(parseInt(e.target.value, 10))}
          >
            <option value={24}>Last 24h</option>
            <option value={48}>Last 48h</option>
            <option value={168}>Last 7d</option>
          </select>
          <button className="btn btn-primary text-12" onClick={onRefresh}>
            Refresh
          </button>
        </div>
      </div>

      <div className="grid grid-cols-5 gap-2 mb-3">
        <div className="p-3 rounded-xl border" style={{ borderColor: 'var(--overlay-line-90)', backgroundColor: 'var(--bg-elevated)' }}>
          <div className="text-11" style={{ color: 'var(--text-secondary)' }}>Resolve calls</div>
          <div className="text-16 font-semibold" style={{ color: 'var(--text-heading)' }}>{resolveCalls}</div>
        </div>
        <div className="p-3 rounded-xl border" style={{ borderColor: 'var(--overlay-line-90)', backgroundColor: 'var(--bg-elevated)' }}>
          <div className="text-11" style={{ color: 'var(--text-secondary)' }}>Redirect hits</div>
          <div className="text-16 font-semibold" style={{ color: 'var(--text-heading)' }}>{redirectHits}</div>
        </div>
        <div className="p-3 rounded-xl border" style={{ borderColor: 'var(--overlay-line-90)', backgroundColor: 'var(--bg-elevated)' }}>
          <div className="text-11" style={{ color: 'var(--text-secondary)' }}>Redirect writes</div>
          <div className="text-16 font-semibold" style={{ color: 'var(--text-heading)' }}>{redirectWrites}</div>
        </div>
        <div className="p-3 rounded-xl border" style={{ borderColor: 'var(--overlay-line-90)', backgroundColor: 'var(--bg-elevated)' }}>
          <div className="text-11" style={{ color: 'var(--text-secondary)' }}>Hit rate</div>
          <div className="text-16 font-semibold" style={{ color: 'var(--text-heading)' }}>
            {(hitRate * 100).toFixed(2)}%
          </div>
        </div>
        <div className="p-3 rounded-xl border" style={{ borderColor: 'var(--overlay-line-90)', backgroundColor: 'var(--bg-elevated)' }}>
          <div className="text-11" style={{ color: 'var(--text-secondary)' }}>Variability</div>
          <div className="text-16 font-semibold" style={{ color: 'var(--text-heading)' }}>{redirectVariability}</div>
          <div className="text-10" style={{ color: 'var(--text-tertiary)' }}>Stable: {redirectStable}</div>
        </div>
      </div>

      <div className="rounded-xl border" style={{ borderColor: 'var(--overlay-line-90)' }}>
        {loading ? (
          <div className="p-3 text-12" style={{ color: 'var(--text-secondary)' }}>Loading…</div>
        ) : error ? (
          <div className="p-3 text-12" style={{ color: 'var(--text-secondary)' }}>{error}</div>
        ) : items.length === 0 ? (
          <div className="p-3 text-12" style={{ color: 'var(--text-secondary)' }}>No telemetry windows</div>
        ) : (
          <div className="divide-y" style={{ borderColor: 'var(--overlay-line-90)' }}>
            {items.map((item) => {
              const calls = item.counters?.resolve_calls || 0;
              const hits = item.counters?.redirect_hits || 0;
              const rate = calls > 0 ? (hits / calls) * 100 : 0;
              return (
                <div key={`${item.windowStart}-${item.updatedAt}`} className="p-3" style={{ backgroundColor: 'var(--bg-elevated)' }}>
                  <div className="flex items-center justify-between">
                    <div>
                      <div className="text-13" style={{ color: 'var(--text-heading)' }}>
                        {new Date(item.windowStart).toLocaleString()}
                      </div>
                      <div className="text-12" style={{ color: 'var(--text-secondary)' }}>
                        Calls: {calls} · Hits: {hits} · Writes: {item.counters?.redirect_writes || 0} · Stable: {item.counters?.redirect_stable || 0} · Variability: {item.counters?.redirect_variability || 0} · Hit rate: {rate.toFixed(2)}%
                      </div>
                      {item.dimensions?.fragmentType && (
                        <div className="text-11" style={{ color: 'var(--text-tertiary)' }}>
                          fragmentType: {item.dimensions.fragmentType}
                        </div>
                      )}
                    </div>
                    <div className="text-11" style={{ color: 'var(--text-secondary)' }}>
                      window: {Math.round(item.windowSizeSec / 3600)}h
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
};

export default AdminAccess;


