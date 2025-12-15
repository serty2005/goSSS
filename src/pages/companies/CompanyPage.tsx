import React, { useMemo } from 'react';
import { useParams, Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Typography, Tabs, Tag, Descriptions, Spin, Empty, Row, Col, Card, Button } from 'antd';
import { BankOutlined, CheckCircleOutlined, CloseCircleOutlined, ArrowLeftOutlined, PlusOutlined } from '@ant-design/icons';
import { companiesApi } from '@/api/companies';
import { ServerEntity, WorkstationEntity, FiscalEntity } from '@/types/api';
import ServerCard from '@/components/entities/ServerCard';
import WorkstationCard from '@/components/entities/WorkstationCard';
import FiscalCard from '@/components/entities/FiscalCard';
import TicketTable from '@/components/tickets/TicketTable'; // Import

const { Title, Text } = Typography;

const CompanyPage: React.FC = () => {
  const { id } = useParams<{ id: string }>();

  // Запрос профиля
  const { data: companyRes, isLoading: loadingCompany } = useQuery({
    queryKey: ['company', id],
    queryFn: () => companiesApi.getCompany(id!),
    enabled: !!id,
  });

  // Запрос инфраструктуры
  const { data: infraRes, isLoading: loadingInfra } = useQuery({
    queryKey: ['company', id, 'infra'],
    queryFn: () => companiesApi.getInfrastructure(id!),
    enabled: !!id,
  });

  const company = companyRes?.data;
  const rawInfrastructure = infraRes?.data;

  // Группировка инфраструктуры
  const groupedInfra = useMemo(() => {
    const servers: ServerEntity[] = [];
    const workstations: WorkstationEntity[] = [];
    const fiscals: FiscalEntity[] = [];
    
    const list = rawInfrastructure || [];

    list.forEach(item => {
      if (item.entity_type === 'Server') servers.push(item.data as ServerEntity);
      else if (item.entity_type === 'Workstation') workstations.push(item.data as WorkstationEntity);
      else if (item.entity_type === 'FiscalRegister') fiscals.push(item.data as FiscalEntity);
    });

    return { servers, workstations, fiscals };
  }, [rawInfrastructure]);

  if (loadingCompany) return <div style={{ padding: 50, textAlign: 'center' }}><Spin size="large" /></div>;
  if (!company) return <Empty description="Компания не найдена" />;

  const items = [
    {
      key: 'infrastructure',
      label: 'Инфраструктура',
      children: (
        <div style={{ marginTop: 16 }}>
          {loadingInfra ? <Spin /> : (
            <>
              {/* Секция Серверов */}
              {groupedInfra.servers.length > 0 && (
                <div style={{ marginBottom: 24 }}>
                   <Title level={5}>Серверы ({groupedInfra.servers.length})</Title>
                   <Row gutter={[16, 16]}>
                     {groupedInfra.servers.map(srv => (
                       <Col key={srv.uuid} xs={24} md={12} lg={8} xl={6}>
                         <ServerCard data={srv} />
                       </Col>
                     ))}
                   </Row>
                </div>
              )}

              {/* Секция Касс */}
              {groupedInfra.fiscals.length > 0 && (
                <div style={{ marginBottom: 24 }}>
                   <Title level={5}>Фискальные регистраторы ({groupedInfra.fiscals.length})</Title>
                   <Row gutter={[16, 16]}>
                     {groupedInfra.fiscals.map(fr => (
                       <Col key={fr.uuid} xs={24} md={12} lg={8} xl={6}>
                         <FiscalCard data={fr} />
                       </Col>
                     ))}
                   </Row>
                </div>
              )}

              {/* Секция Рабочих станций */}
              {groupedInfra.workstations.length > 0 && (
                 <div style={{ marginBottom: 24 }}>
                   <Title level={5}>Рабочие станции ({groupedInfra.workstations.length})</Title>
                   <Row gutter={[16, 16]}>
                     {groupedInfra.workstations.map(ws => (
                       <Col key={ws.uuid} xs={24} sm={12} md={8} lg={6}>
                         <WorkstationCard data={ws} />
                       </Col>
                     ))}
                   </Row>
                 </div>
              )}

              {(!rawInfrastructure || rawInfrastructure.length === 0) && <Empty description="Оборудование не найдено" />}
            </>
          )}
        </div>
      ),
    },
    {
      key: 'tickets',
      label: 'Тикеты',
      children: (
        <div style={{ marginTop: 16 }}>
           <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'flex-end' }}>
             <Button type="primary" icon={<PlusOutlined />} onClick={() => console.log('Create Ticket')}>
               Создать тикет
             </Button>
           </div>
           {/* Передаем ID компании для фильтрации */}
           <TicketTable companyId={company.ID} limit={10} />
        </div>
      ),
    },
    {
      key: 'contracts',
      label: 'Контракты',
      children: <Empty description="Раздел в разработке" style={{ marginTop: 20 }} />,
    },
  ];

  return (
    <div>
      {/* Header Профиля */}
      <Card className="glass-panel" style={{ marginBottom: 24 }}>
        <Link to="/companies" style={{ display: 'inline-flex', alignItems: 'center', marginBottom: 16, color: '#8c8c8c' }}>
           <ArrowLeftOutlined style={{ marginRight: 8 }} /> Назад к списку
        </Link>
        
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
          <div style={{ display: 'flex', alignItems: 'center' }}>
            <div style={{ 
              width: 64, height: 64, 
              background: '#e6f7ff', 
              borderRadius: 8, 
              display: 'flex', alignItems: 'center', justifyContent: 'center',
              marginRight: 16,
              fontSize: 32, color: '#1890ff'
            }}>
              <BankOutlined />
            </div>
            <div>
              <Title level={3} style={{ margin: 0 }}>{company.Title}</Title>
              <Text type="secondary">{company.Address}</Text>
            </div>
          </div>
          
          <div style={{ textAlign: 'right' }}>
             {company.ActiveContract ? (
               <Tag icon={<CheckCircleOutlined />} color="success">Контракт Активен</Tag>
             ) : (
               <Tag icon={<CloseCircleOutlined />} color="error">Нет контракта</Tag>
             )}
             <div style={{ marginTop: 8 }}>
               <Text type="secondary" style={{ fontSize: 12 }}>ID: {company.ID}</Text>
             </div>
          </div>
        </div>

        <Descriptions size="small" style={{ marginTop: 24 }} column={2}>
           <Descriptions.Item label="Юр. название">{company.AdditionalName || '-'}</Descriptions.Item>
           <Descriptions.Item label="Родительская компания">
              {company.ParentID ? <Link to={`/companies/${company.ParentID}`}>{company.ParentID}</Link> : '-'}
           </Descriptions.Item>
           <Descriptions.Item label="Обновлено">
              {company.LastModifiedDate ? new Date(company.LastModifiedDate).toLocaleDateString() : '-'}
           </Descriptions.Item>
        </Descriptions>
      </Card>

      {/* Основной контент */}
      <Tabs defaultActiveKey="infrastructure" items={items} />
    </div>
  );
};

export default CompanyPage;