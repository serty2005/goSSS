import React from 'react';
import { Link } from 'react-router-dom';
import { Button, Card, Col, Row, Space, Typography } from 'antd';
import { FileTextOutlined, PlusOutlined } from '@ant-design/icons';
import DashboardStatsPanel from '@/components/dashboard/DashboardStatsPanel';
import FeaturedArticlesPanel from '@/components/articles/FeaturedArticlesPanel';
import { useAuthStore } from '@/store/authStore';

const { Title, Text } = Typography;

const quickLinks = [
  { label: 'Все статьи', to: '/info/articles' },
  { label: 'Release notes', to: '/info/articles?type=release_note' },
  { label: 'Новости компании', to: '/info/articles?type=company_news' },
  { label: 'Wiki', to: '/info/articles?type=wiki' },
];

const InfoPage: React.FC = () => {
  const user = useAuthStore((state) => state.user);
  const canCreate = Boolean(user?.roles?.includes('admin') || user?.roles?.includes('support_specialist'));

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Space style={{ justifyContent: 'space-between', width: '100%' }} align="start" wrap>
        <Space direction="vertical" size={2}>
          <Title level={2} style={{ margin: 0 }}>Info</Title>
          <Text type="secondary">Статистика рабочего места, база знаний, новости и release notes.</Text>
        </Space>
        {canCreate ? (
          <Link to="/info/articles/new">
            <Button type="primary" icon={<PlusOutlined />}>Создать статью</Button>
          </Link>
        ) : null}
      </Space>

      <Card className="glass-panel" bodyStyle={{ padding: 12 }}>
        <Space size={8} wrap>
          <FileTextOutlined />
          {quickLinks.map((item) => (
            <Link key={item.to} to={item.to}>
              <Button size="small" type="text">{item.label}</Button>
            </Link>
          ))}
          {canCreate ? (
            <Link to="/info/articles/new">
              <Button size="small" type="text" icon={<PlusOutlined />}>Создать статью</Button>
            </Link>
          ) : null}
        </Space>
      </Card>

      <Row gutter={[16, 16]} align="top">
        <Col xs={24} xl={16}>
          <DashboardStatsPanel />
        </Col>
        <Col xs={24} xl={8}>
          <FeaturedArticlesPanel limit={6} />
        </Col>
      </Row>
    </Space>
  );
};

export default InfoPage;
