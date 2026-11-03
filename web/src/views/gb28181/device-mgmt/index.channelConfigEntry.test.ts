import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { hasMarkup, squash } from "@/test/source-assert";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/index.vue"), "utf8");
const workspace = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/ChannelConfigWorkspace.vue"), "utf8");
const modalStart = source.indexOf("channel-config-dialog");
const modalTagStart = source.lastIndexOf("<a-modal", modalStart);
const modalTag = source.slice(modalTagStart, source.indexOf(">", modalStart));

describe("通道配置入口契约", () => {
  it("使用系统对话框，禁用遮罩关闭并通过草稿检查关闭", () => {
    expect(modalTag).toContain("uvp-system-dialog");
    expect(hasMarkup(modalTag, ':mask-closable="false"')).toBe(true);
    expect(modalTag).toContain("unmount-on-close");
    expect(modalTag).toContain(':on-before-cancel="beforeChannelConfigCancel"');
    expect(source).toContain("channelWorkspace.value?.beforeLeave()");
    expect(source).toContain("onBeforeRouteLeave");
    expect(source).toContain('window.addEventListener("beforeunload", protectChannelDraft)');
  });

  it("按通道创建统一工作区，读写分别受接口对应权限控制", () => {
    expect(source).toContain(':key="channelOperationTarget.id"');
    expect(source).toContain(':channel-id="channelOperationTarget.id"');
    expect(source).toContain(':can-read="canViewDeviceFacts"');
    expect(source).toContain(':can-apply="canControlChannelConfig"');
    expect(source).toContain('const canControlChannelConfig = computed(() => hasPermission("gb28181:ptz:control"));');
    expect(workspace).toContain(':can-apply="canApply && online"');
    expect(workspace).toContain('const CONFIG_GROUPS = ["record-plan", "alarm-record", "alarm-report", "basic"]');
    expect(workspace).toContain("external-navigation");
    expect(workspace).not.toContain('key: "video-param"');
    expect(workspace).not.toContain('key: "picture"');
    expect(workspace).not.toContain('key: "osd"');
    expect(workspace).not.toContain("DeviceControlPanel");
    expect(source).not.toContain("channel-config-tabs");
  });

  it("标题保留当前列表顺序的上下一路切换，切换前检查草稿", () => {
    expect(source).toContain('data-testid="channel-config-prev"');
    expect(source).toContain('data-testid="channel-config-next"');
    expect(squash(source)).toContain("channels.value.findIndex(item=>item.id===current.id)");
    const step = source.slice(
      source.indexOf("async function stepChannelOperation"),
      source.indexOf("async function prepareMaintenance")
    );
    expect(step.indexOf("beforeLeave()")).toBeLessThan(step.indexOf("channelOperationTarget.value = next"));
  });

  it("让播放码流下拉框与卡片上的其他配置下拉框保持一致宽度", () => {
    const styleStart = source.indexOf(":deep(.channel-card-inline-select.arco-select-view-single)");
    const streamStyleStart = source.indexOf(":deep(.channel-stream-select.arco-select-view-single)");
    const streamStyle = source.slice(streamStyleStart, source.indexOf("}", streamStyleStart));

    expect(styleStart).toBeGreaterThan(-1);
    expect(streamStyleStart).toBeGreaterThan(styleStart);
    expect(streamStyle).toContain("width: 180px !important;");
    expect(streamStyle).not.toContain("max-width: 220px");

    const inlineStyle = source.slice(styleStart, source.indexOf("}", styleStart));
    expect(inlineStyle).toContain("width: 180px !important;");
  });
});
