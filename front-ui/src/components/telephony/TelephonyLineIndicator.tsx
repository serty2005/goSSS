import React, { startTransition, useEffect, useState } from 'react';
import { Button, Empty, Popover, Space, Spin, Tag, Typography } from 'antd';
import { useNavigate } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { telephonyApi } from '@/api/telephony';
import { useSSE } from '@/features/realtime/useSSE';
import { useAuthStore } from '@/store/authStore';
import { isAdmin } from '@/utils/permissions';
import type { TelephonyLineDTO } from '@/types/api';

const { Text } = Typography;

const colorMap: Record<string, string> = {
  blue: '#1677ff',
  yellow: '#faad14',
  green: '#52c41a',
  red: '#ff4d4f',
};

const lineQueryKey = ['telephony', 'line'] as const;

const TelephonyLineIndicator: React.FC = () => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { subscribe } = useSSE();
  const user = useAuthStore((state) => state.user);
  const [popoverOpen, setPopoverOpen] = useState(false);

  const { data, isLoading } = useQuery({
    queryKey: lineQueryKey,
    queryFn: () => telephonyApi.getLine(),
    staleTime: 15_000,
  });

  useEffect(() => subscribe('telephony.line.updated', (_eventType, rawData) => {
    try {
      const next = JSON.parse(rawData) as TelephonyLineDTO;
      startTransition(() => {
        queryClient.setQueryData(lineQueryKey, next);
      });
    } catch {
      // Игнорируем битый payload SSE.
    }
  }), [queryClient, subscribe]);

  const line = data ?? { color: 'red', on_line_count: 0, missed_open_count: 0, employees: [] };
  const indicatorColor = colorMap[line.color] ?? colorMap.red;

  return (
    <Popover
      trigger="click"
      placement="bottomRight"
      open={popoverOpen}
      onOpenChange={setPopoverOpen}
      content={(
        <div style={{ minWidth: 320, maxWidth: 420 }}>
          {isLoading ? (
            <div style={{ textAlign: 'center', padding: 16 }}>
              <Spin size="small" />
            </div>
          ) : line.employees.length === 0 ? (
            <Empty description="На линии никого нет" image={Empty.PRESENTED_IMAGE_SIMPLE} />
          ) : (
            <Space direction="vertical" size={8} style={{ width: '100%' }}>
              {line.employees.map((employee) => {
                const canOpenEmployeeCalls = isAdmin(user?.roles) || (employee.user_id !== undefined && employee.user_id === user?.id);
                const label = (
                  <Space direction="vertical" size={0} style={{ width: '100%', alignItems: 'flex-start' }}>
                    <Text strong>{employee.name || employee.login}</Text>
                    <Space size={6}>
                      <Tag color={employee.status === 'in_call' ? 'gold' : employee.status === 'online' ? 'green' : 'default'}>
                        {employee.status === 'in_call' ? 'В разговоре' : employee.status === 'online' ? 'На линии' : 'Оффлайн'}
                      </Tag>
                      {employee.provider_ext ? <Text type="secondary">Вн. {employee.provider_ext}</Text> : null}
                    </Space>
                  </Space>
                );

                if (!canOpenEmployeeCalls || employee.user_id === undefined) {
                  return (
                    <div key={`${employee.login}-${employee.user_id ?? 'none'}`}>
                      {label}
                    </div>
                  );
                }

                return (
                  <Button
                    key={`${employee.login}-${employee.user_id}`}
                    type="text"
                    style={{ width: '100%', height: 'auto', justifyContent: 'flex-start', paddingInline: 0 }}
                    onClick={() => {
                      setPopoverOpen(false);
                      navigate(`/telephony/users/${employee.user_id}/calls`);
                    }}
                  >
                    {label}
                  </Button>
                );
              })}
              {line.missed_open_count > 0 ? (
                <Tag color="blue" style={{ alignSelf: 'flex-start' }}>
                  Пропущенных без обратного действия: {line.missed_open_count}
                </Tag>
              ) : null}
            </Space>
          )}
        </div>
      )}
    >
      <Button type="text" style={{ paddingInline: 0 }}>
        <Space size={10}>
          <span
            style={{
              width: 12,
              height: 12,
              borderRadius: '50%',
              backgroundColor: indicatorColor,
              boxShadow: `0 0 0 4px ${indicatorColor}22`,
              flex: '0 0 auto',
            }}
          />
          <Space direction="vertical" size={0} style={{ alignItems: 'flex-start' }}>
            <Text strong>На линии: {line.on_line_count} сотрудников</Text>
            <Text type="secondary" style={{ fontSize: 12 }}>
              {line.color === 'blue' ? 'Есть необработанные пропущенные' : line.color === 'yellow' ? 'Есть активный разговор' : line.color === 'green' ? 'Линия активна' : 'Интегрированных сотрудников нет'}
            </Text>
          </Space>
        </Space>
      </Button>
    </Popover>
  );
};

export default TelephonyLineIndicator;
