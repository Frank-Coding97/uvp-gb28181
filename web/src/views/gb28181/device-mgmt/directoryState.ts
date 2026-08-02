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
