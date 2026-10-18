import { http } from "@/utils/http";
import { baseUrlApi } from "@/api/utils";
import type { BaseResult } from "@/api/types";

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
  policy_version?: number;
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

export interface UpgradeEvent {
  event: string;
  task_id?: string;
  policy_version?: number;
  release_id?: string;
  build_id?: string;
  reason?: string;
  at: string;
}

export interface UpgradePolicy {
  enabled: boolean;
  policy_version: number;
  timezone: string;
  window_start: string;
  window_end: string;
  channel: string;
  deployment?: string;
  os?: string;
  arch?: string;
  patch_only: boolean;
  allow_restart: boolean;
  auto_rollback: boolean;
  /** 兼容后端旧字段；首版只记录页面和本地日志事件，不提供外部通知渠道。 */
  notification_target?: string;
  retain_versions: number;
  created_by?: string;
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

export const getServiceUpgradeStatus = () => http.request<BaseResult<UpgradeStatus>>("get", baseUrlApi("system/upgrade/status"));
export const getServiceUpgradePolicy = () => http.request<BaseResult<UpgradePolicy>>("get", baseUrlApi("system/upgrade/policy"));
export const getServiceUpgradeMigrationPlan = () =>
  http.request<BaseResult<UpgradeMigrationPlan>>("get", baseUrlApi("system/upgrade/migration-plan"));
export const updateServiceUpgradePolicy = (policy: UpgradePolicy) =>
  http.request<BaseResult<UpgradePolicy>>("put", baseUrlApi("system/upgrade/policy"), { data: policy });

export const checkServiceUpgrade = () =>
  http.request<BaseResult<{ manifest: UpgradeManifest; artifact: UpgradeArtifact; currentVersion: string; hasUpdate: boolean }>>(
    "post",
    baseUrlApi("system/upgrade/check")
  );

export const importServiceUpgrade = (file: File) => {
  const data = new FormData();
  data.append("file", file);
  return http.request<BaseResult<UpgradeImportJob>>("post", baseUrlApi("system/upgrade/import"), {
    data,
    headers: { "Content-Type": "multipart/form-data" },
    timeout: 15 * 60 * 1000
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
