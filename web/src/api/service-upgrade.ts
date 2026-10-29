import { http } from "@/utils/http";
import { baseUrlApi } from "@/api/utils";
import type { BaseResult } from "@/api/types";
import type { AxiosProgressEvent } from "axios";

export interface UpgradeArtifact {
  deployment: string;
  os: string;
  arch: string;
  filename: string;
  url: string;
  size: number;
  sha256: string;
}

export interface UpgradeManifest {
  schema_version: number;
  channel: string;
  sequence: number;
  release_id: string;
  version: string;
  build_id: string;
  published_at?: string;
  release_notes?: string;
  min_version?: string;
  min_updater_protocol: number;
  artifacts: UpgradeArtifact[];
}

export interface UpgradeTask {
  release_notes?: string;
  published_at?: string;
  task_id: string;
  idempotency_key: string;
  actor: string;
  target_digest: string;
  version?: string;
  build_id?: string;
  phase: string;
  failure_phase?: string;
  error?: string;
  source?: string;
  committed: boolean;
  updated_at?: string;
}

export interface UpgradeStatus {
  current: { name?: string; version?: string; build_id?: string };
  deployment: string;
  releaseSourceConfigured: boolean;
  capabilities: { onlineCheck: boolean; offlineImport: boolean; apply: boolean; rollback: boolean; updaterProtocol: number };
  activeTask?: UpgradeTask;
  tasks: UpgradeTask[];
  events?: UpgradeEvent[];
}

/**
 * 后台定时检查的结果。助手每 6 小时自查一次官方源，这里只是读取缓存，
 * 不会触发网络请求（手动「检查在线更新」才会）。
 */
export interface UpgradeNotice {
  currentVersion?: string;
  latestVersion?: string;
  latestBuildId?: string;
  publishedAt?: string;
  releaseNotes?: string;
  checkedAt?: string;
  /** 官方源上确实有更高版本 */
  hasUpdate: boolean;
  /** 管理员把「恰好这一个版本」忽略了；更新版本出现时自动失效 */
  ignored: boolean;
  /** 离线部署没有官方源，此时不该显示任何更新标识 */
  checkEnabled: boolean;
  /** 定时检查开关；关掉后助手不再按点探测，手动检查仍然可用 */
  autoCheck: boolean;
}

export interface UpgradeEvent {
  event: string;
  task_id?: string;
  release_id?: string;
  build_id?: string;
  reason?: string;
  at: string;
}

export interface UpgradeMigrationPlan {
  dialect: string;
  applied: string[];
  pending: string[];
  unknown: string[];
  destructive: string[];
  can_apply: boolean;
  block_reason?: string;
}

/**
 * 离线升级包导入是独立于升级任务的持久任务。
 * 上传请求只负责把文件安全写入助手收件目录，校验结果由状态接口异步返回。
 */
export interface UpgradeImportJob {
  upload_id: string;
  job_id?: string;
  task_id?: string;
  phase: string;
  status?: string;
  error?: string;
  message?: string;
  artifact?: Partial<UpgradeImportArtifact>;
  result?: Partial<UpgradeImportArtifact>;
  staged?: Partial<UpgradeImportArtifact>;
  path?: string;
  filename?: string;
  size?: number;
  sha256?: string;
  release_id?: string;
  release_notes?: string;
  published_at?: string;
  version?: string;
  build_id?: string;
  deployment?: string;
  os?: string;
  arch?: string;
}

export interface UpgradeImportArtifact {
  path: string;
  filename: string;
  size: number;
  sha256: string;
  release_id: string;
  release_notes?: string;
  published_at?: string;
  version: string;
  build_id: string;
  deployment: string;
  os: string;
  arch: string;
}

// 这两个 GET 是页面首屏的「状态探针」：失败时页面已经有常驻横幅说明原因，
// 再走一次全局红 toast 就是同一次故障弹两条。调用方自己决定怎么提示。
export const getServiceUpgradeStatus = () =>
  http.request<BaseResult<UpgradeStatus>>("get", baseUrlApi("system/upgrade/status"), undefined, { showErrorMessage: false });
export const getServiceUpgradeMigrationPlan = () =>
  http.request<BaseResult<UpgradeMigrationPlan>>("get", baseUrlApi("system/upgrade/migration-plan"), undefined, {
    showErrorMessage: false
  });

export const checkServiceUpgrade = () =>
  http.request<BaseResult<{ manifest: UpgradeManifest; artifact: UpgradeArtifact; currentVersion: string; hasUpdate: boolean }>>(
    "post",
    baseUrlApi("system/upgrade/check")
  );

export const importServiceUpgrade = (file: File, onUploadProgress?: (event: AxiosProgressEvent) => void) => {
  const data = new FormData();
  data.append("file", file);
  return http.request<BaseResult<UpgradeImportJob>>("post", baseUrlApi("system/upgrade/import"), {
    data,
    headers: { "Content-Type": "multipart/form-data" },
    onUploadProgress,
    timeout: 30 * 60 * 1000
  });
};

export const getServiceUpgradeImport = (id: string) =>
  http.request<BaseResult<UpgradeImportJob>>("get", baseUrlApi(`system/upgrade/import/${encodeURIComponent(id)}`));

export const createServiceUpgradeTask = (idempotencyKey: string, targetDigest: string) =>
  http.request<BaseResult<UpgradeTask>>("post", baseUrlApi("system/upgrade/tasks"), {
    data: { idempotencyKey, targetDigest }
  });

export const getServiceUpgradeTask = (id: string) =>
  http.request<BaseResult<UpgradeTask>>("get", baseUrlApi(`system/upgrade/tasks/${encodeURIComponent(id)}`));

export const cancelServiceUpgradeTask = (id: string) =>
  http.request<BaseResult>("post", baseUrlApi(`system/upgrade/tasks/${encodeURIComponent(id)}/cancel`));

export const applyServiceUpgradeTask = (id: string) =>
  http.request<BaseResult<UpgradeTask>>("post", baseUrlApi(`system/upgrade/tasks/${encodeURIComponent(id)}/apply`));

export const rollbackServiceUpgradeTask = (id: string) =>
  http.request<BaseResult<UpgradeTask>>("post", baseUrlApi(`system/upgrade/tasks/${encodeURIComponent(id)}/rollback`));

export const getServiceUpgradeNotice = () =>
  http.request<BaseResult<UpgradeNotice>>("get", baseUrlApi("system/upgrade/notice"), undefined, {
    showErrorMessage: false
  });

export const ignoreServiceUpgradeNotice = (version: string) =>
  http.request<BaseResult>("post", baseUrlApi("system/upgrade/notice/ignore"), { data: { version } });

export const clearIgnoredServiceUpgradeNotice = () =>
  http.request<BaseResult>("delete", baseUrlApi("system/upgrade/notice/ignore"));

export const setServiceUpgradeAutoCheck = (enabled: boolean) =>
  http.request<BaseResult<{ enabled: boolean }>>("post", baseUrlApi("system/upgrade/notice/auto-check"), {
    data: { enabled }
  });
