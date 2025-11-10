/**
 * Утилиты для парсинга graph data из взаимодействий
 */

export interface GraphData {
  chartTitle: string;
  chartType?: string;
  table: (string | number)[][];
  sources: string[];
}

const ALLOWED_CHART_TYPES = [
  'line',
  'bar',
  'area',
  'stackedBar',
  'stackedArea',
  'composed',
] as const;

export type ChartType = typeof ALLOWED_CHART_TYPES[number];

/**
 * Парсит graph data из взаимодействия
 */
export function parseGraphData(interaction: any): GraphData | null {
  // Если уже есть graphData, используем его
  let gd = interaction.graphData as GraphData | undefined;
  if (gd) {
    return gd;
  }

  // Если subkind не 'graph', возвращаем null
  if (interaction.subkind !== 'graph') {
    return null;
  }

  try {
    let content = String(
      interaction.contentSanitized || interaction.content || '',
    ).trim();

    // Удаляем code fences
    content = content.replace(/^```[a-zA-Z]*\n([\s\S]*?)\n```$/m, '$1').trim();

    let parsed: any = null;

    // Пытаемся распарсить JSON
    if (content.startsWith('{') || content.startsWith('[')) {
      parsed = JSON.parse(content);
    } else {
      // Ищем JSON в тексте
      const match = content.match(/\{[\s\S]*\}/);
      if (match && match[0]) {
        parsed = JSON.parse(match[0]);
      }
    }

    // Валидация структуры
    if (
      parsed &&
      typeof parsed === 'object' &&
      typeof parsed.chartTitle === 'string' &&
      Array.isArray(parsed.table) &&
      Array.isArray(parsed.sources)
    ) {
      const chartType: ChartType =
        typeof parsed.chartType === 'string' &&
        ALLOWED_CHART_TYPES.includes(parsed.chartType as ChartType)
          ? (parsed.chartType as ChartType)
          : 'line';

      return {
        chartTitle: parsed.chartTitle,
        chartType,
        table: parsed.table,
        sources: parsed.sources,
      };
    }
  } catch {
    // Игнорируем ошибки парсинга
  }

  return null;
}

