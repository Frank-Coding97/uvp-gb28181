import { describe, it, expect, vi, beforeEach } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";
import { Message, Modal } from "@arco-design/web-vue";
import FirmwareRepoIndex from "./index.vue";
import * as api from "./api";
import type { FirmwareRepository } from "./api";

vi.mock("@arco-design/web-vue", async () => {
  const actual = await vi.importActual("@arco-design/web-vue");
  return {
    ...actual,
    Message: {
      success: vi.fn(),
      error: vi.fn()
    },
    Modal: {
      confirm: vi.fn()
    }
  };
});

vi.mock("./api");

const mockFirmwareList: FirmwareRepository[] = [
  {
    id: 1,
    firmwareId: "uuid-1",
    version: "v1.0.0",
    manufacturer: "海康威视",
    modelPattern: "DS-2CD.*",
    fileName: "firmware-v1.bin",
    fileSize: 104857600,
    fileHash: "sha256-abc",
    status: "published",
    uploadedBy: 1,
    deptId: 100,
    createdAt: "2024-01-01T10:00:00Z",
    updatedAt: "2024-01-01T10:00:00Z"
  },
  {
    id: 2,
    firmwareId: "uuid-2",
    version: "v2.0.0",
    manufacturer: "大华",
    fileName: "firmware-v2.bin",
    fileSize: 209715200,
    fileHash: "sha256-def",
    status: "draft",
    uploadedBy: 1,
    deptId: 100,
    createdAt: "2024-01-02T10:00:00Z",
    updatedAt: "2024-01-02T10:00:00Z"
  }
];

describe("firmware-repo/index", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(api.listFirmware).mockResolvedValue({
      data: { list: mockFirmwareList, total: 2 }
    } as any);
  });

  describe("TC7.11: 组件挂载时加载固件列表", () => {
    it("应该在 onMounted 时调用 listFirmware", async () => {
      mount(FirmwareRepoIndex);
      await flushPromises();

      expect(api.listFirmware).toHaveBeenCalledWith({
        page: 1,
        pageSize: 10,
        manufacturer: undefined,
        status: undefined
      });
    });

    it("应该将返回的数据填充到 table", async () => {
      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();

      expect(wrapper.vm.tableData).toEqual(mockFirmwareList);
      expect(wrapper.vm.pagination.total).toBe(2);
    });

    it("加载失败时应显示错误消息", async () => {
      vi.mocked(api.listFirmware).mockRejectedValue(new Error("网络错误"));

      mount(FirmwareRepoIndex);
      await flushPromises();

      expect(Message.error).toHaveBeenCalledWith("网络错误");
    });
  });

  describe("TC7.12: 厂商和状态筛选", () => {
    it("点击查询按钮应传递筛选参数", async () => {
      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();

      wrapper.vm.filters.manufacturer = "海康威视";
      wrapper.vm.filters.status = "published";
      await wrapper.vm.handleSearch();
      await flushPromises();

      expect(api.listFirmware).toHaveBeenCalledWith({
        page: 1,
        pageSize: 10,
        manufacturer: "海康威视",
        status: "published"
      });
    });

    it("点击重置按钮应清空筛选条件并重新查询", async () => {
      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();

      wrapper.vm.filters.manufacturer = "海康威视";
      wrapper.vm.filters.status = "published";
      wrapper.vm.pagination.current = 3;

      await wrapper.vm.handleReset();
      await flushPromises();

      expect(wrapper.vm.filters.manufacturer).toBe("");
      expect(wrapper.vm.filters.status).toBe("");
      expect(wrapper.vm.pagination.current).toBe(1);
      expect(api.listFirmware).toHaveBeenCalledWith({
        page: 1,
        pageSize: 10,
        manufacturer: undefined,
        status: undefined
      });
    });
  });

  describe("TC7.13: 分页参数正确传递", () => {
    it("切换页码时应更新 current 并重新查询", async () => {
      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();
      vi.clearAllMocks();

      await wrapper.vm.handlePageChange(3);
      await flushPromises();

      expect(wrapper.vm.pagination.current).toBe(3);
      expect(api.listFirmware).toHaveBeenCalledWith({
        page: 3,
        pageSize: 10,
        manufacturer: undefined,
        status: undefined
      });
    });

    it("切换每页条数时应重置页码到第 1 页", async () => {
      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();

      wrapper.vm.pagination.current = 5;
      await wrapper.vm.handlePageSizeChange(20);
      await flushPromises();

      expect(wrapper.vm.pagination.current).toBe(1);
      expect(wrapper.vm.pagination.pageSize).toBe(20);
      expect(api.listFirmware).toHaveBeenCalledWith({
        page: 1,
        pageSize: 20,
        manufacturer: undefined,
        status: undefined
      });
    });
  });

  describe("TC7.14: 下载操作生成临时链接并打开", () => {
    it("应该调用 generateDownloadLink 并打开新窗口", async () => {
      const mockOpen = vi.fn();
      window.open = mockOpen;

      vi.mocked(api.generateDownloadLink).mockResolvedValue({
        data: {
          downloadUrl: "http://localhost:8280/download/token-abc",
          expiresAt: "2024-01-01T12:00:00Z"
        }
      } as any);

      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();

      await wrapper.vm.handleDownload(mockFirmwareList[0]);
      await flushPromises();

      expect(api.generateDownloadLink).toHaveBeenCalledWith(1);
      expect(mockOpen).toHaveBeenCalledWith("http://localhost:8280/download/token-abc", "_blank");
      expect(Message.success).toHaveBeenCalledWith("下载链接已生成");
    });

    it("生成下载链接失败时应显示错误", async () => {
      vi.mocked(api.generateDownloadLink).mockRejectedValue(new Error("权限不足"));

      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();

      await wrapper.vm.handleDownload(mockFirmwareList[0]);
      await flushPromises();

      expect(Message.error).toHaveBeenCalledWith("权限不足");
    });
  });

  describe("TC7.15: 删除操作确认弹窗", () => {
    it("应该弹出确认对话框", async () => {
      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();

      wrapper.vm.handleDelete(mockFirmwareList[0]);

      expect(Modal.confirm).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "确认删除",
          content: '确定要删除固件 "v1.0.0" 吗？此操作不可恢复。',
          modalClass: "uvp-system-dialog"
        })
      );
    });

    it("用户确认后应调用 deleteFirmware 并刷新列表", async () => {
      let onOkCallback: (() => Promise<void>) | undefined;
      vi.mocked(Modal.confirm).mockImplementation((config: any) => {
        onOkCallback = config.onOk;
        return {} as any;
      });

      vi.mocked(api.deleteFirmware).mockResolvedValue({ data: {} } as any);

      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();
      vi.clearAllMocks();

      wrapper.vm.handleDelete(mockFirmwareList[0]);
      expect(onOkCallback).toBeDefined();

      await onOkCallback!();
      await flushPromises();

      expect(api.deleteFirmware).toHaveBeenCalledWith(1);
      expect(Message.success).toHaveBeenCalledWith("删除成功");
      expect(api.listFirmware).toHaveBeenCalled();
    });

    it("删除失败时应显示错误消息", async () => {
      let onOkCallback: (() => Promise<void>) | undefined;
      vi.mocked(Modal.confirm).mockImplementation((config: any) => {
        onOkCallback = config.onOk;
        return {} as any;
      });

      vi.mocked(api.deleteFirmware).mockRejectedValue(new Error("删除失败"));

      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();

      wrapper.vm.handleDelete(mockFirmwareList[0]);
      await onOkCallback!();
      await flushPromises();

      expect(Message.error).toHaveBeenCalledWith("删除失败");
    });
  });

  describe("TC7.16: 工具函数测试", () => {
    it("formatFileSize 应该正确格式化文件大小", () => {
      const wrapper = mount(FirmwareRepoIndex);

      expect(wrapper.vm.formatFileSize(0)).toBe("0 B");
      expect(wrapper.vm.formatFileSize(1024)).toBe("1.00 KB");
      expect(wrapper.vm.formatFileSize(1048576)).toBe("1.00 MB");
      expect(wrapper.vm.formatFileSize(1073741824)).toBe("1.00 GB");
      expect(wrapper.vm.formatFileSize(104857600)).toBe("100.00 MB");
    });

    it("getStatusColor 应该返回正确的颜色", () => {
      const wrapper = mount(FirmwareRepoIndex);

      expect(wrapper.vm.getStatusColor("draft")).toBe("gray");
      expect(wrapper.vm.getStatusColor("published")).toBe("green");
      expect(wrapper.vm.getStatusColor("archived")).toBe("orange");
      expect(wrapper.vm.getStatusColor("unknown")).toBe("gray");
    });

    it("getStatusText 应该返回正确的文本", () => {
      const wrapper = mount(FirmwareRepoIndex);

      expect(wrapper.vm.getStatusText("draft")).toBe("草稿");
      expect(wrapper.vm.getStatusText("published")).toBe("已发布");
      expect(wrapper.vm.getStatusText("archived")).toBe("已归档");
      expect(wrapper.vm.getStatusText("unknown")).toBe("unknown");
    });

    it("formatDateTime 应该格式化为中文日期时间", () => {
      const wrapper = mount(FirmwareRepoIndex);

      const result = wrapper.vm.formatDateTime("2024-01-15T14:30:00Z");
      // 结果依赖时区，只验证格式包含必要元素
      expect(result).toMatch(/\d{4}\/\d{2}\/\d{2}/);
      expect(result).toMatch(/\d{2}:\d{2}/);
    });
  });

  describe("TC7.17: 上传成功后刷新列表", () => {
    it("handleUploadSuccess 应该重置页码并重新加载", async () => {
      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();
      vi.clearAllMocks();

      wrapper.vm.pagination.current = 5;
      await wrapper.vm.handleUploadSuccess();
      await flushPromises();

      expect(wrapper.vm.pagination.current).toBe(1);
      expect(api.listFirmware).toHaveBeenCalled();
    });
  });
});
