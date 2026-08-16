import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/home/home.vue"), "utf8");

describe("dashboard layout contract", () => {
  it("uses the existing real data sources", () => {
    expect(source).toContain("listDevices");
    expect(source).toContain("listChannels");
    expect(source).toContain("getAnomalyCount");
    expect(source).toContain("listAlarms");
    expect(source).toContain("listZLMNodes");
    expect(source).toContain("fetchSipSetupStatus");
  });

  it("presents a compact dashboard without the old demo modules", () => {
    expect(source).toContain("GB28181 视频平台仪表盘");
    expect(source).toContain("需关注事项");
    expect(source).toContain("service-chip");
    expect(source).toContain("metric-card__rail");
    expect(source).not.toContain("SIP 注册趋势");
    expect(source).not.toContain("播放请求趋势");
    expect(source).not.toContain("异常设备排行");
    expect(source).not.toContain("系统运行时长");
    expect(source).not.toContain("较昨日");
    expect(source).not.toContain("今日消费");
    expect(source).not.toContain("用户排行");
    expect(source).not.toContain("模型排行");
    expect(source).not.toContain("供应商排行");
  });

  it("keeps the desktop dashboard inside its available content height", () => {
    expect(source).toMatch(/\.dashboard-shell\s*\{[^}]*box-sizing:\s*border-box/s);
  });

  it("inherits dashboard surfaces from the system theme tokens", () => {
    expect(source).toContain("--dashboard-canvas: var(--uvp-shell-muted)");
    expect(source).toContain("--dashboard-surface: var(--uvp-panel-bg)");
    expect(source).toContain("--dashboard-surface-muted: var(--uvp-list-toolbar-bg)");
    expect(source).toContain("--dashboard-border: var(--uvp-panel-border)");
  });

  it("uses a compact desktop density when the viewport is short", () => {
    expect(source).toContain("@media (width >= 1101px) and (height <= 799px)");
    expect(source).toContain(".dashboard-sip :deep(.tx-cell)");
  });
});
