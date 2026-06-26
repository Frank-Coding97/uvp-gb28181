import { http } from "@/utils/http";
import { baseUrlApi } from "./utils";
import { BaseResult } from "./types";

// ===== ZLM 节点 =====

export type ZLMNodeState = "active" | "maintenance" | "offline";

export interface ZLMNodeStats {
    lastHeartbeatAt: string;
    mediaSourceCount: number;
    sessionCount: number;
    netThreadLoadAvg: number;
    workThreadLoadAvg: number;
    memoryUsageBytes: number;
    totalBytesIn: number;
    totalBytesOut: number;
}

export interface ZLMNode {
    id: number;
    name: string;
    host: string;
    apiPort: number;
    mediaServerUUID: string;
    weight: number;
    tags?: Record<string, string>;
    state: ZLMNodeState;
    rtpPortStart: number;
    rtpPortEnd: number;
    stats: ZLMNodeStats;
    createdAt: string;
    updatedAt: string;
}

export interface CreateZLMNodeReq {
    name: string;
    host: string;
    apiPort: number;
    apiSecret: string;
    weight?: number;
    tags?: Record<string, string>;
    rtpPortStart?: number;
    rtpPortEnd?: number;
}

export interface UpdateZLMNodeReq {
    name?: string;
    apiSecret?: string;
    weight?: number;
    tags?: Record<string, string>;
    rtpPortStart?: number;
    rtpPortEnd?: number;
}

export const listZLMNodes = () =>
    http.request<BaseResult<{ list: ZLMNode[] }>>("get", baseUrlApi("gb28181/zlm/nodes"));

export const getZLMNode = (id: number) =>
    http.request<BaseResult<ZLMNode>>("get", baseUrlApi(`gb28181/zlm/nodes/${id}`));

export const createZLMNode = (body: CreateZLMNodeReq) =>
    http.request<BaseResult<ZLMNode>>("post", baseUrlApi("gb28181/zlm/nodes"), { data: body });

export const updateZLMNode = (id: number, body: UpdateZLMNodeReq) =>
    http.request<BaseResult<ZLMNode>>("put", baseUrlApi(`gb28181/zlm/nodes/${id}`), { data: body });

export const deleteZLMNode = (id: number) =>
    http.request<BaseResult<{ ok: boolean }>>("delete", baseUrlApi(`gb28181/zlm/nodes/${id}`));

export const setZLMNodeMaintenance = (id: number) =>
    http.request<BaseResult<{ ok: boolean }>>("post", baseUrlApi(`gb28181/zlm/nodes/${id}/maintenance`));

export const activateZLMNode = (id: number) =>
    http.request<BaseResult<{ ok: boolean }>>("post", baseUrlApi(`gb28181/zlm/nodes/${id}/activate`));

// ===== ZLM 节点配置 =====

export interface ConfigItem {
    key: string;
    value: string;
    default: string;
    hotReloadable: boolean;
    restartRequired: boolean;
    comment: string;
}

export interface ConfigGroup {
    name: string;
    items: ConfigItem[];
}

export interface UpdateConfigResp {
    applied: string[];
    requiresRestart: string[];
    unknown: string[];
}

export interface TestConnectionResult {
    online: boolean;
    httpPort?: string;
    error?: string;
}

export const getZLMNodeConfig = (id: number) =>
    http.request<BaseResult<{ groups: ConfigGroup[] }>>("get", baseUrlApi(`gb28181/zlm/nodes/${id}/config`));

export const updateZLMNodeConfig = (id: number, changes: Record<string, string>) =>
    http.request<BaseResult<UpdateConfigResp>>("put", baseUrlApi(`gb28181/zlm/nodes/${id}/config`), {
        data: { changes }
    });

export const testZLMNodeConnection = (id: number) =>
    http.request<BaseResult<TestConnectionResult>>("post",
        baseUrlApi(`gb28181/zlm/nodes/${id}/config/test-connection`));
