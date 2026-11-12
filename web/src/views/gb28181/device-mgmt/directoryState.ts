import type { DirectoryNode } from "./api";

export type DirectoryView = "administrative" | "department" | "business";

export interface DirectoryState {
  view: DirectoryView;
  selectedKey: Record<DirectoryView, string | null>;
  expandedKeys: Record<DirectoryView, string[]>;
  treeKeyword: string;
  listReset: { page: 1; clearSelection: true };
}

const directoryViewStorageKey = "uvp.gb28181.device-mgmt.directory-view";
type DirectoryViewStorage = Pick<Storage, "getItem" | "setItem">;

function browserStorage(): DirectoryViewStorage | undefined {
  if (typeof window === "undefined") return undefined;
  try {
    return window.localStorage;
  } catch {
    return undefined;
  }
}

export function normalizeDirectoryView(value: string | null | undefined): DirectoryView {
  return value === "department" || value === "business" ? value : "administrative";
}

export function readDirectoryViewPreference(storage = browserStorage()): DirectoryView {
  try {
    return normalizeDirectoryView(storage?.getItem(directoryViewStorageKey));
  } catch {
    return "administrative";
  }
}

export function persistDirectoryViewPreference(view: DirectoryView, storage = browserStorage()): void {
  try {
    storage?.setItem(directoryViewStorageKey, view);
  } catch {
    // Storage may be unavailable in restricted browser contexts; the current view still works.
  }
}

export function createDirectoryState(view: DirectoryView = readDirectoryViewPreference()): DirectoryState {
  return {
    view,
    selectedKey: { administrative: null, department: null, business: null },
    expandedKeys: { administrative: [], department: [], business: [] },
    treeKeyword: "",
    listReset: { page: 1, clearSelection: true }
  };
}

export function switchDirectoryView(state: DirectoryState, view: DirectoryView): DirectoryState {
  return { ...state, view, listReset: { page: 1, clearSelection: true } };
}

export function selectDirectory(state: DirectoryState, key: string | null): DirectoryState {
  return {
    ...state,
    selectedKey: { ...state.selectedKey, [state.view]: key },
    listReset: { page: 1, clearSelection: true }
  };
}

export function directoryQuery(state: DirectoryState): { directoryView?: DirectoryView; directoryKey?: string } {
  const key = state.selectedKey[state.view];
  if (key) return { directoryView: state.view, directoryKey: key };
  // 业务目录为空时仍要把视图带给后端：空 key 表示没有匹配分组，不能退化成全量通道查询。
  return state.view === "business" ? { directoryView: "business" } : {};
}

export function findDirectoryNode(nodes: DirectoryNode[], key: string | null): DirectoryNode | null {
  if (!key) return null;
  for (const node of nodes) {
    if (node.key === key) return node;
    const child = findDirectoryNode(node.children || [], key);
    if (child) return child;
  }
  return null;
}
