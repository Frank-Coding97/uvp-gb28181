import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const getRecordingDetail = vi.hoisted(() => vi.fn());
vi.mock("../api", async importOriginal => ({
  ...(await importOriginal<typeof import("../api")>()),
  getRecordingDetail
}));

import RecordingDetailDrawer from "./RecordingDetailDrawer.vue";

const recording = (availability = "available", metadataState = "complete") => ({
  id: "9007199254740993",
  fileKey: "digest",
  channelId: "12",
  channelCode: "34020000001320000001",
  channelName: "东门",
  deviceId: "34020000001110000001",
  deviceName: "一号 NVR",
  node: { id: "8", name: "边缘节点 A", state: "online" },
  fileName: "2026-08-10-12-00-00.mp4",
  startTime: "2026-08-10T12:00:00Z",
  endTime: metadataState === "partial" ? null : "2026-08-10T12:10:00Z",
  timeLen: metadataState === "partial" ? null : 600,
  fileSize: metadataState === "partial" ? null : 10485760,
  source: "hook",
  metadataState,
  availability,
  recordDate: "2026-08-10T00:00:00Z",
  discoveredAt: "2026-08-10T12:10:01Z",
  lastSeenAt: "2026-08-10T12:10:01Z",
  missingAt: null
});

const stubs = {
  "a-drawer": { props: ["visible"], template: "<section v-if='visible'><slot name='title' /><slot /></section>" },
  "a-spin": { template: "<div><slot /></div>" },
  "a-alert": { template: "<div><slot /></div>" },
  "a-button": { emits: ["click"], template: "<button :data-testid='$attrs[`data-testid`]' @click='$emit(`click`)'><slot name='icon' /><slot /></button>" },
  "a-tag": { template: "<span><slot /></span>" },
  "a-descriptions": { template: "<dl><slot /></dl>" },
  "a-descriptions-item": { props: ["label"], template: "<div><dt>{{ label }}</dt><dd><slot /></dd></div>" }
};

describe("RecordingDetailDrawer", () => {
  beforeEach(() => {
    getRecordingDetail.mockReset();
    getRecordingDetail.mockResolvedValue({ code: 0, message: "", data: recording() });
  });

  it("keeps opaque IDs, renders public metadata and emits available actions", async () => {
    const wrapper = mount(RecordingDetailDrawer, {
      props: { visible: true, recordingId: "9007199254740993" },
      global: { stubs }
    });
    await flushPromises();
    expect(getRecordingDetail).toHaveBeenCalledWith("9007199254740993");
    expect(wrapper.text()).toContain("东门");
    expect(wrapper.text()).toContain("边缘节点 A");
    expect(wrapper.text()).not.toContain("filePath");
    await wrapper.get("[data-testid='detail-play']").trigger("click");
    await wrapper.get("[data-testid='detail-download']").trigger("click");
    expect(wrapper.emitted("play")?.[0]?.[0]).toMatchObject({ id: "9007199254740993" });
    expect(wrapper.emitted("download")?.[0]?.[0]).toMatchObject({ id: "9007199254740993" });
  });

  it("shows partial metadata as pending and hides unsupported actions", async () => {
    getRecordingDetail.mockResolvedValue({ code: 0, message: "", data: recording("node_offline", "partial") });
    const wrapper = mount(RecordingDetailDrawer, {
      props: { visible: true, recordingId: "41" },
      global: { stubs }
    });
    await flushPromises();
    expect(wrapper.text()).toContain("待完善");
    expect(wrapper.text()).toContain("--");
    expect(wrapper.text()).toContain("节点离线");
    expect(wrapper.find("[data-testid='detail-play']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='detail-download']").exists()).toBe(false);
  });
});
