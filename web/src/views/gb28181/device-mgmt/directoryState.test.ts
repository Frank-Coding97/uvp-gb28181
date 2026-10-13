import { describe, expect, it } from "vitest";
import {
  createDirectoryState,
  directoryQuery,
  findDirectoryNode,
  normalizeDirectoryView,
  persistDirectoryViewPreference,
  readDirectoryViewPreference,
  selectDirectory,
  switchDirectoryView
} from "./directoryState";

const departmentNode = {
  key: "department:dept:12",
  name: "安保部",
  type: "organization",
  readOnly: true,
  count: 1,
  onlineCount: 1,
  depth: 1,
  children: []
};

describe("directoryState", () => {
  it("defaults to administrative and keeps selection and expansion per view", () => {
    let state = createDirectoryState();
    expect(state.view).toBe("administrative");
    state = selectDirectory(state, "administrative:340200");
    state = switchDirectoryView(state, "department");
    state = selectDirectory(state, "department:dept:12");
    state = switchDirectoryView(state, "administrative");
    expect(state.selectedKey).toEqual({
      administrative: "administrative:340200",
      department: "department:dept:12",
      business: null
    });
  });

  it("remembers a selected view and falls legacy national/custom preferences back to administrative", () => {
    let storedValue: string | null = null;
    const storage = {
      getItem: () => storedValue,
      setItem: (_key: string, value: string) => {
        storedValue = value;
      }
    };
    persistDirectoryViewPreference("department", storage);
    expect(readDirectoryViewPreference(storage)).toBe("department");

    storedValue = "national";
    expect(readDirectoryViewPreference(storage)).toBe("administrative");
    storedValue = "custom";
    expect(readDirectoryViewPreference(storage)).toBe("administrative");
    expect(normalizeDirectoryView("national")).toBe("administrative");
    expect(normalizeDirectoryView("custom")).toBe("administrative");
    expect(normalizeDirectoryView("unknown")).toBe("administrative");
  });

  it("maps the selected department key to the paired query params", () => {
    const state = selectDirectory(switchDirectoryView(createDirectoryState(), "department"), "department:dept:12");
    expect(directoryQuery(state)).toEqual({ directoryView: "department", directoryKey: "department:dept:12" });
    expect(directoryQuery(createDirectoryState())).toEqual({});
  });

  it("keeps an empty business view scoped so it cannot fall back to every channel", () => {
    const businessView = switchDirectoryView(createDirectoryState(), "business");
    expect(directoryQuery(businessView)).toEqual({ directoryView: "business" });
  });

  it("reports list reset effects without owning playback state", () => {
    const result = switchDirectoryView(createDirectoryState(), "department");
    expect(result.listReset).toEqual({ page: 1, clearSelection: true });
    expect(result).not.toHaveProperty("playing");
  });

  it("finds a department in a nested tree", () => {
    expect(findDirectoryNode([{ ...departmentNode, children: [departmentNode] }], departmentNode.key)?.name).toBe("安保部");
    expect(findDirectoryNode([], departmentNode.key)).toBeNull();
  });
});
