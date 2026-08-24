import { describe, expect, it } from "vitest";
import { useCrossPageSelection, type SelectionDevice } from "./useCrossPageSelection";

const device = (id: number): SelectionDevice => ({ id, deviceId: `D${id}`, name: `设备 ${id}` });

describe("useCrossPageSelection", () => {
    it("accumulates devices across pages and removes only the current page cancellation", () => {
        const selection = useCrossPageSelection();
        selection.applyPageSelection([device(1), device(2)], [1]);
        selection.applyPageSelection([device(3), device(4)], [3]);

        expect(selection.selectedIds.value).toEqual([1, 3]);
        expect(selection.selectedDevices.value.map((item) => item.id)).toEqual([1, 3]);

        selection.applyPageSelection([device(3), device(4)], []);
        expect(selection.selectedIds.value).toEqual([1]);
    });

    it("keeps hidden selections when the page filter changes", () => {
        const selection = useCrossPageSelection();
        selection.add(device(1));
        selection.add(device(2));
        selection.setVisiblePage([device(2)]);

        expect(selection.selectedCount.value).toBe(2);
        expect(selection.hiddenCount.value).toBe(1);
        expect(selection.currentPageKeys.value).toEqual([2]);
    });

    it("clears successful results and removes unavailable devices while retaining failures", () => {
        const selection = useCrossPageSelection();
        selection.add(device(1));
        selection.add(device(2));
        selection.add(device(3));
        selection.applyOperationResult({ changed: [1], skipped: [2], failed: [3] });
        expect(selection.selectedIds.value).toEqual([3]);

        selection.removeUnavailable([3]);
        expect(selection.selectedIds.value).toEqual([]);
        expect(selection.unavailableIds.value).toEqual([3]);
    });
});
