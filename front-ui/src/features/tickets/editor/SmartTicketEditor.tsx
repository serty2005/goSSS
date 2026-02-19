import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Input, Modal, Space, Upload, theme as antTheme } from 'antd';
import type { UploadProps } from 'antd';
import { BlockOutlined, BoldOutlined, CodeOutlined, ItalicOutlined, LinkOutlined, PictureOutlined, UserOutlined } from '@ant-design/icons';
import { EditorContent, useEditor } from '@tiptap/react';
import StarterKit from '@tiptap/starter-kit';
import Link from '@tiptap/extension-link';
import Image from '@tiptap/extension-image';
import Placeholder from '@tiptap/extension-placeholder';
import { Extension } from '@tiptap/core';
import { Plugin } from '@tiptap/pm/state';
import { buildMentionHTML, extractMentionQuery, type MentionOption } from '@/features/tickets/editor/mentions';

type UploadRequestOption = Parameters<NonNullable<UploadProps['customRequest']>>[0];

type Props = {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  mentions?: MentionOption[];
  onImageUpload?: (file: File) => Promise<string | null>;
  minHeight?: number;
};

type MentionRange = {
  from: number;
  to: number;
  query: string;
};

const COMMON_FORMAT_TAGS = new Set(['p', 'br', 'strong', 'b', 'em', 'i', 'u', 's', 'a', 'img', 'blockquote', 'ul', 'ol', 'li', 'span', 'div']);
const SPECIAL_HTML_TAGS = new Set(['np', 'center', 'f1', 'qrcode']);

const looksLikeTechnicalMarkup = (value: string) => {
  const source = String(value || '').trim();
  if (!source) return false;

  const tagMatches = Array.from(source.matchAll(/<\/?([a-z][a-z0-9-]*)\b[^>]*>/gi));
  if (tagMatches.length === 0) {
    return /&lt;\/?([a-z][a-z0-9-]*)\b/i.test(source);
  }

  let hasSpecialTag = false;
  let hasCustomTag = false;

  for (const match of tagMatches) {
    const tag = String(match[1] || '').toLowerCase();
    if (!tag) continue;
    if (SPECIAL_HTML_TAGS.has(tag)) {
      hasSpecialTag = true;
      break;
    }
    if (!COMMON_FORMAT_TAGS.has(tag) || /\d/.test(tag) || tag.includes('-')) {
      hasCustomTag = true;
    }
  }

  if (hasSpecialTag) return true;
  if (hasCustomTag && tagMatches.length >= 2) return true;
  return tagMatches.length >= 3 && /\n/.test(source) && source.length > 100;
};

const HTMLCodePaste = Extension.create({
  name: 'htmlCodePaste',
  addProseMirrorPlugins() {
    return [
      new Plugin({
        props: {
          handlePaste: (view, event) => {
            const hasImageFile = Array.from(event.clipboardData?.items || []).some(
              (item) => item.kind === 'file' && item.type.startsWith('image/'),
            );
            if (hasImageFile) {
              return false;
            }
            const html = event.clipboardData?.getData('text/html') || '';
            const text = event.clipboardData?.getData('text/plain') || '';
            const payload = html || text;
            if (!payload || !looksLikeTechnicalMarkup(payload)) {
              return false;
            }

            event.preventDefault();
            const { state } = view;
            const { from, to } = state.selection;
            const block = state.schema.nodes.codeBlock.create(
              null,
              state.schema.text(payload.replace(/\r\n/g, '\n')),
            );
            const tr = state.tr.replaceRangeWith(from, to, block);
            view.dispatch(tr.scrollIntoView());
            return true;
          },
        },
      }),
    ];
  },
});

const SmartTicketEditor: React.FC<Props> = ({
  value,
  onChange,
  placeholder,
  mentions = [],
  onImageUpload,
  minHeight = 120,
}) => {
  const { token } = antTheme.useToken();
  const [mentionRange, setMentionRange] = useState<MentionRange | null>(null);
  const [mentionIndex, setMentionIndex] = useState(0);
  const [isLinkModalOpen, setIsLinkModalOpen] = useState(false);
  const [linkDraft, setLinkDraft] = useState('');
  const [linkLoading, setLinkLoading] = useState(false);
  const mentionRangeRef = useRef<MentionRange | null>(null);
  const mentionItemsRef = useRef<MentionOption[]>([]);

  const extensions = useMemo(
    () => [
      StarterKit.configure({
        heading: false,
        orderedList: false,
        bulletList: false,
        horizontalRule: false,
        link: false,
      }),
      Link.configure({
        openOnClick: false,
        autolink: true,
        protocols: ['http', 'https', 'mailto'],
      }),
      Image.configure({
        inline: false,
      }),
      Placeholder.configure({
        placeholder: placeholder || '',
      }),
      HTMLCodePaste,
    ],
    [placeholder],
  );

  const visibleMentions = useMemo(() => {
    if (!mentionRange) return [];
    const query = mentionRange.query.trim().toLowerCase();
    if (!query) {
      return mentions.slice(0, 8);
    }
    return mentions
      .filter((item) => item.label.toLowerCase().includes(query))
      .slice(0, 8);
  }, [mentionRange, mentions]);

  useEffect(() => {
    mentionRangeRef.current = mentionRange;
  }, [mentionRange]);

  useEffect(() => {
    mentionItemsRef.current = visibleMentions;
    setMentionIndex((prev) => {
      if (visibleMentions.length === 0) return 0;
      return Math.min(prev, visibleMentions.length - 1);
    });
  }, [visibleMentions]);

  const editor = useEditor({
    immediatelyRender: false,
    extensions,
    content: value || '',
    onUpdate: ({ editor: instance }) => {
      onChange(instance.getHTML());
    },
    onSelectionUpdate: ({ editor: instance }) => {
      if (instance.isActive('codeBlock')) {
        setMentionRange(null);
        return;
      }

      const selection = instance.state.selection;
      if (!selection.empty) {
        setMentionRange(null);
        return;
      }

      const to = selection.from;
      const from = Math.max(1, to - 120);
      const before = instance.state.doc.textBetween(from, to, '\n', '\0');
      const query = extractMentionQuery(before);
      if (!query && !before.endsWith('@')) {
        setMentionRange(null);
        return;
      }

      const tokenLength = query.length + 1;
      setMentionRange({
        query,
        from: to - tokenLength,
        to,
      });
    },
    editorProps: {
      attributes: {
        class: 'smart-ticket-editor__content',
      },
      handleKeyDown: (_view, event) => {
        if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
          event.preventDefault();
          setIsLinkModalOpen(true);
          return true;
        }

        const range = mentionRangeRef.current;
        const items = mentionItemsRef.current;
        if (!range || items.length === 0) {
          return false;
        }

        if (event.key === 'ArrowDown') {
          event.preventDefault();
          setMentionIndex((prev) => (prev + 1) % items.length);
          return true;
        }
        if (event.key === 'ArrowUp') {
          event.preventDefault();
          setMentionIndex((prev) => (prev - 1 + items.length) % items.length);
          return true;
        }
        if (event.key === 'Enter' || event.key === 'Tab') {
          event.preventDefault();
          const selected = items[Math.min(mentionIndex, items.length - 1)] || items[0];
          if (selected && editor) {
            editor.chain().focus().insertContentAt({ from: range.from, to: range.to }, buildMentionHTML(selected)).run();
            setMentionRange(null);
            setMentionIndex(0);
          }
          return true;
        }
        if (event.key === 'Escape') {
          event.preventDefault();
          setMentionRange(null);
          setMentionIndex(0);
          return true;
        }

        return false;
      },
      handlePaste: (view, event) => {
        if (!onImageUpload) return false;
        const items = Array.from(event.clipboardData?.items || []);
        const imageItem = items.find((item) => item.kind === 'file' && item.type.startsWith('image/'));
        if (!imageItem) return false;
        const file = imageItem.getAsFile();
        if (!file) return false;

        event.preventDefault();
        onImageUpload(file)
          .then((uploaded) => {
            if (!uploaded) return;
            const { state } = view;
            const node = state.schema.nodes.image.create({ src: uploaded, alt: file.name });
            const tr = state.tr.replaceSelectionWith(node);
            view.dispatch(tr.scrollIntoView());
          })
          .catch(() => null);
        return true;
      },
    },
  });

  useEffect(() => {
    if (!editor) return;
    const current = editor.getHTML();
    if (current === (value || '')) return;
    editor.commands.setContent(value || '', { emitUpdate: false });
  }, [editor, value]);

  const applyLink = useCallback(() => {
    if (!editor) return;
    const href = linkDraft.trim();
    if (!href) {
      editor.chain().focus().unsetLink().run();
      setIsLinkModalOpen(false);
      return;
    }
    editor.chain().focus().extendMarkRange('link').setLink({ href }).run();
    setIsLinkModalOpen(false);
  }, [editor, linkDraft]);

  const openLinkModal = useCallback(async () => {
    setIsLinkModalOpen(true);
    if (!editor) return;

    const current = String(editor.getAttributes('link').href || '').trim();
    if (current) {
      setLinkDraft(current);
      return;
    }

    if (!navigator.clipboard?.readText) {
      setLinkDraft('https://');
      return;
    }

    try {
      setLinkLoading(true);
      const clipboardText = (await navigator.clipboard.readText()).trim();
      if (/^https?:\/\//i.test(clipboardText)) {
        setLinkDraft(clipboardText);
      } else {
        setLinkDraft('https://');
      }
    } catch {
      setLinkDraft('https://');
    } finally {
      setLinkLoading(false);
    }
  }, [editor]);

  const handleImageUpload = useCallback(async (options: UploadRequestOption) => {
    if (!onImageUpload) {
      options.onError?.(new Error('Загрузка изображения недоступна'));
      return;
    }

    try {
      const file = options.file as File;
      const uploaded = await onImageUpload(file);
      if (uploaded && editor) {
        editor.chain().focus().setImage({ src: uploaded, alt: file.name }).run();
      }
      options.onSuccess?.({});
    } catch (error) {
      options.onError?.(error as Error);
    }
  }, [editor, onImageUpload]);

  const applyMention = (item: MentionOption) => {
    if (!editor || !mentionRange) return;
    editor.chain().focus().insertContentAt({ from: mentionRange.from, to: mentionRange.to }, buildMentionHTML(item)).run();
    setMentionRange(null);
    setMentionIndex(0);
  };

  const editorVars: React.CSSProperties = {
    minHeight,
    ['--ste-bg' as string]: token.colorBgContainer,
    ['--ste-toolbar-bg' as string]: token.colorFillAlter,
    ['--ste-border' as string]: token.colorBorder,
    ['--ste-focus' as string]: token.colorPrimary,
    ['--ste-text' as string]: token.colorText,
    ['--ste-text-muted' as string]: token.colorTextSecondary,
    ['--ste-primary-soft' as string]: token.controlItemBgActive,
    ['--ste-shadow' as string]: token.boxShadowSecondary,
  };

  return (
    <div className="smart-ticket-editor" style={editorVars}>
      <div className="smart-ticket-editor__toolbar">
        <Space size={4} wrap>
          <Button
            size="small"
            type={editor?.isActive('bold') ? 'primary' : 'default'}
            icon={<BoldOutlined />}
            onClick={() => editor?.chain().focus().toggleBold().run()}
          />
          <Button
            size="small"
            type={editor?.isActive('italic') ? 'primary' : 'default'}
            icon={<ItalicOutlined />}
            onClick={() => editor?.chain().focus().toggleItalic().run()}
          />
          <Button
            size="small"
            type={editor?.isActive('blockquote') ? 'primary' : 'default'}
            icon={<BlockOutlined />}
            onClick={() => editor?.chain().focus().toggleBlockquote().run()}
          />
          <Button
            size="small"
            type={editor?.isActive('codeBlock') ? 'primary' : 'default'}
            icon={<CodeOutlined />}
            onClick={() => editor?.chain().focus().toggleCodeBlock().run()}
          />
          <Button
            size="small"
            type={editor?.isActive('link') ? 'primary' : 'default'}
            icon={<LinkOutlined />}
            onClick={() => {
              void openLinkModal();
            }}
          />
          {onImageUpload && (
            <Upload showUploadList={false} accept="image/*" customRequest={handleImageUpload}>
              <Button size="small" icon={<PictureOutlined />} />
            </Upload>
          )}
        </Space>
      </div>

      <EditorContent editor={editor} />

      {mentionRange && visibleMentions.length > 0 && (
        <div className="smart-ticket-editor__mentions-popup">
          {visibleMentions.map((item, index) => (
            <button
              key={item.id}
              type="button"
              className={`smart-ticket-editor__mention-item${index === mentionIndex ? ' is-active' : ''}`}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => applyMention(item)}
            >
              <UserOutlined />
              <span>{item.label}</span>
            </button>
          ))}
        </div>
      )}

      <Modal
        open={isLinkModalOpen}
        title="Вставка ссылки"
        okText="Применить"
        cancelText="Отмена"
        confirmLoading={linkLoading}
        onCancel={() => setIsLinkModalOpen(false)}
        onOk={applyLink}
      >
        <Space direction="vertical" size={8} style={{ width: '100%' }}>
          <Input
            value={linkDraft}
            placeholder="https://example.com"
            onChange={(event) => setLinkDraft(event.target.value)}
            onPressEnter={applyLink}
            autoFocus
          />
        </Space>
      </Modal>

      <style>{`
        .smart-ticket-editor {
          position: relative;
          border: 1px solid var(--ste-border);
          border-radius: 8px;
          background: var(--ste-bg);
          color: var(--ste-text);
          overflow: visible;
        }
        .smart-ticket-editor:focus-within {
          border-color: var(--ste-focus);
          box-shadow: inset 0 0 0 1px var(--ste-focus);
        }
        .smart-ticket-editor__toolbar {
          border-bottom: 1px solid var(--ste-border);
          padding: 8px;
          background: var(--ste-toolbar-bg);
          border-radius: 8px 8px 0 0;
        }
        .smart-ticket-editor__content {
          min-height: ${minHeight}px;
          padding: 12px;
          outline: none;
          white-space: pre-wrap;
          word-break: break-word;
          border-radius: 0 0 8px 8px;
        }
        .smart-ticket-editor__content p {
          margin: 0 0 8px;
        }
        .smart-ticket-editor__content p:last-child {
          margin-bottom: 0;
        }
        .smart-ticket-editor__content blockquote {
          margin: 8px 0;
          border-left: 3px solid var(--ste-border);
          padding-left: 10px;
          color: var(--ste-text-muted);
        }
        .smart-ticket-editor__content pre {
          border: 1px solid var(--ste-border);
          border-radius: 6px;
          padding: 10px;
          margin: 8px 0;
          overflow-x: auto;
          font-size: 12px;
          line-height: 1.45;
          background: var(--ste-toolbar-bg);
        }
        .smart-ticket-editor__content img {
          max-width: 100%;
          max-height: 260px;
          object-fit: contain;
          border-radius: 6px;
          border: 1px solid var(--ste-border);
        }
        .smart-ticket-editor__content p.is-editor-empty:first-child::before {
          content: attr(data-placeholder);
          color: var(--ste-text-muted);
          pointer-events: none;
          float: left;
          height: 0;
        }
        .smart-ticket-editor__mentions-popup {
          position: absolute;
          left: 12px;
          right: 12px;
          bottom: 10px;
          z-index: 20;
          background: var(--ste-bg);
          border: 1px solid var(--ste-border);
          border-radius: 8px;
          box-shadow: var(--ste-shadow);
          max-height: 180px;
          overflow: auto;
          padding: 4px;
        }
        .smart-ticket-editor__mention-item {
          width: 100%;
          border: 0;
          background: transparent;
          color: inherit;
          display: flex;
          align-items: center;
          gap: 8px;
          text-align: left;
          padding: 7px 8px;
          border-radius: 6px;
          cursor: pointer;
        }
        .smart-ticket-editor__mention-item:hover,
        .smart-ticket-editor__mention-item.is-active {
          background: var(--ste-primary-soft);
        }
      `}</style>
    </div>
  );
};

export default SmartTicketEditor;
