import React, { useMemo, useState } from 'react';
import { Image } from 'antd';

const allowedTags = new Set([
  'A', 'B', 'I', 'S', 'STRONG', 'EM', 'U', 'P', 'BR', 'DIV', 'SPAN', 'UL', 'OL', 'LI', 'BLOCKQUOTE', 'IMG',
]);

const sanitizeURL = (value: string) => {
  const candidate = String(value || '').trim();
  if (!candidate) return '';
  if (/^javascript:/i.test(candidate)) return '';
  const normalized = candidate
    .replace(/^\/static\//i, '/api/static/')
    .replace(/^static\//i, '/api/static/');
  return normalized;
};

export const sanitizeRichHtml = (value?: string) => {
  const raw = String(value || '').trim();
  if (!raw) return '';
  const parser = new DOMParser();
  const doc = parser.parseFromString(raw, 'text/html');

  const walk = (element: Element) => {
    const children = Array.from(element.children);
    children.forEach((child) => {
      if (!allowedTags.has(child.tagName)) {
        const fragment = doc.createDocumentFragment();
        while (child.firstChild) {
          fragment.appendChild(child.firstChild);
        }
        child.replaceWith(fragment);
        return;
      }

      Array.from(child.attributes).forEach((attr) => {
        const name = attr.name.toLowerCase();
        const isAllowedData = name.startsWith('data-') && child.tagName === 'A' && child.classList.contains('etalon-user-link');
        const allowed = ['href', 'src', 'alt', 'class', 'target', 'rel'].includes(name) || isAllowedData;
        if (!allowed) {
          child.removeAttribute(attr.name);
        }
      });

      if (child.tagName === 'A') {
        const href = sanitizeURL(child.getAttribute('href') || '');
        if (href) {
          child.setAttribute('href', href);
          child.setAttribute('target', '_blank');
          child.setAttribute('rel', 'noreferrer');
        } else if (!child.classList.contains('etalon-user-link')) {
          child.removeAttribute('href');
        }
      }
      if (child.tagName === 'IMG') {
        const src = sanitizeURL(child.getAttribute('src') || '');
        if (!src) {
          child.remove();
          return;
        }
        child.setAttribute('src', src);
      }

      walk(child);
    });
  };

  walk(doc.body);
  return doc.body.innerHTML;
};

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

