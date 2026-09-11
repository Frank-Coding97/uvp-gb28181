import type {
  ZLMMediaIdentity,
  ZLMNetworkSessionQuery
} from "@/api/gb28181-zlm-runtime";

export interface NetworkSessionFilter {
  peerIp: string;
  localPort: string;
  page: number;
  pageSize: number;
}

export interface ViewerTargetForm {
  schema: string;
  vhost: string;
  app: string;
  stream: string;
}

function positiveInteger(value: unknown) {
  const parsed = typeof value === "number" ? value : Number(String(value ?? "").trim());
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null;
}

export function buildNetworkSessionQuery(values: NetworkSessionFilter): ZLMNetworkSessionQuery {
  const query: ZLMNetworkSessionQuery = {
    page: positiveInteger(values.page) ?? 1,
    pageSize: Math.min(100, positiveInteger(values.pageSize) ?? 20)
  };
  const peerIp = values.peerIp.trim();
  const localPort = positiveInteger(values.localPort);
  if (peerIp) query.peerIp = peerIp;
  if (localPort && localPort <= 65535) query.localPort = localPort;
  return query;
}

export function buildViewerTarget(values: ViewerTargetForm): ZLMMediaIdentity | null {
  const media = {
    schema: values.schema.trim(),
    vhost: values.vhost.trim(),
    app: values.app.trim(),
    stream: values.stream.trim()
  };
  return Object.values(media).every(Boolean) ? media : null;
}

export function canKickViewer(viewer: Pick<{ kickable: boolean }, "kickable">, hasPermission: boolean) {
  return viewer.kickable && hasPermission;
}
