/**
 * Модальное окно аккаунта
 * Вынесено из DiscoveryPage для улучшения читаемости
 */

import React from 'react';
import { useTranslation } from 'react-i18next';
import Account from './Account';
import { ModalWithHeader } from './ModalWithHeader';

export interface AccountModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export const AccountModal: React.FC<AccountModalProps> = ({
  isOpen,
  onClose,
}) => {
  const { t } = useTranslation();

  if (!isOpen) return null;

  return (
    <ModalWithHeader
      title={t('unfold.sections.account')}
      onClose={onClose}
    >
      <Account />
    </ModalWithHeader>
  );
};

