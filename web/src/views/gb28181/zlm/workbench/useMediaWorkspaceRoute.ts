import { computed, onMounted, ref, watch } from "vue";
import { storeToRefs } from "pinia";
import { useRoute, useRouter } from "vue-router";

import { useRouteConfigStore } from "@/store/modules/route-config";
import {
  type MediaScope,
  resolveDefaultZLMNodeId,
  useMediaWorkbenchStore,
  useZLMNodeCatalog
} from "@/store/modules/media-workbench";
import { useZLMContextStore } from "@/store/modules/zlm-context";
import type { MediaWorkbenchStatus } from "./MediaWorkspaceShell.vue";
import { resolveMediaWorkspaceAccess } from "./mediaAccess";
import { MEDIA_WORKSPACES } from "./mediaRoutes";

export function useMediaWorkspaceRoute(key: typeof MEDIA_WORKSPACES[number]["key"]) {
  const definition = MEDIA_WORKSPACES.find(workspace => workspace.key === key)!;

  const route = useRoute();
  const router = useRouter();
  const routeStore = useRouteConfigStore();
  const workbenchStore = useMediaWorkbenchStore();
  const contextStore = useZLMContextStore();
  const catalog = useZLMNodeCatalog();
  const { routeList } = storeToRefs(routeStore);
  const { scope } = storeToRefs(workbenchStore);
  const activeView = ref("");
  const autoRefresh = ref(true);

  const allowedViews = computed(() => {
    const legacyPaths = routeList.value.map((item: Menu.MenuOptions) => item.path);
    const granted = resolveMediaWorkspaceAccess(legacyPaths).viewsByWorkspace[definition.path] ?? [];
    return definition.allowedViews.filter(view => granted.includes(view));
  });

  watch(
    [() => route.query.view, allowedViews],
    () => {
      activeView.value = workbenchStore.resolveView(
        definition.path,
        route.query.view,
        allowedViews.value,
        definition.defaultView
      ) ?? "";
    },
    { immediate: true }
  );

  const status = computed<MediaWorkbenchStatus>(() => {
    if (!allowedViews.value.length) return "denied";
    if (catalog.error.value && catalog.nodes.value.length) return "stale";
    if (catalog.error.value) return "error";
    return "ready";
  });

  const statusText = computed(() => {
    if (!allowedViews.value.length) return "当前账号没有该工作台的可用子视图。";
    if (catalog.error.value && catalog.nodes.value.length) return "节点目录刷新失败，继续显示上次成功结果。";
    if (catalog.error.value) return "节点目录暂不可用，可手动重试。";
    return "";
  });

  const lastSuccessAt = computed(() => {
    if (catalog.loadedAt.value === null) return null;
    return new Date(catalog.loadedAt.value).toLocaleString("zh-CN", { hour12: false });
  });

  function setActiveView(view: string) {
    if (!allowedViews.value.includes(view)) return false;
    workbenchStore.setView(definition.path, view, allowedViews.value);
    activeView.value = view;
    void router.replace({ path: definition.path, query: { ...route.query, view } });
    return true;
  }

  function setScope(next: MediaScope) {
    if (!workbenchStore.setScope(next)) return false;
    if (typeof next === "number") {
      contextStore.selectNode(next);
      void router.replace({ path: route.path, query: { ...route.query, nodeId: String(next) } });
    } else {
      contextStore.selectAll();
      const query = { ...route.query };
      delete query.nodeId;
      void router.replace({ path: route.path, query });
    }
    return true;
  }

  async function refreshScope() {
    try {
      const nodes = await catalog.refresh();
      contextStore.reconcileVisibleNodes(nodes);
    } catch {
      // Catalog state carries the retryable error and keeps the previous successful directory.
    }
  }

  onMounted(async () => {
    try {
      const nodes = await catalog.load();
      if (!contextStore.initialized) contextStore.initialize(nodes, route.query.nodeId);
      else contextStore.reconcileVisibleNodes(nodes);
      const nodeId = resolveDefaultZLMNodeId(nodes, route.query.nodeId, contextStore.selectedNodeId);
      if (nodeId !== null) setScope(nodeId);
    } catch {
      // The shared catalog exposes the recoverable error while retaining its last successful nodes.
    }
  });

  return {
    definition,
    activeView,
    allowedViews,
    autoRefresh,
    scope,
    nodes: catalog.nodes,
    scopeLoading: catalog.loading,
    scopeError: catalog.error,
    status,
    statusText,
    lastSuccessAt,
    setActiveView,
    setScope,
    refreshScope
  };
}
