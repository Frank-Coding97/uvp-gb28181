import type { DirectoryNode } from "./api";

export type DirectoryView = "national" | "administrative" | "business" | "custom";

export interface DirectoryState {
    view: DirectoryView;
    selectedKey: Record<DirectoryView, string | null>;
    expandedKeys: Record<DirectoryView, string[]>;
    treeKeyword: string;
    listReset: { page: 1; clearSelection: true };
}

export function createDirectoryState(): DirectoryState {
    return {
        view: "national",
        selectedKey: { national: null, administrative: null, business: null, custom: null },
        expandedKeys: { national: [], administrative: [], business: [], custom: [] },
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
    return key ? { directoryView: state.view, directoryKey: key } : {};
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

export function customGroupBatchActions(assetKind: "device" | "channel", selectedCount: number, selectedNode: DirectoryNode | null) {
    const canAdd = assetKind === "device" && selectedCount > 0;
    if (!canAdd) return { canAdd: false, removeGroupId: null };
    const match = selectedNode?.children?.length ? null : selectedNode?.key.match(/^custom:group:([1-9]\d*)$/);
    return { canAdd: true, removeGroupId: match ? Number(match[1]) : null };
}
