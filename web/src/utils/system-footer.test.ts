import { describe, expect, it } from "vitest";
import { getDisplaySystemCopyright, SYSTEM_COPYRIGHT_DEFAULT } from "./system-footer";
describe("configured copyright", () => {
    it("does not restore a vendor brand for empty or seed values", () => {
        expect(getDisplaySystemCopyright("")).toBe("");
        expect(getDisplaySystemCopyright(SYSTEM_COPYRIGHT_DEFAULT)).toBe("");
        expect(getDisplaySystemCopyright("客户版权所有")).toBe("客户版权所有");
    });
});
