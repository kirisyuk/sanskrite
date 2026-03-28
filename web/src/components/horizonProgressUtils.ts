import type { HorizonRealtimeEvent } from '../stores/horizonRealtimeStore';

export const HOT_PATH_PHASE_ORDER = [
  'question_understood',
  'substrate_reused',
  'scope_completed',
  'metrics_hydrated',
  'simulation_completed',
  'answer_materialized',
] as const;

export const HOT_PATH_PHASE_TITLES: Record<string, string> = {
  question_understood: 'Понимаю запрос',
  substrate_reused: 'Проверяю, что уже есть',
  scope_completed: 'Дозаполняю граф',
  metrics_hydrated: 'Обновляю метрики',
  simulation_completed: 'Симулирую сценарии',
  answer_materialized: 'Собираю ответ',
};

export type HorizonProgressFilterOptions = {
  lensId?: string | null;
  projectId?: string | null;
  isCreatingLens?: boolean;
  preflightActive?: boolean;
  activeCreateCorrelationId?: string | null;
  activeCreateSessionId?: string | null;
};

export function filterHorizonProgressEvents(
  events: HorizonRealtimeEvent[],
  {
    lensId,
    projectId,
    isCreatingLens = false,
    preflightActive = false,
    activeCreateCorrelationId = null,
    activeCreateSessionId = null,
  }: HorizonProgressFilterOptions,
): HorizonRealtimeEvent[] {
  const base = (events || []).filter((ev) => {
    if (projectId) {
      if (ev.projectId && ev.projectId !== projectId) {
        return false;
      }
    }
    if (lensId) {
      if (ev.lensId && ev.lensId !== lensId) {
        return false;
      }
      if (!ev.lensId) {
        return false;
      }
    }
    return true;
  });

  // For an empty/new lens, only show events tied to the active creation attempt.
  if (!lensId) {
    if (!isCreatingLens && !preflightActive) return [];
    const cid = String(activeCreateCorrelationId || '').trim();
    const sid = String(activeCreateSessionId || '').trim();
    if (!cid && !sid) return [];
    return base.filter((ev) => {
      const d: any = ev?.data;
      const evCid = typeof d?.correlationId === 'string' ? String(d.correlationId).trim() : '';
      const evSid = typeof d?.sessionId === 'string' ? String(d.sessionId).trim() : '';
      if (cid && evCid && evCid === cid) return true;
      if (sid && evSid && evSid === sid) return true;
      return false;
    });
  }

  return base;
}

export function buildHotPathProgressView(filteredEvents: HorizonRealtimeEvent[]) {
  const phaseEvents = filteredEvents.filter((ev) => ev.type === 'horizon.question.phase');
  if (phaseEvents.length === 0) return null;
  const latestRunId = [...phaseEvents]
    .reverse()
    .map((ev) => String(ev.data?.questionRunId || '').trim())
    .find(Boolean);
  const scoped = latestRunId
    ? phaseEvents.filter((ev) => String(ev.data?.questionRunId || '').trim() === latestRunId)
    : phaseEvents;
  const latestByPhase = new Map<string, typeof scoped[number]>();
  for (const ev of scoped) {
    const phaseId = String(ev.data?.phase || '').trim();
    if (!phaseId) continue;
    latestByPhase.set(phaseId, ev);
  }
  const phases = HOT_PATH_PHASE_ORDER.map((phaseId) => {
    const ev = latestByPhase.get(phaseId);
    if (!ev) {
      return {
        id: phaseId,
        title: HOT_PATH_PHASE_TITLES[phaseId],
        status: 'pending',
        summary: '',
        startedAt: '',
        endedAt: '',
        durationMs: 0,
        details: {},
        artifacts: {},
        metricsRefreshed: 0,
        simulationRunId: '',
      };
    }
    const data = ev.data || {};
    return {
      id: phaseId,
      title: String(data?.title || HOT_PATH_PHASE_TITLES[phaseId] || phaseId),
      status: String(data?.status || 'pending'),
      summary: String(data?.summary || ev.humanReadable?.description || '').trim(),
      startedAt: String(data?.startedAt || '').trim(),
      endedAt: String(data?.endedAt || '').trim(),
      durationMs: Number(data?.durationMs || 0),
      details: (data?.details && typeof data.details === 'object') ? data.details as Record<string, any> : {},
      artifacts: (data?.artifacts && typeof data.artifacts === 'object') ? data.artifacts as Record<string, any> : {},
      metricsRefreshed: Number(data?.metricsRefreshed || 0),
      simulationRunId: String(data?.simulationRunId || '').trim(),
    };
  });
  return {
    questionRunId: latestRunId,
    phases,
  };
}

