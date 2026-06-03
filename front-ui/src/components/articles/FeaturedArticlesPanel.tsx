import React from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Button, Card, Empty, Skeleton, Space, Typography } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { articlesApi } from '@/api/articles';
import ArticlePreviewCard from '@/components/articles/ArticlePreviewCard';
import { useAuthStore } from '@/store/authStore';

const { Title, Text } = Typography;

const FeaturedArticlesPanel: React.FC<{ limit?: number }> = ({ limit = 6 }) => {
  const user = useAuthStore((state) => state.user);
  const canCreate = Boolean(user?.roles?.includes('admin') || user?.roles?.includes('support_specialist'));
  const { data, isLoading } = useQuery({
    queryKey: ['articles-featured', limit],
    queryFn: () => articlesApi.featured(limit),
    staleTime: 60_000,
  });
  const articles = data?.data || [];

  return (
    <Card className="glass-panel" bodyStyle={{ padding: 16 }}>
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Space style={{ justifyContent: 'space-between', width: '100%' }} align="start">
          <Space direction="vertical" size={0}>
            <Title level={4} style={{ margin: 0 }}>База знаний</Title>
            <Text type="secondary">Свежие и закреплённые публикации</Text>
          </Space>
          <Link to="/info/articles">
            <Button type="link" size="small">Все статьи</Button>
          </Link>
        </Space>

        {isLoading ? (
          <Skeleton active paragraph={{ rows: 5 }} />
        ) : articles.length === 0 ? (
          <Empty
            description={canCreate ? 'Публикаций пока нет' : 'Опубликованных материалов пока нет'}
          >
            {canCreate ? (
              <Link to="/info/articles/new">
                <Button type="primary" icon={<PlusOutlined />}>Создать первую статью</Button>
              </Link>
            ) : null}
          </Empty>
        ) : (
          <Space direction="vertical" size="small" style={{ width: '100%' }}>
            {articles.map((article) => (
              <ArticlePreviewCard key={article.id} article={article} compact />
            ))}
          </Space>
        )}
      </Space>
    </Card>
  );
};

export default FeaturedArticlesPanel;
