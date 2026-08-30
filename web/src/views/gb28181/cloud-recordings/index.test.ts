import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const api = vi.hoisted(() => ({
  listRecordingFiles: vi.fn(),
  listRecordingOptions: vi.fn(),
  listActiveRecordings: vi.fn(),
  listReconciliations: vi.fn(),
  triggerReconciliation: vi.fn(),
  deleteRecordingFile: vi.fn(),
  batchDeleteRecordingFiles: vi.fn(),
  stopActiveRecording: vi.fn()
}));
const enqueueDownload = vi.hoisted(() => vi.fn());
const account = vi.hoisted(() => ({ permissions: ["gb28181:recording:view", "gb28181:recording:reconcile"] as string[] }));
const messages = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
const modalWarning = vi.hoisted(() => vi.fn());

vi.mock("./api", async importOriginal => ({ ...(await importOriginal<typeof import("./api")>()), ...api }));
vi.mock("@arco-design/web-vue", () => ({ Modal: { warning: modalWarning } }));
vi.mock("./recordingDownloadService", () => ({ recordingDownloadCoordinator: { enqueue: enqueueDownload } }));
vi.mock("@/store/modules/user", () => ({
  useUserStoreHook: () => ({ account }),
  registerUserLogoutCleanup: vi.fn()
}));
vi.mock("@/hooks/useGlobalProperties", () => ({ default: () => ({ $message: messages }) }));
vi.mock("@/hooks/useDevicesSize", () => ({ useDevicesSize: () => ({ isMobile: { value: false } }) }));

import { getLucideIconComponent } from "@/utils/lucide-menu-icons";
import CloudRecordings from "./index.vue";
import RecordingWorkspacePanel from "./RecordingWorkspacePanel.vue";

const shellSource = readFileSync(resolve(process.cwd(), "src/views/gb28181/cloud-recordings/index.vue"), "utf8");
const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/cloud-recordings/RecordingWorkspacePanel.vue"), "utf8");

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
  props: ["data", "pagination", "selectedKeys", "rowSelection"],
  emits: ["update:selectedKeys"],
  provide() { return { recordingTable: this }; },
  template: "<div data-testid='recording-table' :data-count='data.length' :data-total='pagination.total'><button v-if='data.length && rowSelection' data-testid='select-first-recording' @click='$emit(`update:selectedKeys`, [data[0].id])'>选择</button><slot name='columns' /><slot v-if='!data.length' name='empty' /></div>"
};
const stubs = {
  "s-layout-search": { template: "<section><slot name='fields' /><slot name='actions' /></section>" },
  "a-table": tableStub,
  "a-table-column": { props: ["title"], inject: ["recordingTable"], template: "<span>{{ title }}<template v-for='record in recordingTable.data'><slot name='cell' :record='record' /></template></span>" },
  "a-button": { emits: ["click"], template: "<button :data-testid='$attrs[`data-testid`]' :data-type='$attrs.type' @click='$emit(`click`)'><slot name='icon' /><slot /></button>" },
  "a-link": { emits: ["click"], template: "<a :data-testid='$attrs[`data-testid`]' @click='$emit(`click`)'><slot name='icon' /><slot /></a>" },
  "a-tag": { template: "<span><slot /></span>" },
  "a-alert": { template: "<div><slot /></div>" },
  "a-empty": { props: ["description"], template: "<div>{{ description }}</div>" },
  "a-select": { template: "<select><slot /></select>" },
  "a-option": { template: "<option><slot /></option>" },
  "a-input": {
    props: ["modelValue", "placeholder"],
    emits: ["update:modelValue", "pressEnter"],
    template: `<input :value="modelValue" :placeholder="placeholder" @input="$emit('update:modelValue', $event.target.value)" @keyup.enter="$emit('pressEnter')" />`
  },
  "a-range-picker": { template: "<div />" },
  RecordingDetailDrawer: { template: "<div />" },
  RecordingPlayerDialog: { template: "<div />" },
  RecordingRuntimeControl: { template: "<div data-testid='runtime-control' />" }
};

describe("cloud recording layout", () => {
  it("keeps the old route as a thin shell over the shared business panel", () => {
    expect(shellSource).toContain("RecordingWorkspacePanel");
    expect(shellSource).toContain('mode="all"');
    expect(shellSource).toContain(':active="true"');
    expect(shellSource).not.toContain("listRecordingFiles");
    expect(shellSource).not.toContain("RecordingPlayerDialog");
  });

  it("keeps vertical scrolling inside the recording table", () => {
    expect(shellSource).toContain('class="snow-fill cloud-recordings-route"');
    expect(source).toContain('class="cloud-recordings-page"');
    expect(source).toContain('class="cloud-recordings-shell"');
    expect(source).toMatch(/\.cloud-recordings-page\s*{[^}]*height:\s*100%;[^}]*min-height:\s*0;[^}]*overflow:\s*hidden;/s);
    expect(source).toMatch(/\.cloud-recordings-shell\s*{[^}]*display:\s*flex;[^}]*height:\s*100%;[^}]*min-height:\s*0;[^}]*flex-direction:\s*column;[^}]*overflow:\s*hidden;/s);
    expect(source).toMatch(/\.recording-files-view,\s*\.active-recordings-view\s*{[^}]*display:\s*flex;[^}]*min-height:\s*0;[^}]*flex-direction:\s*column;/s);
    expect(source).toMatch(/\.cloud-recordings-table-wrap\s*{[^}]*flex:\s*1;[^}]*min-height:\s*0;[^}]*overflow:\s*hidden;/s);
    expect(source).toContain("...(files.value.length ? { y: \"100%\" } : {})");
  });
});

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
    api.deleteRecordingFile.mockResolvedValue({ code: 0, message: "", data: { id: "9007199254740993", deleted: true } });
    api.batchDeleteRecordingFiles.mockResolvedValue({ code: 0, message: "", data: { deletedCount: 1, failedCount: 0, results: [{ id: "9007199254740993", deleted: true }] } });
    api.stopActiveRecording.mockResolvedValue({ code: 0, message: "", data: { id: "77", channelId: "12", stopped: true } });
    enqueueDownload.mockReset();
    messages.success.mockReset();
    messages.error.mockReset();
    modalWarning.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("does not request while inactive and activates only the requested files panel", async () => {
    const wrapper = mount(RecordingWorkspacePanel, {
      props: { active: false, mode: "files" },
      global: { stubs }
    });
    await flushPromises();
    expect(api.listRecordingFiles).not.toHaveBeenCalled();
    expect(api.listActiveRecordings).not.toHaveBeenCalled();

    await wrapper.setProps({ active: true });
    await flushPromises();
    expect(api.listRecordingFiles).toHaveBeenCalledOnce();
    expect(api.listRecordingOptions).toHaveBeenCalledOnce();
    expect(api.listReconciliations).toHaveBeenCalledOnce();
    expect(api.listActiveRecordings).not.toHaveBeenCalled();
    expect(wrapper.emitted("stats")?.some(([payload]) => (payload as { filesTotal?: number }).filesTotal === 1)).toBe(true);
    wrapper.unmount();
  });

  it("activates tasks without issuing a recording-file query", async () => {
    const wrapper = mount(RecordingWorkspacePanel, {
      props: { active: true, mode: "tasks" },
      global: { stubs }
    });
    await flushPromises();
    expect(api.listActiveRecordings).toHaveBeenCalledOnce();
    expect(api.listRecordingFiles).not.toHaveBeenCalled();
    expect(api.listRecordingOptions).not.toHaveBeenCalled();
    expect(api.listReconciliations).not.toHaveBeenCalled();
    expect(wrapper.emitted("stats")?.some(([payload]) => (payload as { activeTotal?: number }).activeTotal === 1)).toBe(true);
    wrapper.unmount();
  });

  it("loads files without a default date range and keeps partial metadata explicit", async () => {
    api.listRecordingFiles.mockResolvedValue(pageResult([file("available", "partial")]));
    const wrapper = mount(CloudRecordings, { global: { stubs } });
    await flushPromises();
    const query = api.listRecordingFiles.mock.calls[0][0];
    expect(query.page).toBe(1);
    expect(query.start).toBeUndefined();
    expect(query.end).toBeUndefined();
    expect(wrapper.text()).toContain("待完善");
    expect(wrapper.text()).toContain("--");
    expect(wrapper.get("[data-testid='recording-table']").attributes("data-total")).toBe("1");
  });

  it("automatically refreshes the current list every 10 seconds with a countdown", async () => {
    vi.useFakeTimers();
    const wrapper = mount(CloudRecordings, { global: { stubs } });
    await flushPromises();

    const refresh = wrapper.get("[data-testid='recording-refresh']");
    expect(refresh.text()).toContain("10s");

    await vi.advanceTimersByTimeAsync(1000);
    expect(refresh.text()).toContain("9s");

    await vi.advanceTimersByTimeAsync(9000);
    await flushPromises();
    expect(api.listRecordingFiles).toHaveBeenCalledTimes(2);
    expect(refresh.text()).toContain("10s");

    wrapper.unmount();
  });

  it("uses one keyword input without rendering device and channel option catalogs", async () => {
    api.listRecordingOptions.mockResolvedValue({
      code: 0,
      message: "",
      data: {
        channels: [{ id: "12", code: "C1", name: "不应渲染的通道" }],
        devices: [{ id: "D1", name: "不应渲染的设备" }],
        nodes: [{ id: "8", name: "边缘节点 A" }]
      }
    });
    const wrapper = mount(CloudRecordings, { global: { stubs } });
    await flushPromises();

    expect(wrapper.findAll("select")).toHaveLength(3);
    expect(wrapper.text()).not.toContain("不应渲染的设备");
    expect(wrapper.text()).not.toContain("不应渲染的通道");
    const keyword = wrapper.get("[data-testid='recording-keyword']");
    expect(keyword.attributes("placeholder")).toBe("文件名 / 设备名称或编号 / 通道名称或编号");
    await keyword.setValue("  一号  ");
    await wrapper.get("[data-testid='recording-query']").trigger("click");
    await flushPromises();
    expect(api.listRecordingFiles.mock.calls.at(-1)?.[0]).toMatchObject({ keyword: "一号" });
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

  it("distinguishes reconciliation from the primary query action and renders a success status icon", async () => {
    api.listReconciliations.mockResolvedValue({ code: 0, message: "", data: { list: [{ status: "success" }] } });
    const wrapper = mount(CloudRecordings, { global: { stubs } });
    await flushPromises();

    expect(wrapper.get("[data-testid='recording-reconcile']").classes()).toContain("recording-reconcile-button");
    expect(wrapper.get("[data-testid='recording-reconcile']").attributes("data-type")).not.toBe("primary");
    expect(source).toMatch(/\.recording-reconcile-button\s*{[^}]*color-mix\(in srgb, #7c3aed 82%, var\(--uvp-text-primary\)\);[^}]*background:[^;]*#7c3aed 9%/s);
    expect(wrapper.get("[data-testid='reconciliation-summary']").text()).toContain("节点目录已对账");
    expect(wrapper.find("[data-testid='reconciliation-success-icon']").exists()).toBe(true);
  });

  it.each(["node_offline", "node_missing", "file_missing", "access_unavailable"])("hides access actions for %s", async availability => {
    api.listRecordingFiles.mockResolvedValue(pageResult([file(availability)]));
    const wrapper = mount(CloudRecordings, { global: { stubs } });
    await flushPromises();
    expect(wrapper.find("[data-testid='play-9007199254740993']").exists()).toBe(false);
    expect(wrapper.find("[data-testid='download-9007199254740993']").exists()).toBe(false);
  });

  it("renders operation icons alongside their text labels", async () => {
    const wrapper = mount(CloudRecordings, { global: { stubs } });
    await flushPromises();

    expect(wrapper.get("[data-testid='recording-actions-column']").attributes("width")).toBe("284");
    expect(wrapper.get(".cloud-recording-actions").classes()).toContain("cloud-recording-actions");
    expect(wrapper.get("[data-testid='detail-9007199254740993']").text()).toContain("详情");
    expect(wrapper.find("[data-testid='detail-icon-9007199254740993']").exists()).toBe(true);
    expect(wrapper.get("[data-testid='play-9007199254740993']").text()).toContain("播放");
    expect(wrapper.find("[data-testid='play-icon-9007199254740993']").exists()).toBe(true);
    expect(wrapper.get("[data-testid='download-9007199254740993']").text()).toContain("下载");
    expect(wrapper.find("[data-testid='download-icon-9007199254740993']").exists()).toBe(true);
  });

  it("shows row selection, batch deletion and a semantic delete action only with delete permission", async () => {
    account.permissions = ["gb28181:recording:view", "gb28181:recording:delete"];
    const wrapper = mount(CloudRecordings, { global: { stubs } });
    await flushPromises();

    expect(wrapper.find("[data-testid='delete-9007199254740993']").exists()).toBe(true);
    expect(source).toContain('v-model:selected-keys="selectedRowKeys"');
    expect(source).toContain('data-testid="recording-batch-delete"');
    expect(source).toContain('class="uvp-table-action uvp-table-action--delete"');

    await wrapper.get("[data-testid='select-first-recording']").trigger("click");
    await wrapper.get("[data-testid='recording-batch-delete']").trigger("click");
    const confirmation = modalWarning.mock.calls[0][0];
    expect(confirmation.content).toContain("1 个录像文件");
    await confirmation.onOk();
    await flushPromises();
    expect(api.batchDeleteRecordingFiles).toHaveBeenCalledWith(["9007199254740993"]);
  });

  it("separates active recordings from catalog files", async () => {
    const wrapper = mount(CloudRecordings, { global: { stubs } });
    await flushPromises();
    expect(wrapper.text()).toContain("record.mp4");
    expect(wrapper.get("[data-testid='recording-view-switch']").attributes("role")).toBe("group");
    expect(wrapper.find("[data-testid='files-tab-icon']").exists()).toBe(true);
    expect(wrapper.find("[data-testid='active-tab-icon']").exists()).toBe(true);
    expect(wrapper.get("[data-testid='files-tab']").attributes("aria-pressed")).toBe("true");
    await wrapper.get("[data-testid='active-tab']").trigger("click");
    await flushPromises();
    expect(wrapper.get("[data-testid='active-tab']").attributes("aria-pressed")).toBe("true");
    expect(wrapper.text()).toContain("正在录制");
    expect(wrapper.text()).not.toContain("record.mp4");
  });

  it("adds typed runtime control without mixing HLS state into the file catalog", () => {
    expect(source).toContain('data-testid="runtime-tab"');
    expect(source).toContain("RecordingRuntimeControl");
    expect(source).toContain("运行控制");
    expect(source).toContain("activeView === 'runtime'");
  });

  it("stops an active ZLMediaKit recording after confirmation when permitted", async () => {
    account.permissions = ["gb28181:recording:view", "gb28181:recording:stop"];
    const wrapper = mount(CloudRecordings, { global: { stubs } });
    await flushPromises();
    await wrapper.get("[data-testid='active-tab']").trigger("click");
    await flushPromises();
    await wrapper.get("[data-testid='stop-recording-77']").trigger("click");
    const confirmation = modalWarning.mock.calls[0][0];
    expect(confirmation.content).toContain("关闭该通道的云端录像");
    await confirmation.onOk();
    await flushPromises();
    expect(api.stopActiveRecording).toHaveBeenCalledWith("77");
    expect(messages.success).toHaveBeenCalledWith("录像已停止");
  });

  it("registers Cloud and creates a cookie-bound download task through the coordinator", async () => {
    const wrapper = mount(CloudRecordings, { global: { stubs } });
    await flushPromises();
    expect(getLucideIconComponent("lucide:Cloud")).toBeTruthy();
    await wrapper.get("[data-testid='download-9007199254740993']").trigger("click");
    await flushPromises();
    expect(enqueueDownload).toHaveBeenCalledWith({ fileId: "9007199254740993", fileName: "record.mp4" });
  });
});
