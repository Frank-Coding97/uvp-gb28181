/**
 * 设备管理页 - 目录树 store(C2)
 *
 * 责任:
 * - 缓存目录树节点(按 id 索引,避免重复请求)
 * - 提供 lazy children 加载(展开节点时拉子节点)
 * - 提供未处理 anomaly 数(AnomalyEntry 底部入口角标)
 * - 当前选中节点 id(由 URL ?node= 双向绑定)
 *
 * 与 plan §5.3 Pinia 三层模型呼应:本 store 属"会话级",刷新页面重新加载。
 */
import { defineStore } from "pinia";
import { ref, computed } from "vue";

import {
  getCatalogTreeRoots,
  getCatalogChildren,
  getAnomalyCount,
  type CatalogNode,
} from "../api/catalog";

export const useCatalogStore = defineStore("device-mgmt-catalog", () => {
  // ============================================================
  // state
  // ============================================================

  /** id → node */
  const nodes = ref<Map<number, CatalogNode>>(new Map());
  /** id → children 是否加载过(防重复请求) */
  const loadedChildren = ref<Set<number>>(new Set());
  /** id → 展开态 */
  const expanded = ref<Set<number>>(new Set());
  /** 根节点 id 列表 */
  const rootIds = ref<number[]>([]);

  const loadingRoots = ref(false);
  const loadingChildrenOf = ref<Set<number>>(new Set());

  const anomalyCount = ref(0);

  // ============================================================
  // getters
  // ============================================================

  const roots = computed<CatalogNode[]>(() => {
    const out: CatalogNode[] = [];
    for (const id of rootIds.value) {
      const n = nodes.value.get(id);
      if (n) out.push(n);
    }
    return out;
  });

  function childrenOf(parentId: number): CatalogNode[] {
    const out: CatalogNode[] = [];
    nodes.value.forEach((n: CatalogNode) => {
      if (n.parentId === parentId) out.push(n);
    });
    return out.sort((a, b) => a.sortOrder - b.sortOrder || a.id - b.id);
  }

  function isExpanded(id: number): boolean {
    return expanded.value.has(id);
  }

  function isLoadingChildren(id: number): boolean {
    return loadingChildrenOf.value.has(id);
  }

  // ============================================================
  // actions
  // ============================================================

  function upsertNodes(list: CatalogNode[]): void {
    const m = new Map(nodes.value);
    for (const n of list) {
      m.set(n.id, n);
    }
    nodes.value = m;
  }

  async function loadRoots(): Promise<void> {
    if (loadingRoots.value) return;
    loadingRoots.value = true;
    try {
      const r = await getCatalogTreeRoots();
      const list = r?.data?.list ?? [];
      upsertNodes(list);
      rootIds.value = list.map(n => n.id);
    } finally {
      loadingRoots.value = false;
    }
  }

  async function loadChildren(parentId: number): Promise<void> {
    if (loadedChildren.value.has(parentId)) return;
    if (loadingChildrenOf.value.has(parentId)) return;
    loadingChildrenOf.value = new Set([...loadingChildrenOf.value, parentId]);
    try {
      const r = await getCatalogChildren(parentId, { withMountCount: true });
      const list = r?.data?.list ?? [];
      upsertNodes(list);
      loadedChildren.value = new Set([...loadedChildren.value, parentId]);
    } finally {
      const s = new Set(loadingChildrenOf.value);
      s.delete(parentId);
      loadingChildrenOf.value = s;
    }
  }

  async function toggleExpand(id: number): Promise<void> {
    if (expanded.value.has(id)) {
      const s = new Set(expanded.value);
      s.delete(id);
      expanded.value = s;
      return;
    }
    expanded.value = new Set([...expanded.value, id]);
    await loadChildren(id);
  }

  async function loadAnomalyCount(): Promise<void> {
    try {
      const r = await getAnomalyCount();
      anomalyCount.value = r?.data?.count ?? 0;
    } catch {
      // 忽略,左侧角标失败不阻塞主流程
    }
  }

  return {
    nodes,
    rootIds,
    roots,
    expanded,
    loadingRoots,
    anomalyCount,
    childrenOf,
    isExpanded,
    isLoadingChildren,
    loadRoots,
    loadChildren,
    toggleExpand,
    loadAnomalyCount,
  };
});
