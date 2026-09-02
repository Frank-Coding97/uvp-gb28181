import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/home/home.vue"), "utf8");

describe("editable realtime dashboard layout", () => {
  it("uses authoritative realtime sources", () => {
    expect(source).toContain("fetchSipDashboardSnapshot");
    expect(source).toContain("getZLMOverview");
    expect(source).toContain("listDevices");
    expect(source).toContain("listChannels");
    expect(source).toContain("setInterval(refreshData, 5_000)");
  });

  it("implements the approved widgets and reference composition", () => {
    for (const title of ["实时 SIP RPM", "今日 SIP 处理数量", "点播成功率", "设备在线率", "通道在线率", "GB28181 SIP 协议监控", "媒体实时速率", "媒体节点健康", "活跃流排行"]) {
      expect(source).toContain(title);
    }
    expect(source).toContain("grid-stack-item");
    expect(source).toContain("<OnlineDonut");
    expect(source).toContain("class=\"runtime-title\"");
    expect(source).not.toContain("<CardTitle icon=\"server\" title=\"流媒体运行态\" />");
  });

  it("supports editing, persistence, conflict messaging and reset", () => {
    expect(source).toContain("编辑仪表盘");
    expect(source).toContain("saveHomeDashboardLayout");
    expect(source).toContain("resetHomeDashboardLayout");
    expect(source).toContain("status === 409");
    expect(source).toContain("grid?.setEditing(true)");
  });

  it("keeps system theme tokens", () => {
    expect(source).not.toContain("background:var(--uvp-shell-muted)");
    expect(source).not.toContain("height:calc(100% - 2px)");
    expect(source).toContain("var(--uvp-panel-bg)");
    expect(source).toContain("var(--uvp-panel-border)");
    expect(source).toContain("var(--uvp-brand)");
  });
});
