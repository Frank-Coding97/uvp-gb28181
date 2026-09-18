import { MEDIA_WORKSPACES } from "./mediaRoutes";

export interface MediaWorkspaceAccess {
  workspacePaths: string[];
  viewsByWorkspace: Record<string, string[]>;
}

export interface MediaWorkspaceAccessOptions {
  wildcard?: boolean;
}

/** 节点详情是挂在 /media/nodes 下的隐藏子路由，按其父工作台授予视图。 */
const NODE_DETAIL_PATTERN = /^\/media\/nodes\/(?:\d+|:id)$/;

const allViewsByWorkspace: Readonly<Record<string, readonly string[]>> = {
  "/media/overview": ["overview"],
  "/media/monitoring": ["streams", "sessions", "viewers"],
  "/media/ingress": ["pull", "push", "ffmpeg", "rtp"],
  "/media/nodes": ["list", "overview", "runtime", "config"],
  "/media/scheduling": ["logs"]
};

function resolveWorkspacePath(path: string): string | undefined {
  if (NODE_DETAIL_PATTERN.test(path)) return "/media/nodes";
  return MEDIA_WORKSPACES.some(workspace => workspace.path === path) ? path : undefined;
}

export function resolveMediaWorkspaceAccess(
  grantedPaths: Iterable<string>,
  options: MediaWorkspaceAccessOptions = {}
): MediaWorkspaceAccess {
  const grantedViews = new Map<string, Set<string>>();

  if (options.wildcard) {
    MEDIA_WORKSPACES.forEach(workspace => grantedViews.set(workspace.path, new Set(allViewsByWorkspace[workspace.path])));
  } else {
    for (const path of grantedPaths) {
      const workspace = resolveWorkspacePath(path);
      if (!workspace) continue;
      grantedViews.set(workspace, new Set(allViewsByWorkspace[workspace]));
    }
  }

  const workspacePaths = MEDIA_WORKSPACES
    .map(workspace => workspace.path)
    .filter(path => (grantedViews.get(path)?.size ?? 0) > 0);
  const viewsByWorkspace: Record<string, string[]> = {};
  for (const [workspace, views] of grantedViews) {
    viewsByWorkspace[workspace] = allViewsByWorkspace[workspace].filter(view => views.has(view));
  }

  return { workspacePaths, viewsByWorkspace };
}

export function resolveFirstMediaWorkspace(
  grantedPaths: Iterable<string>,
  options: MediaWorkspaceAccessOptions = {}
): string | null {
  return resolveMediaWorkspaceAccess(grantedPaths, options).workspacePaths[0] ?? null;
}
