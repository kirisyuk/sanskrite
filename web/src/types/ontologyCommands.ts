// Типы для команд от LLM по работе с онтологией
// 
// LLM возвращает команды (create/update/delete) в ответ на запрос пользователя.
// Эти команды обрабатываются и преобразуются в финальные состояния в interactions.
// В interactions хранятся только финальные состояния сущностей, не операции.

import type { OntologyOperation, OntologySubkind } from '../stores/genaiChatStore';

// Команда от LLM для создания/изменения/удаления онтологической сущности
// Это команда, которую LLM возвращает пользователю, а не состояние в interactions
export interface OntologyCommand {
  operation: OntologyOperation; // 'create' | 'update' | 'delete' - команда от LLM
  type: OntologySubkind; // тип сущности
  id?: string; // ID сущности (обязателен для update/delete - LLM видит его в контексте, не нужен для create - мы генерируем)
  data?: any; // Полный объект сущности (обязателен для create/update, не нужен для delete)
}

// Ответ от LLM с массивом команд
export interface OntologyCommandsResponse {
  commands: OntologyCommand[];
}

// Валидация команды
export function validateOntologyCommand(cmd: any): cmd is OntologyCommand {
  if (!cmd || typeof cmd !== 'object') return false;
  if (!['create', 'update', 'delete'].includes(cmd.operation)) return false;
  if (!['entity', 'event', 'claim', 'relation', 'concept', 'metricState', 'trend', 'causalLink', 'scenario', 'actionableItem'].includes(cmd.type)) return false;
  
  // Для update и delete должен быть id
  if ((cmd.operation === 'update' || cmd.operation === 'delete') && !cmd.id) return false;
  
  // Для create и update должен быть data
  if ((cmd.operation === 'create' || cmd.operation === 'update') && !cmd.data) return false;
  
  return true;
}

// Парсинг команд из ответа LLM
export function parseOntologyCommands(responseText: string): OntologyCommand[] {
  try {
    // Пытаемся найти JSON в тексте (может быть обернут в markdown code fence)
    let jsonText = responseText.trim();
    
    // Убираем markdown code fences если есть (поддерживаем разные форматы)
    // Формат: ```json\n{...}\n``` или ```\n{...}\n```
    jsonText = jsonText.replace(/^```(?:json)?\s*\n?/m, ''); // Убираем начало code fence
    jsonText = jsonText.replace(/\n?```\s*$/m, ''); // Убираем конец code fence
    jsonText = jsonText.trim();
    
    // Также пытаемся найти JSON объект внутри текста (если есть лишний текст)
    const jsonObjectMatch = jsonText.match(/\{[\s\S]*\}/);
    if (jsonObjectMatch) {
      jsonText = jsonObjectMatch[0];
    }
    
    // Парсим JSON
    const parsed = JSON.parse(jsonText);
    
    // Проверяем формат ответа
    if (parsed.commands && Array.isArray(parsed.commands)) {
      // Формат: { commands: [...] }
      return parsed.commands.filter(validateOntologyCommand);
    } else if (Array.isArray(parsed)) {
      // Формат: [...] (массив команд напрямую)
      return parsed.filter(validateOntologyCommand);
    } else if (validateOntologyCommand(parsed)) {
      // Одна команда напрямую
      return [parsed];
    }
    
    return [];
  } catch (err) {
    console.warn('[parseOntologyCommands] Failed to parse commands', err);
    console.warn('[parseOntologyCommands] Response text:', responseText.substring(0, 500));
    // Попробуем более агрессивную очистку markdown
    try {
      let cleaned = responseText.trim();
      // Убираем все возможные варианты markdown code fences
      cleaned = cleaned.replace(/^```[a-z]*\s*\n?/gim, '');
      cleaned = cleaned.replace(/\n?```\s*$/gim, '');
      cleaned = cleaned.trim();
      
      // Ищем JSON объект или массив
      const jsonMatch = cleaned.match(/(\{[\s\S]*\}|\[[\s\S]*\])/);
      if (jsonMatch) {
        const parsed = JSON.parse(jsonMatch[0]);
        if (parsed.commands && Array.isArray(parsed.commands)) {
          return parsed.commands.filter(validateOntologyCommand);
        } else if (Array.isArray(parsed)) {
          return parsed.filter(validateOntologyCommand);
        } else if (validateOntologyCommand(parsed)) {
          return [parsed];
        }
      }
    } catch (retryErr) {
      console.warn('[parseOntologyCommands] Retry parsing also failed', retryErr);
    }
    return [];
  }
}

