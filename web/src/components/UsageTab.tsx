import React from 'react';
import { LineChart, Line, BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer, Cell } from 'recharts';
import { api } from '../lib/api';
import { useAuth } from '../contexts/AuthContext';
import { ThemeContext } from './ThemeProvider';

export const UsageTab: React.FC = () => {
  const { theme } = React.useContext(ThemeContext);
  const { currentUser } = useAuth();
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const [usageData, setUsageData] = React.useState<{
    events: Array<{
      id: string;
      tenantId: string;
      userId: string;
      provider: string;
      model: string;
      modelAlias?: string;
      inputTokens: number;
      outputTokens: number;
      totalTokens: number;
      costUSD: number;
      kind: string;
      createdAt: string;
    }>;
    byModel: Record<string, {
      count: number;
      inputTokens: number;
      outputTokens: number;
      totalTokens: number;
      costUSD: number;
    }>;
    dailyTotals: Record<string, {
      inputTokens: number;
      outputTokens: number;
      totalTokens: number;
      costUSD: number;
      count: number;
    }>;
    tokenCategories?: {
      premiumTokens: {
        inputTokens: number;
        outputTokens: number;
        totalTokens: number;
        costUSD: number;
        count: number;
      };
      regularTokens: {
        inputTokens: number;
        outputTokens: number;
        totalTokens: number;
        costUSD: number;
        count: number;
      };
    };
    period: {
      startDate: string;
      endDate: string;
    };
  } | null>(null);
  
  const [dateRange, setDateRange] = React.useState<'1d' | '7d' | '30d'>('7d');
  const [selectedStartDate, setSelectedStartDate] = React.useState<string>('');
  const [selectedEndDate, setSelectedEndDate] = React.useState<string>('');
  const [eventsPage, setEventsPage] = React.useState(1);
  const eventsPerPage = 20;

  React.useEffect(() => {
    if (!currentUser?.uid) return;
    
    const loadUsage = async () => {
      setLoading(true);
      setError(null);
      try {
        const endDate = selectedEndDate || new Date().toISOString().split('T')[0];
        const startDate = selectedStartDate || (() => {
          const d = new Date();
          const days = dateRange === '1d' ? 1 : dateRange === '7d' ? 7 : 30;
          d.setDate(d.getDate() - days);
          return d.toISOString().split('T')[0];
        })();
        
        const data = await api.getUsage({ startDate, endDate, granularity: dateRange });
        console.log('[UsageTab] Received data:', {
          eventsCount: data.events?.length,
          dailyTotalsKeys: Object.keys(data.dailyTotals || {}),
          dailyTotalsSample: Object.entries(data.dailyTotals || {}).slice(0, 2),
          byModelKeys: Object.keys(data.byModel || {}),
          byModelSample: Object.entries(data.byModel || {}).slice(0, 2),
        });
        setUsageData(data);
      } catch (err: any) {
        setError(err?.message || 'Failed to load usage data');
        console.error('[UsageTab] Error loading usage:', err);
      } finally {
        setLoading(false);
      }
    };
    
    loadUsage();
  }, [currentUser?.uid, dateRange, selectedStartDate, selectedEndDate]);
  
  // Reset to page 1 when date range changes
  React.useEffect(() => {
    setEventsPage(1);
  }, [dateRange, selectedStartDate, selectedEndDate]);

  // Prepare chart data from dailyTotals
  const chartData = React.useMemo(() => {
    if (!usageData?.dailyTotals) {
      console.log('[UsageTab] No dailyTotals in usageData');
      return [];
    }
    
    const entries = Object.entries(usageData.dailyTotals);
    console.log('[UsageTab] Processing dailyTotals:', {
      entriesCount: entries.length,
      firstEntry: entries[0],
      allKeys: Object.keys(usageData.dailyTotals),
    });
    
    const result = entries
      .map(([date, totals]: [string, any], index: number) => {
        // Debug: log what we're getting for first entry
        if (index === 0) {
          console.log('[UsageTab] First totals object:', totals, 'keys:', Object.keys(totals || {}), 'type:', typeof totals);
        }
        return {
          date,
          tokens: totals?.totalTokens ?? totals?.TotalTokens ?? 0,
          cost: totals?.costUSD ?? totals?.CostUSD ?? 0,
          count: totals?.count ?? totals?.Count ?? 0,
        };
      })
      .sort((a, b) => a.date.localeCompare(b.date));
    
    console.log('[UsageTab] Chart data result:', result);
    return result;
  }, [usageData]);

  // Prepare model breakdown data
  const modelData = React.useMemo(() => {
    if (!usageData?.byModel) {
      console.log('[UsageTab] No byModel data');
      return [];
    }
    
    const entries = Object.entries(usageData.byModel);
    console.log('[UsageTab] Processing byModel:', {
      entriesCount: entries.length,
      firstEntry: entries[0],
      allKeys: Object.keys(usageData.byModel),
    });
    
    const result = entries
      .map(([key, stats]: [string, any], index: number) => {
        // Debug: log what we're getting for first entry
        if (index === 0) {
          console.log('[UsageTab] First byModel stats object:', stats, 'keys:', Object.keys(stats || {}), 'type:', typeof stats);
        }
        const parts = key.split(':');
        const costUSD = stats?.costUSD ?? stats?.CostUSD ?? 0;
        const modelName = parts.length > 1 ? parts[1] : key; // Remove provider prefix
        
        return {
          model: modelName,
          provider: parts.length > 1 ? parts[0] : 'unknown',
          count: stats?.count ?? stats?.Count ?? 0,
          inputTokens: stats?.inputTokens ?? stats?.InputTokens ?? 0,
          outputTokens: stats?.outputTokens ?? stats?.OutputTokens ?? 0,
          totalTokens: stats?.totalTokens ?? stats?.TotalTokens ?? 0,
          costUSD: costUSD,
        };
      })
      .filter((item) => {
        // Filter out entries with zero cost, empty model name, or invalid data
        return item.costUSD > 0 && item.model && item.model.trim() !== '';
      })
      .sort((a, b) => b.costUSD - a.costUSD)
      .slice(0, 10); // Top 10 models
    
    console.log('[UsageTab] Model data result:', result);
    return result;
  }, [usageData]);

  // Calculate totals from events directly (more reliable)
  const totals = React.useMemo(() => {
    if (!usageData || !usageData.events) return null;
    
    let totalTokens = 0;
    let totalCost = 0;
    let totalCount = usageData.events.length;
    
    usageData.events.forEach(event => {
      totalTokens += event.totalTokens || 0;
      totalCost += event.costUSD || 0;
    });
    
    return { totalTokens, totalCost, totalCount };
  }, [usageData]);

  // Calculate token category totals
  const tokenCategories = React.useMemo(() => {
    if (!usageData?.tokenCategories) return null;
    return usageData.tokenCategories;
  }, [usageData]);

  // Group events by model and time proximity
  const groupedEvents = React.useMemo(() => {
    if (!usageData?.events) return [];
    
    const TIME_WINDOW_MS = 60000; // 60 seconds (1 minute) window for grouping
    const sortedEvents = [...usageData.events].sort((a, b) => 
      new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
    );
    
    const groups: Array<{
      model: string;
      modelAlias?: string;
      kind: string;
      events: typeof sortedEvents;
      totalTokens: number;
      totalCost: number;
      startTime: Date;
      endTime: Date;
      count: number;
    }> = [];
    
    for (const event of sortedEvents) {
      const eventTime = new Date(event.createdAt).getTime();
      const eventKey = `${event.model}:${event.modelAlias || ''}:${event.kind}`;
      
      // Try to find an existing group that matches model/alias/kind and is within time window
      // Check if event is within time window from the expanded group time range
      let foundGroup = groups.find(group => {
        const groupKey = `${group.model}:${group.modelAlias || ''}:${group.kind}`;
        if (groupKey !== eventKey) return false;
        
        const groupStartTime = group.startTime.getTime() - TIME_WINDOW_MS;
        const groupEndTime = group.endTime.getTime() + TIME_WINDOW_MS;
        // Check if event is within expanded time window
        return eventTime >= groupStartTime && eventTime <= groupEndTime;
      });
      
      if (foundGroup) {
        // Add to existing group
        foundGroup.events.push(event);
        foundGroup.totalTokens += event.totalTokens || 0;
        foundGroup.totalCost += event.costUSD || 0;
        foundGroup.count++;
        // Update time range
        if (eventTime < foundGroup.startTime.getTime()) {
          foundGroup.startTime = new Date(event.createdAt);
        }
        if (eventTime > foundGroup.endTime.getTime()) {
          foundGroup.endTime = new Date(event.createdAt);
        }
      } else {
        // Create new group
        groups.push({
          model: event.model,
          modelAlias: event.modelAlias,
          kind: event.kind,
          events: [event],
          totalTokens: event.totalTokens || 0,
          totalCost: event.costUSD || 0,
          startTime: new Date(event.createdAt),
          endTime: new Date(event.createdAt),
          count: 1,
        });
      }
    }
    
    // Sort groups by start time (newest first)
    return groups.sort((a, b) => b.startTime.getTime() - a.startTime.getTime());
  }, [usageData?.events]);

  const formatDate = (dateStr: string) => {
    const date = new Date(dateStr + 'T00:00:00');
    return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
  };

  const formatCost = (cost: number | string) => {
    const numCost = typeof cost === 'string' ? parseFloat(cost) : cost;
    if (isNaN(numCost)) return '$0.00';
    if (numCost < 0.01) return '<$0.01';
    return `US$${numCost.toFixed(2)}`;
  };

  const formatTokens = (tokens: number) => {
    if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(1)}M`;
    if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}K`;
    return tokens.toString();
  };

  const formatTimeRange = (startTime: Date, endTime: Date) => {
    const start = startTime;
    const end = endTime;
    
    // If same time, show single time
    if (start.getTime() === end.getTime()) {
      return start.toLocaleString();
    }
    
    // Format dates
    const startDateStr = start.toLocaleDateString();
    const endDateStr = end.toLocaleDateString();
    const startTimeStr = start.toLocaleTimeString();
    const endTimeStr = end.toLocaleTimeString();
    
    // If same date, show date once with time range
    if (startDateStr === endDateStr) {
      return `${startDateStr}, ${startTimeStr} - ${endTimeStr}`;
    }
    
    // Different dates, show full range
    return `${start.toLocaleString()} - ${end.toLocaleString()}`;
  };

  const getProviderColor = (provider: string) => {
    const colors: Record<string, string> = {
      gemini: '#4285F4',
      openai: '#10A37F',
      anthropic: '#D97757',
      grok: '#FF6B35',
      deepseek: '#6366F1',
    };
    return colors[provider] || '#6B7280';
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center" style={{ minHeight: '400px' }}>
        <div className="text-13" style={{ color: 'var(--text-secondary-strong)' }}>Loading usage data...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center" style={{ minHeight: '400px' }}>
        <div className="text-13" style={{ color: 'var(--text-error)' }}>{error}</div>
      </div>
    );
  }

  if (!usageData || !totals) {
    return (
      <div className="flex items-center justify-center" style={{ minHeight: '400px' }}>
        <div className="text-13" style={{ color: 'var(--text-secondary-strong)' }}>No usage data available</div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Summary Cards */}
      <div className="grid grid-cols-3 gap-4">
        <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
          <div className="text-[11px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>Total Requests</div>
          <div className="text-2xl font-semibold" style={{ color: 'var(--text-heading)' }}>{totals.totalCount.toLocaleString()}</div>
        </div>
        <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
          <div className="text-[11px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>Total Tokens</div>
          <div className="text-2xl font-semibold" style={{ color: 'var(--text-heading)' }}>{formatTokens(totals.totalTokens)}</div>
        </div>
        <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
          <div className="text-[11px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>Total Cost</div>
          <div className="text-2xl font-semibold" style={{ color: 'var(--text-heading)' }}>{formatCost(totals.totalCost)}</div>
        </div>
      </div>

      {/* Token Categories Breakdown */}
      {tokenCategories && (
        <div className="grid grid-cols-2 gap-4">
          <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
            <div className="text-[11px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>
              Старшие модели (GPT-5, Sonnet 4.5/Opus 4.1, Gemini 2.5 Pro)
            </div>
            <div className="text-xl font-semibold mb-2" style={{ color: 'var(--text-heading)' }}>
              {formatTokens(tokenCategories.premiumTokens.totalTokens)}
            </div>
            <div className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
              {tokenCategories.premiumTokens.count.toLocaleString()} запросов • {formatCost(tokenCategories.premiumTokens.costUSD)}
            </div>
            <div className="text-[10px] mt-1" style={{ color: 'var(--text-tertiary-weak)' }}>
              ({formatTokens(tokenCategories.premiumTokens.inputTokens)} вх / {formatTokens(tokenCategories.premiumTokens.outputTokens)} вых)
            </div>
          </div>
          <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
            <div className="text-[11px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>
              Остальные модели
            </div>
            <div className="text-xl font-semibold mb-2" style={{ color: 'var(--text-heading)' }}>
              {formatTokens(tokenCategories.regularTokens.totalTokens)}
            </div>
            <div className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
              {tokenCategories.regularTokens.count.toLocaleString()} запросов • {formatCost(tokenCategories.regularTokens.costUSD)}
            </div>
            <div className="text-[10px] mt-1" style={{ color: 'var(--text-tertiary-weak)' }}>
              ({formatTokens(tokenCategories.regularTokens.inputTokens)} вх / {formatTokens(tokenCategories.regularTokens.outputTokens)} вых)
            </div>
          </div>
        </div>
      )}

      {/* Date Range Controls */}
      <div className="flex items-center gap-4">
        <div className="flex gap-2">
          <button
            onClick={() => setDateRange('1d')}
            className="px-3 py-1 rounded text-12"
            style={{
              backgroundColor: dateRange === '1d' ? 'var(--white-10)' : 'transparent',
              color: 'var(--text-secondary-strong)',
            }}
          >
            1d
          </button>
          <button
            onClick={() => setDateRange('7d')}
            className="px-3 py-1 rounded text-12"
            style={{
              backgroundColor: dateRange === '7d' ? 'var(--white-10)' : 'transparent',
              color: 'var(--text-secondary-strong)',
            }}
          >
            7d
          </button>
          <button
            onClick={() => setDateRange('30d')}
            className="px-3 py-1 rounded text-12"
            style={{
              backgroundColor: dateRange === '30d' ? 'var(--white-10)' : 'transparent',
              color: 'var(--text-secondary-strong)',
            }}
          >
            30d
          </button>
        </div>
        <div className="flex items-center gap-2">
          <input
            type="date"
            value={selectedStartDate}
            onChange={(e) => setSelectedStartDate(e.target.value)}
            className="px-2 py-1 rounded text-12"
            style={{
              backgroundColor: 'var(--white-05)',
              border: '1px solid var(--overlay-line-90)',
              color: 'var(--text-heading)',
            }}
          />
          <span style={{ color: 'var(--text-tertiary-weak)' }}>to</span>
          <input
            type="date"
            value={selectedEndDate}
            onChange={(e) => setSelectedEndDate(e.target.value)}
            className="px-2 py-1 rounded text-12"
            style={{
              backgroundColor: 'var(--white-05)',
              border: '1px solid var(--overlay-line-90)',
              color: 'var(--text-heading)',
            }}
          />
        </div>
        <button
          onClick={() => {
            const endDate = new Date().toISOString().split('T')[0];
            const startDate = new Date();
            startDate.setDate(startDate.getDate() - (dateRange === '1d' ? 1 : dateRange === '7d' ? 7 : 30));
            setSelectedStartDate(startDate.toISOString().split('T')[0]);
            setSelectedEndDate(endDate);
          }}
          className="px-3 py-1 rounded text-12"
          style={{
            backgroundColor: 'var(--white-05)',
            border: '1px solid var(--overlay-line-90)',
            color: 'var(--text-secondary-strong)',
          }}
        >
          Reset
        </button>
      </div>

      {/* Charts */}
      {chartData.length > 0 && (
        <div className="space-y-6">
          {/* Daily Tokens Chart */}
          <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
            <div className="text-13 mb-4" style={{ color: 'var(--text-heading)' }}>Daily Token Usage</div>
            <ResponsiveContainer width="100%" height={300}>
              <LineChart data={chartData}>
                <CartesianGrid strokeDasharray="3 3" stroke="var(--overlay-line-90)" />
                <XAxis
                  dataKey="date"
                  tickFormatter={formatDate}
                  stroke="var(--text-tertiary-weak)"
                  tick={{ fill: 'var(--text-tertiary-weak)' }}
                  style={{ fontSize: '11px' }}
                />
                <YAxis
                  tickFormatter={formatTokens}
                  stroke="var(--text-tertiary-weak)"
                  tick={{ fill: 'var(--text-tertiary-weak)' }}
                  style={{ fontSize: '11px' }}
                />
                <Tooltip
                  contentStyle={{
                    backgroundColor: theme === 'dark' ? '#1a1a1a' : '#ffffff',
                    border: '1px solid var(--overlay-line-90)',
                    borderRadius: '6px',
                    color: theme === 'dark' ? 'var(--text-heading)' : 'var(--text-primary)',
                  }}
                  itemStyle={{
                    color: theme === 'dark' ? 'var(--text-heading)' : 'var(--text-primary)',
                  }}
                  labelStyle={{
                    color: theme === 'dark' ? 'var(--text-heading)' : 'var(--text-primary)',
                  }}
                  labelFormatter={(label) => formatDate(label)}
                  formatter={(value: any) => formatTokens(value)}
                />
                <Legend 
                  wrapperStyle={{
                    color: theme === 'dark' ? 'var(--text-heading)' : 'var(--text-primary)',
                  }}
                />
                <Line
                  type="monotone"
                  dataKey="tokens"
                  stroke="#4285F4"
                  strokeWidth={2}
                  name="Tokens"
                  dot={{ r: 3 }}
                />
              </LineChart>
            </ResponsiveContainer>
          </div>

          {/* Daily Cost Chart */}
          <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
            <div className="text-13 mb-4" style={{ color: 'var(--text-heading)' }}>Daily Cost</div>
            <ResponsiveContainer width="100%" height={300}>
              <BarChart data={chartData}>
                <CartesianGrid strokeDasharray="3 3" stroke="var(--overlay-line-90)" />
                <XAxis
                  dataKey="date"
                  tickFormatter={formatDate}
                  stroke="var(--text-tertiary-weak)"
                  tick={{ fill: 'var(--text-tertiary-weak)' }}
                  style={{ fontSize: '11px' }}
                />
                <YAxis
                  tickFormatter={(value) => formatCost(value)}
                  stroke="var(--text-tertiary-weak)"
                  tick={{ fill: 'var(--text-tertiary-weak)' }}
                  style={{ fontSize: '11px' }}
                />
                <Tooltip
                  contentStyle={{
                    backgroundColor: theme === 'dark' ? '#1a1a1a' : '#ffffff',
                    border: '1px solid var(--overlay-line-90)',
                    borderRadius: '6px',
                    color: theme === 'dark' ? 'var(--text-heading)' : 'var(--text-primary)',
                  }}
                  itemStyle={{
                    color: theme === 'dark' ? 'var(--text-heading)' : 'var(--text-primary)',
                  }}
                  labelStyle={{
                    color: theme === 'dark' ? 'var(--text-heading)' : 'var(--text-primary)',
                  }}
                  labelFormatter={(label) => formatDate(label)}
                  formatter={(value: any, name: string) => {
                    if (name === 'cost' || name === 'Cost (USD)') {
                      const numValue = typeof value === 'number' ? value : parseFloat(value);
                      return formatCost(numValue);
                    }
                    return value;
                  }}
                />
                <Legend 
                  wrapperStyle={{
                    color: theme === 'dark' ? 'var(--text-heading)' : 'var(--text-primary)',
                  }}
                />
                <Bar dataKey="cost" fill="#10A37F" name="Cost (USD)" />
              </BarChart>
            </ResponsiveContainer>
          </div>
        </div>
      )}

      {/* Model Breakdown */}
      {modelData.length > 0 && (
        <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
          <div className="text-13 mb-4" style={{ color: 'var(--text-heading)' }}>Usage by Model</div>
          <ResponsiveContainer width="100%" height={Math.max(200, modelData.length * 50)}>
            <BarChart data={modelData} layout="vertical" margin={{ top: 5, right: 30, bottom: 5, left: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="var(--overlay-line-90)" />
              <XAxis 
                type="number" 
                stroke="var(--text-tertiary-weak)" 
                tick={{ fill: 'var(--text-tertiary-weak)' }}
                style={{ fontSize: '11px' }}
                tickFormatter={(value) => formatCost(value)}
                domain={[0, 'dataMax']}
              />
              <YAxis
                type="category"
                dataKey="model"
                width={150}
                stroke="var(--text-tertiary-weak)"
                tick={{ fill: 'var(--text-tertiary-weak)' }}
                style={{ fontSize: '11px' }}
                interval={0}
                domain={modelData.map((item) => item.model)}
                allowDataOverflow={false}
              />
              <Tooltip
                contentStyle={{
                  backgroundColor: theme === 'dark' ? '#1a1a1a' : '#ffffff',
                  border: '1px solid var(--overlay-line-90)',
                  borderRadius: '6px',
                  color: theme === 'dark' ? 'var(--text-heading)' : 'var(--text-primary)',
                }}
                itemStyle={{
                  color: theme === 'dark' ? 'var(--text-heading)' : 'var(--text-primary)',
                }}
                labelStyle={{
                  color: theme === 'dark' ? 'var(--text-heading)' : 'var(--text-primary)',
                }}
                formatter={(value: any, name: string) => {
                  // Recharts may pass value as array for some chart types
                  const actualValue = Array.isArray(value) ? value[0] : value;
                  
                  // For "Usage by Model" chart, name is "Cost (USD)" and value is costUSD
                  if (name === 'Cost (USD)' || name === 'costUSD') {
                    return formatCost(actualValue);
                  }
                  if (name === 'Tokens' || name === 'totalTokens') {
                    return formatTokens(actualValue);
                  }
                  // Fallback: format any number < 1 as cost (likely USD)
                  if (typeof actualValue === 'number' && actualValue < 1 && actualValue > 0) {
                    return formatCost(actualValue);
                  }
                  return actualValue;
                }}
              />
              <Legend 
                wrapperStyle={{
                  color: theme === 'dark' ? 'var(--text-heading)' : 'var(--text-primary)',
                }}
              />
              <Bar dataKey="costUSD" name="Cost (USD)" radius={[0, 4, 4, 0]}>
                {modelData.map((entry, index) => (
                  <Cell key={`cell-${index}`} fill={getProviderColor(entry.provider)} />
                ))}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        </div>
      )}

      {/* All Events Table */}
      {usageData.events && usageData.events.length > 0 && (
        <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
          <div className="flex items-center justify-between mb-4">
            <div className="text-13" style={{ color: 'var(--text-heading)' }}>All Events</div>
            <button
              onClick={async () => {
                // Export CSV functionality
                if (!usageData.events) return;
                const csv = [
                  ['Date', 'Model', 'Kind', 'Tokens', 'Cost'].join(','),
                  ...usageData.events.map(e => [
                    new Date(e.createdAt).toLocaleString(),
                    e.model,
                    e.kind,
                    e.totalTokens,
                    e.costUSD.toFixed(4),
                  ].join(',')),
                ].join('\n');
                const blob = new Blob([csv], { type: 'text/csv' });
                const url = URL.createObjectURL(blob);
                const a = document.createElement('a');
                a.href = url;
                a.download = `usage-${usageData.period.startDate}-${usageData.period.endDate}.csv`;
                a.click();
                URL.revokeObjectURL(url);
              }}
              className="px-3 py-1 rounded text-12"
              style={{
                backgroundColor: 'var(--white-10)',
                color: 'var(--text-secondary-strong)',
              }}
            >
              Export CSV
            </button>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full" style={{ fontSize: '12px' }}>
              <thead>
                <tr style={{ borderBottom: '1px solid var(--overlay-line-90)' }}>
                  <th className="text-left py-2 px-2" style={{ color: 'var(--text-tertiary-weak)' }}>Date</th>
                  <th className="text-left py-2 px-2" style={{ color: 'var(--text-tertiary-weak)' }}>Model</th>
                  <th className="text-left py-2 px-2" style={{ color: 'var(--text-tertiary-weak)' }}>Kind</th>
                  <th className="text-right py-2 px-2" style={{ color: 'var(--text-tertiary-weak)' }}>Tokens</th>
                  <th className="text-right py-2 px-2" style={{ color: 'var(--text-tertiary-weak)' }}>Cost</th>
                </tr>
              </thead>
              <tbody>
                {groupedEvents
                  .slice((eventsPage - 1) * eventsPerPage, eventsPage * eventsPerPage)
                  .map((group, index) => (
                  <tr key={`group-${index}-${group.startTime.getTime()}`} style={{ borderBottom: '1px solid var(--overlay-line-10)' }}>
                    <td className="py-2 px-2" style={{ color: 'var(--text-secondary-strong)' }}>
                      <div>
                        {formatTimeRange(group.startTime, group.endTime)}
                        {group.count > 1 && (
                          <div className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                            {group.count} событий
                          </div>
                        )}
                      </div>
                    </td>
                    <td className="py-2 px-2" style={{ color: 'var(--text-secondary-strong)' }}>
                      <div>
                        <div>{group.model}</div>
                        {group.modelAlias && (
                          <div className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                            {group.modelAlias}
                          </div>
                        )}
                      </div>
                    </td>
                    <td className="py-2 px-2">
                      <span
                        className="px-2 py-0.5 rounded text-[10px]"
                        style={{
                          backgroundColor: group.kind === 'included' ? 'rgba(34, 197, 94, 0.1)' : 'rgba(251, 191, 36, 0.1)',
                          color: group.kind === 'included' ? 'rgb(34, 197, 94)' : 'rgb(251, 191, 36)',
                        }}
                      >
                        {group.kind}
                      </span>
                    </td>
                    <td className="text-right py-2 px-2" style={{ color: 'var(--text-secondary-strong)' }}>
                      {formatTokens(group.totalTokens)}
                    </td>
                    <td className="text-right py-2 px-2" style={{ color: 'var(--text-secondary-strong)' }}>
                      {formatCost(group.totalCost)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {groupedEvents.length > eventsPerPage && (
              <div className="flex items-center justify-between mt-4">
                <div className="text-[11px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                  Показано {((eventsPage - 1) * eventsPerPage) + 1} - {Math.min(eventsPage * eventsPerPage, groupedEvents.length)} из {groupedEvents.length} групп ({usageData.events.length} событий)
                </div>
                <div className="flex gap-2">
                  <button
                    onClick={() => setEventsPage(p => Math.max(1, p - 1))}
                    disabled={eventsPage === 1}
                    className="px-3 py-1 rounded text-12"
                    style={{
                      backgroundColor: eventsPage === 1 ? 'var(--white-05)' : 'var(--white-10)',
                      color: eventsPage === 1 ? 'var(--text-tertiary-weak)' : 'var(--text-secondary-strong)',
                      cursor: eventsPage === 1 ? 'not-allowed' : 'pointer',
                    }}
                  >
                    Previous
                  </button>
                  <button
                    onClick={() => setEventsPage(p => p + 1)}
                    disabled={eventsPage * eventsPerPage >= usageData.events.length}
                    className="px-3 py-1 rounded text-12"
                    style={{
                      backgroundColor: eventsPage * eventsPerPage >= usageData.events.length ? 'var(--white-05)' : 'var(--white-10)',
                      color: eventsPage * eventsPerPage >= usageData.events.length ? 'var(--text-tertiary-weak)' : 'var(--text-secondary-strong)',
                      cursor: eventsPage * eventsPerPage >= usageData.events.length ? 'not-allowed' : 'pointer',
                    }}
                  >
                    Next
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

