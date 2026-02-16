import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Button, Dropdown, Space, Upload } from 'antd';
import type { UploadProps } from 'antd';
import { BoldOutlined, ItalicOutlined, StrikethroughOutlined, LinkOutlined, BlockOutlined, UserOutlined, PictureOutlined } from '@ant-design/icons';
import { insertHTMLAtSelection, insertLinkAtSelection, insertQuoteBlock, wrapSelectionWithTag } from '@/features/tickets/editor/formatting';
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

const SmartTicketEditor: React.FC<Props> = ({
  value,
  onChange,
  placeholder,
  mentions = [],
  onImageUpload,
  minHeight = 120,
}) => {
  const rootRef = useRef<HTMLDivElement | null>(null);
  const [linkDraft, setLinkDraft] = useState('');
  const [mentionQuery, setMentionQuery] = useState('');
  const mentionRangeRef = useRef<Range | null>(null);

  useEffect(() => {
    if (!rootRef.current) return;
    if (rootRef.current.innerHTML === value) return;
    rootRef.current.innerHTML = value || '';
  }, [value]);

  const syncValue = () => {
    if (!rootRef.current) return;
    onChange(rootRef.current.innerHTML);
  };

  const updateMentionState = () => {
    const selection = window.getSelection();
    if (!selection || selection.rangeCount === 0) {
      setMentionQuery('');
      mentionRangeRef.current = null;
      return;
    }
    const range = selection.getRangeAt(0);
    if (!range.collapsed || !selection.focusNode || selection.focusNode.nodeType !== Node.TEXT_NODE) {
      setMentionQuery('');
      mentionRangeRef.current = null;
      return;
    }

    const node = selection.focusNode as Text;
    const before = node.data.slice(0, selection.focusOffset);
    const query = extractMentionQuery(before);
    if (!query && !before.endsWith('@')) {
      setMentionQuery('');
      mentionRangeRef.current = null;
      return;
    }

    const tokenLength = query.length + 1;
    const mentionRange = range.cloneRange();
    mentionRange.setStart(node, Math.max(0, selection.focusOffset - tokenLength));
    mentionRange.setEnd(node, selection.focusOffset);
    mentionRangeRef.current = mentionRange;
    setMentionQuery(query);
  };

  const visibleMentions = useMemo(() => {
    if (!mentionQuery && mentionQuery !== '') return [];
    const normalized = mentionQuery.trim().toLowerCase();
    const filtered = mentions.filter((item) => item.label.toLowerCase().includes(normalized));
    return filtered.slice(0, 8);
  }, [mentionQuery, mentions]);

  const applyMention = (option: MentionOption) => {
    const selection = window.getSelection();
    if (!selection || !mentionRangeRef.current) {
      return;
    }
    selection.removeAllRanges();
    selection.addRange(mentionRangeRef.current);
    const html = `${buildMentionHTML(option)}&nbsp;`;
    insertHTMLAtSelection(html);
    syncValue();
    setMentionQuery('');
    mentionRangeRef.current = null;
  };

  const handleImageUpload = async (options: UploadRequestOption) => {
    if (!onImageUpload) {
      options.onError?.(new Error('Загрузка изображения недоступна'));
      return;
    }
    const file = options.file as File;
    try {
      const uploaded = await onImageUpload(file);
      if (uploaded) {
        insertHTMLAtSelection(`<img src="${uploaded}" alt="${file.name}" />`);
        syncValue();
      }
      options.onSuccess?.({});
    } catch (error) {
      options.onError?.(error as Error);
    }
  };

  const handlePaste: React.ClipboardEventHandler<HTMLDivElement> = async (event) => {
    if (!onImageUpload) return;
    const items = Array.from(event.clipboardData.items || []);
    const imageItem = items.find((item) => item.kind === 'file' && item.type.startsWith('image/'));
    if (!imageItem) return;
    const file = imageItem.getAsFile();
    if (!file) return;
    event.preventDefault();
    const uploaded = await onImageUpload(file);
    if (uploaded) {
      insertHTMLAtSelection(`<img src="${uploaded}" alt="${file.name}" />`);
      syncValue();
    }
  };

  return (
    <div>
      <Space wrap style={{ marginBottom: 8 }}>
        <Button icon={<BoldOutlined />} onClick={() => { wrapSelectionWithTag('b'); syncValue(); }} />
        <Button icon={<ItalicOutlined />} onClick={() => { wrapSelectionWithTag('i'); syncValue(); }} />
        <Button icon={<StrikethroughOutlined />} onClick={() => { wrapSelectionWithTag('s'); syncValue(); }} />
        <Button icon={<BlockOutlined />} onClick={() => { insertQuoteBlock(); syncValue(); }} />
        <Dropdown
          trigger={['click']}
          menu={{
            items: [{ key: 'link-input', label: (
              <Space>
                <input
                  value={linkDraft}
                  onChange={(event) => setLinkDraft(event.target.value)}
                  placeholder="https://example.com"
                  style={{ width: 220 }}
                />
                <Button
                  type="primary"
                  size="small"
                  onClick={() => {
                    insertLinkAtSelection(linkDraft);
                    setLinkDraft('');
                    syncValue();
                  }}
                >
                  Вставить
                </Button>
              </Space>
            ) }],
          }}
        >
          <Button icon={<LinkOutlined />} />
        </Dropdown>
        <Upload showUploadList={false} accept="image/*" customRequest={handleImageUpload}>
          <Button icon={<PictureOutlined />}>Изображение</Button>
        </Upload>
      </Space>

      <div
        ref={rootRef}
        contentEditable
        suppressContentEditableWarning
        onInput={syncValue}
        onKeyUp={updateMentionState}
        onClick={updateMentionState}
        onPaste={handlePaste}
        style={{
          minHeight,
          border: '1px solid #d9d9d9',
          borderRadius: 8,
          padding: 12,
          outline: 'none',
        }}
        data-placeholder={placeholder || ''}
      />

      {visibleMentions.length > 0 && (
        <div style={{ marginTop: 8, border: '1px solid #f0f0f0', borderRadius: 8, padding: 8 }}>
          <Space wrap>
            {visibleMentions.map((item) => (
              <Button key={item.id} icon={<UserOutlined />} size="small" onClick={() => applyMention(item)}>
                {item.label}
              </Button>
            ))}
          </Space>
        </div>
      )}

      <style>{`
        [contenteditable="true"]:empty:before {
          content: attr(data-placeholder);
          color: #bfbfbf;
        }
        [contenteditable="true"] img {
          max-width: 240px;
          max-height: 180px;
          object-fit: contain;
          border-radius: 6px;
          border: 1px solid #f0f0f0;
        }
      `}</style>
    </div>
  );
};

export default SmartTicketEditor;
