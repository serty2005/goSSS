import React, { useMemo, useState } from 'react';
import { useLocation, useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeftOutlined } from '@ant-design/icons';
import { Badge, Button, Card, Form, Input, Select, Space, Spin, Typography, message } from 'antd';
import { contractsApi } from '@/api/contracts';

const { Title, Text } = Typography;

const normalizeServices = (raw: unknown): string[] => {
  if (Array.isArray(raw)) {
    return raw.map((item) => String(item));
  }
  if (raw && typeof raw === 'object') {
    return Object.values(raw as Record<string, unknown>).map((item) => String(item));
  }
  return [];
};

const ContractDetails: React.FC = () => {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  const queryClient = useQueryClient();
  const [form] = Form.useForm<{ state: string; contract_type: string }>();
  const [hasInitialized, setHasInitialized] = useState(false);

  const { data: contractRes, isLoading } = useQuery({
    queryKey: ['contract', id],
    queryFn: () => contractsApi.getContract(id!),
    enabled: !!id,
  });

  const updateMutation = useMutation({
    mutationFn: async (values: { state: string; contract_type: string }) => {
      const current = contractRes?.data;
      const services = normalizeServices(current?.services ?? current?.Services);
      const nextServices = services.length > 0 ? [...services] : [''];
      nextServices[0] = values.contract_type.trim();

      return contractsApi.updateContract(id!, {
        state: values.state,
        services: nextServices,
      });
    },
    onSuccess: () => {
      message.success('Контракт обновлён');
      queryClient.invalidateQueries({ queryKey: ['contract', id] });
      queryClient.invalidateQueries({ queryKey: ['company'] });
    },
    onError: () => message.error('Не удалось обновить контракт'),
  });

  const contract = contractRes?.data;
  const services = useMemo(() => normalizeServices(contract?.services ?? contract?.Services), [contract?.services, contract?.Services]);

  if (isLoading) {
    return (
      <div style={{ padding: 50, textAlign: 'center' }}>
        <Spin size="large" />
      </div>
    );
  }

  if (!contract) {
    return <div>Контракт не найден</div>;
  }

  if (!hasInitialized) {
    form.setFieldsValue({
      state: contract.state || contract.State || 'active',
      contract_type: services[0] || '',
    });
    setHasInitialized(true);
  }

  const handleBack = () => {
    const backTo = (location.state as { backTo?: string } | null)?.backTo;
    if (backTo) {
      navigate(backTo);
      return;
    }
    navigate(-1);
  };

  return (
    <div>
      <div style={{ marginBottom: 16, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Space align="center">
          <Button icon={<ArrowLeftOutlined />} onClick={handleBack} />
          <div>
            <Title level={4} style={{ margin: 0 }}>Контракт</Title>
            <Text type="secondary">{contract.id || contract.ID}</Text>
          </div>
        </Space>
        <Badge
          status={(contract.state || contract.State) === 'active' ? 'success' : 'default'}
          text={contract.state || contract.State || 'unknown'}
        />
      </div>

      <Card className="glass-panel" size="small" title="Редактирование контракта">
        <Form form={form} layout="vertical" onFinish={(values) => updateMutation.mutate(values)}>
          <Form.Item label="Статус" name="state" rules={[{ required: true, message: 'Укажите статус' }]}> 
            <Select
              options={[
                { label: 'active', value: 'active' },
                { label: 'inactive', value: 'inactive' },
                { label: 'pending', value: 'pending' },
              ]}
            />
          </Form.Item>
          <Form.Item label="Тип контракта (services[0])" name="contract_type" rules={[{ required: true, message: 'Укажите тип контракта' }]}> 
            <Input placeholder="Название типа контракта" />
          </Form.Item>

          <Space>
            <Button type="primary" htmlType="submit" loading={updateMutation.isPending}>Сохранить</Button>
            <Button onClick={handleBack}>Закрыть</Button>
          </Space>
        </Form>
      </Card>
    </div>
  );
};

export default ContractDetails;
