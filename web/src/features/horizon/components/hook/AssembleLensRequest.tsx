import React, { useState, useRef, useEffect } from 'react';
import { ThemeContext } from '../../../../components/ThemeProvider';
import { api } from '../../../../lib/api';

interface EntitySuggestion {
  canonicalPath: string;
  name: string;
  type: string;
  location?: string;
  sector?: string;
}

interface AssembleLensRequestProps {
  projectId: string;
  onAssemble: (request: AssembleLensRequest) => void;
  disabled?: boolean;
}

export interface AssembleLensRequest {
  projectId: string;
  name: string;
  targetEntities: Array<{
    canonicalPath: string;
    type: string;
  }>;
  filters: {
    horizon: string;
    focus: string;
    depth: number;
  };
}

const AssembleLensRequestComponent: React.FC<AssembleLensRequestProps> = ({ 
  projectId, 
  onAssemble, 
  disabled = false 
}) => {
  const { theme } = React.useContext(ThemeContext);
  const [inputValue, setInputValue] = useState('');
  const [suggestions, setSuggestions] = useState<EntitySuggestion[]>([]);
  const [showSuggestions, setShowSuggestions] = useState(false);
  const [selectedEntities, setSelectedEntities] = useState<EntitySuggestion[]>([]);
  const [filters, setFilters] = useState({
    horizon: 'Tier-2',
    focus: 'Supply Chain',
    depth: 3,
  });
  const inputRef = useRef<HTMLInputElement>(null);
  const suggestionsRef = useRef<HTMLDivElement>(null);
  const abortControllerRef = useRef<AbortController | null>(null);

  // Focus input on mount
  useEffect(() => {
    if (inputRef.current) {
      inputRef.current.focus();
    }
  }, []);

  // Handle click outside to close suggestions
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (
        suggestionsRef.current &&
        !suggestionsRef.current.contains(event.target as Node) &&
        inputRef.current &&
        !inputRef.current.contains(event.target as Node)
      ) {
        setShowSuggestions(false);
      }
    };

    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  // Live Entity Recognition - search entities as user types
  useEffect(() => {
    const searchEntities = async () => {
      const trimmedValue = inputValue.trim();
      if (trimmedValue.length < 2) {
        setSuggestions([]);
        setShowSuggestions(false);
        return;
      }

      // Отменяем предыдущий запрос, если он еще выполняется
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }

      // Создаем новый AbortController для этого запроса
      const abortController = new AbortController();
      abortControllerRef.current = abortController;

      try {
        const response = await api.searchHorizonEntities(trimmedValue, 10, abortController.signal);
        
        // Проверяем, не был ли запрос отменен
        if (abortController.signal.aborted) {
          return;
        }

        // Преобразуем ответ API в формат EntitySuggestion
        const suggestions: EntitySuggestion[] = response.entities.map((entity) => ({
          canonicalPath: entity.canonicalPath,
          name: entity.name || entity.canonicalPath,
          type: entity.type || 'Unknown',
          location: entity.location,
          sector: entity.sector,
        }));

        setSuggestions(suggestions);
        setShowSuggestions(suggestions.length > 0);
      } catch (error: any) {
        // Игнорируем ошибки отмены запроса
        if (error.name === 'AbortError' || error.code === 'ERR_CANCELED') {
          return;
        }
        console.error('Failed to search entities:', error);
        setSuggestions([]);
        setShowSuggestions(false);
      } finally {
        // Очищаем ссылку на AbortController, если это был текущий запрос
        if (abortControllerRef.current === abortController) {
          abortControllerRef.current = null;
        }
      }
    };

    const debounceTimer = setTimeout(searchEntities, 500);
    return () => {
      clearTimeout(debounceTimer);
      // Отменяем запрос при размонтировании или изменении inputValue
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
        abortControllerRef.current = null;
      }
    };
  }, [inputValue]);

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setInputValue(e.target.value);
  };

  const handleSelectEntity = (entity: EntitySuggestion) => {
    if (!selectedEntities.find((e) => e.canonicalPath === entity.canonicalPath)) {
      setSelectedEntities([...selectedEntities, entity]);
    }
    setInputValue('');
    setShowSuggestions(false);
    if (inputRef.current) {
      inputRef.current.focus();
    }
  };

  const handleRemoveEntity = (canonicalPath: string) => {
    setSelectedEntities(selectedEntities.filter((e) => e.canonicalPath !== canonicalPath));
  };

  const handleAssemble = () => {
    if (selectedEntities.length === 0) {
      return;
    }

    const request: AssembleLensRequest = {
      projectId,
      name: selectedEntities.map((e) => e.name).join(', ') || 'New Lens',
      targetEntities: selectedEntities.map((e) => ({
        canonicalPath: e.canonicalPath,
        type: e.type,
      })),
      filters,
    };

    onAssemble(request);
  };

  const containerStyle: React.CSSProperties = {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    justifyContent: 'center',
    height: '100%',
    width: '100%',
    padding: '2rem',
  };

  const inputContainerStyle: React.CSSProperties = {
    width: '100%',
    maxWidth: '800px',
    position: 'relative',
  };

  const inputStyle: React.CSSProperties = {
    width: '100%',
    padding: '1.5rem 1.25rem',
    fontSize: '1.25rem',
    backgroundColor: 'var(--bg-elevated)',
    border: `2px solid var(--overlay-line-90)`,
    borderRadius: '12px',
    outline: 'none',
    color: 'var(--text-heading)',
    transition: 'border-color 0.2s, box-shadow 0.2s',
    boxShadow: theme === 'light' ? '0 2px 10px rgba(0,0,0,0.06)' : 'none',
  };

  const suggestionsStyle: React.CSSProperties = {
    position: 'absolute',
    top: '100%',
    left: 0,
    right: 0,
    marginTop: '8px',
    backgroundColor: 'var(--bg-elevated)',
    border: `1px solid var(--overlay-line-90)`,
    borderRadius: '8px',
    boxShadow: theme === 'light' ? '0 4px 20px rgba(0,0,0,0.12)' : '0 4px 20px rgba(0,0,0,0.4)',
    maxHeight: '300px',
    overflowY: 'auto',
    zIndex: 1000,
  };

  const suggestionItemStyle: React.CSSProperties = {
    padding: '12px 16px',
    cursor: 'pointer',
    borderBottom: `1px solid var(--overlay-line-90)`,
    backgroundColor: 'transparent',
    transition: 'background-color 0.15s ease, transform 0.1s ease',
  };

  const chipContainerStyle: React.CSSProperties = {
    display: 'flex',
    flexWrap: 'wrap',
    gap: '8px',
    marginTop: '16px',
    justifyContent: 'center',
  };

  const chipStyle: React.CSSProperties = {
    padding: '6px 12px',
    backgroundColor: 'var(--bg-secondary)',
    border: `1px solid var(--overlay-line-90)`,
    borderRadius: '20px',
    fontSize: '0.875rem',
    color: 'var(--text-secondary-strong)',
    display: 'flex',
    alignItems: 'center',
    gap: '6px',
  };

  const entityChipStyle: React.CSSProperties = {
    ...chipStyle,
    backgroundColor: 'var(--accent-bg)',
    borderColor: 'var(--accent-border)',
  };

  const buttonStyle: React.CSSProperties = {
    marginTop: '24px',
    padding: '12px 32px',
    fontSize: '1rem',
    fontWeight: 600,
    backgroundColor: 'var(--accent-bg)',
    color: 'var(--accent-text)',
    border: `1px solid var(--accent-border)`,
    borderRadius: '8px',
    cursor: disabled || selectedEntities.length === 0 ? 'not-allowed' : 'pointer',
    opacity: disabled || selectedEntities.length === 0 ? 0.6 : 1,
    transition: 'opacity 0.2s, transform 0.1s',
  };

  return (
    <div style={containerStyle}>
      <div style={inputContainerStyle}>
        <input
          ref={inputRef}
          type="text"
          value={inputValue}
          onChange={handleInputChange}
          placeholder="Define your Lens target..."
          style={inputStyle}
          disabled={disabled}
          onFocus={() => {
            if (suggestions.length > 0) {
              setShowSuggestions(true);
            }
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && selectedEntities.length > 0 && !disabled) {
              handleAssemble();
            }
            if (e.key === 'Escape') {
              setShowSuggestions(false);
            }
          }}
        />

        {/* Entity Suggestions Dropdown */}
        {showSuggestions && suggestions.length > 0 && (
          <div ref={suggestionsRef} style={suggestionsStyle}>
            {suggestions.map((suggestion, index) => (
              <div
                key={suggestion.canonicalPath}
                style={{
                  ...suggestionItemStyle,
                  borderBottom: index === suggestions.length - 1 ? 'none' : suggestionItemStyle.borderBottom,
                }}
                onClick={() => handleSelectEntity(suggestion)}
                onMouseEnter={(e) => {
                  e.currentTarget.style.backgroundColor = 'var(--bg-hover)';
                  e.currentTarget.style.transform = 'translateX(4px)';
                }}
                onMouseLeave={(e) => {
                  e.currentTarget.style.backgroundColor = 'transparent';
                  e.currentTarget.style.transform = 'translateX(0)';
                }}
              >
                <div style={{ fontWeight: 600, color: 'var(--text-heading)' }}>
                  {suggestion.name}
                </div>
                <div style={{ fontSize: '0.875rem', color: 'var(--text-secondary)', marginTop: '4px' }}>
                  {suggestion.type}
                  {suggestion.location && suggestion.sector && (
                    <span> [{suggestion.location}/{suggestion.sector}]</span>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}

        {/* Selected Entities Chips */}
        {selectedEntities.length > 0 && (
          <div style={chipContainerStyle}>
            {selectedEntities.map((entity) => (
              <div key={entity.canonicalPath} style={entityChipStyle}>
                <span>{entity.name}</span>
                <button
                  onClick={() => handleRemoveEntity(entity.canonicalPath)}
                  style={{
                    background: 'none',
                    border: 'none',
                    color: 'var(--text-secondary-strong)',
                    cursor: 'pointer',
                    padding: '0',
                    marginLeft: '4px',
                    fontSize: '1rem',
                    lineHeight: '1',
                  }}
                  aria-label={`Remove ${entity.name}`}
                >
                  ×
                </button>
              </div>
            ))}
          </div>
        )}

        {/* Filter Chips */}
        <div style={chipContainerStyle}>
          <div style={chipStyle}>
            <span>Horizon:</span>
            <span style={{ fontWeight: 600 }}>{filters.horizon}</span>
          </div>
          <div style={chipStyle}>
            <span>Focus:</span>
            <span style={{ fontWeight: 600 }}>{filters.focus}</span>
          </div>
          <div style={chipStyle}>
            <span>Depth:</span>
            <span style={{ fontWeight: 600 }}>{filters.depth} levels</span>
          </div>
        </div>

        {/* Assemble Button */}
        <div style={{ display: 'flex', justifyContent: 'center', marginTop: '24px' }}>
          <button
            type="button"
            onClick={handleAssemble}
            disabled={disabled || selectedEntities.length === 0}
            style={buttonStyle}
            onMouseEnter={(e) => {
              if (!disabled && selectedEntities.length > 0) {
                e.currentTarget.style.transform = 'scale(1.02)';
              }
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.transform = 'scale(1)';
            }}
          >
          </button>
        </div>
      </div>
    </div>
  );
};

export default AssembleLensRequestComponent;

