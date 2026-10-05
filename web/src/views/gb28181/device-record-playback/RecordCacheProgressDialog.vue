<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import {
  cancelRecordCacheTask,
  formatByteSize,
  formatDuration,
  getRecordCacheTask,
  isRecordCacheTaskActive,
  isRecordCacheTaskStoppable,
  type RecordCacheTask
} from "@/api/recordCache";

/**
 * 提交缓存任务后的**实时进度弹窗**。
 *
 * 它只做两件事：让用户看到"确实在跑"（进度 + 实时速率），以及提供两个出口 ——
 * 任务跑在服务端，关掉弹窗甚至关掉页面都不会中断，所以默认引导用户「转后台」，
 * 而不是在这里干等一个可能几十分钟的长任务。
 *
 * ⛔ 「转后台」和「查看缓存任务」是**两件事**，不能共用一个处理函数：
 *    - 转后台：只关窗，用户留在当前页面继续干别的（任务在服务端跑着）。
 *    - 查看缓存任务：关窗 **并且** 把用户送到录像缓存列表页。
 *    2026-10-05 用户明确要求"点转后台不要再跳走，直接关闭 dialog 就行"。
 *
 * ⛔ 第三件事是「取消任务」：它同样**只关窗**（见 cancelTask），既不跳列表页、
 *    也不删台账 —— 取消只是"不再继续拉"，已经拉回来的那部分留在服务器上，
 *    用户仍可在列表里下载或手动删除。
 */
const props = defineProps<{
  visible: boolean;
  taskId: string;
  taskName?: string;
}>();

const emit = defineEmits<{
  (e: "update:visible", value: boolean): void;
  /** 用户主动要去录像缓存列表页看看（只有点「查看缓存任务」才发）。 */
  (e: "navigate"): void;
}>();

const POLL_INTERVAL = 2000;

const task = ref<RecordCacheTask | null>(null);
const loadError = ref("");
const cancelling = ref(false);
let pollTimer: number | null = null;

const state = computed(() => task.value?.state || "queued");
// ⛔ 两个判断不能混用：`isActive` 决定"要不要继续轮询"（含整理中），
//    `isStoppable` 决定"取消按钮出不出来"（整理中不能取消，后端会回 409）。
const isActive = computed(() => isRecordCacheTaskActive(state.value));
const isStoppable = computed(() => isRecordCacheTaskStoppable(state.value));
const isSucceeded = computed(() => state.value === "succeeded");
const isFailed = computed(() => state.value === "failed");

const progressPercent = computed(() => {
  if (isSucceeded.value) return 100;
  return Math.min(100, Math.round((task.value?.progress || 0) * 100));
});

// ⛔ Arco 的 `a-progress` 收的是 **0~1 的比值**，不是百分数：
//    源码 `barStyle.width = `${percent * 100}%``（@arco-design/web-vue/es/progress/line.js）。
//    实测 `:percent="0.15"` → `width: 15%`，而 `:percent="15"` → `width: 1500%`，
//    被容器裁掉后就是"蓝条永远顶满"；偏偏下面我们自己的文字仍是 7%，
//    于是出现"7% 却满格"的自相矛盾（2026-10-04 线上就是这个问题）。
//    ⇒ **显示用百分数（progressPercent），喂给组件用比值（progressRatio）。**
const progressRatio = computed(() => progressPercent.value / 100);

const statusText = computed(() => {
  switch (state.value) {
    case "queued":
      return "排队中，等待设备通道空闲";
    case "running":
      return "正在从设备拉流并缓存到服务器";
    // ⛔ 整理中不是「缓存中」：录像已经全部拉回来了，剩下的只是后端把分片拼成一个文件
    //    （浏览器原生下载要的是一个完整文件）。显示成"缓存中"会让人以为还在耗流量。
    case "merging":
      return "录像已全部缓存，正在整理成单个文件";
    case "succeeded":
      return "缓存完成，可下载到本地";
    case "failed":
      return task.value?.lastError || "缓存失败";
    case "cancelled":
      return "任务已取消";
    case "expired":
      return "缓存文件已过保留期，已被清理";
    default:
      return "缓存中";
  }
});

const progressDetail = computed(() => {
  if (!task.value) return "";
  const parts = [
    `${formatDuration(task.value.cachedSeconds)} / ${formatDuration(task.value.totalSeconds)}`,
    formatByteSize(task.value.cachedBytes)
  ];
  if (state.value === "running" && task.value.speedBytesPerSec > 0) {
    parts.push(`${formatByteSize(task.value.speedBytesPerSec)}/s`);
  }
  return parts.join(" · ");
});

const fileCount = computed(() => (task.value?.files || []).length);

// ⛔ 轮询必须在组件卸载时停掉：漏下来的定时器会打到已卸载组件的 ref 上，
// 在测试里表现为"上一个用例的请求把下一个用例的计数顶高"。
onUnmounted(() => {
  stopPolling();
});

function stopPolling() {
  if (pollTimer !== null) {
    window.clearTimeout(pollTimer);
    pollTimer = null;
  }
}

function schedulePolling() {
  stopPolling();
  pollTimer = window.setTimeout(() => {
    void refresh();
  }, POLL_INTERVAL);
}

async function refresh() {
  if (!props.taskId) return;
  try {
    const response = await getRecordCacheTask(props.taskId, { showErrorMessage: false });
    task.value = response?.data || null;
    loadError.value = "";
  } catch {
    loadError.value = "任务状态读取失败，稍后自动重试";
  }
  if (isActive.value) schedulePolling();
  else stopPolling();
}

async function cancelTask() {
  if (!props.taskId || cancelling.value) return;
  cancelling.value = true;
  try {
    await cancelRecordCacheTask(props.taskId);
    Message.success("已取消缓存任务");
    // ⛔ 取消成功后**直接关窗**，不要留在原地。
    //    取消之后任务既不能取消、也不再推进，底部那组按钮的显示条件（isStoppable / isActive）
    //    同时变假，于是落到兜底的 `v-else` 分支露出「查看缓存任务」——那个按钮是给
    //    "跑完了 / 失败了"用的，出现在"我刚刚取消"的场景里既突兀，又会莫名把人送去列表页。
    //    台账和已落盘的分片仍然保留（后端 Cancel 会补一个过期时间，到期由保留期清理，
    //    也能在录像缓存列表里手动删除），所以这里只关窗、**不发 navigate**。
    //    2026-10-05 用户明确要求："取消完成之后就直接关掉这个缓存弹窗就行了"。
    stayClosed();
  } catch {
    // 取消失败（如后端已把任务判成终态、超时）时**保持弹窗打开**：
    // 关掉的话用户既不知道失败、也再也点不到那个按钮了。
    Message.error("取消失败");
  } finally {
    cancelling.value = false;
  }
}

/**
 * 转后台：只关掉弹窗，任务继续在服务端跑，用户留在当前页面。
 *
 * 给一句提示是必要的 —— 关窗之后页面上不留任何痕迹，不说一声的话
 * 用户会以为任务没了；顶栏那个「缓存任务」入口也没人知道该去哪找。
 */
function goBackground() {
  stayClosed();
  Message.info("已转后台，可在右上角「缓存任务」查看进度");
}

/** 查看缓存任务：关掉弹窗并把用户送到录像缓存列表页。 */
function goTaskList() {
  stopPolling();
  emit("update:visible", false);
  emit("navigate");
}

function stayClosed() {
  stopPolling();
  emit("update:visible", false);
}

watch(
  () => props.visible,
  value => {
    if (value) {
      void refresh();
    } else {
      stopPolling();
    }
  },
  { immediate: true }
);

watch(
  () => props.taskId,
  () => {
    if (props.visible) void refresh();
  }
);

defineExpose({ refresh });
</script>

<template>
  <!--
    ⛔ `modal-class="uvp-system-dialog"` 不能省：本仓所有弹窗都挂它，暗色下的面板底色 /
       描边 / 阴影 / 页脚按钮全部由 `.uvp-system-dialog .arco-modal-*` 那组全局规则提供
       （见 uvp-ui-language.scss）。漏了它这个弹窗就会用 Arco 自带的灰面板，
       摆在深蓝黑的控制台界面上就是"两套皮肤"（2026-10-05 用户实测反馈）。
  -->
  <a-modal
    :visible="visible"
    :footer="false"
    :title="`缓存到服务器 · ${taskName || '录像段'}`"
    :width="420"
    modal-class="uvp-system-dialog record-cache-progress-dialog"
    unmount-on-close
    @cancel="stayClosed"
  >
    <div class="record-cache-progress" data-testid="record-cache-progress">
      <a-progress
        :percent="progressRatio"
        :status="isFailed ? 'danger' : isSucceeded ? 'success' : 'normal'"
        size="large"
        :show-text="false"
      />
      <div class="record-cache-progress__head">
        <span class="record-cache-progress__percent" data-testid="record-cache-percent">{{ progressPercent }}%</span>
        <span class="record-cache-progress__state" data-testid="record-cache-state">{{ statusText }}</span>
      </div>
      <div v-if="progressDetail" class="record-cache-progress__detail" data-testid="record-cache-detail">
        {{ progressDetail }}
      </div>
      <div v-if="isSucceeded && fileCount > 0" class="record-cache-progress__detail">
        共 {{ fileCount }} 个文件，可在「录像缓存」中下载
      </div>
      <div v-if="loadError" class="record-cache-progress__error">{{ loadError }}</div>
    </div>

    <div class="record-cache-progress__actions">
      <a-button v-if="isStoppable" :loading="cancelling" @click="cancelTask">取消任务</a-button>
      <a-button v-if="isActive" type="primary" data-testid="record-cache-background" @click="goBackground"> 转后台 </a-button>
      <a-button v-else type="primary" data-testid="record-cache-done" @click="goTaskList">查看缓存任务</a-button>
    </div>
  </a-modal>
</template>

<style scoped>
.record-cache-progress {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.record-cache-progress__head {
  display: flex;
  gap: 12px;
  align-items: baseline;
  justify-content: space-between;
}

.record-cache-progress__percent {
  font-size: 20px;
  font-weight: 600;
}

/* ⛔ 次级文字用本仓的 `--uvp-text-tertiary`，不用 Arco 的 `--color-text-3`：
   两套 token 在同一块面板里白亮度不一样，并排看就是"深浅不一的灰"。 */
.record-cache-progress__state {
  flex: 1 1 auto;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
  text-align: right;
}

.record-cache-progress__detail {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}

.record-cache-progress__error {
  font-size: 12px;
  color: var(--uvp-danger);
}

.record-cache-progress__actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  margin-top: 16px;
}
</style>
