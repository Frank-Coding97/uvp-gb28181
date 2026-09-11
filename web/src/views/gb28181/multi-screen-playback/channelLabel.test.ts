import { describe, expect, it } from "vitest";
import { channelDisplayName } from "./channelLabel";

const base = { id: 1, channelId: "channel-1", deviceId: "device-1", alias: "", name: "东门" };

describe("channelDisplayName", () => {
    it("prefers the alias edited in the device list", () => {
        expect(channelDisplayName({ ...base, alias: "大门监控" })).toBe("大门监控");
    });

    it("falls back to the reported name when no alias is set", () => {
        expect(channelDisplayName({ ...base, alias: "" })).toBe("东门");
        expect(channelDisplayName({ ...base, alias: "   " })).toBe("东门");
    });

    it("falls back to the channel code and then to the caller fallback", () => {
        expect(channelDisplayName({ ...base, alias: "", name: "" })).toBe("channel-1");
        expect(channelDisplayName(null, "未选择画面")).toBe("未选择画面");
        expect(channelDisplayName(undefined)).toBe("");
    });
});
