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
    
    // Убираем markdown code fences если есть
    const jsonMatch = jsonText.match(/```(?:json)?\s*(\{[\s\S]*\})\s*```/);
    if (jsonMatch) {
      jsonText = jsonMatch[1];
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
    return [];
  }
}

