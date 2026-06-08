import React from 'react';

export const DATA_TABLE_DEFAULT_MIN_COLUMN_WIDTH = 90;

export const estimateDataTableHeaderMinWidth = (title: string) => Math.max(80, title.length * 8 + 44);

export const createDataTableColumnMinWidth = (
  title: string,
  fallback = DATA_TABLE_DEFAULT_MIN_COLUMN_WIDTH,
) => Math.max(fallback, estimateDataTableHeaderMinWidth(title));

export const formatDataTableText = (value?: unknown) => {
  if (value === undefined || value === null) {
    return '';
  }

  return String(value)
    .replace(/\u00a0/g, ' ')
    .replace(/<\s*br\s*\/?>/gi, '\n')
    .replace(/<\/p>\s*<p>/gi, '\n')
    .replace(/<\/?p[^>]*>/gi, '\n')
    .replace(/<[^>]*>/g, ' ')
    .replace(/&nbsp;/gi, ' ')
    .replace(/&amp;/gi, '&')
    .replace(/&lt;/gi, '<')
    .replace(/&gt;/gi, '>')
    .replace(/&quot;/gi, '"')
    .replace(/&#39;/gi, "'")
    .replace(/&#x([0-9a-f]+);/gi, (_match, code: string) => String.fromCodePoint(parseInt(code, 16)))
    .replace(/&#(\d+);/g, (_match, code: string) => String.fromCodePoint(parseInt(code, 10)))
    .replace(/\r\n/g, '\n')
    .replace(/\r/g, '\n')
    .replace(/[ \t\f\v]+\n/g, '\n')
    .replace(/[ \t\f\v]{2,}/g, ' ')
    .replace(/\n{3,}/g, '\n\n')
    .trim();
};

export const serializeDataTableLayout = (columns?: Array<{ key: string; width?: number }>) =>
  JSON.stringify((columns || []).map((column) => ({
    key: String(column.key || ''),
    width: typeof column.width === 'number' ? column.width : undefined,
  })));

export const getDataTableTextTitle = (content: React.ReactNode, text: string) =>
  typeof content === 'string' ? content : text;
