import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Image } from 'antd';
import hljs from 'highlight.js/lib/core';
import xml from 'highlight.js/lib/languages/xml';
import css from 'highlight.js/lib/languages/css';
import javascript from 'highlight.js/lib/languages/javascript';
import python from 'highlight.js/lib/languages/python';
import plaintext from 'highlight.js/lib/languages/plaintext';
import { sanitizeRichHtml } from '@/utils/sanitizeRichHtml';

hljs.registerLanguage('html', xml);
hljs.registerLanguage('xml', xml);
hljs.registerLanguage('css', css);
hljs.registerLanguage('js', javascript);
hljs.registerLanguage('javascript', javascript);
hljs.registerLanguage('python', python);
hljs.registerLanguage('log', plaintext);

type CodeLanguage = 'python' | 'log' | 'html' | 'css' | 'js';

const CODE_LANGUAGES: Array<{ value: CodeLanguage; label: string }> = [
  { value: 'python', label: 'Python' },
  { value: 'log', label: 'log' },
  { value: 'html', label: 'html' },
  { value: 'css', label: 'css' },
  { value: 'js', label: 'js' },
];

const CODE_LANGUAGE_SET = new Set<CodeLanguage>(CODE_LANGUAGES.map((item) => item.value));

const getImageSources = (html: string): string[] => {
  const parser = new DOMParser();
  const doc = parser.parseFromString(html, 'text/html');
  return Array.from(doc.querySelectorAll('img'))
    .map((item) => String(item.getAttribute('src') || '').trim())
    .filter(Boolean);
};

const detectLanguage = (source: string): CodeLanguage => {
  const normalized = String(source || '').trim();

  if (!normalized) return 'log';
  if (/(^|\n)\s*(INFO|WARN|WARNING|ERROR|DEBUG|TRACE)\b/i.test(normalized)) return 'log';
  if (/(^|\n)\s*\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}/.test(normalized)) return 'log';
  if (/<\/?[a-z][\w-]*\b[^>]*>/i.test(normalized) || /&lt;\/?[a-z][\w-]*\b/i.test(normalized)) return 'html';
  if (/(^|\n)\s*(def|class|import|from)\s+\w+/m.test(normalized) || /:\s*(#.*)?$/m.test(normalized)) return 'python';
  if (/[.#][\w-]+\s*\{[^}]*\}/.test(normalized) || /(^|\n)\s*(@media|--[\w-]+\s*:|[a-z-]+\s*:\s*[^;]+;)/i.test(normalized)) return 'css';
  if (/(^|\n)\s*(const|let|var|function)\s+\w+/m.test(normalized) || /=>|console\.|JSON\./.test(normalized)) return 'js';

  return 'log';
};

const normalizeLanguage = (codeClass: string, source: string): CodeLanguage => {
  const classMatch = String(codeClass || '').match(/language-([a-z0-9_-]+)/i);
  const raw = classMatch?.[1]?.toLowerCase() || '';
  if (!raw) return detectLanguage(source);
  if (raw === 'javascript') return 'js';
  if (raw === 'xml') return 'html';
  if (CODE_LANGUAGE_SET.has(raw as CodeLanguage)) return raw as CodeLanguage;
  return detectLanguage(source);
};

const toHljsLanguage = (value: CodeLanguage): string => {
  if (value === 'html') return 'xml';
  if (value === 'js') return 'javascript';
  if (value === 'log') return 'plaintext';
  return value;
};

const enhanceCodeBlocks = (html: string): string => {
  if (!html) return '';
  const parser = new DOMParser();
  const doc = parser.parseFromString(html, 'text/html');
  const preBlocks = Array.from(doc.querySelectorAll('pre'));

  preBlocks.forEach((preNode, index) => {
    const pre = preNode as HTMLElement;
    if (pre.closest('.safe-code-block')) {
      return;
    }
    const code = (pre.querySelector('code') as HTMLElement | null) || (() => {
      const created = doc.createElement('code');
      created.textContent = pre.textContent || '';
      pre.textContent = '';
      pre.appendChild(created);
      return created;
    })();

    const codeID = `code-${index + 1}`;
    const content = code.textContent || '';
    const language = normalizeLanguage(code.className, content);

    const wrapper = doc.createElement('div');
    wrapper.className = 'safe-code-block';

    const header = doc.createElement('div');
    header.className = 'safe-code-block__header';

    const title = doc.createElement('span');
    title.className = 'safe-code-block__title';
    title.textContent = 'code';

    const langWrap = doc.createElement('label');
    langWrap.className = 'safe-code-block__lang-wrap';

    const langLabel = doc.createElement('span');
    langLabel.className = 'safe-code-block__lang-label';
    langLabel.textContent = 'Язык:';

    const select = doc.createElement('select');
    select.className = 'safe-code-block__lang-select';
    select.setAttribute('data-code-id', codeID);

    CODE_LANGUAGES.forEach((item) => {
      const option = doc.createElement('option');
      option.value = item.value;
      option.textContent = item.label;
      option.selected = item.value === language;
      select.appendChild(option);
    });

    langWrap.appendChild(langLabel);
    langWrap.appendChild(select);
    header.appendChild(title);
    header.appendChild(langWrap);

    code.className = `language-${language}`;
    code.setAttribute('data-code-id', codeID);
    pre.classList.add('safe-code-block__body');

    pre.replaceWith(wrapper);
    wrapper.appendChild(header);
    wrapper.appendChild(pre);
  });

  return doc.body.innerHTML;
};

const applyHighlight = (codeNode: HTMLElement, language: CodeLanguage) => {
  const source = codeNode.textContent || '';
  const hljsLanguage = toHljsLanguage(language);

  codeNode.className = `language-${language}`;
  codeNode.removeAttribute('data-highlighted');

  try {
    if (hljsLanguage === 'plaintext') {
      codeNode.textContent = source;
      return;
    }
    const highlighted = hljs.highlight(source, { language: hljsLanguage }).value;
    codeNode.innerHTML = highlighted;
  } catch {
    codeNode.textContent = source;
  }
};

type SafeHtmlContentProps = {
  html?: string;
  onClick?: React.MouseEventHandler<HTMLDivElement>;
  style?: React.CSSProperties;
};

export const SafeHtmlContent: React.FC<SafeHtmlContentProps> = ({ html, onClick, style }) => {
  const rootRef = useRef<HTMLDivElement | null>(null);
  const sanitized = useMemo(() => sanitizeRichHtml(html), [html]);
  const enhanced = useMemo(() => enhanceCodeBlocks(sanitized), [sanitized]);
  const images = useMemo(() => getImageSources(enhanced), [enhanced]);
  const [previewIndex, setPreviewIndex] = useState(-1);

  useEffect(() => {
    const root = rootRef.current;
    if (!root) return;

    const codeBlocks = Array.from(root.querySelectorAll('code[data-code-id]'));
    codeBlocks.forEach((node) => {
      const codeNode = node as HTMLElement;
      const language = normalizeLanguage(codeNode.className, codeNode.textContent || '');
      applyHighlight(codeNode, language);
    });

    const onChange = (event: Event) => {
      const target = event.target as HTMLSelectElement | null;
      if (!target || !target.classList.contains('safe-code-block__lang-select')) return;

      const codeID = String(target.dataset.codeId || '').trim();
      const language = String(target.value || '').trim().toLowerCase();
      if (!codeID || !CODE_LANGUAGE_SET.has(language as CodeLanguage)) return;

      const codeNode = root.querySelector(`code[data-code-id="${codeID}"]`) as HTMLElement | null;
      if (!codeNode) return;

      applyHighlight(codeNode, language as CodeLanguage);
    };

    root.addEventListener('change', onChange);
    return () => root.removeEventListener('change', onChange);
  }, [enhanced]);

  return (
    <>
      <div
        ref={rootRef}
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
        dangerouslySetInnerHTML={{ __html: enhanced || '<span>Нет данных</span>' }}
      />

      {images.length > 0 && (
        <Image.PreviewGroup
          preview={{
            open: previewIndex >= 0,
            current: previewIndex >= 0 ? previewIndex : 0,
            onChange: (current) => {
              setPreviewIndex(current);
            },
            onOpenChange: (open) => {
              if (!open) {
                setPreviewIndex(-1);
              }
            },
          }}
        >
          {images.map((src, index) => (
            <Image
              key={`${src}-${index}`}
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
          border: 1px solid var(--app-color-border);
          border-radius: 8px;
          cursor: zoom-in;
        }
        .safe-code-block {
          margin: 10px 0;
          border: 1px solid var(--app-color-border);
          border-radius: 8px;
          overflow: hidden;
          background: var(--app-bg-container);
        }
        .safe-code-block__header {
          display: flex;
          align-items: center;
          justify-content: space-between;
          gap: 8px;
          padding: 6px 10px;
          border-bottom: 1px solid var(--app-color-divider);
          background: var(--app-color-primary-soft);
        }
        .safe-code-block__title {
          font-size: 12px;
          letter-spacing: 0.04em;
          text-transform: uppercase;
          color: var(--app-color-muted-text);
          font-weight: 600;
        }
        .safe-code-block__lang-wrap {
          display: inline-flex;
          align-items: center;
          gap: 6px;
          color: var(--app-color-muted-text);
          font-size: 12px;
        }
        .safe-code-block__lang-select {
          border: 1px solid var(--app-color-border);
          border-radius: 6px;
          background: var(--app-bg-container);
          color: inherit;
          font-size: 12px;
          padding: 2px 6px;
          cursor: pointer;
        }
        .safe-code-block__lang-select:focus {
          outline: none;
          border-color: var(--app-color-primary);
        }
        .safe-code-block__body {
          margin: 0;
          padding: 10px;
          background: var(--app-bg-container);
          overflow-x: auto;
        }
        .safe-code-block__body code {
          font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
          font-size: 12px;
          line-height: 1.5;
          color: inherit;
          white-space: pre;
        }
        .safe-code-block .hljs-comment,
        .safe-code-block .hljs-quote {
          color: var(--app-color-muted-text);
        }
        .safe-code-block .hljs-keyword,
        .safe-code-block .hljs-selector-tag,
        .safe-code-block .hljs-literal,
        .safe-code-block .hljs-title {
          color: var(--app-color-primary);
        }
        .safe-code-block .hljs-string,
        .safe-code-block .hljs-attr,
        .safe-code-block .hljs-attribute,
        .safe-code-block .hljs-selector-attr,
        .safe-code-block .hljs-selector-class {
          color: #3f8f6b;
        }
        .safe-code-block .hljs-number,
        .safe-code-block .hljs-symbol,
        .safe-code-block .hljs-bullet,
        .safe-code-block .hljs-variable {
          color: #b26c00;
        }
      `}</style>
    </>
  );
};
