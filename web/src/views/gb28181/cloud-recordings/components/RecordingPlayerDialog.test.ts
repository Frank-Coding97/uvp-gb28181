import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

const issueRecordingAccess = vi.hoisted(() => vi.fn());
vi.mock("../api", async importOriginal => ({
  ...(await importOriginal<typeof import("../api")>()),
  issueRecordingAccess,
  contentURL: (id: string, capability: string) => `/content/${id}?cap=${capability}`
}));

import RecordingPlayerDialog from "./RecordingPlayerDialog.vue";

const recording = {
  id: "9007199254740993",
  fileName: "record.mp4",
  channelName: "东门",
  startTime: "2026-08-10T12:00:00Z"
};

const stubs = {
  "a-modal": { props: ["visible"], template: "<section v-if='visible'><slot name='title' /><slot /></section>" },
  "a-spin": { template: "<div><slot /></div>" },
  "a-alert": { template: "<div><slot /></div>" },
  "a-button": { emits: ["click"], template: "<button :data-testid='$attrs[`data-testid`]' @click='$emit(`click`)'><slot /></button>" }
};

describe("RecordingPlayerDialog", () => {
  beforeEach(() => {
    issueRecordingAccess.mockReset();
    issueRecordingAccess
      .mockResolvedValueOnce({ code: 0, message: "", data: { mode: "play", capability: "first", expiresAt: "2026-08-10T13:00:00Z" } })
      .mockResolvedValueOnce({ code: 0, message: "", data: { mode: "play", capability: "renewed", expiresAt: "2026-08-10T13:00:00Z" } });
  });

  it("uses native MP4 content and renews at most once while preserving position", async () => {
    const wrapper = mount(RecordingPlayerDialog, {
      props: { visible: true, recording },
      global: { stubs }
    });
    await flushPromises();
    const video = wrapper.get("video");
    expect(video.attributes("src")).toBe("/content/9007199254740993?cap=first");
    Object.defineProperty(video.element, "currentTime", { configurable: true, writable: true, value: 37.5 });
    await video.trigger("error");
    await flushPromises();
    expect(issueRecordingAccess).toHaveBeenCalledTimes(2);
    expect(video.attributes("src")).toBe("/content/9007199254740993?cap=renewed");
    await video.trigger("loadedmetadata");
    expect((video.element as HTMLVideoElement).currentTime).toBe(37.5);
    await video.trigger("error");
    await flushPromises();
    expect(issueRecordingAccess).toHaveBeenCalledTimes(2);
    expect(wrapper.text()).toContain("播放失败");
  });
});
