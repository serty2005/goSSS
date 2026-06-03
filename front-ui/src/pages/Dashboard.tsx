import React from 'react';
import { Col, Row, Space } from 'antd';
import FeaturedArticlesPanel from '@/components/articles/FeaturedArticlesPanel';
import DashboardStatsPanel from '@/components/dashboard/DashboardStatsPanel';

const Dashboard: React.FC = () => (
  <Space direction="vertical" size="middle" style={{ width: '100%' }}>
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
