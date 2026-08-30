import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  MEDIA_WORKBENCH_VIEW_STORAGE_PREFIX,
  canUseMediaScope,
  createZLMNodeCatalog,
  normalizeMediaScope,
  readStoredMediaView,
  resolveMediaView,
  useMediaWorkbenchStore
} from "./media-workbench";

const nodes = [
  { id: 1, name: "主节点", state: "active" as const },
  { id: 2, name: "备用节点", state: "offline" as const }
];

describe("media workbench context", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    sessionStorage.clear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("singleflights node catalog requests and refreshes after a short TTL", async () => {
    let now = 100;
    const load = vi.fn(async () => [...nodes, nodes[0]]);
    const catalog = createZLMNodeCatalog({ load, ttlMs: 1_000, now: () => now });

    await Promise.all([catalog.load(), catalog.load(), catalog.load()]);
    expect(load).toHaveBeenCalledTimes(1);
    expect(catalog.nodes.value.map(node => node.id)).toEqual([1, 2]);
    await catalog.load();
    expect(load).toHaveBeenCalledTimes(1);

    now = 1_101;
    await catalog.load();
    expect(load).toHaveBeenCalledTimes(2);
    await catalog.load(true);
    expect(load).toHaveBeenCalledTimes(3);
  });

  it("does not let a cleared request repopulate the catalog after logout", async () => {
    let resolve!: (value: typeof nodes) => void;
    const load = vi.fn(() => new Promise<typeof nodes>(done => { resolve = done; }));
    const catalog = createZLMNodeCatalog({ load });
    const pending = catalog.load();
    await Promise.resolve();

    catalog.clear();
    resolve(nodes);
    await pending;

    expect(catalog.nodes.value).toEqual([]);
    expect(catalog.loading.value).toBe(false);
  });

  it("accepts all or an explicit positive node, but never invents a node for writes", () => {
    expect(normalizeMediaScope("all")).toBe("all");
    expect(normalizeMediaScope("2")).toBe(2);
    expect(normalizeMediaScope("0")).toBeNull();
    expect(normalizeMediaScope(["2"])).toBeNull();
    expect(canUseMediaScope("all", { allowAll: true })).toBe(true);
    expect(canUseMediaScope("all", { requiresNode: true })).toBe(false);
    expect(canUseMediaScope(2, { requiresNode: true })).toBe(true);
    expect(canUseMediaScope(null, { requiresNode: true })).toBe(false);
  });

  it("persists only allowlisted views and honors query before session before default", () => {
    const allowed = ["streams", "sessions"] as const;
    const workspace = "monitoring";

    expect(resolveMediaView(workspace, "unknown", allowed, "streams")).toBe("streams");
    expect(readStoredMediaView(workspace, allowed)).toBeNull();

    const store = useMediaWorkbenchStore();
    expect(store.setView(workspace, "sessions", allowed)).toBe(true);
    expect(sessionStorage.getItem(`${MEDIA_WORKBENCH_VIEW_STORAGE_PREFIX}${workspace}`)).toBe("sessions");
    expect(resolveMediaView(workspace, undefined, allowed, "streams")).toBe("sessions");
    expect(resolveMediaView("/media/monitoring", undefined, allowed, "streams")).toBe("sessions");
    expect(resolveMediaView(workspace, "streams", allowed, "sessions")).toBe("streams");
    expect(store.setView(workspace, "dangerous", allowed)).toBe(false);
    expect(sessionStorage.getItem(`${MEDIA_WORKBENCH_VIEW_STORAGE_PREFIX}${workspace}`)).toBe("sessions");

    store.clearForLogout();
    expect(sessionStorage.getItem(`${MEDIA_WORKBENCH_VIEW_STORAGE_PREFIX}${workspace}`)).toBeNull();
  });

  it("keeps a valid view in memory when session storage is unavailable", () => {
    const store = useMediaWorkbenchStore();
    const setItem = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("storage blocked");
    });

    expect(store.setView("monitoring", "streams", ["streams"])).toBe(true);
    expect(store.views.monitoring).toBe("streams");
    setItem.mockRestore();
  });
});
