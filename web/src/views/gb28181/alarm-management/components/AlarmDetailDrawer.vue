<template>
  <a-drawer
    :visible="visible"
    width="min(840px, 94vw)"
    :footer="false"
    unmount-on-close
    class="alarm-detail-drawer"
    @update:visible="emit('update:visible', $event)"
    @cancel="emit('update:visible', false)"
  >
    <template #title>
      <div class="alarm-detail-title">
        <span class="alarm-detail-title__icon"><BellRing :size="18" /></span>
        <div><strong>告警详情</strong><span>ID {{ alarmId || "—" }}</span></div>
      </div>
    </template>

    <a-spin :loading="loading" class="alarm-detail-spin">
      <div class="alarm-detail-content" data-testid="alarm-detail-content">
        <a-alert v-if="errorMessage" type="error" class="alarm-detail-error">
          <div class="alarm-detail-error__content">
            <span>{{ errorMessage }}</span>
            <a-button data-testid="detail-retry" size="small" @click="loadDetail">重试</a-button>
          </div>
        </a-alert>

        <template v-else-if="detail">
          <section class="alarm-detail-context" aria-label="告警对象">
            <div><span>设备</span><strong>{{ displayAlarmEntityName(detail.device) }}</strong><code>{{ detail.device.code }}</code></div>
            <div><span>来源</span><strong>{{ displayAlarmEntityName(detail.channel, detail.sourceCode || "未知来源") }}</strong><code>{{ detail.sourceCode || "—" }}</code></div>
          </section>

          <section class="alarm-detail-section" aria-labelledby="alarm-detail-event">
            <h3 id="alarm-detail-event">事件信息</h3>
            <a-descriptions :column="descriptionColumns" bordered size="medium">
              <a-descriptions-item label="平台接收时间">{{ formatDateTime(detail.receivedAt) }}</a-descriptions-item>
              <a-descriptions-item label="设备告警时间">{{ formatDateTime(detail.alarmTime) }}</a-descriptions-item>
              <a-descriptions-item label="告警级别"><a-tag>{{ detail.priority.label }}</a-tag></a-descriptions-item>
              <a-descriptions-item label="告警方法">{{ detail.method.label }}</a-descriptions-item>
              <a-descriptions-item label="告警类型">{{ detail.alarmType.label }}</a-descriptions-item>
              <a-descriptions-item label="类型原始值">{{ rawEnumValue(detail.alarmType.value) }}</a-descriptions-item>
              <a-descriptions-item label="类型参数" :span="descriptionColumns">{{ detail.alarmTypeParam || "—" }}</a-descriptions-item>
              <a-descriptions-item label="告警描述" :span="descriptionColumns">{{ detail.description || "—" }}</a-descriptions-item>
            </a-descriptions>
          </section>

          <section class="alarm-detail-section" aria-labelledby="alarm-detail-extra">
            <h3 id="alarm-detail-extra">补充信息</h3>
            <a-descriptions :column="descriptionColumns" bordered size="medium">
              <a-descriptions-item label="经度">{{ coordinateText(detail.longitude) }}</a-descriptions-item>
              <a-descriptions-item label="纬度">{{ coordinateText(detail.latitude) }}</a-descriptions-item>
              <a-descriptions-item label="摘要指纹" :span="descriptionColumns"><code>{{ detail.rawDigest || "—" }}</code></a-descriptions-item>
              <a-descriptions-item label="创建时间">{{ formatDateTime(detail.createdAt) }}</a-descriptions-item>
              <a-descriptions-item label="更新时间">{{ formatDateTime(detail.updatedAt) }}</a-descriptions-item>
            </a-descriptions>
          </section>

          <section class="alarm-detail-section" aria-labelledby="alarm-detail-raw">
            <h3 id="alarm-detail-raw">报文摘要</h3>
            <pre class="alarm-raw-summary">{{ detail.rawSummary || "无报文摘要" }}</pre>
          </section>
        </template>

        <div v-else class="alarm-detail-placeholder" aria-live="polite">
          <LoaderCircle v-if="loading" :size="24" class="alarm-detail-loading-icon" />
          <span>{{ loading ? "正在加载告警详情" : "请选择一条告警" }}</span>
        </div>
      </div>
    </a-spin>
  </a-drawer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { BellRing, LoaderCircle } from "@lucide/vue";
import { displayAlarmEntityName } from "../alarmState";
import { getAlarmDetail, type AlarmDetail } from "../api";

const props = defineProps<{
  visible: boolean;
  alarmId: string | null;
  canDelete: boolean;
}>();

const emit = defineEmits<{
  (event: "update:visible", value: boolean): void;
}>();

const detail = ref<AlarmDetail | null>(null);
const loading = ref(false);
const errorMessage = ref("");
const descriptionColumns = computed(() => (window.innerWidth < 640 ? 1 : 2));
let requestToken = 0;

async function loadDetail() {
  if (!props.visible || !props.alarmId) return;
  const alarmId = props.alarmId;
  const token = ++requestToken;
  detail.value = null;
  errorMessage.value = "";
  loading.value = true;
  try {
    const response = await getAlarmDetail(alarmId);
    if (token !== requestToken) return;
    detail.value = response.data;
  } catch (error) {
    if (token !== requestToken) return;
    errorMessage.value = "加载告警详情失败，请重试";
    console.error(error);
  } finally {
    if (token === requestToken) loading.value = false;
  }
}

function formatDateTime(value: string | null | undefined): string {
  if (!value) return "未知";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "未知" : date.toLocaleString("zh-CN", { hour12: false });
}

function coordinateText(value: number | null): string {
  return value === null ? "—" : String(value);
}

function rawEnumValue(value: number | null): string {
  return value === null ? "未知" : String(value);
}

watch(
  () => [props.visible, props.alarmId] as const,
  ([visible, alarmId]) => {
    if (visible && alarmId) loadDetail();
    else {
      requestToken += 1;
      detail.value = null;
      errorMessage.value = "";
      loading.value = false;
    }
  },
  { immediate: true }
);
</script>

<style scoped>
.alarm-detail-spin,
.alarm-detail-content { min-height: 520px; }
.alarm-detail-content { color: var(--uvp-text-primary); }
.alarm-detail-title { display: flex; gap: 10px; align-items: center; min-width: 0; }
.alarm-detail-title__icon { display: grid; width: 34px; height: 34px; place-items: center; color: var(--uvp-danger); background: color-mix(in srgb, var(--uvp-danger) 10%, transparent); border-radius: 6px; }
.alarm-detail-title > div { display: flex; flex-direction: column; min-width: 0; }
.alarm-detail-title strong { font-size: 15px; }
.alarm-detail-title span:last-child { color: var(--uvp-text-tertiary); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 11px; }
.alarm-detail-context { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; padding: 14px 18px; background: var(--uvp-bg-secondary); border-bottom: 1px solid var(--uvp-border); }
.alarm-detail-context > div { display: flex; flex-direction: column; min-width: 0; }
.alarm-detail-context span { color: var(--uvp-text-tertiary); font-size: 11px; }
.alarm-detail-context strong,
.alarm-detail-context code { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.alarm-detail-context strong { margin: 2px 0; font-size: 13px; }
.alarm-detail-context code { color: var(--uvp-text-secondary); font-size: 11px; }
.alarm-detail-section { padding: 16px 18px 0; }
.alarm-detail-section h3 { margin: 0 0 10px; font-size: 14px; }
.alarm-detail-error { margin: 18px; }
.alarm-detail-error__content { display: flex; gap: 12px; align-items: center; justify-content: space-between; }
.alarm-detail-placeholder { display: flex; min-height: 460px; flex-direction: column; gap: 10px; align-items: center; justify-content: center; color: var(--uvp-text-tertiary); }
.alarm-detail-loading-icon { animation: alarm-detail-spin 1s linear infinite; }
.alarm-raw-summary { max-height: 260px; padding: 12px; margin: 0; overflow: auto; color: var(--uvp-text-secondary); font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 12px; line-height: 1.55; white-space: pre; background: var(--uvp-bg-secondary); border: 1px solid var(--uvp-border); border-radius: 6px; }
@keyframes alarm-detail-spin { to { transform: rotate(360deg); } }
@media (max-width: 640px) {
  .alarm-detail-spin,
  .alarm-detail-content { min-height: 420px; }
  .alarm-detail-context { grid-template-columns: 1fr; gap: 10px; padding: 12px; }
  .alarm-detail-section { padding: 14px 12px 0; }
}
@media (prefers-reduced-motion: reduce) { .alarm-detail-loading-icon { animation: none; } }
</style>
