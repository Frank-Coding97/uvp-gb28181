/**
 * useDeviceMgmtState — URL / localStorage / Pinia 三层状态保留(F1)
 *
 * tasks §6 F1 设计(Q8 决议):
 *
 *   Layer 1 — URL query(可分享 / 后退前进保留 / 团队协作贴链接)
 *     view / node / drawerType / page / pageSize / sort / status / vendor / q / ptz / nodeId
 *     - view 切换走 router.push(进入 history)
 *     - filter / sort 改动走 router.replace(不污染历史)
 *     - drawer 开关也用 ?node= 控制,但抽屉切换走 replace 不进 history
 *
 *   Layer 2 — localStorage(用户偏好,跨会话保留)
 *     列宽 / 列显隐 / 抽屉宽度 / 默认 pageSize / 默认 view
 *     - storage 事件跨标签同步
 *
 *   Layer 3 — Pinia store(会话级,刷新即丢)
 *     当前选中节点元数据 / 抽屉滚动位置 / hover 高亮(F2)
 *
 * 本 composable 主要负责 Layer 1+2 的双向绑定,Layer 3 由各 store 自治。
 */
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

type QueryMap = Record<string, string | (string | null)[] | null | undefined>;

// ============================================================
// Layer 2 — localStorage 持久化偏好
// ============================================================

interface DeviceMgmtPrefs {
  defaultView?: "list" | "card" | "map";
  defaultPageSize?: number;
  listColumns?: Record<string, { width?: number; hidden?: boolean }>;
  drawerWidth?: string; // CSS value e.g. "40%"
}

const STORAGE_KEY = "device-mgmt:prefs";

function readPrefs(): DeviceMgmtPrefs {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return {};
    return JSON.parse(raw) as DeviceMgmtPrefs;
  } catch {
    return {};
  }
}

function writePrefs(p: DeviceMgmtPrefs): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(p));
  } catch {
    // 配额满或私密模式 — 忽略
  }
}

// ============================================================
// Layer 1 — URL query helpers
// ============================================================

/** 标记走 replace 而非 push 的 query key(避免历史污染) */
const REPLACE_KEYS = new Set([
  "page",
  "pageSize",
  "sort",
  "status",
  "vendor",
  "q",
  "ptz",
  "node",
  "drawerType",
]);

/** view 改变走 push(可后退);其他改变走 replace */
function shouldReplace(changedKeys: string[]): boolean {
  return changedKeys.every(k => REPLACE_KEYS.has(k));
}

// ============================================================
// Main hook
// ============================================================

export function useDeviceMgmtState() {
  const route = useRoute();
  const router = useRouter();

  const prefs = ref<DeviceMgmtPrefs>(readPrefs());

  // 跨标签同步:监听 localStorage 变化
  function onStorage(e: StorageEvent) {
    if (e.key === STORAGE_KEY) {
      prefs.value = readPrefs();
    }
  }

  onMounted(() => {
    window.addEventListener("storage", onStorage);
  });

  onUnmounted(() => {
    window.removeEventListener("storage", onStorage);
  });

  // ---------- URL 读取(派生 computed) ----------

  const view = computed<"list" | "card" | "map">(() => {
    const v = route.query.view;
    if (v === "card" || v === "map") return v;
    return prefs.value.defaultView ?? "list";
  });

  const activeNodeId = computed<number | null>(() => {
    const v = route.query.node;
    if (typeof v !== "string") return null;
    const n = Number(v);
    return Number.isFinite(n) && n > 0 ? n : null;
  });

  const drawerOpen = computed(() => activeNodeId.value != null);

  const drawerType = computed<"channel" | "device" | "node">(() => {
    const v = route.query.drawerType;
    if (v === "channel" || v === "device") return v;
    return "node";
  });

  // ---------- 状态变更 actions ----------

  function patchQuery(patch: Record<string, string | number | null | undefined>) {
    const next: QueryMap = { ...route.query };
    const changed: string[] = [];
    for (const [k, v] of Object.entries(patch)) {
      if (v == null || v === "") {
        if (next[k] !== undefined) {
          delete next[k];
          changed.push(k);
        }
      } else {
        const sv = String(v);
        if (next[k] !== sv) {
          next[k] = sv;
          changed.push(k);
        }
      }
    }
    if (changed.length === 0) return;
    if (shouldReplace(changed)) {
      router.replace({ query: next });
    } else {
      router.push({ query: next });
    }
  }

  function setView(v: "list" | "card" | "map") {
    patchQuery({ view: v });
    // 顺手记 lastView 偏好
    updatePrefs({ defaultView: v });
  }

  function setNode(nodeId: number | null, type?: "channel" | "device" | "node") {
    patchQuery({
      node: nodeId,
      drawerType: nodeId != null ? type ?? "node" : null,
    });
  }

  function closeDrawer() {
    patchQuery({ node: null, drawerType: null });
  }

  function setFilter(filter: { status?: string | null; vendor?: string | null; q?: string | null; ptz?: 0 | 1 | null }) {
    patchQuery({
      status: filter.status,
      vendor: filter.vendor,
      q: filter.q,
      ptz: filter.ptz,
      // 改 filter 时回到第 1 页
      page: 1,
    });
  }

  function setSort(sort: string | null) {
    patchQuery({ sort });
  }

  function setPage(page: number) {
    patchQuery({ page });
  }

  // ---------- localStorage 偏好 actions ----------

  function updatePrefs(patch: Partial<DeviceMgmtPrefs>) {
    const next = { ...prefs.value, ...patch };
    prefs.value = next;
    writePrefs(next);
  }

  function updateListColumn(key: string, value: { width?: number; hidden?: boolean }) {
    const cols = { ...(prefs.value.listColumns ?? {}) };
    cols[key] = { ...(cols[key] ?? {}), ...value };
    updatePrefs({ listColumns: cols });
  }

  return {
    // URL Layer 1 reads
    view,
    activeNodeId,
    drawerOpen,
    drawerType,
    // URL Layer 1 writes
    setView,
    setNode,
    closeDrawer,
    setFilter,
    setSort,
    setPage,
    // localStorage Layer 2
    prefs,
    updatePrefs,
    updateListColumn,
  };
}
