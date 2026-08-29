import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";

import { resolveInitialZLMNodeID, useZLMContextStore, type ZLMContextNode } from "./zlm-context";

const nodes: ZLMContextNode[] = [
  { id: 1, name: "主节点", state: "active" },
  { id: 2, name: "离线节点", state: "offline" },
  { id: 3, name: "备用节点", state: "active" }
];

describe("ZLM node context", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    sessionStorage.clear();
  });

  it("resolves query, session and first visible active node in order", () => {
    expect(resolveInitialZLMNodeID(nodes, "3", "2")).toBe(3);
    expect(resolveInitialZLMNodeID(nodes, "99", "2")).toBe(2);
    expect(resolveInitialZLMNodeID(nodes, undefined, "99")).toBe(1);
    expect(resolveInitialZLMNodeID(nodes.filter(node => node.state !== "active"), undefined, undefined)).toBeNull();
  });

  it("retains an offline selection but falls back when it is no longer visible", () => {
    const store = useZLMContextStore();
    store.initialize(nodes, "2");
    expect(store.selectedNodeId).toBe(2);
    expect(store.selectedNode?.state).toBe("offline");

    store.reconcileVisibleNodes(nodes.map(node => node.id === 2 ? { ...node, state: "offline" as const } : node));
    expect(store.selectedNodeId).toBe(2);

    store.reconcileVisibleNodes(nodes.filter(node => node.id !== 2));
    expect(store.selectedNodeId).toBe(1);
  });

  it("aborts the old request and invalidates trend and dialog state on node switch", () => {
    const store = useZLMContextStore();
    store.initialize(nodes, "1");
    const first = store.requestScope();
    const trendRevision = store.trendRevision;
    const dialogRevision = store.dialogRevision;

    store.selectNode(3);
    const second = store.requestScope();

    expect(first?.signal.aborted).toBe(true);
    expect(first && store.isCurrent(first)).toBe(false);
    expect(second && store.isCurrent(second)).toBe(true);
    expect(store.trendRevision).toBeGreaterThan(trendRevision);
    expect(store.dialogRevision).toBeGreaterThan(dialogRevision);
    expect(sessionStorage.getItem("uvp:zlm:selected-node")).toBe("3");
  });

  it("clears selection, storage and pending work on logout", () => {
    const store = useZLMContextStore();
    store.initialize(nodes, "1");
    const scope = store.requestScope();

    store.clearForLogout();

    expect(scope?.signal.aborted).toBe(true);
    expect(store.selectedNodeId).toBeNull();
    expect(store.visibleNodes).toEqual([]);
    expect(sessionStorage.getItem("uvp:zlm:selected-node")).toBeNull();
  });
});
