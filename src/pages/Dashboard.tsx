import React from 'react';
import { Card, Typography, Row, Col, Statistic } from 'antd';
import { ArrowUpOutlined, ArrowDownOutlined } from '@ant-design/icons';

const Dashboard: React.FC = () => {
  return (
    <div>
      <Typography.Title level={2}>Обзор системы</Typography.Title>
      <Row gutter={16}>
        <Col span={8}>
          <Card>
            <Statistic
              title="Активные тикеты"
              value={11.28}
              precision={2}
              valueStyle={{ color: '#3f8600' }}
              prefix={<ArrowUpOutlined />}
              suffix="%"
            />
          </Card>
        </Col>
        <Col span={8}>
          <Card>
            <Statistic
              title="Новые задачи"
              value={9.3}
              precision={2}
              valueStyle={{ color: '#cf1322' }}
              prefix={<ArrowDownOutlined />}
              suffix="%"
            />
          </Card>
        </Col>
        <Col span={8}>
          <Card>
            <Statistic title="Оборудование онлайн" value={93} suffix="/ 100" />
          </Card>
        </Col>
      </Row>
      
      <Card style={{ marginTop: 24 }} title="Последние действия">
        <Typography.Paragraph>
          Здесь будет график или таблица с последними действиями в ServiceDesk.
        </Typography.Paragraph>
        <Typography.Paragraph type="secondary">
          Система работает в штатном режиме.
        </Typography.Paragraph>
      </Card>
    </div>
  );
};

export default Dashboard;