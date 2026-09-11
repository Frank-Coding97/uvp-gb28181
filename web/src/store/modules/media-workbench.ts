import { computed, ref, type Ref } from "vue";
import { defineStore } from "pinia";

import { listZLMNodes, type ZLMNode } from "@/api/gb28181-zlm";
import pinia from "@/store";
import { registerUserLogoutCleanup } from "./user";

export type MediaScope = "all" | number;

export interface MediaScopePolicy {
  allowAll?: boolean;
  requiresNode?: boolean;
}

export type MediaNodeCatalogNode = Pick<ZLMNode, "id" | "name" | "state"> &
  Partial<Omit<ZLMNode, "id" | "name" | "state">>;

export type ZLMNodeCatalogLoader = () => Promise<readonly MediaNodeCatalogNode[]>;

export interface ZLMNodeCatalogOptions {
  load?: ZLMNodeCatalogLoader;
  ttlMs?: number;
  now?: () => number;
}

export interface ZLMNodeCatalog {
  nodes: Ref<MediaNodeCatalogNode[]>;
  loading: Ref<boolean>;
  error: Ref<unknown>;
  loadedAt: Ref<number | null>;
  load: (force?: boolean) => Promise<MediaNodeCatalogNode[]>;
  refresh: () => Promise<MediaNodeCatalogNode[]>;
  clear: () => void;
}

export const MEDIA_WORKBENCH_VIEW_STORAGE_PREFIX = "uvp:media:view:";

const DEFAULT_NODE_CATALOG_TTL_MS = 5_000;
const WORKSPACE_KEY_PATTERN = /^[a-z][a-z0-9-]{0,63}$/;
const VIEW_MAX_LENGTH = 64;

function storageAvailable() {
  return typeof window !== "undefined" && window.sessionStorage !== undefined;
}

function workspaceStorageKey(workspace: string) {
  const key = workspace.replace(/^\/media\//, "").replace(/^\/+|\/+$/g, "");
  return WORKSPACE_KEY_PATTERN.test(key) ? `${MEDIA_WORKBENCH_VIEW_STORAGE_PREFIX}${key}` : null;
}

function normalizedView(value: unknown, allowedViews: readonly string[]): string | null {
  if (typeof value !== "string") return null;
  const valueText = value.trim();
  if (!valueText || valueText.length > VIEW_MAX_LENGTH || /[\u0000-\u001f\u007f]/.test(valueText)) return null;
  return allowedViews.includes(valueText) ? valueText : null;
}

function positiveNodeID(value: unknown): number | null {
  if (typeof value === "number") return Number.isSafeInteger(value) && value > 0 ? value : null;
  if (typeof value !== "string" || !/^\d+$/.test(value.trim())) return null;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null;
}

/** Normalize an all-node or explicit positive-node scope without choosing a fallback node. */
export function normalizeMediaScope(value: unknown): MediaScope | null {
  if (value === "all") return "all";
  return positiveNodeID(value);
}

export function resolveDefaultZLMNodeId(
  nodes: readonly MediaNodeCatalogNode[],
  ...candidates: unknown[]
): number | null {
  for (const candidate of candidates) {
    const nodeId = positiveNodeID(candidate);
    if (nodeId !== null && nodes.some(node => node.id === nodeId)) return nodeId;
  }
  return nodes.find(node => node.state === "active")?.id ?? nodes[0]?.id ?? null;
}

export function canUseMediaScope(value: unknown, policy: MediaScopePolicy = {}) {
  const scope = normalizeMediaScope(value);
  if (scope === null) return false;
  if (scope === "all") return policy.requiresNode !== true && policy.allowAll !== false;
  return true;
}

export function readStoredMediaView(workspace: string, allowedViews: readonly string[]): string | null {
  const key = workspaceStorageKey(workspace);
  if (!key || !storageAvailable()) return null;
  try {
    return normalizedView(window.sessionStorage.getItem(key), allowedViews);
  } catch {
    return null;
  }
}

export function resolveMediaView(
  workspace: string,
  queryView: unknown,
  allowedViews: readonly string[],
  defaultView: string
): string | null {
  return normalizedView(queryView, allowedViews)
    ?? readStoredMediaView(workspace, allowedViews)
    ?? normalizedView(defaultView, allowedViews)
    ?? allowedViews[0]
    ?? null;
}

function writeStoredMediaView(workspace: string, view: string, allowedViews: readonly string[]) {
  const key = workspaceStorageKey(workspace);
  const safeView = normalizedView(view, allowedViews);
  if (!key || !safeView || !storageAvailable()) return;
  try {
    window.sessionStorage.setItem(key, safeView);
  } catch {
    // Memory remains authoritative when browser storage is unavailable.
  }
}

function clearStoredMediaViews() {
  if (!storageAvailable()) return;
  try {
    for (let index = window.sessionStorage.length - 1; index >= 0; index -= 1) {
      const key = window.sessionStorage.key(index);
      if (key?.startsWith(MEDIA_WORKBENCH_VIEW_STORAGE_PREFIX)) window.sessionStorage.removeItem(key);
    }
  } catch {
    // Storage may be unavailable in a restricted browser context.
  }
}

async function loadZLMNodesFromAPI(): Promise<readonly MediaNodeCatalogNode[]> {
  const response = await listZLMNodes();
  if (response.code !== 0) throw new Error(response.message || "节点列表加载失败");
  return response.data?.list ?? [];
}

function normalizeCatalogNodes(nodes: readonly MediaNodeCatalogNode[]) {
  const seen = new Set<number>();
  const normalized: MediaNodeCatalogNode[] = [];
  for (const node of nodes) {
    if (!Number.isSafeInteger(node.id) || node.id <= 0 || seen.has(node.id)) continue;
    seen.add(node.id);
    normalized.push({ ...node });
  }
  return normalized;
}

/** A bounded, shared node-directory loader. The loader intentionally has no signal because listZLMNodes has none. */
export function createZLMNodeCatalog(options: ZLMNodeCatalogOptions = {}): ZLMNodeCatalog {
  const ttlMs = Math.max(0, options.ttlMs ?? DEFAULT_NODE_CATALOG_TTL_MS);
  const now = options.now ?? Date.now;
  const loadNodes = options.load ?? loadZLMNodesFromAPI;
  const nodes = ref<MediaNodeCatalogNode[]>([]);
  const loading = ref(false);
  const error = ref<unknown>(null);
  const loadedAt = ref<number | null>(null);
  let loaded = false;
  let epoch = 0;
  let inFlight: { epoch: number; promise: Promise<MediaNodeCatalogNode[]> } | null = null;

  function clear() {
    epoch += 1;
    nodes.value = [];
    loading.value = false;
    error.value = null;
    loadedAt.value = null;
    loaded = false;
  }

  function load(force = false): Promise<MediaNodeCatalogNode[]> {
    const currentTime = now();
    if (!force && loaded && loadedAt.value !== null && currentTime - loadedAt.value < ttlMs) {
      return Promise.resolve(nodes.value);
    }
    if (inFlight?.epoch === epoch) return inFlight.promise;

    const requestEpoch = epoch;
    loading.value = true;
    const promise = Promise.resolve()
      .then(() => loadNodes())
      .then(result => {
        if (requestEpoch !== epoch) return [];
        const next = normalizeCatalogNodes(result);
        nodes.value = next;
        loaded = true;
        loadedAt.value = now();
        error.value = null;
        return next;
      })
      .catch(cause => {
        if (requestEpoch === epoch) error.value = cause;
        throw cause;
      });

    inFlight = { epoch: requestEpoch, promise };
    void promise.finally(() => {
      if (inFlight?.promise === promise) inFlight = null;
      if (requestEpoch === epoch) loading.value = false;
    }).catch(() => undefined);
    return promise;
  }

  return {
    nodes,
    loading,
    error,
    loadedAt,
    load,
    refresh: () => load(true),
    clear
  };
}

/** All media workbenches reuse this instance so concurrent mounts share one request. */
export const sharedZLMNodeCatalog = createZLMNodeCatalog();

export function useZLMNodeCatalog() {
  return sharedZLMNodeCatalog;
}

export const useMediaWorkbenchStore = defineStore("media-workbench", () => {
  const scope = ref<MediaScope>("all");
  const views = ref<Record<string, string>>({});

  const nodeScope = computed(() => scope.value === "all" ? null : scope.value);

  function setScope(value: unknown, policy: MediaScopePolicy = {}) {
    if (!canUseMediaScope(value, policy)) return false;
    scope.value = normalizeMediaScope(value) as MediaScope;
    return true;
  }

  function resolveView(workspace: string, queryView: unknown, allowedViews: readonly string[], defaultView: string) {
    const view = resolveMediaView(workspace, queryView, allowedViews, defaultView);
    if (view !== null) views.value[workspace] = view;
    return view;
  }

  function setView(workspace: string, view: string, allowedViews: readonly string[]) {
    const safeView = normalizedView(view, allowedViews);
    if (!safeView) return false;
    views.value[workspace] = safeView;
    writeStoredMediaView(workspace, safeView, allowedViews);
    return true;
  }

  function clearForLogout() {
    scope.value = "all";
    views.value = {};
    clearStoredMediaViews();
    sharedZLMNodeCatalog.clear();
  }

  return { scope, nodeScope, views, setScope, resolveView, setView, clearForLogout };
});

registerUserLogoutCleanup(() => useMediaWorkbenchStore(pinia).clearForLogout());
