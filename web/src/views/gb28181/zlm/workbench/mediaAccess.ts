import { MEDIA_PAGES, MEDIA_WORKSPACES } from "./mediaRoutes";

export interface MediaWorkspaceAccess {
  workspacePaths: string[];
  viewsByWorkspace: Record<string, string[]>;
}

export interface MediaWorkspaceAccessOptions {
  wildcard?: boolean;
}

const NODE_DETAIL_PATTERN = /^\/gb28181\/zlm\/nodes\/(?:\d+|:id)$/;
const pagePaths = new Set(MEDIA_PAGES.map(page => page.path));

const legacyCapabilityByPage: Readonly<Record<string, { workspace: string; views: readonly string[] }>> = {
  "/gb28181/zlm/overview": { workspace: "/media/overview", views: ["overview"] },
  "/gb28181/zlm/runtime": { workspace: "/media/monitoring", views: [] },
  "/gb28181/zlm/streams": { workspace: "/media/monitoring", views: ["streams"] },
  "/gb28181/zlm/sessions": { workspace: "/media/monitoring", views: ["sessions"] },
  "/gb28181/zlm/proxies": { workspace: "/media/ingress", views: ["pull", "push"] },
  "/gb28181/zlm/ffmpeg-sources": { workspace: "/media/ingress", views: ["ffmpeg"] },
  "/gb28181/zlm/rtp-servers": { workspace: "/media/ingress", views: ["rtp"] },
  "/gb28181/cloud-recordings": { workspace: "/media/recordings", views: ["files", "tasks"] },
  "/gb28181/recording-schedules": { workspace: "/media/recordings", views: ["plans"] },
  "/gb28181/zlm/nodes": { workspace: "/media/nodes", views: ["list"] },
  "/gb28181/zlm/config": { workspace: "/media/nodes", views: ["config"] },
  "/gb28181/zlm/scheduler": { workspace: "/media/scheduling", views: ["strategy"] },
  "/gb28181/zlm/scheduler/logs": { workspace: "/media/scheduling", views: ["logs"] }
};

const allLegacyViews: Readonly<Record<string, readonly string[]>> = {
  "/media/overview": ["overview"],
  "/media/monitoring": ["streams", "sessions"],
  "/media/ingress": ["pull", "push", "ffmpeg", "rtp"],
  "/media/recordings": ["files", "tasks", "plans"],
  "/media/nodes": ["list", "overview", "runtime", "config"],
  "/media/scheduling": ["strategy", "logs"]
};

function canonicalPage(path: string): string | undefined {
  if (NODE_DETAIL_PATTERN.test(path)) return "/gb28181/zlm/nodes";
  return pagePaths.has(path) ? path : undefined;
}

export function resolveMediaWorkspaceAccess(
  legacyPaths: Iterable<string>,
  options: MediaWorkspaceAccessOptions = {}
): MediaWorkspaceAccess {
  const visible = new Set<string>();
  const legacyViews = new Map<string, Set<string>>();

  if (options.wildcard) {
    MEDIA_PAGES.forEach(page => visible.add(page.path));
    MEDIA_WORKSPACES.forEach(workspace => legacyViews.set(workspace.path, new Set(allLegacyViews[workspace.path])));
  } else {
    for (const path of legacyPaths) {
      const page = canonicalPage(path);
      if (page) visible.add(page);
      const capability = legacyCapabilityByPage[page ?? path];
      if (!capability) continue;
      const views = legacyViews.get(capability.workspace) ?? new Set<string>();
      capability.views.forEach(view => views.add(view));
      if (NODE_DETAIL_PATTERN.test(path)) ["overview", "runtime", "config"].forEach(view => views.add(view));
      legacyViews.set(capability.workspace, views);
    }
  }

  const workspacePaths = MEDIA_PAGES.map(page => page.path).filter(path => visible.has(path));
  const viewsByWorkspace: Record<string, string[]> = {};
  workspacePaths.forEach(path => { viewsByWorkspace[path] = ["default"]; });
  for (const [workspace, views] of legacyViews) {
    viewsByWorkspace[workspace] = allLegacyViews[workspace].filter(view => views.has(view));
  }

  return { workspacePaths, viewsByWorkspace };
}

export function resolveFirstMediaWorkspace(
  legacyPaths: Iterable<string>,
  options: MediaWorkspaceAccessOptions = {}
): string | null {
  return resolveMediaWorkspaceAccess(legacyPaths, options).workspacePaths[0] ?? null;
}
