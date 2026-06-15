import { http } from '@/utils/http';
import { baseUrlApi } from "@/api/utils";

import { BaseResult } from "@/api/types";


export interface SysJobResultsData {
    id: number; // 自增主键
    jobId: string; // 任务ID
    status: string; // 执行状态
    error: string; // 错误信息
    startTime: string; // 开始时间
    endTime: string; // 结束时间
    duration: number; // 执行时长(纳秒)
    retryCount: number; // 重试次数
    createdAt: string; // 记录创建时间
}

export type SysJobResultsListResult = BaseResult<{
    list: SysJobResultsData[];
    total: number;
}>;

export interface SysJobResultsListParams {
    pageNum: number;
    pageSize: number;
    id?: number; // 自增主键
    jobId?: string; // 任务ID
    status?: string; // 执行状态
    error?: string; // 错误信息
    startTime?: string; // 开始时间
    endTime?: string; // 结束时间
    duration?: number; // 执行时长(纳秒)
    retryCount?: number; // 重试次数
    createdAt?: string; // 记录创建时间
}


export type SysJobResultsResult = BaseResult<SysJobResultsData>;

export const getSysJobResultsList = (params: SysJobResultsListParams) => {
    return http.request<SysJobResultsListResult>("get", baseUrlApi("sysJobResults/list"), { params });
};

export const deleteSysJobResults = (id: number) => {
    return http.request<BaseResult>("delete", baseUrlApi(`sysJobResults/delete`), { data: { id } });
};