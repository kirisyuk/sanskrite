/**
 * Модальное окно настроек
 * Вынесено из DiscoveryPage для улучшения читаемости
 */

import React from 'react';
import { useTranslation } from 'react-i18next';
import Settings from './Settings';
import { ModalWithHeader } from './ModalWithHeader';

export interface SettingsModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export const SettingsModal: React.FC<SettingsModalProps> = ({
  isOpen,
  onClose,
}) => {
  const { t } = useTranslation();

  if (!isOpen) return null;

  return (
    <ModalWithHeader
      title={t('unfold.sections.settings')}
      onClose={onClose}
    >
      <Settings />
    </ModalWithHeader>
  );
};

