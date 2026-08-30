import { computed, ref } from "vue";
import { defineStore } from "pinia";
import pinia from "@/store";
import { registerUserLogoutCleanup } from "./user";

export type ZLMContextNodeState = "active" | "maintenance" | "offline";

export interface ZLMContextNode {
  id: number;
  name: string;
  state: ZLMContextNodeState;
}

export interface ZLMRequestScope {
  nodeId: number;
  version: number;
  signal: AbortSignal;
}

export const ZLM_CONTEXT_STORAGE_KEY = "uvp:zlm:selected-node";

function parseNodeID(value: unknown): number | null {
  const candidate = Array.isArray(value) ? value[0] : value;
  if (typeof candidate === "number") return Number.isSafeInteger(candidate) && candidate > 0 ? candidate : null;
  if (typeof candidate !== "string" || !/^\d+$/.test(candidate.trim())) return null;
  const parsed = Number(candidate);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null;
}

function containsNode(nodes: ZLMContextNode[], id: number | null) {
  return id !== null && nodes.some(node => node.id === id);
}

export function resolveInitialZLMNodeID(nodes: ZLMContextNode[], queryNodeID?: unknown, storedNodeID?: unknown): number | null {
  const query = parseNodeID(queryNodeID);
  if (containsNode(nodes, query)) return query;
  const stored = parseNodeID(storedNodeID);
  if (containsNode(nodes, stored)) return stored;
  return nodes.find(node => node.state === "active")?.id ?? null;
}

function readStoredNodeID() {
  if (typeof window === "undefined") return null;
  try {
    return window.sessionStorage.getItem(ZLM_CONTEXT_STORAGE_KEY);
  } catch {
    return null;
  }
}

function writeStoredNodeID(nodeID: number | null) {
  if (typeof window === "undefined") return;
  try {
    if (nodeID === null) window.sessionStorage.removeItem(ZLM_CONTEXT_STORAGE_KEY);
    else window.sessionStorage.setItem(ZLM_CONTEXT_STORAGE_KEY, String(nodeID));
  } catch {
    // Storage may be unavailable in a restricted browser context; memory remains authoritative.
  }
}

/** Read only the safe, positive node id kept for legacy route compatibility. */
export function readStoredZLMNodeID(): number | null {
  return parseNodeID(readStoredNodeID());
}

function normalizedNodes(nodes: ZLMContextNode[]) {
  const seen = new Set<number>();
  return nodes.filter(node => {
    if (!Number.isSafeInteger(node.id) || node.id <= 0 || seen.has(node.id)) return false;
    seen.add(node.id);
    return true;
  });
}

export const useZLMContextStore = defineStore("zlm-context", () => {
  const visibleNodes = ref<ZLMContextNode[]>([]);
  const selectedNodeId = ref<number | null>(null);
  const version = ref(0);
  const trendRevision = ref(0);
  const dialogRevision = ref(0);
  const initialized = ref(false);
  let requestController: AbortController | null = null;

  const selectedNode = computed(() => visibleNodes.value.find(node => node.id === selectedNodeId.value) ?? null);

  function invalidatePending(createNext: boolean) {
    requestController?.abort();
    requestController = createNext ? new AbortController() : null;
    version.value += 1;
    trendRevision.value += 1;
    dialogRevision.value += 1;
  }

  function applySelection(nodeID: number | null) {
    if (selectedNodeId.value === nodeID) {
      writeStoredNodeID(nodeID);
      if (nodeID !== null && requestController === null) requestController = new AbortController();
      return;
    }
    selectedNodeId.value = nodeID;
    invalidatePending(nodeID !== null);
    writeStoredNodeID(nodeID);
  }

  function initialize(nodes: ZLMContextNode[], queryNodeID?: unknown) {
    const next = normalizedNodes(nodes);
    if (initialized.value) {
      reconcileVisibleNodes(next);
      return;
    }
    visibleNodes.value = next;
    initialized.value = true;
    applySelection(resolveInitialZLMNodeID(next, queryNodeID, readStoredZLMNodeID()));
  }

  function reconcileVisibleNodes(nodes: ZLMContextNode[]) {
    const next = normalizedNodes(nodes);
    visibleNodes.value = next;
    if (containsNode(next, selectedNodeId.value)) return;
    applySelection(next.find(node => node.state === "active")?.id ?? null);
  }

  function selectNode(nodeID: number) {
    if (!containsNode(visibleNodes.value, nodeID)) return false;
    applySelection(nodeID);
    return true;
  }

  function requestScope(): ZLMRequestScope | null {
    if (selectedNodeId.value === null) return null;
    if (requestController === null || requestController.signal.aborted) requestController = new AbortController();
    return { nodeId: selectedNodeId.value, version: version.value, signal: requestController.signal };
  }

  function isCurrent(scope: ZLMRequestScope) {
    return scope.nodeId === selectedNodeId.value && scope.version === version.value && !scope.signal.aborted;
  }

  function clearForLogout() {
    visibleNodes.value = [];
    selectedNodeId.value = null;
    initialized.value = false;
    invalidatePending(false);
    writeStoredNodeID(null);
  }

  return {
    visibleNodes,
    selectedNodeId,
    selectedNode,
    version,
    trendRevision,
    dialogRevision,
    initialized,
    initialize,
    reconcileVisibleNodes,
    selectNode,
    requestScope,
    isCurrent,
    clearForLogout
  };
});

registerUserLogoutCleanup(() => useZLMContextStore(pinia).clearForLogout());

export function useZLMContextStoreHook() {
  return useZLMContextStore(pinia);
}
