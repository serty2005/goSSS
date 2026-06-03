import React from 'react';
import dayjs from 'dayjs';
import { useNavigate } from 'react-router-dom';
import { Card, Space, Tag, Typography } from 'antd';
import { ClockCircleOutlined, UserOutlined } from '@ant-design/icons';
import { TicketSearchDTO } from '@/types/api';
import { getTicketStatusMeta } from '@/constants/ticketStatus';

const { Text } = Typography;

const stripHtml = (value?: string) =>
  String(value || '')
    .replace(/<[^>]*>/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();

const formatActivity = (value?: string) => {
  if (!value) return '-';
  const date = dayjs(value);
  return date.isValid() ? date.format('DD.MM HH:mm') : '-';
};

type Props = {
  ticket: TicketSearchDTO;
  compact?: boolean;
};

const TicketSearchCard: React.FC<Props> = ({ ticket, compact = false }) => {
  const navigate = useNavigate();
  const statusMeta = getTicketStatusMeta(ticket.status);
  const excerpt = stripHtml(ticket.last_comment || ticket.description);

  return (
    <Card
      size="small"
      className="ticket-search-card"
      hoverable
      onClick={() => navigate(`/tickets/${ticket.id}`)}
      bodyStyle={{ padding: compact ? 10 : 12 }}
    >
      <Space direction="vertical" size={6} style={{ width: '100%' }}>
        <Space size={6} wrap style={{ justifyContent: 'space-between', width: '100%' }}>
          <Space size={6} wrap>
            <Text strong>#{ticket.number}</Text>
            <Tag color={statusMeta.color} style={{ marginInlineEnd: 0 }}>
              {statusMeta.label}
            </Tag>
            {ticket.created_source ? (
              <Tag color="default" style={{ marginInlineEnd: 0 }}>
                {ticket.created_source}
              </Tag>
            ) : null}
          </Space>
          <Text type="secondary" style={{ fontSize: 12 }}>
            <ClockCircleOutlined /> {formatActivity(ticket.last_activity || ticket.updated_at)}
          </Text>
        </Space>

        <Text strong ellipsis={{ tooltip: ticket.subject }} style={{ maxWidth: '100%' }}>
          {ticket.subject || 'Без темы'}
        </Text>

        {excerpt ? (
          <Text type="secondary" className="ticket-search-card__excerpt">
            {excerpt}
          </Text>
        ) : null}

        {ticket.assignee_name || ticket.reporter_name ? (
          <Space size={6} wrap>
            {ticket.assignee_name ? (
              <Text type="secondary" style={{ fontSize: 12 }}>
                <UserOutlined /> {ticket.assignee_name}
              </Text>
            ) : null}
            {ticket.reporter_name ? (
              <Text type="secondary" style={{ fontSize: 12 }}>
                Автор: {ticket.reporter_name}
              </Text>
            ) : null}
          </Space>
        ) : null}
      </Space>
    </Card>
  );
};

export default TicketSearchCard;
