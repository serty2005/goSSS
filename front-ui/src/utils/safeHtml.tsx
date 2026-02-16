import React, { useMemo, useState } from 'react';
import { Image } from 'antd';
import { sanitizeRichHtml } from '@/utils/sanitizeRichHtml';

const getImageSources = (html: string): string[] => {
  const parser = new DOMParser();
  const doc = parser.parseFromString(html, 'text/html');
  return Array.from(doc.querySelectorAll('img'))
    .map((item) => String(item.getAttribute('src') || '').trim())
    .filter(Boolean);
};

type SafeHtmlContentProps = {
  html?: string;
  onClick?: React.MouseEventHandler<HTMLDivElement>;
  style?: React.CSSProperties;
};

export const SafeHtmlContent: React.FC<SafeHtmlContentProps> = ({ html, onClick, style }) => {
  const sanitized = useMemo(() => sanitizeRichHtml(html), [html]);
  const images = useMemo(() => getImageSources(sanitized), [sanitized]);
  const [previewIndex, setPreviewIndex] = useState(-1);

  return (
    <>
      <div
        style={style}
        onClick={(event) => {
          const target = event.target as HTMLElement | null;
          if (target && target.tagName === 'IMG') {
            const src = String((target as HTMLImageElement).src || '').trim();
            const index = images.findIndex((item) => item === src || src.endsWith(item));
            if (index >= 0) {
              setPreviewIndex(index);
            }
          }
          onClick?.(event);
        }}
        dangerouslySetInnerHTML={{ __html: sanitized || '<span>Нет данных</span>' }}
      />

      {images.length > 0 && (
        <Image.PreviewGroup
          preview={{
            visible: previewIndex >= 0,
            current: previewIndex >= 0 ? previewIndex : 0,
            onVisibleChange: (visible) => {
              if (!visible) {
                setPreviewIndex(-1);
              }
            },
          }}
        >
          {images.map((src) => (
            <Image
              key={src}
              src={src}
              alt=""
              style={{ display: 'none' }}
            />
          ))}
        </Image.PreviewGroup>
      )}

      <style>{`
        .etalon-user-link {
          text-decoration: underline;
        }
        img {
          max-width: 100%;
          max-height: 280px;
          object-fit: contain;
          border: 1px solid #f0f0f0;
          border-radius: 8px;
          cursor: zoom-in;
        }
      `}</style>
    </>
  );
};
