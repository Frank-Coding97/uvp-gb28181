export interface MediaWorkspaceDefinition {
  key: "overview" | "monitoring" | "ingress" | "nodes" | "scheduling";
  title: string;
  path: string;
  sort: number;
  defaultView: string;
  allowedViews: readonly string[];
}

/**
 * Canonical two-level navigation: every entry maps to one real workbench page
 * mounted directly under the system sidebar, and owns its own local views.
 *
 * The former `/gb28181/zlm/*` direct pages and the `/media/recordings` entry
 * were retired together with their compatibility redirect layer — those menu
 * rows no longer exist, so no legacy path may be re-introduced here.
 */
export const MEDIA_WORKSPACES: readonly MediaWorkspaceDefinition[] = [
  {
    key: "overview",
    title: "运行总览",
    path: "/media/overview",
    sort: 10,
    defaultView: "overview",
    allowedViews: ["overview"]
  },
  {
    key: "monitoring",
    title: "流与会话",
    path: "/media/monitoring",
    sort: 20,
    defaultView: "streams",
    allowedViews: ["streams", "sessions", "viewers"]
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
    key: "nodes",
    title: "节点管理",
    path: "/media/nodes",
    sort: 40,
    defaultView: "list",
    allowedViews: ["list"]
  },
  {
    key: "scheduling",
    title: "调度日志",
    path: "/media/scheduling",
    sort: 50,
    defaultView: "logs",
    allowedViews: ["logs"]
  }
];
