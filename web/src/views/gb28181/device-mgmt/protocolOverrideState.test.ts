import { describe, expect, it } from "vitest";
import { normalizeProtocolOverride, protocolOverrideAfterSave } from "./protocolOverrideState";

describe("protocol override edit state", () => {
    it.each([
        ["auto", "auto"],
        ["2016", "2016"],
        ["2022", "2022"],
        ["3.0", "auto"],
        [null, "auto"],
    ] as const)("normalizes %s to %s", (input, expected) => {
        expect(normalizeProtocolOverride(input)).toBe(expected);
    });

    it("rolls a failed save back to the value loaded from the server", () => {
        expect(protocolOverrideAfterSave("2016", "2022", false)).toBe("2016");
        expect(protocolOverrideAfterSave("auto", "2022", false)).toBe("auto");
    });

    it("keeps the selected value after a successful save", () => {
        expect(protocolOverrideAfterSave("2016", "2022", true)).toBe("2022");
    });
});
