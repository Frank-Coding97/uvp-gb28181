/**
 * 设备管理页 - 目录树 REST API client(B1 catalogtree)
 *
 * 后端路由前缀:/api/gb28181/device-mgmt/catalog/
 */

import { http } from "@/utils/http";
import { baseUrlApi } from "@/api/utils";
import type { BaseResult } from "@/api/types";

// ============================================================
// 类型定义
// ============================================================

export type NodeType = "civil_code" | "biz_group" | "virtual_org" | "device" | "channel";
export type NodeSource = "catalog" | "manual" | "auto";

export interface CatalogNode {
  id: number;
  tenantId: number;
  nodeType: NodeType;
  parentId: number | null;
  path: string;
  depth: number;
  name: string;
  code: string;
  civilCode: string;
  deviceId: number | null;
  channelId: number | null;
  source: NodeSource;
  sortOrder: number;
  anomaly: boolean;
  anomalyReason: string;
  rawCode: string;
  createdAt: string;
  updatedAt: string;
  deletedAt: string | null;
  /** withMountCount=1 时附加 */
  mountCount?: number;
}

export interface CatalogListResult {
  list: CatalogNode[];
  total: number;
}

export interface CatalogSubtreeResult {
  root: CatalogNode;
  list: CatalogNode[];
  total: number;
}

// ============================================================
// 接口
// ============================================================

/** 树根列表(parent_id IS NULL) */
export const getCatalogTreeRoots = () =>
  http.request<BaseResult<CatalogListResult>>("get", baseUrlApi("gb28181/device-mgmt/catalog/tree"));

/** 单节点 */
export const getCatalogNode = (id: number) =>
  http.request<BaseResult<CatalogNode>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/catalog/tree/${id}`)
  );

/** 子节点(可选挂载数派生) */
export const getCatalogChildren = (parentId: number, options?: { withMountCount?: boolean }) => {
  const params: Record<string, string | number> = {};
  if (options?.withMountCount) {
    params.withMountCount = 1;
  }
  return http.request<BaseResult<CatalogListResult>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/catalog/tree/${parentId}/children`),
    { params }
  );
};

/** 整子树(按物化路径 LIKE) */
export const getCatalogSubtree = (id: number) =>
  http.request<BaseResult<CatalogSubtreeResult>>(
    "get",
    baseUrlApi(`gb28181/device-mgmt/catalog/tree/${id}/subtree`)
  );

/** 未处理 anomaly 数(左侧底部入口角标) */
export const getAnomalyCount = () =>
  http.request<BaseResult<{ count: number }>>(
    "get",
    baseUrlApi("gb28181/device-mgmt/catalog/anomaly/count")
  );
