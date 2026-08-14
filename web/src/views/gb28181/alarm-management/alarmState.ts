import type { AlarmEntitySummary, AlarmEnumValue, AlarmQuery } from "./api";

const VIEW_PERMISSION = "gb28181:alarm:view";
const DELETE_PERMISSION = "gb28181:alarm:delete";
const ALL_PERMISSION = "*:*:*";

export interface AlarmTypeOption {
  value: number;
  label: string;
}

const ALARM_TYPE_OPTIONS: Record<number, AlarmTypeOption[]> = {
  2: [
    { value: 1, label: "视频丢失报警" },
    { value: 2, label: "设备防拆报警" },
    { value: 3, label: "存储设备磁盘满报警" },
    { value: 4, label: "设备高温报警" },
    { value: 5, label: "设备低温报警" }
  ],
  5: [
    { value: 1, label: "人工视频报警" },
    { value: 2, label: "运动目标检测报警" },
    { value: 3, label: "遗留物检测报警" },
    { value: 4, label: "物体移除检测报警" },
    { value: 5, label: "绊线检测报警" },
    { value: 6, label: "入侵检测报警" },
    { value: 7, label: "逆行检测报警" },
    { value: 8, label: "徘徊检测报警" },
    { value: 9, label: "流量统计报警" },
    { value: 10, label: "密度检测报警" },
    { value: 11, label: "视频异常检测报警" },
    { value: 12, label: "快速移动报警" },
    { value: 13, label: "图像遮挡报警（2022）" }
  ],
  6: [
    { value: 1, label: "存储设备磁盘故障报警" },
    { value: 2, label: "存储设备风扇故障报警" }
  ]
};

export function alarmTypeOptionsForMethod(method: number | undefined): AlarmTypeOption[] {
  return method === undefined ? [] : ALARM_TYPE_OPTIONS[method] || [];
}

export function mayViewAlarms(permissions: string[]): boolean {
  return permissions.includes(ALL_PERMISSION) || permissions.includes(VIEW_PERMISSION);
}

export function mayDeleteAlarms(permissions: string[]): boolean {
  return permissions.includes(ALL_PERMISSION) || permissions.includes(DELETE_PERMISSION);
}

export function normalizeAlarmQuery(query: AlarmQuery): AlarmQuery {
  const sourceCode = query.sourceCode?.trim();
  const keyword = query.keyword?.trim();
  const alarmFrom = query.alarmFrom?.trim();
  const alarmTo = query.alarmTo?.trim();

  if (Boolean(alarmFrom) !== Boolean(alarmTo)) {
    throw new Error("告警时间范围必须同时选择开始和结束时间");
  }
  if (alarmFrom && alarmTo) {
    const fromTime = Date.parse(alarmFrom);
    const toTime = Date.parse(alarmTo);
    if (!Number.isFinite(fromTime) || !Number.isFinite(toTime) || fromTime > toTime) {
      throw new Error("告警时间范围无效");
    }
  }

  return {
    page: query.page,
    pageSize: query.pageSize,
    ...(query.deviceId !== undefined ? { deviceId: query.deviceId } : {}),
    ...(sourceCode ? { sourceCode } : {}),
    ...(alarmFrom && alarmTo ? { alarmFrom, alarmTo } : {}),
    ...(query.priority !== undefined ? { priority: query.priority } : {}),
    ...(query.method !== undefined ? { method: query.method } : {}),
    ...(query.alarmType !== undefined ? { alarmType: query.alarmType } : {}),
    ...(keyword ? { keyword } : {})
  };
}

export function pageAfterAlarmDeletion(page: number, pageSize: number, total: number, deletedCount: number): number {
  const remaining = Math.max(0, total - deletedCount);
  const lastPage = Math.max(1, Math.ceil(remaining / pageSize));
  return Math.min(Math.max(1, page), lastPage);
}

export function displayAlarmEntityName(
  entity: Pick<AlarmEntitySummary, "alias" | "name" | "code"> | null | undefined,
  fallback = "—"
): string {
  return entity?.alias?.trim() || entity?.name?.trim() || entity?.code?.trim() || fallback;
}

// 告警级别颜色：一级最严重，四级最轻。
const ALARM_PRIORITY_COLORS: Record<number, string> = {
  1: "red",
  2: "orange",
  3: "gold",
  4: "arcoblue"
};

export function alarmPriorityTagColor(priority: Pick<AlarmEnumValue, "value"> | null | undefined): string {
  const value = priority?.value;
  if (value == null) return "gray";
  return ALARM_PRIORITY_COLORS[value] || "gray";
}
