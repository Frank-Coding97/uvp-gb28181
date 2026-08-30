<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { Message } from "@arco-design/web-vue";
import { Activity, CalendarClock, CircleStop, Radio, ShieldAlert } from "lucide-vue-next";
import { listZLMNodes, type ZLMNode } from "@/api/gb28181-zlm";
import {
  forceStopZLMRecording,
  getZLMRecordingStatus,
  preflightForceStopZLMRecording,
  preflightStartZLMRecording,
  preflightStopZLMRecording,
  startZLMRecording,
  stopZLMRecording,
  type ZLMOwnershipTarget,
  type ZLMRecorderType,
  type ZLMRecordingPreflight,
  type ZLMRecordingResult
} from "@/api/gb28181-zlm-runtime";
import { useZLMContextStore } from "@/store/modules/zlm-context";
import { useUserStoreHook } from "@/store/modules/user";
import ZLMDangerActionDialog from "@/views/gb28181/zlm/components/ZLMDangerActionDialog.vue";
import ZLMNodeContextBar from "@/views/gb28181/zlm/components/ZLMNodeContextBar.vue";
import {
  buildRecordingTarget,
  recordingImpactItems,
  recordingScheduleQuery,
  recordingStatusPresentation,
  recordingTargetLabel
} from "../recordingRuntimeState";
import { recordingErrorPresentation } from "../recordingState";

type RecordingAction = "start" | "stop" | "force-stop";

const router = useRouter();
const context = useZLMContextStore();
const userStore = useUserStoreHook();
const nodes = ref<ZLMNode[]>([]);
const nodesLoading = ref(false);
const loadError = ref("");
const form = reactive({
  schema: "rtsp",
  vhost: "__defaultVhost__",
  app: "live",
  stream: "",
  maxSecond: ""
});
const recorderType = ref<ZLMRecorderType>(1);
const forcePreflightReason = ref("");
const fieldErrors = ref<Record<string, string>>({});
const status = ref<ZLMRecordingResult | null>(null);
const statusLoading = ref(false);
const actionLoading = ref(false);
const preflightLoading = ref(false);
const preflight = ref<ZLMRecordingPreflight | null>(null);
const preparedTarget = ref<ZLMOwnershipTarget | null>(null);
const preparedAction = ref<RecordingAction | null>(null);
const dangerVisible = ref(false);
let generation = 0;

const selectedNodeId = computed(() => context.selectedNodeId);
const contextNodes = computed(() => nodes.value.map(node => ({ id: node.id, name: node.name, state: node.state })));
const selectedNodeName = computed(() => context.selectedNode?.name ?? `节点 #${selectedNodeId.value ?? "—"}`);
const hasPermission = (permission: string) => userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes(permission);
const canControl = computed(() => hasPermission("gb28181:recording:control"));
const canForceStop = computed(() => hasPermission("gb28181:recording:force-stop"));
const statusView = computed(() => recordingStatusPresentation(status.value));
const targetLabel = computed(() => preparedTarget.value ? recordingTargetLabel(preparedTarget.value) : "未选择媒体目标");
const impactItems = computed(() => preflight.value ? recordingImpactItems(preflight.value.snapshot) : []);
const actionCopy = computed(() => {
  if (preparedAction.value === "start") return { phrase: `START ${preparedTarget.value?.media.stream ?? ""}`, label: "确认启动", reason: false };
  if (preparedAction.value === "stop") return { phrase: `STOP ${preparedTarget.value?.media.stream ?? ""}`, label: "确认普通停止", reason: false };
  return { phrase: `FORCE STOP ${preparedTarget.value?.media.stream ?? ""}`, label: "确认强制停止", reason: true };
});

function currentTarget() {
  const validation = buildRecordingTarget({
    nodeId: selectedNodeId.value ?? 0,
    schema: form.schema,
    vhost: form.vhost,
    app: form.app,
    stream: form.stream
  });
  fieldErrors.value = validation.errors;
  if (Object.keys(validation.errors).length > 0) {
    Message.warning(Object.values(validation.errors)[0]);
    return null;
  }
  return validation.target;
}

function requestBody(target: ZLMOwnershipTarget) {
  const rawMaxSecond = form.maxSecond.trim();
  const maxSecond = rawMaxSecond ? Number(rawMaxSecond) : undefined;
  if (recorderType.value === 0 && rawMaxSecond && (!Number.isSafeInteger(maxSecond) || (maxSecond ?? 0) <= 0)) {
    fieldErrors.value = { ...fieldErrors.value, maxSecond: "最长秒数必须为正整数" };
    Message.warning("最长秒数必须为正整数");
    return null;
  }
  return {
    media: target.media,
    type: recorderType.value,
    ...(recorderType.value === 0 && maxSecond !== undefined ? { maxSecond } : {})
  };
}

async function loadNodes() {
  nodesLoading.value = true;
  loadError.value = "";
  try {
    const response = await listZLMNodes();
    if (response.code !== 0) throw new Error(response.message || "媒体节点加载失败");
    nodes.value = response.data?.list ?? [];
  } catch (error) {
    loadError.value = recordingErrorPresentation(error);
  } finally {
    nodesLoading.value = false;
  }
}

async function refresh() {
  const target = currentTarget();
  if (!target) return;
  const currentGeneration = ++generation;
  statusLoading.value = true;
  loadError.value = "";
  try {
    const response = await getZLMRecordingStatus(target.nodeId, target.media, recorderType.value);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "录制状态读取失败");
    if (currentGeneration === generation) status.value = response.data;
  } catch (error) {
    if (currentGeneration === generation) loadError.value = recordingErrorPresentation(error);
  } finally {
    if (currentGeneration === generation) statusLoading.value = false;
  }
}

async function prepare(action: RecordingAction) {
  if (action === "force-stop" ? !canForceStop.value : !canControl.value) return;
  if (action === "force-stop" && !forcePreflightReason.value.trim()) {
    Message.warning("请先填写强制停止预检理由");
    return;
  }
  const target = currentTarget();
  if (!target) return;
  const request = requestBody(target);
  if (!request) return;
  const currentGeneration = ++generation;
  preflightLoading.value = true;
  loadError.value = "";
  preflight.value = null;
  preparedTarget.value = { nodeId: target.nodeId, media: { ...target.media } };
  preparedAction.value = action;
  try {
    const response = action === "start"
      ? await preflightStartZLMRecording(target.nodeId, request)
      : action === "stop"
        ? await preflightStopZLMRecording(target.nodeId, request)
        : await preflightForceStopZLMRecording(target.nodeId, { ...request, reason: forcePreflightReason.value.trim() });
    if (response.code !== 0 || !response.data) throw new Error(response.message || "录制预检失败");
    if (currentGeneration !== generation) return;
    preflight.value = response.data;
    dangerVisible.value = true;
  } catch (error) {
    if (currentGeneration === generation) {
      loadError.value = recordingErrorPresentation(error);
      preparedTarget.value = null;
      preparedAction.value = null;
    }
  } finally {
    if (currentGeneration === generation) preflightLoading.value = false;
  }
}

async function execute(payload: { fingerprint: string; reason: string }) {
  const target = preparedTarget.value;
  const action = preparedAction.value;
  const opening = preflight.value;
  if (!target || !action || !opening || payload.fingerprint !== opening.fingerprint) {
    dangerVisible.value = false;
    Message.warning("录制目标或影响指纹已变化，请重新预检");
    return;
  }
  actionLoading.value = true;
  try {
    const request = requestBody(target);
    if (!request) return;
    const response = action === "start"
      ? await startZLMRecording(target.nodeId, request)
      : action === "stop"
        ? await stopZLMRecording(target.nodeId, { ...request, fingerprint: opening.fingerprint })
        : await forceStopZLMRecording(target.nodeId, { ...request, fingerprint: opening.fingerprint, reason: payload.reason });
    if (response.code !== 0 || !response.data) throw new Error(response.message || "录制操作失败");
    status.value = response.data;
    dangerVisible.value = false;
    Message.success(action === "start" ? "录制已启动并完成回读" : "录制已停止并完成回读");
    preflight.value = null;
    preparedAction.value = null;
    preparedTarget.value = null;
  } catch (error) {
    loadError.value = recordingErrorPresentation(error);
    Message.error(loadError.value);
  } finally {
    actionLoading.value = false;
  }
}

function clearPrepared() {
  dangerVisible.value = false;
  preflight.value = null;
  preparedTarget.value = null;
  preparedAction.value = null;
}

function openSchedules() {
  const target = currentTarget();
  if (!target) return;
  void router.push({ path: "/media/recordings", query: { view: "plans", ...recordingScheduleQuery(target) } });
}

watch(
  () => [selectedNodeId.value, recorderType.value, form.schema, form.vhost, form.app, form.stream, form.maxSecond],
  () => {
    generation += 1;
    status.value = null;
    loadError.value = "";
    clearPrepared();
  }
);

onMounted(loadNodes);
onBeforeUnmount(() => { generation += 1; });
defineExpose({ refresh });
</script>

<template>
  <section class="recording-runtime-control">
    <header class="runtime-heading">
      <div><h2>录制运行控制</h2><p>按完整媒体身份控制单个节点的 MP4 / HLS recorder；所有状态均以后端回读为准。</p></div>
      <a-button :disabled="!selectedNodeId || !form.stream.trim()" @click="openSchedules"><template #icon><CalendarClock :size="15" /></template>查看录像计划</a-button>
    </header>

    <ZLMNodeContextBar :nodes="contextNodes" :loading="nodesLoading || statusLoading" title="录制所在节点" @refresh="loadNodes(); refresh()" />

    <a-alert v-if="loadError" type="error" class="runtime-alert">{{ loadError }}</a-alert>
    <a-alert v-else-if="!canControl && !canForceStop" type="info" class="runtime-alert">当前账号仅可查看录制文件，没有手工录制控制或强制停止权限。</a-alert>

    <div class="runtime-grid">
      <section class="runtime-card runtime-target-card">
        <div class="card-title"><Radio :size="17" /><div><strong>媒体目标</strong><span>Schema / VHost / App / Stream 缺一不可</span></div></div>
        <div class="target-form">
          <a-form-item label="Schema" :validate-status="fieldErrors.schema ? 'error' : undefined" :help="fieldErrors.schema"><a-input v-model="form.schema" placeholder="rtsp" /></a-form-item>
          <a-form-item label="VHost" :validate-status="fieldErrors.vhost ? 'error' : undefined" :help="fieldErrors.vhost"><a-input v-model="form.vhost" placeholder="__defaultVhost__" /></a-form-item>
          <a-form-item label="App" :validate-status="fieldErrors.app ? 'error' : undefined" :help="fieldErrors.app"><a-input v-model="form.app" placeholder="live" /></a-form-item>
          <a-form-item label="Stream ID" :validate-status="fieldErrors.stream ? 'error' : undefined" :help="fieldErrors.stream"><a-input v-model="form.stream" placeholder="国标通道流标识" /></a-form-item>
          <a-form-item label="录制类型"><a-radio-group v-model="recorderType" type="button"><a-radio :value="1">MP4 文件</a-radio><a-radio :value="0">HLS 运行态</a-radio></a-radio-group></a-form-item>
          <a-form-item v-if="recorderType === 0" label="最长秒数" :validate-status="fieldErrors.maxSecond ? 'error' : undefined" :help="fieldErrors.maxSecond"><a-input v-model="form.maxSecond" inputmode="numeric" placeholder="留空表示后端默认" /></a-form-item>
        </div>
        <div v-if="recorderType === 1" class="runtime-note">MP4 只有在后端预检证明该流可归属到既有 GB28181 通道时，才会出现最终确认入口。</div>
        <div v-else class="runtime-note runtime-note--hls">HLS 仅为节点运行态分片能力，不进入云录像文件目录。</div>
      </section>

      <section class="runtime-card runtime-status-card">
        <div class="card-title"><Activity :size="17" /><div><strong>服务端状态</strong><span>实际 recorder 状态与手工租约归属分开显示</span></div></div>
        <div :class="['status-box', `status-box--${statusView.tone}`]">
          <span>当前结论</span><strong>{{ statusView.label }}</strong>
          <small v-if="status">外部状态 {{ status.externalState }} · 手工状态 {{ status.state }} · 可重试 {{ status.retryable ? "是" : "否" }}</small>
        </div>
        <a-button :loading="statusLoading" :disabled="!selectedNodeId" @click="refresh">回读录制状态</a-button>
        <div v-if="status?.ownership" class="ownership-summary">
          <strong>持有与影响</strong>
          <ul><li v-for="item in recordingImpactItems(status.ownership)" :key="item">{{ item }}</li></ul>
        </div>
      </section>
    </div>

    <section class="runtime-card runtime-actions-card">
      <div class="card-title"><CircleStop :size="17" /><div><strong>受控操作</strong><span>预检成功后才能进入不可变目标确认</span></div></div>
      <div class="control-copy">
        <p>普通停止仅释放当前账号创建的手工录制，不会关闭录像计划或持续录像。</p>
        <p>强制停止必须单独授权、填写理由并重新确认影响指纹。</p>
      </div>
      <div class="runtime-actions">
        <a-button v-if="canControl" type="primary" :loading="preflightLoading && preparedAction === 'start'" @click="prepare('start')">预检并启动</a-button>
        <a-button v-if="canControl" status="danger" :loading="preflightLoading && preparedAction === 'stop'" @click="prepare('stop')">预检普通停止</a-button>
        <template v-if="canForceStop">
          <a-input v-model="forcePreflightReason" class="force-reason" :max-length="256" placeholder="强制停止预检理由（必填）" />
          <a-button status="danger" type="primary" :loading="preflightLoading && preparedAction === 'force-stop'" @click="prepare('force-stop')"><ShieldAlert :size="14" />预检强制停止</a-button>
        </template>
      </div>
    </section>

    <ZLMDangerActionDialog
      v-if="preparedTarget && preflight"
      v-model:visible="dangerVisible"
      :node-id="preparedTarget.nodeId"
      :node-name="selectedNodeName"
      :target-key="targetLabel"
      :target-label="targetLabel"
      :fingerprint="preflight.fingerprint"
      :impacts="impactItems"
      :confirm-phrase="actionCopy.phrase"
      :require-reason="actionCopy.reason"
      :action-label="actionCopy.label"
      :busy="actionLoading"
      @confirm="execute"
      @stale="clearPrepared"
    />
  </section>
</template>

<style scoped>
.recording-runtime-control { display: flex; min-height: 0; flex: 1; flex-direction: column; gap: 14px; overflow: auto; padding: 2px 2px 18px; color: var(--zlm-text-2); }
.runtime-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }.runtime-heading h2 { margin: 0; color: var(--zlm-text-1); font-size: 18px; }.runtime-heading p { margin: 5px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }
.runtime-alert { flex: none; }.runtime-grid { display: grid; grid-template-columns: minmax(0, 1.2fr) minmax(320px, .8fr); gap: 14px; }.runtime-card { padding: 16px; background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); }.card-title { display: flex; align-items: flex-start; gap: 9px; margin-bottom: 14px; color: var(--zlm-brand-600); }.card-title > div { display: flex; flex-direction: column; }.card-title strong { color: var(--zlm-text-1); }.card-title span { margin-top: 3px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.target-form { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 12px; }.target-form :deep(.arco-form-item) { margin-bottom: 12px; }.runtime-note { padding: 9px 11px; color: var(--zlm-warn-600); background: var(--zlm-warn-50); border: 1px solid var(--zlm-warn-500); border-radius: var(--zlm-radius-md); font-size: var(--zlm-fs-caption); }.runtime-note--hls { color: var(--zlm-info-600); background: var(--zlm-info-50); border-color: var(--zlm-info-500); }.runtime-status-card { display: flex; flex-direction: column; }.status-box { display: flex; flex-direction: column; gap: 5px; margin-bottom: 12px; padding: 14px; background: var(--zlm-fill-2); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); }.status-box span, .status-box small { color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.status-box strong { color: var(--zlm-text-1); }.status-box--success { border-color: var(--zlm-success-500); }.status-box--warning { border-color: var(--zlm-warn-500); }.status-box--danger { border-color: var(--zlm-danger-500); }.ownership-summary { margin-top: 14px; }.ownership-summary > strong { color: var(--zlm-text-1); font-size: var(--zlm-fs-caption); }.ownership-summary ul { margin: 7px 0 0; padding-left: 18px; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); line-height: 1.7; }.runtime-actions-card { flex: none; }.control-copy { color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }.control-copy p { margin: 3px 0; }.runtime-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-top: 12px; }.force-reason { width: min(360px, 100%); }.runtime-actions svg { margin-right: 4px; vertical-align: -2px; }
@media (max-width: 900px) { .runtime-heading { flex-direction: column; }.runtime-grid { grid-template-columns: 1fr; }.target-form { grid-template-columns: 1fr; } }
</style>
