import { describe, expect, it } from "vitest";
import { createDirectoryState, directoryQuery, selectDirectory, switchDirectoryView } from "./directoryState";

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
});
