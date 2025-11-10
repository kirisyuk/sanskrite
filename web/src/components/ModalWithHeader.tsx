/**
 * Базовый компонент модального окна с заголовком и кнопкой назад
 * Используется для Settings, Account, Admin модальных окон
 */

import React from 'react';
import { useTranslation } from 'react-i18next';

export interface ModalWithHeaderProps {
  title: string;
  onClose: () => void;
  children: React.ReactNode;
  className?: string;
}

export const ModalWithHeader: React.FC<ModalWithHeaderProps> = ({
  title,
  onClose,
  children,
  className = '',
}) => {
  const { t } = useTranslation();

  return (
    <div
      className={`w-full px-6 mt-2 mb-2 ${className}`}
      style={{ maxWidth: '100%' }}
    >
      <div className="mb-2 flex items-center">
        <button
          onClick={onClose}
          className="p-2 rounded-lg transition-colors"
          style={{
            backgroundColor: 'var(--white-10)',
            color: 'var(--text-heading)',
          }}
          onMouseEnter={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'var(--white-20)';
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.backgroundColor =
              'var(--white-10)';
          }}
          aria-label={t('unfold.sections.back')}
        >
          <svg
            className="w-4 h-4"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M15 19l-7-7 7-7"
            />
          </svg>
        </button>
        <div
          className="ml-2 text-13"
          style={{ color: 'var(--text-heading)' }}
        >
          {title}
        </div>
      </div>
      {children}
    </div>
  );
};

