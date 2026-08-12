import { createPinia, setActivePinia } from "pinia";
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const coordinator = vi.hoisted(() => ({ cancel: vi.fn(), retry: vi.fn() }));
vi.mock("@/views/gb28181/cloud-recordings/recordingDownloadService", () => ({ recordingDownloadCoordinator: coordinator }));

import { useRecordingDownloadStore } from "@/store/modules/recording-downloads";
import RecordingDownloadCenter from "./RecordingDownloadCenter.vue";

const stubs = {
  "a-badge": { template: "<span><slot /></span>" },
  "a-tooltip": { template: "<span><slot /></span>" },
  "a-button": { emits: ["click"], template: "<button :aria-expanded='$attrs[`aria-expanded`]' :aria-label='$attrs[`aria-label`]' @click='$emit(`click`)'><slot name='icon' /><slot /></button>" },
  "a-drawer": { props: ["visible"], emits: ["update:visible"], template: "<aside v-if='visible'><slot name='title' /><slot /></aside>" },
  "a-empty": { props: ["description"], template: "<span>{{ description }}</span>" },
  "a-progress": { template: "<span />" }
};

describe("RecordingDownloadCenter", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    coordinator.cancel.mockReset();
    coordinator.retry.mockReset();
  });

  it("shows in-memory task progress and delegates cancel/retry without exposing URLs", async () => {
    const store = useRecordingDownloadStore();
    store.upsert({ taskId: "one", fileId: "file-1", fileName: "one.mp4", status: "streaming", bytesSent: 50, totalBytes: 100, createdAt: "now", expiresAt: "later" });
    store.upsert({ taskId: "two", fileId: "file-2", fileName: "two.mp4", status: "failed", bytesSent: 0, createdAt: "now", expiresAt: "later" });
    const wrapper = mount(RecordingDownloadCenter, { global: { stubs } });
    const trigger = wrapper.get("button[aria-label='下载任务']");
    expect(trigger.attributes("aria-expanded")).toBe("false");
    await trigger.trigger("click");
    expect(wrapper.text()).toContain("one.mp4");
    expect(wrapper.text()).toContain("50%");
    expect(wrapper.text()).not.toContain("contentUrl");
    await wrapper.get("button[aria-label='取消下载']").trigger("click");
    await wrapper.get("button[aria-label='重新下载']").trigger("click");
    expect(coordinator.cancel).toHaveBeenCalledWith("one");
    expect(coordinator.retry).toHaveBeenCalledWith("two");
  });
});
