<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { AlertTriangle, CircleX, Clock3, Eraser, Loader2 } from "@lucide/vue";
import { formatStorageCard, type DeviceOperationResult, type StorageCard } from "@/api/gb28181";

/**
 * 存储卡格式化二次确认（GB/T 28181-2022 A.2.3.1.13）。
 *
 * ⛔⛔ 三条不能含糊的事实，界面上必须说清楚，否则用户会按错误的心智模型操作：
 *
 *  1. **破坏性且不可撤销** —— 卡上录像被清空，平台侧没有"恢复"这条路。
 *  2. **无应答命令**（9.3.1 d)：目标设备**不发送应答命令**（表 1 序号 12 的应答章节
 *     也写作「（无）」）。所以这里只能显示「已下发」，不能显示「已完成」；
 *     把"已下发"包装成"格式化成功"是**骗用户**，他下一眼就会看到卡还是满的。
 *  3. **结果要自己去问** —— 进度的唯一出口是再查一次 SDCardStatus（A.2.6.16：
 *     Status=formatting + FormatProgress）。所以提交成功后把用户导向
 *     「向设备查询」，而不是留一个"等待中"的转圈。
 *
 * ⛔ 门禁顺序与后端一致：确认（本弹窗）在权限之前。没权限时按钮**禁用并说明原因**，
 *    不是隐藏 —— 用户要知道"这里有个功能，但我不能用"，而不是以为平台没这个能力。
 */
const props = withDefaults(
  defineProps<{
    visible: boolean;
    channelId: number | null;
    /** 目标卡。null 表示还没选中（弹窗会停在说明态，提交不可用）。 */
    card: StorageCard | null;
    /** 设备/通道的展示名，纯粹为了让用户确认"动的是哪台"。 */
    targetLabel?: string;
    /** 通道是否在线 —— 离线时命令发出去也只会等失败。 */
    channelOnline?: boolean;
    /** 是否具备 gb28181:device:format_sd。 */
    canFormat: boolean;
    /** 父组件是否正在跑「下发后的观察窗」（每 6s 一轮查 SDCardStatus）。 */
    watching?: boolean;
    /** 已完成的跟踪轮次 / 总轮次，用来告诉用户平台还在盯。 */
    watchRound?: number;
    watchTotal?: number;
  }>(),
  {
    card: null,
    targetLabel: "",
    channelOnline: true,
    canFormat: false,
    watching: false,
    watchRound: 0,
    watchTotal: 0
  }
);

const emit = defineEmits<{
  "update:visible": [value: boolean];
  /** 命令已成功交给平台（不等设备，因为设备不会回执）。父组件据此开始跟踪进度。 */
  submitted: [operationId: string | undefined];
}>();

const submitPending = ref(false);
const submissionError = ref("");
const localResult = ref<DeviceOperationResult | null>(null);
/** 是否已经提交过（提交成功或结果未知都算），用来切成结果态。 */
const hasSubmitted = ref(false);

let requestVersion = 0;

const cardIndex = computed(() => (props.card ? props.card.cardId : null));
const cardName = computed(() => {
  if (!props.card) return "";
  return props.card.hddName?.trim() || `SD 卡 ${props.card.cardId}`;
});
const displayResult = computed(() => localResult.value);
const resultVisible = computed(() => hasSubmitted.value && Boolean(displayResult.value));

/**
 * ⛔ 用**服务端回来的 `responseRequired`** 判断要不要说"设备不会回执"，而不是在前端写死。
 *    这个字段是协议口径的搬运工：后端哪天改了（比如某个厂商变体要求应答），
 *    界面文案跟着变，不需要有人记得回来改这里。
 *    只有缺失时才按标准的无应答来措辞（存储卡格式化本来就没有应答章节）。
 */
const oneWay = computed(() => displayResult.value?.responseRequired !== true);

// ===== 下发后的进度跟踪 =====

/**
 * 是否**见过**设备报 `formatting`。
 *
 * ⛔⛔ 这是判断"格式化到底有没有真的开始过"的唯一记忆，不能省。提交成功那一刻，父组件手里
 *      的卡还是**上一次查询的旧事实**（几乎是 `ok`）；拿 `status !== "formatting"` 直接
 *      宣布完成，就会把一条刚发出的命令当场报成"已完成"，而卡一动没动。
 *      只有"见过 formatting 又回到非 formatting"才是**设备自己报的**结束。
 */
const sawFormatting = ref(false);
watch(
  () => props.card?.status,
  status => {
    if (String(status || "").toLowerCase() === "formatting") sawFormatting.value = true;
  },
  { immediate: true }
);

const formattingNow = computed(() => String(props.card?.status || "").toLowerCase() === "formatting");
/** 设备报上来的进度（A.2.6.16 的 `FormatProgress`，0-100）。null = 这一轮设备没给这个字段。 */
const progressPercent = computed(() => {
  const value = props.card?.formatProgress;
  if (typeof value !== "number" || Number.isNaN(value)) return null;
  return Math.max(0, Math.min(100, Math.round(value)));
});

/**
 * 四个相位**互斥**，按事实排，不按期望排：
 *   - `formatting` 设备当下就在格式化 —— 唯一能显示进度条的状态；
 *   - `finished`   见过 formatting 又回到非 formatting ⇒ 结束是设备报的，不是我们猜的；
 *   - `waiting`    已下发但还没见过 formatting（可能还没开始，也可能设备就不报）；
 *   - `ended`      观察窗跑完仍没见过 formatting —— 把结论交回用户，不假装还在盯。
 */
const trackPhase = computed<"formatting" | "finished" | "waiting" | "ended">(() => {
  if (formattingNow.value) return "formatting";
  if (sawFormatting.value) return "finished";
  return props.watching ? "waiting" : "ended";
});
/** ⛔ 只有"提交过 + 还看得见那张卡"才显示进度区；否则这一块会变成永远转圈的装饰。 */
const trackVisible = computed(() => hasSubmitted.value && Boolean(props.card));
const trackTitle = computed(
  () =>
    ({
      formatting: "设备正在格式化该卡",
      finished: "设备已报告格式化结束",
      waiting: "命令已下发，等待设备报告",
      ended: "进度跟踪已结束"
    })[trackPhase.value]
);
const trackDetail = computed(() => {
  if (trackPhase.value === "formatting") {
    return "进度来自平台主动查询（A.2.6.16）：这条命令设备不回应答，只有被查询时才会上报状态。";
  }
  if (trackPhase.value === "finished") {
    return `卡当前状态为「${props.card?.status || "未知"}」，可用 ${formatCapacity(props.card?.freeSpaceMb)}，录像空间已释放。`;
  }
  if (trackPhase.value === "waiting") {
    return "平台每隔几秒查一次设备。这台设备若很快就做完，会直接回 ok —— 那种「跳变」是没有中间百分比的。";
  }
  return "本次观察窗里没看到「格式化中」。请用「向设备查询」核对卡的实际状态，别盲目重复下发。";
});
const trackPercentText = computed(() => (progressPercent.value === null ? "—" : `${progressPercent.value}%`));
/** 无进度时也留一小截，让用户看见"有进度条、只是还没数"而不是一条空轨。 */
const progressBarWidth = computed(() => (progressPercent.value === null ? 4 : Math.max(4, progressPercent.value)));
const watchRoundText = computed(() =>
  props.watching && props.watchTotal > 0 ? `平台已查询 ${props.watchRound}/${props.watchTotal} 次` : ""
);

function formatCapacity(mb: number | null | undefined) {
  if (!mb || mb <= 0) return "0 MB";
  if (mb >= 1024 * 1024) return `${(mb / 1024 / 1024).toFixed(1)} TB`;
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`;
  return `${mb} MB`;
}

const unavailableReason = computed(() => {
  if (submitPending.value) return "正在下发格式化命令，请稍候。";
  if (hasSubmitted.value) return "本次格式化命令已下发。";
  if (!props.canFormat) return "当前账号没有存储卡格式化权限。";
  if (!props.channelId) return "该设备下暂无通道，无法下发格式化。";
  if (!props.card) return "未选中存储卡。";
  if (cardIndex.value === null || cardIndex.value < 0) return "存储卡编号不合法。";
  if (props.card.status === "formatting") return "该卡正在格式化中，请等待本次结束。";
  if (!props.channelOnline) return "通道离线，命令发不到设备上。";
  return "";
});
const submitDisabled = computed(() => Boolean(unavailableReason.value) || submitPending.value);

function statusKey(value?: string | null) {
  return String(value || "unknown").toLowerCase();
}

function statusText(value?: string | null) {
  return (
    (
      {
        queued: "命令已排队",
        pending: "命令处理中",
        sent: "命令已下发",
        accepted: "设备已受理",
        rejected: "命令被拒绝",
        cancelled: "命令已取消",
        failed: "下发失败",
        timeout: "结果未知",
        unknown: "结果未知"
      } as Record<string, string>
    )[statusKey(value)] || "结果未知"
  );
}

function statusTone(value?: string | null) {
  const key = statusKey(value);
  if (key === "failed" || key === "rejected" || key === "cancelled") return "danger";
  if (key === "timeout" || key === "unknown") return "warning";
  if (key === "queued" || key === "pending") return "pending";
  if (key === "sent") return "neutral";
  return "neutral";
}

/**
 * 结果卡的正文。
 *
 * ⛔ `sent` 这一支是本功能的**核心措辞**：平台只是把这一帧发出去了。
 *    写成"格式化已完成"会在用户下一眼看到卡还是满的时候变成一次信任崩塌，
 *    而这个动作**已经**把录像删了 —— 说不清楚比说错更糟。
 */
function resultDetail(result: DeviceOperationResult) {
  const key = statusKey(result.status);
  if (key === "sent" || key === "queued" || key === "pending") {
    return oneWay.value
      ? "平台已把格式化命令发给设备。按 GB/T 28181-2022 9.3.1 d)，这条命令设备不回应答，所以「已下发」不等于「已完成」——请用「向设备查询」看格式化进度。"
      : "平台已把格式化命令发给设备，正在等设备回执。";
  }
  if (key === "accepted") return "设备已受理本次格式化，可用「向设备查询」查看进度。";
  if (key === "timeout" || key === "unknown") {
    return result.errorMessage?.trim() || "命令是否送达设备无法确认。请稍后用「向设备查询」核对卡的状态，暂不要重复提交。";
  }
  return result.errorMessage?.trim() || "请在设备维护记录中查看本次操作。";
}

function createIdempotencyKey() {
  if (typeof globalThis.crypto?.randomUUID === "function") return globalThis.crypto.randomUUID();
  return `storage-card-format-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
}

function clearSession() {
  requestVersion += 1;
  submitPending.value = false;
  submissionError.value = "";
  localResult.value = null;
  hasSubmitted.value = false;
  // ⛔ 重置成"当前事实"而不是 false：万一弹窗是在卡已经在格式化时被打开的，
  //    watch 不会重新触发（status 没变），清成 false 会让这个卡永远停在 waiting 相位。
  sawFormatting.value = formattingNow.value;
}

function isActive(version: number, channelId: number | null) {
  return version === requestVersion && props.visible && props.channelId === channelId;
}

function closeDialog() {
  if (submitPending.value) return;
  emit("update:visible", false);
}

function handleModalVisible(nextVisible: boolean) {
  if (nextVisible) {
    emit("update:visible", true);
    return;
  }
  closeDialog();
}

function confirmFormat() {
  const channelId = props.channelId;
  const index = cardIndex.value;
  const version = requestVersion;
  if (submitDisabled.value || channelId === null || index === null) return;

  submitPending.value = true;
  submissionError.value = "";
  let responseReturned = false;
  void (async () => {
    try {
      const response = await formatStorageCard(channelId, {
        cardIndex: index,
        idempotencyKey: createIdempotencyKey()
      });
      if (!isActive(version, channelId)) return;
      responseReturned = true;
      if (response.code !== 0 || !response.data) {
        submissionError.value = response.message || "下发存储卡格式化失败";
        return;
      }
      localResult.value = response.data;
      hasSubmitted.value = true;
      // 立刻通知父组件开始跟踪：无应答命令没有"等回执"这一步，
      // 用户接着看到的就是进度，而不是一个空转的弹窗。
      emit("submitted", response.data.operationId);
    } catch (error: any) {
      if (!isActive(version, channelId)) return;
      if (!responseReturned) {
        // 请求本身有没有到平台是**不知道的**：不能当作失败让用户再点一次，
        // 那可能把一次成功的格式化变成两次。按"结果未知"呈现并明确劝阻重试。
        localResult.value = {
          action: "format_sd",
          status: "unknown",
          responseRequired: false,
          targetScope: "channel",
          errorMessage: "命令是否送达平台无法确认，请稍后用「向设备查询」核对卡的状态，暂不要重复提交。"
        };
        hasSubmitted.value = true;
      } else {
        submissionError.value = error?.message || "下发存储卡格式化失败";
      }
    } finally {
      if (isActive(version, channelId) || !responseReturned) submitPending.value = false;
    }
  })();
}

watch(
  [() => props.visible, () => props.channelId, () => props.card?.cardId],
  ([visible], previous) => {
    const justOpened = visible && previous?.[0] === false;
    // ⛔ 关掉再打开必须是**干净的一次**：上一次的 operationId / 错误留在界面上，
    //    用户会以为"又发了一次"或"上次失败了" —— 而这是个不可撤销的动作。
    if (justOpened || !visible) clearSession();
  },
  { immediate: true }
);

onBeforeUnmount(() => clearSession());
</script>

<template>
  <a-modal
    :visible="visible"
    modal-class="uvp-system-dialog storage-card-format-dialog"
    width="min(480px, calc(100vw - 32px))"
    :footer="false"
    :mask-closable="!submitPending"
    :closable="!submitPending"
    unmount-on-close
    @update:visible="handleModalVisible"
    @cancel="closeDialog"
  >
    <template #title>
      <span class="scf-title"><Eraser :size="17" />格式化存储卡</span>
    </template>

    <div class="scf-body" data-testid="storage-format-body">
      <section class="scf-target-card">
        <div class="scf-target-heading">
          <div class="scf-target-icon"><Eraser :size="20" /></div>
          <div class="scf-target-identity">
            <strong>{{ cardName || "未选中存储卡" }}</strong>
            <span class="mono">{{ props.card?.targetCode || props.targetLabel || "-" }}</span>
          </div>
          <span class="scf-target-index">卡编号 {{ cardIndex === null ? "-" : cardIndex }}</span>
        </div>
      </section>

      <section v-if="resultVisible && displayResult" class="scf-result-card" data-testid="storage-format-result" role="status">
        <div class="scf-result-icon" :class="`tone-${statusTone(displayResult.status)}`">
          <Eraser v-if="statusTone(displayResult.status) === 'neutral'" :size="19" />
          <Clock3 v-else-if="statusTone(displayResult.status) === 'pending'" :size="19" />
          <CircleX v-else :size="19" />
        </div>
        <div class="scf-result-copy">
          <strong>{{ statusText(displayResult.status) }}</strong>
          <span>{{ resultDetail(displayResult) }}</span>
          <span v-if="displayResult.operationId" class="mono">操作编号：{{ displayResult.operationId }}</span>
        </div>
        <!--
                  ⭐ 进度跟踪区：就画在用户点「确认格式化」的这个弹窗里，视线不用移动。
                  ⛔ 百分比**只能**来自"平台再查一次 SDCardStatus"（2022 全文里 SDCardStatus
                     只有「查询 + 应答」这一对，没有 NOTIFY 那一半）⇒ 它天生是**离散**的：
                     每轮查询取一个值，不是平滑动画，也可能几轮才跳一次。
                -->
        <div v-if="trackVisible" class="scf-track" :class="`phase-${trackPhase}`" data-testid="storage-format-track">
          <div class="scf-track-head">
            <strong data-testid="storage-format-track-title">{{ trackTitle }}</strong>
            <span v-if="trackPhase === 'formatting'" class="scf-track-percent" data-testid="storage-format-track-percent">{{
              trackPercentText
            }}</span>
          </div>
          <div
            v-if="trackPhase === 'formatting'"
            class="scf-track-bar"
            role="progressbar"
            :aria-valuemin="0"
            :aria-valuemax="100"
            :aria-valuenow="progressPercent === null ? undefined : progressPercent"
            :aria-valuetext="progressPercent === null ? '设备未报告进度' : `格式化 ${progressPercent}%`"
            data-testid="storage-format-track-bar"
          >
            <i :style="{ width: `${progressBarWidth}%` }" data-testid="storage-format-track-fill"></i>
          </div>
          <p class="scf-track-detail" data-testid="storage-format-track-detail">{{ trackDetail }}</p>
          <span v-if="watchRoundText" class="scf-track-round" data-testid="storage-format-track-round">
            {{ watchRoundText }}
          </span>
        </div>

        <div class="scf-result-actions">
          <!-- ⛔ 关闭只是收起弹窗：观察窗在面板里继续跑，进度不会因为关窗而丢。 -->
          <a-button
            data-testid="storage-format-result-close"
            :disabled="submitPending"
            title="关闭弹窗后平台仍会在后台继续查询进度"
            @click="closeDialog"
            >关闭</a-button
          >
        </div>
      </section>

      <section
        v-else
        class="scf-confirm-card"
        data-testid="storage-format-confirmation"
        role="alertdialog"
        aria-label="确认格式化存储卡"
      >
        <div class="scf-confirm-heading">
          <div class="scf-confirm-icon"><AlertTriangle :size="20" /></div>
          <div><strong>确认格式化这张卡？</strong><span>这是一项不可撤销的破坏性操作</span></div>
        </div>
        <p class="scf-impact">卡上<strong>全部录像会被清空且无法恢复</strong>；格式化期间该卡不可写入，录像与回放会中断。</p>
        <p v-if="unavailableReason" class="scf-hint warning" data-testid="storage-format-blocked" role="alert">
          <CircleX :size="14" />{{ unavailableReason }}
        </p>
        <p v-else class="scf-hint">
          <AlertTriangle :size="14" />存储卡格式化是<strong>无应答命令</strong>（GB/T 28181-2022 9.3.1
          d)），设备不会回执——「已下发」不代表「已完成」。
        </p>
        <p v-if="submissionError" class="scf-error" data-testid="storage-format-error" role="alert">
          <CircleX :size="14" />{{ submissionError }}
        </p>
        <div class="scf-confirm-actions">
          <a-button data-testid="storage-format-cancel" :disabled="submitPending" @click="closeDialog">取消</a-button>
          <a-button
            data-testid="storage-format-confirm"
            status="danger"
            type="primary"
            :disabled="submitDisabled"
            :loading="submitPending"
            @click="confirmFormat"
          >
            <template #icon><Loader2 v-if="submitPending" :size="14" class="spin" /><Eraser v-else :size="14" /></template>
            {{ submitPending ? "正在下发" : "确认格式化" }}
          </a-button>
        </div>
      </section>
    </div>
  </a-modal>
</template>

<style scoped>
.scf-body {
  display: grid;
  gap: 12px;
  color: var(--uvp-text-primary);
}
.scf-title {
  display: inline-flex;
  gap: 7px;
  align-items: center;
  font-weight: 650;
  color: var(--uvp-text-primary);
}
.scf-target-card,
.scf-confirm-card,
.scf-result-card {
  padding: 15px;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 12px;
}
.scf-target-heading,
.scf-confirm-heading {
  display: flex;
  gap: 10px;
  align-items: center;
}
.scf-target-heading {
  min-width: 0;
}
.scf-target-icon,
.scf-confirm-icon,
.scf-result-icon {
  display: grid;
  flex: 0 0 auto;
  place-items: center;
  width: 36px;
  height: 36px;
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
  border-radius: 10px;
}
.scf-target-identity,
.scf-confirm-heading > div:last-child,
.scf-result-copy {
  display: grid;
  gap: 3px;
  min-width: 0;
}
.scf-target-identity strong {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.scf-target-identity span,
.scf-confirm-heading span,
.scf-impact,
.scf-hint,
.scf-result-copy span {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.scf-target-index {
  padding: 4px 9px;
  margin-left: auto;
  font-size: 12px;
  color: var(--uvp-text-secondary);
  white-space: nowrap;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 999px;
}
.scf-confirm-card,
.scf-result-card {
  background: var(--uvp-list-toolbar-bg);
}
.scf-confirm-heading {
  color: var(--uvp-warning);
}
.scf-confirm-heading strong,
.scf-result-copy strong {
  font-size: 15px;
  color: var(--uvp-text-primary);
}
.scf-confirm-heading span {
  display: block;
  margin-top: 2px;
}
.scf-impact {
  padding: 11px 12px;
  margin: 14px 0 0;
  line-height: 1.55;
  color: var(--uvp-text-secondary);
  background: var(--uvp-warning-soft);
  border: 1px solid var(--uvp-warning-border);
  border-radius: 9px;
}
.scf-impact strong {
  color: var(--uvp-danger);
}
.scf-hint,
.scf-error {
  display: flex;
  gap: 6px;
  align-items: flex-start;
  margin: 10px 0 0;
  line-height: 1.5;
}
.scf-hint strong {
  color: var(--uvp-text-secondary);
}
.scf-hint.warning {
  color: var(--uvp-warning);
}
.scf-error {
  padding: 8px 10px;
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border: 1px solid var(--uvp-danger-border);
  border-radius: 8px;
}
.scf-confirm-actions,
.scf-result-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  margin-top: 17px;
}
.scf-confirm-actions :deep(.arco-btn[disabled]),
.scf-confirm-actions :deep(.arco-btn-disabled),
.scf-result-actions :deep(.arco-btn[disabled]),
.scf-result-actions :deep(.arco-btn-disabled) {
  color: var(--uvp-text-tertiary) !important;
  cursor: not-allowed;
  background: var(--uvp-panel-bg) !important;
  border-color: var(--uvp-panel-border) !important;
  box-shadow: none !important;
  opacity: 0.58;
}
.scf-result-card {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 10px;
  align-items: start;
}
.scf-result-icon.tone-neutral {
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent);
}
.scf-result-icon.tone-pending,
.scf-result-icon.tone-warning {
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
}
.scf-result-icon.tone-danger {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
}
.scf-result-copy span {
  line-height: 1.5;
}
.scf-result-copy .mono {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.scf-result-actions {
  grid-column: 1 / -1;
}

/* 进度跟踪区：跨满整行。⛔ 只用仓库里**确实定义过**的 token —— 引一个不存在的 --uvp-* 会让整块隐形 */
.scf-track {
  display: grid;
  grid-column: 1 / -1;
  gap: 7px;
  padding: 11px 12px;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
}
.scf-track-head {
  display: flex;
  gap: 10px;
  align-items: baseline;
  justify-content: space-between;
}
.scf-track-head strong {
  font-size: 13px;
  color: var(--uvp-text-primary);
}
.scf-track.phase-finished .scf-track-head strong {
  color: var(--uvp-brand-cyan);
}
.scf-track.phase-ended .scf-track-head strong {
  color: var(--uvp-warning);
}
.scf-track-percent {
  font-size: 17px;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
  color: var(--uvp-brand-cyan);
}
.scf-track-bar {
  position: relative;
  height: 7px;
  overflow: hidden;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 999px;
}
.scf-track-bar > i {
  display: block;
  height: 100%;
  background: var(--uvp-brand-cyan);
  border-radius: 999px;
  transition: width 0.4s ease;
}
.scf-track-detail {
  margin: 0;
  font-size: 12px;
  line-height: 1.5;
  color: var(--uvp-text-tertiary);
}
.scf-track-round {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.spin {
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

@media (width <= 480px) {
  .scf-target-heading {
    flex-wrap: wrap;
    align-items: flex-start;
  }
  .scf-target-index {
    margin-left: 46px;
  }
  .scf-confirm-actions,
  .scf-result-actions {
    flex-wrap: wrap;
  }
}
</style>
