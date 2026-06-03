import React from 'react';
import { Tag } from 'antd';
import type { ArticleStatus } from '@/types/api';

const articleStatusMeta: Record<ArticleStatus, { label: string; color: string }> = {
  draft: { label: 'Черновик', color: 'default' },
  published: { label: 'Опубликовано', color: 'success' },
  archived: { label: 'Архив', color: 'warning' },
};

const ArticleStatusTag: React.FC<{ status: ArticleStatus }> = ({ status }) => {
  const meta = articleStatusMeta[status] || articleStatusMeta.draft;
  return <Tag color={meta.color} style={{ marginInlineEnd: 0 }}>{meta.label}</Tag>;
};

export default ArticleStatusTag;
