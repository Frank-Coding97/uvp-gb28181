import type { PlayLifecycleQuery } from "@/api/gb28181";

export const PLAYBACK_LOG_PAGE_SIZE = 10;
export const PLAYBACK_LOG_PAGE_SIZE_OPTIONS = [10, 20, 50, 100];

export interface PlaybackLogFilters {
  deviceCode?: string;
  channelCode?: string;
  streamId?: string;
  nodeId?: number;
  lifecycleState?: string;
  mediaState?: string;
  clientState?: string;
  failureStage?: string;
  range?: string[];
}

export function createPlaybackLogQuery(filters: PlaybackLogFilters, page: number, pageSize: number): PlayLifecycleQuery {
  return {
    page,
    pageSize,
    deviceCode: filters.deviceCode || undefined,
    channelCode: filters.channelCode || undefined,
    streamId: filters.streamId || undefined,
    nodeId: filters.nodeId || undefined,
    lifecycleState: filters.lifecycleState || undefined,
    mediaState: filters.mediaState || undefined,
    clientState: filters.clientState || undefined,
    failureStage: filters.failureStage || undefined,
    from: filters.range?.[0] || undefined,
    to: filters.range?.[1] || undefined
  };
}

export function factStateLabel(state: string) {
  return (
    {
      confirmed: "已证实",
      failed: "失败",
      in_progress: "进行中",
      unknown: "未知",
      not_applicable: "不适用"
    }[state] ??
    state ??
    "未知"
  );
}

export function mediaStateLabel(state: string) {
  return { ready: "媒体已就绪", failed: "媒体失败", stopped: "媒体已停止", unknown: "媒体未知" }[state] ?? state;
}

export function clientStateLabel(state: string) {
  return { first_frame: "已见首帧", failed: "客户端失败", unknown: "客户端未知" }[state] ?? state;
}

export function stateColor(state: string) {
  if (["ready", "first_frame", "confirmed", "completed"].includes(state)) return "green";
  if (["failed", "player_error"].includes(state)) return "red";
  if (["in_progress"].includes(state)) return "blue";
  if (["stale_in_progress"].includes(state)) return "orange";
  return "gray";
}
