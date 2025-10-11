/**
 * Примеры использования автогенерированных типов из backend
 * 
 * Этот файл демонстрирует, как использовать типы User, Project, DataSource
 * для обеспечения type-safety в приложении.
 */

import type { User, Project, DataSource } from './index';

// ============================================================================
// Пример 1: Создание объектов с полной типизацией
// ============================================================================

export const createExampleUser = (): User => {
  return {
    id: 'user-123',
    tenantId: 'tenant-456',
    email: 'user@example.com',
    displayName: 'John Doe',
    role: 'admin',
    status: 'active',
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
    schemaVersion: '1.0',
    // Опциональные поля можно опустить
    projectRoles: {
      'project-1': 'owner',
      'project-2': 'member',
    },
  };
};

export const createExampleProject = (): Project => {
  return {
    id: 'project-789',
    pk: 'Projects/project-789',
    tenantId: 'tenant-456',
    displayName: 'Research Project',
    description: 'AI-powered research and discovery',
    status: 'active',
    ownerUserId: 'user-123',
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
    view: 'discovery',
    aiguided: true,
  };
};

// ============================================================================
// Пример 2: Функции с типизированными параметрами и возвращаемыми значениями
// ============================================================================

/**
 * Получить полное имя пользователя или email если имя не указано
 */
export function getUserDisplayName(user: User): string {
  return user.displayName || user.email;
}

/**
 * Проверить, является ли пользователь администратором
 */
export function isUserAdmin(user: User): boolean {
  return user.role === 'admin';
}

/**
 * Получить роль пользователя в проекте
 */
export function getUserProjectRole(user: User, projectId: string): string | undefined {
  return user.projectRoles?.[projectId];
}

/**
 * Проверить, активен ли проект
 */
export function isProjectActive(project: Project): boolean {
  return project.status === 'active';
}

/**
 * Получить количество дней с момента создания проекта
 */
export function getProjectAge(project: Project): number {
  const created = new Date(project.createdAt);
  const now = new Date();
  const diffTime = Math.abs(now.getTime() - created.getTime());
  const diffDays = Math.ceil(diffTime / (1000 * 60 * 60 * 24));
  return diffDays;
}

// ============================================================================
// Пример 3: Работа с источниками данных (DataSource)
// ============================================================================

/**
 * Проверить, завершен ли анализ источника
 */
export function isSourceProfiled(source: DataSource): boolean {
  return source.status === 'completed' && source.profile !== undefined;
}

/**
 * Получить оценку доверия источника
 */
export function getSourceTrustScore(source: DataSource): number | null {
  return source.profile?.trustScore ?? null;
}

/**
 * Получить список red flags источника
 */
export function getSourceRedFlags(source: DataSource): Array<{ code: string; score: number }> {
  return source.profile?.redFlags ?? [];
}

/**
 * Проверить, является ли источник Telegram каналом
 */
export function isTelegramChannel(source: DataSource): boolean {
  return source.kind === 'telegram_channel';
}

/**
 * Получить URL веб-источника
 */
export function getWebSourceUrl(source: DataSource): string | null {
  if (source.kind !== 'web') return null;
  return source.sourceDetails.uri ?? source.sourceDetails.sourceUri ?? null;
}

// ============================================================================
// Пример 4: Partial типы для обновлений
// ============================================================================

/**
 * Обновить профиль пользователя (только указанные поля)
 */
export function updateUserProfile(
  user: User,
  updates: Partial<Pick<User, 'displayName' | 'jobTitle' | 'department'>>
): User {
  return {
    ...user,
    ...updates,
    updatedAt: new Date().toISOString(),
  };
}

/**
 * Обновить описание проекта
 */
export function updateProjectDescription(
  project: Project,
  description: string
): Project {
  return {
    ...project,
    description,
    updatedAt: new Date().toISOString(),
  };
}

// ============================================================================
// Пример 5: Type guards для проверки типов
// ============================================================================

/**
 * Проверить, имеет ли пользователь компанию
 */
export function hasCompany(user: User): user is User & { companyRef: string } {
  return user.companyRef !== undefined && user.companyRef !== null;
}

/**
 * Проверить, есть ли у источника профиль с trust score
 */
export function hasTrustScore(
  source: DataSource
): source is DataSource & { profile: { trustScore: number } } {
  return (
    source.profile !== undefined &&
    source.profile.trustScore !== undefined &&
    typeof source.profile.trustScore === 'number'
  );
}

// ============================================================================
// Пример 6: Утилиты для фильтрации и сортировки
// ============================================================================

/**
 * Отфильтровать активные проекты
 */
export function filterActiveProjects(projects: Project[]): Project[] {
  return projects.filter(isProjectActive);
}

/**
 * Отсортировать проекты по дате последнего взаимодействия
 */
export function sortProjectsByLastInteraction(projects: Project[]): Project[] {
  return [...projects].sort((a, b) => {
    const aTime = a.lastInteractionAt ? new Date(a.lastInteractionAt).getTime() : 0;
    const bTime = b.lastInteractionAt ? new Date(b.lastInteractionAt).getTime() : 0;
    return bTime - aTime; // Новые первыми
  });
}

/**
 * Отфильтровать источники по типу
 */
export function filterSourcesByKind(
  sources: DataSource[],
  kind: string
): DataSource[] {
  return sources.filter((source) => source.kind === kind);
}

/**
 * Отсортировать источники по trust score (от высокого к низкому)
 */
export function sortSourcesByTrust(sources: DataSource[]): DataSource[] {
  return [...sources].sort((a, b) => {
    const aScore = a.profile?.trustScore ?? 0;
    const bScore = b.profile?.trustScore ?? 0;
    return bScore - aScore;
  });
}

// ============================================================================
// Пример 7: Константы и енумы (можно расширить на основе API)
// ============================================================================

export const USER_ROLES = {
  ADMIN: 'admin',
  USER: 'user',
  VIEWER: 'viewer',
} as const;

export const PROJECT_STATUS = {
  ACTIVE: 'active',
  ARCHIVED: 'archived',
  DRAFT: 'draft',
} as const;

export const SOURCE_STATUS = {
  PENDING: 'pending',
  PROCESSING: 'processing',
  COMPLETED: 'completed',
  ERROR: 'error',
} as const;

export const SOURCE_KINDS = {
  WEB: 'web',
  RSS: 'rss',
  TELEGRAM_CHANNEL: 'telegram_channel',
  TWITTER_SEARCH: 'twitter_search',
  DOCUMENT: 'document',
  TOPIC_BASED_INTEREST: 'topic_based_interest',
} as const;

