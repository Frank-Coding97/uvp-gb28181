<template>
  <div class="snow-page service-upgrade-page">
    <div class="snow-inner uvp-page-shell-flat upgrade-shell">
      <a-card :bordered="false" class="uvp-system-panel version-panel" :loading="loading && !status">
        <div class="version-main">
          <div>
            <div class="upgrade-caption">当前运行版本</div>
            <div class="version-title">
              <strong>v{{ status?.current?.version || "未知" }}</strong
              ><a-tag color="arcoblue">{{ deploymentLabel }}</a-tag>
            </div>
            <div class="version-meta">
              <span>升级通道：{{ policyBaseline?.channel === "beta" ? "预发布版" : "稳定版" }}</span
              ><span>自动升级：{{ policyBaseline?.enabled ? "已启用" : "已关闭" }}</span>
            </div>
          </div>
          <a-space wrap class="version-actions">
            <a-button :loading="loading" @click="loadStatus">刷新状态</a-button>
            <span :title="status?.releaseSourceConfigured ? '' : '尚未配置发布站点'"
              ><a-button :loading="checking" :disabled="!status?.capabilities.onlineCheck || Boolean(statusError)" @click="check"
                >检查在线更新</a-button
              ></span
            >
            <a-upload
              v-if="canManage"
              :auto-upload="false"
              :file-list="[]"
              :show-file-list="false"
              :disabled="!status?.capabilities.offlineImport || importing || Boolean(statusError)"
              accept=".tar,.gz,.upgrade.tar"
              @change="(_files: FileItem[], file: FileItem) => file.file && importOffline(file.file)"
            >
              <template #upload-button
                ><a-button
                  type="primary"
                  :loading="importing"
                  :disabled="!status?.capabilities.offlineImport || Boolean(statusError)"
                  >导入离线升级包</a-button
                ></template
              >
            </a-upload>
          </a-space>
        </div>
        <div class="version-footer">
          <div class="version-source">
            <span class="upgrade-caption">发布站点</span
            ><span>{{ status?.releaseSourceConfigured ? "已配置" : "尚未配置 · 可使用离线升级包" }}</span>
          </div>
          <a-space wrap
            ><a-link
              @click="
                showReleaseNotes(status?.current?.version || '未知', currentRelease?.release_notes, currentRelease?.published_at)
              "
              >本版本更新说明</a-link
            ><a-link :aria-expanded="environmentExpanded" @click="environmentExpanded = !environmentExpanded">{{
              environmentExpanded ? "收起环境详情" : "版本与环境详情"
            }}</a-link></a-space
          >
        </div>
        <a-descriptions v-if="environmentExpanded" class="environment-details" :column="{ xs: 1, sm: 1, md: 2 }">
          <a-descriptions-item label="前端构建身份">{{ clientIdentityText }}</a-descriptions-item
          ><a-descriptions-item label="服务端构建身份">{{ serverIdentityText }}</a-descriptions-item>
          <a-descriptions-item label="系统/架构">{{ policy.os || "未知" }} / {{ policy.arch || "未知" }}</a-descriptions-item
          ><a-descriptions-item label="数据库"
            >{{ migrationPlan?.dialect || "未知"
            }}<span v-if="migrationPlan?.can_apply"> · 待迁移 {{ migrationPlan.pending.length }} 项</span></a-descriptions-item
          >
        </a-descriptions>
      </a-card>
      <UpgradeProgress
        v-if="showImportProgress || (progressTask && progressTask.phase !== 'succeeded')"
        :data-testid="showImportProgress ? 'import-progress' : 'task-progress'"
        class="upgrade-progress-panel"
        v-bind="showImportProgress ? { ...importProgress, title: '服务升级' } : taskProgress(progressTask!)"
        :upload="showImportProgress ? uploadTransfer : undefined"
        :show-upload-bar="Boolean(showImportProgress && uploadTransfer && !importJob)"
        :reconnecting="showImportProgress ? Boolean(importJobError && !importJobFinished) : Boolean(statusError)"
      >
        <template v-if="!showImportProgress && progressTask" #action
          ><a-space wrap>
            <a-button
              v-if="
                canManage &&
                status?.capabilities.apply &&
                progressTask.phase === 'awaiting_confirmation' &&
                pendingUpgrade?.taskID !== progressTask.task_id
              "
              type="primary"
              :loading="taskAction === progressTask.task_id"
              :disabled="Boolean(taskAction || statusError) || migrationBlocked"
              @click="confirmApply(progressTask.task_id)"
              >立即升级</a-button
            ><a-link @click="showTaskDetails(progressTask.task_id)">查看详情</a-link>
          </a-space></template
        >
        <p v-if="showImportProgress && importJob && !importJobFinished" class="upgrade-caption">
          校验在后台继续执行，刷新页面后可恢复查看。
        </p>
        <p v-else-if="!showImportProgress && progressTask" class="upgrade-caption">{{ taskHint(progressTask) }}</p>
      </UpgradeProgress>
      <a-alert v-if="migrationPlan && !migrationPlan.can_apply" class="upgrade-alert" type="warning" title="升级预检未通过">
        {{ migrationBlockLabel }}<template #action><a-link @click="migrationDetailsVisible = true">查看原因</a-link></template>
      </a-alert>
      <a-alert v-if="statusError" class="upgrade-alert" type="warning"
        >服务状态暂不可用，正在重连。保留的任务信息为上次结果，不代表升级成功；{{ statusError }}</a-alert
      >
      <a-alert v-if="policyError" class="upgrade-alert" type="warning">无人值守策略暂不可用：{{ policyError }}</a-alert>
      <a-alert v-if="migrationError" class="upgrade-alert" type="warning"
        >迁移信息暂不可用，应用升级前需重新检查：{{ migrationError }}</a-alert
      >
      <a-alert v-if="errorMessage" class="upgrade-alert" type="error">{{ errorMessage }}</a-alert>
      <a-alert v-if="runtimeIdentityMismatch" class="upgrade-alert" type="warning">
        <div>
          当前浏览器前端 {{ clientIdentityText }} 与服务端 {{ serverIdentityText }} 不一致，升级后的前端入口可能尚未加载。
        </div>
        <a-space
          ><span v-if="clientRefreshState === 'refreshing'">正在刷新前端入口…</span
          ><span v-else-if="clientRefreshState === 'blocked'">自动刷新后仍未匹配，请确认发布站点已切换到目标版本。</span
          ><a-button v-if="clientRefreshState !== 'refreshing'" size="small" @click="reloadClient"
            >立即刷新前端</a-button
          ></a-space
        >
      </a-alert>
      <a-card v-if="update?.hasUpdate" class="uvp-system-panel available-update" :bordered="false">
        <template #title>待升级版本 v{{ update.manifest.version }}</template>
        <template #extra
          ><a-tag>{{ update.manifest.channel === "offline" ? "离线升级包" : "在线更新" }}</a-tag></template
        >
        <div class="update-summary">
          <span>{{ update.artifact.filename }}</span
          ><span v-if="update.manifest.published_at" class="upgrade-caption"
            >发布于 {{ formatTime(update.manifest.published_at) }}</span
          >
        </div>
        <div class="release-notes-inline">{{ update.manifest.release_notes || "该版本未提供更新说明" }}</div>
        <a-space wrap
          ><a-button
            v-if="canManage"
            type="primary"
            :loading="creating"
            :disabled="
              !status?.capabilities.apply ||
              !update.hasUpdate ||
              Boolean(status?.activeTask || taskAction || pendingUpgrade) ||
              Boolean(statusError) ||
              migrationBlocked
            "
            @click="confirmStartUpgrade"
            >开始升级</a-button
          ><a-link @click="showReleaseNotes(update.manifest.version, update.manifest.release_notes, update.manifest.published_at)"
            >查看完整更新说明</a-link
          ></a-space
        >
        <details class="package-details">
          <summary>升级包校验信息</summary>
          <div>Build ID：{{ update.manifest.build_id }}</div>
          <div>最低版本：{{ update.manifest.min_version || "未指定" }}</div>
          <div>SHA-256：{{ update.artifact.sha256 }}</div>
        </details>
      </a-card>
      <nav class="upgrade-tabs" aria-label="升级管理">
        <button
          type="button"
          :class="{ active: activeTab === 'history' }"
          :aria-pressed="activeTab === 'history'"
          @click="activeTab = 'history'"
        >
          升级记录
        </button>
        <button
          type="button"
          data-testid="settings-tab"
          :class="{ active: activeTab === 'settings' }"
          :aria-pressed="activeTab === 'settings'"
          @click="activeTab = 'settings'"
        >
          自动升级设置
          <span class="tab-state">{{ policyBaseline?.enabled ? "已启用" : "已关闭" }}{{ policyDirty ? " · 未保存" : "" }}</span>
        </button>
      </nav>
      <section v-show="activeTab === 'history'" data-testid="upgrade-history">
        <a-card class="uvp-system-panel history-panel" :bordered="false">
          <template #title
            ><div>
              升级记录
              <p class="section-subtitle">查看每次升级的结果，诊断日志可在详情中查看</p>
            </div></template
          ><template #extra
            ><span class="upgrade-caption">共 {{ sortedTasks.length }} 条记录</span></template
          >
          <div class="upgrade-tasks">
            <a-table
              class="uvp-data-table"
              :data="sortedTasks"
              :pagination="{ pageSize: 10, hideOnSinglePage: true }"
              :bordered="false"
              row-key="task_id"
              :scroll="{ x: 860 }"
            >
              <template #columns>
                <a-table-column title="目标版本" :width="130"
                  ><template #cell="{ record }"
                    ><strong>v{{ record.version || "待校验" }}</strong>
                    <div v-if="record.committed && record.build_id === status?.current?.build_id" class="upgrade-detail">
                      当前运行版本
                    </div></template
                  ></a-table-column
                >
                <a-table-column title="阶段 / 结果" :width="160"
                  ><template #cell="{ record }"
                    ><a-tag :color="phaseColor(record.phase)">{{ phaseLabel(record.phase) }}</a-tag></template
                  ></a-table-column
                >
                <a-table-column title="升级方式" :width="120"
                  ><template #cell="{ record }">{{ record.policy_version ? "自动升级" : "手动升级" }}</template></a-table-column
                >
                <a-table-column title="操作人" :width="100"
                  ><template #cell="{ record }">{{ record.actor || "未提供" }}</template></a-table-column
                >
                <a-table-column title="更新时间" :width="165"
                  ><template #cell="{ record }">{{ formatTime(record.updated_at) }}</template></a-table-column
                >
                <a-table-column title="操作" :width="360" align="center"
                  ><template #cell="{ record }"
                    ><div class="uvp-table-actions">
                      <a-link
                        class="uvp-table-action uvp-table-action--logs"
                        @click="showReleaseNotes(record.version || '待校验', record.release_notes, record.published_at)"
                        ><template #icon><icon-file /></template>更新说明</a-link
                      ><a-link class="uvp-table-action uvp-table-action--detail" @click="showTaskDetails(record.task_id)"
                        ><template #icon><icon-eye /></template>任务详情</a-link
                      >
                      <a-link
                        class="uvp-table-action uvp-table-action--stop"
                        v-if="canManage && canCancel(record.phase)"
                        :disabled="Boolean(taskAction || statusError)"
                        @click="cancel(record.task_id)"
                        ><template #icon><icon-close-circle /></template>取消</a-link
                      >
                      <a-link
                        class="uvp-table-action uvp-table-action--execute"
                        v-if="
                          canManage &&
                          status?.capabilities.apply &&
                          record.phase === 'awaiting_confirmation' &&
                          pendingUpgrade?.taskID !== record.task_id
                        "
                        :disabled="Boolean(taskAction || statusError) || migrationBlocked"
                        @click="confirmApply(record.task_id)"
                        ><template #icon><icon-arrow-up /></template>立即升级</a-link
                      >
                      <a-link
                        class="uvp-table-action uvp-table-action--scope"
                        v-if="canManage && status?.capabilities.rollback && canRollback(record)"
                        :disabled="Boolean(taskAction || statusError)"
                        @click="confirmRollback(record.task_id)"
                        ><template #icon><icon-undo /></template>回退</a-link
                      >
                    </div></template
                  ></a-table-column
                >
              </template>
            </a-table>
          </div>
        </a-card>
        <details class="upgrade-events">
          <summary>
            升级事件 <span>含手动与自动升级 · 共 {{ status?.events?.length || 0 }} 条</span>
          </summary>
          <p v-if="!status?.events?.length" class="upgrade-caption">暂无升级事件</p>
          <div v-for="(event, index) in status?.events || []" :key="index" class="event-row">
            <a-tag :color="eventColor(event)">{{ eventLabel(event) }}</a-tag
            ><span class="event-scope">{{ event.policy_version ? "自动" : "手动" }}</span
            ><span>{{ event.reason || event.build_id || event.release_id || "" }}</span
            ><time>{{ formatTime(event.at) }}</time>
          </div>
        </details>
      </section>
      <section v-show="activeTab === 'settings'" data-testid="upgrade-settings">
        <a-card class="uvp-system-panel settings-panel" :bordered="false"
          ><template #title
            ><div>
              自动升级
              <p class="section-subtitle">自动下载更新，在维护窗口内按保存的策略应用</p>
            </div></template
          >
          <template #extra
            ><a-space
              ><span>{{ policy.enabled ? "启用自动升级" : "自动升级已关闭" }}</span
              ><a-switch
                v-model="policy.enabled"
                :loading="policyLoading"
                :disabled="policyUnavailable"
                aria-label="启用自动升级" /></a-space
          ></template>
          <a-alert v-if="policyDirty" type="warning" class="policy-dirty-alert"
            >策略有未保存修改，自动刷新不会覆盖当前编辑。</a-alert
          >
          <a-form :model="policy" layout="vertical" class="upgrade-policy-form">
            <a-form-item label="通道">
              <a-select v-model="policy.channel" :disabled="policyUnavailable" :style="{ width: '120px' }">
                <a-option value="stable">稳定版</a-option>
                <a-option value="beta">预发布版</a-option>
              </a-select>
            </a-form-item>
            <a-form-item
              label="时区"
              required
              :validate-status="policyTouched && policyErrors.timezone ? 'error' : undefined"
              :help="policyTouched ? policyErrors.timezone : ''"
            >
              <a-input
                v-model="policy.timezone"
                allow-clear
                :disabled="policyUnavailable"
                placeholder="Asia/Shanghai"
                :style="{ width: '150px' }"
                @blur="policyTouched = true"
              />
            </a-form-item>
            <a-form-item
              label="维护窗口"
              required
              :validate-status="policyTouched && policyErrors.window ? 'error' : undefined"
              :help="policyTouched ? policyErrors.window : ''"
            >
              <a-space>
                <a-input
                  v-model="policy.window_start"
                  allow-clear
                  :disabled="policyUnavailable"
                  placeholder="02:00"
                  :style="{ width: '80px' }"
                  @blur="policyTouched = true"
                />
                <span>-</span>
                <a-input
                  v-model="policy.window_end"
                  allow-clear
                  :disabled="policyUnavailable"
                  placeholder="04:00"
                  :style="{ width: '80px' }"
                  @blur="policyTouched = true"
                />
              </a-space>
            </a-form-item>
            <a-form-item label="升级范围" class="policy-rules">
              <a-space>
                <a-switch v-model="policy.patch_only" :disabled="policyUnavailable" />
                <span>同主版本（补丁/小版本）</span>
              </a-space>
            </a-form-item>
            <a-form-item label="允许自动重启服务">
              <a-space
                ><a-switch v-model="policy.allow_restart" :disabled="policyUnavailable" /><span class="upgrade-detail"
                  >应用升级可能短暂中断服务</span
                ></a-space
              >
            </a-form-item>
            <a-form-item label="自动回退">
              <a-switch v-model="policy.auto_rollback" :disabled="policyUnavailable" />
            </a-form-item>
            <a-form-item
              label="保留版本"
              required
              :validate-status="policyTouched && policyErrors.retention ? 'error' : undefined"
              :help="policyTouched ? policyErrors.retention : ''"
            >
              <a-input
                v-model="policy.retain_versions"
                allow-clear
                :disabled="policyUnavailable"
                @blur="policyTouched = true"
                :style="{ width: '90px' }"
              />
            </a-form-item>
            <a-form-item label="通知记录">
              <a-space>
                <span>页面和本地日志</span>
                <span>外部通知渠道暂未接入</span>
              </a-space>
            </a-form-item>
            <a-form-item class="policy-save">
              <a-button
                v-if="canManage"
                type="primary"
                :loading="policyLoading"
                :disabled="Boolean(policyError) || Boolean(statusError) || !policyBaseline"
                @click="savePolicy"
                >保存策略</a-button
              ><span v-if="policyDirty" class="upgrade-detail">有未保存修改</span>
            </a-form-item>
          </a-form>
        </a-card>
      </section>
      <footer class="upgrade-footnote"><span>签名校验 · SHA-256 完整性校验</span><span>时间按浏览器本地时区显示</span></footer>
    </div>
    <a-drawer
      class="uvp-system-drawer"
      :visible="Boolean(selectedTask)"
      width="min(560px, 100vw)"
      :footer="false"
      @cancel="selectedTaskID = ''"
    >
      <template #title>任务详情</template>
      <template v-if="selectedTask"
        ><UpgradeProgress
          data-testid="task-detail-progress"
          v-bind="taskProgress(selectedTask)"
          :reconnecting="Boolean(statusError)"
        />
        <p class="task-hint">{{ taskHint(selectedTask) }}</p>
        <a-descriptions :column="1" bordered class="uvp-system-description uvp-system-description--compact task-descriptions"
          ><a-descriptions-item label="目标版本">{{ selectedTask.version || "待校验" }}</a-descriptions-item
          ><a-descriptions-item label="升级方式">{{
            selectedTask.policy_version ? `自动升级 · 策略 v${selectedTask.policy_version}` : "管理员手动操作"
          }}</a-descriptions-item
          ><a-descriptions-item label="操作人">{{ selectedTask.actor || "未提供" }}</a-descriptions-item
          ><a-descriptions-item label="更新时间">{{ formatTime(selectedTask.updated_at) }}</a-descriptions-item
          ><a-descriptions-item label="Build ID">{{ selectedTask.build_id || "待校验" }}</a-descriptions-item
          ><a-descriptions-item label="任务 ID">{{ selectedTask.task_id }}</a-descriptions-item
          ><a-descriptions-item v-if="selectedTask.failure_phase" label="失败阶段">{{
            phaseLabel(selectedTask.failure_phase)
          }}</a-descriptions-item></a-descriptions
        >
        <h3 class="drawer-section-title">版本更新说明</h3>
        <p class="release-notes-text">{{ selectedTask.release_notes || "该版本未提供更新说明" }}</p>
        <template v-if="selectedTask.error"
          ><h3 class="drawer-section-title">诊断日志</h3>
          <pre class="diagnostic-log">{{ selectedTask.error }}</pre>
        </template>
      </template>
    </a-drawer>
    <a-drawer
      class="uvp-system-drawer"
      :visible="releaseNotesVisible"
      width="min(560px, 100vw)"
      :footer="false"
      @cancel="releaseNotesVisible = false"
      ><template #title>版本更新说明</template>
      <h2 class="release-version">v{{ releaseNotesDetail.version }}</h2>
      <p v-if="releaseNotesDetail.publishedAt" class="upgrade-caption">发布于 {{ formatTime(releaseNotesDetail.publishedAt) }}</p>
      <p class="release-notes-text">{{ releaseNotesDetail.notes || "该版本未提供更新说明" }}</p></a-drawer
    >
    <a-drawer
      class="uvp-system-drawer"
      :visible="migrationDetailsVisible"
      width="min(560px, 100vw)"
      :footer="false"
      @cancel="migrationDetailsVisible = false"
      ><template #title>升级预检</template><a-alert type="warning">{{ migrationBlockLabel }}</a-alert>
      <h3 class="drawer-section-title">诊断信息</h3>
      <pre class="diagnostic-log">{{ migrationPlan?.block_reason || migrationError || "未提供" }}</pre>
      <template v-if="migrationPlan?.unknown?.length"
        ><h3 class="drawer-section-title">无法识别的迁移记录</h3>
        <pre class="diagnostic-log">{{ migrationPlan.unknown.join("\n") }}</pre>
      </template></a-drawer
    >
  </div>
</template>

<script setup lang="ts">
import { Message, Modal } from "@arco-design/web-vue";
import type { FileItem } from "@arco-design/web-vue/es/upload/interfaces";
import { computed, onMounted, onUnmounted, ref } from "vue";
import {
  applyServiceUpgradeTask,
  cancelServiceUpgradeTask,
  checkServiceUpgrade,
  createServiceUpgradeTask,
  getServiceUpgradeImport,
  getServiceUpgradeStatus,
  importServiceUpgrade,
  getServiceUpgradePolicy,
  getServiceUpgradeMigrationPlan,
  rollbackServiceUpgradeTask,
  type UpgradeEvent,
  type UpgradeImportArtifact,
  type UpgradeMigrationPlan,
  type UpgradeImportJob,
  type UpgradePolicy,
  type UpgradeStatus,
  type UpgradeTask,
  updateServiceUpgradePolicy
} from "@/api/service-upgrade";
import { hasPermission } from "@/hooks/usePermission";
import { APP_BUILD_ID, APP_VERSION, checkRuntimeBuildIdentity } from "@/config/version";
import UpgradeProgress, { type ProgressDisplay, type UploadTransfer } from "./UpgradeProgress.vue";

const canManage = computed(() => hasPermission("system:upgrade:manage"));

const loading = ref(false);
const statusError = ref("");
const policyError = ref("");
const migrationError = ref("");
const creating = ref(false);
const taskAction = ref("");
const sortedTasks = computed(() =>
  [...(status.value?.tasks || [])].sort((a, b) => (b.updated_at || "").localeCompare(a.updated_at || ""))
);
const lastActiveTaskID = ref("");
const progressTask = computed(
  () =>
    status.value?.activeTask ||
    (lastActiveTaskID.value ? status.value?.tasks.find(task => task.task_id === lastActiveTaskID.value) : sortedTasks.value[0])
);
const activeTab = ref("history");
const environmentExpanded = ref(false);
const migrationDetailsVisible = ref(false);
const selectedTaskID = ref("");
const selectedTask = computed(() => status.value?.tasks.find(task => task.task_id === selectedTaskID.value));
const showTaskDetails = (id: string) => {
  selectedTaskID.value = id;
};
const releaseNotesVisible = ref(false);
const releaseNotesDetail = ref({ version: "", notes: "", publishedAt: "" });
const showReleaseNotes = (version: string, notes = "", publishedAt = "") => {
  selectedTaskID.value = "";
  releaseNotesDetail.value = { version, notes, publishedAt };
  releaseNotesVisible.value = true;
};
const currentRelease = computed(() =>
  sortedTasks.value.find(
    task =>
      task.phase === "succeeded" && task.committed && Boolean(task.build_id) && task.build_id === status.value?.current?.build_id
  )
);
const deploymentLabel = computed(
  () =>
    (
      ({ standalone: "绿色包部署", systemd: "系统服务部署", compose: "Docker Compose 部署", docker: "Docker 部署" }) as Record<
        string,
        string
      >
    )[status.value?.deployment || ""] ||
    status.value?.deployment ||
    "部署方式未知"
);
const formatTime = (value?: string) => {
  if (!value) return "未提供时间";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? "时间格式无效"
    : new Intl.DateTimeFormat("zh-CN", {
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        hourCycle: "h23"
      }).format(date);
};
// 事件流水同时记录手动升级与自动升级（同一个任务管道），文案必须两种都能读通。
const eventLabels: Record<string, string> = {
  blocked: "已阻断",
  succeeded: "升级成功",
  failed: "升级失败",
  rolled_back: "已恢复旧版本",
  downloaded: "下载完成",
  import_succeeded: "导入校验通过",
  import_failed: "导入失败",
  recovery_required: "需要恢复处理",
  cleanup_failed: "清理失败",
  auto_approved_by_policy: "自动批准",
  auto_started: "自动开始升级"
};
const eventLabel = (event: UpgradeEvent) => {
  // 离线导入与自动下载共用同一条准备管道，都写 downloaded；
  // 手动升级是本地选包，语义应为「包已就绪」而不是「下载完成」。
  if (event.event === "downloaded" && !event.policy_version) return "离线包就绪";
  return eventLabels[event.event] || `未知事件（${event.event}）`;
};
const eventColor = (event: UpgradeEvent) => {
  if (event.event === "succeeded") return "green";
  if (["failed", "import_failed", "cleanup_failed", "recovery_required"].includes(event.event)) return "red";
  return event.event === "blocked" ? "orange" : "blue";
};
const migrationBlocked = computed(() => !migrationPlan.value?.can_apply || Boolean(migrationError.value));
const migrationBlockLabel = computed(() =>
  migrationPlan.value?.unknown?.length
    ? "数据库中存在无法识别的迁移记录，请先排查后再应用升级。"
    : "数据库迁移预检未通过，请查看原因后再应用升级。"
);
const policyUnavailable = computed(
  () => !canManage.value || Boolean(policyError.value || statusError.value) || !policyBaseline.value || policyLoading.value
);
const phaseLabels: Record<string, string> = {
  fetching: "下载中",
  verifying: "校验中",
  preparing: "准备与预检",
  awaiting_confirmation: "等待应用授权",
  stopping: "停止服务",
  backing_up: "备份数据",
  migrating: "执行迁移",
  activating: "切换版本",
  validating: "验证启动",
  committing: "提交升级结果",
  succeeded: "升级成功",
  failed: "任务失败或已取消",
  rolled_back: "已恢复旧版本",
  recovery_required: "需要恢复处理"
};
const phaseLabel = (phase: string) => phaseLabels[phase] || `未知阶段（${phase}）`;
const taskSteps = ["准备与授权", "备份数据", "执行迁移", "切换与验证", "完成"];
const taskStepIndex: Record<string, number> = {
  fetching: 0,
  verifying: 0,
  preparing: 0,
  awaiting_confirmation: 0,
  stopping: 0,
  backing_up: 1,
  migrating: 2,
  activating: 3,
  validating: 3,
  committing: 3,
  succeeded: 4
};
const taskProgress = (task: UpgradeTask): ProgressDisplay => {
  const interrupted = ["failed", "rolled_back", "recovery_required"].includes(task.phase);
  return {
    title: `服务升级${task.version ? ` · v${task.version}` : ""}`,
    detail: `${phaseLabel(task.phase)}${interrupted && task.failure_phase ? ` · 失败阶段：${phaseLabel(task.failure_phase)}` : ""}`,
    steps: taskSteps,
    current: taskStepIndex[interrupted ? task.failure_phase || "" : task.phase] ?? -1,
    state:
      task.phase === "succeeded"
        ? "success"
        : task.phase === "rolled_back"
          ? "warning"
          : interrupted
            ? "error"
            : task.phase === "awaiting_confirmation"
              ? "waiting"
              : task.phase in taskStepIndex
                ? "active"
                : "unknown",
    error: task.error
  };
};
const phaseColor = (phase: string) =>
  phase === "succeeded"
    ? "green"
    : ["failed", "recovery_required"].includes(phase)
      ? "red"
      : phase === "rolled_back"
        ? "orange"
        : "blue";
const taskHint = (task: UpgradeTask) => {
  if (task.phase === "succeeded") return "目标版本已通过助手启动验证。";
  if (task.phase === "rolled_back") return "升级未成功，助手已验证恢复旧版本。";
  if (task.phase === "recovery_required") return "尚未证明恢复成功，请查看本机助手诊断并处理；不可视为已回退。";
  if (task.phase === "failed") return "请查看失败原因；如已进入停服阶段，以助手恢复结果为准。";
  if (task.phase === "awaiting_confirmation")
    return Boolean(task.task_id) && pendingUpgrade.value?.taskID === task.task_id
      ? "已确认升级，准备完成后自动开始应用。"
      : task.policy_version
        ? "制品已准备，由已保存策略在维护窗口内再次预检并授权应用。"
        : canManage.value && status.value?.capabilities.apply
          ? "升级包已就绪，点击「立即升级」并确认后开始；升级期间服务可能短暂中断。"
          : "升级包已就绪，等待管理员确认应用。";
  if (["fetching", "verifying", "preparing"].includes(task.phase)) return "正在获取或检查制品，当前服务继续运行。";
  if (!(task.phase in taskStepIndex)) return "当前阶段无法识别，请查看任务详情并刷新状态。";
  return "助手正在执行升级，业务可能中断；刷新或关闭页面不会取消任务。";
};
const checking = ref(false);
const status = ref<UpgradeStatus>();
const update = ref<Awaited<ReturnType<typeof checkServiceUpgrade>>["data"]>();
const errorMessage = ref("");
const importing = ref(false);
const uploadTransfer = ref<UploadTransfer>();
let importGeneration = 0;
const importJob = ref<UpgradeImportJob>();
const importJobError = ref("");
const importJobID = computed(() => importJob.value?.job_id || importJob.value?.task_id || importJob.value?.upload_id || "");
const importPhase = computed(() => (importJob.value?.phase || importJob.value?.status || "queued").toLowerCase());
const importJobFinished = computed(() =>
  [
    "succeeded",
    "success",
    "verified",
    "ready",
    "completed",
    "complete",
    "done",
    "failed",
    "rejected",
    "cancelled",
    "canceled",
    "abandoned"
  ].includes(importPhase.value)
);
// Once a task owns this artifact, continue the same panel with its execution
// state. A new import must not be hidden by an unrelated historical task.
const showImportProgress = computed(() => {
  if (!uploadTransfer.value && !importJob.value) return false;
  if (!importJob.value || !importJobFinished.value || importJobFailed.value || importJobError.value) return true;
  const artifact = importedArtifactFromJob(importJob.value);
  const task = progressTask.value;
  return !(
    task &&
    ((artifact.sha256 && artifact.sha256 === task.target_digest) || (artifact.build_id && artifact.build_id === task.build_id))
  );
});
const importJobFailed = computed(() => ["failed", "rejected", "cancelled", "canceled", "abandoned"].includes(importPhase.value));
const importPhaseLabel = (phase: string) =>
  (
    ({
      queued: "等待校验",
      pending: "等待校验",
      uploading: "上传中",
      received: "等待校验",
      verifying: "正在校验签名与完整性",
      validating: "正在校验签名与完整性",
      succeeded: "已校验并暂存",
      success: "已校验并暂存",
      verified: "已校验并暂存",
      ready: "已校验并暂存",
      completed: "已校验并暂存",
      complete: "已校验并暂存",
      done: "已校验并暂存",
      failed: "校验失败",
      rejected: "校验失败",
      cancelled: "已取消",
      canceled: "已取消",
      abandoned: "上传未完成"
    }) as Record<string, string>
  )[phase.toLowerCase()] || `处理中（${phase}）`;
const importProgress = computed<ProgressDisplay>(() => {
  if (!importJob.value) {
    const failed = uploadTransfer.value?.state === "failed";
    const waiting = uploadTransfer.value?.state === "waiting";
    return {
      title: "导入离线升级包",
      steps: ["上传升级包", "后台校验", "升级包就绪"],
      current: 0,
      detail: failed ? "上传未完成" : waiting ? "文件已传输，等待服务端受理" : "正在上传升级包",
      state: failed ? "error" : waiting ? "waiting" : "active",
      error: uploadTransfer.value?.error
    };
  }
  const failed = importJobFailed.value || (importJobFinished.value && Boolean(importJobError.value));
  const ready = importJobFinished.value && !failed;
  const phase = importPhase.value;
  const known = ["queued", "pending", "received", "verifying", "validating"].includes(phase) || importJobFinished.value;
  return {
    title: "导入离线升级包",
    steps: ["上传升级包", "后台校验", "升级包就绪"],
    current: ready ? 2 : phase === "abandoned" ? 0 : known ? 1 : -1,
    detail: ready
      ? `升级包已就绪${importedArtifactFromJob(importJob.value).version ? ` · v${importedArtifactFromJob(importJob.value).version}` : ""}`
      : failed && !importJobFailed.value
        ? "升级包信息不完整，无法就绪"
        : importPhaseLabel(phase),
    state: failed
      ? "error"
      : ready
        ? "success"
        : !known
          ? "unknown"
          : ["queued", "pending", "received"].includes(phase)
            ? "waiting"
            : "active",
    error: importJob.value.error || importJob.value.message || importJobError.value
  };
});
const importJobStorageKey = "uvp.service-upgrade.import-job";
const readStoredImportJobID = () => {
  if (typeof window === "undefined") return "";
  try {
    return window.localStorage.getItem(importJobStorageKey) || "";
  } catch {
    return "";
  }
};
const storeImportJobID = (id: string) => {
  if (typeof window === "undefined") return;
  try {
    if (id) window.localStorage.setItem(importJobStorageKey, id);
    else window.localStorage.removeItem(importJobStorageKey);
  } catch {
    // Browser storage can be unavailable in privacy-restricted contexts.
  }
};
const policyLoading = ref(false);
const policy = ref<Omit<UpgradePolicy, "retain_versions"> & { retain_versions: number | string }>({
  enabled: false,
  policy_version: 1,
  timezone: "Asia/Shanghai",
  window_start: "02:00",
  window_end: "04:00",
  channel: "stable",
  deployment: "",
  os: "",
  arch: "",
  patch_only: true,
  allow_restart: false,
  auto_rollback: false,
  retain_versions: 2
});
const migrationPlan = ref<UpgradeMigrationPlan>();
const policyTouched = ref(false);
const policyErrors = computed(() => {
  let timezone = "";
  try {
    if (!policy.value.timezone) throw new Error("missing timezone");
    new Intl.DateTimeFormat("zh-CN", { timeZone: policy.value.timezone });
  } catch {
    timezone = "请输入有效的 IANA 时区，例如 Asia/Shanghai";
  }
  const hhmm = /^(?:[01][0-9]|2[0-3]):[0-5][0-9]$/;
  const windowError =
    hhmm.test(policy.value.window_start) && hhmm.test(policy.value.window_end) ? "" : "维护窗口必须使用 HH:MM（00:00–23:59）";
  const retention = String(policy.value.retain_versions);
  return {
    timezone,
    window: windowError,
    retention: /^\d+$/.test(retention) && Number(retention) <= 50 ? "" : "保留版本数必须是 0–50 的整数"
  };
});
const policyBaseline = ref<UpgradePolicy>();
const clientRefreshState = ref<"idle" | "refreshing" | "blocked">("idle");
const clientRefreshTarget = ref("");
const clientRefreshAttempt = ref("");
const clientRefreshTargetStorageKey = "uvp.service-upgrade.refresh-target";
const clientRefreshAttemptStorageKey = "uvp.service-upgrade.refresh-attempt";

const readSessionValue = (key: string) => {
  if (typeof window === "undefined") return "";
  try {
    return window.sessionStorage.getItem(key) || "";
  } catch {
    return "";
  }
};

const writeSessionValue = (key: string, value: string) => {
  if (typeof window === "undefined") return;
  try {
    if (value) window.sessionStorage.setItem(key, value);
    else window.sessionStorage.removeItem(key);
  } catch {
    // A privacy-restricted browser may disable sessionStorage. The page still
    // offers an explicit refresh button in that case.
  }
};

type PendingUpgrade = { taskID: string; digest: string; buildID: string };
const pendingUpgradeKey = "uvp.service-upgrade.auto-apply";
const readPendingUpgrade = (): PendingUpgrade | undefined => {
  try {
    const value = JSON.parse(readSessionValue(pendingUpgradeKey));
    if (
      typeof value?.taskID === "string" &&
      value.taskID &&
      /^[a-f0-9]{64}$/i.test(value.digest) &&
      typeof value.buildID === "string"
    )
      return value;
  } catch {
    // A missing or malformed session value never authorizes an application.
  }
  return undefined;
};
const pendingUpgrade = ref<PendingUpgrade | undefined>(readPendingUpgrade());
const clearPendingUpgrade = () => {
  pendingUpgrade.value = undefined;
  writeSessionValue(pendingUpgradeKey, "");
};

clientRefreshTarget.value = readSessionValue(clientRefreshTargetStorageKey);
clientRefreshAttempt.value = readSessionValue(clientRefreshAttemptStorageKey);

const clientIdentityText = computed(() => `${APP_VERSION} / ${APP_BUILD_ID || "未注入 build_id"}`);
const runtimeIdentityCheck = computed(() => checkRuntimeBuildIdentity(status.value?.current));
const runtimeIdentityMismatch = computed(() => Boolean(status.value) && !runtimeIdentityCheck.value.matches);
const serverIdentityText = computed(() => {
  const version = status.value?.current?.version || "未知";
  const buildID = status.value?.current?.build_id || "未提供 build_id";
  return `${version} / ${buildID}`;
});

const reloadClient = () => {
  if (typeof window === "undefined") return;
  const url = new URL(window.location.href);
  url.searchParams.set("uvp_refresh", `${Date.now()}`);
  clientRefreshState.value = "refreshing";
  window.location.replace(url.toString());
};

const rememberClientRefreshTarget = (buildID: string) => {
  const normalized = buildID.trim();
  if (!normalized) return;
  clientRefreshTarget.value = normalized;
  clientRefreshAttempt.value = "";
  writeSessionValue(clientRefreshTargetStorageKey, normalized);
  writeSessionValue(clientRefreshAttemptStorageKey, "");
};

const clearClientRefreshTarget = () => {
  clientRefreshTarget.value = "";
  clientRefreshAttempt.value = "";
  clientRefreshState.value = "idle";
  writeSessionValue(clientRefreshTargetStorageKey, "");
  writeSessionValue(clientRefreshAttemptStorageKey, "");
};

const latestCommittedBuildID = (currentBuildID: string) =>
  (status.value?.tasks || [])
    .filter(task => task.phase === "succeeded" && task.committed && task.build_id === currentBuildID)
    .sort((left, right) => (right.updated_at || "").localeCompare(left.updated_at || ""))[0]?.build_id || "";

const productVersionPattern = /^(\d+)\.(\d+)\.(\d+)(?:-rc\.(\d+))?$/;
const compareProductVersions = (left: string, right: string) => {
  const leftMatch = productVersionPattern.exec(left.trim());
  const rightMatch = productVersionPattern.exec(right.trim());
  if (!leftMatch || !rightMatch) return Number.NaN;
  for (let index = 1; index <= 3; index += 1) {
    const difference = Number(leftMatch[index]) - Number(rightMatch[index]);
    if (difference !== 0) return difference;
  }
  const leftRC = leftMatch[4] === undefined ? Number.POSITIVE_INFINITY : Number(leftMatch[4]);
  const rightRC = rightMatch[4] === undefined ? Number.POSITIVE_INFINITY : Number(rightMatch[4]);
  return leftRC === rightRC ? 0 : leftRC > rightRC ? 1 : -1;
};

const isNewerProductVersion = (target: string, current: string) => {
  if (!target.trim() || !current.trim()) return false;
  const comparison = compareProductVersions(target, current);
  return Number.isFinite(comparison) && comparison > 0;
};

// Reconcile against a fresh runtime status, including restored browser imports.
// Keep release notes in task history; browser storage only tracks pending work.
const reconcileUpdate = () => {
  if (statusError.value || !status.value?.current?.version) return;
  const currentVersion = status.value.current.version;
  if (update.value) {
    const comparison = compareProductVersions(update.value.manifest.version, currentVersion);
    if (!Number.isNaN(comparison)) {
      update.value.currentVersion = currentVersion;
      update.value.hasUpdate = comparison > 0;
    }
  }
  if (
    importJob.value &&
    importJobFinished.value &&
    !importJobFailed.value &&
    compareProductVersions(importedArtifactFromJob(importJob.value).version || "", currentVersion) <= 0
  ) {
    importJob.value = undefined;
    uploadTransfer.value = undefined;
    importJobError.value = "";
    storeImportJobID("");
  }
};

const importedArtifactFromJob = (job: UpgradeImportJob): Partial<UpgradeImportArtifact> =>
  job.staged || job.artifact || job.result || job;

const applyImportedArtifact = (job: UpgradeImportJob) => {
  const artifact = importedArtifactFromJob(job);
  if (!artifact.version || !artifact.sha256 || !artifact.filename || !artifact.build_id) {
    update.value = undefined;
    importJobError.value = "校验已完成，但服务端没有返回完整的升级包信息";
    return;
  }
  update.value = {
    manifest: {
      schema_version: 1,
      channel: "offline",
      sequence: 0,
      release_id: artifact.release_id || "",
      version: artifact.version,
      build_id: artifact.build_id,
      release_notes: artifact.release_notes || "",
      published_at: artifact.published_at,
      min_updater_protocol: 1,
      artifacts: []
    },
    artifact: {
      deployment: artifact.deployment || "",
      os: artifact.os || "",
      arch: artifact.arch || "",
      filename: artifact.filename,
      url: "",
      size: artifact.size || 0,
      sha256: artifact.sha256
    },
    currentVersion: status.value?.current?.version || "",
    hasUpdate: isNewerProductVersion(artifact.version, status.value?.current?.version || "")
  };
  importJobError.value = "";
  reconcileUpdate();
};

const refreshImportJob = async (id = importJobID.value) => {
  if (!id || pollingImport) return;
  pollingImport = true;
  const generation = importGeneration;
  try {
    const response = await getServiceUpgradeImport(id);
    if (generation !== importGeneration) return;
    const job = ((response.data as any)?.job || response.data) as UpgradeImportJob;
    if (!job.phase && !job.status && job.version && job.sha256 && job.filename && job.build_id) job.phase = "verified";
    importJob.value = job;
    importJobError.value = "";
    if (importJobFailed.value) {
      importing.value = false;
      importJobError.value = job.error || job.message || "升级包校验失败，服务端未提供详细原因";
      return;
    }
    if (importJobFinished.value) {
      importing.value = false;
      applyImportedArtifact(job);
      if (!importJobError.value) Message.success("离线升级包已校验并暂存");
      return;
    }
    importing.value = true;
  } catch (error: any) {
    if (generation !== importGeneration) return;
    // The server job survives page refresh; retain its ID and retry transient failures.
    importJobError.value = error?.message || "读取离线升级包校验状态失败，正在重试";
  } finally {
    pollingImport = false;
  }
};

const restoreImportJob = async () => {
  const id = readStoredImportJobID();
  if (!id) return;
  importJob.value = { upload_id: id, job_id: id, phase: "queued" };
  importing.value = true;
  await refreshImportJob(id);
};

const maybeRefreshClient = () => {
  const currentBuildID = status.value?.current?.build_id?.trim() || "";
  if (!currentBuildID || !runtimeIdentityMismatch.value) return;

  // An explicit apply stores the target before the service goes down. A
  // completed task also covers a background/unattended upgrade while this
  // browser tab was open.
  const targetBuildID = clientRefreshTarget.value || latestCommittedBuildID(currentBuildID);
  if (!targetBuildID || targetBuildID !== currentBuildID) return;
  if (clientRefreshAttempt.value === targetBuildID) {
    clientRefreshState.value = "blocked";
    return;
  }
  clientRefreshAttempt.value = targetBuildID;
  writeSessionValue(clientRefreshAttemptStorageKey, targetBuildID);
  reloadClient();
};
const editablePolicyKeys = [
  "enabled",
  "timezone",
  "window_start",
  "window_end",
  "channel",
  "deployment",
  "os",
  "arch",
  "patch_only",
  "allow_restart",
  "auto_rollback",
  "retain_versions"
] as const satisfies readonly (keyof UpgradePolicy)[];
const policyDirty = computed(() => {
  if (!policyBaseline.value) return false;
  return editablePolicyKeys.some(key => policy.value[key] !== policyBaseline.value?.[key]);
});
let refreshTimer: ReturnType<typeof setInterval> | undefined;
let importRefreshTimer: ReturnType<typeof setInterval> | undefined;
let pollingImport = false;

const loadStatus = async () => {
  if (loading.value) return;
  loading.value = true;
  try {
    const [statusResult, policyResult, migrationResult] = await Promise.allSettled([
      getServiceUpgradeStatus(),
      getServiceUpgradePolicy(),
      getServiceUpgradeMigrationPlan()
    ]);
    if (statusResult.status === "fulfilled") {
      status.value = statusResult.value.data;
      if (status.value.activeTask) lastActiveTaskID.value = status.value.activeTask.task_id;
      statusError.value = "";
      reconcileUpdate();
      if (!runtimeIdentityMismatch.value) clearClientRefreshTarget();
      else maybeRefreshClient();
    } else {
      statusError.value = statusResult.reason?.message || "读取升级状态失败，正在等待服务恢复";
    }
    if (policyResult.status === "fulfilled") {
      if (!policyBaseline.value || !policyDirty.value) {
        policy.value = { ...policyResult.value.data };
        policyBaseline.value = { ...policyResult.value.data };
      }
      policyError.value = "";
    } else {
      policyError.value = policyResult.reason?.message || "读取无人值守策略失败";
    }
    if (migrationResult.status === "fulfilled") {
      migrationPlan.value = migrationResult.value.data;
      migrationError.value = "";
    } else {
      migrationPlan.value = undefined;
      migrationError.value = migrationResult.reason?.message || "读取迁移计划失败";
    }
  } finally {
    loading.value = false;
  }
  await continueConfirmedUpgrade();
};

const savePolicy = async () => {
  if (!canManage.value || policyLoading.value || policyError.value || statusError.value) return;
  policyTouched.value = true;
  const errors = Object.values(policyErrors.value).filter(Boolean);
  if (errors.length) {
    errorMessage.value = errors.join("；");
    return;
  }
  errorMessage.value = "";
  policyLoading.value = true;
  try {
    const saved = (
      await updateServiceUpgradePolicy({
        ...policy.value,
        retain_versions: Number(policy.value.retain_versions),
        // The first release only records upgrade events in the page and local log.
        // Do not preserve a legacy or manually injected external target.
        notification_target: ""
      })
    ).data;
    policy.value = { ...saved };
    policyBaseline.value = { ...saved };
    Message.success("无人值守策略已保存");
  } catch (error: any) {
    errorMessage.value = error?.message || "保存无人值守策略失败";
  } finally {
    policyLoading.value = false;
  }
};

const check = async () => {
  if (checking.value || !status.value?.capabilities.onlineCheck || statusError.value) return;
  errorMessage.value = "";
  checking.value = true;
  try {
    update.value = (await checkServiceUpgrade()).data;
    reconcileUpdate();
    Message.success(update.value.hasUpdate ? "发现可用更新" : "当前已是最新版本");
  } catch (error: any) {
    errorMessage.value = error?.message || "检查更新失败";
  } finally {
    checking.value = false;
  }
};

const importOffline = async (file: File) => {
  if (!canManage.value || importing.value || !status.value?.capabilities.offlineImport || statusError.value) return;
  errorMessage.value = "";
  importJobError.value = "";
  importJob.value = undefined;
  update.value = undefined;
  storeImportJobID("");
  const generation = ++importGeneration;
  uploadTransfer.value = { filename: file.name, loaded: 0, total: file.size || undefined, state: "uploading" };
  importing.value = true;
  try {
    const result = await importServiceUpgrade(file, event => {
      if (generation !== importGeneration || !uploadTransfer.value) return;
      const total =
        event.lengthComputable && event.total && Number.isFinite(event.total) && event.total > 0 ? event.total : undefined;
      uploadTransfer.value.loaded = Number.isFinite(event.loaded) ? Math.max(0, event.loaded) : 0;
      uploadTransfer.value.total = total;
      uploadTransfer.value.state = total && event.loaded >= total ? "waiting" : "uploading";
    });
    if (generation !== importGeneration) return;
    const job = result.data;
    const id = job.job_id || job.task_id || job.upload_id;
    if (!id && job.version && job.sha256 && job.filename && job.build_id) {
      // Older servers returned the verified artifact directly from the upload request.
      importJob.value = { ...job, phase: "verified" };
      importing.value = false;
      applyImportedArtifact(importJob.value);
      Message.success("离线升级包已校验并暂存");
      return;
    }
    if (!id) throw new Error("服务端未返回离线导入任务 ID");
    importJob.value = { ...job, upload_id: job.upload_id || id, job_id: job.job_id || job.task_id || id };
    storeImportJobID(job.job_id || job.task_id || job.upload_id || id);
    await refreshImportJob(job.job_id || job.task_id || job.upload_id || id);
  } catch (error: any) {
    if (generation !== importGeneration) return;
    importing.value = false;
    errorMessage.value = error?.message || "导入离线升级包失败";
    if (uploadTransfer.value) {
      uploadTransfer.value.state = "failed";
      uploadTransfer.value.error = errorMessage.value;
    }
  }
};

function createIdempotencyKey() {
  if (typeof globalThis.crypto?.randomUUID === "function") return globalThis.crypto.randomUUID();
  return `service-upgrade-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

const submitTask = async (target = update.value) => {
  if (
    !canManage.value ||
    creating.value ||
    !status.value?.capabilities.apply ||
    !target?.hasUpdate ||
    target.artifact.sha256 !== update.value?.artifact.sha256 ||
    target.manifest.build_id !== update.value?.manifest.build_id ||
    status.value?.activeTask ||
    taskAction.value ||
    pendingUpgrade.value ||
    statusError.value ||
    migrationBlocked.value
  )
    return;
  creating.value = true;
  errorMessage.value = "";
  try {
    const response = await createServiceUpgradeTask(createIdempotencyKey(), target.artifact.sha256);
    if (!response.data?.task_id) throw new Error("服务端未返回升级任务 ID，请刷新状态查看任务是否已创建。");
    pendingUpgrade.value = {
      taskID: response.data.task_id,
      digest: target.artifact.sha256,
      buildID: target.manifest.build_id || ""
    };
    writeSessionValue(pendingUpgradeKey, JSON.stringify(pendingUpgrade.value));
    Message.success("升级已受理，准备完成后将自动应用");
    await loadStatus();
  } catch (error: any) {
    errorMessage.value = error?.message || "创建升级任务失败";
  } finally {
    creating.value = false;
  }
};

const confirmStartUpgrade = () => {
  if (
    !canManage.value ||
    !status.value?.capabilities.apply ||
    creating.value ||
    taskAction.value ||
    pendingUpgrade.value ||
    !update.value?.hasUpdate ||
    status.value.activeTask ||
    statusError.value ||
    migrationBlocked.value
  )
    return;
  const target = update.value;
  Modal.confirm({
    title: "确认开始服务升级？",
    modalClass: "uvp-system-dialog",
    content: `将升级至 ${target.manifest.version}（${target.manifest.build_id}）。准备完成后自动开始应用，SIP 注册、点播、回放及录像可能短暂中断。应用前会再次检查数据库迁移和备份；程序回切不能撤销不兼容的数据迁移。`,
    okText: "开始升级",
    cancelText: "暂不升级",
    onOk: () => submitTask(target)
  });
};

const canCancel = (phase: string) => ["fetching", "verifying", "preparing", "awaiting_confirmation"].includes(phase);
const canRollback = (task: { phase: string; failure_phase?: string; committed: boolean }) =>
  !task.committed &&
  ["failed", "recovery_required"].includes(task.phase) &&
  ["stopping", "backing_up", "migrating", "activating", "validating"].includes(task.failure_phase || "");
const taskByID = (id: string) => status.value?.tasks.find(task => task.task_id === id);
const cancel = async (id: string) => {
  if (!canManage.value || taskAction.value || statusError.value || !canCancel(taskByID(id)?.phase || "")) return;
  taskAction.value = id;
  errorMessage.value = "";
  try {
    if (pendingUpgrade.value?.taskID === id) clearPendingUpgrade();
    await cancelServiceUpgradeTask(id);
    Message.success("升级任务已取消");
    await loadStatus();
  } catch (error: any) {
    errorMessage.value = error?.message || "取消任务失败";
  } finally {
    taskAction.value = "";
  }
};

const apply = async (id: string) => {
  const task = taskByID(id);
  if (
    !canManage.value ||
    !status.value?.capabilities.apply ||
    statusError.value ||
    taskAction.value ||
    migrationBlocked.value ||
    task?.phase !== "awaiting_confirmation"
  )
    return;
  taskAction.value = id;
  if (pendingUpgrade.value?.taskID === id) clearPendingUpgrade();
  errorMessage.value = "";
  try {
    // Persist before the RPC: the old backend may stop before its response
    // reaches this tab. The target is session-scoped and only triggers a
    // reload after status reports that exact build as active.
    if (task.build_id) rememberClientRefreshTarget(task.build_id);
    await applyServiceUpgradeTask(id);
    Message.success("已提交给本机升级助手，服务将进入维护流程");
    await loadStatus();
  } catch (error: any) {
    errorMessage.value = error?.message || "提交应用失败";
  } finally {
    taskAction.value = "";
  }
};

const continueConfirmedUpgrade = async () => {
  const pending = pendingUpgrade.value;
  if (!pending || statusError.value || taskAction.value) return;
  const task = taskByID(pending.taskID);
  if (!task) return;
  if (!canManage.value || !status.value?.capabilities.apply || task.policy_version || !canCancel(task.phase)) {
    clearPendingUpgrade();
    return;
  }
  if (
    task.target_digest !== pending.digest ||
    (pending.buildID && (task.build_id || task.phase === "awaiting_confirmation") && task.build_id !== pending.buildID)
  ) {
    clearPendingUpgrade();
    errorMessage.value = "升级目标与已确认的升级包不一致，请重新确认。";
    return;
  }
  if (task.phase !== "awaiting_confirmation") return;
  if (migrationBlocked.value) {
    clearPendingUpgrade();
    return;
  }
  // Consume before submission: a lost response must not trigger repeated POSTs.
  clearPendingUpgrade();
  await apply(task.task_id);
};

const confirmApply = (id: string) => {
  const target = taskByID(id);
  if (
    !canManage.value ||
    !status.value?.capabilities.apply ||
    statusError.value ||
    taskAction.value ||
    migrationBlocked.value ||
    target?.phase !== "awaiting_confirmation"
  )
    return;
  const version = target?.version || "已验证目标版本";
  const buildID = target?.build_id || "由助手按任务清单复核";
  Modal.confirm({
    title: "确认应用服务升级？",
    modalClass: "uvp-system-dialog",
    content: `将应用 ${version}（${buildID}）。执行期间 SIP 注册、点播、回放及录像可能短暂中断。应用前会再次检查数据库迁移和备份；程序回切不能撤销不兼容的数据迁移，条件不满足时任务会保持维护状态并要求人工恢复。`,
    okText: "确认应用",
    cancelText: "暂不升级",
    onOk: () => apply(id)
  });
};

const rollback = async (id: string) => {
  const task = taskByID(id);
  if (
    !canManage.value ||
    !status.value?.capabilities.rollback ||
    statusError.value ||
    taskAction.value ||
    !task ||
    !canRollback(task)
  )
    return;
  taskAction.value = id;
  errorMessage.value = "";
  try {
    await rollbackServiceUpgradeTask(id);
    Message.success("已提交回退请求");
    await loadStatus();
  } catch (error: any) {
    errorMessage.value = error?.message || "提交回退失败";
  } finally {
    taskAction.value = "";
  }
};

const confirmRollback = (id: string) => {
  const task = taskByID(id);
  if (
    !canManage.value ||
    !status.value?.capabilities.rollback ||
    statusError.value ||
    taskAction.value ||
    !task ||
    !canRollback(task)
  )
    return;
  Modal.confirm({
    modalClass: "uvp-system-dialog",
    title: "确认回退服务版本？",
    content:
      "回退会再次检查当前运行版本、数据库迁移和兼容条件。回切程序不能自动撤销数据库迁移；无法证明旧版本兼容时，系统会保持维护状态并要求人工恢复。",
    okText: "确认回退",
    cancelText: "取消",
    onOk: () => rollback(id)
  });
};

onMounted(async () => {
  await loadStatus();
  refreshTimer = setInterval(loadStatus, 5000);
  await restoreImportJob();
  importRefreshTimer = setInterval(() => {
    if (importJob.value && !importJobFinished.value) void refreshImportJob();
  }, 2500);
});
onUnmounted(() => {
  importGeneration += 1;
  if (refreshTimer) clearInterval(refreshTimer);
  if (importRefreshTimer) clearInterval(importRefreshTimer);
});
</script>

<style scoped lang="scss">
.service-upgrade-page {
  min-width: 0;
}
.upgrade-shell {
  display: block;
  min-width: 0;
  max-width: none;
  margin: 0;
}
.upgrade-progress-panel {
  margin-bottom: 16px;
  > .upgrade-caption {
    margin-top: 12px;
  }
}
.version-panel {
  margin-bottom: 16px;
  :deep(.arco-card-body) {
    padding: 26px 28px 20px;
  }
}
.version-main {
  display: flex;
  gap: 24px;
  align-items: center;
  justify-content: space-between;
}
.version-title {
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
  align-items: center;
  margin: 4px 0 10px;
  strong {
    font-size: 36px;
    font-weight: 600;
    line-height: 1.3;
    color: var(--color-text-1);
  }
}
.version-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 20px;
  font-size: 13px;
  color: var(--color-text-3);
}
.version-actions {
  flex-shrink: 0;
}
.version-footer {
  display: flex;
  gap: 15px;
  align-items: center;
  justify-content: space-between;
  padding-top: 18px;
  margin-top: 22px;
  font-size: 13px;
  border-top: 1px solid var(--color-border-2);
}
.version-source {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}
.upgrade-caption {
  font-size: 13px;
  color: var(--color-text-3);
}
.environment-details {
  padding-top: 20px;
  :deep(.arco-descriptions-item-value) {
    overflow-wrap: anywhere;
  }
}
.upgrade-alert {
  margin: 12px 0;
  :deep(.arco-alert-content) {
    min-width: 0;
    overflow-wrap: anywhere;
  }
}
.upgrade-tabs {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  padding: 4px;
  margin: 20px 0 16px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
  button {
    display: flex;
    gap: 8px;
    align-items: center;
    justify-content: center;
    min-width: 104px;
    padding: 7px 14px;
    font: inherit;
    font-size: 14px;
    color: var(--uvp-text-secondary);
    cursor: pointer;
    background: transparent;
    border: 0;
    border-radius: 6px;
    transition:
      color 0.2s ease,
      background-color 0.2s ease,
      box-shadow 0.2s ease;
  }
  button.active {
    color: var(--uvp-brand-strong);
    background: var(--uvp-brand-soft);
  }
  button:hover:not(.active) {
    color: var(--uvp-text-primary);
    background: var(--uvp-secondary-action-bg);
  }
  button:focus-visible {
    outline: 2px solid rgb(var(--primary-6));
    outline-offset: 3px;
  }
}
.tab-state {
  padding: 1px 5px;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-secondary-action-bg);
  border-radius: 4px;
}
.section-subtitle {
  margin: 4px 0 0;
  font-size: 12px;
  font-weight: 400;
  line-height: 1.5;
  color: var(--color-text-3);
}
.history-panel,
.settings-panel {
  :deep(.arco-card-header) {
    flex-wrap: wrap;
    gap: 12px;
    height: auto;
    min-height: 68px;
    padding: 16px 20px 12px;
  }
}
.history-panel :deep(.arco-card-body) {
  padding: 0;
}
.upgrade-tasks {
  min-width: 0;
  overflow-x: auto;
  :deep(.arco-table-cell) {
    padding: 14px 16px;
  }
  :deep(.arco-table-tr-empty .arco-table-td) {
    height: 168px;
  }
  :deep(.arco-table-pagination) {
    padding: 0 16px 14px;
  }
}
.upgrade-detail {
  margin-top: 4px;
  font-size: 12px;
  color: var(--color-text-3);
  overflow-wrap: anywhere;
}
.upgrade-events {
  padding: 0;
  margin-top: 16px;
  overflow: hidden;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--color-border-2);
  border-radius: 14px;
  summary {
    padding: 15px 20px;
    font-size: 13px;
    color: var(--color-text-2);
    cursor: pointer;
    list-style-position: inside;
    span {
      margin-left: 8px;
      font-size: 12px;
      color: var(--color-text-3);
    }
  }
  &[open] summary {
    border-bottom: 1px solid var(--color-border-2);
  }
  > :not(summary) {
    margin-right: 20px;
    margin-left: 20px;
  }
  > .event-row:last-child,
  > .upgrade-caption:last-child {
    margin-bottom: 16px;
  }
}
.event-row {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: flex-start;
  margin-top: 12px;
  font-size: 13px;
  span {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .event-scope {
    flex: none;
    padding: 0 6px;
    font-size: 12px;
    line-height: 20px;
    color: var(--color-text-3);
    border: 1px solid var(--color-border-2);
    border-radius: 6px;
  }
  time {
    margin-left: auto;
    color: var(--color-text-3);
  }
}
.settings-panel :deep(.arco-card-body) {
  padding: 24px;
}
.upgrade-policy-form {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 6px 40px;
  max-width: 940px;
  :deep(.arco-form-item) {
    min-width: 0;
    margin-bottom: 18px;
  }
  :deep(.arco-form-item-label-col) {
    padding-bottom: 8px;
  }
  :deep(.arco-form-item-wrapper-col) {
    min-width: 0;
  }
}
.policy-save {
  grid-column: 1 / -1;
  padding-top: 20px;
  border-top: 1px solid var(--color-border-2);
  :deep(.arco-form-item-content-flex) {
    gap: 12px;
  }
}
.policy-dirty-alert {
  margin-bottom: 20px;
}
.available-update {
  margin: 18px 0;
}
.update-summary {
  display: flex;
  gap: 12px;
  justify-content: space-between;
  font-size: 13px;
  overflow-wrap: anywhere;
}
.release-notes-inline {
  max-height: 180px;
  margin: 18px 0;
  overflow-y: auto;
  line-height: 1.8;
  color: var(--color-text-2);
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
.package-details {
  margin-top: 20px;
  font-size: 12px;
  color: var(--color-text-3);
  overflow-wrap: anywhere;
  summary {
    margin-bottom: 8px;
    cursor: pointer;
  }
}
.upgrade-footnote {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: space-between;
  margin-top: 22px;
  font-size: 12px;
  color: var(--color-text-4);
}
.task-hint,
.release-notes-text {
  line-height: 1.8;
  color: var(--color-text-2);
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
.task-hint {
  margin: 16px 0;
}
.task-descriptions :deep(.arco-descriptions-table) {
  width: 100%;
  table-layout: fixed;
}
.task-descriptions :deep(.arco-descriptions-item-label) {
  width: 88px;
}
.task-descriptions :deep(.arco-descriptions-item-value) {
  overflow-wrap: anywhere;
}
.drawer-section-title {
  margin: 24px 0 12px;
  font-size: 14px;
  font-weight: 600;
  color: var(--color-text-1);
}
.diagnostic-log {
  max-height: 360px;
  padding: 16px;
  overflow-y: auto;
  font:
    12px/1.8 ui-monospace,
    Menlo,
    monospace;
  color: var(--color-text-2);
  overflow-wrap: anywhere;
  white-space: pre-wrap;
  background: var(--color-fill-1);
  border: 1px solid var(--color-border-2);
  border-radius: 6px;
}
.release-version {
  margin: 0 0 8px;
  font-size: 28px;
  color: var(--color-text-1);
}

@media (width <= 900px) {
  .version-main {
    flex-direction: column;
    gap: 20px;
    align-items: flex-start;
  }
  .version-footer {
    flex-direction: column;
    align-items: flex-start;
  }
}

@media (width <= 600px) {
  .service-upgrade-page {
    padding: 12px 8px;
  }
  .version-panel :deep(.arco-card-body) {
    padding: 20px 16px;
  }
  .version-title strong {
    font-size: 30px;
  }
  .upgrade-tabs {
    display: flex;
    gap: 4px;
    button {
      flex: 1;
      flex-wrap: wrap;
      min-width: 0;
      padding: 8px;
      font-size: 13px;
    }
  }
  .upgrade-policy-form {
    grid-template-columns: minmax(0, 1fr);
    gap: 0;
  }
  .history-panel,
  .settings-panel {
    :deep(.arco-card-header) {
      padding: 16px;
    }
  }
  .settings-panel :deep(.arco-card-body) {
    padding: 16px;
  }
  .update-summary {
    flex-direction: column;
  }
}
</style>
