import { api } from './api';
import type { SxEntityMark, SxMonitorMetric } from '../types/sx';

/**
 * Сохраняет артефакты на сервер, объединяя с существующими данными
 * @param projectId ID проекта
 * @param newEntities новые entities для добавления
 * @param newMetrics новые metrics для добавления
 */
export async function persistArtifacts(
  projectId: string,
  newEntities: SxEntityMark[],
  newMetrics: SxMonitorMetric[]
): Promise<void> {
  if (!projectId || projectId === 'local-unfold') return;
  if (newEntities.length === 0 && newMetrics.length === 0) return;

  try {
    // Загружаем существующие документы
    const docs = await api.docsList(projectId);
    
    // Обрабатываем entities
    if (newEntities.length > 0) {
      const entitiesDoc = docs.find((d: any) => String(d.filename || '').toLowerCase() === 'entities.json');
      let existingEntities: any[] = [];
      
      if (entitiesDoc?.id) {
        try {
          const blob = await api.docsDownload(projectId, entitiesDoc.id);
          const text = await new Response(blob).text();
          const json = JSON.parse(text || '{}');
          existingEntities = Array.isArray(json.entities) ? json.entities : [];
        } catch (e) {
          console.warn('[Artifacts] Failed to load existing entities, starting fresh', e);
        }
      }
      
      // Объединяем: добавляем только новые (по name + kind/type)
      // Используем type для существующих (может быть kind в старых данных)
      const seen = new Set(existingEntities.map((e: any) => 
        `${String(e.type || e.kind || 'other').toLowerCase()}:${String(e.name || '').toLowerCase()}`
      ));
      
      const mergedEntities = [...existingEntities];
      for (const entity of newEntities) {
        const entityType = entity.kind || 'other';
        const key = `${String(entityType).toLowerCase()}:${String(entity.name || '').toLowerCase()}`;
        if (!seen.has(key)) {
          seen.add(key);
          // Преобразуем SxEntityMark в формат entities.json (используем kind как type)
          mergedEntities.push({
            name: entity.name,
            type: entityType,
            description: entity.description,
            ...(entity.aliases && entity.aliases.length > 0 ? { aliases: entity.aliases } : {}),
            ...(entity.tags && entity.tags.length > 0 ? { tags: entity.tags } : {}),
          });
        }
      }
      
      // Сохраняем обратно
      const entitiesJson = JSON.stringify({ entities: mergedEntities }, null, 2);
      await api.docsCreateFromText(projectId, {
        filename: 'entities.json',
        contentType: 'application/json',
        text: entitiesJson,
        description: 'Entities extracted from chat interactions',
      });
    }
    
    // Обрабатываем metrics
    if (newMetrics.length > 0) {
      const metricsDoc = docs.find((d: any) => String(d.filename || '').toLowerCase() === 'metrics.json');
      let existingMetrics: any[] = [];
      
      if (metricsDoc?.id) {
        try {
          const blob = await api.docsDownload(projectId, metricsDoc.id);
          const text = await new Response(blob).text();
          const json = JSON.parse(text || '{}');
          existingMetrics = Array.isArray(json.metrics) ? json.metrics : [];
        } catch (e) {
          console.warn('[Artifacts] Failed to load existing metrics, starting fresh', e);
        }
      }
      
      // Объединяем: добавляем только новые (по metricName/id)
      const seen = new Set(existingMetrics.map((m: any) => 
        String(m.id || m.metricName || m.label || m.key || '').toLowerCase()
      ));
      
      const mergedMetrics = [...existingMetrics];
      for (const metric of newMetrics) {
        const key = String(metric.id || metric.metricName || '').toLowerCase();
        if (key && !seen.has(key)) {
          seen.add(key);
          // Преобразуем SxMonitorMetric в формат metrics.json
          // Note: SxMonitorMetric это описание метрики для мониторинга, а не значение
          // В metrics.json хранятся фактические значения, поэтому сохраняем как есть или адаптируем
          mergedMetrics.push({
            label: metric.metricName,
            key: metric.id || metric.metricName,
            description: metric.description,
            ...(metric.unit ? { unit: metric.unit } : {}),
            ...(metric.frequency ? { frequency: metric.frequency } : {}),
            ...(metric.thresholds ? { thresholds: metric.thresholds } : {}),
            ...(metric.priority ? { priority: metric.priority } : {}),
            // Не добавляем value, так как это описание метрики, а не значение
          });
        }
      }
      
      // Сохраняем обратно
      const metricsJson = JSON.stringify({ metrics: mergedMetrics }, null, 2);
      await api.docsCreateFromText(projectId, {
        filename: 'metrics.json',
        contentType: 'application/json',
        text: metricsJson,
        description: 'Metrics extracted from chat interactions',
      });
    }
  } catch (error) {
    console.error('[Artifacts] Failed to persist artifacts', error);
    // Не бросаем ошибку, чтобы не прерывать основной поток
  }
}

