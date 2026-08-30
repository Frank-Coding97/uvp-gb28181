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
  "/media/scheduling"
]);

const MEDIA_NODE_DETAIL_PATTERN = "/media/nodes/:id";
const MEDIA_NODE_DETAIL_PATH = /^\/media\/nodes\/\d+$/;

export function resolveMediaRouteRenderKey(route: MediaRouteKeyInput): string {
  const matchedPath = route.matched?.at(-1)?.path;
  if (matchedPath === MEDIA_NODE_DETAIL_PATTERN) return MEDIA_NODE_DETAIL_PATTERN;
  if (matchedPath && STABLE_MEDIA_WORKSPACE_PATHS.has(matchedPath)) return matchedPath;
  if (STABLE_MEDIA_WORKSPACE_PATHS.has(route.path)) return route.path;
  if (MEDIA_NODE_DETAIL_PATH.test(route.path)) return MEDIA_NODE_DETAIL_PATTERN;
  return route.fullPath;
}
