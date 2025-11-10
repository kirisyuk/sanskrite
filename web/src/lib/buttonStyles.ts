/**
 * Общие стили и утилиты для кнопок правой панели
 */

export interface RightPanelButtonStyle {
  width: number;
  height: number;
  backgroundColor: string;
  color: string;
}

export const getRightPanelButtonStyle = (
  isActive: boolean,
): RightPanelButtonStyle => ({
  width: 32,
  height: 32,
  backgroundColor: isActive
    ? 'var(--accent-primary)'
    : 'var(--newchat-bg)',
  color: isActive ? 'var(--text-on-accent)' : 'var(--text-heading)',
});

export const getRightPanelButtonHoverStyle = (
  isActive: boolean,
): string => {
  return isActive ? 'var(--accent-primary)' : 'var(--newchat-bg-hover)';
};

