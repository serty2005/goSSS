import React from 'react';
import { useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Card, List, Typography, Spin, Empty, Badge, Button, Space } from 'antd';
import { searchApi } from '@/api/search';
import { getEntityIcon, getEntityLabel, getStatusColor } from '@/utils/mappers';
import { ArrowRightOutlined } from '@ant-design/icons';
import { SearchFoundEntity, EntityData } from '@/types/api';

const { Title, Text } = Typography;

const SearchPage: React.FC = () => {
  const [searchParams] = useSearchParams();
  const term = searchParams.get('term') || '';

  const { data, isLoading, isError } = useQuery({
    queryKey: ['search', term],
    queryFn: () => searchApi.searchEntities(term),
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
                <Space>
                   {getEntityIcon('Company')}
                   <Text strong>{group.owner.name}</Text>
                   <Text type="secondary" style={{ fontSize: 12 }}>{group.owner.address}</Text>
                </Space>
              }
              className="glass-panel"
              extra={<Button type="link">Перейти к компании <ArrowRightOutlined /></Button>}
            >
              <List
                itemLayout="horizontal"
                dataSource={group.found_entities}
                renderItem={(item: SearchFoundEntity) => {
                  const d = item.data as EntityData;
                  const title = d.device_name || d.rn_kkt || d.uuid;
                  const subtitle = d.ip || d.serial_number || '';
                  const statusRaw = d.operational_status || d.health_status;
                  
                  // getStatusColor теперь возвращает корректный Union Type для Badge
                  const badgeStatus = getStatusColor(statusRaw);

                  return (
                    <List.Item>
                      <List.Item.Meta
                        avatar={
                          <div style={{ fontSize: 24, color: '#1890ff' }}>
                            {getEntityIcon(item.entity_type)}
                          </div>
                        }
                        title={
                          <Space>
                            <Text strong>{title}</Text>
                            <Badge status={badgeStatus} text={statusRaw || 'unknown'} />
                          </Space>
                        }
                        description={
                          <Space split="|">
                             <Text type="secondary">{getEntityLabel(item.entity_type)}</Text>
                             <Text>{subtitle}</Text>
                          </Space>
                        }
                      />
                      <Button size="small">Детали</Button>
                    </List.Item>
                  );
                }}
              />
            </Card>
          ))}
        </Space>
      )}
    </div>
  );
};

export default SearchPage;