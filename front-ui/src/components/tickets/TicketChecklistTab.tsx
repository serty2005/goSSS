import React, { useCallback, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Button,
  Checkbox,
  Dropdown,
  Empty,
  Input,
  Modal,
  Popover,
  Progress,
  Select,
  Space,
  Switch,
  Tooltip,
  Typography,
  message,
} from 'antd';
import type { MenuProps } from 'antd';
import {
  CaretDownOutlined,
  CaretRightOutlined,
  CopyOutlined,
  DeleteOutlined,
  EditOutlined,
  HolderOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  MoreOutlined,
  PlusOutlined,
  SnippetsOutlined,
  SubnodeOutlined,
  UserAddOutlined,
} from '@ant-design/icons';
import {
  DndContext,
  type DragEndEvent,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
} from '@dnd-kit/core';
import { SortableContext, arrayMove, useSortable, verticalListSortingStrategy } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import dayjs from 'dayjs';
import { checklistsApi, ticketChecklistQueryKey } from '@/api/checklists';
import type { TicketChecklistItemDTO } from '@/types/api';
import {
  CHECKLIST_MAX_DEPTH,
  type ChecklistMove,
  type ChecklistNode,
  buildChecklistTree,
  collectChecklistStats,
  planChecklistIndent,
  planChecklistInsertAfter,
  planChecklistOutdent,
} from '@/features/tickets/checklist/checklistTree';
import { withApiError } from '@/utils/apiError';

const { Text } = Typography;
const { TextArea } = Input;

type AssigneeOption = { value: number; label: string };

type ComposerState = {
  parentID: string | null;
  afterID?: string;
};

type Props = {
  ticketID: string;
  items: TicketChecklistItemDTO[];
  loading?: boolean;
  assigneeOptions: AssigneeOption[];
};

type RowProps = {
  node: ChecklistNode;
  collapsed: boolean;
  hasHiddenChildren: boolean;
  isEditing: boolean;
  editDraft: string;
  assigneeOptions: AssigneeOption[];
  onToggleCollapse: (id: string) => void;
  onToggleDone: (node: ChecklistNode, done: boolean) => void;
  onStartEdit: (node: ChecklistNode) => void;
  onEditDraftChange: (value: string) => void;
  onSubmitEdit: () => void;
  onCancelEdit: () => void;
  onAssigneesChange: (node: ChecklistNode, ids: number[]) => void;
  buildMenu: (node: ChecklistNode) => MenuProps['items'];
  onMenuClick: (node: ChecklistNode, key: string) => void;
};

const countSubtree = (node: ChecklistNode): { total: number; done: number } =>
  node.children.reduce(
    (acc, child) => {
      const nested = countSubtree(child);
      return {
        total: acc.total + 1 + nested.total,
        done: acc.done + (child.is_done ? 1 : 0) + nested.done,
      };
    },
    { total: 0, done: 0 },
  );

const ChecklistRow: React.FC<RowProps> = ({
  node,
  collapsed,
  hasHiddenChildren,
  isEditing,
  editDraft,
  assigneeOptions,
  onToggleCollapse,
  onToggleDone,
  onStartEdit,
  onEditDraftChange,
  onSubmitEdit,
  onCancelEdit,
  onAssigneesChange,
  buildMenu,
  onMenuClick,
}) => {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({
    id: node.id,
    disabled: isEditing,
  });
  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    paddingLeft: (node.depth - 1) * 24,
    ...(isDragging ? { position: 'relative', zIndex: 3, opacity: 0.85 } : {}),
  };
  const subtree = node.children.length > 0 ? countSubtree(node) : null;
  const doneTooltip = node.is_done
    ? [node.done_by?.name, node.done_at ? dayjs(node.done_at).format('DD.MM.YYYY HH:mm') : ''].filter(Boolean).join(' • ')
    : '';

  return (
    <div ref={setNodeRef} style={style} className={`ticket-checklist__row${node.is_done ? ' is-done' : ''}`}>
      <span
        ref={setActivatorNodeRef}
        className="ticket-checklist__handle"
        aria-label="Перетащить пункт"
        {...attributes}
        {...listeners}
      >
        <HolderOutlined />
      </span>
      <span className="ticket-checklist__caret">
        {node.children.length > 0 || hasHiddenChildren ? (
          <Button
            type="text"
            size="small"
            aria-label={collapsed ? 'Развернуть подпункты' : 'Свернуть подпункты'}
            icon={collapsed ? <CaretRightOutlined /> : <CaretDownOutlined />}
            onClick={() => onToggleCollapse(node.id)}
          />
        ) : null}
      </span>
      <Tooltip title={doneTooltip ? `Выполнено: ${doneTooltip}` : undefined}>
        <Checkbox
          className="ticket-checklist__checkbox"
          checked={node.is_done}
          onChange={(event) => onToggleDone(node, event.target.checked)}
        />
      </Tooltip>
      <div className="ticket-checklist__body">
        {isEditing ? (
          <Input
            size="small"
            autoFocus
            value={editDraft}
            maxLength={1000}
            onChange={(event) => onEditDraftChange(event.target.value)}
            onPressEnter={onSubmitEdit}
            onBlur={onSubmitEdit}
            onKeyDown={(event) => {
              if (event.key === 'Escape') {
                event.preventDefault();
                onCancelEdit();
              }
            }}
          />
        ) : (
          <span className="ticket-checklist__title" onDoubleClick={() => onStartEdit(node)}>
            {node.title}
          </span>
        )}
        {subtree ? (
          <Text type="secondary" className="ticket-checklist__meta">{subtree.done}/{subtree.total}</Text>
        ) : null}
        <Popover
          trigger="click"
          placement="bottomLeft"
          title="Исполнители пункта"
          content={(
            <Select
              mode="multiple"
              allowClear
              showSearch
              optionFilterProp="label"
              style={{ width: 320 }}
              placeholder="Выберите сотрудников"
              options={assigneeOptions}
              value={node.assignees.map((item) => item.id)}
              onChange={(ids) => onAssigneesChange(node, ids as number[])}
            />
          )}
        >
          {node.assignees.length > 0 ? (
            <Text type="secondary" className="ticket-checklist__assignees">
              {node.assignees.map((item) => item.name).join(', ')}
            </Text>
          ) : (
            <Button
              type="text"
              size="small"
              className="ticket-checklist__assign-btn"
              aria-label="Назначить исполнителей"
              icon={<UserAddOutlined />}
            />
          )}
        </Popover>
      </div>
      <Dropdown
        trigger={['click']}
        menu={{ items: buildMenu(node), onClick: ({ key }) => onMenuClick(node, String(key)) }}
      >
        <Button type="text" size="small" className="ticket-checklist__menu-btn" aria-label="Действия с пунктом" icon={<MoreOutlined />} />
      </Dropdown>
    </div>
  );
};

type ComposerProps = {
  depth: number;
  placeholder: string;
  loading: boolean;
  autoFocus?: boolean;
  onSubmit: (value: string) => Promise<boolean>;
  onCancel?: () => void;
};

const ChecklistComposer: React.FC<ComposerProps> = ({ depth, placeholder, loading, autoFocus, onSubmit, onCancel }) => {
  const [draft, setDraft] = useState('');
  const submit = async () => {
    if (!draft.trim() || loading) return;
    const ok = await onSubmit(draft);
    if (ok) setDraft('');
  };
  return (
    <div className="ticket-checklist__composer" style={{ paddingLeft: (depth - 1) * 24 + 52 }}>
      <TextArea
        autoSize={{ minRows: 1, maxRows: 6 }}
        autoFocus={autoFocus}
        value={draft}
        placeholder={placeholder}
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Enter' && !event.shiftKey) {
            event.preventDefault();
            void submit();
          }
          if (event.key === 'Escape' && onCancel) {
            event.preventDefault();
            onCancel();
          }
        }}
      />
      <Space size={4}>
        <Button size="small" type="primary" icon={<PlusOutlined />} loading={loading} disabled={!draft.trim()} onClick={() => void submit()}>
          Добавить
        </Button>
        {onCancel ? <Button size="small" onClick={onCancel}>Отмена</Button> : null}
      </Space>
    </div>
  );
};

const TicketChecklistTab: React.FC<Props> = ({ ticketID, items, loading, assigneeOptions }) => {
  const queryClient = useQueryClient();
  const queryKey = ticketChecklistQueryKey(ticketID);
  const [collapsedIDs, setCollapsedIDs] = useState<Set<string>>(() => new Set());
  const [hideDone, setHideDone] = useState(false);
  const [composer, setComposer] = useState<ComposerState | null>(null);
  const [editingID, setEditingID] = useState('');
  const [editDraft, setEditDraft] = useState('');
  const editingRef = useRef('');

  const tree = useMemo(() => buildChecklistTree(items), [items]);
  const stats = useMemo(() => collectChecklistStats(items), [items]);
  const percent = stats.total > 0 ? Math.round((stats.done / stats.total) * 100) : 0;

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));

  const { data: templatesRes } = useQuery({
    queryKey: ['checklist-templates', 'active'],
    queryFn: () => checklistsApi.listTemplates(),
    staleTime: 60_000,
  });
  const templates = templatesRes?.data || [];

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: ticketChecklistQueryKey(ticketID) }),
    [queryClient, ticketID],
  );

  const createMutation = useMutation({
    mutationFn: (payload: { title: string; parentID: string | null }) =>
      checklistsApi.createItems(ticketID, { title: payload.title, parent_id: payload.parentID }),
    onError: (error) => message.error(withApiError('Не удалось добавить пункт', error)),
  });

  const updateMutation = useMutation({
    mutationFn: (payload: { itemID: string; title?: string; is_done?: boolean; assignee_ids?: number[] }) =>
      checklistsApi.updateItem(ticketID, payload.itemID, {
        title: payload.title,
        is_done: payload.is_done,
        assignee_ids: payload.assignee_ids,
      }),
    onMutate: async (payload) => {
      await queryClient.cancelQueries({ queryKey });
      const previous = queryClient.getQueryData(queryKey);
      queryClient.setQueryData(queryKey, (current: { data?: TicketChecklistItemDTO[] } | undefined) => {
        if (!current?.data) return current;
        return {
          ...current,
          data: current.data.map((item) => {
            if (item.id !== payload.itemID) return item;
            return {
              ...item,
              ...(payload.title !== undefined ? { title: payload.title } : {}),
              ...(payload.is_done !== undefined ? { is_done: payload.is_done } : {}),
              ...(payload.assignee_ids !== undefined
                ? {
                    assignees: payload.assignee_ids.map((id) => ({
                      id,
                      name: assigneeOptions.find((option) => option.value === id)?.label || `#${id}`,
                    })),
                  }
                : {}),
            };
          }),
        };
      });
      return { previous };
    },
    onError: (error, _payload, context) => {
      if (context?.previous) {
        queryClient.setQueryData(queryKey, context.previous);
      }
      message.error(withApiError('Не удалось изменить пункт', error));
    },
    onSettled: () => {
      void invalidate();
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (itemID: string) => checklistsApi.deleteItem(ticketID, itemID),
    onSuccess: () => void invalidate(),
    onError: (error) => message.error(withApiError('Не удалось удалить пункт', error)),
  });

  const reorderMutation = useMutation({
    mutationFn: (move: ChecklistMove) => checklistsApi.reorder(ticketID, move.parentID, move.orderedIDs),
    onSettled: () => void invalidate(),
    onError: (error) => message.error(withApiError('Не удалось переместить пункт', error)),
  });

  const applyTemplateMutation = useMutation({
    mutationFn: (templateID: string) => checklistsApi.applyTemplate(ticketID, templateID),
    onSuccess: (response) => {
      message.success(`Добавлено пунктов из шаблона: ${response.data?.length || 0}`);
      void invalidate();
    },
    onError: (error) => message.error(withApiError('Не удалось применить шаблон', error)),
  });

  const submitComposer = async (value: string, state: ComposerState): Promise<boolean> => {
    try {
      const response = await createMutation.mutateAsync({ title: value, parentID: state.parentID });
      const createdIDs = (response.data || []).map((item) => item.id);
      if (state.afterID && createdIDs.length > 0) {
        const move = planChecklistInsertAfter(tree, state.parentID, state.afterID, createdIDs);
        if (move) {
          await reorderMutation.mutateAsync(move);
        }
      }
      if (state.parentID) {
        setCollapsedIDs((prev) => {
          const next = new Set(prev);
          next.delete(state.parentID as string);
          return next;
        });
      }
      await invalidate();
      return true;
    } catch {
      return false;
    }
  };

  const startEdit = (node: ChecklistNode) => {
    editingRef.current = node.id;
    setEditingID(node.id);
    setEditDraft(node.title);
  };

  const cancelEdit = () => {
    editingRef.current = '';
    setEditingID('');
  };

  const submitEdit = () => {
    const currentID = editingRef.current;
    if (!currentID) return;
    editingRef.current = '';
    const node = items.find((item) => item.id === currentID);
    const value = editDraft.trim();
    setEditingID('');
    if (!node || !value || value === node.title) return;
    updateMutation.mutate({ itemID: node.id, title: value });
  };

  const toggleCollapse = (id: string) => {
    setCollapsedIDs((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const buildMenu = (node: ChecklistNode): MenuProps['items'] => [
    { key: 'add-sibling', icon: <PlusOutlined />, label: 'Добавить пункт' },
    { key: 'add-child', icon: <SubnodeOutlined />, label: 'Добавить подпункт', disabled: node.depth >= CHECKLIST_MAX_DEPTH },
    { key: 'edit', icon: <EditOutlined />, label: 'Редактировать' },
    { type: 'divider' },
    { key: 'indent', icon: <MenuUnfoldOutlined />, label: 'Сделать подпунктом предыдущего', disabled: !planChecklistIndent(tree, node.id) },
    { key: 'outdent', icon: <MenuFoldOutlined />, label: 'Поднять на уровень выше', disabled: !node.parent_id },
    { type: 'divider' },
    { key: 'copy', icon: <CopyOutlined />, label: 'Копировать текст' },
    { key: 'delete', icon: <DeleteOutlined />, label: 'Удалить', danger: true },
  ];

  const handleMenuClick = (node: ChecklistNode, key: string) => {
    switch (key) {
      case 'add-sibling':
        setComposer({ parentID: node.parent_id, afterID: node.id });
        break;
      case 'add-child':
        setCollapsedIDs((prev) => {
          const next = new Set(prev);
          next.delete(node.id);
          return next;
        });
        setComposer({ parentID: node.id });
        break;
      case 'edit':
        startEdit(node);
        break;
      case 'indent': {
        const move = planChecklistIndent(tree, node.id);
        if (move) reorderMutation.mutate(move);
        break;
      }
      case 'outdent': {
        const move = planChecklistOutdent(tree, node.id);
        if (move) reorderMutation.mutate(move);
        break;
      }
      case 'copy':
        void navigator.clipboard.writeText(node.title)
          .then(() => message.success('Текст пункта скопирован'))
          .catch(() => message.error('Не удалось скопировать текст'));
        break;
      case 'delete':
        Modal.confirm({
          title: 'Удалить пункт чеклиста?',
          content: node.children.length > 0 ? 'Вместе с пунктом будут удалены все его подпункты.' : node.title,
          okText: 'Удалить',
          okButtonProps: { danger: true },
          cancelText: 'Отмена',
          onOk: () => deleteMutation.mutateAsync(node.id),
        });
        break;
      default:
        break;
    }
  };

  const handleDragEnd = (siblings: ChecklistNode[], parentID: string | null) => (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const ids = siblings.map((item) => item.id);
    const from = ids.indexOf(String(active.id));
    const to = ids.indexOf(String(over.id));
    if (from < 0 || to < 0) return;
    const orderedIDs = arrayMove(ids, from, to);
    queryClient.setQueryData(queryKey, (current: { data?: TicketChecklistItemDTO[] } | undefined) => {
      if (!current?.data) return current;
      const positions = new Map(orderedIDs.map((id, index) => [id, index]));
      return {
        ...current,
        data: current.data.map((item) => (positions.has(item.id) ? { ...item, position: positions.get(item.id) as number } : item)),
      };
    });
    reorderMutation.mutate({ parentID, orderedIDs });
  };

  const renderComposerFor = (parentID: string | null, afterID: string | undefined, depth: number) => {
    if (!composer || composer.parentID !== parentID || composer.afterID !== afterID) return null;
    return (
      <ChecklistComposer
        key={`composer-${parentID || 'root'}-${afterID || 'end'}`}
        depth={depth}
        autoFocus
        loading={createMutation.isPending || reorderMutation.isPending}
        placeholder={parentID && !afterID ? 'Текст подпункта (каждая строка — отдельный пункт)' : 'Текст пункта (каждая строка — отдельный пункт)'}
        onSubmit={async (value) => {
          const ok = await submitComposer(value, composer);
          if (ok) setComposer(null);
          return ok;
        }}
        onCancel={() => setComposer(null)}
      />
    );
  };

  const renderLevel = (nodes: ChecklistNode[], parentID: string | null): React.ReactNode => {
    const visible = hideDone ? nodes.filter((node) => !node.is_done) : nodes;
    return (
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd(nodes, parentID)}>
        <SortableContext items={visible.map((node) => node.id)} strategy={verticalListSortingStrategy}>
          {visible.map((node) => {
            const collapsed = collapsedIDs.has(node.id);
            return (
              <React.Fragment key={node.id}>
                <ChecklistRow
                  node={node}
                  collapsed={collapsed}
                  hasHiddenChildren={hideDone && node.children.length > 0}
                  isEditing={editingID === node.id}
                  editDraft={editDraft}
                  assigneeOptions={assigneeOptions}
                  onToggleCollapse={toggleCollapse}
                  onToggleDone={(target, done) => updateMutation.mutate({ itemID: target.id, is_done: done })}
                  onStartEdit={startEdit}
                  onEditDraftChange={setEditDraft}
                  onSubmitEdit={submitEdit}
                  onCancelEdit={cancelEdit}
                  onAssigneesChange={(target, ids) => updateMutation.mutate({ itemID: target.id, assignee_ids: ids })}
                  buildMenu={buildMenu}
                  onMenuClick={handleMenuClick}
                />
                {!collapsed && node.children.length > 0 ? renderLevel(node.children, node.id) : null}
                {!collapsed ? renderComposerFor(node.id, undefined, node.depth + 1) : null}
                {renderComposerFor(parentID, node.id, node.depth)}
              </React.Fragment>
            );
          })}
        </SortableContext>
      </DndContext>
    );
  };

  const templateMenu: MenuProps['items'] = templates.length > 0
    ? templates.map((template) => ({ key: template.id, label: template.title, title: template.description || undefined }))
    : [{ key: 'empty', label: 'Шаблонов пока нет', disabled: true }];

  return (
    <div className="ticket-checklist">
      <div className="ticket-checklist__header">
        <Space size={12} wrap>
          <Text strong>Выполнено {stats.done} из {stats.total}</Text>
          {stats.total > 0 ? (
            <Progress percent={percent} size="small" showInfo={false} style={{ width: 160, margin: 0 }} />
          ) : null}
        </Space>
        <Space size={12} wrap>
          <Space size={6}>
            <Switch size="small" checked={hideDone} onChange={setHideDone} />
            <Text type="secondary">Скрыть выполненные</Text>
          </Space>
          <Dropdown
            trigger={['click']}
            menu={{
              items: templateMenu,
              onClick: ({ key }) => {
                const template = templates.find((item) => item.id === key);
                if (!template) return;
                Modal.confirm({
                  title: `Применить шаблон «${template.title}»?`,
                  content: 'Пункты шаблона будут добавлены в конец чеклиста.',
                  okText: 'Применить',
                  cancelText: 'Отмена',
                  onOk: () => applyTemplateMutation.mutateAsync(template.id),
                });
              },
            }}
          >
            <Button size="small" icon={<SnippetsOutlined />} loading={applyTemplateMutation.isPending}>
              Из шаблона
            </Button>
          </Dropdown>
        </Space>
      </div>

      {loading && items.length === 0 ? (
        <Text type="secondary">Загрузка чеклиста...</Text>
      ) : tree.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="В чеклисте пока нет пунктов" />
      ) : (
        <div className="ticket-checklist__list">{renderLevel(tree, null)}</div>
      )}

      <ChecklistComposer
        depth={1}
        loading={createMutation.isPending && composer === null}
        placeholder="Добавить пункт (Enter — сохранить, Shift+Enter — новая строка; каждая строка станет отдельным пунктом)"
        onSubmit={(value) => submitComposer(value, { parentID: null })}
      />

      <style>{`
        .ticket-checklist__header {
          display: flex;
          justify-content: space-between;
          align-items: center;
          gap: 12px;
          flex-wrap: wrap;
          margin-bottom: 8px;
        }
        .ticket-checklist__list {
          display: flex;
          flex-direction: column;
        }
        .ticket-checklist__row {
          display: flex;
          align-items: flex-start;
          gap: 4px;
          min-height: 32px;
          padding-top: 4px;
          padding-bottom: 4px;
          border-radius: 6px;
        }
        .ticket-checklist__row:hover {
          background: rgba(22, 119, 255, 0.06);
        }
        .ticket-checklist__handle {
          width: 16px;
          flex: 0 0 16px;
          display: inline-flex;
          justify-content: center;
          padding-top: 4px;
          cursor: grab;
          opacity: 0;
          color: rgba(128, 128, 128, 0.9);
        }
        .ticket-checklist__row:hover .ticket-checklist__handle,
        .ticket-checklist__handle:focus-visible {
          opacity: 1;
        }
        .ticket-checklist__caret {
          width: 24px;
          flex: 0 0 24px;
        }
        .ticket-checklist__checkbox {
          padding-top: 4px;
        }
        .ticket-checklist__body {
          flex: 1 1 auto;
          min-width: 0;
          display: flex;
          align-items: baseline;
          flex-wrap: wrap;
          column-gap: 8px;
          row-gap: 2px;
          padding-top: 3px;
          padding-left: 4px;
        }
        .ticket-checklist__title {
          word-break: break-word;
          cursor: text;
        }
        .ticket-checklist__row.is-done .ticket-checklist__title {
          text-decoration: line-through;
          opacity: 0.6;
        }
        .ticket-checklist__meta,
        .ticket-checklist__assignees {
          font-size: 12px;
          cursor: pointer;
        }
        .ticket-checklist__assign-btn,
        .ticket-checklist__menu-btn {
          opacity: 0;
        }
        .ticket-checklist__row:hover .ticket-checklist__assign-btn,
        .ticket-checklist__row:hover .ticket-checklist__menu-btn,
        .ticket-checklist__menu-btn.ant-dropdown-open,
        .ticket-checklist__menu-btn:focus-visible,
        .ticket-checklist__assign-btn:focus-visible {
          opacity: 1;
        }
        .ticket-checklist__composer {
          display: flex;
          flex-direction: column;
          gap: 6px;
          padding-top: 6px;
          padding-bottom: 6px;
        }
        @media (hover: none) {
          .ticket-checklist__handle,
          .ticket-checklist__assign-btn,
          .ticket-checklist__menu-btn {
            opacity: 1;
          }
        }
      `}</style>
    </div>
  );
};

export default TicketChecklistTab;
