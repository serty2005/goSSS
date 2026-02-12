import React from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Card, Empty, List, Space, Spin, Tag, Typography } from 'antd';
import { BankOutlined } from '@ant-design/icons';
import { companiesApi } from '@/api/companies';
import { CompanyModel } from '@/types/api';
import { resolveCompanyID, resolveCompanyParentTitle, resolveCompanyTitle } from '@/utils/companyHierarchy';

const { Title, Text } = Typography;

const CompaniesListPage: React.FC = () => {
  const [searchParams] = useSearchParams();
  const term = (searchParams.get('q') || '').trim();

  const { data, isLoading } = useQuery({
    queryKey: ['companies', 'list', term],
    queryFn: () => companiesApi.searchCompanies(term, 100, 0),
    staleTime: 30_000,
  });

  if (isLoading) {
    return <div style={{ textAlign: 'center', padding: 40 }}><Spin size="large" /></div>;
  }

  const companies = data?.data || [];

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Title level={4} style={{ margin: 0 }}>
        Компании {term ? `по запросу "${term}"` : ''}
      </Title>

      <Card className="glass-panel">
        {companies.length === 0 ? (
          <Empty description="Компании не найдены" />
        ) : (
          <List
            dataSource={companies}
            renderItem={(item) => {
              const company = item as CompanyModel;
              const id = resolveCompanyID(company);
              const title = resolveCompanyTitle(company) || id;
              const parentTitle = resolveCompanyParentTitle(company);
              const address = company.address;
              const additional = company.additional_name;
              const is_active = company.active_contract === true;

              return (
                <List.Item key={id || title}>
                  <Space direction="vertical" size={2} style={{ width: '100%' }}>
                    <Space size={8}>
                      <BankOutlined />
                      {id ? <Link to={`/companies/${id}`}>{title}</Link> : <Text strong>{title}</Text>}
                      <Tag color={is_active ? 'success' : 'default'}>{is_active ? 'Активен' : 'Завершён'}</Tag>
                    </Space>
                    {parentTitle && <Text type="secondary">Группа: {parentTitle}</Text>}
                    {additional && <Text type="secondary">Юр. название: {additional}</Text>}
                    {address && <Text type="secondary">{address}</Text>}
                  </Space>
                </List.Item>
              );
            }}
          />
        )}
      </Card>
    </Space>
  );
};

export default CompaniesListPage;

