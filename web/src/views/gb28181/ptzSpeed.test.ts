import { describe, expect, it } from "vitest";
import { DEFAULT_PTZ_SPEED_LEVEL, levelToProtocolSpeed, normalizePtzSpeedLevel } from "./ptzSpeed";

describe("PTZ speed mapping", () => {
    it.each([
        [1, 26],
        [2, 51],
        [3, 77],
        [4, 102],
        [5, 128],
        [6, 153],
        [7, 179],
        [8, 204],
        [9, 230],
        [10, 255]
    ])("maps level %i to protocol speed %i", (level, expected) => {
        expect(levelToProtocolSpeed(level)).toBe(expected);
    });

    it("normalizes invalid and out-of-range levels", () => {
        expect(normalizePtzSpeedLevel(Number.NaN)).toBe(DEFAULT_PTZ_SPEED_LEVEL);
        expect(normalizePtzSpeedLevel(0)).toBe(1);
        expect(normalizePtzSpeedLevel(11)).toBe(10);
        expect(normalizePtzSpeedLevel(6.6)).toBe(7);
    });
});
