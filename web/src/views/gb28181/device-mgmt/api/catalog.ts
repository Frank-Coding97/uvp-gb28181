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
export function getCatalogTreeRoots(): Promise<BaseResult<CatalogListResult>> {
  return http.request({
    url: baseUrlApi("gb28181/device-mgmt/catalog/tree"),
    method: "get",
  });
}

/** 单节点 */
export function getCatalogNode(id: number): Promise<BaseResult<CatalogNode>> {
  return http.request({
    url: baseUrlApi(`gb28181/device-mgmt/catalog/tree/${id}`),
    method: "get",
  });
}

/** 子节点(可选挂载数派生) */
export function getCatalogChildren(
  parentId: number,
  options?: { withMountCount?: boolean }
): Promise<BaseResult<CatalogListResult>> {
  const params: Record<string, string | number> = {};
  if (options?.withMountCount) {
    params.withMountCount = 1;
  }
  return http.request({
    url: baseUrlApi(`gb28181/device-mgmt/catalog/tree/${parentId}/children`),
    method: "get",
    params,
  });
}

/** 整子树(按物化路径 LIKE) */
export function getCatalogSubtree(id: number): Promise<BaseResult<CatalogSubtreeResult>> {
  return http.request({
    url: baseUrlApi(`gb28181/device-mgmt/catalog/tree/${id}/subtree`),
    method: "get",
  });
}

/** 未处理 anomaly 数(左侧底部入口角标) */
export function getAnomalyCount(): Promise<BaseResult<{ count: number }>> {
  return http.request({
    url: baseUrlApi("gb28181/device-mgmt/catalog/anomaly/count"),
    method: "get",
  });
}
