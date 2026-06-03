import React from 'react';
import { Tag } from 'antd';
import type { ArticleType } from '@/types/api';

const articleTypeMeta: Record<ArticleType, { label: string; color: string }> = {
  wiki: { label: 'Wiki', color: 'blue' },
  release_note: { label: 'Release notes', color: 'green' },
  company_news: { label: 'Новости', color: 'cyan' },
  incident_note: { label: 'Инцидент', color: 'orange' },
  internal_doc: { label: 'Инструкция', color: 'geekblue' },
};

const ArticleTypeTag: React.FC<{ type: ArticleType }> = ({ type }) => {
  const meta = articleTypeMeta[type] || articleTypeMeta.wiki;
  return <Tag color={meta.color} style={{ marginInlineEnd: 0 }}>{meta.label}</Tag>;
};

export default ArticleTypeTag;
