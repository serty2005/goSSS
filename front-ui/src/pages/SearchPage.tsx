import React, { useMemo, useState } from 'react';
import { useSearchParams, Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Card, Typography, Spin, Empty, Space, Button, Tag, Grid } from 'antd';
import { ArrowRightOutlined } from '@ant-design/icons';
import { searchApi } from '@/api/search';
import { getEntityIcon } from '@/utils/mappers';
import { SearchFoundEntity, ServerEntity, WorkstationEntity, FiscalEntity } from '@/types/api';
import ServerCard from '@/components/entities/ServerCard';
import WorkstationCard from '@/components/entities/WorkstationCard';
import FiscalCard from '@/components/entities/FiscalCard';
import NewTicketModal from '@/components/tickets/NewTicketModal';
import { useAuthStore } from '@/store/authStore';

const { Title, Text } = Typography;
const { useBreakpoint } = Grid;

const SearchPage: React.FC = () => {
  const [searchParams] = useSearchParams();
  const term = searchParams.get('term') || '';
  const showInactive = ['1', 'true', 'yes', 'on'].includes((searchParams.get('show_inactive') || '').toLowerCase());
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [presetCompany, setPresetCompany] = useState<{ id: string; title?: string } | null>(null);
  const user = useAuthStore((state) => state.user);
  const screens = useBreakpoint();

  const { data, isLoading, isError } = useQuery({
    queryKey: ['search', term, showInactive],
    queryFn: () => searchApi.searchEntities(term, 50, showInactive),
    enabled: !!term,
    staleTime: 60_000,
  });

  const responsiveColumns = useMemo(() => {
    if (screens.xxl) return 5;
    if (screens.xl) return 4;
    if (screens.lg) return 3;
    if (screens.md) return 2;
    return 1;
  }, [screens.xxl, screens.xl, screens.lg, screens.md]);

  const configuredColumnsRaw = Number(user?.profile_config?.interface?.search?.cards_columns ?? 5);
  const configuredColumns = Number.isFinite(configuredColumnsRaw)
    ? Math.max(1, Math.min(5, Math.round(configuredColumnsRaw)))
    : 5;
  const actualColumns = Math.max(1, Math.min(configuredColumns, responsiveColumns));
  const results = data?.data?.search_results || [];

  if (!term) {
    return (
      <div style={{ textAlign: 'center', marginTop: 100 }}>
        <Title level={3}>Введите запрос для поиска</Title>
        <Text type="secondary">IP адрес, серийный номер, ИНН или название</Text>
      </div>
    );
  }

  if (isLoading) return <div style={{ textAlign: 'center', padding: 50 }}><Spin size="large" /></div>;
  if (isError) return <Text type="danger">Ошибка при выполнении поиска</Text>;

  const renderEntityCard = (item: SearchFoundEntity) => {
    switch (item.entity_type) {
      case 'Server':
        return <ServerCard data={item.data as ServerEntity} />;
      case 'Workstation':
        return <WorkstationCard data={item.data as WorkstationEntity} />;
      case 'FiscalRegister':
        return <FiscalCard data={item.data as FiscalEntity} />;
      default:
        return <div>Неизвестный тип сущности</div>;
    }
  };

  return (
    <div>
      <Title level={4}>Результаты поиска: "{term}"</Title>

      {results.length === 0 ? (
        <Empty description="Ничего не найдено" />
      ) : (
        <Space direction="vertical" style={{ width: '100%' }} size="middle">
          {results.map((group) => (
            <Card
              key={group.owner.uuid}
              className="glass-panel"
              size="small"
              title={(
                <Link to={`/companies/${group.owner.uuid}`} style={{ color: 'inherit' }}>
                  <Space size={8} align="start" style={{ lineHeight: 1.1 }}>
                    {getEntityIcon('Company')}
                    <Space direction="vertical" size={0}>
                      <Text strong style={{ cursor: 'pointer', textDecoration: 'underline' }}>
                        {group.owner.name}
                      </Text>
                      <Text type="secondary" style={{ fontSize: 12 }} ellipsis>
                        {group.owner.address || '-'}
                      </Text>
                    </Space>
                    <Tag color={group.owner.active_contract ? 'success' : 'default'} style={{ marginTop: 1 }}>
                      {group.owner.active_contract ? 'Активен' : 'Завершён'}
                    </Tag>
                  </Space>
                </Link>
              )}
              extra={(
                <Space size={6}>
                  <Button
                    type="link"
                    size="small"
                    onClick={(event) => {
                      event.preventDefault();
                      setPresetCompany({ id: group.owner.uuid, title: group.owner.name });
                      setIsCreateOpen(true);
                    }}
                  >
                    Создать заявку
                  </Button>
                  <Link to={`/companies/${group.owner.uuid}`}>
                    <Button type="link" size="small">К компании <ArrowRightOutlined /></Button>
                  </Link>
                </Space>
              )}
              bodyStyle={{ paddingTop: 12 }}
            >
              <div
                style={{
                  display: 'grid',
                  gridTemplateColumns: `repeat(${actualColumns}, minmax(0, 1fr))`,
                  gap: 12,
                }}
              >
                {group.found_entities.map((item, idx) => (
                  <div key={`${item.entity_type}-${idx}`}>
                    {renderEntityCard(item)}
                  </div>
                ))}
              </div>
            </Card>
          ))}
        </Space>
      )}

      <NewTicketModal
        open={isCreateOpen}
        onClose={() => setIsCreateOpen(false)}
        presetCompany={presetCompany}
        onCreated={() => {}}
      />
    </div>
  );
};

export default SearchPage;
