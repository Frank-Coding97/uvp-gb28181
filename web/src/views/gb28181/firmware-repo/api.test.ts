import { describe, it, expect, vi, beforeEach } from "vitest";
import { http } from "@/utils/http";
import { uploadFirmware, listFirmware, deleteFirmware, generateDownloadLink, getFirmwareById } from "./api";
import type { UploadFirmwareParams, FirmwareListParams } from "./api";

vi.mock("@/utils/http");

describe("firmware-repo/api", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe("TC7.1: uploadFirmware 构造 multipart form 并调用后端", () => {
    it("应该构造正确的 FormData 并发送 POST 请求", async () => {
      const mockFile = new File(["content"], "firmware.bin", { type: "application/octet-stream" });
      const params: UploadFirmwareParams = {
        file: mockFile,
        version: "v1.2.3",
        manufacturer: "海康威视",
        modelPattern: "DS-2CD.*",
        remark: "测试固件"
      };

      const mockResponse = { data: { id: 1, firmwareId: "uuid-123" } };
      vi.mocked(http.request).mockResolvedValue(mockResponse as any);

      await uploadFirmware(params);

      expect(http.request).toHaveBeenCalledTimes(1);
      const [method, url, config] = vi.mocked(http.request).mock.calls[0];

      expect(method).toBe("post");
      expect(url).toContain("gb28181/device-mgmt/firmware-repository");
      expect(config?.data).toBeInstanceOf(FormData);
      expect(config?.headers?.["Content-Type"]).toBe("multipart/form-data");

      // 验证 FormData 内容
      const fd = config?.data as FormData;
      expect(fd.get("file")).toBe(mockFile);
      expect(fd.get("version")).toBe("v1.2.3");
      expect(fd.get("manufacturer")).toBe("海康威视");
      expect(fd.get("modelPattern")).toBe("DS-2CD.*");
      expect(fd.get("remark")).toBe("测试固件");
    });

    it("应该省略可选字段 modelPattern 和 remark", async () => {
      const mockFile = new File(["content"], "firmware.bin", { type: "application/octet-stream" });
      const params: UploadFirmwareParams = {
        file: mockFile,
        version: "v2.0.0",
        manufacturer: "大华"
      };

      vi.mocked(http.request).mockResolvedValue({ data: {} } as any);

      await uploadFirmware(params);

      const fd = vi.mocked(http.request).mock.calls[0][2]?.data as FormData;
      expect(fd.has("file")).toBe(true);
      expect(fd.has("version")).toBe(true);
      expect(fd.has("manufacturer")).toBe(true);
      expect(fd.has("modelPattern")).toBe(false);
      expect(fd.has("remark")).toBe(false);
    });
  });

  describe("TC7.2: listFirmware 支持分页和筛选参数", () => {
    it("应该发送带分页参数的 GET 请求", async () => {
      const params: FirmwareListParams = {
        page: 2,
        pageSize: 20
      };

      const mockResponse = { data: { list: [], total: 0 } };
      vi.mocked(http.request).mockResolvedValue(mockResponse as any);

      await listFirmware(params);

      expect(http.request).toHaveBeenCalledWith("get", expect.stringContaining("gb28181/device-mgmt/firmware-repository"), {
        params
      });
    });

    it("应该包含可选的筛选参数", async () => {
      const params: FirmwareListParams = {
        page: 1,
        pageSize: 10,
        manufacturer: "海康威视",
        status: "published"
      };

      vi.mocked(http.request).mockResolvedValue({ data: { list: [], total: 0 } } as any);

      await listFirmware(params);

      expect(http.request).toHaveBeenCalledWith("get", expect.stringContaining("gb28181/device-mgmt/firmware-repository"), {
        params
      });
    });
  });

  describe("TC7.3: deleteFirmware 调用 DELETE 端点", () => {
    it("应该发送正确的 DELETE 请求", async () => {
      const firmwareId = 123;
      vi.mocked(http.request).mockResolvedValue({ data: {} } as any);

      await deleteFirmware(firmwareId);

      expect(http.request).toHaveBeenCalledWith(
        "delete",
        expect.stringContaining(`gb28181/device-mgmt/firmware-repository/${firmwareId}`)
      );
    });
  });

  describe("TC7.4: generateDownloadLink 返回临时下载链接", () => {
    it("应该请求下载链接并返回 URL 和过期时间", async () => {
      const firmwareId = 456;
      const mockResponse = {
        data: {
          downloadUrl: "http://localhost:8280/api/download/token-abc",
          expiresAt: "2024-01-01T12:00:00Z"
        }
      };
      vi.mocked(http.request).mockResolvedValue(mockResponse as any);

      const result = await generateDownloadLink(firmwareId);

      expect(http.request).toHaveBeenCalledWith(
        "post",
        expect.stringContaining(`gb28181/device-mgmt/firmware-repository/${firmwareId}/download-link`)
      );
      expect(result.data.downloadUrl).toBe("http://localhost:8280/api/download/token-abc");
      expect(result.data.expiresAt).toBe("2024-01-01T12:00:00Z");
    });
  });

  describe("TC7.5: getFirmwareById 根据 ID 获取详情", () => {
    it("应该发送正确的 GET 请求获取固件详情", async () => {
      const firmwareId = 789;
      const mockFirmware = {
        id: 789,
        firmwareId: "uuid-789",
        version: "v3.0.0",
        manufacturer: "宇视",
        fileName: "firmware-v3.bin",
        fileSize: 104857600,
        fileHash: "sha256-abc",
        status: "published",
        uploadedBy: 1,
        deptId: 100,
        createdAt: "2024-01-01T00:00:00Z",
        updatedAt: "2024-01-01T00:00:00Z"
      };
      vi.mocked(http.request).mockResolvedValue({ data: mockFirmware } as any);

      const result = await getFirmwareById(firmwareId);

      expect(http.request).toHaveBeenCalledWith(
        "get",
        expect.stringContaining(`gb28181/device-mgmt/firmware-repository/${firmwareId}`)
      );
      expect(result.data.id).toBe(789);
      expect(result.data.version).toBe("v3.0.0");
    });
  });
});
