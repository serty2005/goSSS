import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Button, Checkbox, Popover, Space, Table, Typography } from 'antd';
import { FilterOutlined } from '@ant-design/icons';
import type { CheckboxChangeEvent } from 'antd/es/checkbox';
import type { ColumnType, TableProps } from 'antd/es/table';
import {
  DndContext,
  type DragEndEvent,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
} from '@dnd-kit/core';
import {
  SortableContext,
  arrayMove,
  horizontalListSortingStrategy,
  useSortable,
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { Resizable } from 'react-resizable';
import {
  DATA_TABLE_DEFAULT_MIN_COLUMN_WIDTH,
  formatDataTableText,
  getDataTableTextTitle,
  serializeDataTableLayout,
} from '@/components/common/dataTableUtils';

const { Text } = Typography;

export type DataTableLayoutColumn = {
  key: string;
  width?: number;
};

export type DataTableSortOrder = 'asc' | 'desc';

export type DataTableSortState = {
  key: string;
  order: DataTableSortOrder;
} | null;

export type DataTableColumn<T extends object> = ColumnType<T> & {
  key: string;
  width: number;
  minWidth?: number;
  maxWidth?: number;
  menuTitle?: React.ReactNode;
  isSortable?: boolean;
  sortValue?: (record: T) => string | number | boolean | null | undefined;
  filterContent?: React.ReactNode;
  isFilterActive?: boolean;
  autoFormatText?: boolean;
};

interface DataTableProps<T extends object> {
  columns: DataTableColumn<T>[];
  dataSource: T[];
  rowKey: TableProps<T>['rowKey'];
  loading?: TableProps<T>['loading'];
  pagination?: TableProps<T>['pagination'];
  size?: TableProps<T>['size'];
  bordered?: boolean;
  tableLayout?: TableProps<T>['tableLayout'];
  rowClassName?: TableProps<T>['rowClassName'];
  onRow?: TableProps<T>['onRow'];
  className?: string;
  emptyText?: string;
  sortableColumnKeys?: string[];
  sortState?: DataTableSortState;
  onSortChange?: (columnKey: string) => void;
  defaultSortState?: DataTableSortState;
  visibleColumnKeys?: string[];
  defaultVisibleColumnKeys?: string[];
  availableColumnKeys?: string[];
  onVisibleColumnKeysChange?: (keys: string[]) => void;
  visibilityStorageKey?: string;
  enableColumnMenu?: boolean;
  layoutColumns?: DataTableLayoutColumn[];
  layoutKey?: string;
  layoutStorage?: 'local' | 'none';
  onLayoutChange?: (columns: DataTableLayoutColumn[]) => void;
  footer?: React.ReactNode;
}

type HeaderCellProps = React.HTMLAttributes<HTMLTableCellElement> & {
  id?: string;
  width?: number;
  minWidth?: number;
  isDragDisabled?: boolean;
  isSortable?: boolean;
  sortOrder?: DataTableSortOrder | null;
  onSort?: () => void;
  onResize?: (
    event: React.SyntheticEvent,
    data: { size: { width: number; height: number } },
  ) => void;
  onResizeStart?: () => void;
  onResizeStop?: () => void;
  isResizing?: boolean;
  filterContent?: React.ReactNode;
  isFilterActive?: boolean;
};

export const DataTableTextCell: React.FC<{
  value?: unknown;
  fallback?: React.ReactNode;
  className?: string;
  multiline?: boolean;
  secondary?: boolean;
}> = ({ value, fallback = '-', className, multiline, secondary }) => {
  const text = formatDataTableText(value);
  const content = text || fallback;
  const classNames = [
    multiline ? 'tickets-table-multiline-cell' : 'data-table__cell-ellipsis company-ticket-table__cell-ellipsis',
    secondary ? 'tickets-table-multiline-cell-secondary' : '',
    className,
  ].filter(Boolean).join(' ');

  return (
    <div className={classNames} title={getDataTableTextTitle(content, text)}>
      {content}
    </div>
  );
};

const compareValues = (
  left?: string | number | boolean | null,
  right?: string | number | boolean | null,
) => {
  if (typeof left === 'number' && typeof right === 'number') {
    return left - right;
  }
  return String(left ?? '').localeCompare(String(right ?? ''), 'ru', {
    numeric: true,
    sensitivity: 'base',
  });
};

const getRecordValue = <T extends object>(record: T, dataIndex?: ColumnType<T>['dataIndex']) => {
  if (dataIndex === undefined || dataIndex === null) {
    return undefined;
  }
  const path = Array.isArray(dataIndex) ? dataIndex : [dataIndex];
  return path.reduce<unknown>((current, key) => {
    if (current && typeof current === 'object') {
      return (current as Record<string, unknown>)[String(key)];
    }
    return undefined;
  }, record);
};

const clampColumnWidth = <T extends object>(column: DataTableColumn<T>, width?: number) => {
  const currentWidth = Number(width ?? column.width ?? column.minWidth);
  const minWidth = column.minWidth || DATA_TABLE_DEFAULT_MIN_COLUMN_WIDTH;
  const maxWidth = column.maxWidth;
  const bounded = Math.max(Number.isFinite(currentWidth) ? currentWidth : minWidth, minWidth);
  return typeof maxWidth === 'number' ? Math.min(bounded, maxWidth) : bounded;
};

const applyStoredColumnLayout = <T extends object>(
  baseColumns: DataTableColumn<T>[],
  storedColumns?: DataTableLayoutColumn[],
) => {
  if (!Array.isArray(storedColumns) || storedColumns.length === 0) {
    return baseColumns;
  }

  const baseByKey = new Map(baseColumns.map((column) => [column.key, column]));
  const next: DataTableColumn<T>[] = [];
  const seen = new Set<string>();

  storedColumns.forEach((storedColumn) => {
    const baseColumn = baseByKey.get(String(storedColumn.key || ''));
    if (!baseColumn) {
      return;
    }
    next.push({
      ...baseColumn,
      width: clampColumnWidth(baseColumn, storedColumn.width),
    });
    seen.add(baseColumn.key);
  });

  baseColumns.forEach((column) => {
    if (!seen.has(column.key)) {
      next.push(column);
    }
  });

  return next.length ? next : baseColumns;
};

const readStoredLayout = (layoutKey?: string): DataTableLayoutColumn[] | undefined => {
  if (!layoutKey) {
    return undefined;
  }
  try {
    const raw = window.localStorage.getItem(layoutKey);
    return raw ? JSON.parse(raw) : undefined;
  } catch {
    return undefined;
  }
};

const readStoredVisibleKeys = (storageKey?: string) => {
  if (!storageKey) {
    return undefined;
  }
  try {
    const raw = window.localStorage.getItem(storageKey);
    const parsed = raw ? JSON.parse(raw) : undefined;
    return Array.isArray(parsed) ? parsed.map((item) => String(item)).filter(Boolean) : undefined;
  } catch {
    return undefined;
  }
};

const writeStoredVisibleKeys = (storageKey: string | undefined, keys: string[]) => {
  if (!storageKey) {
    return;
  }
  try {
    window.localStorage.setItem(storageKey, JSON.stringify(keys));
  } catch {
    // localStorage может быть недоступен в приватном режиме.
  }
};

const ResizableHeaderCell = React.forwardRef<HTMLTableCellElement, HeaderCellProps>(
  (props, ref) => {
    const {
      onResize,
      onResizeStart,
      onResizeStop,
      width,
      minWidth,
      children,
      ...rest
    } = props;

    if (!width || !onResize) {
      return (
        <th ref={ref} {...rest}>
          {children}
        </th>
      );
    }

    return (
      <Resizable
        width={width}
        height={0}
        handle={
          <span
            className="resize-handle"
            onMouseDown={(event) => event.stopPropagation()}
            onTouchStart={(event) => event.stopPropagation()}
          />
        }
        onResize={onResize}
        onResizeStart={onResizeStart}
        onResizeStop={onResizeStop}
        minConstraints={[minWidth || DATA_TABLE_DEFAULT_MIN_COLUMN_WIDTH, 0]}
        draggableOpts={{ enableUserSelectHack: false }}
      >
        <th ref={ref} {...rest}>
          {children}
        </th>
      </Resizable>
    );
  },
);

ResizableHeaderCell.displayName = 'ResizableHeaderCell';

const DraggableHeaderCell: React.FC<HeaderCellProps> = ({
  id,
  style,
  isResizing,
  isDragDisabled,
  isSortable,
  sortOrder,
  onSort,
  filterContent,
  isFilterActive,
  children,
  className,
  ...rest
}) => {
  const sortableDisabled = Boolean(isResizing || isDragDisabled || !id);
  const {
    attributes,
    listeners,
    setActivatorNodeRef,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: id || '', disabled: sortableDisabled });

  const mergedStyle: React.CSSProperties = {
    ...style,
    transform: CSS.Transform.toString(transform),
    transition,
    ...(isDragging ? { position: 'relative', zIndex: 2 } : {}),
  };
  const mergedClassName = [
    className,
    isSortable ? 'data-table__header-cell--sortable company-ticket-table__header-cell--sortable' : '',
    sortOrder ? 'data-table__header-cell--sorted company-ticket-table__header-cell--sorted' : '',
  ].filter(Boolean).join(' ');

  return (
    <ResizableHeaderCell
      ref={setNodeRef}
      style={mergedStyle}
      className={mergedClassName}
      aria-sort={
        sortOrder === 'asc' ? 'ascending' : sortOrder === 'desc' ? 'descending' : undefined
      }
      {...attributes}
      {...rest}
    >
      <div
        ref={setActivatorNodeRef}
        className="tickets-table-header data-table__header company-ticket-table__header"
        title={isSortable ? 'Клик - сортировка, потянуть - переместить столбец' : undefined}
        onClick={(event) => {
          event.stopPropagation();
          onSort?.();
        }}
        onMouseDown={(event) => event.stopPropagation()}
        onTouchStart={(event) => event.stopPropagation()}
        {...(!sortableDisabled ? listeners : {})}
      >
        <span className="tickets-table-header-title data-table__header-title company-ticket-table__header-title">
          {children}
        </span>
        {sortOrder && (
          <span className="data-table__sort-marker company-ticket-table__sort-marker" aria-hidden="true">
            {sortOrder === 'asc' ? '↑' : '↓'}
          </span>
        )}
        {filterContent && (
          <Popover
            trigger="click"
            placement="bottomRight"
            content={filterContent}
          >
            <Button
              type={isFilterActive ? 'primary' : 'text'}
              size="small"
              icon={<FilterOutlined />}
              className="data-table__filter-button company-ticket-table__filter-button"
              aria-label="Фильтр столбца"
              onClick={(event) => event.stopPropagation()}
              onMouseDown={(event) => event.stopPropagation()}
              onTouchStart={(event) => event.stopPropagation()}
            />
          </Popover>
        )}
      </div>
    </ResizableHeaderCell>
  );
};

const DataTable = <T extends object>({
  columns: baseColumns,
  dataSource,
  rowKey,
  loading,
  pagination = false,
  size = 'small',
  bordered = true,
  tableLayout = 'fixed',
  rowClassName,
  onRow,
  className,
  emptyText = 'Данные не найдены',
  sortableColumnKeys,
  sortState,
  onSortChange,
  defaultSortState = null,
  visibleColumnKeys,
  defaultVisibleColumnKeys,
  availableColumnKeys,
  onVisibleColumnKeysChange,
  visibilityStorageKey,
  enableColumnMenu = true,
  layoutColumns,
  layoutKey,
  layoutStorage = 'none',
  onLayoutChange,
  footer,
}: DataTableProps<T>) => {
  const tableContainerRef = useRef<HTMLDivElement | null>(null);
  const columnsStateRef = useRef<DataTableColumn<T>[]>([]);
  const isResizeActiveRef = useRef(false);
  const suppressHeaderClickRef = useRef(false);
  const [isResizingColumn, setIsResizingColumn] = useState(false);
  const [tableContainerWidth, setTableContainerWidth] = useState(0);
  const [innerSort, setInnerSort] = useState<DataTableSortState>(defaultSortState);
  const [columnsMenu, setColumnsMenu] = useState<{ open: boolean; x: number; y: number }>({
    open: false,
    x: 0,
    y: 0,
  });
  const [innerVisibleColumnKeys, setInnerVisibleColumnKeys] = useState<string[] | undefined>(() =>
    readStoredVisibleKeys(visibilityStorageKey) || defaultVisibleColumnKeys,
  );
  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 6 },
    }),
  );

  useLayoutEffect(() => {
    const node = tableContainerRef.current;
    if (!node) {
      return;
    }

    const updateWidth = () => {
      setTableContainerWidth(Math.floor(node.getBoundingClientRect().width));
    };
    updateWidth();

    const observer = new ResizeObserver(updateWidth);
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (visibleColumnKeys) {
      return;
    }
    setInnerVisibleColumnKeys(readStoredVisibleKeys(visibilityStorageKey) || defaultVisibleColumnKeys);
  }, [defaultVisibleColumnKeys, visibilityStorageKey, visibleColumnKeys]);

  const [columnsState, setColumnsState] = useState<DataTableColumn<T>[]>(baseColumns);

  useEffect(() => {
    const storedColumns = layoutStorage === 'local' && !layoutColumns
      ? readStoredLayout(layoutKey)
      : layoutColumns;
    const nextColumns = applyStoredColumnLayout(baseColumns, storedColumns);
    columnsStateRef.current = nextColumns;
    setColumnsState(nextColumns);
  }, [baseColumns, layoutColumns, layoutKey, layoutStorage]);

  const saveColumnsLayout = useCallback((nextColumns: DataTableColumn<T>[]) => {
    if (nextColumns.length === 0) {
      return;
    }

    const nextLayoutColumns = nextColumns.map((column) => ({
      key: column.key,
      width: column.width,
    }));

    if (layoutStorage === 'local' && layoutKey) {
      window.localStorage.setItem(layoutKey, serializeDataTableLayout(nextLayoutColumns));
    }
    onLayoutChange?.(nextLayoutColumns);
  }, [layoutKey, layoutStorage, onLayoutChange]);

  const handleResize = useCallback(
    (stateIndex: number) =>
      (_event: React.SyntheticEvent, data: { size: { width: number } }) => {
        if (stateIndex < 0) {
          return;
        }
        setColumnsState((currentColumns) => {
          const nextColumns = [...currentColumns];
          const currentColumn = nextColumns[stateIndex];
          if (!currentColumn) {
            return currentColumns;
          }
          nextColumns[stateIndex] = {
            ...currentColumn,
            width: clampColumnWidth(currentColumn, data.size.width),
          };
          columnsStateRef.current = nextColumns;
          return nextColumns;
        });
      },
    [],
  );

  const handleResizeStop = useCallback(() => {
    setIsResizingColumn(false);
    if (!isResizeActiveRef.current) {
      return;
    }
    isResizeActiveRef.current = false;
    saveColumnsLayout(columnsStateRef.current);
  }, [saveColumnsLayout]);

  const handleDragEnd = useCallback(({ active, over }: DragEndEvent) => {
    window.setTimeout(() => {
      suppressHeaderClickRef.current = false;
    }, 0);

    if (!over || active.id === over.id) {
      return;
    }

    const activeID = String(active.id);
    const overID = String(over.id);
    setColumnsState((currentColumns) => {
      const oldIndex = currentColumns.findIndex((column) => column.key === activeID);
      const newIndex = currentColumns.findIndex((column) => column.key === overID);
      if (oldIndex === -1 || newIndex === -1) {
        return currentColumns;
      }
      const nextColumns = arrayMove(currentColumns, oldIndex, newIndex);
      columnsStateRef.current = nextColumns;
      saveColumnsLayout(nextColumns);
      return nextColumns;
    });
  }, [saveColumnsLayout]);

  const handleDragStart = useCallback(() => {
    suppressHeaderClickRef.current = true;
  }, []);

  const handleDragCancel = useCallback(() => {
    window.setTimeout(() => {
      suppressHeaderClickRef.current = false;
    }, 0);
  }, []);

  const effectiveSort = sortState !== undefined ? sortState : innerSort;

  const toggleSort = useCallback((columnKey: string) => {
    if (isResizingColumn || suppressHeaderClickRef.current) {
      return;
    }

    if (onSortChange) {
      onSortChange(columnKey);
      return;
    }

    setInnerSort((currentSort) => {
      if (currentSort?.key !== columnKey) {
        return { key: columnKey, order: 'asc' };
      }
      if (currentSort.order === 'asc') {
        return { key: columnKey, order: 'desc' };
      }
      return null;
    });
  }, [isResizingColumn, onSortChange]);

  const visibleKeys = visibleColumnKeys || innerVisibleColumnKeys;
  const availableColumnSet = useMemo(
    () => new Set(availableColumnKeys || columnsState.map((column) => column.key)),
    [availableColumnKeys, columnsState],
  );
  const columnMenuRows = useMemo(
    () => columnsState.filter((column) => availableColumnSet.has(column.key)),
    [availableColumnSet, columnsState],
  );
  const currentVisibleColumnKeys = useMemo(
    () => visibleKeys || columnMenuRows.map((column) => column.key),
    [columnMenuRows, visibleKeys],
  );

  const toggleColumnVisibility = useCallback((columnKey: string, checked: boolean) => {
    const next = checked
      ? [...currentVisibleColumnKeys, columnKey]
      : currentVisibleColumnKeys.filter((key) => key !== columnKey);
    const orderSource = columnMenuRows.map((column) => column.key);
    const ordered = orderSource
      .filter((key) => availableColumnSet.has(key) && next.includes(key));

    if (onVisibleColumnKeysChange) {
      onVisibleColumnKeysChange(ordered);
      return;
    }
    setInnerVisibleColumnKeys(ordered);
    writeStoredVisibleKeys(visibilityStorageKey, ordered);
  }, [
    availableColumnSet,
    columnMenuRows,
    currentVisibleColumnKeys,
    onVisibleColumnKeysChange,
    visibilityStorageKey,
  ]);

  const normalizedColumns = useMemo<ColumnType<T>[]>(() => (
    columnsState
      .filter((column) => {
        if (!visibleKeys) {
          return true;
        }
        return visibleKeys.includes(column.key);
      })
      .map((column) => {
        const stateIndex = columnsState.findIndex((item) => item.key === column.key);
        const isSortable = sortableColumnKeys
          ? sortableColumnKeys.includes(column.key)
          : column.isSortable !== false;
        const render = column.render || (column.autoFormatText === false
          ? undefined
          : (value: unknown) => <DataTableTextCell value={value} />);
        return {
          ...column,
          render,
          onHeaderCell: () => ({
            id: column.key,
            width: column.width,
            minWidth: column.minWidth,
            isSortable,
            sortOrder: isSortable && effectiveSort?.key === column.key ? effectiveSort.order : null,
            onSort: isSortable ? () => toggleSort(column.key) : undefined,
            filterContent: column.filterContent,
            isFilterActive: Boolean(column.isFilterActive),
            onResize: handleResize(stateIndex),
            onResizeStart: () => {
              isResizeActiveRef.current = true;
              setIsResizingColumn(true);
            },
            onResizeStop: handleResizeStop,
            isResizing: isResizingColumn,
          }),
        };
      })
  ), [
    columnsState,
    effectiveSort,
    handleResize,
    handleResizeStop,
    isResizingColumn,
    sortableColumnKeys,
    toggleSort,
    visibleKeys,
  ]);

  const sortedRows = useMemo(() => {
    if (!effectiveSort) {
      return dataSource;
    }
    const sortColumn = columnsState.find((column) => column.key === effectiveSort.key);
    if (!sortColumn) {
      return dataSource;
    }

    return [...dataSource].sort((left, right) => {
      const leftValue = sortColumn.sortValue
        ? sortColumn.sortValue(left)
        : getRecordValue(left, sortColumn.dataIndex);
      const rightValue = sortColumn.sortValue
        ? sortColumn.sortValue(right)
        : getRecordValue(right, sortColumn.dataIndex);
      const result = compareValues(
        typeof leftValue === 'boolean' ? Number(leftValue) : leftValue as string | number | null | undefined,
        typeof rightValue === 'boolean' ? Number(rightValue) : rightValue as string | number | null | undefined,
      );
      return effectiveSort.order === 'asc' ? result : -result;
    });
  }, [columnsState, dataSource, effectiveSort]);

  const tableScrollX = useMemo(() => {
    return normalizedColumns.reduce((sum, column) => {
      const width = Number(column.width ?? (column as { minWidth?: number }).minWidth ?? DATA_TABLE_DEFAULT_MIN_COLUMN_WIDTH);
      return sum + (Number.isFinite(width) ? width : DATA_TABLE_DEFAULT_MIN_COLUMN_WIDTH);
    }, 0);
  }, [normalizedColumns]);
  const shouldShowHorizontalScroll = !tableContainerWidth || tableScrollX > tableContainerWidth + 1;

  const closeColumnsMenu = useCallback(() => {
    setColumnsMenu((current) => ({ ...current, open: false }));
  }, []);

  useEffect(() => {
    if (!columnsMenu.open) {
      return;
    }
    const close = () => closeColumnsMenu();
    window.addEventListener('click', close);
    window.addEventListener('scroll', close, true);
    return () => {
      window.removeEventListener('click', close);
      window.removeEventListener('scroll', close, true);
    };
  }, [closeColumnsMenu, columnsMenu.open]);

  const canOpenColumnsMenu = enableColumnMenu && columnMenuRows.length > 0;
  const mergedClassName = [
    'tickets-table data-table company-ticket-table',
    shouldShowHorizontalScroll ? '' : 'data-table--no-horizontal-scroll company-ticket-table--no-horizontal-scroll',
    className,
  ].filter(Boolean).join(' ');

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCenter}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
      onDragCancel={handleDragCancel}
    >
      <SortableContext
        items={normalizedColumns.map((column) => String(column.key))}
        strategy={horizontalListSortingStrategy}
      >
        <div ref={tableContainerRef} className="data-table__scroll-frame company-ticket-table__scroll-frame">
          <Table<T>
            dataSource={sortedRows}
            columns={normalizedColumns}
            rowKey={rowKey}
            loading={loading}
            pagination={pagination}
            size={size}
            bordered={bordered}
            className={mergedClassName}
            tableLayout={tableLayout}
            scroll={{ x: tableScrollX }}
            components={{
              header: {
                cell: DraggableHeaderCell,
              },
            }}
            locale={{ emptyText }}
            rowClassName={rowClassName}
            onHeaderRow={() => ({
              onContextMenu: (event) => {
                if (!canOpenColumnsMenu) {
                  return;
                }
                event.preventDefault();
                event.stopPropagation();
                setColumnsMenu({
                  open: true,
                  x: event.clientX,
                  y: event.clientY,
                });
              },
            })}
            onRow={onRow}
          />
        </div>
        {columnsMenu.open && createPortal(
          <div
            className="data-table__columns-menu company-ticket-table__columns-menu"
            style={{ left: columnsMenu.x, top: columnsMenu.y }}
            onClick={(event) => event.stopPropagation()}
            onContextMenu={(event) => event.preventDefault()}
          >
            <Text strong>Столбцы</Text>
            <Space direction="vertical" size={4} style={{ width: '100%', marginTop: 8 }}>
              {columnMenuRows.map((column) => (
                <Checkbox
                  key={column.key}
                  checked={currentVisibleColumnKeys.includes(column.key)}
                  onChange={(event: CheckboxChangeEvent) => toggleColumnVisibility(column.key, event.target.checked)}
                >
                  {column.menuTitle || column.title as React.ReactNode}
                </Checkbox>
              ))}
            </Space>
          </div>,
          document.body,
        )}
        {footer}
      </SortableContext>
    </DndContext>
  );
};

export default DataTable;
