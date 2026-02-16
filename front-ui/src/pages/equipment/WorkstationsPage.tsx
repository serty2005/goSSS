import React, { useEffect, useMemo, useRef } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useInfiniteQuery } from '@tanstack/react-query';
import { Card, Empty, Space, Spin, Table, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { equipmentApi } from '@/api/equipment';

const { Title, Text } = Typography;

type Row = {
  id: string;
  name: string;
  anydesk: string;
  teamviewer: string;
  ownerId: string;
};

const WorkstationsPage: React.FC = () => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const term = (searchParams.get('q') || '').trim();
  const limit = 20;
  const loadMoreRef = useRef<HTMLDivElement | null>(null);

  const { data, isLoading, isFetchingNextPage, hasNextPage, fetchNextPage } = useInfiniteQuery({
    queryKey: ['equipment', 'workstations', term],
    initialPageParam: 0,
    queryFn: ({ pageParam }) => equipmentApi.listWorkstations(term, limit, Number(pageParam) || 0),
    getNextPageParam: (lastPage) => {
      const meta = lastPage.meta;
      if (!meta?.has_next) {
        return undefined;
      }
      return (meta.offset || 0) + (meta.limit || limit);
    },
    staleTime: 30_000,
  });

  const rows: Row[] = useMemo(
    () =>
      (data?.pages || [])
        .flatMap((pageData) => pageData.data || [])
        .map((item) => {
          const row = item as Record<string, unknown>;
          const id = String(row.id || '');
          return {
            id,
            name: String(row.device_name || id || 'Рабочая станция'),
            anydesk: String(row.anydesk || '-'),
            teamviewer: String(row.teamviewer || '-'),
            ownerId: String(row.owner_id || ''),
          };
        }),
    [data?.pages],
  );
  const total = data?.pages?.[0]?.meta?.total || 0;

  useEffect(() => {
    const node = loadMoreRef.current;
    if (!node || !hasNextPage) {
      return;
    }

    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries[0]?.isIntersecting || isFetchingNextPage) {
          return;
        }
        void fetchNextPage();
      },
      { rootMargin: '240px 0px' },
    );

    observer.observe(node);
    return () => observer.disconnect();
  }, [fetchNextPage, hasNextPage, isFetchingNextPage, rows.length]);

  const columns: ColumnsType<Row> = [
    { title: 'Название', dataIndex: 'name', key: 'name' },
    { title: 'AnyDesk', dataIndex: 'anydesk', key: 'anydesk', width: 180 },
    { title: 'TeamViewer', dataIndex: 'teamviewer', key: 'teamviewer', width: 180 },
    {
      title: 'Владелец',
      dataIndex: 'ownerId',
      key: 'ownerId',
      width: 260,
      render: (ownerId: string) => (
        ownerId ? <Link to={`/companies/${ownerId}`} onClick={(e) => e.stopPropagation()}>{ownerId}</Link> : '-'
      ),
    },
  ];

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Title level={4} style={{ margin: 0 }}>
        Рабочие станции {term ? `по запросу "${term}"` : ''}
      </Title>
      <Card className="glass-panel">
        {isLoading ? (
          <div style={{ textAlign: 'center', padding: 32 }}><Spin /></div>
        ) : rows.length === 0 ? (
          <Empty description="Рабочие станции не найдены" />
        ) : (
          <Table<Row>
            rowKey="id"
            dataSource={rows}
            columns={columns}
            pagination={false}
            onRow={(record) => ({
              onClick: () => record.id && navigate(`/workstations/${record.id}`),
              style: { cursor: 'pointer' },
            })}
          />
        )}
        {!isLoading && rows.length > 0 && <Text type="secondary">Найдено: {total}</Text>}
        <div ref={loadMoreRef} style={{ marginTop: 16, display: 'flex', justifyContent: 'center', minHeight: 40 }}>
          {(isFetchingNextPage || (hasNextPage && rows.length > 0)) && <Spin size="small" />}
          {!hasNextPage && rows.length > 0 && (
            <Text type="secondary">Показано: {rows.length} из {total}</Text>
          )}
        </div>
      </Card>
    </Space>
  );
};

export default WorkstationsPage;
