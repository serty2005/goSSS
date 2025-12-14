import React from 'react';
import {
  DatabaseOutlined,
  DesktopOutlined,
  CalculatorOutlined,
  BankOutlined,
  QuestionCircleOutlined,
} from '@ant-design/icons';
import { AntBadgeStatus } from '@/types/api';

export const getEntityIcon = (type: string): React.ReactNode => {
  switch (type) {
    case 'Server': return <DatabaseOutlined />;
    case 'Workstation': return <DesktopOutlined />;
    case 'FiscalRegister': return <CalculatorOutlined />;
    case 'Company': return <BankOutlined />;
    default: return <QuestionCircleOutlined />;
  }
};

export const getEntityLabel = (type: string): string => {
  const map: Record<string, string> = {
    Server: 'Сервер',
    Workstation: 'Рабочая станция',
    FiscalRegister: 'Фискальный регистратор',
  };
  return map[type] || type;
};

export const getStatusColor = (status?: unknown): AntBadgeStatus => {
  // Приведение строки к валидному статусу Badge (check ci)
  const s = String(status);
  switch (s) {
    case 'active':
    case 'ok':
      return 'success';
    case 'offline':
    case 'locked':
      return 'error';
    case 'attention_required':
    case 'unknown':
      return 'warning';
    default:
      return 'default';
  }
};