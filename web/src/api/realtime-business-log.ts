import { getAccessToken } from "@/utils/auth";

export type RealtimeLogLevel = "info" | "warn" | "error";
export interface RealtimeBusinessLogEvent {
  schemaVersion: string;
  eventId: string;
  instanceId: string;
  sequence: number;
  occurredAt: string;
  observedAt: string;
  level: RealtimeLogLevel;
  module: string;
  event: string;
  stage?: string;
  outcome?: string;
  requestId?: string;
  operationId?: string;
  correlationId?: string;
  deviceId?: string;
  channelId?: string;
  nodeId?: string;
  streamId?: string;
  callId?: string;
  reasonCode?: string;
  durationMs?: number;
  message: string;
  truncated?: boolean;
}

export interface RealtimeLogFilter {
  level?: string;
  module?: string;
  event?: string;
  deviceId?: string;
  channelId?: string;
  nodeId?: string;
  streamId?: string;
  callId?: string;
  requestId?: string;
  operationId?: string;
  correlationId?: string;
  since?: number;
}

export type RealtimeLogStreamItem =
  | { type: "ready"; data: { subscriptionId: string; instanceId?: string; latestSequence: number } }
  | { type: "message"; data: RealtimeBusinessLogEvent }
  | { type: "gap"; data: { reason: string; since?: number; latestSequence?: number } }
  | { type: "dropped"; data: { count: number } }
  | { type: "ping"; data: { at: string } }
  | { type: "auth_expired"; data: { reason: string } };

function streamUrl(filter: RealtimeLogFilter): string {
  const search = new URLSearchParams();
  Object.entries(filter).forEach(([key, value]) => {
    if (value !== undefined && value !== "") search.set(key, String(value));
  });
  const query = search.toString();
  return `/api/gb28181/logs/stream${query ? `?${query}` : ""}`;
}

export async function openRealtimeLogStream(
  filter: RealtimeLogFilter,
  onItem: (item: RealtimeLogStreamItem) => void,
  signal: AbortSignal
): Promise<void> {
  const token = getAccessToken()?.accessToken;
  const response = await fetch(streamUrl(filter), {
    method: "GET",
    headers: {
      Accept: "text/event-stream",
      ...(token ? { Authorization: `Bearer ${token}` } : {})
    },
    signal
  });
  if (!response.ok || !response.body) throw new Error(`实时日志连接失败(${response.status})`);
  const reader = response.body.getReader();
  signal.addEventListener("abort", () => reader.cancel().catch(() => undefined), { once: true });
  const decoder = new TextDecoder();
  let buffer = "";
  while (true) {
    const { value, done } = await reader.read();
    buffer += decoder.decode(value || new Uint8Array(), { stream: !done }).replace(/\r\n/g, "\n");
    let boundary = buffer.indexOf("\n\n");
    while (boundary >= 0) {
      const block = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);
      let type = "message";
      const data: string[] = [];
      block.split("\n").forEach(line => {
        if (line.startsWith("event:")) type = line.slice(6).trim();
        if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
      });
      if (data.length) onItem({ type, data: JSON.parse(data.join("\n")) } as RealtimeLogStreamItem);
      boundary = buffer.indexOf("\n\n");
    }
    if (done) break;
  }
}
