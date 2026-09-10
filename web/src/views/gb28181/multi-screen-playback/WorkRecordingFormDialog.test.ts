import { mount, flushPromises } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
const api = vi.hoisted(() => ({ getWorkRecordingForm: vi.fn(), saveWorkRecordingForm: vi.fn() }));
vi.mock("@/api/gb28181-work-recording", () => api);
import WorkRecordingFormDialog from "./WorkRecordingFormDialog.vue";
const detail = { jobId: "job-a", channelId: 1, formVersion: 2, formState: "draft", schemaVersion: 1, deviceId: "", editable: true, form: { projectName: "已有项目", major: "接触网", workPersonnel: ["张三", "李四"] } };
const inputStub = { props: ["modelValue", "disabled", "placeholder"], emits: ["update:modelValue"], template: '<input :value="modelValue" :disabled="disabled" :placeholder="placeholder" @input="$emit(\'update:modelValue\', $event.target.value)" />' };
const mountDialog = () => mount(WorkRecordingFormDialog, { props: { visible: true, jobId: "job-a", channelName: "东门", canEdit: true }, global: { stubs: { 'a-modal': { template: '<section><slot /></section>' }, 'a-button': { props: ['disabled'], template: '<button :disabled="disabled"><slot /></button>' }, 'a-input': inputStub, 'a-textarea': inputStub } } });
function button(wrapper: ReturnType<typeof mountDialog>, text: string) { return wrapper.findAll("button").find(item => item.text() === text)!; }
describe("durable work form", () => {
    beforeEach(() => {
        api.getWorkRecordingForm.mockReset().mockResolvedValue({ code: 0, data: structuredClone(detail) });
        api.saveWorkRecordingForm.mockReset().mockImplementation(async (_id, version, form) => ({ code: 0, data: { ...detail, formVersion: version + 1, form } }));
    });
    it("restores server values and saves the bound job with its version and all personnel", async () => {
        const wrapper = mountDialog();
        await flushPromises();
        expect((wrapper.get('[placeholder="请输入项目名称"]').element as HTMLInputElement).value).toBe("已有项目");
        await wrapper.get('[placeholder="请输入项目名称"]').setValue("新项目");
        await button(wrapper, "保存草稿").trigger("click");
        await flushPromises();
        expect(api.saveWorkRecordingForm).toHaveBeenCalledWith("job-a", 2, expect.objectContaining({ projectName: "新项目", workPersonnel: ["张三", "李四"] }));
        expect(wrapper.text()).toContain("草稿已保存");
        await button(wrapper, "关闭").trigger("click");
        expect(wrapper.emitted("close")).toHaveLength(1);
    });
    it("preserves inputs on version conflict and prevents accidental discard", async () => {
        api.saveWorkRecordingForm.mockRejectedValue({ response: { data: { message: "版本已变化，请重新打开" } } });
        const wrapper = mountDialog();
        await flushPromises();
        await wrapper.get('[placeholder="请输入项目名称"]').setValue("尚未保存");
        await button(wrapper, "保存草稿").trigger("click");
        await flushPromises();
        expect(wrapper.get('[role="alert"]').text()).toContain("版本已变化");
        expect((wrapper.get('[placeholder="请输入项目名称"]').element as HTMLInputElement).value).toBe("尚未保存");
        await button(wrapper, "关闭").trigger("click");
        expect(wrapper.emitted("close")).toBeUndefined();
        await button(wrapper, "放弃修改并关闭").trigger("click");
        expect(wrapper.emitted("close")).toHaveLength(1);
        expect(wrapper.emitted("saved")).toBeUndefined();
    });
    it("does not expose a blank editable form after a failed read", async () => {
        api.getWorkRecordingForm.mockRejectedValue(new Error("读取失败"));
        const wrapper = mountDialog();
        await flushPromises();
        expect(wrapper.find('input').exists()).toBe(false);
        expect(button(wrapper, "保存草稿").attributes('disabled')).toBeDefined();
        expect(api.saveWorkRecordingForm).not.toHaveBeenCalled();
    });
    it("does not allow saving without the form permission", async () => {
        const wrapper = mountDialog();
        await wrapper.setProps({ canEdit: false });
        await flushPromises();
        expect(button(wrapper, "保存草稿")).toBeUndefined();
        expect(wrapper.get('input').attributes('disabled')).toBeDefined();
    });
});
