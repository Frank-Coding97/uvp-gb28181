import { describe, it, expect, vi, beforeEach } from "vitest";
import { mount } from "@vue/test-utils";
import { Message } from "@arco-design/web-vue";
import FirmwareUploadDialog from "./FirmwareUploadDialog.vue";
import * as api from "./api";

vi.mock("@arco-design/web-vue", async () => {
  const actual = await vi.importActual("@arco-design/web-vue");
  return {
    ...actual,
    Message: {
      success: vi.fn(),
      error: vi.fn(),
      warning: vi.fn()
    }
  };
});

vi.mock("./api");

describe("FirmwareUploadDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe("TC7.6: 文件大小限制 2GB", () => {
    it("应该拒绝超过 2GB 的文件", async () => {
      const wrapper = mount(FirmwareUploadDialog, {
        props: { visible: true }
      });

      const largeFile = new File(["x"], "large.bin", { type: "application/octet-stream" });
      Object.defineProperty(largeFile, "size", { value: 2 * 1024 * 1024 * 1024 + 1 });

      await wrapper.vm.handleFileChange([{ file: largeFile }]);
      wrapper.vm.touched.file = true;
      await wrapper.vm.$nextTick();

      expect(wrapper.vm.fileError).toContain("文件大小超过限制");
    });

    it("应该接受小于等于 2GB 的文件", async () => {
      const wrapper = mount(FirmwareUploadDialog, {
        props: { visible: true }
      });

      const validFile = new File(["x"], "valid.bin", { type: "application/octet-stream" });
      Object.defineProperty(validFile, "size", { value: 2 * 1024 * 1024 * 1024 });

      await wrapper.vm.handleFileChange([{ file: validFile }]);
      wrapper.vm.touched.file = true;
      await wrapper.vm.$nextTick();

      expect(wrapper.vm.fileError).toBe("");
    });
  });

  describe("TC7.7: 必填字段校验", () => {
    it("未填写文件时应显示错误", async () => {
      const wrapper = mount(FirmwareUploadDialog, {
        props: { visible: true }
      });

      wrapper.vm.touched.file = true;
      await wrapper.vm.$nextTick();

      expect(wrapper.vm.fileError).toBe("请选择固件文件");
    });

    it("未填写版本时应显示错误", async () => {
      const wrapper = mount(FirmwareUploadDialog, {
        props: { visible: true }
      });

      wrapper.vm.form.version = "";
      wrapper.vm.touched.version = true;
      await wrapper.vm.$nextTick();

      expect(wrapper.vm.versionError).toBe("请填写固件版本");
    });

    it("未填写厂商时应显示错误", async () => {
      const wrapper = mount(FirmwareUploadDialog, {
        props: { visible: true }
      });

      wrapper.vm.form.manufacturer = "";
      wrapper.vm.touched.manufacturer = true;
      await wrapper.vm.$nextTick();

      expect(wrapper.vm.manufacturerError).toBe("请填写厂商");
    });

    it("版本号超过 50 字符应显示错误", async () => {
      const wrapper = mount(FirmwareUploadDialog, {
        props: { visible: true }
      });

      wrapper.vm.form.version = "x".repeat(51);
      wrapper.vm.touched.version = true;
      await wrapper.vm.$nextTick();

      expect(wrapper.vm.versionError).toContain("不能超过 50 个字符");
    });

    it("厂商名称超过 100 字符应显示错误", async () => {
      const wrapper = mount(FirmwareUploadDialog, {
        props: { visible: true }
      });

      wrapper.vm.form.manufacturer = "x".repeat(101);
      wrapper.vm.touched.manufacturer = true;
      await wrapper.vm.$nextTick();

      expect(wrapper.vm.manufacturerError).toContain("不能超过 100 个字符");
    });
  });

  describe("TC7.8: 上传成功后触发 success 事件", () => {
    it("表单有效时应调用 API 并触发 success", async () => {
      const wrapper = mount(FirmwareUploadDialog, {
        props: { visible: true }
      });

      const mockFile = new File(["content"], "firmware.bin", { type: "application/octet-stream" });
      Object.defineProperty(mockFile, "size", { value: 1024 });

      wrapper.vm.form.file = mockFile;
      wrapper.vm.form.version = "v1.0.0";
      wrapper.vm.form.manufacturer = "海康威视";
      wrapper.vm.form.modelPattern = "DS-2CD.*";
      wrapper.vm.form.remark = "测试备注";

      vi.mocked(api.uploadFirmware).mockResolvedValue({ data: { id: 1 } } as any);

      const result = await wrapper.vm.handleUpload();

      expect(result).toBe(true);
      expect(api.uploadFirmware).toHaveBeenCalledWith({
        file: mockFile,
        version: "v1.0.0",
        manufacturer: "海康威视",
        modelPattern: "DS-2CD.*",
        remark: "测试备注"
      });
      expect(Message.success).toHaveBeenCalledWith("固件上传成功");
      expect(wrapper.emitted("success")).toBeTruthy();
      expect(wrapper.emitted("update:visible")?.[0]).toEqual([false]);
    });

    it("表单无效时应阻止提交", async () => {
      const wrapper = mount(FirmwareUploadDialog, {
        props: { visible: true }
      });

      // 不填写任何字段
      const result = await wrapper.vm.handleUpload();

      expect(result).toBe(false);
      expect(api.uploadFirmware).not.toHaveBeenCalled();
      expect(Message.error).toHaveBeenCalledWith("请检查表单填写");
    });

    it("API 失败时应显示错误消息", async () => {
      const wrapper = mount(FirmwareUploadDialog, {
        props: { visible: true }
      });

      const mockFile = new File(["content"], "firmware.bin", { type: "application/octet-stream" });
      Object.defineProperty(mockFile, "size", { value: 1024 });

      wrapper.vm.form.file = mockFile;
      wrapper.vm.form.version = "v1.0.0";
      wrapper.vm.form.manufacturer = "海康威视";

      const error = new Error("网络错误");
      vi.mocked(api.uploadFirmware).mockRejectedValue(error);

      const result = await wrapper.vm.handleUpload();

      expect(result).toBe(false);
      expect(Message.error).toHaveBeenCalledWith("网络错误");
      expect(wrapper.emitted("success")).toBeFalsy();
    });
  });

  describe("TC7.9: 关闭弹窗时重置表单", () => {
    it("visible 从 true 变为 false 时应重置所有字段", async () => {
      const wrapper = mount(FirmwareUploadDialog, {
        props: { visible: true }
      });

      const mockFile = new File(["content"], "firmware.bin", { type: "application/octet-stream" });
      wrapper.vm.form.file = mockFile;
      wrapper.vm.form.version = "v1.0.0";
      wrapper.vm.form.manufacturer = "海康威视";
      wrapper.vm.form.modelPattern = "DS-2CD.*";
      wrapper.vm.form.remark = "备注";
      wrapper.vm.touched.file = true;
      wrapper.vm.touched.version = true;
      wrapper.vm.touched.manufacturer = true;

      await wrapper.setProps({ visible: false });
      await wrapper.vm.$nextTick();

      expect(wrapper.vm.form.file).toBeNull();
      expect(wrapper.vm.form.version).toBe("");
      expect(wrapper.vm.form.manufacturer).toBe("");
      expect(wrapper.vm.form.modelPattern).toBe("");
      expect(wrapper.vm.form.remark).toBe("");
      expect(wrapper.vm.touched.file).toBe(false);
      expect(wrapper.vm.touched.version).toBe(false);
      expect(wrapper.vm.touched.manufacturer).toBe(false);
    });
  });

  describe("TC7.10: 可选字段 modelPattern 和 remark 为空时不传给后端", () => {
    it("modelPattern 和 remark 为空字符串时应传 undefined", async () => {
      const wrapper = mount(FirmwareUploadDialog, {
        props: { visible: true }
      });

      const mockFile = new File(["content"], "firmware.bin", { type: "application/octet-stream" });
      Object.defineProperty(mockFile, "size", { value: 1024 });

      wrapper.vm.form.file = mockFile;
      wrapper.vm.form.version = "v1.0.0";
      wrapper.vm.form.manufacturer = "海康威视";
      wrapper.vm.form.modelPattern = "   "; // 仅空白
      wrapper.vm.form.remark = ""; // 空字符串

      vi.mocked(api.uploadFirmware).mockResolvedValue({ data: { id: 1 } } as any);

      await wrapper.vm.handleUpload();

      expect(api.uploadFirmware).toHaveBeenCalledWith({
        file: mockFile,
        version: "v1.0.0",
        manufacturer: "海康威视",
        modelPattern: undefined,
        remark: undefined
      });
    });
  });
});
