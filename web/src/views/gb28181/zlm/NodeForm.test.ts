import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

describe("ZLM node address form", () => {
    const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/zlm/NodeForm.vue"), "utf8");

    it("exposes separate receive and playback addresses", () => {
        expect(source).toContain("receiveHost");
        expect(source).toContain("playbackHost");
        expect(source).toContain("设备收流地址");
        expect(source).toContain("播放访问地址");
    });

    it("supports editing an existing node", () => {
        expect(source).toContain("updateZLMNode");
        expect(source).toContain("props.node");
    });
});
