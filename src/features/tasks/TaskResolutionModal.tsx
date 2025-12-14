import React, { useState } from 'react';
import { Modal, Button, Input, Descriptions, Space, message, Alert } from 'antd';
import { TaskDTO, TaskResolutionPayload } from '@/types/api';
import { tasksApi } from '@/api/tasks';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import TaskStatusTag from '@/components/common/TaskStatusTag';

interface Props {
  task: TaskDTO | null;
  visible: boolean;
  onClose: () => void;
}

const TaskResolutionModal: React.FC<Props> = ({ task, visible, onClose }) => {
  const [comment, setComment] = useState('');
  const queryClient = useQueryClient();

  // Мутация для решения задачи
  const resolveMutation = useMutation({
    mutationFn: (payload: TaskResolutionPayload) => 
      tasksApi.resolveTask(task!.id, payload),
    onSuccess: () => {
      message.success('Задача обработана');
      queryClient.invalidateQueries({ queryKey: ['tasks'] });
      onClose();
      setComment('');
    },
    onError: () => message.error('Ошибка при обработке задачи'),
  });

  // Мутация для создания сущности в SD
  const createSdMutation = useMutation({
    mutationFn: () => tasksApi.createEntityInSd(task!.id, task!.entity_type),
    onSuccess: () => {
      message.success('Запрос на создание в SD отправлен');
      queryClient.invalidateQueries({ queryKey: ['tasks'] });
      onClose();
    },
  });

  if (!task) return null;

  const handleResolve = () => {
    // В реальном приложении здесь может быть сложная форма выбора действия (Action)
    // Для примера берем action='create' или 'approve' по умолчанию
    resolveMutation.mutate({
      status: 'resolved',
      comment,
      resolution_payload: { action: 'create' }
    });
  };

  const handleReject = () => {
    resolveMutation.mutate({
      status: 'rejected',
      comment,
      resolution_payload: { action: 'reject' } // Зависит от бэкенда
    });
  };

  const renderDetails = () => {
    // Рендерим детали динамически, так как структура зависит от типа задачи
    return (
      <Descriptions bordered column={1} size="small" style={{ marginTop: 16 }}>
        {Object.entries(task.details).map(([key, value]) => {
          // Если значение - объект, рендерим его как JSON string (упрощенно)
          const displayValue = typeof value === 'object' ? JSON.stringify(value, null, 2) : String(value);
          return (
            <Descriptions.Item key={key} label={key}>
              <pre style={{ margin: 0, whiteSpace: 'pre-wrap', maxHeight: 200, overflow: 'auto' }}>
                {displayValue}
              </pre>
            </Descriptions.Item>
          );
        })}
      </Descriptions>
    );
  };

  return (
    <Modal
      title={`Задача #${task.id} (${task.task_type})`}
      open={visible}
      onCancel={onClose}
      footer={null}
      width={700}
      className="glass-panel"
    >
      <div style={{ marginBottom: 16 }}>
        <Space>
           <TaskStatusTag status={task.status} />
           <span style={{ color: '#8c8c8c' }}>{new Date(task.created_at).toLocaleString()}</span>
        </Space>
      </div>

      {task.status === 'new' && (
         <Alert message="Требуется решение оператора" type="info" showIcon style={{ marginBottom: 16 }} />
      )}

      {renderDetails()}

      <div style={{ marginTop: 24 }}>
        <Input.TextArea 
          placeholder="Комментарий к решению..." 
          rows={3} 
          value={comment} 
          onChange={e => setComment(e.target.value)}
          style={{ marginBottom: 16 }}
          disabled={task.status !== 'new'}
        />

        {task.status === 'new' && (
          <Space>
            <Button 
              type="primary" 
              onClick={handleResolve} 
              loading={resolveMutation.isPending}
            >
              Подтвердить / Решить
            </Button>
            <Button 
              danger 
              onClick={handleReject}
              loading={resolveMutation.isPending}
            >
              Отклонить
            </Button>
            {/* Спец кнопка для Add Equipment, если еще не отправлено в SD */}
            {task.task_type === 'add_equipment' && (
               <Button 
                 type="dashed" 
                 onClick={() => createSdMutation.mutate()}
                 loading={createSdMutation.isPending}
               >
                 Создать в ServiceDesk
               </Button>
            )}
          </Space>
        )}
      </div>
    </Modal>
  );
};

export default TaskResolutionModal;