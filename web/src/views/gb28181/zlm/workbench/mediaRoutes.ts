export interface MediaWorkspaceDefinition {
  key: "overview" | "monitoring" | "ingress" | "recordings" | "nodes" | "scheduling";
  title: string;
  path: string;
  sort: number;
  defaultView: string;
  allowedViews: readonly string[];
}

export interface MediaRouteLocation {
  path: string;
  query: Record<string, string>;
  replace: true;
}

export interface LegacyMediaRouteOptions {
  recentNodeId?: unknown;
}

export const MEDIA_WORKSPACES: readonly MediaWorkspaceDefinition[] = [
  {
    key: "overview",
    title: "媒体总览",
    path: "/media/overview",
    sort: 10,
    defaultView: "overview",
    allowedViews: ["overview"]
  },
  {
    key: "monitoring",
    title: "媒体监控",
    path: "/media/monitoring",
    sort: 20,
    defaultView: "streams",
    allowedViews: ["streams", "sessions"]
  },
  {
    key: "ingress",
    title: "接入管理",
    path: "/media/ingress",
    sort: 30,
    defaultView: "pull",
    allowedViews: ["pull", "push", "ffmpeg", "rtp"]
  },
  {
    key: "recordings",
    title: "录制中心",
    path: "/media/recordings",
    sort: 40,
    defaultView: "files",
    allowedViews: ["files", "tasks", "plans"]
  },
  {
    key: "nodes",
    title: "节点管理",
    path: "/media/nodes",
    sort: 50,
    defaultView: "list",
    allowedViews: ["list"]
  },
  {
    key: "scheduling",
    title: "调度管理",
    path: "/media/scheduling",
    sort: 60,
    defaultView: "strategy",
    allowedViews: ["strategy", "logs"]
  }
];

export const MEDIA_NODE_DETAIL = {
  path: "/media/nodes/:id",
  defaultView: "overview",
  allowedViews: ["overview", "runtime", "config"] as const
};

export const LEGACY_MEDIA_ROUTE_PATHS = [
  "/gb28181/zlm/overview",
  "/gb28181/zlm/runtime",
  "/gb28181/zlm/streams",
  "/gb28181/zlm/sessions",
  "/gb28181/zlm/proxies",
  "/gb28181/zlm/ffmpeg-sources",
  "/gb28181/zlm/rtp-servers",
  "/gb28181/cloud-recordings",
  "/gb28181/recording-schedules",
  "/gb28181/zlm/nodes",
  "/gb28181/zlm/nodes/:id",
  "/gb28181/zlm/config",
  "/gb28181/zlm/scheduler",
  "/gb28181/zlm/scheduler/logs"
] as const;

type QueryInput = Record<string, unknown>;
type QueryValidator = (value: unknown) => string | undefined;

const NODE_DETAIL_PATTERN = /^\/gb28181\/zlm\/nodes\/(\d+)$/;
const SAFE_TEXT_MAX_LENGTH = 128;

function scalarString(value: unknown): string | undefined {
  if (typeof value !== "string" && typeof value !== "number" && typeof value !== "boolean") {
    return undefined;
  }
  const normalized = String(value).trim();
  return normalized || undefined;
}

function safeText(value: unknown): string | undefined {
  const normalized = scalarString(value);
  if (!normalized || normalized.length > SAFE_TEXT_MAX_LENGTH) return undefined;
  if (/[:][/][/]|[\u0000-\u001f\u007f]/.test(normalized)) return undefined;
  return normalized;
}

function positiveInteger(value: unknown): string | undefined {
  const normalized = scalarString(value);
  if (!normalized || !/^\d+$/.test(normalized)) return undefined;
  const parsed = Number(normalized);
  return Number.isSafeInteger(parsed) && parsed > 0 ? String(parsed) : undefined;
}

function port(value: unknown): string | undefined {
  const normalized = positiveInteger(value);
  if (!normalized) return undefined;
  return Number(normalized) <= 65535 ? normalized : undefined;
}

function enumValue(...allowed: string[]): QueryValidator {
  return value => {
    const normalized = scalarString(value)?.toLowerCase();
    return normalized && allowed.includes(normalized) ? normalized : undefined;
  };
}

function token(value: unknown): string | undefined {
  const normalized = scalarString(value);
  if (!normalized || normalized.length > 64 || !/^[\w.-]+$/.test(normalized)) return undefined;
  return normalized;
}

function digits(value: unknown): string | undefined {
  const normalized = scalarString(value);
  return normalized && /^\d{1,20}$/.test(normalized) ? normalized : undefined;
}

function selectQuery(source: QueryInput, fields: readonly (readonly [string, QueryValidator])[]): Record<string, string> {
  const result: Record<string, string> = {};
  for (const [key, validate] of fields) {
    const normalized = validate(source[key]);
    if (normalized !== undefined) result[key] = normalized;
  }
  return result;
}

function destination(path: string, query: Record<string, string> = {}): MediaRouteLocation {
  return { path, query, replace: true };
}

const nodeField = ["nodeId", positiveInteger] as const;
const commonIdentityFields = [
  ["app", safeText],
  ["stream", safeText]
] as const;

export function resolveLegacyMediaRoute(
  path: string,
  query: QueryInput = {},
  options: LegacyMediaRouteOptions = {}
): MediaRouteLocation | null {
  const nodeDetail = path.match(NODE_DETAIL_PATTERN);
  if (nodeDetail) {
    const view = enumValue(...MEDIA_NODE_DETAIL.allowedViews)(query.view) ?? MEDIA_NODE_DETAIL.defaultView;
    return destination(`/media/nodes/${Number(nodeDetail[1])}`, { view });
  }
  if (path.startsWith("/gb28181/zlm/nodes/")) return null;

  switch (path) {
    case "/gb28181/zlm/overview":
      return destination("/media/overview", selectQuery(query, [nodeField, ["status", token], ["keyword", safeText]]));
    case "/gb28181/zlm/runtime": {
      const nodeId = positiveInteger(query.nodeId);
      return nodeId
        ? destination(`/media/nodes/${nodeId}`, { view: "runtime" })
        : destination("/media/overview", { focus: "runtime" });
    }
    case "/gb28181/zlm/streams":
      return destination("/media/monitoring", {
        view: "streams",
        ...selectQuery(query, [
          nodeField,
          ["schema", token],
          ["vhost", safeText],
          ...commonIdentityFields,
          ["originType", token],
          ["recording", enumValue("true", "false")],
          ["recordingMp4", enumValue("true", "false")],
          ["recordingHls", enumValue("true", "false")]
        ])
      });
    case "/gb28181/zlm/sessions":
      return destination("/media/monitoring", {
        view: "sessions",
        ...selectQuery(query, [nodeField, ["peerIp", safeText], ["localPort", port], ["type", token], ["identifier", safeText]])
      });
    case "/gb28181/zlm/proxies": {
      const view = enumValue("pull", "push")(query.view) ?? enumValue("pull", "push")(query.kind) ?? "pull";
      return destination("/media/ingress", {
        view,
        ...selectQuery(query, [nodeField, ...commonIdentityFields, ["status", token], ["keyword", safeText]])
      });
    }
    case "/gb28181/zlm/ffmpeg-sources":
      return destination("/media/ingress", {
        view: "ffmpeg",
        ...selectQuery(query, [nodeField, ...commonIdentityFields, ["status", token], ["keyword", safeText]])
      });
    case "/gb28181/zlm/rtp-servers":
      return destination("/media/ingress", {
        view: "rtp",
        ...selectQuery(query, [nodeField, ...commonIdentityFields, ["ssrc", digits]])
      });
    case "/gb28181/cloud-recordings":
      return destination("/media/recordings", {
        view: "files",
        ...selectQuery(query, [
          nodeField,
          ["file", safeText],
          ["deviceId", safeText],
          ["channelId", safeText],
          ["from", safeText],
          ["to", safeText],
          ["status", token]
        ])
      });
    case "/gb28181/recording-schedules":
      return destination("/media/recordings", {
        view: "plans",
        ...selectQuery(query, [nodeField, ["stream", safeText], ["status", token], ["keyword", safeText]])
      });
    case "/gb28181/zlm/nodes":
      return destination(
        "/media/nodes",
        selectQuery(query, [
          ["status", token],
          ["keyword", safeText]
        ])
      );
    case "/gb28181/zlm/config": {
      const nodeId = positiveInteger(query.nodeId) ?? positiveInteger(options.recentNodeId);
      return nodeId
        ? destination(`/media/nodes/${nodeId}`, { view: "config" })
        : destination("/media/nodes", { intent: "config" });
    }
    case "/gb28181/zlm/scheduler":
      return destination("/media/scheduling", {
        view: "strategy",
        ...selectQuery(query, [["algorithm", token]])
      });
    case "/gb28181/zlm/scheduler/logs":
      return destination("/media/scheduling", {
        view: "logs",
        ...selectQuery(query, [
          ["from", safeText],
          ["to", safeText],
          ["result", enumValue("success", "failure")],
          nodeField,
          ["algorithm", token],
          ["streamId", safeText]
        ])
      });
    default:
      return null;
  }
}
