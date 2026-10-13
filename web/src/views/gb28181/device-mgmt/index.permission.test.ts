import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/device-mgmt/index.vue"), "utf8");

describe("device-mgmt guest permission boundary", () => {
  it("defines the read, playback, recording and runtime permission gates", () => {
    // ⭐ 权限收敛（2026-10-11）：本地那份 `hasPermission` 实现已删除，
    //    改为复用全平台唯一入口 `@/hooks/usePermission` —— 断言点随之从
    //    「页内自己实现通配规则」改成「页内引用了统一入口」。
    expect(source).toContain('import { hasPermission } from "@/hooks/usePermission"');
    for (const permission of [
      "canViewDevices",
      "canStartPlayback",
      "canQueryDeviceRecords",
      "canViewTraffic",
      "canAddDevice",
      "canEditDevice",
      "canDeleteDevice",
      "canRefreshCatalog",
      "canManageSubscriptions",
      "canEditChannel",
      "canEditTransport",
      "canUpdateRecording",
      "canStopPlayback"
    ]) {
      expect(source).toContain(`const ${permission} = computed`);
    }
  });

  it("rechecks permissions at every device-management action entry", () => {
    const guardedEntries = [
      ["showDeviceChannels", "canViewDevices"],
      ["openChannel", "canViewDevices"],
      ["openDevice", "canViewDevices"],
      ["openSubscriptionManager", "canManageSubscriptions"],
      ["openStatusEvents", "canViewTraffic"],
      ["playChannel", "canStartPlayback"],
      ["openRecordQuery", "canQueryDeviceRecords"],
      ["handleRefreshDeviceCatalog", "canRefreshCatalog"],
      ["openCreateDeviceModal", "canAddDevice"],
      ["handleCreateDevice", "canAddDevice"],
      ["openEditDeviceModal", "canEditDevice"],
      ["handleEditDevice", "canEditDevice"],
      ["openEditChannelModal", "canEditChannel"],
      ["handleEditChannel", "canEditChannel"],
      ["handleDeleteDevice", "canDeleteDevice"],
      ["handleDeleteChannel", "canDeleteChannel"],
      ["handleStreamTransportChange", "canEditTransport"],
      ["handlePtzTypeChange", "canEditChannel"],
      ["handleAudioEnabledChange", "canEditChannel"],
      ["handleOnDemandLiveChange", "canEditChannel"],
      ["handleCloudRecordingChange", "canUpdateRecording"],
      ["handleStopChannel", "canStopPlayback"]
    ] as const;

    for (const [entry, permission] of guardedEntries) {
      const start = source.indexOf(`function ${entry}`);
      const asyncStart = source.indexOf(`async function ${entry}`);
      const bodyStart = Math.max(start, asyncStart);
      expect(bodyStart, entry).toBeGreaterThan(-1);
      const body = source.slice(bodyStart, source.indexOf("\n}", bodyStart) + 2);
      expect(body, entry).toContain(`if (!${permission}.value) return;`);
    }
  });

  it("greys out guest write controls instead of removing them, while dialogs stay hidden", () => {
    // ⭐ 权限改造（2026-10-11，产品要求「让用户看得到平台有什么功能」）：
    //    按钮 / 链接 / 表单控件一律「可见但置灰」，不再从模板里消失。
    //    本用例原先的反向断言（`v-if="canXxx"`）已随之翻转为 `:disabled` 形态。
    for (const gate of [
      ':disabled="!canAddDevice"',
      "!canRefreshCatalog",
      ':disabled="!canManageSubscriptions"',
      ':disabled="!canEditDevice"',
      ':disabled="deleting || !canDeleteDevice"',
      ':disabled="!canEditChannel"',
      "!canEditTransport",
      "!canUpdateRecording",
      "!canStopPlayback"
    ]) {
      expect(source).toContain(gate);
    }
    // ⛔ 弹窗 / 整块内容没有「禁用」形态，继续隐藏：新增设备弹窗、运行监控弹窗，
    //    以及本来就带 v-else 只读兜底的展示元素（状态带 / 内联状态）。
    expect(source).toContain('v-if="canAddDevice"');
    expect(source).toContain('v-if="canViewTraffic"');
    expect(source).toContain(':disabled="!canViewTraffic"');
  });

  it("does not submit transport changes through the general channel edit without its permission", () => {
    const start = source.indexOf("async function handleEditChannel");
    const end = source.indexOf("\n}\n\nfunction cancelEditDevice", start);
    const body = source.slice(start, end);
    expect(body).toMatch(/canEditTransport\.value\s*\?\s*updateChannelStreamTransport/);
    expect(body).toContain("if (canEditTransport.value)");
  });

  it("loads only read dependencies for a guest device detail", () => {
    expect(source).toContain("canManageSubscriptions.value ? listDeviceSubscriptions(record.id) : Promise.resolve(null)");
    expect(source).toContain("if (canEditChannel.value) await loadPtzTypeDict()");
    expect(source).toContain("if (!canViewTraffic.value) return;");
  });
});
