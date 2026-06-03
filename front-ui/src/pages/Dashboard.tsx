import React from 'react';
import { Col, Row, Space, Typography } from 'antd';
import DashboardStatsPanel from '@/components/dashboard/DashboardStatsPanel';
import FeaturedArticlesPanel from '@/components/articles/FeaturedArticlesPanel';

const { Title, Text } = Typography;

const Dashboard: React.FC = () => (
  <Space direction="vertical" size="middle" style={{ width: '100%' }}>
    <Space direction="vertical" size={2}>
      <Title level={2} style={{ margin: 0 }}>Обзор системы</Title>
      <Text type="secondary">Оперативная статистика и свежие материалы для смены поддержки.</Text>
    </Space>

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

export default Dashboard;
