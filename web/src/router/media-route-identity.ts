interface RouteRecordLike {
  path: string;
}

export interface MediaRouteKeyInput {
  path: string;
  fullPath: string;
  matched?: readonly RouteRecordLike[];
}

const STABLE_MEDIA_WORKSPACE_PATHS = new Set([
  "/media/overview",
  "/media/monitoring",
  "/media/ingress",
  "/media/recordings",
  "/media/nodes",
  "/media/scheduling",
  "/gb28181/zlm/overview",
  "/gb28181/zlm/nodes",
  "/gb28181/zlm/runtime",
  "/gb28181/zlm/streams",
  "/gb28181/zlm/sessions",
  "/gb28181/zlm/proxies",
  "/gb28181/zlm/ffmpeg-sources",
  "/gb28181/zlm/rtp-servers",
  "/gb28181/zlm/config",
  "/gb28181/zlm/scheduler",
  "/gb28181/zlm/scheduler/logs"
]);

const MEDIA_NODE_DETAIL_PATTERNS = new Set(["/gb28181/zlm/nodes/:id", "/media/nodes/:id"]);
const MEDIA_NODE_DETAIL_PATHS = [
  { pattern: "/gb28181/zlm/nodes/:id", path: /^\/gb28181\/zlm\/nodes\/\d+$/ },
  { pattern: "/media/nodes/:id", path: /^\/media\/nodes\/\d+$/ }
] as const;

const MEDIA_WORKBENCH_PATHS = new Set([
  "/media/overview",
  "/media/monitoring",
  "/media/ingress",
  "/media/nodes",
  "/media/scheduling"
]);

function routePath(value: string) {
  return value.split(/[?#]/, 1)[0];
}

export function resolveMediaWorkbenchTabGroup(pathOrFullPath: string): "media-workbench" | null {
  const path = routePath(pathOrFullPath);
  if (MEDIA_WORKBENCH_PATHS.has(path) || /^\/media\/nodes\/\d+$/.test(path)) return "media-workbench";
  return null;
}

export function resolveMediaRouteRenderKey(route: MediaRouteKeyInput): string {
  const matchedPath = route.matched?.at(-1)?.path;
  if (matchedPath && MEDIA_NODE_DETAIL_PATTERNS.has(matchedPath)) return matchedPath;
  if (matchedPath && STABLE_MEDIA_WORKSPACE_PATHS.has(matchedPath)) return matchedPath;
  if (STABLE_MEDIA_WORKSPACE_PATHS.has(route.path)) return route.path;
  for (const detail of MEDIA_NODE_DETAIL_PATHS) {
    if (detail.path.test(route.path)) return detail.pattern;
  }
  return route.fullPath;
}
