import React, { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import {
  App as AntdApp,
  Button,
  Card,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  DeleteOutlined,
  PlusOutlined,
  SaveOutlined,
} from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import { translationsApi } from '@/api/translations';
import {
  getAllBaseTranslationEntries,
  getAllTranslationEntries,
  getTranslationOverrides,
  splitTranslationKey,
} from '@/i18n/customTranslations';
import {
  DEFAULT_APP_LOCALE,
  buildSupportedLocaleList,
  builtInSupportedLocaleList,
} from '@/i18n/supportedLocales';
import { useLocalizationStore } from '@/store/localizationStore';
import type {
  GlobalTranslationLocaleDTO,
  GlobalTranslationsDTO,
} from '@/types/api';

const { Text, Title } = Typography;
const TRANSLATIONS_PAGE_SIZE = 40;

type TranslationRow = {
  key: string;
  sourceValue: string;
  currentValue: string;
  hasOverride: boolean;
};

const createDefaultCatalog = (): GlobalTranslationsDTO => ({
  locales: builtInSupportedLocaleList.map((locale) => ({
    code: locale.code,
    label: locale.label,
    native_label: locale.nativeLabel,
    is_builtin: locale.isDefault || locale.code === 'en' || locale.code === 'ru',
  })),
  overrides: {},
});

const cloneCatalog = (catalog?: GlobalTranslationsDTO | null): GlobalTranslationsDTO =>
  JSON.parse(JSON.stringify(catalog || createDefaultCatalog())) as GlobalTranslationsDTO;

const buildCustomLocale = (
  code: string,
  label: string,
  nativeLabel: string,
): GlobalTranslationLocaleDTO => ({
  code,
  label,
  native_label: nativeLabel,
  is_builtin: false,
});

const normalizeLocaleCode = (value: string) =>
  String(value || '')
    .trim()
    .toLowerCase()
    .replace(/_/g, '-');

const AdminTranslationsPage: React.FC = () => {
  const { t } = useTranslation(['admin', 'common']);
  const { message } = AntdApp.useApp();
  const catalog = useLocalizationStore((state) => state.catalog);
  const setCatalog = useLocalizationStore((state) => state.setCatalog);
  const [selectedLocale, setSelectedLocale] = useState('en');
  const [searchValue, setSearchValue] = useState('');
  const [draftValues, setDraftValues] = useState<Record<string, string>>({});
  const [languageModalOpen, setLanguageModalOpen] = useState(false);
  const [newLanguageCode, setNewLanguageCode] = useState('');
  const [newLanguageLabel, setNewLanguageLabel] = useState('');
  const [newLanguageNativeLabel, setNewLanguageNativeLabel] = useState('');
  const [visibleCount, setVisibleCount] = useState(TRANSLATIONS_PAGE_SIZE);
  const loadMoreRef = useRef<HTMLDivElement | null>(null);

  const persistMutation = useMutation({
    mutationFn: (payload: GlobalTranslationsDTO) => translationsApi.updateCatalog(payload),
  });

  const supportedLocales = useMemo(
    () => buildSupportedLocaleList((catalog?.locales || []).filter((item) => item.is_builtin !== true)),
    [catalog?.locales],
  );

  useEffect(() => {
    if (!supportedLocales.some((item) => item.code === selectedLocale)) {
      setSelectedLocale(supportedLocales[0]?.code || DEFAULT_APP_LOCALE);
    }
  }, [selectedLocale, supportedLocales]);

  const overrides = useMemo(() => getTranslationOverrides(catalog), [catalog]);

  const sourceEntries = useMemo(() => {
    const localeEntries = getAllBaseTranslationEntries(selectedLocale);
    return Object.keys(localeEntries).length > 0
      ? localeEntries
      : getAllBaseTranslationEntries(DEFAULT_APP_LOCALE);
  }, [selectedLocale]);

  const currentEntries = useMemo(
    () => getAllTranslationEntries(selectedLocale, catalog),
    [catalog, selectedLocale],
  );

  const rows = useMemo<TranslationRow[]>(() => {
    const normalizedSearch = searchValue.trim().toLowerCase();
    const keys = Array.from(
      new Set([...Object.keys(sourceEntries), ...Object.keys(currentEntries)]),
    ).sort((left, right) => left.localeCompare(right));

    return keys
      .map((key) => {
        const parts = splitTranslationKey(key);
        const overrideValue = parts
          ? overrides[selectedLocale]?.[parts.namespace]?.[parts.key]
          : undefined;

        return {
          key,
          sourceValue: sourceEntries[key] || '',
          currentValue: currentEntries[key] || sourceEntries[key] || '',
          hasOverride: Boolean(overrideValue),
        };
      })
      .filter((item) => {
        if (!normalizedSearch) {
          return true;
        }

        return [item.key, item.sourceValue, item.currentValue]
          .join(' ')
          .toLowerCase()
          .includes(normalizedSearch);
      });
  }, [currentEntries, overrides, searchValue, selectedLocale, sourceEntries]);

  useEffect(() => {
    setVisibleCount(TRANSLATIONS_PAGE_SIZE);
  }, [rows.length, searchValue, selectedLocale]);

  useEffect(() => {
    const node = loadMoreRef.current;
    if (!node) {
      return;
    }

    const observer = new IntersectionObserver(
      (entries) => {
        const [entry] = entries;
        if (!entry?.isIntersecting) {
          return;
        }

        setVisibleCount((current) => {
          if (current >= rows.length) {
            return current;
          }
          return Math.min(current + TRANSLATIONS_PAGE_SIZE, rows.length);
        });
      },
      {
        rootMargin: '200px 0px',
      },
    );

    observer.observe(node);
    return () => observer.disconnect();
  }, [rows.length]);

  const visibleRows = useMemo(
    () => rows.slice(0, visibleCount),
    [rows, visibleCount],
  );

  const hasMoreRows = visibleCount < rows.length;

  const keyColumnWidth = useMemo(() => {
    const longestKey = rows.reduce(
      (current, row) => (row.key.length > current.length ? row.key : current),
      '',
    );
    if (!longestKey) {
      return 260;
    }

    if (typeof document === 'undefined') {
      return Math.max(260, longestKey.length * 8 + 56);
    }

    const canvas = document.createElement('canvas');
    const context = canvas.getContext('2d');
    if (!context) {
      return Math.max(260, longestKey.length * 8 + 56);
    }

    context.font =
      '12px ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, Liberation Mono, monospace';
    return Math.ceil(context.measureText(longestKey).width) + 72;
  }, [rows]);

  const persistCatalog = async (nextCatalog: GlobalTranslationsDTO) => {
    const response = await persistMutation.mutateAsync(nextCatalog);
    const nextValue = (response as any)?.data || nextCatalog;
    setCatalog(nextValue);
    return nextValue;
  };

  const ensureLocaleInCatalog = (nextCatalog: GlobalTranslationsDTO) => {
    if (nextCatalog.locales.some((item) => item.code === selectedLocale)) {
      return;
    }

    nextCatalog.locales.push(
      buildCustomLocale(
        selectedLocale,
        selectedLocale.toUpperCase(),
        selectedLocale.toUpperCase(),
      ),
    );
  };

  const saveTranslation = async (row: TranslationRow) => {
    const nextValue = String(draftValues[row.key] ?? row.currentValue).trim();
    if (!nextValue) {
      message.warning(t('admin:translations.validation.valueRequired'));
      return;
    }

    const parts = splitTranslationKey(row.key);
    if (!parts) {
      message.error(t('admin:translations.saveError'));
      return;
    }

    try {
      const nextCatalog = cloneCatalog(catalog);
      ensureLocaleInCatalog(nextCatalog);
      nextCatalog.overrides[selectedLocale] = nextCatalog.overrides[selectedLocale] || {};
      nextCatalog.overrides[selectedLocale][parts.namespace] =
        nextCatalog.overrides[selectedLocale][parts.namespace] || {};
      nextCatalog.overrides[selectedLocale][parts.namespace][parts.key] = nextValue;

      await persistCatalog(nextCatalog);
      setDraftValues((current) => {
        const nextDrafts = { ...current };
        delete nextDrafts[row.key];
        return nextDrafts;
      });
      message.success(t('admin:translations.saved'));
    } catch {
      message.error(t('admin:translations.saveError'));
    }
  };

  const deleteTranslation = async (fullKey: string) => {
    const parts = splitTranslationKey(fullKey);
    if (!parts) {
      message.error(t('admin:translations.deleteError'));
      return;
    }

    try {
      const nextCatalog = cloneCatalog(catalog);
      delete nextCatalog.overrides[selectedLocale]?.[parts.namespace]?.[parts.key];

      if (
        nextCatalog.overrides[selectedLocale]?.[parts.namespace]
        && Object.keys(nextCatalog.overrides[selectedLocale][parts.namespace] || {}).length === 0
      ) {
        delete nextCatalog.overrides[selectedLocale][parts.namespace];
      }

      if (
        nextCatalog.overrides[selectedLocale]
        && Object.keys(nextCatalog.overrides[selectedLocale] || {}).length === 0
      ) {
        delete nextCatalog.overrides[selectedLocale];
      }

      await persistCatalog(nextCatalog);
      setDraftValues((current) => {
        const nextDrafts = { ...current };
        delete nextDrafts[fullKey];
        return nextDrafts;
      });
      message.success(t('admin:translations.deleted'));
    } catch {
      message.error(t('admin:translations.deleteError'));
    }
  };

  const addLanguage = async () => {
    const normalizedCode = normalizeLocaleCode(newLanguageCode);
    const normalizedLabel = newLanguageLabel.trim();
    const normalizedNativeLabel = newLanguageNativeLabel.trim() || normalizedLabel;

    if (!normalizedCode) {
      message.warning(t('admin:translations.validation.codeRequired'));
      return;
    }

    if (!normalizedLabel) {
      message.warning(t('admin:translations.validation.labelRequired'));
      return;
    }

    if (supportedLocales.some((item) => item.code === normalizedCode)) {
      message.warning(t('admin:translations.validation.languageExists'));
      return;
    }

    try {
      const nextCatalog = cloneCatalog(catalog);
      nextCatalog.locales.push(
        buildCustomLocale(
          normalizedCode,
          normalizedLabel,
          normalizedNativeLabel,
        ),
      );

      const nextValue = await persistCatalog(nextCatalog);
      setSelectedLocale(normalizedCode);
      setCatalog(nextValue);
      setLanguageModalOpen(false);
      setNewLanguageCode('');
      setNewLanguageLabel('');
      setNewLanguageNativeLabel('');
      message.success(t('admin:translations.languageCreated'));
    } catch {
      message.error(t('admin:translations.createLanguageError'));
    }
  };

  const columns = useMemo<ColumnsType<TranslationRow>>(
    () => [
      {
        title: t('admin:translations.table.key'),
        dataIndex: 'key',
        key: 'key',
        width: keyColumnWidth,
        render: (value: string) => <Text code>{value}</Text>,
      },
      {
        title: t('admin:translations.table.source'),
        dataIndex: 'sourceValue',
        key: 'sourceValue',
        render: (value: string) => value || '-',
      },
      {
        title: t('admin:translations.table.translation'),
        key: 'translation',
        width: 360,
        render: (_value, record) => (
          <Input
            value={draftValues[record.key] ?? record.currentValue}
            onChange={(event) => {
              const nextValue = event.target.value;
              setDraftValues((current) => ({
                ...current,
                [record.key]: nextValue,
              }));
            }}
            placeholder={t('admin:translations.valuePlaceholder')}
          />
        ),
      },
      {
        title: t('admin:translations.table.actions'),
        key: 'actions',
        width: 180,
        render: (_value, record) => (
          <Space>
            <Button
              type="primary"
              icon={<SaveOutlined />}
              loading={persistMutation.isPending}
              onClick={() => void saveTranslation(record)}
            >
              {t('common:actions.save')}
            </Button>
            {record.hasOverride ? (
              <Popconfirm
                title={t('admin:translations.delete')}
                okText={t('common:actions.delete')}
                cancelText={t('common:actions.cancel')}
                onConfirm={() => void deleteTranslation(record.key)}
              >
                <Button
                  danger
                  icon={<DeleteOutlined />}
                  loading={persistMutation.isPending}
                />
              </Popconfirm>
            ) : null}
          </Space>
        ),
      },
    ],
    [draftValues, keyColumnWidth, persistMutation.isPending, t],
  );

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Card className="glass-panel">
        <Title level={4} style={{ marginBottom: 0 }}>
          {t('admin:translations.title')}
        </Title>
        <Text type="secondary">{t('admin:translations.subtitle')}</Text>
      </Card>

      <Card className="glass-panel">
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Text type="secondary">{t('admin:translations.help')}</Text>

          <Space wrap size={12} style={{ width: '100%' }}>
            <div style={{ minWidth: 240 }}>
              <Text type="secondary">{t('admin:translations.language')}</Text>
              <Select
                value={selectedLocale}
                onChange={setSelectedLocale}
                options={supportedLocales.map((item) => ({
                  value: item.code,
                  label: `${item.nativeLabel} (${item.code})`,
                }))}
                style={{ width: '100%', marginTop: 6 }}
              />
            </div>

            <Button
              icon={<PlusOutlined />}
              onClick={() => setLanguageModalOpen(true)}
            >
              {t('admin:translations.addLanguage')}
            </Button>
          </Space>

          <div>
            <Text type="secondary">{t('admin:translations.search')}</Text>
            <Input
              value={searchValue}
              onChange={(event) => setSearchValue(event.target.value)}
              placeholder={t('admin:translations.searchPlaceholder')}
              style={{ marginTop: 6 }}
            />
          </div>

          <Table<TranslationRow>
            rowKey="key"
            columns={columns}
            dataSource={visibleRows}
            pagination={false}
            scroll={{ x: keyColumnWidth + 720 }}
            locale={{ emptyText: t('admin:translations.empty') }}
          />
          <div ref={loadMoreRef} style={{ height: 1 }} />
          {hasMoreRows ? (
            <Text type="secondary">
              {t('admin:translations.loadingMore', {
                loaded: visibleRows.length,
                total: rows.length,
              })}
            </Text>
          ) : null}
        </Space>
      </Card>

      <Modal
        open={languageModalOpen}
        title={t('admin:translations.addLanguageTitle')}
        onCancel={() => setLanguageModalOpen(false)}
        onOk={() => void addLanguage()}
        okText={t('admin:translations.addLanguage')}
        cancelText={t('common:actions.cancel')}
        confirmLoading={persistMutation.isPending}
      >
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <div>
            <Text type="secondary">{t('admin:translations.languageCode')}</Text>
            <Input
              value={newLanguageCode}
              onChange={(event) => setNewLanguageCode(event.target.value)}
              placeholder={t('admin:translations.languageCodePlaceholder')}
              style={{ marginTop: 6 }}
            />
          </div>

          <div>
            <Text type="secondary">{t('admin:translations.languageLabel')}</Text>
            <Input
              value={newLanguageLabel}
              onChange={(event) => setNewLanguageLabel(event.target.value)}
              placeholder={t('admin:translations.languageLabelPlaceholder')}
              style={{ marginTop: 6 }}
            />
          </div>

          <div>
            <Text type="secondary">{t('admin:translations.languageNativeLabel')}</Text>
            <Input
              value={newLanguageNativeLabel}
              onChange={(event) => setNewLanguageNativeLabel(event.target.value)}
              placeholder={t('admin:translations.languageNativeLabelPlaceholder')}
              style={{ marginTop: 6 }}
            />
          </div>
        </Space>
      </Modal>
    </Space>
  );
};

export default AdminTranslationsPage;
