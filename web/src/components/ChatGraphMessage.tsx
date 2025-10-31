import React from 'react';
import { ResponsiveContainer, LineChart, Line, XAxis, YAxis, CartesianGrid, Legend, Tooltip } from 'recharts';

interface ChatGraphMessageProps {
  chartTitle: string;
  table: (string | number)[][];
  sources: string[];
}

const COLORS_LIGHT = [
  '#4078c0', // blue
  '#e87c22', // orange
  '#66bb6a', // green
  '#dc3987', // magenta
  '#a05abb', // purple
  '#d34088', // pink
  '#f5b800', // yellow
  '#0086b3', // cyan
];
const COLORS_DARK = [
  '#60aaff', '#eab676', '#c7f464', '#ea5d93', '#b388ff', '#f06292', '#ffd740', '#80d6ff',
];

function isUrl(text: string) {
  try { return /^https?:\/\//.test(text); } catch { return false; }
}

const ChatGraphMessage: React.FC<ChatGraphMessageProps> = ({ chartTitle, table, sources }) => {
  const [err, setErr] = React.useState<string | null>(null);
  // detect theme
  const isDark = typeof window !== 'undefined' && document.body ?
    getComputedStyle(document.body).getPropertyValue('--text-heading').includes('255') : false;
  const COLORS = isDark ? COLORS_DARK : COLORS_LIGHT;

  // Валидация/препроцессинг
  let content = null;
  if (!Array.isArray(table) || table.length < 2 || table[0].length < 2) {
    content = <div style={{ color: 'var(--accent-danger)' }}>Ошибка: некорректная структура данных для графика.</div>;
  } else {
    try {
      // Первая строка — X (ось: время/категории), кроме 0-й колонки
      const xLabels = table[0].slice(1);
      // Остальные строки: label = первая колонка, значения — далее
      const series: { label: string; data: (number|null)[] }[] = table.slice(1).map((row) => ({
        label: String(row[0]),
        data: row.slice(1).map((v) => {
          const n = Number(v);
          return isNaN(n) ? null : n;
        })
      }));
      // Преобразуем к виду, подходящему для recharts: массив объектов с x, [y1, y2 ...]
      const chartData = xLabels.map((x, xIdx) => {
        const obj: any = { x: String(x) };
        series.forEach((s) => {
          obj[s.label] = typeof s.data[xIdx] === 'number' ? s.data[xIdx] : null;
        });
        return obj;
      });
      content = (
        <div style={{ width: '100%', minHeight: 280 }}>
          <div className="text-[14px] font-bold mb-1" style={{ color: 'var(--text-heading)' }}>{chartTitle}</div>
          <ResponsiveContainer width="100%" height={280}>
            <LineChart data={chartData} margin={{ top: 14, right: 24, left: 12, bottom: 12 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="var(--overlay-line-90)" />
              <XAxis dataKey="x" stroke="var(--text-secondary)" />
              <YAxis stroke="var(--text-secondary)" />
              <Tooltip />
              <Legend />
              {series.map((s, i) => (
                <Line
                  key={s.label}
                  type="monotone"
                  dataKey={s.label}
                  stroke={COLORS[i % COLORS.length]}
                  strokeWidth={2}
                  dot={{ r: 2.5 }}
                  activeDot={{ r: 5 }}
                  connectNulls
                />
              ))}
            </LineChart>
          </ResponsiveContainer>
          {sources && sources.length > 0 && (
            <div className="mt-2 text-xs" style={{ color: 'var(--text-secondary-strong)' }}>
              <div className="font-semibold mb-0.5">Источники данных:</div>
              <ul className="list-disc pl-4 space-y-0.5">
                {sources.map((src, i) => (
                  <li key={i}>
                    {isUrl(src)
                      ? <a href={src} target="_blank" rel="noopener noreferrer" style={{ color: 'var(--link)', textDecoration: 'underline' }}>{src}</a>
                      : <span>{src}</span>
                    }
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      );
    } catch (e: any) {
      setErr('Ошибка при построении графика: ' + (e?.message || 'unknown'));
      content = <div style={{ color: 'var(--accent-danger)' }}>Ошибка построения графика: {String(e?.message || '')}</div>;
    }
  }
  return <div className="etg-graphmessage" style={{ background: 'var(--bg-elevated)', border: '1px solid var(--overlay-line-90)', borderRadius: 12, padding: '18px 16px', marginTop: 2 }}>{content}</div>;
};

export default ChatGraphMessage;
