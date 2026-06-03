import React from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Button, Card, Divider, Skeleton, Space, Tag, Typography, message } from 'antd';
import { EditOutlined } from '@ant-design/icons';
import { articlesApi } from '@/api/articles';
import ArticleTypeTag from '@/components/articles/ArticleTypeTag';
import ArticleStatusTag from '@/components/articles/ArticleStatusTag';
import { useAuthStore } from '@/store/authStore';

const { Title, Paragraph, Text } = Typography;

const ArticleDetailsPage: React.FC = () => {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const user = useAuthStore((state) => state.user);
  const canEdit = Boolean(user?.roles?.includes('admin') || user?.roles?.includes('support_specialist'));
  const { data, isLoading } = useQuery({
    queryKey: ['article', id],
    queryFn: () => articlesApi.get(id),
    enabled: Boolean(id),
    staleTime: 60_000,
  });
  const article = data?.data;

  const publishMutation = useMutation({
    mutationFn: () => articlesApi.publish(id),
    onSuccess: async () => {
      message.success('Публикация опубликована');
      await queryClient.invalidateQueries({ queryKey: ['article', id] });
      await queryClient.invalidateQueries({ queryKey: ['articles'] });
      await queryClient.invalidateQueries({ queryKey: ['articles-featured'] });
    },
  });

  const archiveMutation = useMutation({
    mutationFn: () => articlesApi.archive(id),
    onSuccess: async () => {
      message.success('Публикация отправлена в архив');
      await queryClient.invalidateQueries({ queryKey: ['article', id] });
      await queryClient.invalidateQueries({ queryKey: ['articles'] });
      await queryClient.invalidateQueries({ queryKey: ['articles-featured'] });
    },
  });

  if (isLoading || !article) {
    return <Skeleton active paragraph={{ rows: 8 }} />;
  }

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Space style={{ justifyContent: 'space-between', width: '100%' }} align="start" wrap>
        <Space direction="vertical" size={8}>
          <Space size={6} wrap>
            <ArticleTypeTag type={article.type} />
            <ArticleStatusTag status={article.status} />
            {article.show_on_home ? <Tag color="blue">На главной</Tag> : null}
            {article.is_pinned ? <Tag color="gold">Закреплено</Tag> : null}
          </Space>
          <Title level={2} style={{ margin: 0 }}>{article.title}</Title>
          {article.summary ? <Paragraph type="secondary" style={{ margin: 0 }}>{article.summary}</Paragraph> : null}
          <Text type="secondary">
            {article.author_name} · {article.published_at || article.updated_at}
          </Text>
        </Space>
        {canEdit ? (
          <Space wrap>
            <Button icon={<EditOutlined />} onClick={() => navigate(`/info/articles/${article.id}/edit`)}>
              Редактировать
            </Button>
            {article.status !== 'published' ? (
              <Button type="primary" loading={publishMutation.isPending} onClick={() => publishMutation.mutate()}>
                Опубликовать
              </Button>
            ) : null}
            {article.status !== 'archived' ? (
              <Button danger loading={archiveMutation.isPending} onClick={() => archiveMutation.mutate()}>
                В архив
              </Button>
            ) : null}
          </Space>
        ) : null}
      </Space>

      <Space size={6} wrap>
        {article.project_key ? <Tag>{article.project_key}</Tag> : null}
        {article.version ? <Tag>{article.version}</Tag> : null}
        {article.tags.map((tag) => <Tag key={tag}>{tag}</Tag>)}
      </Space>

      <Card className="article-content-card">
        <div className="article-markdown">
          <ReactMarkdown remarkPlugins={[remarkGfm]}>{article.content}</ReactMarkdown>
        </div>
      </Card>

      <Divider />
      <Link to="/info">К базе знаний</Link>
    </Space>
  );
};

export default ArticleDetailsPage;
