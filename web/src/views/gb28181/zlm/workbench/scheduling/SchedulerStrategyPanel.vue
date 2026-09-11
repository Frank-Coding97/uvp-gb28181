<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { useUserStoreHook } from "@/store/modules/user";
import {
  getScheduler,
  switchScheduler,
  type SchedulerAlgorithm,
  type SchedulerEffectiveFrom
} from "@/api/gb28181-zlm";
import { zlmErrorPresentation } from "../../components/zlmFormatters";

const props = withDefaults(defineProps<{
  active?: boolean;
  canManage?: boolean;
}>(), {
  active: true
});

const algorithmMeta: Record<SchedulerAlgorithm, { title: string; desc: string }> = {
  roundrobin: {
    title: "轮询",
    desc: "依次分配，节点能力相近时最公平；无状态依赖。"
  },
  weighted: {
    title: "加权轮询",
    desc: "按节点权重比例分配，适合 CPU、带宽不同的异构集群。"
  },
  leastload: {
    title: "最小负载",
    desc: "按 NetThread 与 WorkThread 负载选择节点，需要可靠心跳数据。"
  }
};

const userStore = useUserStoreHook();
const current = ref<SchedulerAlgorithm | "">("");
const selected = ref<SchedulerAlgorithm | "">("");
const available = ref<SchedulerAlgorithm[]>([]);
const loading = ref(false);
const saving = ref(false);
const loadError = ref<unknown>(null);
const switchError = ref("");
const effectiveFrom = ref<SchedulerEffectiveFrom>("next_invite");
let generation = 0;

const canManage = computed(() => props.canManage ?? (
  userStore.account.permissions.includes("*:*:*")
  || userStore.account.permissions.includes("gb28181:zlm:scheduler:manage")
));
const currentTitle = computed(() => current.value ? algorithmMeta[current.value]?.title || current.value : "未装配");
const effectiveFromText = computed(() => effectiveFrom.value === "next_invite" ? "下次点播请求" : effectiveFrom.value);
const loadErrorText = computed(() => zlmErrorPresentation(loadError.value).label);

async function refresh() {
  if (!props.active) return false;
  const requestGeneration = ++generation;
  loading.value = true;
  loadError.value = null;
  try {
    const response = await getScheduler();
    if (requestGeneration !== generation || !props.active) return false;
    if (response.code !== 0 || !response.data) throw new Error(response.message || "调度策略加载失败");
    current.value = response.data.algorithm || "";
    selected.value = response.data.algorithm || "roundrobin";
    available.value = response.data.available ?? [];
    switchError.value = "";
    return true;
  } catch (error) {
    if (requestGeneration === generation && props.active) loadError.value = error;
    return false;
  } finally {
    if (requestGeneration === generation) loading.value = false;
  }
}

async function handleSave() {
  if (!props.active || !canManage.value || !selected.value || selected.value === current.value) return;
  const previous = current.value;
  const next = selected.value;
  saving.value = true;
  switchError.value = "";
  try {
    const response = await switchScheduler(next);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "调度策略切换失败");
    current.value = response.data.algorithm || next;
    selected.value = current.value;
    effectiveFrom.value = response.data.effectiveFrom || "next_invite";
    Message.success(`已切换到 ${algorithmMeta[next]?.title || next}，只影响新点播`);
    await refresh();
  } catch (error) {
    // A failed DB write is not a successful switch. Keep the previous local
    // strategy visible and ask for an explicit refresh if the backend reports
    // an uncertain split state.
    current.value = previous;
    selected.value = previous || "roundrobin";
    switchError.value = `切换失败，旧策略“${algorithmMeta[previous as SchedulerAlgorithm]?.title || previous || "未装配"}”仍保留在当前页面；请刷新确认实际状态。`;
    Message.error(zlmErrorPresentation(error).label);
  } finally {
    saving.value = false;
  }
}

watch(() => props.active, active => {
  if (!active) {
    generation += 1;
    loading.value = false;
    return;
  }
  void refresh();
}, { immediate: true });

defineExpose({ refresh });
</script>

<template>
  <section class="scheduler-strategy-panel" data-panel="scheduler-strategy" :aria-busy="loading ? 'true' : 'false'">
    <header class="panel-heading">
      <div>
        <span class="panel-eyebrow">SCHEDULER POLICY</span>
        <h2>调度策略</h2>
        <p>选择新媒体分配策略；已经建立的流和会话不会迁移。</p>
      </div>
      <div class="panel-actions">
        <span class="current-label">当前生效 <strong>{{ currentTitle }}</strong></span>
        <button type="button" class="refresh-button" :disabled="loading || !active" @click="refresh">刷新</button>
        <button v-if="canManage" type="button" class="primary-button" :disabled="saving || loading || !selected || selected === current || !active" @click="handleSave">
          {{ saving ? "保存中…" : "保存策略" }}
        </button>
      </div>
    </header>

    <div class="effective-boundary" role="status">
      生效范围：<strong>{{ effectiveFromText }}</strong>。只影响后续点播请求（INVITE）的选点策略，不重启节点、不迁移现有媒体会话。
    </div>
    <div v-if="!canManage" class="permission-state" role="status">当前账号没有调度策略管理权限，当前页面为只读。</div>
    <div v-if="switchError" class="switch-error" role="alert">{{ switchError }}</div>
    <div v-if="loadError" class="load-error" role="alert">
      <span>{{ loadErrorText }}</span>
      <button type="button" @click="refresh">重新加载</button>
    </div>
    <div v-if="!active" class="inactive-state" role="status">当前视图未激活，未请求调度策略。</div>
    <div v-else-if="loading && !available.length" class="loading-state" role="status">正在读取调度策略…</div>
    <div v-else-if="!available.length" class="empty-state" role="status">后端尚未返回可用调度策略。</div>
    <div v-else class="algorithm-list" role="radiogroup" aria-label="调度策略">
      <label v-for="algorithm in available" :key="algorithm" class="algorithm-card" :class="{ 'is-selected': selected === algorithm }">
        <input v-model="selected" type="radio" name="scheduler-algorithm" :value="algorithm" :disabled="!canManage || saving || !active" />
        <span class="algorithm-card__body">
          <strong>{{ algorithmMeta[algorithm]?.title || algorithm }}</strong>
          <small>{{ algorithmMeta[algorithm]?.desc || "后端返回的可用调度策略" }}</small>
        </span>
        <span v-if="current === algorithm" class="active-badge">当前生效</span>
      </label>
    </div>
  </section>
</template>

<style scoped>
.scheduler-strategy-panel { box-sizing: border-box; min-width: 0; padding: 18px; color: var(--zlm-text-2); background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-xl); box-shadow: var(--uvp-panel-shadow); }
.panel-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; }
.panel-eyebrow { color: var(--zlm-brand-600); font-family: var(--zlm-font-mono); font-size: 10px; letter-spacing: .12em; }
.panel-heading h2 { margin: 4px 0 0; color: var(--zlm-text-1); font-size: 18px; }
.panel-heading p { margin: 5px 0 0; color: var(--zlm-text-3); font-size: var(--zlm-fs-caption); }
.panel-actions { display: flex; align-items: center; justify-content: flex-end; gap: 8px; flex-wrap: wrap; }
.current-label { color: var(--zlm-text-3); font-size: 12px; white-space: nowrap; }.current-label strong { margin-left: 4px; color: var(--zlm-brand-600); }
.panel-actions button, .load-error button { min-height: 32px; padding: 0 11px; color: var(--zlm-text-2); background: var(--zlm-card); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-md); cursor: pointer; }
.panel-actions button:hover, .load-error button:hover { color: var(--zlm-brand-600); border-color: var(--zlm-brand-500); }
.panel-actions button:disabled, .load-error button:disabled { cursor: wait; opacity: .6; }
.panel-actions .primary-button { color: #fff; background: var(--zlm-brand-600); border-color: var(--zlm-brand-600); }
.effective-boundary { margin-top: 16px; padding: 10px 12px; color: var(--zlm-text-2); font-size: var(--zlm-fs-caption); line-height: 1.6; background: var(--zlm-brand-50); border: 1px solid var(--zlm-brand-200); border-radius: var(--zlm-radius-md); }
.effective-boundary strong { color: var(--zlm-brand-700); font-family: var(--zlm-font-mono); }
.permission-state, .switch-error, .load-error { margin-top: 12px; padding: 10px 12px; font-size: var(--zlm-fs-caption); border-radius: var(--zlm-radius-md); }
.permission-state { color: var(--zlm-text-3); border: 1px dashed var(--zlm-border-strong); }.switch-error, .load-error { color: var(--zlm-danger-600); background: var(--zlm-danger-50); border: 1px solid var(--zlm-danger-200); }.load-error { display: flex; align-items: center; justify-content: space-between; gap: 12px; }.load-error button { flex: none; min-height: 28px; padding: 0 8px; font-size: 12px; }
.algorithm-list { display: grid; gap: 10px; margin-top: 16px; }.algorithm-card { display: flex; align-items: flex-start; gap: 12px; min-width: 0; padding: 14px; background: var(--zlm-fill-1); border: 1px solid var(--zlm-border); border-radius: var(--zlm-radius-lg); cursor: pointer; transition: border-color .18s ease, box-shadow .18s ease, background-color .18s ease; }.algorithm-card:hover, .algorithm-card.is-selected { background: var(--zlm-brand-50); border-color: var(--zlm-brand-300); box-shadow: 0 10px 22px -18px rgb(37 99 235 / 34%); }.algorithm-card input { flex: none; margin-top: 3px; accent-color: var(--zlm-brand-600); }.algorithm-card__body { display: flex; min-width: 0; flex: 1; flex-direction: column; gap: 4px; }.algorithm-card__body strong { color: var(--zlm-text-1); font-size: 14px; }.algorithm-card__body small { color: var(--zlm-text-3); font-size: 12px; line-height: 1.5; }.active-badge { flex: none; padding: 2px 7px; color: var(--zlm-success-600); background: var(--zlm-success-50); border-radius: 999px; font-size: 11px; }.loading-state, .empty-state, .inactive-state { display: grid; min-height: 190px; margin-top: 16px; color: var(--zlm-text-3); text-align: center; background: var(--zlm-fill-1); border: 1px dashed var(--zlm-border); border-radius: var(--zlm-radius-lg); place-items: center; }
button:focus-visible, input:focus-visible { outline: 2px solid var(--zlm-brand-500); outline-offset: 2px; }
@media (max-width: 760px) { .panel-heading { flex-direction: column; }.panel-actions { width: 100%; justify-content: flex-start; }.current-label { width: 100%; }.algorithm-card { align-items: flex-start; flex-wrap: wrap; }.active-badge { margin-left: 28px; } }
@media (prefers-reduced-motion: reduce) { .algorithm-card { transition: none; } }
</style>
