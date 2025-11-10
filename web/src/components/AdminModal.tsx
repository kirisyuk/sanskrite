/**
 * Модальное окно админ-доступа
 * Вынесено из DiscoveryPage для улучшения читаемости
 */

import React from 'react';
import { useTranslation } from 'react-i18next';
import AdminAccess from './AdminAccess';
import { ModalWithHeader } from './ModalWithHeader';

export interface AdminModalProps {
  isOpen: boolean;
  onClose: () => void;
}

export const AdminModal: React.FC<AdminModalProps> = ({
  isOpen,
  onClose,
}) => {
  const { t } = useTranslation();

  if (!isOpen) return null;

  return (
    <ModalWithHeader
      title={t('unfold.sections.adminAccess')}
      onClose={onClose}
      className="px-4"
    >
      <AdminAccess />
    </ModalWithHeader>
  );
};

