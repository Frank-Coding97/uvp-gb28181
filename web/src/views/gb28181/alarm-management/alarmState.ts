import type { AlarmEntitySummary, AlarmQuery } from "./api";

const VIEW_PERMISSION = "gb28181:alarm:view";
const DELETE_PERMISSION = "gb28181:alarm:delete";
const ALL_PERMISSION = "*:*:*";

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

export function normalizeCurrentPageSelection(selectedIds: string[], currentPageIds: string[]): string[] {
  const currentPage = new Set(currentPageIds);
  return [...new Set(selectedIds)].filter(id => currentPage.has(id));
}

export function displayAlarmEntityName(
  entity: Pick<AlarmEntitySummary, "alias" | "name" | "code"> | null | undefined,
  fallback = "—"
): string {
  return entity?.alias?.trim() || entity?.name?.trim() || entity?.code?.trim() || fallback;
}
