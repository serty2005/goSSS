import React, {
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
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
  Grid,
  Input,
  List,
  Popconfirm,
  Row,
  Select,
  Space,
  Spin,
  Tag,
  Tooltip,
  Typography,
  message,
  theme as antTheme,
} from "antd";
import { LinkOutlined } from "@ant-design/icons";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "react-router-dom";
import dayjs from "dayjs";
import { ticketsApi } from "@/api/tickets";
import { companiesApi } from "@/api/companies";
import { usersApi } from "@/api/users";
import { profileApi } from "@/api/profile";
import { useLayoutHeader } from "@/components/layout/LayoutHeaderContext";
import TelephonyLineIndicator from "@/components/telephony/TelephonyLineIndicator";
import TicketTable from "@/components/tickets/TicketTable";
import ManagerTransferModal, {
  ManagerTransferPayload,
} from "@/components/tickets/ManagerTransferModal";
import TicketContactsControl from "@/components/tickets/TicketContactsControl";
import { TicketDetailsDTO, TicketStatus } from "@/types/api";
import SmartTicketEditor from "@/features/tickets/editor/SmartTicketEditor";
import { hasEditorContent } from "@/features/tickets/editor/content";
import type { MentionOption } from "@/features/tickets/editor/mentions";
import { useAuthStore } from "@/store/authStore";
import { useTicketParamsStore } from "@/store/ticketParamsStore";
import { SafeHtmlContent } from "@/utils/safeHtml";
import { getTelephonyContactPhoneForCopy } from "@/utils/telephony";
import { getPrimaryTicketPhone, getPrimaryTicketTelegram } from "@/utils/ticketContacts";
import {
  getTicketStatusMeta,
  isClosedLikeTicketStatus,
  TICKET_ACTIVE_STATUS_VALUES,
  TICKET_STATUS_OPTIONS,
} from "@/constants/ticketStatus";
import i18n from "@/i18n/i18n";

const { Text, Paragraph } = Typography;
const { useBreakpoint } = Grid;
const LazyNewTicketModal = React.lazy(
  () => import("@/components/tickets/NewTicketModal"),
);

type ViewMode = "list" | "cards" | "table";

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
  return normalized || i18n.t("tickets:fallback.noDescription");
};

const resolveTicketCreatedSourceLabel = (source?: string) => {
  if (source === "ui") return i18n.t("tickets:sourceLabels.ui");
  if (source === "bitrix") return i18n.t("tickets:sourceLabels.bitrix");
  if (source === "servicedesk") return i18n.t("tickets:sourceLabels.servicedesk");
  if (source === "system") return i18n.t("tickets:sourceLabels.system");
  return i18n.t("tickets:fallback.unknown");
};

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

const encodeTableLayout = (columns: Array<{ key: string; width?: number }>) => {
  if (!columns.length) {
    return undefined;
  }
  return encodeURIComponent(JSON.stringify(columns));
};

const decodeTableLayout = (value: string) => {
  if (!value) {
    return undefined;
  }
  try {
    const parsed = JSON.parse(decodeURIComponent(value)) as Array<{ key?: unknown; width?: unknown }>;
    if (!Array.isArray(parsed)) {
      return undefined;
    }
    const columns = parsed
      .map((item) => ({
        key: String(item.key || '').trim(),
        width: typeof item.width === 'number' ? item.width : undefined,
      }))
      .filter((item) => item.key);
    return columns.length ? columns : undefined;
  } catch {
    return undefined;
  }
};

const formatDateStamp = (value?: string) => ({
  date: value ? dayjs(value).format("DD.MM.YYYY") : "-",
  time: value ? dayjs(value).format("HH:mm") : "--:--",
});

const formatActivityTime = (value?: string) => {
  if (!value) return "--:--";
  const activity = dayjs(value);
  if (!activity.isValid()) return "--:--";
  const now = dayjs();
  if (activity.isSame(now, "day")) {
    return activity.format("HH:mm");
  }
  if (activity.isSame(now.subtract(1, "day"), "day")) {
    return `вчера ${activity.format("HH:mm")}`;
  }
  return activity.format("DD.MM HH:mm");
};

const formatDeferredDateTime = (value?: string) => {
  if (!value) return "";
  const dt = dayjs(value);
  if (!dt.isValid()) return "";
  return dt.format("DD.MM.YYYY HH:mm");
};

const formatDeferredTooltip = (value?: string) => {
  const formatted = formatDeferredDateTime(value);
  return formatted
    ? i18n.t("tickets:deferred.until", { value: formatted })
    : "";
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
        title={i18n.t("tickets:externalLinks.openBitrix")}
        color="success"
        compact={compact}
        onClick={onClick}
      />
      <ExternalLinkBadge
        label="Pyrus"
        href={pyrusURL}
        title={i18n.t("tickets:externalLinks.openPyrus")}
        color="geekblue"
        compact={compact}
        onClick={onClick}
      />
    </Space>
  );
};

const TicketsPage: React.FC = () => {
  const { t } = useTranslation(["common", "layout", "tickets"]);
  const { token } = antTheme.useToken();
  const screens = useBreakpoint();
  const isMobile = !screens.md;
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
  const isCommentAuthor = (comment?: {
    authorRaw?: string;
    authorUserID?: number;
  }) => {
    const authorUserID = Number(comment?.authorUserID || 0);
    if (authorUserID > 0 && user?.id) {
      return authorUserID === user.id;
    }
    return String(comment?.authorRaw || "").trim() === String(user?.full_name || "").trim();
  };
  const canManageComment = (comment?: {
    authorRaw?: string;
    authorUserID?: number;
  }) => isCommentAuthor(comment);
  const canDeleteComment = (comment?: {
    authorRaw?: string;
    authorUserID?: number;
  }) => isCommentAuthor(comment);

  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [selectedTicketId, setSelectedTicketId] = useState<string | null>(null);
  const [commentDraft, setCommentDraft] = useState("");
  const [commentIsPrivate, setCommentIsPrivate] = useState(false);
  const [editingCommentID, setEditingCommentID] = useState("");
  const [editingCommentDraft, setEditingCommentDraft] = useState("");
  const [statusComment, setStatusComment] = useState("");
  const [pendingStatus, setPendingStatus] = useState<TicketStatus | null>(null);
  const [pendingDeferredAt, setPendingDeferredAt] = useState<string>("");

  const resetPendingStatusState = useCallback(() => {
    setPendingStatus(null);
    setStatusComment("");
    setPendingDeferredAt("");
  }, []);

  const q = searchParams.get("q") || "";
  const status = searchParams.get("status") || "";
  const tableColumnsParam = searchParams.get("table_columns") || "";
  const tableSortParam = searchParams.get("table_sort") || "";
  const tableLayoutParam = searchParams.get("table_layout") || "";
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
  const periodFromParamKey =
    archiveMode === "archive" ? "archive_period_from" : "period_from";
  const periodToParamKey =
    archiveMode === "archive" ? "archive_period_to" : "period_to";
  const viewMode = (isMobile ? "cards" : "table") as ViewMode;
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
  const activeFilterCount = useMemo(
    () =>
      [
        q,
        status,
        onlyActiveStatuses ? "active" : "",
        assigneeIDs,
        company,
        periodFrom,
        periodTo,
        archiveMode === "archive" ? "archive" : "",
      ].filter(Boolean).length,
    [
      archiveMode,
      assigneeIDs,
      company,
      onlyActiveStatuses,
      periodFrom,
      periodTo,
      q,
      status,
    ],
  );
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
  const availableTableColumnKeys = useMemo<TableColumnKey[]>(
    () => (
      isBitrixEnabled
        ? [...TABLE_COLUMN_KEYS]
        : TABLE_COLUMN_KEYS.filter((key) => key !== "bitrix_deal_title" && key !== "sync_with_bitrix")
    ),
    [isBitrixEnabled],
  );
  const tableLayoutColumns = useMemo(() => decodeTableLayout(tableLayoutParam), [tableLayoutParam]);
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

  const { data, isLoading, isFetching, isFetchingNextPage, hasNextPage, fetchNextPage } =
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
  const isRefreshingTickets = isFetching && !isFetchingNextPage && !isLoading;
  const statusCounts = useMemo(() => {
    const counts = new Map<string, number>();
    visibleTickets.forEach((ticket) => {
      counts.set(ticket.status, (counts.get(ticket.status) || 0) + 1);
    });
    return counts;
  }, [visibleTickets]);

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
  const activeAssigneeOptions = useMemo(
    () =>
      (usersResponse?.data || [])
        .filter((item) => item.is_active)
        .map((item) => ({
          value: String(item.id),
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
          t("tickets:fallback.equipment");
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
  }, [infrastructure, t]);

  const comments = useMemo(() => {
    const sorted = [...(details?.comments || [])].sort((a, b) => {
      const delta =
        dayjs(a.creation_date).valueOf() - dayjs(b.creation_date).valueOf();
      return commentsNewFirst ? -delta : delta;
    });
    return sorted.map((item) => ({
      id: item.uuid,
      author: item.author_name || t("tickets:fallback.employee"),
      authorRaw: item.author_name || "",
      authorUserID: item.author_user_id,
      date: dayjs(item.creation_date).format("DD.MM.YYYY HH:mm"),
      text: item.text,
      isPrivate: item.is_private ?? false,
    }));
  }, [commentsNewFirst, details?.comments, t]);

  const changeStatusMutation = useMutation({
    mutationFn: async (payload: {
      id: string;
      status: TicketStatus;
      comment?: string;
      deferredUntil?: string;
    } & Partial<ManagerTransferPayload>) =>
      ticketsApi.changeStatus(payload.id, payload.status, {
        comment: payload.comment,
        deferredUntil: payload.deferredUntil,
        managerTransferTarget: payload.managerTransferTarget,
        clientContactType: payload.clientContactType,
        clientContactValue: payload.clientContactValue,
      }),
    onSuccess: () => {
      message.success(t("tickets:messages.statusUpdated"));
      resetPendingStatusState();
      queryClient.invalidateQueries({ queryKey: ["tickets"] });
      queryClient.invalidateQueries({ queryKey: ["ticket", selectedTicketId] });
    },
    onError: () => message.error(t("tickets:messages.statusUpdateError")),
  });

  const addCommentMutation = useMutation({
    mutationFn: async (payload: {
      id: string;
      comment: string;
      isPrivate: boolean;
    }) => ticketsApi.addComment(payload.id, payload.comment, payload.isPrivate),
    onSuccess: () => {
      message.success(t("tickets:messages.commentAdded"));
      setCommentDraft("");
      setCommentIsPrivate(false);
      queryClient.invalidateQueries({ queryKey: ["tickets"] });
      queryClient.invalidateQueries({ queryKey: ["ticket", selectedTicketId] });
    },
    onError: () => message.error(t("tickets:messages.commentAddError")),
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
      message.success(t("tickets:messages.commentUpdated"));
      setEditingCommentID("");
      setEditingCommentDraft("");
      queryClient.invalidateQueries({ queryKey: ["tickets"] });
      queryClient.invalidateQueries({ queryKey: ["ticket", selectedTicketId] });
    },
    onError: () => message.error(t("tickets:messages.commentUpdateError")),
  });

  const deleteCommentMutation = useMutation({
    mutationFn: async (payload: { id: string; commentUUID: string }) =>
      ticketsApi.deleteComment(payload.id, payload.commentUUID),
    onSuccess: () => {
      message.success(t("tickets:messages.commentDeleted"));
      setEditingCommentID("");
      setEditingCommentDraft("");
      queryClient.invalidateQueries({ queryKey: ["tickets"] });
      queryClient.invalidateQueries({ queryKey: ["ticket", selectedTicketId] });
    },
    onError: () => message.error(t("tickets:messages.commentDeleteError")),
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

  const closeQuickModal = useCallback(() => {
    setSelectedTicketId(null);
    setCommentDraft("");
    setCommentIsPrivate(false);
    setEditingCommentID("");
    setEditingCommentDraft("");
    resetPendingStatusState();
  }, [resetPendingStatusState]);

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
  }, [closeQuickModal, selectedTicketId]);

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

  const tableLayoutStorageKey = useMemo(() => {
    const userKey = user?.id ? String(user.id) : "guest";
    return `tickets-table-layout-${userKey}`;
  }, [user?.id]);
  const tableDateRangeValue = useMemo(
    () => (
      periodFrom || periodTo
        ? [
            periodFrom ? dayjs(periodFrom) : null,
            periodTo ? dayjs(periodTo) : null,
          ] as any
        : null
    ),
    [periodFrom, periodTo],
  );

  function updateTicketParams(next: Record<string, string | undefined>) {
    const params = new URLSearchParams(searchParams);
    Object.entries(next).forEach(([key, value]) => {
      if (!value) {
        params.delete(key);
      } else {
        params.set(key, value);
      }
    });
    params.set("page", "1");
    setSearchParamsRaw(params.toString());
  }

  function applyTableSort(key: TableSortKey) {
    const nextOrder: TableSortOrder | null =
      tableSort?.key !== key
        ? "asc"
        : tableSort.order === "asc"
          ? "desc"
          : null;
    const params = new URLSearchParams(searchParams);
    if (!nextOrder) {
      params.delete("table_sort");
    } else {
      params.set("table_sort", `${key}:${nextOrder}`);
    }
    params.set("page", "1");
    setSearchParamsRaw(params.toString());
  }

  const applyAssigneeFilter = (assigneeID?: number) => {
    if (!assigneeID) return;
    updateTicketParams({ assignee_ids: String(assigneeID) });
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
        exists
          ? t("tickets:messages.subscriptionDisabled")
          : t("tickets:messages.subscriptionEnabled"),
      );
    } catch {
      setUser(user);
      message.error(t("tickets:messages.subscriptionError"));
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
        placeholder={t("tickets:placeholders.addComment")}
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
        {t("tickets:labels.privateXenionOnly")}
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
        {t("tickets:actions.send")}
      </Button>
    </Space>
  );

  return (
    <Space direction="vertical" size="middle" style={{ width: "100%" }}>
      <Card className="tickets-workspace-card">
        {isMobile && (
          <section
            className="tickets-mobile-summary-bar"
            aria-label={t("layout:headerSearch.ticket.mobileListSummary")}
          >
            <div>
              <Text type="secondary" className="tickets-mobile-kicker">
                {t("layout:headerSearch.ticket.activeFilterCount", {
                  count: activeFilterCount,
                })}
              </Text>
              <Text strong className="tickets-mobile-total">
                {t("tickets:labels.showing", {
                  visible: visibleTickets.length,
                  total,
                })}
              </Text>
            </div>
            <Button
              type="primary"
              onClick={() => setIsCreateOpen(true)}
              aria-label={t("layout:headerSearch.ticket.newTicket")}
            >
              {t("layout:headerSearch.ticket.newTicket")}
            </Button>
          </section>
        )}
        {isRefreshingTickets && (
          <div
            style={{
              display: "inline-flex",
              alignItems: "center",
              gap: 8,
              minHeight: 28,
              marginBottom: 12,
              padding: "4px 10px",
              borderRadius: 8,
              border: `1px solid ${token.colorBorderSecondary}`,
              background: token.colorBgContainer,
            }}
          >
            <Spin size="small" />
            <Text type="secondary" style={{ fontSize: 12 }}>
              {t("tickets:labels.searching")}
            </Text>
          </div>
        )}
        {viewMode === "list" && (
          <List
            loading={{
              spinning: isLoading || isRefreshingTickets,
              tip: t("tickets:labels.searching"),
            }}
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
                          <Tag color="gold">{t("tickets:labels.paid")}</Tag>
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
                        {item.assignee?.full_name || t("tickets:fallback.unassigned")}
                      </Text>
                      <Text type="secondary">
                        {item.reporter_name || t("tickets:fallback.employee")} •{" "}
                        {resolveTicketCreatedSourceLabel(item.created_source)}
                      </Text>
                      <TicketDateStamp
                        label={t("tickets:labels.created")}
                        value={item.created_at}
                      />
                      <TicketDateStamp
                        label={t("tickets:labels.updated")}
                        value={item.last_activity}
                      />
                    </Space>
                  </Space>
                </List.Item>
              );
            }}
          />
        )}

        {viewMode === "cards" && isLoading && (
          <div style={{ display: "flex", justifyContent: "center", padding: 32 }}>
            <Spin />
          </div>
        )}

        {viewMode === "cards" && !isLoading && (
          <Row gutter={[12, 12]} className="tickets-mobile-list">
            {visibleTickets.map((item) => {
              const meta = getTicketStatusMeta(item.status);
              const deferredTitle =
                item.status === "deferred"
                  ? formatDeferredTooltip(item.deferred_until)
                  : "";
              const subject = resolveTicketSubjectFromDescription(
                item.description,
              );
              const lastComment = normalizeDescription(item.last_comment);
              const companyLabel =
                item.company_name ||
                item.company_id ||
                t("tickets:fallback.companyNotSpecified");
              const assigneeLabel =
                item.assignee?.full_name || t("tickets:fallback.unassigned");
              return (
                <Col key={item.id} xs={24} md={12} xl={8}>
                  <article className="ticket-mobile-card">
                    <button
                      type="button"
                      className="ticket-mobile-card__main"
                      aria-label={`Быстрый просмотр тикета #${item.number}`}
                      onClick={(event) =>
                        onTicketRowClick(
                          item.id,
                          event as unknown as React.MouseEvent,
                        )
                      }
                    >
                      <span className="ticket-mobile-card__top">
                        <span className="ticket-mobile-card__number">
                          #{item.number}
                        </span>
                        <span className="ticket-mobile-card__status">
                          {deferredTitle ? (
                            <Tooltip title={deferredTitle}>
                              <Tag color={meta.color}>{meta.label}</Tag>
                            </Tooltip>
                          ) : (
                            <Tag color={meta.color}>{meta.label}</Tag>
                          )}
                          {item.is_common_contract && (
                            <Tag color="gold">{t("tickets:labels.paid")}</Tag>
                          )}
                        </span>
                      </span>
                      <span className="ticket-mobile-card__subject">
                        {subject}
                      </span>
                      <span className="ticket-mobile-card__meta-grid">
                        <span>
                          <Text type="secondary">Компания</Text>
                          <Text strong>{companyLabel}</Text>
                        </span>
                        <span>
                          <Text type="secondary">
                            {t(
                              "layout:headerSearch.ticket.tableColumns.assignee_display",
                            )}
                          </Text>
                          <Text>{assigneeLabel}</Text>
                        </span>
                        <span>
                          <Text type="secondary">
                            {t("tickets:labels.updated")}
                          </Text>
                          <Text>{formatActivityTime(item.last_activity)}</Text>
                        </span>
                      </span>
                      <span className="ticket-mobile-card__comment">
                        <Text type="secondary">
                          {t(
                            "layout:headerSearch.ticket.tableColumns.last_comment",
                          )}
                        </Text>
                        <Text>
                          {lastComment || t("tickets:fallback.noComments")}
                        </Text>
                      </span>
                    </button>
                    <div className="ticket-mobile-card__actions">
                      <ExternalLinksBadges
                        bitrixURL={
                          isBitrixEnabled ? item.bitrix_deal_url : undefined
                        }
                        pyrusURL={item.pyrus_task_url}
                        compact
                        onClick={(event) => event.stopPropagation()}
                      />
                      <Space size={4} wrap>
                        {item.assignee?.id && (
                          <Button
                            size="small"
                            onClick={() => applyAssigneeFilter(item.assignee?.id)}
                          >
                            По исполнителю
                          </Button>
                        )}
                        <Button
                          size="small"
                          onClick={() => navigate(`/tickets/${item.id}`)}
                        >
                          {t("tickets:actions.openPage")}
                        </Button>
                      </Space>
                    </div>
                  </article>
                </Col>
              );
            })}
          </Row>
        )}
        {viewMode === "table" && (
          <TicketTable
            variant="workspace"
            dataSource={visibleTickets}
            total={total}
            loading={{
              spinning: isLoading || isRefreshingTickets,
              tip: t("tickets:labels.searching"),
            }}
            visibleColumnKeys={selectedTableColumnKeys}
            availableColumnKeys={availableTableColumnKeys}
            onVisibleColumnKeysChange={(keys) => {
              const normalized = availableTableColumnKeys.filter((key) => keys.includes(key));
              updateTicketParams({
                table_columns: normalized.length ? normalized.join(",") : undefined,
              });
            }}
            showPeriodFilters={false}
            showFooter={false}
            layoutStorage="local"
            layoutKey={tableLayoutStorageKey}
            layoutColumns={tableLayoutColumns}
            onLayoutChange={(columns) => {
              updateTicketParams({ table_layout: encodeTableLayout(columns) });
            }}
            sortState={tableSort}
            onSortChange={(columnKey) => applyTableSort(columnKey as TableSortKey)}
            columnFilters={{
              status: archiveMode === "archive"
                ? undefined
                : {
                    values: statusValues,
                    options: TICKET_STATUS_OPTIONS.map((item) => ({
                      value: item.value,
                      label: t(`layout:headerSearch.ticket.statusOptions.${item.value}`),
                      count: statusCounts.get(item.value) || 0,
                    })),
                    onChange: (values) => updateTicketParams({ status: values.length ? values.join(",") : undefined }),
                  },
              assignee: archiveMode === "archive"
                ? undefined
                : {
                    values: assigneeIDs ? assigneeIDs.split(",").filter(Boolean) : [],
                    options: activeAssigneeOptions,
                    ownValue: user?.id ? String(user.id) : undefined,
                    onChange: (values) => updateTicketParams({ assignee_ids: values.length ? values.join(",") : undefined }),
                  },
              created: {
                value: tableDateRangeValue,
                onChange: (value) => {
                  const from = value?.[0] ? value[0].startOf("day").format("YYYY-MM-DD") : undefined;
                  const to = value?.[1] ? value[1].endOf("day").format("YYYY-MM-DD") : from;
                  updateTicketParams({ [periodFromParamKey]: from, [periodToParamKey]: to });
                },
              },
              activity: {
                value: tableDateRangeValue,
                onChange: (value) => {
                  const from = value?.[0] ? value[0].startOf("day").format("YYYY-MM-DD") : undefined;
                  const to = value?.[1] ? value[1].endOf("day").format("YYYY-MM-DD") : from;
                  updateTicketParams({ [periodFromParamKey]: from, [periodToParamKey]: to });
                },
              },
            }}
            selectedTicketIds={selectedTicketIDs}
            onSelectedTicketIdsChange={setSelectedTicketIDs}
            rowClassName={(record) =>
              selectedTicketIDs.includes(record.id)
                ? "ant-table-row-selected"
                : ""
            }
            onRowClick={(record, event) => onTicketRowClick(record.id, event)}
          />
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
              {t("tickets:labels.showing", {
                visible: visibleTickets.length,
                total,
              })}
            </Text>
          )}
        </div>
      </Card>

      <Drawer
        className="ticket-quick-preview-drawer"
        open={Boolean(selectedTicketId)}
        onClose={closeQuickModal}
        closable
        width={isMobile ? undefined : "min(656px, 100vw)"}
        height={isMobile ? "min(60dvh, 480px)" : undefined}
        title={
          metadata ? (
            <div
              className="ticket-quick-preview-title"
              style={{
                display: "grid",
                alignItems: "center",
                gridTemplateColumns: isMobile
                  ? "minmax(0, 1fr)"
                  : "1fr auto 1fr",
                gap: 8,
              }}
            >
              <span>
                {t("tickets:titles.quickPreview")} #{metadata.number}
              </span>
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
            t("tickets:titles.quickPreviewTicket")
          )
        }
        placement={isMobile ? "bottom" : "right"}
        mask={isMobile}
      >
        {isDetailsLoading || !details || !metadata ? (
          <div style={{ padding: 24, textAlign: "center" }}>
            <Spin />
          </div>
        ) : (
          <Space direction="vertical" size="middle" style={{ width: "100%" }}>
            <div className="ticket-quick-preview-actions">
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
                  {t("tickets:actions.returnToWork")}
                </Button>
              ) : (
                <Select
                  value={metadata.status}
                  options={TICKET_STATUS_OPTIONS.filter(
                    (item) => item.value !== "closed",
                  ).map((item) => ({
                    value: item.value,
                    label: t(
                      `layout:headerSearch.ticket.statusOptions.${item.value}`,
                    ),
                  }))}
                  className="ticket-quick-preview-status"
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
                    if (nextStatus === "to_manager") {
                      setPendingStatus(nextStatus);
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
                      ? t("tickets:deferred.untilShort", {
                          value: formatDeferredDateTime(metadata.deferred_until),
                        })
                      : t("tickets:deferred.setTime")}
                  </Button>
                </Space>
              )}
              <Text type="secondary">
                {t("tickets:labels.assignee", {
                  name:
                    metadata.assignee?.full_name ||
                    t("tickets:fallback.unassigned"),
                })}
              </Text>
              <Button onClick={() => void toggleTicketSubscription()}>
                {ticketSubscriptions.includes(String(metadata.id))
                  ? t("tickets:actions.unsubscribe")
                  : t("tickets:actions.subscribe")}
              </Button>
              <Button
                onClick={() => {
                  if (!selectedTicketId) return;
                  navigate(`/tickets/${selectedTicketId}`);
                  closeQuickModal();
                }}
              >
                {t("tickets:actions.openPage")}
              </Button>
            </div>

            <Card size="small" title={t("tickets:cards.contact")}>
              <TicketContactsControl
                ticketId={selectedTicketId || undefined}
                contacts={details.contacts}
                legacyContact={details.contact}
                disabled={metadata.status === "to_manager"}
              />
            </Card>

            <Card size="small" title={t("tickets:cards.description")}>
              <SafeHtmlContent
                html={
                  metadata.description ||
                  t("tickets:fallback.noContactDescription")
                }
                style={{ whiteSpace: "pre-wrap" }}
              />
            </Card>

            {isClosedLikeTicketStatus(metadata.status) &&
              Boolean((metadata.result || "").trim()) && (
                <Card size="small" title={t("tickets:cards.result")}>
                  <SafeHtmlContent
                    html={metadata.result || ""}
                    style={{ whiteSpace: "pre-wrap" }}
                  />
                </Card>
              )}

            <Card size="small" title={t("tickets:cards.connections")}>
              {isInfraLoading ? (
                <div style={{ textAlign: "center", padding: 12 }}>
                  <Spin />
                </div>
              ) : connections.length === 0 ? (
                <Text type="secondary">{t("tickets:fallback.noConnections")}</Text>
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

            <Card size="small" title={t("tickets:cards.comments")}>
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
                              <Tag color="orange">{t("tickets:labels.private")}</Tag>
                            )}
                            {canManageComment(item) && (
                              <Button
                                type="link"
                                size="small"
                                onClick={() => {
                                  setEditingCommentID(item.id);
                                  setEditingCommentDraft(item.text || "");
                                }}
                              >
                                {t("tickets:actions.edit")}
                              </Button>
                            )}
                            {canDeleteComment(item) && (
                              <Popconfirm
                                title={t("tickets:titles.deleteComment")}
                                okText={t("common:actions.delete")}
                                cancelText={t("common:actions.cancel")}
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
                                  {t("tickets:actions.delete")}
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
                              placeholder={t("tickets:placeholders.editComment")}
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
                                {t("tickets:actions.save")}
                              </Button>
                              <Button
                                onClick={() => {
                                  setEditingCommentID("");
                                  setEditingCommentDraft("");
                                }}
                              >
                                {t("common:actions.cancel")}
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
        open={Boolean(pendingStatus && pendingStatus !== "to_manager")}
        onClose={resetPendingStatusState}
        width="min(420px, 100vw)"
        title={
          pendingStatus === "deferred"
            ? t("tickets:titles.deferTicket")
            : t("tickets:titles.completeTicket")
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
              placeholder={t("tickets:placeholders.selectDateTime")}
            />
          ) : (
            <Input.TextArea
              rows={4}
              value={statusComment}
              onChange={(event) => setStatusComment(event.target.value)}
              placeholder={t("tickets:placeholders.resolutionComment")}
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
            {pendingStatus === "deferred"
              ? t("tickets:actions.deferTicket")
              : t("tickets:actions.completeTicket")}
          </Button>
        </Space>
      </Drawer>
      <ManagerTransferModal
        open={pendingStatus === "to_manager"}
        initialTarget={details?.metadata.manager_transfer_target}
        initialContactPhone={getPrimaryTicketPhone(details?.contacts, getTelephonyContactPhoneForCopy(details?.contact))}
        initialContactTelegram={getPrimaryTicketTelegram(details?.contacts)}
        confirmLoading={changeStatusMutation.isPending}
        onCancel={resetPendingStatusState}
        onSubmit={(payload) => {
          if (!selectedTicketId) return;
          changeStatusMutation.mutate({
            id: selectedTicketId,
            status: "to_manager",
            ...payload,
          });
        }}
      />
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
