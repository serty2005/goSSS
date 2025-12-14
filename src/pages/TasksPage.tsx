import React, { useState } from 'react';
import { Table, Card, Select, Button, Space, Typography } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { tasksApi } from '@/api/tasks';
import { TaskDTO, TaskStatus } from '@/types/api';
import { getEntityIcon } from '@/utils/mappers';
import TaskStatusTag from '@/components/common/TaskStatusTag';
import TaskResolutionModal from '@/features/tasks/TaskResolutionModal';
import { ReloadOutlined } from '@ant-design/icons';

const { Option } = Select;
const { Title } = Typography;

// Тип для фильтра, включающий 'all'
type FilterStatus = TaskStatus | 'all';

const TasksPage: React.FC = () => {
  const [statusFilter, setStatusFilter] = useState<FilterStatus>('new');
  const [page, setPage] = useState(1);
  const [selectedTask, setSelectedTask] = useState<TaskDTO | null>(null);

  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: ['tasks', statusFilter, page],
    queryFn: () => tasksApi.getTasks(statusFilter === 'all' ? undefined : statusFilter, page),
  });

  const columns = [
    {
      title: 'ID',
      dataIndex: 'id',
      width: 80,
    },
    {
      title: 'Тип',
      dataIndex: 'task_type',
      render: (type: string) => <Space>{getEntityIcon('default')} {type}</Space>,
    },
    {
      title: 'Сущность',
      dataIndex: 'entity_type',
    },
    {
      title: 'Статус',
      dataIndex: 'status',
      render: (status: TaskStatus) => <TaskStatusTag status={status} />,
    },
    {
      title: 'Создана',
      dataIndex: 'created_at',
      render: (date: string) => new Date(date).toLocaleString(),
    },
    {
      title: 'Действие',
      key: 'action',
      // Используем unknown для первого аргумента, так как он не используется
      render: (_: unknown, record: TaskDTO) => (
        <Button size="small" onClick={() => setSelectedTask(record)}>
          Открыть
        </Button>
      ),
    },
  ];

  return (
    <div style={{ padding: 0 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <Title level={3} style={{ margin: 0 }}>Задачи оператора</Title>
        <Space>
           <Button icon={<ReloadOutlined />} onClick={() => refetch()} loading={isFetching} />
           {/* Явно указываем Generic для Select, чтобы val имел правильный тип */}
           <Select<FilterStatus>
             defaultValue="new" 
             style={{ width: 200 }} 
             onChange={(val) => setStatusFilter(val)}
           >
             <Option value="all">Все задачи</Option>
             <Option value="new">Новые</Option>
             <Option value="resolved">Решенные</Option>
             <Option value="rejected">Отклоненные</Option>
             <Option value="pending_sd_action">В обработке SD</Option>
           </Select>
        </Space>
      </div>

      <Card className="glass-panel" styles={{ body: { padding: 0 } }}>
        <Table
          dataSource={data?.data}
          columns={columns}
          rowKey="id"
          loading={isLoading}
          pagination={{
            current: page,
            total: data?.meta?.total || 0,
            pageSize: data?.meta?.limit || 50,
            onChange: (p) => setPage(p),
          }}
        />
      </Card>

      <TaskResolutionModal
        visible={!!selectedTask}
        task={selectedTask}
        onClose={() => setSelectedTask(null)}
      />
    </div>
  );
};

export default TasksPage;