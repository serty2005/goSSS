import React, { Suspense, useEffect, useMemo, useRef, useState } from "react";
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import {
  Button,
  Card,
  Col,
  Drawer,
  DatePicker,
  Input,
  List,
  Popconfirm,
  Popover,
  Row,
  Select,
  Space,
  Spin,
  Table,
  Checkbox,
  Tag,
  Tooltip,
  Typography,
  message,
  theme as antTheme,
} from "antd";
import { LinkOutlined, MenuOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import {
  DndContext,
  DragEndEvent,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  horizontalListSortingStrategy,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Resizable } from "react-resizable";
import { Link, useNavigate } from "react-router-dom";
import dayjs from "dayjs";
import { ticketsApi } from "@/api/tickets";
import { companiesApi } from "@/api/companies";
import { usersApi } from "@/api/users";
import { profileApi } from "@/api/profile";
import { useLayoutHeader } from "@/components/layout/LayoutHeaderContext";
import TelephonyLineIndicator from "@/components/telephony/TelephonyLineIndicator";
import { TicketDetailsDTO, TicketStatus } from "@/types/api";
import SmartTicketEditor from "@/features/tickets/editor/SmartTicketEditor";
import { hasEditorContent } from "@/features/tickets/editor/content";
import type { MentionOption } from "@/features/tickets/editor/mentions";
import { useAuthStore } from "@/store/authStore";
import { useTicketParamsStore } from "@/store/ticketParamsStore";
import { SafeHtmlContent } from "@/utils/safeHtml";
import {
  getTicketStatusMeta,
  isClosedLikeTicketStatus,
  TICKET_ACTIVE_STATUS_VALUES,
  TICKET_STATUS_OPTIONS,
} from "@/constants/ticketStatus";

const { Text, Paragraph } = Typography;
const LazyNewTicketModal = React.lazy(
  () => import("@/components/tickets/NewTicketModal"),
);

type ViewMode = "list" | "cards" | "table";

type HeaderCellProps = React.HTMLAttributes<HTMLTableCellElement> & {
  id?: string;
  width?: number;
  minWidth?: number;
  isDragDisabled?: boolean;
  onResize?: (
    event: React.SyntheticEvent,
    data: { size: { width: number; height: number } },
  ) => void;
  onResizeStart?: () => void;
  onResizeStop?: () => void;
  isResizing?: boolean;
};

const normalizeDescription = (value?: string) => {
  const normalized = normalizeDescriptionMultiline(value);
  return normalized.replace(/\s+/g, " ").trim();
};

const normalizeDescriptionMultiline = (value?: string) => {
  if (!value) return "";
  return value
    .replace(/<\s*br\s*\/?>/gi, "\n")
    .replace(/<\/p>\s*<p>/gi, "\n")
    .replace(/<\/?p[^>]*>/gi, "\n")
    .replace(/<[^>]*>/g, " ")
    .replace(/&nbsp;/gi, " ")
    .replace(/&amp;/gi, "&")
    .replace(/&lt;/gi, "<")
    .replace(/&gt;/gi, ">")
    .replace(/&quot;/gi, '"')
    .replace(/&#39;/gi, "'")
    .replace(/\r/g, "")
    .replace(/[ \t\f\v]+/g, " ")
    .replace(/ *\n */g, "\n")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
};

const resolveTicketSubjectFromDescription = (value?: string) => {
  const normalized = normalizeDescription(value);
  return normalized || "Без описания";
};

const resolveTicketCreatedSourceLabel = (source?: string) => {
  if (source === "ui") return "UI";
  if (source === "bitrix") return "Bitrix24";
  if (source === "servicedesk") return "ServiceDesk";
  if (source === "system") return "System";
  return "Неизвестно";
};

const DATE_STAMP_MIN_WIDTH = "10ch";
const TIME_STAMP_MIN_WIDTH = "5ch";
const TABLE_COLUMN_KEYS = [
  "selection",
  "number",
  "status",
  "company_display",
  "assignee_display",
  "reporter_display",
  "subject",
  "bitrix_deal_title",
  "last_comment",
  "created_at",
  "last_activity",
  "sync_with_bitrix",
] as const;
const DEFAULT_TABLE_COLUMN_KEYS = TABLE_COLUMN_KEYS.filter(
  (key) => key !== "bitrix_deal_title" && key !== "selection",
);
type TableColumnKey = (typeof TABLE_COLUMN_KEYS)[number];
type TableSortKey =
  | "number"
  | "assignee_display"
  | "created_at"
  | "last_activity";
type TableSortOrder = "asc" | "desc";
const COLUMN_MAX_WIDTH: Partial<Record<TableColumnKey, number>> = {
  subject: 500,
  last_comment: 500,
};

const formatDateStamp = (value?: string) => ({
  date: value ? dayjs(value).format("DD.MM.YYYY") : "-",
  time: value ? dayjs(value).format("HH:mm") : "--:--",
});

const formatDeferredDateTime = (value?: string) => {
  if (!value) return "";
  const dt = dayjs(value);
  if (!dt.isValid()) return "";
  return dt.format("DD.MM.YYYY HH:mm");
};

const formatDeferredTooltip = (value?: string) => {
  const formatted = formatDeferredDateTime(value);
  return formatted ? `Отложено до ${formatted}` : "";
};

const TicketDateStamp: React.FC<{ label: string; value?: string }> = ({
  label,
  value,
}) => {
  const stamp = formatDateStamp(value);
  return (
    <div className="ticket-date-stamp">
      <Text type="secondary" className="ticket-date-stamp-label">
        {label}
      </Text>
      <Text className="ticket-date-stamp-value">
        <span>{stamp.date}</span>
        <span>{stamp.time}</span>
      </Text>
    </div>
  );
};

const ExternalLinkBadge: React.FC<{
  label: string;
  href?: string;
  title: string;
  color: string;
  compact?: boolean;
  onClick?: (event: React.MouseEvent) => void;
}> = ({ label, href, title, color, compact, onClick }) => {
  if (!String(href || "").trim()) {
    return null;
  }
  return (
    <Tooltip title={title}>
      <a
        href={href}
        target="_blank"
        rel="noreferrer"
        onClick={onClick}
        style={{ display: "inline-flex", alignItems: "center", gap: 4 }}
      >
        <Tag color={color} style={{ marginInlineEnd: 0 }}>
          {compact ? label : label}
        </Tag>
        <LinkOutlined />
      </a>
    </Tooltip>
  );
};

const ExternalLinksBadges: React.FC<{
  bitrixURL?: string;
  pyrusURL?: string;
  compact?: boolean;
  onClick?: (event: React.MouseEvent) => void;
}> = ({ bitrixURL, pyrusURL, compact, onClick }) => {
  if (!String(bitrixURL || "").trim() && !String(pyrusURL || "").trim()) {
    return null;
  }
  return (
    <Space size={4} wrap>
      <ExternalLinkBadge
        label="B24"
        href={bitrixURL}
        title="Открыть сделку в Bitrix24"
        color="success"
        compact={compact}
        onClick={onClick}
      />
      <ExternalLinkBadge
        label="Pyrus"
        href={pyrusURL}
        title="Открыть задачу в Pyrus"
        color="geekblue"
        compact={compact}
        onClick={onClick}
      />
    </Space>
  );
};

const estimateHeaderMinWidth = (title: string) => {
  // Минимальная ширина: текст заголовка + внутренние отступы + область drag-handle.
  return Math.max(70, title.length * 8 + 44);
};

const clampColumnWidth = (key: string, width: number, minWidth: number) => {
  const min = minWidth || 90;
  const max = COLUMN_MAX_WIDTH[key as TableColumnKey];
  const bounded = Math.max(width, min);
  if (typeof max === "number") {
    return Math.min(bounded, max);
  }
  return bounded;
};

const ResizableHeaderCell = React.forwardRef<
  HTMLTableCellElement,
  HeaderCellProps
>((props, ref) => {
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
      minConstraints={[minWidth || 90, 0]}
      draggableOpts={{ enableUserSelectHack: false }}
    >
      <th ref={ref} {...rest}>
        {children}
      </th>
    </Resizable>
  );
});

ResizableHeaderCell.displayName = "ResizableHeaderCell";

const DraggableHeaderCell: React.FC<HeaderCellProps> = ({
  id,
  style,
  isResizing,
  isDragDisabled,
  children,
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
  } = useSortable({ id: id || "", disabled: sortableDisabled });

  const mergedStyle: React.CSSProperties = {
    ...style,
    transform: CSS.Transform.toString(transform),
    transition,
    cursor: sortableDisabled ? "default" : "move",
    ...(isDragging ? { position: "relative", zIndex: 2 } : {}),
  };

  return (
    <ResizableHeaderCell
      ref={setNodeRef}
      style={mergedStyle}
      {...attributes}
      {...rest}
    >
      <div className="tickets-table-header">
        <span className="tickets-table-header-title">{children}</span>
        {!sortableDisabled && (
          <span
            ref={setActivatorNodeRef}
            className={`tickets-table-drag-handle${isResizing ? " is-disabled" : ""}`}
            {...listeners}
            onClick={(event) => event.stopPropagation()}
            onMouseDown={(event) => event.stopPropagation()}
            onTouchStart={(event) => event.stopPropagation()}
          >
            <MenuOutlined />
          </span>
        )}
      </div>
    </ResizableHeaderCell>
  );
};

const TicketsPage: React.FC = () => {
  const { token } = antTheme.useToken();
  const { setHeaderAddon, setHeaderAddonPlacement } = useLayoutHeader();
  const searchParamsRaw = useTicketParamsStore((state) => state.ticketParams);
  const setSearchParamsRaw = useTicketParamsStore(
    (state) => state.setTicketParams,
  );
  const createTicketRequestID = useTicketParamsStore(
    (state) => state.createTicketRequestID,
  );
  const clearCreateTicketRequest = useTicketParamsStore(
    (state) => state.clearCreateTicketRequest,
  );
  const selectedTicketIDs = useTicketParamsStore(
    (state) => state.selectedTicketIDs,
  );
  const setSelectedTicketIDs = useTicketParamsStore(
    (state) => state.setSelectedTicketIDs,
  );
  const searchParams = useMemo(
    () => new URLSearchParams(searchParamsRaw),
    [searchParamsRaw],
  );
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const user = useAuthStore((state) => state.user);
  const setUser = useAuthStore((state) => state.setUser);
  const isBitrixEnabled = user?.bitrix_enabled === true;
  const userRoles = user?.roles || [];
  const isAdminRole = userRoles.includes("admin");
  const canAccessTelephony =
    isAdminRole || userRoles.includes("support_specialist");
  const isDeleteBlockedRole =
    userRoles.includes("support_specialist") || userRoles.includes("intern");
  const isCommentAuthor = (authorName?: string) =>
    String(authorName || "").trim() === String(user?.full_name || "").trim();
  const canManageComment = (authorName?: string) =>
    isAdminRole || isCommentAuthor(authorName);
  const canDeleteComment = (authorName?: string) =>
    isAdminRole || (!isDeleteBlockedRole && isCommentAuthor(authorName));

  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [selectedTicketId, setSelectedTicketId] = useState<string | null>(null);
  const [commentDraft, setCommentDraft] = useState("");
  const [commentIsPrivate, setCommentIsPrivate] = useState(false);
  const [editingCommentID, setEditingCommentID] = useState("");
  const [editingCommentDraft, setEditingCommentDraft] = useState("");
  const [statusComment, setStatusComment] = useState("");
  const [pendingStatus, setPendingStatus] = useState<TicketStatus | null>(null);
  const [pendingDeferredAt, setPendingDeferredAt] = useState<string>("");
  const [isResizingColumn, setIsResizingColumn] = useState(false);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
  );

  const q = searchParams.get("q") || "";
  const status = searchParams.get("status") || "";
  const tableColumnsParam = searchParams.get("table_columns") || "";
  const tableSortParam = searchParams.get("table_sort") || "";
  const selectedPresetID = searchParams.get("preset_id") || "";
  const onlyActiveStatuses = searchParams.get("only_active_statuses") === "1";
  const assigneeIDs = searchParams.get("assignee_ids") || "";
  const archiveMode =
    searchParams.get("archive_mode") === "archive" ? "archive" : "active";
  const activeCompany = searchParams.get("company") || "";
  const archiveCompany = searchParams.get("archive_company") || "";
  const company = archiveMode === "archive" ? archiveCompany : activeCompany;
  const activePeriodFrom = searchParams.get("period_from") || "";
  const activePeriodTo = searchParams.get("period_to") || "";
  const archivePeriodFrom = searchParams.get("archive_period_from") || "";
  const archivePeriodTo = searchParams.get("archive_period_to") || "";
  const periodFrom =
    archiveMode === "archive" ? archivePeriodFrom : activePeriodFrom;
  const periodTo = archiveMode === "archive" ? archivePeriodTo : activePeriodTo;
  const viewMode = (searchParams.get("view") as ViewMode) || "list";
  const limit = 20;
  const loadMoreRef = useRef<HTMLDivElement | null>(null);
  const statusValues = useMemo(
    () =>
      status
        .split(",")
        .filter((value): value is TicketStatus => Boolean(value)),
    [status],
  );
  const effectiveStatusValues = useMemo(() => {
    if (archiveMode === "archive") {
      return [];
    }
    if (!onlyActiveStatuses) {
      return statusValues;
    }
    const filtered = statusValues.filter((value) =>
      TICKET_ACTIVE_STATUS_VALUES.includes(value),
    );
    return filtered.length ? filtered : TICKET_ACTIVE_STATUS_VALUES;
  }, [archiveMode, onlyActiveStatuses, statusValues]);
  const effectiveStatus = effectiveStatusValues.join(",");
  const selectedTableColumnKeys = useMemo<TableColumnKey[]>(() => {
    const allowedColumnKeys = isBitrixEnabled
      ? TABLE_COLUMN_KEYS
      : TABLE_COLUMN_KEYS.filter(
          (key) => key !== "bitrix_deal_title" && key !== "sync_with_bitrix",
        );
    const defaultColumnKeys = isBitrixEnabled
      ? DEFAULT_TABLE_COLUMN_KEYS
      : TABLE_COLUMN_KEYS.filter(
          (key) =>
            key !== "selection" &&
            key !== "bitrix_deal_title" &&
            key !== "sync_with_bitrix",
        );
    if (!tableColumnsParam) {
      return [...defaultColumnKeys];
    }
    const values = tableColumnsParam
      .split(",")
      .filter((value): value is TableColumnKey =>
        (allowedColumnKeys as readonly string[]).includes(value),
      );
    return values.length ? values : [...defaultColumnKeys];
  }, [isBitrixEnabled, tableColumnsParam]);
  const tableSort = useMemo<{
    key: TableSortKey;
    order: TableSortOrder;
  } | null>(() => {
    if (!tableSortParam) {
      return null;
    }
    const [rawKey, rawOrder] = tableSortParam.split(":");
    if (
      (rawKey === "number" ||
        rawKey === "assignee_display" ||
        rawKey === "created_at" ||
        rawKey === "last_activity") &&
      (rawOrder === "asc" || rawOrder === "desc")
    ) {
      return { key: rawKey, order: rawOrder };
    }
    return null;
  }, [tableSortParam]);
  const commentsNewFirst =
    (user?.profile_config as any)?.tickets?.comments_new_first !== false;
  const headerAddon = useMemo(
    () => (canAccessTelephony ? <TelephonyLineIndicator /> : null),
    [canAccessTelephony],
  );
  const ticketSubscriptions = useMemo<string[]>(() => {
    const list = (user?.profile_config as any)?.tickets?.subscriptions;
    if (!Array.isArray(list)) return [];
    return list.map((item: unknown) => String(item).trim()).filter(Boolean);
  }, [user?.profile_config]);

  useEffect(() => {
    setHeaderAddon(headerAddon);
    setHeaderAddonPlacement("inline");
    return () => {
      setHeaderAddon(null);
      setHeaderAddonPlacement("below");
    };
  }, [headerAddon, setHeaderAddon, setHeaderAddonPlacement]);

  const { data, isLoading, isFetchingNextPage, hasNextPage, fetchNextPage } =
    useInfiniteQuery({
      queryKey: [
        "tickets",
        {
          q,
          status,
          onlyActiveStatuses,
          effectiveStatus,
          company,
          assigneeIDs,
          periodFrom,
          periodTo,
          archiveMode,
          activeCompany,
          archiveCompany,
          activePeriodFrom,
          activePeriodTo,
          archivePeriodFrom,
          archivePeriodTo,
        },
      ],
      initialPageParam: 0,
      queryFn: ({ pageParam }) =>
        ticketsApi.getTickets({
          search: q || undefined,
          status:
            archiveMode === "archive"
              ? undefined
              : effectiveStatus || undefined,
          company_id: company || undefined,
          assignee_ids: assigneeIDs || undefined,
          period_from: periodFrom || undefined,
          period_to: periodTo || undefined,
          archive_mode: archiveMode,
          limit,
          offset: Number(pageParam) || 0,
        }),
      getNextPageParam: (lastPage) => {
        const meta = lastPage.meta;
        if (!meta?.has_next) {
          return undefined;
        }
        return (meta.offset || 0) + (meta.limit || limit);
      },
      staleTime: 20_000,
    });

  const tickets = useMemo(
    () => (data?.pages || []).flatMap((pageData) => pageData.data || []),
    [data?.pages],
  );
  const visibleTickets = tickets;
  const total = data?.pages?.[0]?.meta?.total || 0;

  const { data: detailsResponse, isLoading: isDetailsLoading } = useQuery({
    queryKey: ["ticket", selectedTicketId],
    queryFn: () => ticketsApi.getTicket(selectedTicketId || ""),
    enabled: Boolean(selectedTicketId),
  });

  const details: TicketDetailsDTO | undefined = detailsResponse?.data;
  const metadata = details?.metadata;

  const { data: usersResponse } = useQuery({
    queryKey: ["users-assignees"],
    queryFn: () => usersApi.getAssignees(),
    retry: false,
    staleTime: 60_000,
  });

  const mentionOptions = useMemo<MentionOption[]>(
    () =>
      (usersResponse?.data || [])
        .filter((item) => item.is_active)
        .map((item) => ({
          id: item.id,
          label: item.full_name || item.username,
        })),
    [usersResponse?.data],
  );

  const { data: infraResponse, isLoading: isInfraLoading } = useQuery({
    queryKey: ["company-infra", metadata?.company_id],
    queryFn: () => companiesApi.getInfrastructure(metadata?.company_id || ""),
    enabled: Boolean(metadata?.company_id),
    staleTime: 30_000,
  });

  const infrastructure = useMemo(
    () => infraResponse?.data || [],
    [infraResponse?.data],
  );

  const { data: companyResponse } = useQuery({
    queryKey: ["company-profile", metadata?.company_id],
    queryFn: () => companiesApi.getCompany(metadata?.company_id || ""),
    enabled: Boolean(metadata?.company_id),
    staleTime: 30_000,
  });

  const companyTitle = useMemo(() => {
    const companyData = companyResponse?.data;
    return (
      companyData?.title ||
      companyData?.additional_name ||
      details?.company_name ||
      metadata?.company_name ||
      metadata?.company_id ||
      ""
    );
  }, [
    companyResponse?.data,
    details?.company_name,
    metadata?.company_id,
    metadata?.company_name,
  ]);

  const connections = useMemo(() => {
    return infrastructure
      .map((item) => {
        if (
          item.entity_type !== "Server" &&
          item.entity_type !== "Workstation"
        ) {
          return null;
        }
        const dataRow = item.data as Record<string, string | undefined>;
        const title =
          dataRow.device_name ||
          dataRow.server_name ||
          dataRow.uuid ||
          "Оборудование";
        const entityID = String(dataRow.uuid || "").trim();
        const entityPath =
          item.entity_type === "Server"
            ? `/servers/${entityID}`
            : `/workstations/${entityID}`;
        const rows = [
          ...(item.entity_type === "Server"
            ? [{ label: "IP", value: dataRow.ip }]
            : []),
          { label: "AnyDesk", value: dataRow.anydesk },
          { label: "TeamViewer", value: dataRow.teamviewer },
          { label: "RDP", value: dataRow.rdp },
          { label: "LM", value: dataRow.litemanager },
          { label: "RustDesk", value: dataRow.rustdesk },
        ].filter((entry) => entry.value);
        if (rows.length === 0) return null;
        return {
          key: `${item.entity_type}-${dataRow.uuid || title}`,
          title,
          rows,
          entityPath,
        };
      })
      .filter(Boolean) as Array<{
      key: string;
      title: string;
      rows: Array<{ label: string; value?: string }>;
      entityPath: string;
    }>;
  }, [infrastructure]);

  const comments = useMemo(() => {
    const sorted = [...(details?.comments || [])].sort((a, b) => {
      const delta =
        dayjs(a.creation_date).valueOf() - dayjs(b.creation_date).valueOf();
      return commentsNewFirst ? -delta : delta;
    });
    return sorted.map((item) => ({
      id: item.uuid,
      author: item.author_name || "Сотрудник",
      authorRaw: item.author_name || "",
      date: dayjs(item.creation_date).format("DD.MM.YYYY HH:mm"),
      text: item.text,
      isPrivate: item.is_private ?? false,
    }));
  }, [commentsNewFirst, details?.comments]);

  const changeStatusMutation = useMutation({
    mutationFn: async (payload: {
      id: string;
      status: TicketStatus;
      comment?: string;
      deferredUntil?: string;
    }) =>
      ticketsApi.changeStatus(
        payload.id,
        payload.status,
        payload.comment,
        payload.deferredUntil,
      ),
    onSuccess: () => {
      message.success("Статус обновлён");
      setPendingStatus(null);
      setStatusComment("");
      setPendingDeferredAt("");
      queryClient.invalidateQueries({ queryKey: ["tickets"] });
      queryClient.invalidateQueries({ queryKey: ["ticket", selectedTicketId] });
    },
    onError: () => message.error("Не удалось обновить статус"),
  });

  const addCommentMutation = useMutation({
    mutationFn: async (payload: {
      id: string;
      comment: string;
      isPrivate: boolean;
    }) => ticketsApi.addComment(payload.id, payload.comment, payload.isPrivate),
    onSuccess: () => {
      message.success("Комментарий добавлен");
      setCommentDraft("");
      setCommentIsPrivate(false);
      queryClient.invalidateQueries({ queryKey: ["tickets"] });
      queryClient.invalidateQueries({ queryKey: ["ticket", selectedTicketId] });
    },
    onError: () => message.error("Не удалось добавить комментарий"),
  });

  const updateCommentMutation = useMutation({
    mutationFn: async (payload: {
      id: string;
      commentUUID: string;
      comment: string;
    }) =>
      ticketsApi.updateComment(
        payload.id,
        payload.commentUUID,
        payload.comment,
      ),
    onSuccess: () => {
      message.success("Комментарий обновлён");
      setEditingCommentID("");
      setEditingCommentDraft("");
      queryClient.invalidateQueries({ queryKey: ["tickets"] });
      queryClient.invalidateQueries({ queryKey: ["ticket", selectedTicketId] });
    },
    onError: () => message.error("Не удалось обновить комментарий"),
  });

  const deleteCommentMutation = useMutation({
    mutationFn: async (payload: { id: string; commentUUID: string }) =>
      ticketsApi.deleteComment(payload.id, payload.commentUUID),
    onSuccess: () => {
      message.success("Комментарий удалён");
      setEditingCommentID("");
      setEditingCommentDraft("");
      queryClient.invalidateQueries({ queryKey: ["tickets"] });
      queryClient.invalidateQueries({ queryKey: ["ticket", selectedTicketId] });
    },
    onError: () => message.error("Не удалось удалить комментарий"),
  });

  const updateProfileConfigMutation = useMutation({
    mutationFn: (config: Record<string, unknown>) =>
      profileApi.updateConfig({ profile_config: config as any }),
    onSuccess: (response) => {
      const dtoUser = (response as any)?.data;
      if (dtoUser && typeof dtoUser === "object" && "id" in dtoUser) {
        setUser(dtoUser as any);
      }
    },
  });

  const copyConnectionMutation = useMutation({
    mutationFn: async (payload: { id: string; label: string; value: string }) =>
      ticketsApi.recordConnectionCopy(payload.id, payload.label, payload.value),
  });

  const uploadInlineImage = async (source: File): Promise<string | null> => {
    if (!selectedTicketId) {
      return null;
    }
    const response = await ticketsApi.uploadAttachments(selectedTicketId, [
      source,
    ]);
    const uploaded = response.data?.items?.[0];
    if (!uploaded?.file_path) {
      return null;
    }
    queryClient.invalidateQueries({ queryKey: ["ticket", selectedTicketId] });
    return String(uploaded.file_path)
      .replace(/^\/static\//, "/api/static/")
      .replace(/^static\//, "/api/static/");
  };

  const uploadInlineFile = async (source: File): Promise<string | null> => {
    if (!selectedTicketId) {
      return null;
    }
    const response = await ticketsApi.uploadAttachments(selectedTicketId, [
      source,
    ]);
    const uploaded = response.data?.items?.[0];
    if (!uploaded?.file_path) {
      return null;
    }
    queryClient.invalidateQueries({ queryKey: ["ticket", selectedTicketId] });
    return String(uploaded.file_path)
      .replace(/^\/static\//, "/api/static/")
      .replace(/^static\//, "/api/static/");
  };

  const closeQuickModal = () => {
    setSelectedTicketId(null);
    setCommentDraft("");
    setCommentIsPrivate(false);
    setEditingCommentID("");
    setEditingCommentDraft("");
    setPendingStatus(null);
    setStatusComment("");
    setPendingDeferredAt("");
  };

  useEffect(() => {
    if (createTicketRequestID === 0) {
      return;
    }
    setIsCreateOpen(true);
    clearCreateTicketRequest();
  }, [clearCreateTicketRequest, createTicketRequestID]);

  useEffect(() => {
    if (!editingCommentID) return;
    const exists = (details?.comments || []).some(
      (item) => item.uuid === editingCommentID,
    );
    if (!exists) {
      setEditingCommentID("");
      setEditingCommentDraft("");
    }
  }, [details?.comments, editingCommentID]);

  useEffect(() => {
    const visibleSet = new Set(visibleTickets.map((item) => item.id));
    const next = selectedTicketIDs.filter((id) => visibleSet.has(id));
    if (next.length !== selectedTicketIDs.length) {
      setSelectedTicketIDs(next);
    }
  }, [selectedTicketIDs, setSelectedTicketIDs, visibleTickets]);

  useEffect(() => {
    if (!selectedTicketId) {
      return;
    }
    const interactiveOverlaySelector = [
      ".ant-select-dropdown",
      ".ant-popover",
      ".ant-picker-dropdown",
      ".ant-dropdown",
      ".ant-modal-root",
      ".ant-image-preview-root",
      ".ant-image-preview",
    ].join(", ");
    const onMouseDown = (event: MouseEvent) => {
      const target = event.target as HTMLElement | null;
      if (target?.closest(".ant-drawer")) {
        return;
      }
      if (target?.closest(interactiveOverlaySelector)) {
        return;
      }
      closeQuickModal();
    };
    document.addEventListener("mousedown", onMouseDown);
    return () => document.removeEventListener("mousedown", onMouseDown);
  }, [selectedTicketId]);

  useEffect(() => {
    const node = loadMoreRef.current;
    if (!node || !hasNextPage) {
      return;
    }

    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries[0]?.isIntersecting || isFetchingNextPage) {
          return;
        }
        void fetchNextPage();
      },
      { rootMargin: "240px 0px" },
    );

    observer.observe(node);
    return () => observer.disconnect();
  }, [fetchNextPage, hasNextPage, isFetchingNextPage, tickets.length]);

  const tableDataBase = useMemo(
    () =>
      visibleTickets.map((ticket) => ({
        ...ticket,
        subject: resolveTicketSubjectFromDescription(ticket.description),
        subject_multiline:
          normalizeDescriptionMultiline(ticket.description) || "Без описания",
        company_display:
          ticket.company_name || ticket.company_id || "Компания не указана",
        last_comment_display: normalizeDescription(ticket.last_comment),
        last_comment_multiline: normalizeDescriptionMultiline(
          ticket.last_comment,
        ),
        assignee_display: ticket.assignee?.full_name || "Не назначен",
        reporter_display: ticket.reporter_name || "Сотрудник",
      })),
    [visibleTickets],
  );
  const tableData = useMemo(() => {
    if (!tableSort) {
      return tableDataBase;
    }
    const factor = tableSort.order === "asc" ? 1 : -1;
    return [...tableDataBase].sort((a, b) => {
      switch (tableSort.key) {
        case "number":
          return ((a.number || 0) - (b.number || 0)) * factor;
        case "assignee_display":
          return (
            String(a.assignee_display || "").localeCompare(
              String(b.assignee_display || ""),
              "ru",
            ) * factor
          );
        case "created_at":
          return (
            (dayjs(a.created_at).valueOf() - dayjs(b.created_at).valueOf()) *
            factor
          );
        case "last_activity":
          return (
            (dayjs(a.last_activity).valueOf() -
              dayjs(b.last_activity).valueOf()) *
            factor
          );
        default:
          return 0;
      }
    });
  }, [tableDataBase, tableSort]);

  type TableRow = (typeof tableData)[number];

  const tableColumnsBase: ColumnsType<TableRow> = useMemo(
    () => [
      {
        title: "Номер",
        dataIndex: "number",
        key: "number",
        width: 90,
        minWidth: estimateHeaderMinWidth("Номер"),
        render: (val: number, row) => (
          <Link
            to={`/tickets/${row.id}`}
            onClick={(event) => event.stopPropagation()}
          >
            <Text strong>#{val}</Text>
          </Link>
        ),
      },
      {
        title: "Статус",
        dataIndex: "status",
        key: "status",
        width: 140,
        minWidth: estimateHeaderMinWidth("Статус"),
        render: (value: TicketStatus, row) => {
          const meta = getTicketStatusMeta(value);
          const deferredTitle =
            value === "deferred"
              ? formatDeferredTooltip(row.deferred_until)
              : "";
          const statusTag = <Tag color={meta.color}>{meta.label}</Tag>;
          return (
            <Space size={4}>
              {deferredTitle ? (
                <Tooltip title={deferredTitle}>{statusTag}</Tooltip>
              ) : (
                statusTag
              )}
              {row.is_common_contract && <Tag color="gold">Платный</Tag>}
            </Space>
          );
        },
      },
      {
        title: "Компания",
        dataIndex: "company_display",
        key: "company_display",
        width: 220,
        minWidth: estimateHeaderMinWidth("Компания"),
        ellipsis: true,
        render: (value: string) => (
          <Text ellipsis style={{ width: "100%", display: "block" }}>
            {value}
          </Text>
        ),
      },
      {
        title: "Исполнитель",
        dataIndex: "assignee_display",
        key: "assignee_display",
        width: 170,
        minWidth: estimateHeaderMinWidth("Исполнитель"),
        ellipsis: true,
      },
      {
        title: "Автор",
        dataIndex: "reporter_display",
        key: "reporter_display",
        width: 180,
        minWidth: estimateHeaderMinWidth("Автор"),
        ellipsis: true,
      },
      {
        title: "Описание",
        dataIndex: "subject_multiline",
        key: "subject",
        width: 260,
        minWidth: estimateHeaderMinWidth("Описание"),
        render: (value: string) => (
          <div className="tickets-table-multiline-cell" title={value}>
            {value}
          </div>
        ),
      },
      {
        title: "Заголовок Bitrix24",
        dataIndex: "bitrix_deal_title",
        key: "bitrix_deal_title",
        width: 240,
        minWidth: estimateHeaderMinWidth("Заголовок Bitrix24"),
        ellipsis: true,
        render: (value?: string) => value || "-",
      },
      {
        title: "Последний комментарий",
        dataIndex: "last_comment_multiline",
        key: "last_comment",
        width: 260,
        minWidth: estimateHeaderMinWidth("Последний комментарий"),
        render: (value: string) => (
          <div
            className="tickets-table-multiline-cell tickets-table-multiline-cell-secondary"
            title={value || "-"}
          >
            {value || "-"}
          </div>
        ),
      },
      {
        title: "Создано",
        dataIndex: "created_at",
        key: "created_at",
        width: 110,
        minWidth: estimateHeaderMinWidth("Создано"),
        render: (value?: string) => {
          const stamp = formatDateStamp(value);
          return (
            <Space direction="vertical" size={0}>
              <Text style={{ minWidth: DATE_STAMP_MIN_WIDTH }}>
                {stamp.date}
              </Text>
              <Text type="secondary" style={{ minWidth: TIME_STAMP_MIN_WIDTH }}>
                {stamp.time}
              </Text>
            </Space>
          );
        },
      },
      {
        title: "Обновлено",
        dataIndex: "last_activity",
        key: "last_activity",
        width: 110,
        minWidth: estimateHeaderMinWidth("Обновлено"),
        render: (value: string) => {
          const stamp = formatDateStamp(value);
          return (
            <Space direction="vertical" size={0}>
              <Text style={{ minWidth: DATE_STAMP_MIN_WIDTH }}>
                {stamp.date}
              </Text>
              <Text type="secondary" style={{ minWidth: TIME_STAMP_MIN_WIDTH }}>
                {stamp.time}
              </Text>
            </Space>
          );
        },
      },
      {
        title: "External",
        dataIndex: "sync_with_bitrix",
        key: "sync_with_bitrix",
        width: 160,
        minWidth: estimateHeaderMinWidth("External"),
        render: (_value: boolean, row) => (
          <ExternalLinksBadges
            bitrixURL={row.bitrix_deal_url}
            pyrusURL={row.pyrus_task_url}
            compact
            onClick={(event) => event.stopPropagation()}
          />
        ),
      },
    ],
    [],
  );

  const [tableColumnsState, setTableColumnsState] =
    useState<ColumnsType<TableRow>>(tableColumnsBase);
  const [isTableLayoutHydrated, setIsTableLayoutHydrated] = useState(false);

  const tableLayoutStorageKey = useMemo(() => {
    const userKey = user?.id ? String(user.id) : "guest";
    return `tickets-table-layout-${userKey}`;
  }, [user?.id]);

  useEffect(() => {
    setIsTableLayoutHydrated(false);
    const raw = localStorage.getItem(tableLayoutStorageKey);
    if (!raw) {
      setTableColumnsState(tableColumnsBase);
      setIsTableLayoutHydrated(true);
      return;
    }
    try {
      const parsed = JSON.parse(raw) as Array<{ key: string; width?: number }>;
      const baseByKey = new Map(tableColumnsBase.map((col) => [col.key, col]));
      const next: ColumnsType<TableRow> = [];
      const seen = new Set<string>();

      for (const entry of parsed) {
        const base = baseByKey.get(entry.key);
        if (!base) continue;
        const minWidth = (base as { minWidth?: number }).minWidth || 90;
        const nextWidth = clampColumnWidth(
          String(base.key),
          entry.width ?? (base.width as number),
          minWidth,
        );
        next.push({
          ...base,
          width: nextWidth,
        });
        seen.add(entry.key);
      }
      for (const col of tableColumnsBase) {
        const key = col.key as string;
        if (seen.has(key)) continue;
        next.push(col);
      }
      setTableColumnsState(next.length ? next : tableColumnsBase);
      setIsTableLayoutHydrated(true);
    } catch {
      setTableColumnsState(tableColumnsBase);
      setIsTableLayoutHydrated(true);
    }
  }, [tableColumnsBase, tableLayoutStorageKey]);

  useEffect(() => {
    if (!isTableLayoutHydrated) return;
    if (!tableColumnsState.length) return;
    const payload = tableColumnsState.map((col) => ({
      key: col.key as string,
      width: col.width as number | undefined,
    }));
    localStorage.setItem(tableLayoutStorageKey, JSON.stringify(payload));
  }, [isTableLayoutHydrated, tableColumnsState, tableLayoutStorageKey]);

  const handleResize =
    (index: number) =>
    (_event: React.SyntheticEvent, data: { size: { width: number } }) => {
      setTableColumnsState((columns) => {
        const nextColumns = [...columns];
        const minWidth =
          (nextColumns[index] as { minWidth?: number }).minWidth || 90;
        const columnKey = String(nextColumns[index]?.key || "");
        nextColumns[index] = {
          ...nextColumns[index],
          width: clampColumnWidth(columnKey, data.size.width, minWidth),
        };
        return nextColumns;
      });
    };

  const handleDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    const activeID = String(active.id);
    const overID = String(over.id);
    if (activeID === "selection" || overID === "selection") return;
    setTableColumnsState((columns) => {
      const oldIndex = columns.findIndex((col) => col.key === activeID);
      const newIndex = columns.findIndex((col) => col.key === overID);
      if (oldIndex === -1 || newIndex === -1) return columns;
      return arrayMove(columns, oldIndex, newIndex);
    });
  };

  function applyTableSort(key: TableSortKey) {
    const nextOrder: TableSortOrder | null =
      tableSort?.key !== key
        ? "asc"
        : tableSort.order === "asc"
          ? "desc"
          : null;
    const nextTableSortValue = nextOrder ? `${key}:${nextOrder}` : "";
    if (user && selectedPresetID) {
      const profileConfig = (user.profile_config || {}) as Record<string, any>;
      const ticketsConfig = (profileConfig.tickets || {}) as Record<
        string,
        any
      >;
      const filtersConfig = (ticketsConfig.filters || {}) as Record<
        string,
        any
      >;
      const presets = Array.isArray(filtersConfig.presets)
        ? filtersConfig.presets
        : [];
      const presetIndex = presets.findIndex(
        (item: any) => item?.id === selectedPresetID,
      );
      if (presetIndex >= 0) {
        const currentPreset = presets[presetIndex] as {
          values?: Record<string, string>;
        };
        const currentSortValue =
          typeof currentPreset?.values?.table_sort === "string"
            ? currentPreset.values.table_sort
            : "";
        if (currentSortValue !== nextTableSortValue) {
          const nextPreset = {
            ...presets[presetIndex],
            values: {
              ...((presets[presetIndex] as any)?.values || {}),
            } as Record<string, string>,
          };
          if (nextTableSortValue) {
            nextPreset.values.table_sort = nextTableSortValue;
          } else {
            delete nextPreset.values.table_sort;
          }
          const nextPresets = [...presets];
          nextPresets[presetIndex] = nextPreset;
          const nextConfig = {
            ...profileConfig,
            tickets: {
              ...ticketsConfig,
              filters: {
                ...filtersConfig,
                presets: nextPresets,
              },
            },
          };
          const prevUser = user;
          setUser({ ...user, profile_config: nextConfig as any });
          updateProfileConfigMutation.mutate(nextConfig as any, {
            onError: () => {
              setUser(prevUser);
            },
          });
        }
      }
    }
    const params = new URLSearchParams(searchParams);
    if (!nextOrder) {
      params.delete("table_sort");
    } else {
      params.set("table_sort", `${key}:${nextOrder}`);
    }
    params.set("page", "1");
    setSearchParamsRaw(params.toString());
  }

  function renderSortableTitle(label: string, key: TableSortKey) {
    const order = tableSort?.key === key ? tableSort.order : null;
    return (
      <Space
        size={4}
        onClick={(event) => {
          event.stopPropagation();
          applyTableSort(key);
        }}
        style={{ cursor: "pointer", userSelect: "none" }}
      >
        <span>{label}</span>
        {order ? (
          <span aria-hidden="true">{order === "asc" ? "↑" : "↓"}</span>
        ) : null}
      </Space>
    );
  }

  const tableColumnsVisibleState = tableColumnsState.filter((col) =>
    selectedTableColumnKeys.includes(String(col.key) as TableColumnKey),
  );
  const tableColumns = tableColumnsVisibleState.map((col) => {
    const columnKey = String(col.key);
    const stateIndex = tableColumnsState.findIndex(
      (item) => item.key === col.key,
    );
    const sortableLabel =
      columnKey === "number"
        ? "Номер"
        : columnKey === "assignee_display"
          ? "Исполнитель"
          : columnKey === "created_at"
            ? "Создано"
            : columnKey === "last_activity"
              ? "Обновлено"
              : null;
    return {
      ...col,
      title: sortableLabel
        ? renderSortableTitle(sortableLabel, columnKey as TableSortKey)
        : col.title,
      onHeaderCell: () => ({
        id: col.key as string,
        width: col.width,
        minWidth: (col as { minWidth?: number }).minWidth || 90,
        isDragDisabled: columnKey === "selection",
        onResize: handleResize(stateIndex),
        onResizeStart: () => setIsResizingColumn(true),
        onResizeStop: () => setIsResizingColumn(false),
        isResizing: isResizingColumn,
      }),
    };
  });
  if (selectedTableColumnKeys.includes("selection")) {
    tableColumns.unshift({
      key: "selection",
      title: (
        <Checkbox
          checked={
            tableData.length > 0 &&
            selectedTicketIDs.length === tableData.length
          }
          indeterminate={
            selectedTicketIDs.length > 0 &&
            selectedTicketIDs.length < tableData.length
          }
          onChange={(event) => {
            if (event.target.checked) {
              setSelectedTicketIDs(tableData.map((item) => item.id));
            } else {
              setSelectedTicketIDs([]);
            }
          }}
        />
      ),
      width: 44,
      onHeaderCell: () => ({
        id: "selection",
        width: 44,
        minWidth: 44,
        isDragDisabled: true,
      }),
      render: (_value: unknown, row: TableRow) => (
        <Checkbox
          checked={selectedTicketIDs.includes(row.id)}
          onChange={() => {
            const next = selectedTicketIDs.includes(row.id)
              ? selectedTicketIDs.filter((item) => item !== row.id)
              : [...selectedTicketIDs, row.id];
            setSelectedTicketIDs(next);
          }}
          onClick={(event) => event.stopPropagation()}
        />
      ),
    } as any);
  }

  const tableScrollX = useMemo(() => {
    return tableColumns.reduce((sum, col) => {
      const width = Number(
        col.width ?? (col as { minWidth?: number }).minWidth ?? 90,
      );
      return sum + (Number.isFinite(width) ? width : 90);
    }, 0);
  }, [tableColumns]);

  const applyAssigneeFilter = (assigneeID?: number) => {
    if (!assigneeID) return;
    const params = new URLSearchParams(searchParams);
    params.set("assignee_ids", String(assigneeID));
    params.set("page", "1");
    setSearchParamsRaw(params.toString());
  };

  const toggleTicketSubscription = async () => {
    if (!user || !selectedTicketId) {
      return;
    }
    const current = ticketSubscriptions;
    const exists = current.includes(selectedTicketId);
    const nextSubscriptions = exists
      ? current.filter((item) => item !== selectedTicketId)
      : [...current, selectedTicketId];
    const nextConfig = {
      ...(user.profile_config || {}),
      tickets: {
        ...((user.profile_config || {}).tickets || {}),
        subscriptions: nextSubscriptions,
      },
    };
    setUser({ ...user, profile_config: nextConfig as any });
    try {
      await updateProfileConfigMutation.mutateAsync(nextConfig as any);
      message.success(
        exists ? "Подписка на тикет отключена" : "Подписка на тикет включена",
      );
    } catch {
      setUser(user);
      message.error("Не удалось изменить подписку на тикет");
    }
  };

  const onTicketRowClick = (ticketID: string, event?: React.MouseEvent) => {
    if (event?.ctrlKey || event?.metaKey) {
      const next = selectedTicketIDs.includes(ticketID)
        ? selectedTicketIDs.filter((item) => item !== ticketID)
        : [...selectedTicketIDs, ticketID];
      setSelectedTicketIDs(next);
      return;
    }
    setSelectedTicketId(ticketID);
  };
  const commentComposer = (
    <Space
      direction="vertical"
      size="small"
      style={{
        width: "100%",
        marginTop: commentsNewFirst ? 0 : 12,
        marginBottom: commentsNewFirst ? 12 : 0,
      }}
    >
      <SmartTicketEditor
        value={commentDraft}
        onChange={setCommentDraft}
        placeholder="Добавьте комментарий"
        mentions={mentionOptions}
        onImageUpload={uploadInlineImage}
        onFileUpload={uploadInlineFile}
        minHeight={96}
      />
      <label
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          fontSize: 12,
          color: token.colorTextSecondary,
        }}
      >
        <input
          type="checkbox"
          checked={commentIsPrivate}
          onChange={(event) => setCommentIsPrivate(event.target.checked)}
        />
        Приватный (Только в Xenion)
      </label>
      <Button
        type="primary"
        loading={addCommentMutation.isPending}
        disabled={!hasEditorContent(commentDraft) || !selectedTicketId}
        onClick={() => {
          if (!selectedTicketId) return;
          addCommentMutation.mutate({
            id: selectedTicketId,
            comment: commentDraft,
            isPrivate: commentIsPrivate,
          });
        }}
      >
        Отправить
      </Button>
    </Space>
  );

  return (
    <Space direction="vertical" size="middle" style={{ width: "100%" }}>
      <Card>
        {viewMode === "list" && (
          <List
            loading={isLoading}
            dataSource={visibleTickets}
            renderItem={(item) => {
              const meta = getTicketStatusMeta(item.status);
              const deferredTitle =
                item.status === "deferred"
                  ? formatDeferredTooltip(item.deferred_until)
                  : "";
              return (
                <List.Item
                  key={item.id}
                  style={{ cursor: "pointer" }}
                  onClick={(event) =>
                    onTicketRowClick(
                      item.id,
                      event as unknown as React.MouseEvent,
                    )
                  }
                >
                  <Space className="ticket-list-item-wrap">
                    <Space
                      direction="vertical"
                      size={0}
                      className="ticket-list-main"
                    >
                      <Text className="ticket-company-centered" strong>
                        {item.company_name || item.company_id}
                      </Text>
                      <Space size={8}>
                        <Link
                          to={`/tickets/${item.id}`}
                          onClick={(event) => event.stopPropagation()}
                        >
                          <Text strong>#{item.number}</Text>
                        </Link>
                        {deferredTitle ? (
                          <Tooltip title={deferredTitle}>
                            <Tag color={meta.color}>{meta.label}</Tag>
                          </Tooltip>
                        ) : (
                          <Tag color={meta.color}>{meta.label}</Tag>
                        )}
                        {item.is_common_contract && (
                          <Tag color="gold">Платный</Tag>
                        )}
                        <ExternalLinksBadges
                          bitrixURL={
                            isBitrixEnabled ? item.bitrix_deal_url : undefined
                          }
                          pyrusURL={item.pyrus_task_url}
                          compact
                          onClick={(event) => event.stopPropagation()}
                        />
                      </Space>
                      <Text>
                        {resolveTicketSubjectFromDescription(item.description)}
                      </Text>
                      {item.last_comment && (
                        <Paragraph
                          className="ticket-description-paragraph"
                          type="secondary"
                          ellipsis={{ rows: 3 }}
                        >
                          {normalizeDescription(item.last_comment)}
                        </Paragraph>
                      )}
                    </Space>
                    <Space
                      direction="vertical"
                      size={6}
                      className="ticket-list-side"
                    >
                      <Text className="ticket-assignee-linklike">
                        {item.assignee?.full_name || "Не назначен"}
                      </Text>
                      <Text type="secondary">
                        {item.reporter_name || "Сотрудник"} •{" "}
                        {resolveTicketCreatedSourceLabel(item.created_source)}
                      </Text>
                      <TicketDateStamp
                        label="Создано"
                        value={item.created_at}
                      />
                      <TicketDateStamp
                        label="Обновлено"
                        value={item.last_activity}
                      />
                    </Space>
                  </Space>
                </List.Item>
              );
            }}
          />
        )}

        {viewMode === "cards" && (
          <Row gutter={[12, 12]}>
            {visibleTickets.map((item) => {
              const meta = getTicketStatusMeta(item.status);
              const deferredTitle =
                item.status === "deferred"
                  ? formatDeferredTooltip(item.deferred_until)
                  : "";
              return (
                <Col key={item.id} xs={24} md={12} xl={8}>
                  <Card
                    hoverable
                    className="glass-panel"
                    onClick={(event) =>
                      onTicketRowClick(
                        item.id,
                        event as unknown as React.MouseEvent,
                      )
                    }
                  >
                    <Space
                      direction="vertical"
                      size={6}
                      style={{ width: "100%" }}
                    >
                      <div className="ticket-card-top">
                        <div className="ticket-card-left">
                          <Link
                            to={`/tickets/${item.id}`}
                            onClick={(event) => event.stopPropagation()}
                          >
                            <Text strong className="ticket-card-number">
                              #{item.number}
                            </Text>
                          </Link>
                          <ExternalLinksBadges
                            bitrixURL={
                              isBitrixEnabled ? item.bitrix_deal_url : undefined
                            }
                            pyrusURL={item.pyrus_task_url}
                            compact
                            onClick={(event) => event.stopPropagation()}
                          />
                        </div>
                        <div className="ticket-company-centered ticket-company-top">
                          {/* TODO: Реализовать содержимое popover компании вместе с popover исполнителя. */}
                          <Popover
                            trigger="hover"
                            content={
                              <div style={{ minWidth: 180, minHeight: 48 }} />
                            }
                          >
                            <a
                              className="ticket-assignee-linklike"
                              onClick={(event) => {
                                event.preventDefault();
                                event.stopPropagation();
                              }}
                            >
                              {item.company_name || item.company_id}
                            </a>
                          </Popover>
                        </div>
                        <div className="ticket-card-right">
                          <Space size={4} className="ticket-card-status-wrap">
                            {deferredTitle ? (
                              <Tooltip title={deferredTitle}>
                                <Tag color={meta.color}>{meta.label}</Tag>
                              </Tooltip>
                            ) : (
                              <Tag color={meta.color}>{meta.label}</Tag>
                            )}
                            {item.is_common_contract && (
                              <Tag color="gold">Платный</Tag>
                            )}
                          </Space>
                          <div className="ticket-card-assignee-right">
                            <Popover
                              trigger="hover"
                              content={
                                <div style={{ minWidth: 180, minHeight: 48 }} />
                              }
                            >
                              <a
                                className="ticket-assignee-linklike"
                                onClick={(event) => {
                                  event.preventDefault();
                                  event.stopPropagation();
                                  applyAssigneeFilter(item.assignee?.id);
                                }}
                              >
                                {item.assignee?.full_name || "Не назначен"}
                              </a>
                            </Popover>
                          </div>
                        </div>
                      </div>
                      <div className="ticket-company-centered ticket-company-mobile">
                        <Popover
                          trigger="hover"
                          content={
                            <div style={{ minWidth: 180, minHeight: 48 }} />
                          }
                        >
                          <a
                            className="ticket-assignee-linklike"
                            onClick={(event) => {
                              event.preventDefault();
                              event.stopPropagation();
                            }}
                          >
                            {item.company_name || item.company_id}
                          </a>
                        </Popover>
                      </div>
                      <Paragraph
                        style={{ marginBottom: 0 }}
                        ellipsis={{ rows: 2 }}
                      >
                        {resolveTicketSubjectFromDescription(item.description)}
                      </Paragraph>
                      <Text type="secondary">
                        {item.reporter_name || "Сотрудник"} •{" "}
                        {resolveTicketCreatedSourceLabel(item.created_source)}
                      </Text>
                      {item.last_comment && (
                        <Paragraph
                          className="ticket-description-paragraph"
                          type="secondary"
                          style={{ marginBottom: 0 }}
                          ellipsis={{ rows: 3 }}
                        >
                          {normalizeDescription(item.last_comment)}
                        </Paragraph>
                      )}
                      <Space
                        style={{
                          width: "100%",
                          justifyContent: "space-between",
                        }}
                        wrap
                      >
                        <TicketDateStamp
                          label="Создано"
                          value={item.created_at}
                        />
                        <TicketDateStamp
                          label="Обновлено"
                          value={item.last_activity}
                        />
                      </Space>
                    </Space>
                  </Card>
                </Col>
              );
            })}
          </Row>
        )}
        {viewMode === "table" && (
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragEnd={handleDragEnd}
          >
            <SortableContext
              items={tableColumns.map((col) => col.key as string)}
              strategy={horizontalListSortingStrategy}
            >
              <Table<TableRow>
                dataSource={tableData}
                columns={tableColumns}
                rowKey="id"
                size="small"
                bordered
                className="tickets-table"
                style={{ width: "100%" }}
                tableLayout="fixed"
                scroll={{ x: tableScrollX }}
                pagination={false}
                components={{
                  header: {
                    cell: DraggableHeaderCell,
                  },
                }}
                rowClassName={(record) =>
                  selectedTicketIDs.includes(record.id)
                    ? "ant-table-row-selected"
                    : ""
                }
                onRow={(record) => ({
                  onClick: (event) => onTicketRowClick(record.id, event),
                  style: { cursor: "pointer" },
                })}
              />
            </SortableContext>
          </DndContext>
        )}

        <div
          ref={loadMoreRef}
          style={{
            marginTop: 16,
            display: "flex",
            justifyContent: "center",
            minHeight: 40,
          }}
        >
          {(isFetchingNextPage ||
            (hasNextPage && visibleTickets.length > 0)) && (
            <Spin size="small" />
          )}
          {!hasNextPage && visibleTickets.length > 0 && (
            <Text type="secondary">
              Показано: {visibleTickets.length} из {total}
            </Text>
          )}
        </div>
      </Card>

      <Drawer
        open={Boolean(selectedTicketId)}
        onClose={closeQuickModal}
        closable={false}
        width={656}
        title={
          metadata ? (
            <div
              style={{
                display: "grid",
                alignItems: "center",
                gridTemplateColumns: "1fr auto 1fr",
                gap: 8,
              }}
            >
              <span>Быстрый просмотр #{metadata.number}</span>
              {metadata.company_id ? (
                <Link
                  to={`/companies/${metadata.company_id}`}
                  onClick={closeQuickModal}
                >
                  {companyTitle}
                </Link>
              ) : (
                <span />
              )}
              <span />
            </div>
          ) : (
            "Быстрый просмотр заявки"
          )
        }
        placement="right"
        mask={false}
      >
        {isDetailsLoading || !details || !metadata ? (
          <div style={{ padding: 24, textAlign: "center" }}>
            <Spin />
          </div>
        ) : (
          <Space direction="vertical" size="middle" style={{ width: "100%" }}>
            <Space wrap>
              {metadata.is_archived ? (
                <Button
                  type="primary"
                  loading={changeStatusMutation.isPending}
                  onClick={() => {
                    if (!selectedTicketId) return;
                    changeStatusMutation.mutate({
                      id: selectedTicketId,
                      status: "in_progress",
                    });
                  }}
                >
                  Вернуть в работу
                </Button>
              ) : (
                <Select
                  value={metadata.status}
                  options={TICKET_STATUS_OPTIONS.filter(
                    (item) => item.value !== "closed",
                  ).map((item) => ({ value: item.value, label: item.label }))}
                  style={{ width: 220 }}
                  onChange={(nextStatus: TicketStatus) => {
                    if (!selectedTicketId || nextStatus === metadata.status) {
                      return;
                    }
                    if (nextStatus === "deferred") {
                      setPendingStatus(nextStatus);
                      setPendingDeferredAt(
                        dayjs().add(1, "hour").toISOString(),
                      );
                      return;
                    }
                    if (nextStatus === "resolved") {
                      const hasComments = (details?.comments || []).length > 0;
                      if (!hasComments) {
                        setPendingStatus(nextStatus);
                        return;
                      }
                      changeStatusMutation.mutate({
                        id: selectedTicketId,
                        status: nextStatus,
                      });
                      return;
                    }
                    changeStatusMutation.mutate({
                      id: selectedTicketId,
                      status: nextStatus,
                    });
                  }}
                />
              )}
              <ExternalLinksBadges
                bitrixURL={
                  isBitrixEnabled ? metadata.bitrix_deal_url : undefined
                }
                pyrusURL={metadata.pyrus_task_url}
              />
              {metadata.status === "deferred" && (
                <Space size={4}>
                  <Tooltip
                    title={formatDeferredTooltip(metadata.deferred_until)}
                  ></Tooltip>
                  <Button
                    type="link"
                    size="small"
                    style={{ paddingInline: 0 }}
                    onClick={() => {
                      setPendingStatus("deferred");
                      setPendingDeferredAt(
                        metadata.deferred_until ||
                          dayjs().add(1, "hour").toISOString(),
                      );
                    }}
                  >
                    {metadata.deferred_until
                      ? `до ${formatDeferredDateTime(metadata.deferred_until)}`
                      : "установить время"}
                  </Button>
                </Space>
              )}
              <Text type="secondary">
                Исполнитель: {metadata.assignee?.full_name || "Не назначен"}
              </Text>
              <Button onClick={() => void toggleTicketSubscription()}>
                {ticketSubscriptions.includes(String(metadata.id))
                  ? "Отписаться"
                  : "Подписаться на тикет"}
              </Button>
              <Button
                onClick={() => {
                  if (!selectedTicketId) return;
                  navigate(`/tickets/${selectedTicketId}`);
                  closeQuickModal();
                }}
              >
                Открыть страницу
              </Button>
            </Space>

            <Card size="small" title="Описание">
              <SafeHtmlContent
                html={metadata.description || "<span>Нет описания</span>"}
                style={{ whiteSpace: "pre-wrap" }}
              />
            </Card>

            {isClosedLikeTicketStatus(metadata.status) &&
              Boolean((metadata.result || "").trim()) && (
                <Card size="small" title="Результат">
                  <SafeHtmlContent
                    html={metadata.result || ""}
                    style={{ whiteSpace: "pre-wrap" }}
                  />
                </Card>
              )}

            <Card size="small" title="Подключения">
              {isInfraLoading ? (
                <div style={{ textAlign: "center", padding: 12 }}>
                  <Spin />
                </div>
              ) : connections.length === 0 ? (
                <Text type="secondary">Подключений нет</Text>
              ) : (
                <List
                  dataSource={connections}
                  renderItem={(group) => (
                    <List.Item key={group.key}>
                      <Space
                        direction="vertical"
                        size={0}
                        style={{ width: "100%" }}
                      >
                        <a
                          href={group.entityPath}
                          target="_blank"
                          rel="noreferrer"
                          onClick={(event) => event.stopPropagation()}
                        >
                          <Text strong>{group.title}</Text>
                        </a>
                        {group.rows.map((row) => (
                          <Paragraph
                            key={`${group.key}-${row.label}-${row.value}`}
                            style={{ margin: 0 }}
                            copyable={
                              row.value
                                ? {
                                    text: row.value,
                                    onCopy: () => {
                                      if (!selectedTicketId || !row.value)
                                        return;
                                      copyConnectionMutation.mutate({
                                        id: selectedTicketId,
                                        label: row.label,
                                        value: row.value,
                                      });
                                    },
                                  }
                                : false
                            }
                          >
                            <Text type="secondary">{row.label}:</Text>{" "}
                            {row.value}
                          </Paragraph>
                        ))}
                      </Space>
                    </List.Item>
                  )}
                />
              )}
            </Card>

            <Card size="small" title="Комментарии">
              {commentsNewFirst && commentComposer}
              {comments.length > 0 && (
                <List
                  dataSource={comments}
                  renderItem={(item) => (
                    <List.Item key={item.id}>
                      <Space
                        direction="vertical"
                        size={2}
                        style={{ width: "100%" }}
                      >
                        <Space
                          size={8}
                          style={{
                            justifyContent: "space-between",
                            width: "100%",
                          }}
                          wrap
                        >
                          <Text type="secondary">
                            {item.author} • {item.date}
                          </Text>
                          <Space size={8}>
                            {item.isPrivate && (
                              <Tag color="orange">Приватный</Tag>
                            )}
                            {canManageComment(item.authorRaw) && (
                              <Button
                                type="link"
                                size="small"
                                onClick={() => {
                                  setEditingCommentID(item.id);
                                  setEditingCommentDraft(item.text || "");
                                }}
                              >
                                Редактировать
                              </Button>
                            )}
                            {canDeleteComment(item.authorRaw) && (
                              <Popconfirm
                                title="Удалить комментарий?"
                                okText="Удалить"
                                cancelText="Отмена"
                                onConfirm={() => {
                                  if (!selectedTicketId) return;
                                  deleteCommentMutation.mutate({
                                    id: selectedTicketId,
                                    commentUUID: item.id,
                                  });
                                }}
                              >
                                <Button
                                  type="link"
                                  size="small"
                                  danger
                                  loading={deleteCommentMutation.isPending}
                                >
                                  Удалить
                                </Button>
                              </Popconfirm>
                            )}
                          </Space>
                        </Space>
                        {editingCommentID === item.id ? (
                          <Space
                            direction="vertical"
                            size={8}
                            style={{ width: "100%" }}
                          >
                            <SmartTicketEditor
                              value={editingCommentDraft}
                              onChange={setEditingCommentDraft}
                              placeholder="Измените комментарий"
                              mentions={mentionOptions}
                              onImageUpload={uploadInlineImage}
                              onFileUpload={uploadInlineFile}
                              minHeight={96}
                            />
                            <Space>
                              <Button
                                type="primary"
                                loading={updateCommentMutation.isPending}
                                disabled={
                                  !hasEditorContent(editingCommentDraft) ||
                                  !selectedTicketId
                                }
                                onClick={() => {
                                  if (!selectedTicketId) return;
                                  updateCommentMutation.mutate({
                                    id: selectedTicketId,
                                    commentUUID: item.id,
                                    comment: editingCommentDraft,
                                  });
                                }}
                              >
                                Сохранить
                              </Button>
                              <Button
                                onClick={() => {
                                  setEditingCommentID("");
                                  setEditingCommentDraft("");
                                }}
                              >
                                Отмена
                              </Button>
                            </Space>
                          </Space>
                        ) : (
                          <SafeHtmlContent
                            html={item.text}
                            style={{ whiteSpace: "pre-wrap" }}
                          />
                        )}
                      </Space>
                    </List.Item>
                  )}
                />
              )}
              {!commentsNewFirst && commentComposer}
            </Card>
          </Space>
        )}
      </Drawer>

      <Drawer
        open={Boolean(pendingStatus)}
        onClose={() => {
          setPendingStatus(null);
          setStatusComment("");
          setPendingDeferredAt("");
        }}
        width={420}
        title={
          pendingStatus === "deferred" ? "Отложить заявку" : "Завершение заявки"
        }
        placement="right"
      >
        <Space direction="vertical" size="middle" style={{ width: "100%" }}>
          {pendingStatus === "deferred" ? (
            <DatePicker
              showTime
              style={{ width: "100%" }}
              format="DD.MM.YYYY HH:mm"
              value={pendingDeferredAt ? dayjs(pendingDeferredAt) : null}
              onChange={(value) =>
                setPendingDeferredAt(value ? value.toISOString() : "")
              }
              placeholder="Выберите дату и время"
            />
          ) : (
            <Input.TextArea
              rows={4}
              value={statusComment}
              onChange={(event) => setStatusComment(event.target.value)}
              placeholder="Опишите итог выполнения заявки"
            />
          )}
          <Button
            type="primary"
            loading={changeStatusMutation.isPending}
            disabled={
              pendingStatus === "deferred"
                ? !pendingDeferredAt
                : !statusComment.trim()
            }
            onClick={() => {
              if (!selectedTicketId || !pendingStatus) return;
              if (pendingStatus === "deferred") {
                if (!pendingDeferredAt) return;
                changeStatusMutation.mutate({
                  id: selectedTicketId,
                  status: pendingStatus,
                  deferredUntil: pendingDeferredAt,
                });
                return;
              }
              if (!statusComment.trim()) return;
              changeStatusMutation.mutate({
                id: selectedTicketId,
                status: pendingStatus,
                comment: statusComment.trim(),
              });
            }}
          >
            {pendingStatus === "deferred" ? "Отложить" : "Завершить заявку"}
          </Button>
        </Space>
      </Drawer>
      {isCreateOpen && (
        <Suspense fallback={null}>
          <LazyNewTicketModal
            open={isCreateOpen}
            onClose={() => {
              setIsCreateOpen(false);
            }}
            onCreated={() => {
              queryClient.invalidateQueries({ queryKey: ["tickets"] });
            }}
          />
        </Suspense>
      )}
    </Space>
  );
};

export default TicketsPage;
