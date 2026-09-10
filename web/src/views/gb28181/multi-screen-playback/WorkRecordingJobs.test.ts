import { mount, flushPromises } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
const api = vi.hoisted(() => ({ listWorkRecordings: vi.fn(), stopWorkRecording: vi.fn() }));
vi.mock("@/api/gb28181-work-recording", () => api);
import WorkRecordingJobs from "./WorkRecordingJobs.vue";
const job = { id: "job-a", channelId: 1, channelName: "东门", state: "recording", startedAt: null };
const mountDialog = () => mount(WorkRecordingJobs, { props: { visible: false, canStop: true }, global: { stubs: { 'a-modal': { template: '<section><slot /></section>' }, 'a-button': { template: '<button><slot /></button>' }, 'a-empty': true, 'a-pagination': true } } });
describe("work recording recovery list", () => {
    beforeEach(() => {
        api.listWorkRecordings.mockReset().mockResolvedValue({ code: 0, data: { items: [job], total: 1, page: 1, pageSize: 10 } });
        api.stopWorkRecording.mockReset().mockResolvedValue({ code: 0, data: { ...job, state: "stopped" } });
    });
    it("loads durable jobs on open and stops the exact job without needing a playback window", async () => {
        const wrapper = mountDialog();
        await wrapper.setProps({ visible: true });
        await flushPromises();
        expect(api.listWorkRecordings).toHaveBeenCalledWith(1);
        expect(wrapper.text()).toContain("东门");
        await wrapper.findAll("button").find(button => button.text() === "结束录制")!.trigger("click");
        await flushPromises();
        expect(api.stopWorkRecording).toHaveBeenCalledWith("job-a");
        expect(wrapper.emitted("changed")).toHaveLength(1);
    });
    it("does not stop anything on close and hides stop without permission", async () => {
        const wrapper = mountDialog();
        await wrapper.setProps({ visible: true, canStop: false });
        await flushPromises();
        expect(wrapper.text()).not.toContain("结束录制");
        await wrapper.setProps({ visible: false });
        expect(api.stopWorkRecording).not.toHaveBeenCalled();
    });
    it("keeps failed stop visible and never claims completion", async () => {
        api.stopWorkRecording.mockRejectedValue({ response: { data: { message: "节点不可达" } } });
        const wrapper = mountDialog();
        await wrapper.setProps({ visible: true });
        await flushPromises();
        await wrapper.findAll("button").find(button => button.text() === "结束录制")!.trigger("click");
        await flushPromises();
        expect(wrapper.get('[role="alert"]').text()).toBe("节点不可达");
        expect(wrapper.emitted("changed")).toBeUndefined();
    });
});
