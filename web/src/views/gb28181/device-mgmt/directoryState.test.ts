import { describe, expect, it } from "vitest";
import { createDirectoryState, customGroupBatchActions, directoryQuery, findDirectoryNode, selectDirectory, switchDirectoryView } from "./directoryState";

const leafGroup = { key: "custom:group:12", name: "叶子组", type: "group", readOnly: false, count: 1, onlineCount: 0, depth: 1, children: [] };
const parentGroup = { ...leafGroup, key: "custom:group:11", name: "父组", depth: 0, children: [leafGroup] };

describe("directoryState", () => {
    it("defaults to national and keeps view state separately", () => {
        let state = createDirectoryState();
        expect(state.view).toBe("national");
        state = selectDirectory(state, "national:area:370112");
        state = switchDirectoryView(state, "custom");
        state = selectDirectory(state, "custom:group:12");
        state = switchDirectoryView(state, "national");
        expect(state.selectedKey).toEqual({ national: "national:area:370112", custom: "custom:group:12" });
    });

    it("maps selected namespaced key to paired query params", () => {
        const state = selectDirectory(createDirectoryState(), "national:unknown");
        expect(directoryQuery(state)).toEqual({ directoryView: "national", directoryKey: "national:unknown" });
        expect(directoryQuery(createDirectoryState())).toEqual({});
    });

    it("reports list reset effects without owning playback state", () => {
        const result = switchDirectoryView(createDirectoryState(), "custom");
        expect(result.listReset).toEqual({ page: 1, clearSelection: true });
        expect(result).not.toHaveProperty("playing");
    });

    it("shows member actions only for selected devices and real custom groups", () => {
        expect(customGroupBatchActions("device", 2, null)).toEqual({ canAdd: true, removeGroupId: null });
        expect(customGroupBatchActions("device", 2, leafGroup)).toEqual({ canAdd: true, removeGroupId: 12 });
        expect(customGroupBatchActions("device", 2, parentGroup)).toEqual({ canAdd: true, removeGroupId: null });
        expect(customGroupBatchActions("channel", 2, leafGroup)).toEqual({ canAdd: false, removeGroupId: null });
        expect(customGroupBatchActions("device", 0, leafGroup)).toEqual({ canAdd: false, removeGroupId: null });
    });

    it("refreshes the selected node name from the latest tree", () => {
        expect(findDirectoryNode([{ ...leafGroup, name: "新名称" }], leafGroup.key)?.name).toBe("新名称");
        expect(findDirectoryNode([], leafGroup.key)).toBeNull();
    });
});
