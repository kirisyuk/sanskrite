/**
 * Component for displaying ScenarioData - SQL queries and BigQuery results
 */

import React from 'react';
import { useTranslation } from 'react-i18next';
import { ResponsiveContainer, LineChart, Line, XAxis, YAxis, CartesianGrid, Legend, Tooltip } from 'recharts';
import type { ScenarioData, ColumnSchema } from '../types/scenarioData';
import { api } from '../lib/api';
import { useScenarioDataStore } from '../stores/scenarioDataStore';
import { useGenaiChatStore } from '../stores/genaiChatStore';

interface ScenarioDataDisplayProps {
  scenarioData: ScenarioData;
}

export const ScenarioDataDisplay: React.FC<ScenarioDataDisplayProps> = ({ scenarioData }) => {
  const { t } = useTranslation();
  const projectId = useGenaiChatStore((state) => state.projectId || '');
  const removeScenarioData = useScenarioDataStore((state) => state.removeScenarioData);
  const [isDeleting, setIsDeleting] = React.useState(false);
  
  // Check if query is still in progress
  const isInProgress = scenarioData.executionStages?.some(s => s.status === 'running') || false;
  const hasCompleted = scenarioData.executionStages?.every(s => s.status === 'completed' || s.status === 'error') || false;
  
  const handleDelete = async () => {
    if (isDeleting || !hasCompleted || projectId === 'local-unfold') {
      return;
    }
    
    if (!confirm(t('unfold.scenarios.display.confirmDelete', { defaultValue: 'Are you sure you want to delete this result?' }))) {
      return;
    }
    
    setIsDeleting(true);
    try {
      await api.deleteScenarioData(projectId, scenarioData.scenarioId, scenarioData.id);
      removeScenarioData(scenarioData.id);
    } catch (error) {
      console.error('[ScenarioDataDisplay] Failed to delete scenario data:', error);
      alert(t('unfold.scenarios.display.deleteError', { defaultValue: 'Failed to delete result' }));
    } finally {
      setIsDeleting(false);
    }
  };

  // Convert table data to CSV
  const convertToCSV = (schema: ColumnSchema[], rows: Array<Record<string, any>>): string => {
    // CSV header
    const headers = schema.map(col => col.name);
    const csvRows = [headers.join(',')];
    
    // CSV rows
    rows.forEach(row => {
      const values = schema.map(col => {
        const value = row[col.name];
        if (value === null || value === undefined) {
          return '';
        }
        // Escape commas and quotes in values
        const stringValue = String(value);
        if (stringValue.includes(',') || stringValue.includes('"') || stringValue.includes('\n')) {
          return `"${stringValue.replace(/"/g, '""')}"`;
        }
        return stringValue;
      });
      csvRows.push(values.join(','));
    });
    
    return csvRows.join('\n');
  };

  const [savingResultIndex, setSavingResultIndex] = React.useState<number | null>(null);

  const handleSaveAsCSV = async (resultIndex: number) => {
    if (!projectId || projectId === 'local-unfold' || savingResultIndex !== null) {
      return;
    }

    const result = scenarioData.results[resultIndex];
    if (!result || !result.schema || !result.rows || result.rows.length === 0) {
      return;
    }

    setSavingResultIndex(resultIndex);
    try {
      // Convert to CSV
      const csvContent = convertToCSV(result.schema, result.rows);
      
      // Generate filename
      const timestamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, -5);
      const queryPreview = scenarioData.userQuery.substring(0, 50).replace(/[^a-zA-Z0-9]/g, '_');
      const filename = `scenario_${queryPreview}_${timestamp}.csv`;
      
      // Save as document
      await api.docsCreateFromText(projectId, {
        filename,
        contentType: 'text/csv',
        text: csvContent,
        description: `Scenario query result: ${scenarioData.userQuery.substring(0, 200)}`,
      });

      // Show success message (optional)
      console.log('[ScenarioDataDisplay] CSV saved successfully', { filename });
    } catch (error) {
      console.error('[ScenarioDataDisplay] Failed to save CSV:', error);
      alert(t('unfold.scenarios.display.saveError', { defaultValue: 'Failed to save CSV file' }));
    } finally {
      setSavingResultIndex(null);
    }
  };
  
  // Collapsible sections state
  const [isSchemaExpanded, setIsSchemaExpanded] = React.useState(false); // collapsed by default
  const [isQueriesExpanded, setIsQueriesExpanded] = React.useState(false); // collapsed by default
  const [isResultsExpanded, setIsResultsExpanded] = React.useState(true); // expanded by default
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

  // Check if column type is date
  const isDateColumn = (colType: string, sampleValue: any): boolean => {
    const dateTypes = ['DATE', 'TIMESTAMP', 'DATETIME'];
    if (dateTypes.some(type => colType.toUpperCase().includes(type))) {
      return true;
    }
    // Check if value looks like a date string (YYYY-MM-DD or YYYY-MM)
    if (typeof sampleValue === 'string') {
      const datePattern = /^\d{4}-\d{2}(-\d{2})?/;
      return datePattern.test(sampleValue);
    }
    return false;
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
      {/* Top divider */}
      <div style={{ borderTop: '1px solid var(--overlay-line-90)', marginTop: '0' }} />
      
      {/* Header */}
      <div>
        <div className="flex items-start justify-between gap-2 mb-2">
          <div className="flex-1">
            <h3 className="text-lg font-semibold" style={{ color: 'var(--text-heading)' }}>
              {t('unfold.scenarios.display.query')}:
            </h3>
            <div className="text-xs font-light mt-1" style={{ color: 'var(--text-heading)', wordBreak: 'break-word', overflowWrap: 'break-word' }}>
              {scenarioData.userQuery}
            </div>
          </div>
          {hasCompleted && projectId !== 'local-unfold' && (
            <button
              type="button"
              onClick={handleDelete}
              disabled={isDeleting}
              className="flex-shrink-0 p-1 rounded transition-colors"
              style={{
                color: 'var(--red-400)',
                backgroundColor: 'transparent',
              }}
              onMouseEnter={(e) => {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--red-05)';
              }}
              onMouseLeave={(e) => {
                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
              }}
              title={t('unfold.scenarios.display.delete', { defaultValue: 'Delete this result' })}
            >
              {isDeleting ? (
                <svg className="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                  <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83" strokeWidth={2} strokeLinecap="round" />
                </svg>
              ) : (
                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                  <path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" strokeLinecap="round" strokeLinejoin="round" />
                </svg>
              )}
            </button>
          )}
        </div>
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
                    <div className="flex items-center gap-3 mt-3 mb-3">
                      <span className="text-sm" style={{ color: 'var(--text-secondary)' }}>
                        {t('unfold.scenarios.display.showAs')}:
                      </span>
                      <div className="flex items-center gap-2">
                        <button
                          type="button"
                          onClick={() => setDisplayType(prev => ({ ...prev, [index]: 'table' }))}
                          className="px-3 py-1 text-sm rounded transition-colors"
                          style={{
                            backgroundColor: (displayType[index] || 'table') === 'table' ? 'var(--white-10)' : 'transparent',
                            color: (displayType[index] || 'table') === 'table' ? 'var(--text-heading)' : 'var(--text-secondary)',
                            border: '1px solid var(--overlay-line-90)',
                          }}
                          onMouseEnter={(e) => {
                            if ((displayType[index] || 'table') !== 'table') {
                              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-05)';
                            }
                          }}
                          onMouseLeave={(e) => {
                            if ((displayType[index] || 'table') !== 'table') {
                              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                            }
                          }}
                        >
                          {t('unfold.scenarios.display.table')}
                        </button>
                        <button
                          type="button"
                          onClick={() => setDisplayType(prev => ({ ...prev, [index]: 'graph' }))}
                          className="px-3 py-1 text-sm rounded transition-colors"
                          style={{
                            backgroundColor: (displayType[index] || 'table') === 'graph' ? 'var(--white-10)' : 'transparent',
                            color: (displayType[index] || 'table') === 'graph' ? 'var(--text-heading)' : 'var(--text-secondary)',
                            border: '1px solid var(--overlay-line-90)',
                          }}
                          onMouseEnter={(e) => {
                            if ((displayType[index] || 'table') !== 'graph') {
                              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-05)';
                            }
                          }}
                          onMouseLeave={(e) => {
                            if ((displayType[index] || 'table') !== 'graph') {
                              (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'transparent';
                            }
                          }}
                        >
                          {t('unfold.scenarios.display.graph')}
                        </button>
                      </div>
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

                            // Determine X and Y axes based on column types
                            // Priority: dates -> X axis, numeric -> Y axis
                            let xAxisColumn: ColumnSchema | null = null;
                            const yAxisColumns: ColumnSchema[] = [];
                            
                            // First pass: find date column for X axis
                            for (const col of result.schema) {
                              const sampleValue = result.rows.length > 0 ? result.rows[0][col.name] : null;
                              if (isDateColumn(col.type, sampleValue)) {
                                xAxisColumn = col;
                                break;
                              }
                            }
                            
                            // Second pass: find numeric columns for Y axis
                            for (const col of result.schema) {
                              if (col === xAxisColumn) continue; // Skip X axis column
                              if (isNumericColumn(col.type)) {
                                yAxisColumns.push(col);
                              }
                            }
                            
                            // Fallback: if no date or numeric columns found, use first column as X, rest as Y
                            if (!xAxisColumn && yAxisColumns.length === 0) {
                              if (result.schema.length > 0) {
                                xAxisColumn = result.schema[0];
                                yAxisColumns.push(...result.schema.slice(1));
                              }
                            } else if (!xAxisColumn && yAxisColumns.length > 0) {
                              // If we have numeric columns but no date, use first column as X
                              if (result.schema.length > 0 && result.schema[0] !== yAxisColumns[0]) {
                                xAxisColumn = result.schema[0];
                              }
                            } else if (xAxisColumn && yAxisColumns.length === 0) {
                              // If we have date but no numeric, use remaining columns as Y
                              for (const col of result.schema) {
                                if (col !== xAxisColumn) {
                                  yAxisColumns.push(col);
                                }
                              }
                            }

                            // Convert table data to chart format
                            const chartData = result.rows.map((row) => {
                              const obj: any = {};
                              
                              // Set X axis value
                              if (xAxisColumn) {
                                const xValue = row[xAxisColumn.name];
                                obj.x = xValue !== null && xValue !== undefined ? String(xValue) : '';
                              }
                              
                              // Set Y axis values (series)
                              yAxisColumns.forEach((col) => {
                                const value = row[col.name];
                                if (isNumericColumn(col.type)) {
                                  const numValue = Number(value);
                                  obj[col.name] = isNaN(numValue) ? null : numValue;
                                } else {
                                  // For non-numeric Y columns, try to convert to number
                                  const numValue = Number(value);
                                  obj[col.name] = isNaN(numValue) ? null : numValue;
                                }
                              });
                              
                              return obj;
                            });

                            // Get series columns (Y axis columns)
                            const seriesColumns = yAxisColumns;

                            // Detect theme for colors
                            const isDark = typeof window !== 'undefined' && document.body ?
                              window.getComputedStyle(document.body).getPropertyValue('--text-heading').includes('255') : false;
                            const COLORS = isDark
                              ? ['#60aaff', '#eab676', '#c7f464', '#ea5d93', '#b388ff', '#f06292', '#ffd740', '#80d6ff']
                              : ['#4078c0', '#e87c22', '#66bb6a', '#dc3987', '#a05abb', '#d34088', '#f5b800', '#0086b3'];

                            // Ensure we have at least X axis column (fallback)
                            if (!xAxisColumn && result.schema.length > 0) {
                              xAxisColumn = result.schema[0];
                            }
                            
                            // Ensure we have at least one series column (fallback)
                            if (seriesColumns.length === 0 && result.schema.length > 1) {
                              seriesColumns.push(...result.schema.filter(col => col !== xAxisColumn));
                            }

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
                      
                      {/* Save button */}
                      {result.rows && result.rows.length > 0 && result.schema && result.schema.length > 0 && (
                        <div className="mt-3 flex justify-end">
                          <button
                            type="button"
                            onClick={() => handleSaveAsCSV(index)}
                            disabled={savingResultIndex === index || projectId === 'local-unfold'}
                            className="px-4 py-2 text-sm rounded transition-colors flex items-center gap-2"
                            style={{
                              backgroundColor: savingResultIndex === index ? 'var(--white-10)' : 'var(--white-10)',
                              color: savingResultIndex === index ? 'var(--text-secondary)' : 'var(--text-heading)',
                              border: '1px solid var(--overlay-line-90)',
                              cursor: savingResultIndex === index || projectId === 'local-unfold' ? 'not-allowed' : 'pointer',
                            }}
                            onMouseEnter={(e) => {
                              if (savingResultIndex !== index && projectId !== 'local-unfold') {
                                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-20)';
                              }
                            }}
                            onMouseLeave={(e) => {
                              if (savingResultIndex !== index) {
                                (e.currentTarget as HTMLButtonElement).style.backgroundColor = 'var(--white-10)';
                              }
                            }}
                          >
                            {savingResultIndex === index ? (
                              <>
                                <svg className="w-4 h-4 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor">
                                  <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83" strokeWidth={2} strokeLinecap="round" />
                                </svg>
                                {t('unfold.scenarios.display.saving', { defaultValue: 'Saving...' })}
                              </>
                            ) : (
                              <>
                                <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                                  <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z" strokeLinecap="round" strokeLinejoin="round" />
                                  <polyline points="17 21 17 13 7 13 7 21" strokeLinecap="round" strokeLinejoin="round" />
                                  <polyline points="7 3 7 8 15 8" strokeLinecap="round" strokeLinejoin="round" />
                                </svg>
                                {t('unfold.scenarios.display.save', { defaultValue: 'Save' })}
                              </>
                            )}
                          </button>
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
      
      {/* Bottom divider */}
      <div style={{ borderTop: '1px solid var(--overlay-line-90)', marginTop: '0' }} />
    </div>
  );
};

