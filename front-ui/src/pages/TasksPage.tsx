import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Modal,
  Radio,
  Spin,
  Space,
  Table,
  Tabs,
  Tag,
  Typography,
  message,
} from 'antd';
import { useInfiniteQuery, useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query';
import { ReloadOutlined } from '@ant-design/icons';
import { Link, useSearchParams } from 'react-router-dom';
import { deletionCandidatesApi } from '@/api/deletionCandidates';
import { companiesApi } from '@/api/companies';
import type {
  CompanyModel,
  EntityDeletionCandidateDTO,
  EntityDeletionCandidateDetailsDTO,
  EntityDeletionCandidateEntityDetailsDTO,
  InfrastructureItem,
} from '@/types/api';
import { useAuthStore } from '@/store/authStore';
import { isAdmin } from '@/utils/permissions';

const { Text } = Typography;

type OwnerServerSummary = {
  id: string;
  ip: string;
  status: string;
  version: string;
};

const ENTITY_FIELDS_BY_TYPE: Record<string, string[]> = {
  Server: ['device_name', 'server_name', 'ip', 'unique_id', 'crm_id'],
  Workstation: ['device_name', 'anydesk', 'teamviewer', 'litemanager', 'rustdesk', 'server_id'],
  FiscalRegister: ['model_kkt', 'fr_serial_number', 'rn_kkt', 'fn_number', 'workstation_id'],
};

function formatDateTime(value?: string | null): string {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function formatScalar(value: unknown): string {
  if (value === null || value === undefined) return '-';
  if (typeof value === 'string') return value.trim() || '-';
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return JSON.stringify(value);
}

function getEntityFieldRows(entity: EntityDeletionCandidateEntityDetailsDTO): Array<{ key: string; value: string }> {
  const preferredKeys = ENTITY_FIELDS_BY_TYPE[entity.entity_type] ?? [];
  const rows: Array<{ key: string; value: string }> = [];
  const seen = new Set<string>();
  const rawEntries = Object.entries(entity.raw || {});
  const skipKeys = new Set([
    'id',
    'owner_id',
    'ownerid',
    'last_updated_by',
    'lastupdatedby',
    'createdat',
    'created_at',
    'updatedat',
    'updated_at',
    'lastmodifieddate',
    'last_modified_date',
    'deletedat',
    'deleted_at',
    'attributes',
    'companies',
    'parent',
    'additionalowners',
    'statusdetails',
    'status_details',
  ]);

  const findRawValueByAlias = (key: string): [string, unknown] | null => {
    const normalized = key.replace(/_/g, '').toLowerCase();
    for (const [rawKey, rawValue] of rawEntries) {
      if (rawKey.replace(/_/g, '').toLowerCase() === normalized) {
        return [rawKey, rawValue];
      }
    }
    return null;
  };

  for (const key of preferredKeys) {
    const matched = findRawValueByAlias(key);
    const rawKey = matched?.[0] || key;
    const rawValue = matched?.[1];
    const value = formatScalar(rawValue);
    if (value === '-') continue;
    rows.push({ key: rawKey, value });
    seen.add(rawKey.toLowerCase());
  }

  if (rows.length === 0 && entity.raw) {
    for (const [key, rawValue] of rawEntries) {
      const normalized = key.replace(/_/g, '').toLowerCase();
      if (seen.has(key.toLowerCase()) || skipKeys.has(normalized)) {
        continue;
      }
      if (Array.isArray(rawValue) && rawValue.some((item) => item && typeof item === 'object')) {
        continue;
      }
      if (rawValue && typeof rawValue === 'object' && !Array.isArray(rawValue)) {
        continue;
      }
      const value = formatScalar(rawValue);
      if (value === '-') continue;
      rows.push({ key, value });
      if (rows.length >= 8) break;
    }
  }

  return rows;
}

function pickOwnerServerSummary(
  entity: EntityDeletionCandidateEntityDetailsDTO,
  ownerInfra?: InfrastructureItem[],
  parentInfra?: InfrastructureItem[],
): OwnerServerSummary | null {
  const ownerItems = ownerInfra || [];
  const serverItems = ownerItems.filter((item) => item.entity_type === 'Server');

  const entityID = String(entity.entity_id || '').trim();
  const raw = entity.raw || {};
  const directServerID = String(raw.server_id || '').trim();
  const workstationID = String(raw.workstation_id || '').trim();

  const getServerID = (item: InfrastructureItem) => {
    const data = item.data as Record<string, unknown>;
    return String(data.uuid || data.id || '').trim();
  };

  const toSummary = (item: InfrastructureItem): OwnerServerSummary => {
    const data = item.data as Record<string, unknown>;
    return {
      id: String(data.uuid || data.id || '').trim(),
      ip: String(data.ip || '').trim(),
      status: String(data.status || data.operational_status || '').trim(),
      version: String(data.server_version || '').trim(),
    };
  };

  const findServer = (id: string) => {
    if (!id) return undefined;
    return serverItems.find((item) => getServerID(item) === id);
  };

  if (entity.entity_type === 'Server') {
    const exact = findServer(entityID);
    if (exact) return toSummary(exact);
  }

  if (entity.entity_type === 'Workstation') {
    const exact = findServer(directServerID);
    if (exact) return toSummary(exact);
  }

  if (entity.entity_type === 'FiscalRegister' && workstationID) {
    const wsItem = ownerItems.find((item) => {
      if (item.entity_type !== 'Workstation') return false;
      const data = item.data as Record<string, unknown>;
      const wsID = String(data.uuid || data.id || '').trim();
      return wsID === workstationID;
    });
    if (wsItem) {
      const wsData = wsItem.data as Record<string, unknown>;
      const serverID = String(wsData.server_id || '').trim();
      const exact = findServer(serverID);
      if (exact) return toSummary(exact);
    }
  }

  const fallbackDirect = findServer(directServerID);
  if (fallbackDirect) return toSummary(fallbackDirect);

  if (serverItems.length > 0) {
    return toSummary(serverItems[0]);
  }

  const parentServer = (parentInfra || []).find((item) => {
    if (item.entity_type !== 'Server') return false;
    const data = item.data as Record<string, unknown>;
    const ip = String(data.ip || '').trim().toLowerCase();
    return ip.includes('iiko.it') || ip.includes('syrve.online');
  });
  if (parentServer) {
    return toSummary(parentServer);
  }

  return null;
}

function CandidateEntityCard(props: {
  entity: EntityDeletionCandidateEntityDetailsDTO;
  ownerCompany?: CompanyModel | null;
  ownerServer?: OwnerServerSummary | null;
  selectionState?: 'keep' | 'delete' | null;
}) {
  const { entity, ownerCompany, ownerServer, selectionState } = props;
  const fieldRows = getEntityFieldRows(entity);
  const isKeep = selectionState === 'keep';
  const isDelete = selectionState === 'delete';

  return (
    <Card
      size="small"
      style={{
        borderColor: isKeep ? '#1677ff' : isDelete ? '#ff4d4f' : undefined,
        background: isKeep ? 'rgba(22,119,255,0.04)' : isDelete ? 'rgba(255,77,79,0.04)' : undefined,
      }}
      title={
        <Space wrap>
          <Text strong>{entity.display_name || entity.entity_id}</Text>
          <Tag>{entity.entity_type}</Tag>
          {isKeep && <Tag color="blue">Оставить</Tag>}
          {isDelete && <Tag color="red">Удалить</Tag>}
          {entity.is_more_actual && <Tag color="blue">Более актуальная</Tag>}
          {entity.deleted && <Tag color="red">Удалена</Tag>}
        </Space>
      }
      styles={{ body: { paddingBottom: 8 } }}
    >
      <Descriptions size="small" column={1} styles={{ label: { width: 190 } }}>
        <Descriptions.Item label="ID">{entity.entity_id}</Descriptions.Item>
        <Descriptions.Item label="Владелец">
          {entity.owner_id ? (
            <Space wrap size={6}>
              <Link to={`/companies/${entity.owner_id}`}>{ownerCompany?.title || entity.owner_id}</Link>
              {ownerCompany?.title && <Text type="secondary">({entity.owner_id})</Text>}
            </Space>
          ) : (
            '-'
          )}
        </Descriptions.Item>
        <Descriptions.Item label="Сервер владельца">
          {ownerServer ? (
            <Space wrap size={6}>
              <Link to={`/servers/${ownerServer.id}`}>{ownerServer.ip || ownerServer.id}</Link>
              <Tag color={ownerServer.status === 'active' ? 'green' : ownerServer.status === 'offline' ? 'red' : 'default'}>
                {(ownerServer.status || 'unknown').toUpperCase()}
              </Tag>
              <Text type="secondary">{ownerServer.version || 'версия не указана'}</Text>
            </Space>
          ) : (
            '-'
          )}
        </Descriptions.Item>
        <Descriptions.Item label="Кем обновлено">{entity.last_updated_by || '-'}</Descriptions.Item>
        <Descriptions.Item label="Обновлено">{formatDateTime(entity.updated_at)}</Descriptions.Item>
        <Descriptions.Item label="Изменено вручную/агентом">{formatDateTime(entity.last_modified_date)}</Descriptions.Item>
      </Descriptions>

      {fieldRows.length > 0 && (
        <Descriptions
          size="small"
          column={1}
          title={<Text type="secondary">Ключевые поля</Text>}
          styles={{ label: { width: 190 } }}
        >
          {fieldRows.map((row) => (
            <Descriptions.Item key={`${entity.entity_id}-${row.key}`} label={row.key}>
              <Text code>{row.value}</Text>
            </Descriptions.Item>
          ))}
        </Descriptions>
      )}
    </Card>
  );
}

const TasksPage: React.FC = () => {
  const [selectedDeletionCandidate, setSelectedDeletionCandidate] = useState<EntityDeletionCandidateDTO | null>(null);
  const [replayKeepEntityID, setReplayKeepEntityID] = useState<string>('');
  const [searchParams, setSearchParams] = useSearchParams();

  const queryClient = useQueryClient();
  const user = useAuthStore((state) => state.user);
  const currentUserID = String(user?.id || '');
  const canConfirmDeletion = isAdmin(user?.roles);
  const selectedDeletionCandidateID = selectedDeletionCandidate?.id ?? 0;
  const preopenDeletionCandidateID = Number(searchParams.get('deletion_candidate_id') || 0);
  const deletionCandidatesLimit = 20;
  const loadMoreRef = useRef<HTMLDivElement | null>(null);

  const {
    data: deletionCandidatesData,
    isLoading: isDeletionCandidatesLoading,
    isFetching: isDeletionCandidatesFetching,
    isFetchingNextPage: isDeletionCandidatesFetchingNextPage,
    hasNextPage: hasDeletionCandidatesNextPage,
    fetchNextPage: fetchDeletionCandidatesNextPage,
  } = useInfiniteQuery({
    queryKey: ['deletion-candidates', 'tasks-page'],
    initialPageParam: 0,
    queryFn: ({ pageParam }) =>
      deletionCandidatesApi.list({ status: 'PENDING', limit: deletionCandidatesLimit, offset: Number(pageParam) || 0 }),
    getNextPageParam: (lastPage) => {
      const meta = lastPage.meta;
      if (!meta?.has_next) {
        return undefined;
      }
      return (meta.offset || 0) + (meta.limit || deletionCandidatesLimit);
    },
    enabled: canConfirmDeletion,
  });
  const allDeletionCandidates = useMemo(
    () => (deletionCandidatesData?.pages || []).flatMap((pageData) => pageData.data || []),
    [deletionCandidatesData?.pages],
  );
  const deletionCandidatesTotal = deletionCandidatesData?.pages?.[0]?.meta?.total || 0;

  useEffect(() => {
    if (!preopenDeletionCandidateID || allDeletionCandidates.length === 0 || selectedDeletionCandidate) {
      return;
    }
    const found = allDeletionCandidates.find((item) => item.id === preopenDeletionCandidateID);
    if (!found) {
      return;
    }
    setSelectedDeletionCandidate(found);
    const next = new URLSearchParams(searchParams);
    next.delete('deletion_candidate_id');
    setSearchParams(next, { replace: true });
  }, [allDeletionCandidates, preopenDeletionCandidateID, searchParams, selectedDeletionCandidate, setSearchParams]);

  useEffect(() => {
    const node = loadMoreRef.current;
    if (!node || !hasDeletionCandidatesNextPage) {
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries[0]?.isIntersecting || isDeletionCandidatesFetchingNextPage) {
          return;
        }
        void fetchDeletionCandidatesNextPage();
      },
      { rootMargin: '240px 0px' },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [
    fetchDeletionCandidatesNextPage,
    hasDeletionCandidatesNextPage,
    isDeletionCandidatesFetchingNextPage,
    allDeletionCandidates.length,
  ]);

  const {
    data: deletionCandidateDetailsData,
    isFetching: isDeletionCandidateDetailsFetching,
    isError: isDeletionCandidateDetailsError,
    error: deletionCandidateDetailsError,
  } = useQuery({
    queryKey: ['deletion-candidates', 'details', selectedDeletionCandidateID],
    queryFn: () => deletionCandidatesApi.getDetails(selectedDeletionCandidateID),
    enabled: Boolean(selectedDeletionCandidateID),
  });

  const deletionCandidateDetails = deletionCandidateDetailsData?.data;

  useEffect(() => {
    if (!deletionCandidateDetails) {
      setReplayKeepEntityID('');
      return;
    }
    const pairEntityIDs = [
      String(deletionCandidateDetails.candidate.entity_id || '').trim(),
      String(deletionCandidateDetails.candidate.duplicate_of_entity_id || '').trim(),
    ].filter(Boolean);
    const fallbackKeepID =
      deletionCandidateDetails.keep_entity?.entity_id ||
      deletionCandidateDetails.more_actual_entity_id ||
      pairEntityIDs[1] ||
      pairEntityIDs[0] ||
      deletionCandidateDetails.entities[0]?.entity_id ||
      '';
    setReplayKeepEntityID(fallbackKeepID);
  }, [deletionCandidateDetails]);

  const modalOwnerIDs = useMemo(() => {
    const ids = new Set<string>();
    const allEntities = [
      ...(deletionCandidateDetails?.entities || []),
      ...(deletionCandidateDetails?.cascade_entities || []),
    ];
    for (const entity of allEntities) {
      const ownerID = String(entity.owner_id || '').trim();
      if (ownerID) ids.add(ownerID);
    }
    return Array.from(ids);
  }, [deletionCandidateDetails?.entities, deletionCandidateDetails?.cascade_entities]);

  const ownerCompanyQueries = useQueries({
    queries: modalOwnerIDs.map((ownerID) => ({
      queryKey: ['company', ownerID, 'deletion-modal'],
      queryFn: () => companiesApi.getCompany(ownerID),
      enabled: Boolean(selectedDeletionCandidateID),
      staleTime: 60_000,
    })),
  });

  const ownerInfraQueries = useQueries({
    queries: modalOwnerIDs.map((ownerID) => ({
      queryKey: ['company', ownerID, 'infra', 'deletion-modal'],
      queryFn: () => companiesApi.getInfrastructure(ownerID),
      enabled: Boolean(selectedDeletionCandidateID),
      staleTime: 30_000,
    })),
  });

  const ownerCompanyMap = useMemo(() => {
    const map = new Map<string, CompanyModel>();
    modalOwnerIDs.forEach((ownerID, index) => {
      const company = ownerCompanyQueries[index]?.data?.data;
      if (company) map.set(ownerID, company);
    });
    return map;
  }, [modalOwnerIDs, ownerCompanyQueries]);

  const modalParentOwnerIDs = useMemo(() => {
    const ids = new Set<string>();
    ownerCompanyMap.forEach((company) => {
      const parentID = String(company.parent_id || '').trim();
      if (parentID) {
        ids.add(parentID);
      }
    });
    return Array.from(ids);
  }, [ownerCompanyMap]);

  const ownerInfraMap = useMemo(() => {
    const map = new Map<string, InfrastructureItem[]>();
    modalOwnerIDs.forEach((ownerID, index) => {
      const infra = ownerInfraQueries[index]?.data?.data;
      if (infra) map.set(ownerID, infra);
    });
    return map;
  }, [modalOwnerIDs, ownerInfraQueries]);

  const parentOwnerInfraQueries = useQueries({
    queries: modalParentOwnerIDs.map((ownerID) => ({
      queryKey: ['company', ownerID, 'infra', 'deletion-modal', 'parent-fallback'],
      queryFn: () => companiesApi.getInfrastructure(ownerID),
      enabled: Boolean(selectedDeletionCandidateID),
      staleTime: 30_000,
    })),
  });

  const parentOwnerInfraMap = useMemo(() => {
    const map = new Map<string, InfrastructureItem[]>();
    modalParentOwnerIDs.forEach((ownerID, index) => {
      const infra = parentOwnerInfraQueries[index]?.data?.data;
      if (infra) map.set(ownerID, infra);
    });
    return map;
  }, [modalParentOwnerIDs, parentOwnerInfraQueries]);

  const confirmDeletionMutation = useMutation({
    mutationFn: (candidateID: number) => deletionCandidatesApi.confirm(candidateID),
    onSuccess: (_data, candidateID) => {
      void queryClient.invalidateQueries({ queryKey: ['deletion-candidates'] });
      void queryClient.invalidateQueries({ queryKey: ['deletion-candidates', 'details', candidateID] });
      message.success('Удаление подтверждено');
    },
    onError: () => {
      message.error('Не удалось подтвердить удаление');
    },
  });

  const replayDeletionChoiceMutation = useMutation({
    mutationFn: (payload: { candidateID: number; keepEntityID: string; deleteEntityID: string }) =>
      deletionCandidatesApi.replay(payload.candidateID, {
        keep_entity_id: payload.keepEntityID,
        delete_entity_id: payload.deleteEntityID,
      }),
    onError: () => {
      message.error('Не удалось применить ручной выбор дубля');
    },
  });
  const deletionColumns = [
    { title: 'ID', dataIndex: 'id', width: 80 },
    {
      title: 'Сущность',
      key: 'entity',
      render: (_: unknown, row: EntityDeletionCandidateDTO) => `${row.entity_type} • ${row.entity_display_name || row.entity_id}`,
    },
    {
      title: 'Причина',
      key: 'reason',
      render: (_: unknown, row: EntityDeletionCandidateDTO) => row.reason || row.comment || '-',
    },
    { title: 'Источник', dataIndex: 'source', width: 160 },
    {
      title: 'Инициатор',
      key: 'requested_by_user_id',
      width: 140,
      render: (_: unknown, row: EntityDeletionCandidateDTO) => row.requested_by_user_id || 'system',
    },
    {
      title: 'Создано',
      dataIndex: 'created_at',
      width: 180,
      render: (value: string) => formatDateTime(value),
    },
    {
      title: 'Действие',
      key: 'action',
      width: 190,
      render: (_: unknown, row: EntityDeletionCandidateDTO) => {
        if (String(row.source || '').trim() !== 'manual') {
          return null;
        }
        const canConfirm = canConfirmDeletion && String(row.requested_by_user_id || '') !== currentUserID;
        return (
          <Button
            type="primary"
            size="small"
            disabled={!canConfirm}
            loading={confirmDeletionMutation.isPending && confirmDeletionMutation.variables === row.id}
            onClick={(e) => {
              e.stopPropagation();
              confirmDeletionMutation.mutate(row.id);
            }}
          >
            Подтвердить удаление
          </Button>
        );
      },
    },
  ];

  const modalEntities = useMemo(() => deletionCandidateDetails?.entities ?? [], [deletionCandidateDetails?.entities]);
  const replayPairEntityIDs = useMemo(() => {
    if (!deletionCandidateDetails) return [];
    const pair = [
      String(deletionCandidateDetails.candidate.entity_id || '').trim(),
      String(deletionCandidateDetails.candidate.duplicate_of_entity_id || '').trim(),
    ].filter(Boolean);
    return Array.from(new Set(pair));
  }, [deletionCandidateDetails]);
  const replayPairEntities = useMemo(
    () => modalEntities.filter((entity) => replayPairEntityIDs.includes(entity.entity_id)),
    [modalEntities, replayPairEntityIDs],
  );

  const isDuplicateReplayAvailable = Boolean(
    deletionCandidateDetails &&
      deletionCandidateDetails.candidate.status === 'PENDING' &&
      deletionCandidateDetails.candidate.duplicate_of_entity_id &&
      replayPairEntityIDs.length === 2,
  );

  const replayDeleteEntityID = useMemo(() => {
    if (!isDuplicateReplayAvailable || !replayKeepEntityID) return '';
    return replayPairEntityIDs.find((entityID) => entityID !== replayKeepEntityID) || '';
  }, [isDuplicateReplayAvailable, replayKeepEntityID, replayPairEntityIDs]);

  const modalConfirmAllowed = Boolean(
    selectedDeletionCandidate &&
      canConfirmDeletion &&
      String(selectedDeletionCandidate.requested_by_user_id || '') !== currentUserID &&
      selectedDeletionCandidate.status === 'PENDING',
  );

  const moreActualEntity =
    deletionCandidateDetails?.entities.find((item) => item.is_more_actual) ||
    deletionCandidateDetails?.keep_entity ||
    null;

  const handleModalConfirmDeletion = async () => {
    if (!selectedDeletionCandidate) return;
    const candidateID = selectedDeletionCandidate.id;

    try {
      if (isDuplicateReplayAvailable && replayKeepEntityID && replayDeleteEntityID && deletionCandidateDetails) {
        const currentKeepID = String(
          deletionCandidateDetails.keep_entity?.entity_id || deletionCandidateDetails.candidate.duplicate_of_entity_id || '',
        ).trim();
        const currentDeleteID = String(
          deletionCandidateDetails.delete_entity?.entity_id || deletionCandidateDetails.candidate.entity_id || '',
        ).trim();

        const changedChoice = replayKeepEntityID !== currentKeepID || replayDeleteEntityID !== currentDeleteID;
        if (changedChoice) {
          await replayDeletionChoiceMutation.mutateAsync({
            candidateID,
            keepEntityID: replayKeepEntityID,
            deleteEntityID: replayDeleteEntityID,
          });
        }
      }

      setSelectedDeletionCandidate(null);
      await confirmDeletionMutation.mutateAsync(candidateID);
    } catch {
      // Сообщения об ошибках уже показывает слой мутаций
    }
  };

  const renderDeletionCandidateModalContent = (details: EntityDeletionCandidateDetailsDTO | undefined) => {
    if (isDeletionCandidateDetailsFetching) {
      return <Text type="secondary">Загрузка деталей кандидата...</Text>;
    }

    if (isDeletionCandidateDetailsError) {
      const apiError = (deletionCandidateDetailsError as { response?: { data?: { error?: { error?: string } } } })?.response?.data?.error?.error;
      return (
        <Alert
          type="error"
          showIcon
          message="Не удалось загрузить детали кандидата"
          description={apiError || 'Ошибка запроса'}
        />
      );
    }

    if (!details) {
      return <Alert type="warning" showIcon message="Детали кандидата не получены" />;
    }

    const cascadeEntities = details.cascade_entities || [];

    const mainContent = (
      <Space direction="vertical" size={16} style={{ width: '100%' }}>
        <Descriptions bordered size="small" column={1} styles={{ label: { width: 220 } }}>
          <Descriptions.Item label="Кандидат">{details.candidate.id}</Descriptions.Item>
          <Descriptions.Item label="Причина">{details.reason_text || '-'}</Descriptions.Item>
          <Descriptions.Item label="Тип сущности">{details.candidate.entity_type}</Descriptions.Item>
          <Descriptions.Item label="Источник">{details.candidate.source}</Descriptions.Item>
          <Descriptions.Item label="Поле дубля">{details.candidate.duplicate_field || '-'}</Descriptions.Item>
          <Descriptions.Item label="Значение дубля">{details.candidate.duplicate_value || '-'}</Descriptions.Item>
          <Descriptions.Item label="Инициатор">{details.candidate.requested_by_user_id || 'system'}</Descriptions.Item>
          <Descriptions.Item label="Создано">{formatDateTime(details.candidate.created_at)}</Descriptions.Item>
        </Descriptions>

        {isDuplicateReplayAvailable && (
          <Card size="small" title="Ручной выбор актуальной сущности">
            <Space direction="vertical" size={12} style={{ width: '100%' }}>
              <Alert
                type="info"
                showIcon
                message="Выберите сущность, которая останется актуальной"
              />
              <Radio.Group
                value={replayKeepEntityID}
                onChange={(e) => setReplayKeepEntityID(String(e.target.value || ''))}
                style={{ width: '100%' }}
              >
                <Space direction="vertical" style={{ width: '100%' }}>
                  {replayPairEntities.map((entity) => (
                    <Radio key={entity.entity_id} value={entity.entity_id}>
                      <Space wrap>
                        <Text strong>{entity.display_name || entity.entity_id}</Text>
                        <Text type="secondary">({entity.entity_type})</Text>
                        <Text code>{entity.entity_id}</Text>
                        {entity.is_more_actual && <Tag color="blue">Сейчас более актуальная</Tag>}
                      </Space>
                    </Radio>
                  ))}
                </Space>
              </Radio.Group>
              <Text type="secondary">Будет удалена: {replayDeleteEntityID || '-'}</Text>
            </Space>
          </Card>
        )}

        <Space direction="vertical" size={12} style={{ width: '100%' }}>
          {details.entities.map((entity) => {
            const ownerID = String(entity.owner_id || '').trim();
            const ownerCompany = ownerCompanyMap.get(ownerID) || null;
            const parentOwnerID = String(ownerCompany?.parent_id || '').trim();
            return (
              <CandidateEntityCard
                key={entity.entity_id}
                entity={entity}
                ownerCompany={ownerCompany}
                ownerServer={pickOwnerServerSummary(
                  entity,
                  ownerInfraMap.get(ownerID),
                  parentOwnerInfraMap.get(parentOwnerID),
                )}
                selectionState={
                  isDuplicateReplayAvailable
                    ? entity.entity_id === replayKeepEntityID
                      ? 'keep'
                      : entity.entity_id === replayDeleteEntityID
                        ? 'delete'
                        : null
                    : null
                }
              />
            );
          })}
        </Space>

        {moreActualEntity?.latest_agent_data && (
          <Card size="small" title="Последние данные агента более актуальной сущности">
            <Descriptions size="small" column={1} styles={{ label: { width: 180 } }}>
              <Descriptions.Item label="Observation ID">{moreActualEntity.latest_agent_data.observation_id}</Descriptions.Item>
              <Descriptions.Item label="Observed At">{formatDateTime(moreActualEntity.latest_agent_data.observed_at)}</Descriptions.Item>
            </Descriptions>
            <pre
              style={{
                margin: 0,
                padding: 12,
                borderRadius: 8,
                background: 'rgba(0,0,0,0.04)',
                overflowX: 'auto',
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
              }}
            >
              {JSON.stringify(moreActualEntity.latest_agent_data.payload_json || {}, null, 2)}
            </pre>
          </Card>
        )}
      </Space>
    );

    if (!cascadeEntities.length) {
      return mainContent;
    }

    return (
      <Tabs
        items={[
          {
            key: 'main',
            label: 'Основная сущность',
            children: mainContent,
          },
          {
            key: 'cascade',
            label: `Каскадное удаление (${cascadeEntities.length})`,
            children: (
              <Space direction="vertical" size={12} style={{ width: '100%' }}>
                <Alert
                  type="warning"
                  showIcon
                  message="Эти сущности также будут удалены при подтверждении удаления компании"
                />
                {cascadeEntities.map((entity) => {
                  const ownerID = String(entity.owner_id || '').trim();
                  const ownerCompany = ownerCompanyMap.get(ownerID) || null;
                  const parentOwnerID = String(ownerCompany?.parent_id || '').trim();
                  return (
                    <Card key={`cascade-${entity.entity_type}-${entity.entity_id}`} size="small">
                      <CandidateEntityCard
                        entity={entity}
                        ownerCompany={ownerCompany}
                        ownerServer={pickOwnerServerSummary(
                          entity,
                          ownerInfraMap.get(ownerID),
                          parentOwnerInfraMap.get(parentOwnerID),
                        )}
                        selectionState={null}
                      />
                    </Card>
                  );
                })}
              </Space>
            ),
          },
        ]}
      />
    );
  };

  return (
    <div style={{ padding: 0 }}>
      {canConfirmDeletion && (
        <Card
          className="glass-panel"
          title="Кандидаты на удаление"
          extra={
            <Button
              icon={<ReloadOutlined />}
              onClick={() => queryClient.invalidateQueries({ queryKey: ['deletion-candidates'] })}
              loading={isDeletionCandidatesFetching}
            />
          }
        >
          <Table<EntityDeletionCandidateDTO>
            rowKey="id"
            pagination={false}
            loading={isDeletionCandidatesLoading}
            dataSource={allDeletionCandidates}
            columns={deletionColumns}
            onRow={(record) => ({
              onClick: () => setSelectedDeletionCandidate(record),
              style: { cursor: 'pointer' },
            })}
            locale={{ emptyText: 'Нет кандидатов на удаление' }}
          />
          <div ref={loadMoreRef} style={{ marginTop: 16, display: 'flex', justifyContent: 'center', minHeight: 40 }}>
            {(isDeletionCandidatesFetchingNextPage || (hasDeletionCandidatesNextPage && allDeletionCandidates.length > 0)) && <Spin size="small" />}
            {!hasDeletionCandidatesNextPage && allDeletionCandidates.length > 0 && (
              <Text type="secondary">Показано: {allDeletionCandidates.length} из {deletionCandidatesTotal}</Text>
            )}
          </div>
        </Card>
      )}

      <Modal
        open={!!selectedDeletionCandidate}
        onCancel={() => setSelectedDeletionCandidate(null)}
        width={980}
        destroyOnHidden
        title={
          selectedDeletionCandidate
            ? `Кандидат на удаление #${selectedDeletionCandidate.id}`
            : 'Кандидат на удаление'
        }
        footer={
          <Space>
            <Button onClick={() => setSelectedDeletionCandidate(null)}>Закрыть</Button>
            {selectedDeletionCandidate && (
              <Button
                type="primary"
                disabled={!modalConfirmAllowed}
                loading={
                  replayDeletionChoiceMutation.isPending ||
                  (confirmDeletionMutation.isPending && confirmDeletionMutation.variables === selectedDeletionCandidate.id)
                }
                onClick={handleModalConfirmDeletion}
              >
                Подтвердить удаление
              </Button>
            )}
          </Space>
        }
      >
        {renderDeletionCandidateModalContent(deletionCandidateDetails)}
      </Modal>
    </div>
  );
};

export default TasksPage;
