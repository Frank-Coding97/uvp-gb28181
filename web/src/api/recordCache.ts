import { http } from "@/utils/http";
import { baseUrlApi } from "@/api/utils";
import type { BaseResult } from "@/api/types";

/**
 * 设备录像缓存任务的对外契约。
 *
 * ⛔ 这些字面量与后端 `recordcache` 包的状态常量逐字对应（`gb_record_cache_task.state`）。
 * 前端把它们写成"看起来像"的别的词（如 `finished`、`error`）不会报错，
 * 只会让状态标签永远走 default 分支 —— 表现是「任务跑完了但列表里一直显示处理中」。
 */
export type RecordCacheState = "queued" | "running" | "merging" | "succeeded" | "failed" | "cancelled" | "expired";

/** 一个已落盘的产出文件。一个任务可能有多个分片（长录像分段续拉）。 */
export interface RecordCacheFile {
  index: number;
  name: string;
  size: number;
  startTime: string;
  endTime: string;
  downloadable: boolean;
}

export interface RecordCacheTask {
  taskId: string;
  channelId: number;
  deviceId: string;
  channelCode: string;
  channelName: string;
  deviceName: string;
  startTime: string;
  endTime: string;
  recordType: string;
  downloadSpeed: number;
  state: RecordCacheState;
  lastError: string;
  cachedBytes: number;
  cachedSeconds: number;
  totalSeconds: number;
  progress: number;
  speedBytesPerSec: number;
  estimatedBytes: number;
  /**
   * 收藏标记。收藏后这条录像**不参与保留期自动清理**（后端 cleanupExpired 会跳过它），
   * 只有手动删除才会清掉文件。
   */
  favorite: boolean;
  files: RecordCacheFile[];
  createdByName: string;
  createdAt: string;
  startedAt: string;
  finishedAt: string;
  expiresAt: string;
}

export interface RecordCachePage {
  list: RecordCacheTask[];
  total: number;
  page: number;
  size: number;
}

export interface CreateRecordCacheRequest {
  channelId: number;
  recordKey: string;
  recordType?: string;
  playFrom?: string;
  downloadSpeed?: number;
}

export interface RecordCacheListQuery {
  page?: number;
  size?: number;
  state?: string;
  keyword?: string;
  channelId?: number;
  /**
   * 收藏筛选。`undefined` = 不筛（三态之一，别用 false 代替 ——
   * 后端把「传了 false」当成「只看未收藏」）。
   */
  favorite?: boolean;
}

const taskCollectionPath = "gb28181/record-cache/tasks";

function taskPath(taskId: string) {
  return `${taskCollectionPath}/${encodeURIComponent(taskId)}`;
}

/** 受理一个缓存任务（服务端后台拉流落盘，不占浏览器）。 */
export function createRecordCacheTask(data: CreateRecordCacheRequest) {
  return http.request<BaseResult<RecordCacheTask>>("post", baseUrlApi(taskCollectionPath), { data });
}

/**
 * 查询任务列表。
 *
 * `options.showErrorMessage` 必须是可关的：顶栏那个「缓存任务」角标是**后台轮询**在调它，
 * 服务端偶发抖动时每一轮都弹一次错误提示会把整个界面刷满 —— 而角标本来只是"顺带看一眼"。
 */
export function listRecordCacheTasks(params: RecordCacheListQuery = {}, options: { showErrorMessage?: boolean } = {}) {
  return http.request<BaseResult<RecordCachePage>>("get", baseUrlApi(taskCollectionPath), { params }, options);
}

export function getRecordCacheTask(taskId: string, options: { showErrorMessage?: boolean } = {}) {
  return http.request<BaseResult<RecordCacheTask>>("get", baseUrlApi(taskPath(taskId)), undefined, options);
}

export function cancelRecordCacheTask(taskId: string) {
  return http.request<BaseResult<RecordCacheTask>>("post", baseUrlApi(`${taskPath(taskId)}/cancel`), { data: {} });
}

export function deleteRecordCacheTask(taskId: string) {
  return http.request<BaseResult<{ taskId: string }>>("delete", baseUrlApi(taskPath(taskId)));
}

/**
 * 收藏 / 取消收藏。
 *
 * ⛔ 必须显式传布尔值：后端把「缺字段」当成参数不合法（422），
 * 不会把漏传悄悄当成 false —— 否则"取消收藏"和"没点"就分不开了。
 */
export function setRecordCacheFavorite(taskId: string, favorite: boolean) {
  return http.request<BaseResult<RecordCacheTask>>("post", baseUrlApi(`${taskPath(taskId)}/favorite`), {
    data: { favorite }
  });
}

/**
 * 缓存任务是否还在"推进"（决定列表要不要继续轮询）。
 *
 * ⛔ `merging`（收尾整理）必须算**推进中**：那几十秒里任务自己在动，不轮询的话
 * 列表会永远停在「整理中」，下载按钮再也不会出现 —— 只能靠用户手动刷新页面。
 *
 * ⛔ 但别把这个判断当成后端的 `RecordCacheTaskActive`：那个是**不含** merging 的，
 * 它的语义是"还占着设备通道"（整理阶段已经不占通道了）。两边含义不同，不要互相抄。
 */
export function isRecordCacheTaskActive(state: RecordCacheState) {
  return state === "queued" || state === "running" || state === "merging";
}

/**
 * 任务是否"还没结束"——也就是还能取消、还占着设备通道。
 *
 * ⛔ 单独写一个而不是复用上面那个：`merging`（收尾整理）**不能**取消 ——
 * 录像已经全部拉回来了，取消没有意义，后端会回 409「任务已结束，无法取消」。
 * 两个判断共用的话，整理中的行会一直挂着一个点了必然报错的「取消」。
 *
 * 取反（`!isRecordCacheTaskStoppable`）就是"可以删除"：**整理中的任务必须允许删除** ——
 * 合并协程万一卡死（超时兜底之前），取消不让点、删除再藏起来，这一行就再也清不掉了。
 */
export function isRecordCacheTaskStoppable(state: RecordCacheState) {
  return state === "queued" || state === "running";
}

/**
 * 一次「票据下载」的受理结果。
 *
 * `contentUrl` 是**同源相对路径**的免鉴权下载地址，凭据由后端种成 HttpOnly cookie
 * （一次性、只对这一个地址有效、60 秒内必须用掉）。
 */
export interface RecordCacheDownloadCreation {
  task: { taskId: string; status: string; bytesSent: number; totalBytes?: number };
  contentUrl: string;
}

/**
 * 签发一次原生下载。
 *
 * ⛔ 这是"点下载立刻开始、进度是真实网速"的唯一入口 —— 别再退回 XHR 取 Blob：
 * 整包收进内存的下载没有下载栏条目、没有进度、不能暂停续传，1GB 级文件还会把标签页拖崩。
 * 浏览器自己发起的下载带不了 JWT 头，所以必须靠这张一次性票据（同仓云录像已跑通同一套）。
 *
 * ⛔ 不传 `index` = 整段录像（多分片时是后端在收尾期合好的那一个文件）；
 * 传了 = 只要第 `index` 片 —— 目标会写进 `contentUrl` 的**路径**里，
 * 于是这张凭据也只对那一段有效。
 */
export function createRecordCacheDownload(taskId: string, index?: number) {
  return http.request<BaseResult<RecordCacheDownloadCreation>>("post", baseUrlApi(`${taskPath(taskId)}/downloads`), {
    data: index === undefined ? {} : { index }
  });
}

/**
 * 后端应该返回的 `contentUrl`。
 *
 * ⛔ 必须拿来和后端给的地址**逐字比对**：那个地址会被直接交给浏览器当导航目标 ——
 * 后端哪天返回了绝对地址（或有人把路径拼错），浏览器就会带着这张一次性凭据
 * 跑去访问别的站点。同时它也是"cookie Path 与前端预期不一致"的哨兵：
 * 不一致时浏览器不会带上票据，表现是点下载静默 403。
 */
export function recordCacheDownloadContentPath(downloadId: string, index?: number) {
  const base = `/api/gb28181/record-cache/downloads/${downloadId}`;
  return index === undefined ? `${base}/content` : `${base}/segments/${index}/content`;
}

/** 把字节数格式化成人类可读的大小。 */
export function formatByteSize(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 100 || unit === 0 ? Math.round(value) : value.toFixed(1)} ${units[unit]}`;
}

/** 把秒数格式化成 `1小时23分` 这样的时长。 */
export function formatDuration(seconds: number) {
  const total = Math.max(0, Math.round(seconds));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const secs = total % 60;
  if (hours > 0) return `${hours}小时${minutes}分`;
  if (minutes > 0) return `${minutes}分${secs}秒`;
  return `${secs}秒`;
}
