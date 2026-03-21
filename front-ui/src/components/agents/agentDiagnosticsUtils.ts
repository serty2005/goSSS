import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import 'dayjs/locale/ru';

dayjs.extend(relativeTime);
dayjs.locale('ru');

export const FRESH_HEARTBEAT_MINUTES = 5;
export const WARN_HEARTBEAT_MINUTES = 30;

export const isMeaningfulDate = (value?: string | null) => {
  if (!value) {
    return false;
  }
  const parsed = dayjs(value);
  return parsed.isValid() && parsed.year() > 1;
};

export const formatDateTime = (value?: string | null) => {
  if (!isMeaningfulDate(value)) {
    return '-';
  }
  return dayjs(value).format('DD.MM.YYYY HH:mm:ss');
};

export const formatRelativeTime = (value?: string | null, now = Date.now()) => {
  if (!isMeaningfulDate(value)) {
    return '';
  }
  return dayjs(value).from(dayjs(now));
};

export const getHeartbeatAgeMinutes = (value?: string | null, now = Date.now()) => {
  if (!isMeaningfulDate(value)) {
    return null;
  }
  return Math.max(0, dayjs(now).diff(dayjs(value), 'minute', true));
};

export const getHeartbeatFreshness = (value?: string | null, now = Date.now()) => {
  const ageMinutes = getHeartbeatAgeMinutes(value, now);
  if (ageMinutes === null) {
    return {
      color: 'default',
      label: 'Нет heartbeat',
    };
  }
  if (ageMinutes <= FRESH_HEARTBEAT_MINUTES) {
    return {
      color: 'success',
      label: 'Свежий',
    };
  }
  if (ageMinutes <= WARN_HEARTBEAT_MINUTES) {
    return {
      color: 'warning',
      label: 'Устаревает',
    };
  }
  return {
    color: 'error',
    label: 'Просрочен',
  };
};

export const getAgentStatusColor = (status?: string | null) => {
  const normalized = String(status || '').trim().toLowerCase();
  if (!normalized) {
    return 'default';
  }
  if (['active', 'online', 'running', 'ok', 'healthy', 'connected'].includes(normalized)) {
    return 'success';
  }
  if (['new', 'pending', 'processing', 'starting', 'unknown'].includes(normalized)) {
    return 'processing';
  }
  if (['warning', 'degraded', 'stale', 'pending_registration', 'pending_owner', 'pendingzabbix', 'pending_zabbix'].includes(normalized)) {
    return 'warning';
  }
  if (['offline', 'error', 'failed', 'disconnected', 'stopped', 'registration_failed'].includes(normalized)) {
    return 'error';
  }
  return 'default';
};

type RegistrationStatusMeta = {
  color: string;
  label: string;
  alertType: 'success' | 'info' | 'warning' | 'error';
  helper: string;
};

export const getRegistrationStatusMeta = (status?: string | null): RegistrationStatusMeta => {
  const normalized = String(status || '').trim().toLowerCase();
  if (!normalized) {
    return {
      color: 'default',
      label: 'Нет данных',
      alertType: 'info',
      helper: 'Сервер ещё не зафиксировал bootstrap-регистрацию для этого агента.',
    };
  }
  if (normalized === 'success') {
    return {
      color: 'success',
      label: 'Успешно',
      alertType: 'success',
      helper: 'Последняя bootstrap-регистрация завершилась успешно.',
    };
  }
  if (normalized === 'pending_approval') {
    return {
      color: 'processing',
      label: 'Ожидает подтверждения',
      alertType: 'info',
      helper: 'Агент отправил bootstrap-запрос, но токены будут выданы только после подтверждения оператором.',
    };
  }
  if (normalized === 'unauthorized') {
    return {
      color: 'error',
      label: 'Ошибка авторизации',
      alertType: 'error',
      helper: 'Сервер отклонил bootstrap-авторизацию агента.',
    };
  }
  if (normalized === 'invalid_request') {
    return {
      color: 'warning',
      label: 'Неверный payload',
      alertType: 'warning',
      helper: 'Сервер получил запрос регистрации, но отклонил его из-за формата или валидации payload.',
    };
  }
  if (normalized === 'failed') {
    return {
      color: 'error',
      label: 'Серверная ошибка',
      alertType: 'error',
      helper: 'Попытка регистрации дошла до сервера, но завершилась внутренней ошибкой.',
    };
  }
  return {
    color: 'default',
    label: normalized,
    alertType: 'info',
    helper: 'Сервер вернул нестандартный статус регистрации.',
  };
};
