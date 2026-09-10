import { describe, expect, it } from "vitest";
import {
    emptyWorkOrderForm,
    formatWorkOrderDuration,
    formatWorkOrderSize,
    isWorkOrderActive,
    missingWorkOrderFields,
    normalizeWorkOrderForm,
    workOrderStateColor,
    workOrderStateLabel,
    workOrderChannelRows,
    workOrderTotalBytes,
    workOrderSliceCount
} from "./orderState";

describe("work order state presentation", () => {
    it("has exactly one label per state so pages cannot disagree", () => {
        expect(workOrderStateLabel("recording")).toBe("录像中");
        expect(workOrderStateLabel("stopped")).toBe("已结束");
        expect(workOrderStateLabel("failed")).toBe("失败");
        expect(workOrderStateLabel("unknown")).toBe("待核实");
        // 未知取值一律回落到「待核实」，不再各页各写一套
        expect(workOrderStateLabel("something-new")).toBe("待核实");
        expect(workOrderStateColor("unknown")).toBe("orangered");
    });

    // 「录像中」与「失败」曾经同为红色，列表里根本分不出来。
    it("gives every state its own colour so neighbours cannot be confused", () => {
        const states = ["idle", "starting", "recording", "stopping", "stopped", "failed", "unknown"];
        const colors = states.map(state => workOrderStateColor(state));
        expect(new Set(colors).size).toBe(states.length);
        // 绿色沿用「云端录像」页正在录制的既有约定，红色只留给失败。
        expect(workOrderStateColor("recording")).toBe("green");
        expect(workOrderStateColor("failed")).toBe("red");
    });

    it("treats every unsettled state as active", () => {
        for (const state of ["starting", "recording", "stopping", "unknown"]) {
            expect(isWorkOrderActive(state)).toBe(true);
        }
        for (const state of ["idle", "stopped", "failed"]) {
            expect(isWorkOrderActive(state)).toBe(false);
        }
    });
});

describe("work order channel rows", () => {
    it("labels each channel with its device and GB codes", () => {
        const rows = workOrderChannelRows([
            { channelId: 12, channelCode: "34020000001320000010", channelName: "K12+300 左线", deviceId: "37010301021180000007", deviceName: "前置摄像头", state: "stopped" }
        ]);
        expect(rows).toEqual([
            {
                key: "12",
                deviceName: "前置摄像头",
                deviceId: "37010301021180000007",
                channelName: "K12+300 左线",
                channelId: "34020000001320000010",
                state: "stopped"
            }
        ]);
    });

    // 设备行可能尚未同步，弹窗不能因此显示空白或报错。
    it("falls back to the internal id when codes are missing", () => {
        const [row] = workOrderChannelRows([{ channelId: 12 }]);
        expect(row.channelId).toBe("12");
        expect(row.channelName).toBe("通道 12");
        expect(row.deviceId).toBe("—");
        expect(row.deviceName).toBe("—");
        expect(row.state).toBe("unknown");
    });

    it("handles a work order without cameras", () => {
        expect(workOrderChannelRows()).toEqual([]);
    });
});

describe("work order form normalisation", () => {
    it("splits the personnel text and drops blank entries", () => {
        const form = normalizeWorkOrderForm({ projectName: " 沪宁线 ", workPersonnel: "张伟、李强, 王敏，\n赵磊、、" });
        expect(form.projectName).toBe("沪宁线");
        expect(form.workPersonnel).toEqual(["张伟", "李强", "王敏", "赵磊"]);
    });

    it("treats an already-split personnel array the same way", () => {
        expect(normalizeWorkOrderForm({ workPersonnel: ["张伟", " ", "李强"] }).workPersonnel).toEqual(["张伟", "李强"]);
    });

    it("always produces every field so the payload shape is stable", () => {
        const form = normalizeWorkOrderForm({});
        expect(Object.keys(form).sort()).toEqual(Object.keys(emptyWorkOrderForm()).sort());
    });
});

// 必填集合必须与后端 Form.missingRequiredFields() 一致，
// 否则现场会遇到「前端放过、后端拒绝」。
describe("work order required fields", () => {
    const complete = {
        projectName: "沪宁线放线作业",
        stationArea: "南京南—江宁",
        anchorSectionNo: "A-12",
        workLeader: "张伟",
        workPersonnel: "张伟、李强"
    };

    it("accepts a form that fills the five required entries", () => {
        expect(missingWorkOrderFields(normalizeWorkOrderForm(complete))).toEqual([]);
    });

    it("names every missing entry", () => {
        expect(missingWorkOrderFields(normalizeWorkOrderForm({}))).toEqual(["项目名称", "站区", "锚段号", "作业负责人", "作业人员"]);
    });

    it("does not let whitespace satisfy a required entry", () => {
        const missing = missingWorkOrderFields(normalizeWorkOrderForm({ ...complete, stationArea: "   ", workPersonnel: " 、 " }));
        expect(missing).toEqual(["站区", "作业人员"]);
    });
});

describe("work order formatters", () => {
    it("formats sizes with readable units", () => {
        expect(formatWorkOrderSize(null)).toBe("—");
        expect(formatWorkOrderSize(512)).toBe("512 B");
        expect(formatWorkOrderSize(2048)).toBe("2.0 KB");
        expect(formatWorkOrderSize(3 * 1024 * 1024)).toBe("3.0 MB");
    });

    it("formats durations by magnitude", () => {
        expect(formatWorkOrderDuration(null)).toBe("—");
        expect(formatWorkOrderDuration(45)).toBe("45秒");
        expect(formatWorkOrderDuration(125)).toBe("2分5秒");
        expect(formatWorkOrderDuration(7300)).toBe("2时1分");
    });

    it("aggregates slices and bytes across cameras", () => {
        const snapshot = {
            cameras: [
                { channelId: 1, files: [{ fileSize: 100 }, { fileSize: 200 }] },
                { channelId: 2, files: [{ fileSize: 300 }] }
            ]
        };
        expect(workOrderSliceCount(snapshot)).toBe(3);
        expect(workOrderTotalBytes(snapshot)).toBe(600);
        expect(workOrderSliceCount({})).toBe(0);
    });
});
