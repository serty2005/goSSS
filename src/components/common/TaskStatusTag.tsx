import React from 'react';
import { Tag } from 'antd';
import {
  CheckCircleOutlined,
  SyncOutlined,
  CloseCircleOutlined,
  ExclamationCircleOutlined
} from '@ant-design/icons';
import { TaskStatus } from '@/types/api';

interface Props {
  status: TaskStatus | string; // string для универсальности, если придет неизвестный статус
}

const TaskStatusTag: React.FC<Props> = ({ status }) => {
  let color = 'default';
  let label = status;
  let icon = null;

  switch (status) {
    case 'new': 
      color = 'blue'; 
      label = 'Новая'; 
      icon = <ExclamationCircleOutlined />;
      break;
    case 'resolved': 
      color = 'green'; 
      label = 'Решена'; 
      icon = <CheckCircleOutlined />;
      break;
    case 'rejected': 
      color = 'red'; 
      label = 'Отклонена'; 
      icon = <CloseCircleOutlined />;
      break;
    case 'pending_sd_action': 
      color = 'orange'; 
      label = 'В обработке SD'; 
      icon = <SyncOutlined spin />;
      break;
    case 'sd_error': 
      color = 'volcano'; 
      label = 'Ошибка SD'; 
      icon = <CloseCircleOutlined />;
      break;
  }

  return <Tag color={color} icon={icon}>{label}</Tag>;
};

export default TaskStatusTag;