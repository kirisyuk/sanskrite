import React from 'react';
import { LineChart, Line, BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer, Cell } from 'recharts';
import { api } from '../lib/api';
import { useAuth } from '../contexts/AuthContext';
import { ThemeContext } from './ThemeProvider';
import { useTranslation } from 'react-i18next';

export const UsageTab: React.FC = () => {
  const { t } = useTranslation();
  const { theme } = React.useContext(ThemeContext);
  const { currentUser } = useAuth();
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const [usageData, setUsageData] = React.useState<{
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
    totals: {
      inputTokens: number;
      outputTokens: number;
      totalTokens: number;
      costUSD: number;
      count: number;
    };
    period: {
      startDate: string;
      endDate: string;
    };
  } | null>(null);

  // Separate state for paginated events
  const [eventsData, setEventsData] = React.useState<{
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
      taskType?: string;
      useCase?: string; // Business purpose: "chat", "bias_analysis", "frame_decomposition", etc.
      kind: string;
      createdAt: string;
    }>;
    pagination: {
      page: number;
      limit: number;
      total: number;
      totalPages: number;
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
  const [eventsLoading, setEventsLoading] = React.useState(false);
  const eventsPerPage = 20;
  const eventsLimit = 200; // Minimum limit for efficient grouping

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
          totals: data.totals,
          dailyTotalsKeys: Object.keys(data.dailyTotals || {}),
          dailyTotalsSample: Object.entries(data.dailyTotals || {}).slice(0, 2),
          byModelKeys: Object.keys(data.byModel || {}),
          byModelSample: Object.entries(data.byModel || {}).slice(0, 2),
          byModelRaw: data.byModel,
          hasByModel: !!data.byModel,
          byModelType: typeof data.byModel,
        });
        setUsageData(data);
      } catch (err: any) {
        setError(err?.message || t('account.tabs.usageDetails.failedToLoad'));
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

  // Load paginated events separately
  React.useEffect(() => {
    if (!currentUser?.uid || !usageData) {
      console.log('[UsageTab] Skipping events load:', {
        hasUser: !!currentUser?.uid,
        hasUsageData: !!usageData,
      });
      return;
    }

    const loadEvents = async () => {
      setEventsLoading(true);
      try {
        const endDate = selectedEndDate || usageData.period.endDate;
        const startDate = selectedStartDate || usageData.period.startDate;
        
        console.log('[UsageTab] Loading events with params:', {
          startDate,
          endDate,
          page: eventsPage,
          limit: eventsLimit,
          userId: currentUser?.uid,
        });
        
        const data = await api.getUsageEvents({
          startDate,
          endDate,
          page: eventsPage,
          limit: eventsLimit,
        });
        console.log('[UsageTab] Received events data:', {
          eventsCount: data.events.length,
          pagination: data.pagination,
          period: data.period,
          firstEvent: data.events[0],
          lastEvent: data.events[data.events.length - 1],
        });
        setEventsData(data);
      } catch (err: any) {
        console.error('[UsageTab] Error loading events:', err);
        console.error('[UsageTab] Error details:', {
          message: err?.message,
          response: err?.response?.data,
          status: err?.response?.status,
        });
        // Don't show error to user, just log it
      } finally {
        setEventsLoading(false);
      }
    };

    loadEvents();
  }, [currentUser?.uid, usageData, eventsPage, selectedStartDate, selectedEndDate]);

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
          inputTokens: totals?.inputTokens ?? totals?.InputTokens ?? 0,
          outputTokens: totals?.outputTokens ?? totals?.OutputTokens ?? 0,
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
      rawByModel: usageData.byModel,
    });
    
    if (entries.length === 0) {
      console.log('[UsageTab] byModel is empty object');
      return [];
    }
    
    const result = entries
      .map(([key, stats]: [string, any], index: number) => {
        // Debug: log what we're getting for first entry
        if (index === 0) {
          console.log('[UsageTab] First byModel stats object:', stats, 'keys:', Object.keys(stats || {}), 'type:', typeof stats);
        }
        const parts = key.split(':');
        const costUSD = stats?.costUSD ?? stats?.CostUSD ?? 0;
        const totalTokens = stats?.totalTokens ?? stats?.TotalTokens ?? 0;
        const count = stats?.count ?? stats?.Count ?? 0;
        const modelName = parts.length > 1 ? parts[1] : key; // Remove provider prefix
        
        return {
          model: modelName,
          provider: parts.length > 1 ? parts[0] : 'unknown',
          count: count,
          inputTokens: stats?.inputTokens ?? stats?.InputTokens ?? 0,
          outputTokens: stats?.outputTokens ?? stats?.OutputTokens ?? 0,
          totalTokens: totalTokens,
          costUSD: costUSD,
        };
      })
      .filter((item) => {
        // Filter out entries with zero tokens AND zero cost, empty model name, or invalid data
        // Show models even if cost is 0, as long as they have tokens or count
        const hasUsage = item.totalTokens > 0 || item.count > 0;
        const isValid = item.model && item.model.trim() !== '';
        const shouldInclude = hasUsage && isValid;
        
        if (!shouldInclude) {
          console.log('[UsageTab] Filtering out model:', item.model, {
            totalTokens: item.totalTokens,
            count: item.count,
            costUSD: item.costUSD,
            isValid,
          });
        }
        
        return shouldInclude;
      })
      .sort((a, b) => {
        // Sort by cost first, then by tokens, then by count
        if (b.costUSD !== a.costUSD) {
          return b.costUSD - a.costUSD;
        }
        if (b.totalTokens !== a.totalTokens) {
          return b.totalTokens - a.totalTokens;
        }
        return b.count - a.count;
      })
      .slice(0, 10); // Top 10 models
    
    console.log('[UsageTab] Model data result:', {
      count: result.length,
      models: result.map(m => ({ model: m.model, cost: m.costUSD, tokens: m.totalTokens, count: m.count })),
    });
    return result;
  }, [usageData]);

  // Use totals from API
  const totals = React.useMemo(() => {
    if (!usageData?.totals) return null;
    
    return {
      totalTokens: usageData.totals.totalTokens,
      totalCost: usageData.totals.costUSD,
      totalCount: usageData.totals.count,
    };
  }, [usageData]);

  // Calculate token category totals
  const tokenCategories = React.useMemo(() => {
    if (!usageData?.tokenCategories) return null;
    return usageData.tokenCategories;
  }, [usageData]);

  // Format model name for display (defined early so it can be used in premiumModelNames)
  const formatModelName = (modelName: string): string => {
    if (!modelName) return modelName;
    
    // Handle Tavily models: convert "tavily-search-basic" -> "Tavily Search"
    if (modelName.includes('tavily-')) {
      return 'Tavily Search';
    }
    
    // Handle Claude models: convert "claude-sonnet-4-5-20250929" -> "claude sonnet"
    if (modelName.includes('claude-')) {
      const parts = modelName.split('-');
      if (parts.length >= 2) {
        // Extract model type (sonnet, opus, haiku, etc.)
        const modelType = parts[1];
        return `claude ${modelType}`;
      }
    }
    
    // Handle Gemini models: keep readable parts
    if (modelName.includes('gemini-')) {
      const parts = modelName.split('-');
      if (parts.length >= 2) {
        // Extract version and type (e.g., "2.5-pro", "2.5-flash-lite")
        const versionType = parts.slice(1).join(' ').replace(/\d{8}/g, '').trim(); // Remove date patterns
        return `gemini ${versionType}`;
      }
    }
    
    // Handle Grok models
    if (modelName.includes('grok-')) {
      // Format grok-4-0709 as "Grok 4.1"
      if (modelName.includes('grok-4-0709')) {
        return 'Grok 4.1';
      }
      // Format grok-4-1-* as "Grok 4.1" (but exclude fast versions in premium detection)
      if (modelName.includes('grok-4-1-') && !modelName.includes('fast')) {
        return 'Grok 4.1';
      }
      return modelName.replace(/-/g, ' ').replace(/\d{8}/g, '').trim();
    }
    
    // Handle GPT models
    if (modelName.includes('gpt-')) {
      const parts = modelName.split('-');
      if (parts.length >= 2) {
        return parts.slice(0, 2).join(' '); // Keep "gpt-5" format
      }
    }
    
    // Default: replace dashes with spaces, remove date patterns
    return modelName.replace(/-/g, ' ').replace(/\d{8}/g, '').trim();
  };

  // Helper function to determine if a model is premium based on modelAlias
  const isPremiumModel = (provider: string, modelAlias?: string): boolean => {
    if (!modelAlias) return false;
    // Check for _quality suffix (any provider)
    if (modelAlias.endsWith('_quality')) return true;
    // Check for _balanced suffix (Claude only)
    if (provider === 'anthropic' && modelAlias.endsWith('_balanced')) return true;
    return false;
  };

  // Collect premium model names from events or byModel
  const premiumModelNames = React.useMemo(() => {
    const premiumModels = new Set<string>();
    
    // First, try to get from events (most accurate, has modelAlias)
    if (eventsData?.events && eventsData.events.length > 0) {
      eventsData.events.forEach(event => {
        if (isPremiumModel(event.provider, event.modelAlias)) {
          // Format model name nicely
          const formatted = formatModelName(event.model);
          premiumModels.add(formatted);
        }
      });
    }
    
    // Fallback: determine premium models from byModel by checking model names
    // This covers cases where events aren't loaded yet, or when modelAlias is missing
    // We always check byModel to catch premium models that might not have modelAlias set
    if (usageData?.byModel) {
      Object.keys(usageData.byModel).forEach(key => {
        const [provider, model] = key.split(':');
        const modelLower = model.toLowerCase();
        
        // GPT premium models: gpt-5, gpt-5.1, gpt-5.2, etc. (but not gpt-5-nano, gpt-5-mini)
        if (provider === 'openai' && modelLower.includes('gpt-5') && 
            !modelLower.includes('nano') && !modelLower.includes('mini')) {
          premiumModels.add(formatModelName(model));
        }
        
        // Gemini premium models: gemini-2.5-pro, gemini-3-pro (but not flash or flash-lite)
        if (provider === 'gemini' && 
            (modelLower.includes('gemini-2.5-pro') || modelLower.includes('gemini-3-pro'))) {
          premiumModels.add(formatModelName(model));
        }
        
        // Claude premium models: all Opus versions (claude-opus-*) and Sonnet 4.5
        // Opus is premium (claude-opus-*), Sonnet 4.5 is also premium (claude-sonnet-4-5-*)
        if (provider === 'anthropic') {
          if (modelLower.includes('claude-opus-')) {
            // All Opus versions are premium
            premiumModels.add(formatModelName(model));
          } else if (modelLower.includes('claude-sonnet-4-5-') || modelLower.includes('claude-sonnet-4.5-')) {
            // Sonnet 4.5 is premium
            premiumModels.add(formatModelName(model));
          }
        }
        
        // Grok premium models: grok-4-1-* but not fast versions (grok-4-1-fast-*)
        if (provider === 'grok' && modelLower.includes('grok-4-1-') && !modelLower.includes('fast')) {
          premiumModels.add(formatModelName(model));
        }
        // Also check for grok-4-0709 (quality model)
        if (provider === 'grok' && modelLower.includes('grok-4-0709')) {
          premiumModels.add(formatModelName(model));
        }
      });
    }
    
    return Array.from(premiumModels).sort();
  }, [eventsData?.events, usageData?.byModel]);

  // Group events by model and time proximity, or consecutive same-model events
  const groupedEvents = React.useMemo(() => {
    if (!eventsData?.events || eventsData.events.length === 0) return [];
    
    const TIME_WINDOW_MS = 60000; // 60 seconds (1 minute) window for grouping
    // Sort events chronologically (oldest first) for proper consecutive checking
    const sortedEvents = [...eventsData.events].sort((a, b) => 
      new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime()
    );
    
    const groups: Array<{
      model: string;
      modelAlias?: string;
      useCase?: string;
      events: typeof sortedEvents;
      totalTokens: number;
      totalCost: number;
      startTime: Date;
      endTime: Date;
      count: number;
    }> = [];
    
    for (const event of sortedEvents) {
      const eventTime = new Date(event.createdAt).getTime();
      // Group by model + alias + useCase (not kind) - don't group same models with different use cases
      const eventKey = `${event.model}:${event.modelAlias || ''}:${event.useCase || 'unknown'}`;
      
      // First, try to find an existing group that matches model/alias/useCase and is within time window
      let foundGroup = groups.find(group => {
        const groupKey = `${group.model}:${group.modelAlias || ''}:${group.useCase || 'unknown'}`;
        if (groupKey !== eventKey) return false;
        
        const groupStartTime = group.startTime.getTime() - TIME_WINDOW_MS;
        const groupEndTime = group.endTime.getTime() + TIME_WINDOW_MS;
        // Check if event is within expanded time window
        return eventTime >= groupStartTime && eventTime <= groupEndTime;
      });
      
      // If not found by time window, check if this event is consecutive (same model/useCase, no other models in between)
      // Find groups that match the same model/alias/useCase and check if event is consecutive to them
      if (!foundGroup) {
        // Find all groups with matching model/alias/useCase
        const matchingGroups = groups.filter(group => {
          const groupKey = `${group.model}:${group.modelAlias || ''}:${group.useCase || 'unknown'}`;
          return groupKey === eventKey;
        });
        
        // Helper function to check if there are other models between two time points
        const hasOtherModelsBetween = (startTime: number, endTime: number) => {
          return sortedEvents.some(otherEvent => {
            // Skip the current event
            if (otherEvent.id === event.id) return false;
            
            const otherEventTime = new Date(otherEvent.createdAt).getTime();
            const otherEventKey = `${otherEvent.model}:${otherEvent.modelAlias || ''}:${otherEvent.useCase || 'unknown'}`;
            
            // Check if other event is between startTime and endTime and has different model/useCase
            if (otherEventTime >= startTime && otherEventTime <= endTime && otherEventKey !== eventKey) {
              return true;
            }
            return false;
          });
        };
        
        // Check if this event is consecutive to any matching group
        // Check both directions: event before group and event after group
        for (const group of matchingGroups) {
          const groupStartTime = group.startTime.getTime();
          const groupEndTime = group.endTime.getTime();
          
          // Case 1: Event comes before the group (eventTime < groupStartTime)
          if (eventTime < groupStartTime) {
            if (!hasOtherModelsBetween(eventTime, groupStartTime)) {
              foundGroup = group;
              break;
            }
          }
          
          // Case 2: Event comes after the group (eventTime > groupEndTime)
          if (!foundGroup && eventTime > groupEndTime) {
            if (!hasOtherModelsBetween(groupEndTime, eventTime)) {
              foundGroup = group;
              break;
            }
          }
        }
      }
      
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
          useCase: event.useCase,
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
  }, [eventsData?.events]);

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

  const formatUseCase = (useCase: string) => {
    if (!useCase) return 'Unknown';
    // Convert snake_case to Title Case
    return useCase
      .split('_')
      .map(word => word.charAt(0).toUpperCase() + word.slice(1))
      .join(' ');
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
      tavily: '#8B5CF6', // Purple color for Tavily
    };
    return colors[provider] || '#6B7280';
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center" style={{ minHeight: '400px' }}>
        <div className="text-13" style={{ color: 'var(--text-secondary-strong)' }}>{t('account.tabs.usageDetails.loading')}</div>
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
        <div className="text-13" style={{ color: 'var(--text-secondary-strong)' }}>{t('account.tabs.usageDetails.noDataAvailable')}</div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Summary Cards */}
      <div className="grid grid-cols-3 gap-4">
        <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
          <div className="text-[11px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>{t('account.tabs.usageDetails.totalRequests')}</div>
          <div className="text-2xl font-semibold" style={{ color: 'var(--text-heading)' }}>{totals.totalCount.toLocaleString()}</div>
        </div>
        <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
          <div className="text-[11px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>{t('account.tabs.usageDetails.totalTokens')}</div>
          <div className="text-2xl font-semibold" style={{ color: 'var(--text-heading)' }}>{formatTokens(totals.totalTokens)}</div>
        </div>
        <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
          <div className="text-[11px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>{t('account.tabs.usageDetails.totalCost')}</div>
          <div className="text-2xl font-semibold" style={{ color: 'var(--text-heading)' }}>{formatCost(totals.totalCost)}</div>
        </div>
      </div>

      {/* Token Categories Breakdown */}
      {tokenCategories && (
        <div className="grid grid-cols-2 gap-4">
          <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
            <div className="text-[11px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>
              {premiumModelNames.length > 0 
                ? `Premium models (${premiumModelNames.join(', ')})`
                : t('account.tabs.usageDetails.premiumModels')}
            </div>
            <div className="text-xl font-semibold mb-2" style={{ color: 'var(--text-heading)' }}>
              {formatTokens(tokenCategories.premiumTokens.totalTokens)}
            </div>
            <div className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
              {tokenCategories.premiumTokens.count.toLocaleString()} {t('account.tabs.usageDetails.requests')} • {formatCost(tokenCategories.premiumTokens.costUSD)}
            </div>
            <div className="text-[10px] mt-1" style={{ color: 'var(--text-tertiary-weak)' }}>
              ({formatTokens(tokenCategories.premiumTokens.inputTokens)} {t('account.tabs.usageDetails.input')} / {formatTokens(tokenCategories.premiumTokens.outputTokens)} {t('account.tabs.usageDetails.output')})
            </div>
          </div>
          <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
            <div className="text-[11px] mb-1" style={{ color: 'var(--text-tertiary-weak)' }}>
              {t('account.tabs.usageDetails.otherModels')}
            </div>
            <div className="text-xl font-semibold mb-2" style={{ color: 'var(--text-heading)' }}>
              {formatTokens(tokenCategories.regularTokens.totalTokens)}
            </div>
            <div className="text-[10px]" style={{ color: 'var(--text-tertiary-weak)' }}>
              {tokenCategories.regularTokens.count.toLocaleString()} {t('account.tabs.usageDetails.requests')} • {formatCost(tokenCategories.regularTokens.costUSD)}
            </div>
            <div className="text-[10px] mt-1" style={{ color: 'var(--text-tertiary-weak)' }}>
              ({formatTokens(tokenCategories.regularTokens.inputTokens)} {t('account.tabs.usageDetails.input')} / {formatTokens(tokenCategories.regularTokens.outputTokens)} {t('account.tabs.usageDetails.output')})
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
          <span style={{ color: 'var(--text-tertiary-weak)' }}>{t('account.tabs.usageDetails.to')}</span>
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
          {t('account.tabs.usageDetails.reset')}
        </button>
      </div>

      {/* Charts */}
      {chartData.length > 0 && (
        <div className="space-y-6">
          {/* Daily Tokens Chart */}
          <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
            <div className="text-13 mb-4" style={{ color: 'var(--text-heading)' }}>{t('account.tabs.usageDetails.dailyTokenUsage')}</div>
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
                  cursor={{
                    stroke: theme === 'dark' ? 'var(--white-20)' : '#ccc',
                    strokeWidth: 1,
                    strokeDasharray: '3 3'
                  }}
                  wrapperStyle={{
                    backgroundColor: theme === 'dark' ? 'var(--white-05)' : '#ffffff',
                    border: '1px solid var(--overlay-line-90)',
                    borderRadius: '6px',
                  }}
                  contentStyle={{
                    backgroundColor: theme === 'dark' ? 'var(--white-05)' : '#ffffff',
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
                    if (name === t('account.tabs.usageDetails.total') || name === t('account.tabs.usageDetails.input') || name === t('account.tabs.usageDetails.output')) {
                      return formatTokens(value);
                    }
                    return formatTokens(value);
                  }}
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
                  name={t('account.tabs.usageDetails.total')}
                  dot={{ r: 3 }}
                  activeDot={{ 
                    r: 5, 
                    fill: '#4285F4',
                    stroke: theme === 'dark' ? 'var(--white-10)' : '#ffffff',
                    strokeWidth: 2
                  }}
                />
                <Line
                  type="monotone"
                  dataKey="inputTokens"
                  stroke="#10A37F"
                  strokeWidth={2}
                  name={t('account.tabs.usageDetails.input')}
                  dot={{ r: 3 }}
                  activeDot={{ 
                    r: 5, 
                    fill: '#10A37F',
                    stroke: theme === 'dark' ? 'var(--white-10)' : '#ffffff',
                    strokeWidth: 2
                  }}
                />
                <Line
                  type="monotone"
                  dataKey="outputTokens"
                  stroke="#FF6B35"
                  strokeWidth={2}
                  name={t('account.tabs.usageDetails.output')}
                  dot={{ r: 3 }}
                  activeDot={{ 
                    r: 5, 
                    fill: '#FF6B35',
                    stroke: theme === 'dark' ? 'var(--white-10)' : '#ffffff',
                    strokeWidth: 2
                  }}
                />
              </LineChart>
            </ResponsiveContainer>
          </div>

          {/* Daily Cost Chart */}
          <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
            <div className="text-13 mb-4" style={{ color: 'var(--text-heading)' }}>{t('account.tabs.usageDetails.dailyCost')}</div>
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
                  cursor={{
                    fill: theme === 'dark' ? 'rgba(255, 255, 255, 0.05)' : 'rgba(0, 0, 0, 0.05)'
                  }}
                  wrapperStyle={{
                    backgroundColor: theme === 'dark' ? 'var(--white-05)' : '#ffffff',
                    border: '1px solid var(--overlay-line-90)',
                    borderRadius: '6px',
                  }}
                  contentStyle={{
                    backgroundColor: theme === 'dark' ? 'var(--white-05)' : '#ffffff',
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
                    if (name === 'cost' || name === t('account.tabs.usageDetails.costUSD')) {
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
                <Bar 
                  dataKey="cost" 
                  fill="#10A37F" 
                  name={t('account.tabs.usageDetails.costUSD')}
                  activeBar={{
                    fill: '#10A37F',
                    stroke: theme === 'dark' ? 'var(--white-10)' : '#ffffff',
                    strokeWidth: 2,
                    opacity: theme === 'dark' ? 0.9 : 0.8
                  }}
                />
              </BarChart>
            </ResponsiveContainer>
          </div>
        </div>
      )}

      {/* Model Breakdown */}
      {modelData.length > 0 && (
        <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
          <div className="text-13 mb-4" style={{ color: 'var(--text-heading)' }}>{t('account.tabs.usageDetails.usageByModel')}</div>
          <ResponsiveContainer width="100%" height={Math.max(200, modelData.length * 50)}>
            <BarChart data={modelData} layout="vertical" margin={{ top: 5, right: 30, bottom: 5, left: 10 }}>
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
                width={220}
                stroke="var(--text-tertiary-weak)"
                tick={{ fill: 'var(--text-tertiary-weak)' }}
                style={{ fontSize: '11px' }}
                interval={0}
                domain={modelData.map((item) => item.model)}
                allowDataOverflow={false}
                tickFormatter={formatModelName}
              />
              <Tooltip
                cursor={{
                  fill: theme === 'dark' ? 'rgba(255, 255, 255, 0.05)' : 'rgba(0, 0, 0, 0.05)'
                }}
                wrapperStyle={{
                  backgroundColor: theme === 'dark' ? 'var(--white-05)' : '#ffffff',
                  border: '1px solid var(--overlay-line-90)',
                  borderRadius: '6px',
                }}
                contentStyle={{
                  backgroundColor: theme === 'dark' ? 'var(--white-05)' : '#ffffff',
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
                labelFormatter={(label) => {
                  // Find the full model name from modelData (label is formatted, need to find original)
                  const entry = modelData.find(item => formatModelName(item.model) === label);
                  return entry ? entry.model : label;
                }}
                formatter={(value: any, name: string) => {
                  // Recharts may pass value as array for some chart types
                  const actualValue = Array.isArray(value) ? value[0] : value;
                  
                  // For "Usage by Model" chart, name is "Cost (USD)" and value is costUSD
                  if (name === t('account.tabs.usageDetails.costUSD') || name === 'costUSD') {
                    return formatCost(actualValue);
                  }
                  if (name === t('account.tabs.usageDetails.tokens') || name === 'totalTokens') {
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
              <Bar 
                dataKey="costUSD" 
                name={t('account.tabs.usageDetails.costUSD')} 
                radius={[0, 4, 4, 0]}
                activeBar={{
                  stroke: theme === 'dark' ? 'var(--white-10)' : '#ffffff',
                  strokeWidth: 2,
                  opacity: theme === 'dark' ? 0.9 : 0.8
                }}
              >
                {modelData.map((entry, index) => (
                  <Cell key={`cell-${index}`} fill={getProviderColor(entry.provider)} />
                ))}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        </div>
      )}

      {/* All Events Table */}
      {eventsData && eventsData.events.length > 0 && (
        <div className="p-4 rounded-lg" style={{ backgroundColor: 'var(--white-05)', border: '1px solid var(--overlay-line-90)' }}>
          <div className="flex items-center justify-between mb-4">
            <div className="text-13" style={{ color: 'var(--text-heading)' }}>{t('account.tabs.usageDetails.allEvents')}</div>
            <button
              onClick={async () => {
                // Export CSV functionality - export current page events
                if (!eventsData?.events) return;
                const csv = [
                  [t('account.tabs.usageDetails.date'), t('account.tabs.usageDetails.model'), 'Use Case', t('account.tabs.usageDetails.tokens'), t('account.tabs.usageDetails.cost')].join(','),
                  ...eventsData.events.map(e => [
                    new Date(e.createdAt).toLocaleString(),
                    e.model,
                    e.useCase || 'unknown',
                    e.totalTokens,
                    e.costUSD.toFixed(4),
                  ].join(',')),
                ].join('\n');
                const blob = new Blob([csv], { type: 'text/csv' });
                const url = URL.createObjectURL(blob);
                const a = document.createElement('a');
                a.href = url;
                a.download = `usage-${eventsData.period.startDate}-${eventsData.period.endDate}-page-${eventsData.pagination.page}.csv`;
                a.click();
                URL.revokeObjectURL(url);
              }}
              disabled={eventsLoading}
              className="px-3 py-1 rounded text-12"
              style={{
                backgroundColor: 'var(--white-10)',
                color: 'var(--text-secondary-strong)',
              }}
            >
              {t('account.tabs.usageDetails.exportCSV')}
            </button>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full" style={{ fontSize: '12px' }}>
              <thead>
                <tr style={{ borderBottom: '1px solid var(--overlay-line-90)' }}>
                  <th className="text-left py-2 px-2" style={{ color: 'var(--text-tertiary-weak)' }}>{t('account.tabs.usageDetails.date')}</th>
                  <th className="text-left py-2 px-2" style={{ color: 'var(--text-tertiary-weak)' }}>{t('account.tabs.usageDetails.model')}</th>
                  <th className="text-left py-2 px-2" style={{ color: 'var(--text-tertiary-weak)' }}>Use Case</th>
                  <th className="text-right py-2 px-2" style={{ color: 'var(--text-tertiary-weak)' }}>{t('account.tabs.usageDetails.tokens')}</th>
                  <th className="text-right py-2 px-2" style={{ color: 'var(--text-tertiary-weak)' }}>{t('account.tabs.usageDetails.cost')}</th>
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
                            {group.count} {t('account.tabs.usageDetails.events')}
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
                          backgroundColor: 'var(--white-10)',
                          color: 'var(--text-secondary-strong)',
                        }}
                      >
                        {group.useCase ? formatUseCase(group.useCase) : 'unknown'}
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
            {(groupedEvents.length > eventsPerPage || (eventsData.pagination && eventsData.pagination.totalPages > 1)) && (
              <div className="flex items-center justify-between mt-4">
                <div className="text-[11px]" style={{ color: 'var(--text-tertiary-weak)' }}>
                  {eventsLoading ? (
                    t('account.tabs.usageDetails.loading')
                  ) : (
                    <>
                      {t('account.tabs.usageDetails.showing', {
                        start: ((eventsPage - 1) * eventsPerPage) + 1,
                        end: Math.min(eventsPage * eventsPerPage, groupedEvents.length),
                        total: groupedEvents.length,
                        eventsCount: eventsData.pagination?.total || groupedEvents.length
                      })}
                      {' • '}
                      {eventsData.pagination && eventsData.pagination.totalPages > 1 && (
                        <span>~{eventsData.pagination.totalPages} {t('account.tabs.usageDetails.pages')}</span>
                      )}
                    </>
                  )}
                </div>
                <div className="flex gap-2">
                  <button
                    onClick={() => setEventsPage(p => Math.max(1, p - 1))}
                    disabled={eventsPage === 1 || eventsLoading}
                    className="px-3 py-1 rounded text-12"
                    style={{
                      backgroundColor: eventsPage === 1 || eventsLoading ? 'var(--white-05)' : 'var(--white-10)',
                      color: eventsPage === 1 || eventsLoading ? 'var(--text-tertiary-weak)' : 'var(--text-secondary-strong)',
                      cursor: eventsPage === 1 || eventsLoading ? 'not-allowed' : 'pointer',
                      border: '1px solid var(--overlay-line-90)',
                    }}
                  >
                    {t('account.tabs.usageDetails.previous')}
                  </button>
                  <button
                    onClick={() => setEventsPage(p => p + 1)}
                    disabled={(eventsData.pagination && eventsPage >= eventsData.pagination.totalPages) || eventsLoading}
                    className="px-3 py-1 rounded text-12"
                    style={{
                      backgroundColor: (eventsData.pagination && eventsPage >= eventsData.pagination.totalPages) || eventsLoading ? 'var(--white-05)' : 'var(--white-10)',
                      color: (eventsData.pagination && eventsPage >= eventsData.pagination.totalPages) || eventsLoading ? 'var(--text-tertiary-weak)' : 'var(--text-secondary-strong)',
                      cursor: (eventsData.pagination && eventsPage >= eventsData.pagination.totalPages) || eventsLoading ? 'not-allowed' : 'pointer',
                      border: '1px solid var(--overlay-line-90)',
                    }}
                  >
                    {t('account.tabs.usageDetails.next')}
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

