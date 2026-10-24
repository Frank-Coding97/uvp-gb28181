import type { PlayLifecycleQuery } from "@/api/gb28181";

export const PLAYBACK_LOG_PAGE_SIZE = 10;
export const PLAYBACK_LOG_PAGE_SIZE_OPTIONS = [10, 20, 50, 100];

export const PLAYBACK_MEDIA_STATE_DICT_CODE = "playback_media_state";
export const PLAYBACK_CLIENT_STATE_DICT_CODE = "playback_client_state";
export const PLAYBACK_STAGE_DICT_CODE = "playback_stage";
export const PLAYBACK_LIFECYCLE_STATE_DICT_CODE = "playback_lifecycle_state";
export const PLAYBACK_FACT_STATE_DICT_CODE = "playback_fact_state";
export const PLAYBACK_EVENT_SOURCE_DICT_CODE = "playback_event_source";
export const PLAYBACK_EVENT_DICT_CODE = "playback_event";

export interface PlaybackDictionaryItem {
  name?: string | null;
  value?: string | null;
  status?: number;
}

export function playbackDictionaryOptions(items: readonly PlaybackDictionaryItem[]) {
  return items
    .filter(item => item.status !== 0 && item.name && item.value)
    .map(item => ({ label: item.name as string, value: item.value as string }));
}

export function playbackDictionaryLabel(items: readonly PlaybackDictionaryItem[], value: string) {
  return playbackDictionaryOptions(items).find(option => option.value === value)?.label ?? value;
}

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

export type PlaybackStateTone = "success" | "warning" | "danger" | "info" | "neutral";

export function playbackStateTone(state: string): PlaybackStateTone {
  if (["ready", "first_frame", "confirmed", "completed"].includes(state)) return "success";
  if (["failed", "player_error"].includes(state)) return "danger";
  if (["in_progress"].includes(state)) return "info";
  if (["stale_in_progress"].includes(state)) return "warning";
  return "neutral";
}

export function playbackStateColor(state: string) {
  return {
    success: "var(--zlm-success-500)",
    warning: "var(--zlm-warn-500)",
    danger: "var(--zlm-danger-500)",
    info: "var(--zlm-brand-500)",
    neutral: "var(--zlm-text-4)"
  }[playbackStateTone(state)];
}
