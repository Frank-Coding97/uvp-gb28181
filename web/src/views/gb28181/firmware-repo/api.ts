import { http } from "@/utils/http";
import { baseUrlApi } from "@/api/utils";
import type { BaseResult } from "@/api/types";

export interface FirmwareRepository {
  id: number;
  firmwareId: string;
  version: string;
  manufacturer: string;
  modelPattern?: string;
  fileName: string;
  fileSize: number;
  fileHash: string;
  releaseDate?: string;
  status: "draft" | "published" | "archived";
  uploadedBy: number;
  deptId: number;
  remark?: string;
  createdAt: string;
  updatedAt: string;
}

export interface FirmwareListParams {
  page: number;
  pageSize: number;
  manufacturer?: string;
  status?: string;
}

export interface FirmwareListResult {
  list: FirmwareRepository[];
  total: number;
}

export interface UploadFirmwareParams {
  file: File;
  version: string;
  manufacturer: string;
  modelPattern?: string;
  remark?: string;
}

export interface DownloadLinkResult {
  downloadUrl: string;
  expiresAt: string;
}

// 上传固件
export function uploadFirmware(params: UploadFirmwareParams) {
  const formData = new FormData();
  formData.append("file", params.file);
  formData.append("version", params.version);
  formData.append("manufacturer", params.manufacturer);
  if (params.modelPattern) {
    formData.append("modelPattern", params.modelPattern);
  }
  if (params.remark) {
    formData.append("remark", params.remark);
  }

  return http.request<BaseResult<FirmwareRepository>>("post", baseUrlApi("gb28181/device-mgmt/firmware-repository"), {
    data: formData,
    headers: {
      "Content-Type": "multipart/form-data"
    }
  });
}

// 查询固件列表
export function listFirmware(params: FirmwareListParams) {
  return http.request<BaseResult<FirmwareListResult>>("get", baseUrlApi("gb28181/device-mgmt/firmware-repository"), {
    params
  });
}

// 删除固件
export function deleteFirmware(id: number) {
  return http.request<BaseResult<void>>("delete", baseUrlApi(`gb28181/device-mgmt/firmware-repository/${id}`));
}

// 生成下载链接
export function generateDownloadLink(id: number) {
  return http.request<BaseResult<DownloadLinkResult>>(
    "post",
    baseUrlApi(`gb28181/device-mgmt/firmware-repository/${id}/download-link`)
  );
}

// 获取固件详情
export function getFirmwareById(id: number) {
  return http.request<BaseResult<FirmwareRepository>>("get", baseUrlApi(`gb28181/device-mgmt/firmware-repository/${id}`));
}

// 更新固件状态
export function updateFirmwareStatus(id: number, status: "draft" | "published" | "archived") {
  return http.request<BaseResult<void>>("patch", baseUrlApi(`gb28181/device-mgmt/firmware-repository/${id}/status`), {
    data: { status }
  });
}
