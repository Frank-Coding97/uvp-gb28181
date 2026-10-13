import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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
    // ⛔ 桩要照**真实 Arco** 建模：组件里用的是 Modal.warning / Modal.info，
    //   这里原先只 stub 了 confirm，于是组件一换弹窗形态就报
    //   "Modal.warning is not a function"（桩按"我们以为的 prop"建模的典型坑）。
    Modal: {
      confirm: vi.fn(),
      warning: vi.fn(),
      info: vi.fn(),
      success: vi.fn(),
      error: vi.fn()
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

const realWindowOpen = window.open;
// ⛔ happy-dom 对 `<a href=...>` 的赋值会**自发去连 localhost:3000**（预取/解析），
//   下载回退用例里于是刷出一屏 AggregateError/ECONNREFUSED 噪声 —— 它不是失败，
//   但会掩盖真正的错误信息。这里统一拦掉 anchor 的导航属性。
const realAnchorClick = HTMLAnchorElement.prototype.click;

describe("firmware-repo/index", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // ⛔ 必须还原 window.open：下载用例会直接替换它，若不还原会**泄漏到后续用例**，
    //   某个用例拿到真实 open 就去发导航请求，刷一屏 ECONNREFUSED 噪声、掩盖真失败。
    window.open = realWindowOpen;
    // 同上：把 anchor.click 换成空实现，验证"走了回退路径"但不真发请求
    vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
    vi.mocked(api.listFirmware).mockResolvedValue({
      data: { list: mockFirmwareList, total: 2 }
    } as any);
  });

  afterEach(() => {
    window.open = realWindowOpen;
    vi.restoreAllMocks();
    HTMLAnchorElement.prototype.click = realAnchorClick;
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
          downloadUrl: "/api/gb28181/device-mgmt/firmware-repository/download/token-abc",
          expiresAt: "2024-01-01T12:00:00Z"
        }
      } as any);

      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();

      await wrapper.vm.handleDownload(mockFirmwareList[0]);
      await flushPromises();

      expect(api.generateDownloadLink).toHaveBeenCalledWith(1);
      expect(mockOpen).toHaveBeenCalledWith("/api/gb28181/device-mgmt/firmware-repository/download/token-abc", "_blank");
      expect(Message.success).toHaveBeenCalledWith("已开始下载");
    });

    it("window.open 被拦截时应回退到隐藏 a 标签，不能让下载无声失败", async () => {
      window.open = vi.fn(() => null);
      // ⛔ anchor.click 已在 beforeEach 里被统一 mock 掉（避免 happy-dom 真发请求），
      //   这里只验证"确实走了回退路径"。

      vi.mocked(api.generateDownloadLink).mockResolvedValue({
        data: {
          downloadUrl: "/api/gb28181/device-mgmt/firmware-repository/download/token-abc",
          expiresAt: "2024-01-01T12:00:00Z"
        }
      } as any);

      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();

      await wrapper.vm.handleDownload(mockFirmwareList[0]);
      await flushPromises();

      expect(HTMLAnchorElement.prototype.click).toHaveBeenCalled();
      expect(Message.success).toHaveBeenCalledWith("已开始下载");
    });

    it("后端没返回链接时应报错，不能拿 undefined 去开窗口", async () => {
      window.open = vi.fn();
      vi.mocked(api.generateDownloadLink).mockResolvedValue({ data: {} } as any);

      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();

      await wrapper.vm.handleDownload(mockFirmwareList[0]);
      await flushPromises();

      expect(window.open).not.toHaveBeenCalled();
      expect(Message.error).toHaveBeenCalled();
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
    /**
     * ⛔ 基准 = 设备列表删除设备（`device-mgmt/index.vue` handleDeleteDevice）的真实配置。
     *   只抠**共享的配置项**做期望值（不含 title/content —— 那是各页自己的业务文案，
     *   固件叫"删除固件"、设备叫"删除设备"，本来就不该相等）。
     *   基准页改了这两项这里就会红，不会出现"两边各写一套、看着像但其实不一样"的漂移。
     */
    const deviceListDeleteConfirm = {
      hideCancel: false,
      okText: "删除",
      cancelText: "取消",
      okButtonProps: { status: "danger" }
    };

    // ⛔ 契约：与设备列表**逐字对齐**。
    //   两条最容易被忽略、且一眼就能看出来的差异：
    //   ① 用 Modal.warning 而不是 Modal.confirm —— 两者都是 simple 弹窗，但 warning 会渲染
    //      一个橙色感叹号图标，confirm 没有（见 arco modal.js messageType==="warning" 分支）；
    //   ② **不传 modalClass** —— `uvp-system-dialog` 是给 `<a-modal modal-class>` 用的（挂在
    //      .arco-modal 面板上），simple 弹窗结构不同、挂不上去，设备列表也没挂。
    it("应该用与设备列表一致的危险确认弹窗", async () => {
      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();

      wrapper.vm.handleDelete(mockFirmwareList[0]);

      expect(Modal.warning).toHaveBeenCalledWith(expect.objectContaining(deviceListDeleteConfirm));

      const config = vi.mocked(Modal.warning).mock.calls[0][0] as any;
      // ⛔ 不得再挂 modalClass：与设备列表保持一致，且对 simple 弹窗无效
      expect(config.modalClass).toBeUndefined();
      // 弹窗文案要点名固件版本与文件名，避免"删的是哪个"看不懂
      expect(config.content).toContain("v1.0.0");
      expect(config.content).toContain("firmware-v1.bin");
    });

    it("⛔ 不得退回 Modal.confirm（confirm 没有警告图标，和设备列表观感不同）", async () => {
      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();

      wrapper.vm.handleDelete(mockFirmwareList[0]);
      wrapper.vm.handleArchive(mockFirmwareList[0]);

      expect(Modal.confirm).not.toHaveBeenCalled();
    });

    it("归档弹窗同样用 danger 色确认按钮（全仓 9 处惯例，无一用 warning）", async () => {
      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();

      wrapper.vm.handleArchive(mockFirmwareList[0]);

      const config = vi.mocked(Modal.warning).mock.calls[0][0] as any;
      expect(config.okButtonProps).toEqual({ status: "danger" });
      expect(config.modalClass).toBeUndefined();
    });

    it("用户确认后应调用 deleteFirmware 并刷新列表", async () => {
      let onOkCallback: (() => Promise<void>) | undefined;
      vi.mocked(Modal.warning).mockImplementation((config: any) => {
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
      expect(Message.success).toHaveBeenCalledWith("固件已删除");
      expect(api.listFirmware).toHaveBeenCalled();
    });

    it("删掉当前页最后一条时应回退一页，避免停在越界空页（看着像没删掉）", async () => {
      let onOkCallback: (() => Promise<void>) | undefined;
      vi.mocked(Modal.warning).mockImplementation((config: any) => {
        onOkCallback = config.onOk;
        return {} as any;
      });
      vi.mocked(api.deleteFirmware).mockResolvedValue({ data: {} } as any);

      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();

      // 造出「第 3 页只剩 1 条」的形状
      wrapper.vm.pagination.current = 3;
      wrapper.vm.tableData = [mockFirmwareList[0]];
      vi.clearAllMocks();

      wrapper.vm.handleDelete(mockFirmwareList[0]);
      await onOkCallback!();
      await flushPromises();

      expect(api.listFirmware).toHaveBeenCalledWith(expect.objectContaining({ page: 2 }));
    });

    // 后端对"已被设备升级记录引用"返回明确原因，必须原样透传，
    // 不能被二次包装成"删除失败"把原因吃掉（那是用户唯一能判断该怎么办的信息）。
    it("被设备升级记录引用时原样透传后端原因", async () => {
      let onOkCallback: (() => Promise<void>) | undefined;
      vi.mocked(Modal.warning).mockImplementation((config: any) => {
        onOkCallback = config.onOk;
        return {} as any;
      });
      vi.mocked(api.deleteFirmware).mockRejectedValue(new Error("固件: 已被设备升级记录引用，无法删除（3 条设备升级记录）"));

      const wrapper = mount(FirmwareRepoIndex);
      await flushPromises();

      wrapper.vm.handleDelete(mockFirmwareList[0]);
      await onOkCallback!();
      await flushPromises();

      expect(Message.error).toHaveBeenCalledWith("固件: 已被设备升级记录引用，无法删除（3 条设备升级记录）");
    });

    it("删除失败时应显示错误消息", async () => {
      let onOkCallback: (() => Promise<void>) | undefined;
      vi.mocked(Modal.warning).mockImplementation((config: any) => {
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

  describe("TC7.18: 操作列居中（与其它列表页一致）", () => {
    /**
     * ⛔ 契约：操作列必须 `align="center"`，否则按钮整体偏左、右侧留一截空白。
     *   原因：`.uvp-table-actions` 虽是 `inline-flex + justify-content:center`，
     *   但那只在**它自己的盒子内**居中；单元格默认左对齐，盒子紧贴左边 ⇒ 两侧不对称。
     *   全仓操作列主流写法就是 `align="center"`（9 处），设备列表亦然。
     *   —— 老板 2026-10-06 亲自指出"两侧留对等的空白"。
     */
    it("操作列声明 align=center", () => {
      const wrapper = mount(FirmwareRepoIndex);
      const actionsColumn = wrapper.vm.columns.find((column: any) => column.title === "操作");
      expect(actionsColumn).toBeDefined();
      expect(actionsColumn?.align).toBe("center");
      expect(actionsColumn?.fixed).toBe("right");
    });

    it("操作列容器挂了统一操作区 class（内部按整组居中）", () => {
      // 表格桩不会渲染 slot，用源码断言兜底：容器 class 必须同时含统一类与本页补充类，
      // 且补充样式里带 justify-content:center。
      const source = readFileSync(resolve(process.cwd(), "src/views/gb28181/firmware-repo/index.vue"), "utf8");
      expect(source).toMatch(/class="uvp-table-actions firmware-repo-actions"/);
      expect(source).toMatch(/\.firmware-repo-actions\s*\{[^}]*justify-content:\s*center/s);
      // ⛔ 不得用 flex-wrap:nowrap —— 全局是 inline-flex，nowrap 会让窄屏溢出 fixed 列
      expect(source).not.toMatch(/\.firmware-repo-actions\s*\{[^}]*flex-wrap:\s*nowrap/s);
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
