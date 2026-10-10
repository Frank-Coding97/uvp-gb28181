interface RouteRecordLike {
  path: string;
}

export interface MediaRouteKeyInput {
  path: string;
  fullPath: string;
  matched?: readonly RouteRecordLike[];
}

/** 流媒体管理在系统侧栏下只有这五个正典工作台入口。 */
const MEDIA_WORKBENCH_PATHS = new Set([
  "/media/overview",
  "/media/monitoring",
  "/media/ingress",
  "/media/nodes",
  "/media/scheduling"
]);

/** 节点详情挂在 /media/nodes 下，用固定 pattern 做 keep-alive 身份，避免按 id 反复重建。 */
const NODE_DETAIL_PATTERN = "/media/nodes/:id";
const NODE_DETAIL_PATH = /^\/media\/nodes\/\d+$/;

function routePath(value: string) {
  return value.split(/[?#]/, 1)[0];
}

export function resolveMediaWorkbenchTabGroup(pathOrFullPath: string): string | null {
  const path = routePath(pathOrFullPath);
  if (MEDIA_WORKBENCH_PATHS.has(path)) return path;
  if (NODE_DETAIL_PATH.test(path)) return "/media/nodes";
  return null;
}

export function resolveMediaRouteRenderKey(route: MediaRouteKeyInput): string {
  const matchedPath = route.matched?.at(-1)?.path;
  if (matchedPath === NODE_DETAIL_PATTERN) return NODE_DETAIL_PATTERN;
  if (matchedPath && MEDIA_WORKBENCH_PATHS.has(matchedPath)) return matchedPath;
  if (MEDIA_WORKBENCH_PATHS.has(route.path)) return route.path;
  if (NODE_DETAIL_PATH.test(route.path)) return NODE_DETAIL_PATTERN;
  return route.fullPath;
}
