import { useEffect, useRef, useState } from 'react';
import { Tooltip, theme as antTheme } from 'antd';
import { SyncOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import { useGlobalLoadingCount } from '@/hooks/useGlobalLoading';

// Задержка перед показом, чтобы быстрые ответы не вызывали мигание.
const SHOW_DELAY_MS = 150;
// Минимальное время показа, чтобы индикатор успевал считываться.
const MIN_VISIBLE_MS = 400;

// Единый индикатор ожидания приложения в header.
// Всегда занимает одно и то же место, поэтому не сдвигает соседние элементы.
const GlobalLoadingIndicator = () => {
  const { t } = useTranslation('layout');
  const { token } = antTheme.useToken();
  const count = useGlobalLoadingCount();
  const active = count > 0;
  const [visible, setVisible] = useState(false);
  const shownAtRef = useRef(0);

  useEffect(() => {
    if (active && !visible) {
      const timer = window.setTimeout(() => {
        shownAtRef.current = Date.now();
        setVisible(true);
      }, SHOW_DELAY_MS);
      return () => window.clearTimeout(timer);
    }
    if (!active && visible) {
      const remaining = Math.max(0, MIN_VISIBLE_MS - (Date.now() - shownAtRef.current));
      const timer = window.setTimeout(() => setVisible(false), remaining);
      return () => window.clearTimeout(timer);
    }
    return undefined;
  }, [active, visible]);

  const label = visible
    ? t('globalLoading.active', { count: Math.max(count, 1) })
    : t('globalLoading.idle');

  return (
    <Tooltip title={label} placement="bottom">
      <span
        className="app-global-loading"
        role="status"
        aria-live="polite"
        aria-busy={visible}
        aria-label={label}
        data-loading={visible ? 'true' : 'false'}
        style={{
          width: 32,
          height: 32,
          flex: '0 0 32px',
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          borderRadius: '50%',
          border: `1px solid ${visible ? token.colorPrimary : token.colorBorder}`,
          color: visible ? token.colorPrimary : token.colorTextQuaternary,
          transition: 'color 0.2s, border-color 0.2s',
        }}
      >
        <SyncOutlined spin={visible} />
      </span>
    </Tooltip>
  );
};

export default GlobalLoadingIndicator;
