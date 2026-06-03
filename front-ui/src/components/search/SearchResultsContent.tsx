import React, { Suspense, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { ArrowRightOutlined } from '@ant-design/icons';
import { Button, Card, Empty, Grid, Skeleton, Space, Tag, Typography } from 'antd';
import { searchApi } from '@/api/search';
import { useAuthStore } from '@/store/authStore';
import { SearchFoundEntity, ServerEntity, WorkstationEntity, FiscalEntity } from '@/types/api';
import { getEntityIcon } from '@/utils/mappers';
import ServerCard from '@/components/entities/ServerCard';
import WorkstationCard from '@/components/entities/WorkstationCard';
import FiscalCard from '@/components/entities/FiscalCard';
import TicketSearchCard from '@/components/tickets/TicketSearchCard';

const { Text, Title } = Typography;
const { useBreakpoint } = Grid;
const LazyNewTicketModal = React.lazy(() => import('@/components/tickets/NewTicketModal'));

type Props = {
  term: string;
  variant?: 'page' | 'popover';
};

const SearchResultsContent: React.FC<Props> = ({
  term,
  variant = 'page',
}) => {
  const normalizedTerm = term.trim();
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [presetCompany, setPresetCompany] = useState<{ id: string; title?: string } | null>(null);
  const user = useAuthStore((state) => state.user);
  const screens = useBreakpoint();

  const { data, isLoading, isError } = useQuery({
    queryKey: ['search', normalizedTerm],
    queryFn: () => searchApi.searchEntities(normalizedTerm, 50),
    enabled: normalizedTerm.length > 0,
    staleTime: 60_000,
  });

  const responsiveColumns = useMemo(() => {
    if (variant === 'popover') {
      if (screens.xxl) return 4;
      if (screens.xl) return 3;
      if (screens.lg) return 2;
      return 1;
    }

    if (screens.xxl) return 5;
    if (screens.xl) return 4;
    if (screens.lg) return 3;
    if (screens.md) return 2;
    return 1;
  }, [screens.md, screens.lg, screens.xl, screens.xxl, variant]);

  const configuredColumnsRaw = Number(user?.profile_config?.interface?.search?.cards_columns ?? 5);
  const configuredColumns = Number.isFinite(configuredColumnsRaw)
    ? Math.max(1, Math.min(5, Math.round(configuredColumnsRaw)))
    : 5;
  const actualColumns = Math.max(1, Math.min(configuredColumns, responsiveColumns));
  const results = data?.data?.search_results || [];
  const ticketsWithoutCompany = data?.data?.ticket_results_without_company || [];

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

  if (!normalizedTerm) {
    return (
      <Empty
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        description={variant === 'popover'
          ? 'Начните вводить запрос, чтобы увидеть результаты'
          : 'Введите запрос для поиска'}
      />
    );
  }

  if (isLoading) {
    return (
      <Space direction="vertical" style={{ width: '100%' }} size="middle">
        {variant === 'page' && <Title level={4} style={{ margin: 0 }}>Результаты поиска: "{normalizedTerm}"</Title>}
        <Skeleton active paragraph={{ rows: 6 }} />
      </Space>
    );
  }

  if (isError) {
    return <Text type="danger">Ошибка при выполнении поиска</Text>;
  }

  return (
    <Space direction="vertical" style={{ width: '100%' }} size="middle">
      {variant === 'page' && (
        <Title level={4} style={{ margin: 0 }}>Результаты поиска: "{normalizedTerm}"</Title>
      )}
      {variant === 'popover' && (
        <Text type="secondary">
          {results.length > 0
            ? `Найдено групп: ${results.length}`
            : 'По этому запросу пока ничего не найдено'}
        </Text>
      )}

      {results.length === 0 && ticketsWithoutCompany.length === 0 ? (
        <Empty description="Ничего не найдено" />
      ) : (
        <>
          {ticketsWithoutCompany.length ? (
            <Card className="glass-panel" size="small" title="Заявки без компании">
              <div className="search-ticket-grid">
                {ticketsWithoutCompany.slice(0, variant === 'popover' ? 3 : 5).map((ticket) => (
                  <TicketSearchCard key={ticket.id} ticket={ticket} compact={variant === 'popover'} />
                ))}
              </div>
            </Card>
          ) : null}
          {results.map((group) => (
            <Card
              key={group.owner.uuid}
            className="glass-panel"
            size="small"
            title={(
              <Link to={`/companies/${group.owner.uuid}`} style={{ color: 'inherit' }}>
                <Space size={8} align="start" style={{ lineHeight: 1.1 }} wrap>
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
              <Space size={6} wrap>
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
            <Space direction="vertical" size="middle" style={{ width: '100%' }}>
              {group.matched_tickets?.length ? (
                <div className="search-ticket-section">
                  <Space style={{ justifyContent: 'space-between', width: '100%' }} align="center">
                    <Text strong>Найденные заявки</Text>
                    <Link to={`/tickets?company_ids=${encodeURIComponent(group.owner.uuid)}&archive_mode=all`}>
                      <Button type="link" size="small">Все заявки компании</Button>
                    </Link>
                  </Space>
                  <div className="search-ticket-grid">
                    {group.matched_tickets.slice(0, variant === 'popover' ? 3 : 5).map((ticket) => (
                      <TicketSearchCard key={ticket.id} ticket={ticket} compact={variant === 'popover'} />
                    ))}
                  </div>
                </div>
              ) : null}

              {group.active_tickets?.length ? (
                <div className="search-ticket-section">
                  <Space style={{ justifyContent: 'space-between', width: '100%' }} align="center">
                    <Text strong>Активные заявки</Text>
                    <Link to={`/tickets?company_ids=${encodeURIComponent(group.owner.uuid)}&archive_mode=active`}>
                      <Button type="link" size="small">Все активные</Button>
                    </Link>
                  </Space>
                  <div className="search-ticket-grid">
                    {group.active_tickets.slice(0, variant === 'popover' ? 3 : 5).map((ticket) => (
                      <TicketSearchCard key={ticket.id} ticket={ticket} compact={variant === 'popover'} />
                    ))}
                  </div>
                </div>
              ) : null}

              {group.found_entities.length ? (
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
              ) : null}
            </Space>
            </Card>
          ))}
        </>
      )}

      {isCreateOpen && (
        <Suspense fallback={null}>
          <LazyNewTicketModal
            open={isCreateOpen}
            onClose={() => setIsCreateOpen(false)}
            presetCompany={presetCompany}
            onCreated={() => {}}
          />
        </Suspense>
      )}
    </Space>
  );
};

export default SearchResultsContent;
