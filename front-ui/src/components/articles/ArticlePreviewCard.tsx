import React from 'react';
import dayjs from 'dayjs';
import { Link } from 'react-router-dom';
import { Card, Space, Tag, Typography } from 'antd';
import type { ArticleDTO } from '@/types/api';
import ArticleTypeTag from '@/components/articles/ArticleTypeTag';
import ArticleStatusTag from '@/components/articles/ArticleStatusTag';

const { Text, Title, Paragraph } = Typography;

type Props = {
  article: ArticleDTO;
  showStatus?: boolean;
  compact?: boolean;
};

const formatArticleDate = (article: ArticleDTO) => {
  const value = article.published_at || article.updated_at;
  const date = dayjs(value);
  return date.isValid() ? date.format('DD.MM.YYYY') : '';
};

const ArticlePreviewCard: React.FC<Props> = ({ article, showStatus = false, compact = false }) => (
  <Card size="small" className="article-preview-card" hoverable bodyStyle={{ padding: compact ? 12 : 16 }}>
    <Space direction="vertical" size={8} style={{ width: '100%' }}>
      <Space size={6} wrap>
        <ArticleTypeTag type={article.type} />
        {showStatus ? <ArticleStatusTag status={article.status} /> : null}
        {showStatus && article.show_on_home ? <Tag color="blue" style={{ marginInlineEnd: 0 }}>На главной</Tag> : null}
        {article.is_pinned ? <Tag color="gold" style={{ marginInlineEnd: 0 }}>Закреплено</Tag> : null}
      </Space>
      <Link to={`/info/articles/${article.id}`} style={{ color: 'inherit' }}>
        <Title level={compact ? 5 : 4} style={{ margin: 0 }}>
          {article.title}
        </Title>
      </Link>
      {article.summary ? (
        <Paragraph type="secondary" ellipsis={{ rows: compact ? 2 : 3 }} style={{ margin: 0 }}>
          {article.summary}
        </Paragraph>
      ) : null}
      <Space size={6} wrap>
        {article.type === 'release_note' && (article.project_key || article.version) ? (
          <Text type="secondary">{[article.project_key, article.version].filter(Boolean).join(' / ')}</Text>
        ) : null}
        <Text type="secondary">{formatArticleDate(article)}</Text>
        {article.tags.slice(0, 4).map((tag) => (
          <Tag key={tag} style={{ marginInlineEnd: 0 }}>{tag}</Tag>
        ))}
      </Space>
    </Space>
  </Card>
);

export default ArticlePreviewCard;
