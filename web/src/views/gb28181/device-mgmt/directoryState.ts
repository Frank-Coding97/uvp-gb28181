export type DirectoryView = "national" | "custom";

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
        selectedKey: { national: null, custom: null },
        expandedKeys: { national: [], custom: [] },
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

export function customGroupBatchActions(assetKind: "device" | "channel", selectedCount: number, selectedKey: string | null) {
    const canAdd = assetKind === "device" && selectedCount > 0;
    if (!canAdd) return { canAdd: false, removeGroupId: null };
    const match = selectedKey?.match(/^custom:group:([1-9]\d*)$/);
    return { canAdd: true, removeGroupId: match ? Number(match[1]) : null };
}
