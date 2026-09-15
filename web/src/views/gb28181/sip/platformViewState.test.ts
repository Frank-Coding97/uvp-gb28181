import { describe, expect, it } from "vitest";
import { mayEditSipConfig, runtimeColor, runtimeLabel } from "./platformViewState";

describe("SIP platform view states", () => {
    it("maps runtime states to stable labels", () => {
        expect(runtimeLabel("unconfigured")).toBe("未配置");
        expect(runtimeLabel("running")).toBe("运行中");
        expect(runtimeLabel("restart_required")).toBe("待重启");
        expect(runtimeLabel("failed")).toBe("启动失败");
        expect(runtimeColor("failed")).toBe("red");
    });

    it("shows edit only with account permission", () => {
        // 2026-07-20 起 canConfigure 语义废弃,mayEditSipConfig 只看 permissions.
        expect(mayEditSipConfig(["gb28181:sip:config:update"])).toBe(true);
        expect(mayEditSipConfig(["*:*:*"])).toBe(true);
        expect(mayEditSipConfig([])).toBe(false);
        expect(mayEditSipConfig(["other:perm"])).toBe(false);
    });
});
