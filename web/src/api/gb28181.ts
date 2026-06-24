import { http } from "@/utils/http";
import { baseUrlApi } from "./utils";
import { BaseResult } from "./types";

// ===== 设备 =====

export interface GbDevice {
    id: number;
    deviceId: string;
    name: string;
    transport: string;
    manufacturer: string;
    model: string;
    firmware: string;
    ip: string;
    port: number;
    status: number; // 0 离线 1 在线
    online: boolean; // 从事实派生
    keepaliveTime: string | null;
    registerTime: string | null;
}

export interface GbChannel {
    id: number;
    deviceId: string;
    channelId: string;
    name: string;
    manufacturer: string;
    model: string;
    parentId: string;
    status: number;
    streamId: string;
}

export type DeviceListResult = BaseResult<{
    list: GbDevice[];
    total: number;
    page: number;
    pageSize: number;
}>;

export type ChannelListResult = BaseResult<{
    list: GbChannel[];
    total: number;
}>;

// ===== 点播 =====

export interface PlayResult {
    streamId: string;
    ssrc: string;
    app: string;
    wsflvUrl: string;
    httpFlvUrl: string;
    hlsUrl: string;
    expireAt: number;
}

export type PlayApiResult = BaseResult<PlayResult>;

// ===== API =====

/** 设备列表(分页) */
export const listDevices = (params: { page?: number; pageSize?: number } = {}) =>
    http.request<DeviceListResult>("get", baseUrlApi("gb28181/device/list"), { params });

/** 某设备的通道列表 */
export const listChannels = (deviceId: string) =>
    http.request<ChannelListResult>("get", baseUrlApi(`gb28181/device/${deviceId}/channels`));

/** 发起点播 */
export const startPlay = (deviceId: string, channelId: string) =>
    http.request<PlayApiResult>("post", baseUrlApi(`gb28181/play/${deviceId}/${channelId}`));

/** 停播 */
export const stopPlay = (streamId: string) =>
    http.request<BaseResult<unknown>>("delete", baseUrlApi(`gb28181/play/${streamId}`));
