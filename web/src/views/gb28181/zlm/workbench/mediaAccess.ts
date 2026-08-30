import { MEDIA_WORKSPACES } from "./mediaRoutes";

export interface MediaWorkspaceAccess {
  workspacePaths: string[];
  viewsByWorkspace: Record<string, string[]>;
}

export interface MediaWorkspaceAccessOptions {
  wildcard?: boolean;
}

interface LegacyCapability {
  workspace: string;
  views: readonly string[];
}

const NODE_DETAIL_PATTERN = /^\/gb28181\/zlm\/nodes\/(?:\d+|:id)$/;

const capabilityByLegacyPath: Readonly<Record<string, LegacyCapability>> = {
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

const allViewsByWorkspace: Readonly<Record<string, readonly string[]>> = {
  "/media/overview": ["overview"],
  "/media/monitoring": ["streams", "sessions"],
  "/media/ingress": ["pull", "push", "ffmpeg", "rtp"],
  "/media/recordings": ["files", "tasks", "plans"],
  "/media/nodes": ["list", "overview", "runtime", "config"],
  "/media/scheduling": ["strategy", "logs"]
};

const nodeDetailCapability: LegacyCapability = {
  workspace: "/media/nodes",
  views: ["overview", "runtime", "config"]
};

function capabilityFor(path: string): LegacyCapability | undefined {
  if (NODE_DETAIL_PATTERN.test(path)) return nodeDetailCapability;
  return capabilityByLegacyPath[path];
}

export function resolveMediaWorkspaceAccess(
  legacyPaths: Iterable<string>,
  options: MediaWorkspaceAccessOptions = {}
): MediaWorkspaceAccess {
  const visible = new Set<string>();
  const grantedViews = new Map<string, Set<string>>();

  if (options.wildcard) {
    for (const workspace of MEDIA_WORKSPACES) {
      visible.add(workspace.path);
      grantedViews.set(workspace.path, new Set(allViewsByWorkspace[workspace.path]));
    }
  } else {
    for (const path of legacyPaths) {
      const capability = capabilityFor(path);
      if (!capability) continue;
      visible.add(capability.workspace);
      const views = grantedViews.get(capability.workspace) ?? new Set<string>();
      capability.views.forEach(view => views.add(view));
      grantedViews.set(capability.workspace, views);
    }
  }

  const workspacePaths = MEDIA_WORKSPACES.map(workspace => workspace.path).filter(path => visible.has(path));
  const viewsByWorkspace: Record<string, string[]> = {};
  for (const workspace of workspacePaths) {
    const granted = grantedViews.get(workspace) ?? new Set<string>();
    viewsByWorkspace[workspace] = allViewsByWorkspace[workspace].filter(view => granted.has(view));
  }

  return { workspacePaths, viewsByWorkspace };
}

export function resolveFirstMediaWorkspace(
  legacyPaths: Iterable<string>,
  options: MediaWorkspaceAccessOptions = {}
): string | null {
  return resolveMediaWorkspaceAccess(legacyPaths, options).workspacePaths[0] ?? null;
}
