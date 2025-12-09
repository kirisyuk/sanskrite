/**
 * Component for displaying ScenarioData - SQL queries and BigQuery results
 */

import React from 'react';
import { useTranslation } from 'react-i18next';
import { ResponsiveContainer, LineChart, Line, XAxis, YAxis, CartesianGrid, Legend, Tooltip } from 'recharts';
import type { ScenarioData } from '../types/scenarioData';

interface ScenarioDataDisplayProps {
  scenarioData: ScenarioData;
}

export const ScenarioDataDisplay: React.FC<ScenarioDataDisplayProps> = ({ scenarioData }) => {
  const { t } = useTranslation();
  // Check if query is still in progress
  const isInProgress = scenarioData.executionStages?.some(s => s.status === 'running') || false;
  const hasCompleted = scenarioData.executionStages?.every(s => s.status === 'completed' || s.status === 'error') || false;
  
  // Collapsible sections state
  const [isSchemaExpanded, setIsSchemaExpanded] = React.useState(false); // collapsed by default
  const [isQueriesExpanded, setIsQueriesExpanded] = React.useState(false); // collapsed by default
  const [isResultsExpanded, setIsResultsExpanded] = React.useState(true); // expanded by default
  const [isShowAsExpanded, setIsShowAsExpanded] = React.useState<Record<number, boolean>>({}); // collapsed by default per result
  const [displayType, setDisplayType] = React.useState<Record<number, 'table' | 'graph'>>({}); // default to table per result

  // Format number with thousands separator and 2 decimal places
  const formatNumber = (value: any): string => {
    if (value === null || value === undefined) {
      return 'null';
    }
    const numValue = Number(value);
    if (isNaN(numValue)) {
      return String(value);
    }
    return numValue.toLocaleString('ru-RU', {
      minimumFractionDigits: 2,
      maximumFractionDigits: 2,
    });
  };

  // Check if column type is numeric
  const isNumericColumn = (colType: string): boolean => {
    const numericTypes = ['INTEGER', 'INT64', 'FLOAT', 'FLOAT64', 'NUMERIC', 'BIGNUMERIC', 'DOUBLE'];
    return numericTypes.some(type => colType.toUpperCase().includes(type));
  };

  // Check if value should be formatted as number (checks both column type and value)
  const shouldFormatAsNumber = (colType: string, value: any): boolean => {
    if (value === null || value === undefined) {
      return false;
    }
    // First check column type
    if (isNumericColumn(colType)) {
      return true;
    }
    // Fallback: check if value can be converted to a number
    // Exclude empty strings and non-numeric strings
    if (typeof value === 'string' && value.trim() === '') {
      return false;
    }
    const numValue = Number(value);
    return !isNaN(numValue) && isFinite(numValue);
  };
  
  return (
    <div className="space-y-4" style={{ padding: '16px', maxWidth: '100%', overflow: 'hidden' }}>
      {/* Header */}
      <div>
        <h3 className="text-lg font-semibold" style={{ color: 'var(--text-heading)', wordBreak: 'break-word', overflowWrap: 'break-word' }}>
          {t('unfold.scenarios.display.query')}: {scenarioData.userQuery}
        </h3>
        <div className="text-sm" style={{ color: 'var(--text-secondary)' }}>
          {hasCompleted ? (
            <>{t('unfold.scenarios.display.completed')}: {scenarioData.interpretation?.executedAt ? new Date(scenarioData.interpretation.executedAt).toLocaleString() : ''}</>
          ) : (
            <>{t('unfold.scenarios.display.executing')}</>
          )}
          {scenarioData.isComposite && scenarioData.interpretation?.queries && scenarioData.interpretation.queries.length > 0 && (
            <span className="ml-2 px-2 py-0.5 rounded text-xs" style={{ background: 'var(--white-10)' }}>
              {t('unfold.scenarios.display.compositeQuery', { count: scenarioData.interpretation.queries.length })}
            </span>
          )}
        </div>
      </div>

      {/* Execution Stages */}
      {scenarioData.executionStages && scenarioData.executionStages.length > 0 && (
        <div className="space-y-2">
          <h4 className="text-base font-medium" style={{ color: 'var(--text-heading)' }}>
            {t('unfold.scenarios.display.executionStages')}:
          </h4>
          <div className="space-y-2">
            {scenarioData.executionStages.map((stage, index) => (
              <div
                key={index}
                className="rounded p-2 flex items-center justify-between"
                style={{
                  background: 
                    stage.status === 'completed' ? 'var(--white-06)' :
                    stage.status === 'running' ? 'var(--blue-05)' :
                    stage.status === 'error' ? 'var(--red-05)' :
                    'var(--white-03)',
                  border: `1px solid ${
                    stage.status === 'completed' ? 'var(--green-400)' :
                    stage.status === 'running' ? 'var(--blue-400)' :
                    stage.status === 'error' ? 'var(--red-400)' :
                    'var(--overlay-line-90)'
                  }`,
                }}
              >
                <div className="flex items-center gap-2">
                  <div
                    className="w-2 h-2 rounded-full"
                    style={{
                      background:
                        stage.status === 'completed' ? 'var(--green-400)' :
                        stage.status === 'running' ? 'var(--blue-400)' :
                        stage.status === 'error' ? 'var(--red-400)' :
                        'var(--text-secondary)',
                    }}
                  />
                  <span className="text-sm" style={{ color: 'var(--text-heading)' }}>
                    {stage.name.startsWith('unfold.scenarios.display.') ? t(stage.name) : stage.name}
                  </span>
                  {stage.status === 'running' && (
                    <span className="text-xs" style={{ color: 'var(--text-secondary)' }}>
                      {t('unfold.scenarios.display.executingStage')}
                    </span>
                  )}
                </div>
                {stage.durationMs !== undefined && (
                  <span className="text-xs" style={{ color: 'var(--text-secondary)' }}>
                    {(stage.durationMs / 1000).toFixed(1)}с
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Database Schema */}
      {scenarioData.databaseSchema && (
        <div className="space-y-2">
          <button
            type="button"
            onClick={() => setIsSchemaExpanded(!isSchemaExpanded)}
            className="flex items-center gap-2 text-base font-medium w-full text-left"
            style={{ color: 'var(--text-heading)' }}
          >
            <svg
              className="w-4 h-4 flex-shrink-0"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              style={{
                transform: isSchemaExpanded ? 'rotate(90deg)' : 'rotate(0deg)',
                transition: 'transform 0.15s ease',
              }}
            >
              <path d="M9 6l6 6-6 6" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
            </svg>
            <span>{t('unfold.scenarios.display.databaseSchema')}:</span>
          </button>
          {isSchemaExpanded && (
            <div
              className="rounded p-3"
              style={{
                background: 'var(--white-06)',
                border: '1px solid var(--overlay-line-90)',
              }}
            >
              <div className="text-xs mb-2" style={{ color: 'var(--text-secondary)' }}>
                <div>{t('unfold.scenarios.display.dataset')}: <strong style={{ color: 'var(--text-heading)' }}>{scenarioData.databaseSchema.datasetID}</strong></div>
                <div>{t('unfold.scenarios.display.tenant')}: <strong style={{ color: 'var(--text-heading)' }}>{scenarioData.databaseSchema.tenantID}</strong></div>
              </div>
              <pre
                className="text-xs overflow-x-auto p-2 rounded"
                style={{
                  background: 'var(--bg-sidebar)',
                  color: 'var(--text-heading)',
                  border: '1px solid var(--overlay-line-90)',
                  fontFamily: 'monospace',
                  maxHeight: '300px',
                  overflowY: 'auto',
                  maxWidth: '100%',
                  wordBreak: 'break-word',
                  whiteSpace: 'pre-wrap',
                }}
              >
                {scenarioData.databaseSchema.schemaInfo}
              </pre>
            </div>
          )}
        </div>
      )}

      {/* SQL Queries */}
      <div className="space-y-3">
        <button
          type="button"
          onClick={() => setIsQueriesExpanded(!isQueriesExpanded)}
          className="flex items-center gap-2 text-base font-medium w-full text-left"
          style={{ color: 'var(--text-heading)' }}
        >
          <svg
            className="w-4 h-4 flex-shrink-0"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            style={{
              transform: isQueriesExpanded ? 'rotate(90deg)' : 'rotate(0deg)',
              transition: 'transform 0.15s ease',
            }}
          >
            <path d="M9 6l6 6-6 6" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
          </svg>
          <span>{t('unfold.scenarios.display.sqlQueries')}:</span>
        </button>
        {isQueriesExpanded && scenarioData.interpretation?.queries && scenarioData.interpretation.queries.map((query, index) => (
          <div
            key={index}
            className="rounded p-3"
            style={{
              background: 'var(--white-06)',
              border: '1px solid var(--overlay-line-90)',
            }}
          >
              <div className="flex items-start justify-between mb-2">
                <div className="text-sm font-medium" style={{ color: 'var(--text-heading)', wordBreak: 'break-word', overflowWrap: 'break-word', flex: 1, minWidth: 0 }}>
                  {t('unfold.scenarios.display.queryNumber', { number: index + 1 })}: {query.description}
                </div>
              {!query.execute && (
                <span className="text-xs px-2 py-0.5 rounded" style={{ background: 'var(--white-10)' }}>
                  {t('unfold.scenarios.display.skipped')}
                </span>
              )}
            </div>
            <pre
              className="text-xs overflow-x-auto p-2 rounded"
              style={{
                background: 'var(--bg-sidebar)',
                color: 'var(--text-heading)',
                border: '1px solid var(--overlay-line-90)',
                fontFamily: 'monospace',
                maxWidth: '100%',
                wordBreak: 'break-word',
                whiteSpace: 'pre-wrap',
              }}
            >
              {query.sql}
            </pre>
          </div>
        ))}
        {isQueriesExpanded && (!scenarioData.interpretation?.queries || scenarioData.interpretation.queries.length === 0) && (
          <div className="text-sm" style={{ color: 'var(--text-secondary)' }}>
            {t('unfold.scenarios.display.noResults')}
          </div>
        )}
      </div>

      {/* Results */}
      <div className="space-y-3">
        <button
          type="button"
          onClick={() => setIsResultsExpanded(!isResultsExpanded)}
          className="flex items-center gap-2 text-base font-medium w-full text-left"
          style={{ color: 'var(--text-heading)' }}
        >
          <svg
            className="w-4 h-4 flex-shrink-0"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            style={{
              transform: isResultsExpanded ? 'rotate(90deg)' : 'rotate(0deg)',
              transition: 'transform 0.15s ease',
            }}
          >
            <path d="M9 6l6 6-6 6" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
          </svg>
          <span>{t('unfold.scenarios.display.executionResults')}:</span>
        </button>
        {isResultsExpanded && (
          <>
            {!scenarioData.results || scenarioData.results.length === 0 ? (
              <div className="text-sm" style={{ color: 'var(--text-secondary)' }}>
                {t('unfold.scenarios.display.noResults')}
              </div>
            ) : (
              scenarioData.results.map((result, index) => (
            <div
              key={index}
              className="rounded p-3"
              style={{
                background: result.error ? 'var(--red-05)' : 'var(--white-06)',
                border: `1px solid ${result.error ? 'var(--red-400)' : 'var(--overlay-line-90)'}`,
              }}
            >
              <div className="flex items-start justify-between mb-2">
                <div className="text-sm font-medium" style={{ color: 'var(--text-heading)', wordBreak: 'break-word', overflowWrap: 'break-word', flex: 1, minWidth: 0 }}>
                  {t('unfold.scenarios.display.resultQueryNumber', { number: result.queryIndex + 1 })}
                </div>
                <div className="text-xs" style={{ color: 'var(--text-secondary)' }}>
                  {result.executionTimeMs && `${(result.executionTimeMs / 1000).toFixed(1)}с`}
                  {result.rowCount > 0 && ` • ${result.rowCount} ${t('unfold.scenarios.display.rows')}`}
                </div>
              </div>

              {result.error ? (
                <div className="text-sm" style={{ color: 'var(--red-400)', wordBreak: 'break-word', overflowWrap: 'break-word' }}>
                  <div className="font-medium mb-1">{t('unfold.scenarios.display.error')}:</div>
                  <div>{result.error}</div>
                  {result.errorHistory && result.errorHistory.length > 0 && (
                    <div className="mt-2 text-xs">
                      <div className="font-medium">{t('unfold.scenarios.display.errorHistory')}:</div>
                      <ul className="list-disc list-inside">
                        {result.errorHistory.map((err, i) => (
                          <li key={i}>{err}</li>
                        ))}
                      </ul>
                    </div>
                  )}
                </div>
              ) : (
                <>
                  {result.explanation && (
                    <div className="text-sm mb-2" style={{ color: 'var(--text-secondary-strong)', wordBreak: 'break-word', overflowWrap: 'break-word' }}>
                      {result.explanation}
                    </div>
                  )}

                  {/* Schema */}
                  {result.schema && result.schema.length > 0 && (
                    <div className="mb-2">
                      <div className="text-xs font-medium mb-1" style={{ color: 'var(--text-secondary)' }}>
                        {t('unfold.scenarios.display.schema')}:
                      </div>
                      <div className="flex flex-wrap gap-2">
                        {result.schema.map((col, i) => (
                          <span
                            key={i}
                            className="text-xs px-2 py-0.5 rounded"
                            style={{ background: 'var(--white-10)' }}
                          >
                            {col.name}: {col.type}
                          </span>
                        ))}
                      </div>
                    </div>
                  )}

                  {/* Show as section */}
                  {result.rows && result.rows.length > 0 && (
                    <div className="space-y-2 mt-3">
                      <button
                        type="button"
                        onClick={() => setIsShowAsExpanded(prev => ({ ...prev, [index]: !prev[index] }))}
                        className="flex items-center gap-2 text-sm font-medium w-full text-left"
                        style={{ color: 'var(--text-heading)' }}
                      >
                        <svg
                          className="w-4 h-4 flex-shrink-0"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          style={{
                            transform: isShowAsExpanded[index] ? 'rotate(90deg)' : 'rotate(0deg)',
                            transition: 'transform 0.15s ease',
                          }}
                        >
                          <path d="M9 6l6 6-6 6" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" />
                        </svg>
                        <span>{t('unfold.scenarios.display.showAs')}:</span>
                      </button>
                      {isShowAsExpanded[index] && (
                        <div className="pl-6 space-y-2">
                          <label className="flex items-center gap-2 cursor-pointer">
                            <input
                              type="radio"
                              name={`displayType-${index}`}
                              value="table"
                              checked={(displayType[index] || 'table') === 'table'}
                              onChange={(e) => setDisplayType(prev => ({ ...prev, [index]: e.target.value as 'table' | 'graph' }))}
                              className="cursor-pointer"
                            />
                            <span className="text-sm" style={{ color: 'var(--text-heading)' }}>
                              {t('unfold.scenarios.display.table')}
                            </span>
                          </label>
                          <label className="flex items-center gap-2 cursor-pointer">
                            <input
                              type="radio"
                              name={`displayType-${index}`}
                              value="graph"
                              checked={(displayType[index] || 'table') === 'graph'}
                              onChange={(e) => setDisplayType(prev => ({ ...prev, [index]: e.target.value as 'table' | 'graph' }))}
                              className="cursor-pointer"
                            />
                            <span className="text-sm" style={{ color: 'var(--text-heading)' }}>
                              {t('unfold.scenarios.display.graph')}
                            </span>
                          </label>
                        </div>
                      )}
                    </div>
                  )}

                  {/* Results Table or Graph */}
                  {result.rows && result.rows.length > 0 && (
                    <>
                      {(displayType[index] || 'table') === 'table' ? (
                        <div className="overflow-x-auto" style={{ maxWidth: '100%' }}>
                          {result.schema && result.schema.length > 0 ? (
                            <table className="text-xs border-collapse" style={{ minWidth: '100%', tableLayout: 'auto' }}>
                              <thead>
                                <tr style={{ background: 'var(--white-10)' }}>
                                  {result.schema.map((col) => (
                                    <th
                                      key={col.name}
                                      className="px-2 py-1 text-left border"
                                      style={{
                                        borderColor: 'var(--overlay-line-90)',
                                        color: 'var(--text-heading)',
                                        wordBreak: 'break-word',
                                        overflowWrap: 'break-word',
                                        maxWidth: '200px',
                                      }}
                                    >
                                      {col.name}
                                    </th>
                                  ))}
                                </tr>
                              </thead>
                              <tbody>
                                {result.rows.slice(0, 100).map((row, rowIndex) => (
                                  <tr
                                    key={rowIndex}
                                    style={{
                                      background: rowIndex % 2 === 0 ? 'transparent' : 'var(--white-05)',
                                    }}
                                  >
                                    {result.schema.map((col) => {
                                      const cellValue = row[col.name];
                                      const shouldFormat = shouldFormatAsNumber(col.type, cellValue);
                                      return (
                                        <td
                                          key={col.name}
                                          className="px-2 py-1 border"
                                          style={{
                                            borderColor: 'var(--overlay-line-90)',
                                            color: 'var(--text-heading)',
                                            wordBreak: 'break-word',
                                            overflowWrap: 'break-word',
                                            maxWidth: '200px',
                                          }}
                                        >
                                          {shouldFormat ? formatNumber(cellValue) : 
                                           (cellValue !== null && cellValue !== undefined
                                             ? String(cellValue)
                                             : 'null')}
                                        </td>
                                      );
                                    })}
                                  </tr>
                                ))}
                              </tbody>
                            </table>
                          ) : (
                            <div className="text-sm" style={{ color: 'var(--text-secondary)' }}>
                              {t('unfold.scenarios.display.noSchema')}
                            </div>
                          )}
                          {result.rows.length > 100 && (
                            <div className="text-xs mt-1" style={{ color: 'var(--text-secondary)' }}>
                              {t('unfold.scenarios.display.showingRows', { total: result.rows.length })}
                            </div>
                          )}
                        </div>
                      ) : (
                        <div style={{ width: '100%', minHeight: 280, marginTop: '12px' }}>
                          {(() => {
                            if (!result.schema || result.schema.length === 0) {
                              return (
                                <div className="text-sm" style={{ color: 'var(--text-secondary)' }}>
                                  {t('unfold.scenarios.display.noSchema')}
                                </div>
                              );
                            }

                            if (!result.rows || result.rows.length === 0) {
                              return (
                                <div className="text-sm" style={{ color: 'var(--text-secondary)' }}>
                                  {t('unfold.scenarios.display.noResults')}
                                </div>
                              );
                            }

                            // Convert table data to chart format
                            // First column is X axis, rest are series
                            const chartData = result.rows.map((row) => {
                              const obj: any = {};
                              result.schema.forEach((col, idx) => {
                                const value = row[col.name];
                                if (idx === 0) {
                                  // First column is X axis
                                  obj.x = value !== null && value !== undefined ? String(value) : '';
                                } else {
                                  // Other columns are series
                                  const numValue = Number(value);
                                  obj[col.name] = isNaN(numValue) ? null : numValue;
                                }
                              });
                              return obj;
                            });

                            // Get series columns (all except first)
                            const seriesColumns = result.schema.slice(1);

                            // Detect theme for colors
                            const isDark = typeof window !== 'undefined' && document.body ?
                              window.getComputedStyle(document.body).getPropertyValue('--text-heading').includes('255') : false;
                            const COLORS = isDark
                              ? ['#60aaff', '#eab676', '#c7f464', '#ea5d93', '#b388ff', '#f06292', '#ffd740', '#80d6ff']
                              : ['#4078c0', '#e87c22', '#66bb6a', '#dc3987', '#a05abb', '#d34088', '#f5b800', '#0086b3'];

                            return (
                              <ResponsiveContainer width="100%" height={280}>
                                <LineChart data={chartData} margin={{ top: 14, right: 24, left: 12, bottom: 12 }}>
                                  <CartesianGrid strokeDasharray="3 3" stroke="var(--overlay-line-90)" />
                                  <XAxis dataKey="x" stroke="var(--text-secondary)" />
                                  <YAxis 
                                    stroke="var(--text-secondary)" 
                                    tickFormatter={(value) => formatNumber(value)}
                                  />
                                  <Tooltip 
                                    formatter={(value: any) => formatNumber(value)}
                                  />
                                  <Legend />
                                  {seriesColumns.map((col, i) => (
                                    <Line
                                      key={col.name}
                                      type="monotone"
                                      dataKey={col.name}
                                      stroke={COLORS[i % COLORS.length]}
                                      strokeWidth={2}
                                      dot={{ r: 2.5 }}
                                      activeDot={{ r: 5 }}
                                      connectNulls
                                    />
                                  ))}
                                </LineChart>
                              </ResponsiveContainer>
                            );
                          })()}
                        </div>
                      )}
                    </>
                  )}
                </>
              )}
            </div>
              ))
            )}
          </>
        )}
      </div>
    </div>
  );
};

