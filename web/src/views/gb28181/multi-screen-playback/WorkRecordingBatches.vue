<script setup lang="ts">
import { ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { listWorkRecordingBatches, stopWorkRecordingBatch, workRecordingBatchDownloadUrl, workRecordingBatchFileUrl, type WorkRecordingBatchSnapshot } from "@/api/gb28181-work-recording";

const props = defineProps<{ visible: boolean; canStop: boolean }>();
const emit = defineEmits<{ (event: "close"): void; (event: "form", batch: WorkRecordingBatchSnapshot): void }>();
const rows = ref<WorkRecordingBatchSnapshot[]>([]);
const loading = ref(false);
const error = ref("");
const stopping = ref("");
const selected = ref<WorkRecordingBatchSnapshot | null>(null);
let sequence = 0;
const stateLabel: Record<string, string> = { starting: "开始中", recording: "录像中", stopping: "结束中", stopped: "已结束", failed: "失败", unknown: "待核实" };
async function load() {
  const current = ++sequence;
  loading.value = true;
  error.value = "";
  try {
    const response = await listWorkRecordingBatches();
    if (response.code !== 0) throw new Error(response.message || "查询台账失败");
    if (current === sequence) rows.value = response.data.items;
  } catch (reason: any) {
    if (current === sequence) error.value = reason?.response?.data?.message || reason?.message || "查询台账失败";
  } finally { if (current === sequence) loading.value = false; }
}
async function stop(batch: WorkRecordingBatchSnapshot) {
  if (!props.canStop || stopping.value) return;
  stopping.value = batch.id;
  try {
    const response = await stopWorkRecordingBatch(batch.id);
    if (response.code !== 0) throw new Error(response.message || "结束批次失败");
    await load();
  } catch (reason: any) { Message.error(reason?.response?.data?.message || reason?.message || "结束批次失败"); }
  finally { stopping.value = ""; }
}
function download(batch: WorkRecordingBatchSnapshot) { window.open(workRecordingBatchDownloadUrl(batch.id), "_blank", "noopener"); }
function view(batch: WorkRecordingBatchSnapshot) { selected.value = batch; }
watch(() => props.visible, visible => { if (visible) void load(); else sequence += 1; });
</script>

<template>
  <a-modal :visible="visible" title="作业台账" :width="900" :footer="false" @cancel="emit('close')">
    <div class="ledger-heading"><span>本地保存的作业台账，按批次关联本次录制的 1～4 路摄像头及 ZLM 全部分片。</span><a-button :loading="loading" @click="load">刷新</a-button></div>
    <p v-if="error" role="alert" class="ledger-error">{{ error }}</p>
    <div v-if="!loading && !error && !rows.length" class="ledger-empty">暂无作业台账</div>
    <div v-for="batch in rows" :key="batch.id" class="ledger-row">
      <div class="ledger-main"><strong>{{ batch.id }}</strong><small>{{ batch.cameras.length }} 路 · {{ stateLabel[batch.state] || "待核实" }}</small><div class="camera-list"><span v-for="camera in batch.cameras" :key="camera.channelId">{{ camera.channelName || `摄像头 ${camera.channelId}` }}：{{ camera.files?.length || 0 }} 个分片</span></div></div>
      <a-button @click="view(batch)">查看台账</a-button>
      <a-button @click="emit('form', batch)">填写台账</a-button>
      <a-button @click="download(batch)">下载 ZIP</a-button>
      <a-button v-if="canStop && ['recording', 'unknown', 'starting'].includes(batch.state)" status="danger" :loading="stopping === batch.id" :disabled="Boolean(stopping)" @click="stop(batch)">结束录制</a-button>
    </div>
  </a-modal>
  <a-modal v-if="selected" :visible="Boolean(selected)" title="台账详情" :width="900" :footer="false" @cancel="selected = null">
    <div class="detail-summary"><strong>{{ selected.id }}</strong><span>状态：{{ stateLabel[selected.state] || "待核实" }}</span><a-button @click="emit('form', selected)">填写台账</a-button><a-button @click="download(selected)">下载整批 ZIP</a-button></div>
    <section v-for="camera in selected.cameras" :key="camera.channelId" class="camera-section">
      <header><strong>{{ camera.channelName || `摄像头 ${camera.channelId}` }}</strong><span>{{ stateLabel[camera.state] || "待核实" }} · {{ camera.files?.length || 0 }} 个分片</span></header>
      <div v-for="file in camera.files || []" :key="file.id" class="file-row"><span>{{ file.fileName }}</span><small>{{ file.startTime ? new Date(file.startTime).toLocaleString() : "时间待核实" }}</small><a :href="workRecordingBatchFileUrl(selected.id, file.id)" target="_blank" rel="noopener">在线播放</a></div>
      <p v-if="!camera.files?.length" class="empty-file">暂无已归档分片</p>
    </section>
  </a-modal>
</template>

<style scoped>
.ledger-heading { display:flex; align-items:center; justify-content:space-between; gap:16px; color:var(--color-text-3); font-size:12px; margin-bottom:12px; }
.ledger-error { color:rgb(var(--danger-6)); }
.ledger-empty { padding:24px; text-align:center; color:var(--color-text-3); }
.ledger-row { display:flex; align-items:center; gap:12px; padding:14px 0; border-bottom:1px solid var(--color-border-2); }
.ledger-main { flex:1; min-width:0; }.ledger-main strong { display:block; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }.ledger-main small { color:var(--color-text-3); }.camera-list { display:flex; flex-wrap:wrap; gap:6px 12px; margin-top:8px; color:var(--color-text-2); font-size:12px; }
.detail-summary { display:flex; align-items:center; gap:16px; margin-bottom:16px; }.detail-summary strong { flex:1; }.camera-section { border-top:1px solid var(--color-border-2); padding:14px 0; }.camera-section header { display:flex; justify-content:space-between; margin-bottom:8px; }.camera-section header span, .file-row small, .empty-file { color:var(--color-text-3); font-size:12px; }.file-row { display:flex; align-items:center; gap:12px; padding:8px 0; }.file-row span { flex:1; min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }.file-row a { color:rgb(var(--primary-6)); }
</style>
