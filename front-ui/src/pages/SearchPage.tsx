import React, { useState } from 'react';
import { useSearchParams, Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Card, Typography, Spin, Empty, Space, Row, Col, Button, Tag } from 'antd';
import { searchApi } from '@/api/search';
import { getEntityIcon } from '@/utils/mappers';
import { ArrowRightOutlined } from '@ant-design/icons';
import { SearchFoundEntity, ServerEntity, WorkstationEntity, FiscalEntity } from '@/types/api';
import ServerCard from '@/components/entities/ServerCard';
import WorkstationCard from '@/components/entities/WorkstationCard';
import FiscalCard from '@/components/entities/FiscalCard';
import NewTicketModal from '@/components/tickets/NewTicketModal';

const { Title, Text } = Typography;

const SearchPage: React.FC = () => {
  const [searchParams] = useSearchParams();
  const term = searchParams.get('term') || '';
  const showInactive = ['1', 'true', 'yes', 'on'].includes((searchParams.get('show_inactive') || '').toLowerCase());
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [presetCompany, setPresetCompany] = useState<{ id: string; title?: string } | null>(null);

  const { data, isLoading, isError } = useQuery({
    queryKey: ['search', term, showInactive],
    queryFn: () => searchApi.searchEntities(term, 50, showInactive),
    enabled: !!term,
    staleTime: 1000 * 60,
  });

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

  const results = data?.data?.search_results || [];

  const renderEntityCard = (item: SearchFoundEntity) => {
    switch (item.entity_type) {
      case 'Server':
        return <ServerCard data={item.data as ServerEntity} />;
      case 'Workstation':
        return <WorkstationCard data={item.data as WorkstationEntity} />;
      case 'FiscalRegister':
        return <FiscalCard data={item.data as FiscalEntity} />;
      default:
        return <div>Unknown entity type</div>;
    }
  };

  return (
    <div>
      <Title level={4}>Результаты поиска: "{term}"</Title>
      
      {results.length === 0 ? (
        <Empty description="Ничего не найдено" />
      ) : (
        <Space direction="vertical" style={{ width: '100%' }} size="large">
          {results.map((group) => (
            <Card 
              key={group.owner.uuid} 
              title={
                <Link to={`/companies/${group.owner.uuid}`} style={{ color: 'inherit' }}>
                  <Space>
                     {getEntityIcon('Company')}
                     <Text strong style={{ cursor: 'pointer', textDecoration: 'underline' }}>
                        {group.owner.name}
                     </Text>
                     <Text type="secondary" style={{ fontSize: 12 }}>{group.owner.address}</Text>
                     <Tag color={group.owner.active_contract ? 'success' : 'default'}>
                        {group.owner.active_contract ? 'Активен' : 'Завершён'}
                     </Tag>
                  </Space>
                </Link>
              }
              className="glass-panel"
              extra={
                 <Space>
                    <Button
                      type="link"
                      onClick={(event) => {
                        event.preventDefault();
                        setPresetCompany({ id: group.owner.uuid, title: group.owner.name });
                        setIsCreateOpen(true);
                      }}
                    >
                      Создать заявку
                    </Button>
                    <Link to={`/companies/${group.owner.uuid}`}>
                      <Button type="link">Перейти к компании <ArrowRightOutlined /></Button>
                    </Link>
                 </Space>
              }
            >
              <Row gutter={[16, 16]}>
                {group.found_entities.map((item, idx) => (
                  <Col key={`${item.entity_type}-${idx}`} xs={24} md={12} lg={8} xl={6}>
                    {renderEntityCard(item)}
                  </Col>
                ))}
              </Row>
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
