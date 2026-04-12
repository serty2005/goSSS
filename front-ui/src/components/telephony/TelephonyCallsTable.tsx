import React, { useDeferredValue, useEffect, useMemo, useState } from "react";
import {
  Alert,
  Button,
  Card,
  Checkbox,
  DatePicker,
  Empty,
  Input,
  Select,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { useQuery } from "@tanstack/react-query";
import { Link, useSearchParams } from "react-router-dom";
import dayjs, { type Dayjs } from "dayjs";
import { telephonyApi } from "@/api/telephony";
import { usersApi } from "@/api/users";
import type { TelephonyCallDTO, TelephonyCallListParams } from "@/types/api";

const { RangePicker } = DatePicker;
const { Title, Text } = Typography;

type Props = {
  mode: "user" | "admin";
  title: string;
  userId?: number;
};

const statusOptions = [
  { value: "incoming", label: "Входящий" },
  { value: "accepted", label: "Принят" },
  { value: "transferred", label: "Переведен" },
  { value: "outgoing", label: "Исходящий" },
  { value: "success", label: "Успешно" },
  { value: "completed", label: "Завершен" },
  { value: "missed", label: "Пропущен" },
  { value: "noanswer", label: "Без ответа" },
  { value: "busy", label: "Занято" },
  { value: "cancelled", label: "Отменен" },
  { value: "failed", label: "Ошибка" },
];

const statusMetaMap: Record<string, { label: string; color: string }> = {
  incoming: { label: "Входящий", color: "processing" },
  accepted: { label: "Принят", color: "gold" },
  transferred: { label: "Переведен", color: "purple" },
  outgoing: { label: "Исходящий", color: "default" },
  success: { label: "Успешно", color: "success" },
  completed: { label: "Завершен", color: "success" },
  missed: { label: "Пропущен", color: "error" },
  noanswer: { label: "Без ответа", color: "error" },
  busy: { label: "Занято", color: "warning" },
  cancelled: { label: "Отменен", color: "default" },
  failed: { label: "Ошибка", color: "error" },
};

const missedStatusMetaMap: Record<string, { label: string; color: string }> = {
  "1": { label: "Клиент перезвонил", color: "success" },
  "2": { label: "Мы перезвонили", color: "success" },
  "3": { label: "Без обратного звонка", color: "blue" },
  "4": { label: "Не удалось дозвониться", color: "blue" },
};

const resolveDirectionLabel = (direction?: string) => {
  const normalized = String(direction || "")
    .trim()
    .toLowerCase();
  return normalized === "out" || normalized === "outgoing"
    ? "Исходящий"
    : "Входящий";
};

const resolveStatusMeta = (status?: string) => {
  const normalized = String(status || "")
    .trim()
    .toLowerCase();
  return (
    statusMetaMap[normalized] || {
      label: status || "Неизвестно",
      color: "default",
    }
  );
};

const resolveMissedStatusMeta = (status?: string) => {
  const normalized = String(status || "").trim();
  return missedStatusMetaMap[normalized];
};

const formatDateTime = (value?: string) => {
  if (!value) return "-";
  const parsed = dayjs(value);
  return parsed.isValid() ? parsed.format("DD.MM.YYYY HH:mm:ss") : value;
};

const formatDuration = (value?: number) => {
  if (typeof value !== "number" || Number.isNaN(value) || value < 0) {
    return "-";
  }
  const hours = Math.floor(value / 3600);
  const minutes = Math.floor((value % 3600) / 60);
  const seconds = value % 60;
  if (hours > 0) {
    return `${hours}:${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
  }
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
};

const buildDefaultTelephonyPeriod = (): [Dayjs, Dayjs] => [
  dayjs().subtract(24, "hour"),
  dayjs(),
];

const parseTelephonyBooleanParam = (value: string | null) =>
  String(value || "").trim().toLowerCase() === "true";

const parseTelephonyNumberParam = (value: string | null) => {
  const parsed = Number(String(value || "").trim());
  return Number.isInteger(parsed) && parsed > 0 ? parsed : undefined;
};

const parseTelephonyCSVParam = (value: string | null) =>
  String(value || "")
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);

const parseTelephonyDateParam = (value: string | null) => {
  const parsed = dayjs(String(value || "").trim());
  return parsed.isValid() ? parsed : null;
};

const buildPeriodFromSearchParams = (searchParams: URLSearchParams) => {
  const startedFrom = parseTelephonyDateParam(searchParams.get("started_from"));
  const startedTo = parseTelephonyDateParam(searchParams.get("started_to"));
  if (startedFrom && startedTo) {
    return [startedFrom, startedTo] as [Dayjs, Dayjs];
  }
  return buildDefaultTelephonyPeriod();
};

const TelephonyCallsTable: React.FC<Props> = ({ mode, title, userId }) => {
  const [searchParams] = useSearchParams();
  const searchParamsKey = searchParams.toString();
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [employeeUserID, setEmployeeUserID] = useState<number | undefined>();
  const [clientPhoneInput, setClientPhoneInput] = useState("");
  const [statuses, setStatuses] = useState<string[]>([]);
  const [groupNames, setGroupNames] = useState<string[]>([]);
  const [period, setPeriod] = useState<[Dayjs | null, Dayjs | null] | null>(
    () => buildDefaultTelephonyPeriod(),
  );
  const [onlyMissed, setOnlyMissed] = useState(false);
  const [onlyWithoutTicket, setOnlyWithoutTicket] = useState(false);
  const deferredClientPhone = useDeferredValue(clientPhoneInput.trim());

  useEffect(() => {
    const nextSearchParams = new URLSearchParams(searchParamsKey);
    setEmployeeUserID(
      mode === "admin"
        ? parseTelephonyNumberParam(nextSearchParams.get("employee_user_id"))
        : undefined,
    );
    setClientPhoneInput(
      String(nextSearchParams.get("client_phone") || "").trim(),
    );
    setStatuses(parseTelephonyCSVParam(nextSearchParams.get("status")));
    setGroupNames(parseTelephonyCSVParam(nextSearchParams.get("group_name")));
    setPeriod(buildPeriodFromSearchParams(nextSearchParams));
    setOnlyMissed(
      parseTelephonyBooleanParam(nextSearchParams.get("only_missed")),
    );
    setOnlyWithoutTicket(
      parseTelephonyBooleanParam(
        nextSearchParams.get("only_without_ticket"),
      ),
    );
    setPage(1);
    setPageSize(20);
  }, [mode, searchParamsKey]);

  useEffect(() => {
    setPage(1);
  }, [
    employeeUserID,
    deferredClientPhone,
    groupNames,
    statuses,
    period,
    onlyMissed,
    onlyWithoutTicket,
  ]);

  const params = useMemo<TelephonyCallListParams>(
    () => ({
      employee_user_id: mode === "admin" ? employeeUserID : undefined,
      client_phone: deferredClientPhone || undefined,
      status: statuses.length > 0 ? statuses : undefined,
      group_name: groupNames.length > 0 ? groupNames : undefined,
      started_from: period?.[0] ? period[0].toISOString() : undefined,
      started_to: period?.[1] ? period[1].toISOString() : undefined,
      only_missed: onlyMissed || undefined,
      only_without_ticket: onlyWithoutTicket || undefined,
      limit: pageSize,
      offset: (page - 1) * pageSize,
    }),
    [
      deferredClientPhone,
      employeeUserID,
      groupNames,
      mode,
      onlyMissed,
      onlyWithoutTicket,
      page,
      pageSize,
      period,
      statuses,
    ],
  );

  const { data: assigneesResponse } = useQuery({
    queryKey: ["users-assignees"],
    queryFn: () => usersApi.getAssignees(),
    enabled: mode === "admin",
    staleTime: 60_000,
  });

  const assigneeOptions = useMemo(
    () =>
      (assigneesResponse?.data || [])
        .filter((item) => item.is_active)
        .map((item) => ({
          value: item.id,
          label: item.full_name || item.username,
        })),
    [assigneesResponse?.data],
  );

  const { data, error, isFetching } = useQuery({
    queryKey: ["telephony", mode, userId ?? "all", params],
    queryFn: () =>
      mode === "admin"
        ? telephonyApi.getCalls(params)
        : telephonyApi.getUserCalls(userId ?? 0, params),
    enabled: mode === "admin" || Boolean(userId),
    staleTime: 15_000,
  });

  const groupOptions = useMemo(
    () =>
      [
        ...new Set(
          (data?.items || [])
            .map((item) => String(item.group_name || "").trim())
            .filter(Boolean),
        ),
      ]
        .sort((a, b) => a.localeCompare(b, "ru"))
        .map((value) => ({ value, label: value })),
    [data?.items],
  );

  const columns = useMemo<ColumnsType<TelephonyCallDTO>>(() => {
    const baseColumns: ColumnsType<TelephonyCallDTO> = [
      {
        title: "Дата и время",
        dataIndex: "started_at",
        key: "started_at",
        width: 180,
        render: (_value, record) =>
          formatDateTime(
            record.started_at || record.answered_at || record.completed_at,
          ),
      },
      {
        title: "Направление",
        dataIndex: "direction",
        key: "direction",
        width: 120,
        render: (value) => <Tag>{resolveDirectionLabel(value)}</Tag>,
      },
      {
        title: "Статус",
        dataIndex: "status",
        key: "status",
        width: 140,
        render: (value) => {
          const meta = resolveStatusMeta(value);
          return <Tag color={meta.color}>{meta.label}</Tag>;
        },
      },
      {
        title: "Номер клиента",
        dataIndex: "client_phone",
        key: "client_phone",
        width: 160,
        render: (value) =>
          value ? <Text code>{value}</Text> : <Text type="secondary">-</Text>,
      },
      {
        title: "Сотрудник",
        key: "employee",
        width: 200,
        render: (_value, record) => {
          const label =
            record.employee_name || record.employee_login || "Не определен";
          if (mode !== "admin" || !record.employee_user_id) {
            return label;
          }
          return (
            <Button
              type="link"
              style={{ paddingInline: 0 }}
              onClick={() => setEmployeeUserID(record.employee_user_id)}
            >
              {label}
            </Button>
          );
        },
      },
      {
        title: "Длительность",
        dataIndex: "duration_seconds",
        key: "duration_seconds",
        width: 120,
        render: (value) => formatDuration(value),
      },
      {
        title: "Номер ВАТС",
        dataIndex: "vat_number",
        key: "vat_number",
        width: 140,
        render: (value) => value || "-",
      },
      {
        title: "Статус пропущенного",
        dataIndex: "missed_status",
        key: "missed_status",
        width: 200,
        render: (value) => {
          const meta = resolveMissedStatusMeta(value);
          return meta ? (
            <Tag color={meta.color}>{meta.label}</Tag>
          ) : (
            <Text type="secondary">-</Text>
          );
        },
      },
      {
        title: "Запись",
        key: "recording_url",
        width: 130,
        render: (_value, record) =>
          record.has_recording && record.recording_url ? (
            <a
              href={record.recording_url}
              target="_blank"
              rel="noopener noreferrer"
            >
              Открыть
            </a>
          ) : (
            <Text type="secondary">Нет</Text>
          ),
      },
      {
        title: "Тикет",
        key: "ticket_id",
        width: 120,
        render: (_value, record) =>
          record.ticket_id ? (
            <Link to={`/tickets/${record.ticket_id}`}>
              #{record.ticket_id.slice(0, 8)}
            </Link>
          ) : (
            <Text type="secondary">Нет</Text>
          ),
      },
    ];
    return baseColumns;
  }, [mode]);

  const resetFilters = () => {
    setEmployeeUserID(undefined);
    setClientPhoneInput("");
    setStatuses([]);
    setGroupNames([]);
    setPeriod(buildDefaultTelephonyPeriod());
    setOnlyMissed(false);
    setOnlyWithoutTicket(false);
    setPage(1);
    setPageSize(20);
  };

  return (
    <Space direction="vertical" size="middle" style={{ width: "100%" }}>
      <div>
        <Title level={4} style={{ margin: 0 }}>
          {title}
        </Title>
        <Text type="secondary">Всего звонков: {data?.total ?? 0}</Text>
      </div>

      <Card className="glass-panel">
        <Space direction="vertical" size="middle" style={{ width: "100%" }}>
          {error ? (
            <Alert
              type="error"
              showIcon
              message="Не удалось загрузить звонки"
              description={String(
                (error as { message?: string } | undefined)?.message ||
                  "Ошибка запроса",
              )}
            />
          ) : null}

          <Space wrap size={[12, 12]}>
            {mode === "admin" ? (
              <Select
                allowClear
                showSearch
                style={{ minWidth: 260 }}
                placeholder="Сотрудник"
                optionFilterProp="label"
                options={assigneeOptions}
                value={employeeUserID}
                onChange={(value) =>
                  setEmployeeUserID(
                    typeof value === "number" ? value : undefined,
                  )
                }
              />
            ) : null}

            <Input
              allowClear
              style={{ width: 220 }}
              placeholder="Номер клиента"
              value={clientPhoneInput}
              onChange={(event) => setClientPhoneInput(event.target.value)}
            />

            <Select
              mode="multiple"
              allowClear
              style={{ minWidth: 260 }}
              placeholder="Статусы"
              options={statusOptions}
              value={statuses}
              onChange={setStatuses}
            />

            <Select
              mode="multiple"
              allowClear
              showSearch
              style={{ minWidth: 260 }}
              placeholder="Отделы"
              optionFilterProp="label"
              options={groupOptions}
              value={groupNames}
              onChange={setGroupNames}
            />

            <RangePicker
              showTime
              value={period}
              onChange={(value) =>
                setPeriod(value ?? buildDefaultTelephonyPeriod())
              }
            />

            <Checkbox
              checked={onlyMissed}
              onChange={(event) => setOnlyMissed(event.target.checked)}
            >
              Только пропущенные
            </Checkbox>

            <Checkbox
              checked={onlyWithoutTicket}
              onChange={(event) => setOnlyWithoutTicket(event.target.checked)}
            >
              Только без тикета
            </Checkbox>

            <Button onClick={resetFilters}>Сбросить фильтры</Button>
          </Space>

          {isFetching && !data ? (
            <div style={{ textAlign: "center", padding: 32 }}>
              <Spin />
            </div>
          ) : (
            <Table<TelephonyCallDTO>
              rowKey="id"
              columns={columns}
              dataSource={data?.items || []}
              loading={isFetching}
              locale={{ emptyText: <Empty description="Звонки не найдены" /> }}
              pagination={{
                current: page,
                pageSize,
                total: data?.total || 0,
                showSizeChanger: true,
                pageSizeOptions: ["20", "50", "100"],
                onChange: (nextPage, nextPageSize) => {
                  setPage(nextPage);
                  setPageSize(nextPageSize);
                },
              }}
              scroll={{ x: 1600 }}
            />
          )}
        </Space>
      </Card>
    </Space>
  );
};

export default TelephonyCallsTable;
