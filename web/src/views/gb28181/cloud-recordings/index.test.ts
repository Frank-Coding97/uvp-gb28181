import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
  listRecordingFiles: vi.fn(),
  listRecordingOptions: vi.fn(),
  listActiveRecordings: vi.fn(),
  listReconciliations: vi.fn(),
  triggerReconciliation: vi.fn(),
  issueRecordingAccess: vi.fn()
}));
const account = vi.hoisted(() => ({ permissions: ["gb28181:recording:view", "gb28181:recording:reconcile"] as string[] }));
const messages = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));

vi.mock("./api", async importOriginal => ({ ...(await importOriginal<typeof import("./api")>()), ...api }));
vi.mock("@/store/modules/user", () => ({ useUserStoreHook: () => ({ account }) }));
vi.mock("@/hooks/useGlobalProperties", () => ({ default: () => ({ $message: messages }) }));
vi.mock("@/hooks/useDevicesSize", () => ({ useDevicesSize: () => ({ isMobile: { value: false } }) }));

import { getLucideIconComponent } from "@/utils/lucide-menu-icons";
import CloudRecordings from "./index.vue";

const file = (availability = "available", metadataState = "complete") => ({
  id: "9007199254740993",
  fileKey: "digest",
  channelId: "12",
  channelCode: "34020000001320000001",
  channelName: "东门",
  deviceId: "34020000001110000001",
  deviceName: "一号 NVR",
  node: { id: "8", name: "边缘节点 A", state: "online" },
  fileName: "record.mp4",
  startTime: "2026-08-10T12:00:00Z",
  endTime: metadataState === "partial" ? null : "2026-08-10T12:10:00Z",
  timeLen: metadataState === "partial" ? null : 600,
  fileSize: metadataState === "partial" ? null : 10485760,
  source: "hook",
  metadataState,
  availability,
  recordDate: null,
  discoveredAt: null,
  lastSeenAt: null,
  missingAt: null
});

const tableStub = {
  props: ["data", "pagination"],
  provide() { return { recordingTable: this }; },
  template: "<div data-testid='recording-table' :data-count='data.length' :data-total='pagination.total'><slot name='columns' /><slot v-if='!data.length' name='empty' /></div>"
};
const stubs = {
  "s-layout-search": { template: "<section><slot name='fields' /><slot name='actions' /></section>" },
  "a-tabs": { props: ["activeKey"], emits: ["update:activeKey"], template: "<div><button data-testid='files-tab' @click='$emit(`update:activeKey`, `files`)'>录像文件</button><button data-testid='active-tab' @click='$emit(`update:activeKey`, `active`)'>正在录像</button><slot /></div>" },
  "a-tab-pane": { props: ["key"], template: "<div><slot /></div>" },
  "a-table": tableStub,
  "a-table-column": { props: ["title"], inject: ["recordingTable"], template: "<span>{{ title }}<template v-for='record in recordingTable.data'><slot name='cell' :record='record' /></template></span>" },
  "a-button": { emits: ["click"], template: "<button :data-testid='$attrs[`data-testid`]' @click='$emit(`click`)'><slot name='icon' /><slot /></button>" },
  "a-link": { emits: ["click"], template: "<a :data-testid='$attrs[`data-testid`]' @click='$emit(`click`)'><slot /></a>" },
  "a-tag": { template: "<span><slot /></span>" },
  "a-alert": { template: "<div><slot /></div>" },
  "a-empty": { props: ["description"], template: "<div>{{ description }}</div>" },
  "a-select": { template: "<select><slot /></select>" },
  "a-option": { template: "<option><slot /></option>" },
  "a-input": { template: "<input />" },
  "a-range-picker": { template: "<div />" },
  RecordingDetailDrawer: { template: "<div />" },
  RecordingPlayerDialog: { template: "<div />" }
};

function pageResult(list = [file()]) {
  return { code: 0, message: "", data: { list, total: list.length, page: 1, pageSize: 20 } };
}

describe("CloudRecordings", () => {
  beforeEach(() => {
    account.permissions = ["gb28181:recording:view", "gb28181:recording:reconcile"];
    Object.values(api).forEach(mock => mock.mockReset());
    api.listRecordingFiles.mockResolvedValue(pageResult());
    api.listRecordingOptions.mockResolvedValue({ code: 0, message: "", data: { channels: [], devices: [], nodes: [] } });
    api.listActiveRecordings.mockResolvedValue({ code: 0, message: "", data: { list: [{ id: "77", channelId: "12", channelCode: "c", channelName: "东门", deviceId: "d", node: { id: "8", name: "节点 A" }, state: "recording", startedAt: "2026-08-10T12:00:00Z", updatedAt: "2026-08-10T12:01:00Z" }] } });
    api.listReconciliations.mockResolvedValue({ code: 0, message: "", data: { list: [] } });
    api.triggerReconciliation.mockResolvedValue({ code: 0, message: "", data: { acceptedNodeIds: [8] } });
    api.issueRecordingAccess.mockResolvedValue({ code: 0, message: "", data: { mode: "download", capability: "signed", expiresAt: "2026-08-10T13:00:00Z" } });
    messages.success.mockReset();
    messages.error.mockReset();
  });

  it("loads files with default range and keeps partial metadata explicit", async () => {
    api.listRecordingFiles.mockResolvedValue(pageResult([file("available", "partial")]));
    const wrapper = mount(CloudRecordings, { global: { stubs } });
    await flushPromises();
    const query = api.listRecordingFiles.mock.calls[0][0];
    expect(query.page).toBe(1);
    expect(Date.parse(query.end) - Date.parse(query.start)).toBe(24 * 60 * 60 * 1000);
    expect(wrapper.text()).toContain("待完善");
    expect(wrapper.text()).toContain("--");
    expect(wrapper.get("[data-testid='recording-table']").attributes("data-total")).toBe("1");
  });

  it("allows the admin wildcard permission to view and reconcile recordings", async () => {
    account.permissions = ["*:*:*"];
    const wrapper = mount(CloudRecordings, { global: { stubs } });
    await flushPromises();

    expect(api.listRecordingFiles).toHaveBeenCalledTimes(1);
    await wrapper.get("[data-testid='recording-reconcile']").trigger("click");
    await flushPromises();
    expect(api.triggerReconciliation).toHaveBeenCalledTimes(1);
  });

  it.each(["node_offline", "node_missing", "file_missing", "access_unavailable"])("hides access actions for %s", async availability => {
    api.listRecordingFiles.mockResolvedValue(pageResult([file(availability)]));
    const wrapper = mount(CloudRecordings, { global: { stubs } });
    await flushPromises();
    expect(wrapper.find("[data-testid='play-9007199254740993']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='download-9007199254740993']").exists()).toBe(false);
  });

  it("separates active recordings from catalog files", async () => {
    const wrapper = mount(CloudRecordings, { global: { stubs } });
    await flushPromises();
    expect(wrapper.text()).toContain("record.mp4");
    await wrapper.get("[data-testid='active-tab']").trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("正在录制");
    expect(wrapper.text()).not.toContain("record.mp4");
  });

  it("registers Cloud and downloads through a freshly issued same-origin URL", async () => {
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => undefined);
    const wrapper = mount(CloudRecordings, { global: { stubs } });
    await flushPromises();
    expect(getLucideIconComponent("lucide:Cloud")).toBeTruthy();
    await wrapper.get("[data-testid='download-9007199254740993']").trigger("click");
    await flushPromises();
    expect(api.issueRecordingAccess).toHaveBeenCalledWith("9007199254740993", "download");
    expect(click).toHaveBeenCalledTimes(1);
    click.mockRestore();
  });
});
