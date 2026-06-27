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
    nearCapacity?: boolean;  // T3.4: 后端给出的容量预警标志
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

// 驱逐节点全部会话(高危,UI 必须二次确认),返回被踢的会话数
export const kickZLMNodeSessions = (id: number) =>
    http.request<BaseResult<{ count: number }>>("post", baseUrlApi(`gb28181/zlm/nodes/${id}/kick`));

// 重启 ZLM 服务(高危,所有流中断,UI 必须二次确认)
// graceMS 当前接口预留(ZLM 不支持 grace shutdown),后端忽略
export const restartZLMNode = (id: number, graceMS?: number) =>
    http.request<BaseResult<{ ok: boolean }>>(
        "post",
        baseUrlApi(`gb28181/zlm/nodes/${id}/restart`),
        { data: { graceMS: graceMS ?? 0 } }
    );

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

// ===== ZLM Scheduler(M3 T3.3)算法切换 + 调度日志 =====

export type SchedulerAlgorithm = "roundrobin" | "weighted" | "leastload";

export interface SchedulerInfo {
    algorithm: SchedulerAlgorithm | "";
    available: SchedulerAlgorithm[];
}

// 一条调度决策日志(对应后端 scheduler.SchedulerLog)
export interface SchedulerLogEntry {
    id: number;
    happenedAt: string;
    algorithm: string;
    nodeID: number;
    nodeName: string;
    streamID: string;
    deviceID: string;
    channelID: string;
    errorMessage: string;
}

// 查询当前算法 + 可用列表
export const getScheduler = () =>
    http.request<BaseResult<SchedulerInfo>>("get", baseUrlApi("gb28181/zlm/scheduler"));

// 切换算法(写 DB + Manager.Switch)
export const switchScheduler = (algorithm: SchedulerAlgorithm) =>
    http.request<BaseResult<{ algorithm: SchedulerAlgorithm }>>(
        "put",
        baseUrlApi("gb28181/zlm/scheduler"),
        { data: { algorithm } }
    );

// 最近 N 条调度日志(默认 100,上限 1000)
export const listSchedulerLogs = (limit = 100) =>
    http.request<BaseResult<{ list: SchedulerLogEntry[]; limit: number }>>(
        "get",
        baseUrlApi(`gb28181/zlm/scheduler/logs?limit=${limit}`)
    );
