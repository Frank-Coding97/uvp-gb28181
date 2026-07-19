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

    it("shows edit only with both server and account permission", () => {
        expect(mayEditSipConfig(true, ["gb28181:sip:config:update"])).toBe(true);
        expect(mayEditSipConfig(false, ["gb28181:sip:config:update"])).toBe(false);
        expect(mayEditSipConfig(true, [])).toBe(false);
    });
});
