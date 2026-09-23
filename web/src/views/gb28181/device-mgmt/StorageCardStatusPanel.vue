<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { Eraser, HardDrive, RefreshCcw } from "@lucide/vue";
import { getChannelStorageCards, getPtzOperation, type PTZResourceFreshness, type StorageCard } from "@/api/gb28181";
import FactChannelPicker from "./FactChannelPicker.vue";
import StorageCardFormatDialog from "./StorageCardFormatDialog.vue";
import type { FactChannelOption } from "./deviceFactChannel";

/**
 * 存储卡状态（GB/T 28181-2022 A.2.4.14 / A.2.6.16）—— 从播放控制台搬到设备详情抽屉。
 *
 * ⛔ 与「设备状态」分开维护：它们是不同的协议命令、不同的查询节拍，
 *    合成一个 pending 会让「查卡」把「查设备状态」的按钮一起转圈。
 * ⛔ `refresh=true` 在服务端只是**发起**一次 SDCardStatus 查询（SIP 应答异步），
 *    拿回来的还是上一次的事实 ⇒ 必须轮询 operation 到终态再重读列表。
 * ⭐ 空列表是**合法结果**（设备可以不装卡，标准的 SumNum=0 且不带列表），
 *    所以空的时候说「设备未安装存储卡」，而不是报错。
 *
 * ⭐ 2026-09-20 新增**格式化**入口（A.2.3.1.13）。它与本页其余「读」的部分有一条
 *    本质区别：格式化是**无应答命令**（9.3.1 d)，表 1 序号 12 的应答章节写作「（无）」）
 *    ⇒ 「已下发」不等于「已完成」，而进度的唯一出口是**再查一次 SDCardStatus**。
 *    所以下发成功之后这里要主动**跟踪**一段时间的查询（见 FORMAT_WATCH_*），
 *    否则用户点完只会看到"卡还是满的"，然后以为没生效、再点一次 —— 而它不可撤销。
 */
const props = defineProps<{
  channelId: number | null;
  channelOptions: FactChannelOption[];
  /** 本页当前是否显示 —— 只在该页首次显示时读一次缓存。 */
  active: boolean;
  /** 是否具备查看权限（gb28181:ptz:view）。 */
  canView: boolean;
  /**
   * 是否具备**格式化**权限（gb28181:device:format_sd）。
   * ⛔ 与 canView 是两个码：能给"看卡状态"不等于能给"把卡抹掉"。
   *    缺权限时按钮**禁用并说明原因**，不是隐藏（隐藏会让用户以为平台没这功能）。
   */
  canFormat?: boolean;
  /** 设备/通道展示名，只用于确认弹窗里指明"动的是哪台"。 */
  deviceLabel?: string;
  channelsLoading?: boolean;
}>();

const emit = defineEmits<{ (event: "update:channelId", value: number | null): void }>();

const cards = ref<StorageCard[]>([]);
const freshness = ref<PTZResourceFreshness>("unknown");
const error = ref("");
const pending = ref(false);
/** 是否成功读到过一份结果：用来区分「还没查过」与「设备确实没有卡」。 */
const loaded = ref(false);

const STORAGE_CARD_POLL_DELAYS_MS = [300, 600, 1200, 2000, 3000, 5000];
let pollTimer: number | null = null;
let generation = 0;

/**
 * 格式化下发后的**进度跟踪**参数。
 *
 * ⛔ 为什么必须自己跟踪：格式化是无应答命令，设备不会回执；而 A.2.6.16 的
 *    `Status=formatting` + `FormatProgress` **只在被查询时**才上报（2022 全文中
 *    SDCardStatus 只有"查询 + 应答"这一对，没有 NOTIFY/推送那一半）。
 *    也就是说，不主动查就永远看不到进度。
 *
 * ⭐ 为什么是**有限次**：这不是"实时进度条"，而是"下发后的一段观察窗"。
 *    每轮都是一次真实的 SIP MESSAGE，无限轮询等于拿设备的 CPU 换一个像素。
 *    8 轮 × 6s ≈ 50 秒足够覆盖绝大多数卡的格式化；跑完还没结束就把结论交回用户
 *    （提示继续手动查询），而不是假装还在盯着。
 */
const FORMAT_WATCH_ROUNDS = 8;
const FORMAT_WATCH_DELAY_MS = 6000;
let formatWatchTimer: number | null = null;
const formatWatchRound = ref(0);
const formatWatching = ref(false);

const formatVisible = ref(false);
/**
 * ⛔ 这里存的是**卡编号**，不是卡对象快照。
 *    弹窗要回答的是"这张卡**此刻**怎么样"：存快照的话，父组件每轮查询拿到的新进度
 *    永远传不进已经打开的弹窗，进度条会停在打开那一刻不动。
 */
const formatCardId = ref<number | null>(null);
/** 卡列表里查不到时的兜底（设备把卡摘了 / 列表被清理），免得弹窗突然变成"未选中存储卡"。 */
const formatSnapshot = ref<StorageCard | null>(null);
const formatTarget = computed(() => {
  if (formatCardId.value === null) return null;
  return cards.value.find(card => card.cardId === formatCardId.value) ?? formatSnapshot.value;
});
watch(formatTarget, card => {
  if (card) formatSnapshot.value = card;
});

function isStale(channelId: number, currentGeneration: number) {
  return currentGeneration !== generation || props.channelId !== channelId;
}

function clearPolling(invalidate = true) {
  if (pollTimer !== null) window.clearTimeout(pollTimer);
  pollTimer = null;
  if (invalidate) generation += 1;
  pending.value = false;
}

function resetCards() {
  cards.value = [];
  freshness.value = "unknown";
  error.value = "";
  loaded.value = false;
}

function operationSettled(status: string | undefined) {
  return ["accepted", "rejected", "timeout", "unknown", "cancelled"].includes(String(status));
}

/**
 * 读取/发起存储卡状态查询。
 *
 * ⛔ 不带 refresh 时是「静默重读」（打开本页 / 轮询收尾），不点亮按钮转圈 ——
 *    否则切个通道就闪一下「查询中」，看起来像用户在等一件没发生的事。
 */
async function load(channelId = props.channelId, refresh = false) {
  if (!props.canView || !channelId) return;
  clearPolling();
  const currentGeneration = generation;
  if (refresh) pending.value = true;
  error.value = "";
  try {
    const response = await getChannelStorageCards(channelId, refresh);
    if (isStale(channelId, currentGeneration)) return;
    if (response.code !== 0 || !response.data) throw new Error(response.message || "查询存储卡状态失败");
    cards.value = response.data.list || [];
    freshness.value = response.data.freshness || "unknown";
    loaded.value = true;
    if (response.data.refreshError) error.value = response.data.refreshError;
    const operationId = response.data.refreshOperationId;
    if (refresh && operationId) {
      schedulePoll(channelId, operationId, 0, currentGeneration);
      return; // pending 保持 true，等轮询收尾时再熄灭
    }
  } catch (loadError: any) {
    if (isStale(channelId, currentGeneration)) return;
    error.value = loadError?.message || "查询存储卡状态失败";
  }
  pending.value = false;
}

/** 轮询到 operation 终态后**重读一次列表** —— 这才是「刚问出来的卡事实」。 */
function schedulePoll(channelId: number, operationId: string, attempt: number, currentGeneration: number) {
  const delay = STORAGE_CARD_POLL_DELAYS_MS[Math.min(attempt, STORAGE_CARD_POLL_DELAYS_MS.length - 1)];
  pollTimer = window.setTimeout(async () => {
    pollTimer = null;
    if (isStale(channelId, currentGeneration)) return;
    let settled = false;
    try {
      const response = await getPtzOperation(channelId, operationId);
      settled = operationSettled(response.data?.status);
    } catch {
      // 读 operation 失败按「未终态」处理，继续退避重试；
      // 次数用尽后无论如何收尾一次，不让按钮永远转圈。
    }
    if (isStale(channelId, currentGeneration)) return;
    if (settled || attempt >= STORAGE_CARD_POLL_DELAYS_MS.length - 1) {
      await load(channelId, false);
      // ⛔ 只在**重读完列表之后**再决定要不要继续跟踪：那一步才拿到"设备刚报上来的
      //    卡状态"，在此之前 cards 里还是上一轮的事实，按它判断会过早收工。
      continueFormatWatch(channelId);
      return;
    }
    schedulePoll(channelId, operationId, attempt + 1, currentGeneration);
  }, delay);
}

// ===== 格式化后的进度跟踪 =====

function stopFormatWatch() {
  if (formatWatchTimer !== null) window.clearTimeout(formatWatchTimer);
  formatWatchTimer = null;
  formatWatchRound.value = 0;
  formatWatching.value = false;
}

function hasFormattingCard() {
  return cards.value.some(card => card.status === "formatting");
}

/**
 * 判断是否再跟一轮。**只**在"重读列表之后"调用（见 schedulePoll 的终态分支）。
 *
 * 结束条件三条，任一命中就收工：已被用户停掉 / 轮次用尽 / 已经没有卡处于 formatting。
 * 最后一条是"格式化真的结束了"的正常出口 —— 它比轮次上限更早命中。
 */
function continueFormatWatch(channelId: number) {
  if (!formatWatching.value) return;
  if (formatWatchRound.value >= FORMAT_WATCH_ROUNDS || !hasFormattingCard()) {
    stopFormatWatch();
    return;
  }
  formatWatchRound.value += 1;
  const currentGeneration = generation;
  formatWatchTimer = window.setTimeout(() => {
    formatWatchTimer = null;
    if (isStale(channelId, currentGeneration)) {
      stopFormatWatch();
      return;
    }
    // 这一轮自己会在 schedulePoll 的终态分支里再回到 continueFormatWatch。
    void load(channelId, true);
  }, FORMAT_WATCH_DELAY_MS);
}

/** 手动查询会打断自动跟踪：两条路同时向设备发 SDCardStatus 只会打架。 */
function refreshManually() {
  stopFormatWatch();
  void load(props.channelId, true);
}

function openFormat(card: StorageCard) {
  if (!canFormatCard(card)) return;
  formatCardId.value = card.cardId;
  formatSnapshot.value = card;
  formatVisible.value = true;
}

/**
 * 下发成功后的第一跳：立刻查一次设备（用户点完按钮最想看的就是"动了没有"），
 * 后面的轮次由 schedulePoll → continueFormatWatch 接力。
 */
function onFormatSubmitted() {
  stopFormatWatch();
  formatWatching.value = true;
  formatWatchRound.value = 0;
  void load(props.channelId, true);
}

function canFormatCard(card: StorageCard) {
  return Boolean(props.canFormat) && hasChannel.value && !pending.value && card.status !== "formatting";
}

function formatButtonTitle(card: StorageCard) {
  if (!props.canFormat) return "当前账号没有存储卡格式化权限";
  if (!hasChannel.value) return "该设备下暂无通道，无法下发";
  if (card.status === "formatting") return "该卡正在格式化中";
  // ⛔ 离线不在 canFormatCard 里拦，只在这里说明：按钮点了还能进弹窗，
  //    由弹窗把「为什么不能下发」讲清楚（tooltip 在触屏和截图里等于不存在）。
  if (!channelOnline.value) return "通道离线，命令发不到设备上";
  return "格式化该存储卡（会清空卡上录像，且无法恢复）";
}

watch(
  [() => props.channelId, () => props.active, () => props.canView],
  ([channelId, active, canView], previous) => {
    const previousChannelId = previous?.[0] ?? null;
    if (!active) {
      // 切走这一页就停掉所有后台活动：进度跟踪每一轮、以及正在进行的那次查询轮询，
      // 对着一块看不见的面板反复问设备是最没必要的那种"后台活动"。
      clearPolling();
      stopFormatWatch();
      return;
    }
    if (!canView || !channelId) return;
    if (channelId === previousChannelId && loaded.value) return;
    clearPolling();
    stopFormatWatch();
    // 换通道时把确认弹窗也收掉：弹窗里显示的卡属于上一个通道，
    // 留着它会让"确认格式化"动到一张已经不在眼前的卡。
    formatVisible.value = false;
    formatCardId.value = null;
    formatSnapshot.value = null;
    resetCards();
    void load(channelId, false);
  },
  { immediate: true }
);

onBeforeUnmount(() => {
  clearPolling();
  stopFormatWatch();
});

const hasChannel = computed(() => typeof props.channelId === "number" && props.channelId > 0);
/**
 * 当前通道是否在线。
 *
 * ⭐ 查不到时按**在线**处理：`channelOptions` 里没有这一条只说明"还没加载到"，
 *    把"不知道"当成"离线"会禁掉一个本来能用的按钮；而离线通道真发出去也只是失败一次，
 *    失败是可见的，误禁是不可见的。
 */
const channelOnline = computed(() => props.channelOptions.find(option => option.value === props.channelId)?.online ?? true);
const emptyText = computed(() => {
  if (pending.value) return "正在查询存储卡…";
  if (!loaded.value) return "尚未读取存储卡状态。";
  return "设备未安装存储卡。";
});
const freshnessText = computed(() => {
  if (error.value) return "查询失败";
  // ⛔ 跟踪态排在 pending 之前：跟踪的每一轮本来就是一次查询，
  //    先报「正在查询存储卡」的话，整段观察窗里用户只会看到它在反复查询，
  //    看不出"这是在盯格式化进度"。
  if (formatWatching.value) return `正在跟踪格式化进度（${formatWatchRound.value}/${FORMAT_WATCH_ROUNDS}）`;
  if (pending.value) return "正在查询存储卡";
  if (!loaded.value) return "尚未读取存储卡状态";
  if (freshness.value === "fresh") return "存储卡状态已同步";
  if (freshness.value === "stale") return "存储卡状态已过期";
  return "存储卡状态未知";
});

function stateText(state: string) {
  switch (state) {
    case "ok":
      return "正常";
    case "formatting":
      return "格式化中";
    case "unformatted":
      return "未格式化";
    case "idle":
      return "空闲";
    case "error":
      return "异常";
    default:
      return "未知";
  }
}

/** 设备有没有报容量 —— 没报就不画进度条（一条 0% 的空条会被误读成「整块都还空着」）。 */
function capacityKnown(card: StorageCard) {
  return Number.isFinite(card.capacityMb) && card.capacityMb > 0;
}

/** 已用空间 = 容量 − 可用，且夹在 [0, 容量] 内。 */
function usedSpaceMb(card: StorageCard) {
  if (!capacityKnown(card)) return 0;
  const free = Math.max(0, Math.min(card.freeSpaceMb, card.capacityMb));
  return card.capacityMb - free;
}

/** 可用空间由**夹紧后的已用**反推 —— 两个数字必须加起来正好等于总容量，floor/ceil 不同步。 */
function freeSpaceMb(card: StorageCard) {
  return capacityKnown(card) ? card.capacityMb - usedSpaceMb(card) : 0;
}

function usedPercent(card: StorageCard) {
  if (!capacityKnown(card)) return 0;
  return Math.max(0, Math.min(100, Math.round((usedSpaceMb(card) / card.capacityMb) * 100)));
}

/** 两个百分比必须互补：各自独立四舍五入会出现 27% + 74% = 101%。 */
function freePercent(card: StorageCard) {
  return capacityKnown(card) ? 100 - usedPercent(card) : 0;
}

/**
 * 已用空间的档位 —— 决定这一段的颜色。
 * ⛔ 卡自身状态（异常 / 格式化中）优先于容量阈值：设备说卡有毛病，比「用得满」更该被看见。
 */
type UsageLevel = "ok" | "warning" | "danger";
function usageLevel(card: StorageCard): UsageLevel {
  if (card.status === "error") return "danger";
  if (card.status === "formatting") return "warning";
  const percent = usedPercent(card);
  if (percent >= 90) return "danger";
  if (percent >= 75) return "warning";
  return "ok";
}

/**
 * 这张卡此刻正在格式化 —— 容量那一段要**整段换掉**，换成格式化进度。
 *
 * ⛔ 格式化期间「已用 / 可用」两个读数**都不成立**：真机实测（海康，2026-09-20）格式化全程
 *    `FreeSpace=0`（文件系统正在重建，剩余量本就无从谈起）。照直画出来就是一条 **100% 满**的
 *    红条，而它比旁边任何文字都抢眼 —— 用户会把它读成「格式化卡住了」或「盘还是满的」，
 *    这正是本轮用户报「看不到进度百分比」的**第二个误读源**。
 */
function isFormattingCard(card: StorageCard) {
  return card.status === "formatting";
}

/**
 * 格式化进度条的宽度；设备**没报**进度时返回 `null`。
 *
 * ⛔ 不报 = `null`，绝不回落成 `0`：0% 画出来是一条空条，读起来是「进度 0%，明显没动」，
 *    而事实是「不知道进度」。这两种情况在界面上的形态必须不同（空条 vs 不确定态脉冲条）。
 */
function formatBarWidth(card: StorageCard): number | null {
  const progress = card.formatProgress;
  if (progress === null || progress === undefined) return null;
  return Math.max(0, Math.min(100, Math.round(progress)));
}

/** 进度条的无障碍读法：不确定态要说「进度未知」，而不是留一句空的或谎报 0%。 */
function formatBarAriaText(card: StorageCard) {
  const width = formatBarWidth(card);
  return width === null ? "正在格式化，设备未上报进度" : `正在格式化 ${width}%`;
}

function formatCapacity(mb: number | null | undefined) {
  if (!mb || mb <= 0) return "0 MB";
  if (mb >= 1024 * 1024) return `${(mb / 1024 / 1024).toFixed(1)} TB`;
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`;
  return `${mb} MB`;
}
</script>

<template>
  <div class="storage-panel">
    <FactChannelPicker
      :options="props.channelOptions"
      :model-value="props.channelId"
      :loading="props.channelsLoading"
      @update:model-value="value => emit('update:channelId', value)"
    />

    <p v-if="props.channelOptions.length === 0" class="storage-empty">该设备下暂无通道，无法查询存储卡状态。</p>

    <section class="storage-group" data-testid="storage-card-status">
      <header class="storage-group-head">
        <span class="storage-group-title"><HardDrive :size="14" />存储卡状态</span>
        <span class="storage-group-status" :class="{ pending: pending, error: !!error }" data-testid="storage-freshness">
          {{ freshnessText }}
        </span>
      </header>

      <div v-if="cards.length" class="storage-list" data-testid="storage-card-list">
        <div
          v-for="card in cards"
          :key="card.cardId"
          class="storage-item"
          :data-status="card.status"
          :data-level="usageLevel(card)"
          :data-testid="`storage-card-${card.cardId}`"
        >
          <div class="storage-item-line">
            <span class="storage-item-name">{{ card.hddName || `SD 卡 ${card.cardId}` }}</span>
            <em class="storage-item-state" :class="`state-${card.status}`">{{ stateText(card.status) }}</em>
            <!--
              ⛔ 无权限时**禁用而不是隐藏**：隐藏会让用户以为平台没这个能力，
                禁用 + title 说明原因才说得清"这里有个功能，但这个账号不能用"。
              ⛔ 破坏性动作不在卡片本体上直接执行，走弹窗二次确认
                （StorageCardFormatDialog：确认 → 权限 → 参数，顺序与后端一致）。
            -->
            <button
              class="storage-format-btn"
              type="button"
              :disabled="!canFormatCard(card)"
              :title="formatButtonTitle(card)"
              :data-testid="`storage-format-${card.cardId}`"
              @click="openFormat(card)"
            >
              <Eraser :size="12" />格式化
            </button>
          </div>

          <template v-if="capacityKnown(card)">
            <!--
              ⛔ 正在格式化的卡：容量那一段**整段不画**，换成格式化进度本身。
                理由见 isFormattingCard 的注释 —— 这段窗口里设备报 `FreeSpace=0`，
                「已用/可用」照直画出来是 100% 满的红条，会被读成「格式化卡住了」。
            -->
            <div
              v-if="isFormattingCard(card)"
              class="storage-item-bar storage-item-bar-format"
              :class="{ 'is-indeterminate': formatBarWidth(card) === null }"
              role="progressbar"
              aria-valuemin="0"
              aria-valuemax="100"
              :aria-valuenow="formatBarWidth(card) ?? undefined"
              :aria-valuetext="formatBarAriaText(card)"
              :title="formatBarAriaText(card)"
              data-testid="storage-format-bar"
            >
              <i
                class="storage-bar-format"
                :style="formatBarWidth(card) === null ? undefined : { width: `${formatBarWidth(card)}%` }"
                data-testid="storage-format-bar-fill"
              ></i>
            </div>
            <div v-if="isFormattingCard(card)" class="storage-item-meta">
              <span class="storage-legend storage-legend-format" data-testid="storage-legend-format">
                <em class="storage-dot dot-format"></em>{{ formatBarAriaText(card) }}
              </span>
              <span class="storage-legend storage-legend-total">总容量 {{ formatCapacity(card.capacityMb) }}</span>
            </div>

            <!--
              两段式：实心段 = 已用，底轨 = 可用。
              ⛔ 底轨颜色是这个控件能不能读懂的关键，别改回 --uvp-border 那个 token
                （它在全仓从未定义 ⇒ 背景会失效成透明 ⇒ 底轨跟卡片底色一样白）。
            -->
            <template v-else>
              <div
                class="storage-item-bar"
                role="progressbar"
                :aria-valuemin="0"
                :aria-valuemax="100"
                :aria-valuenow="usedPercent(card)"
                :aria-valuetext="`已用 ${usedPercent(card)}%，可用 ${freePercent(card)}%`"
                :title="`已用 ${usedPercent(card)}%（${formatCapacity(usedSpaceMb(card))}），可用 ${freePercent(card)}%（${formatCapacity(freeSpaceMb(card))}）`"
                data-testid="storage-bar"
              >
                <i class="storage-bar-used" :style="{ width: `${usedPercent(card)}%` }" data-testid="storage-bar-used"></i>
              </div>
              <div class="storage-item-meta">
                <span class="storage-legend" data-testid="storage-legend-used">
                  <em class="storage-dot dot-used"></em>已用 {{ formatCapacity(usedSpaceMb(card)) }}（{{ usedPercent(card) }}%）
                </span>
                <span class="storage-legend storage-legend-total">总容量 {{ formatCapacity(card.capacityMb) }}</span>
                <span class="storage-legend" data-testid="storage-legend-free">
                  <em class="storage-dot dot-free"></em>可用 {{ formatCapacity(freeSpaceMb(card)) }}（{{ freePercent(card) }}%）
                </span>
              </div>
              <div v-if="usageLevel(card) === 'danger'" class="storage-item-foot">
                <span class="storage-item-alert" data-testid="storage-space-alert">剩余空间不足</span>
              </div>
            </template>
          </template>
          <p v-else class="storage-empty" data-testid="storage-capacity-unknown">设备未上报容量，无法判断已用空间。</p>
        </div>
      </div>
      <p v-else class="storage-empty" data-testid="storage-card-empty">{{ emptyText }}</p>

      <p v-if="error" class="storage-error" data-testid="storage-error">{{ error }}</p>

      <div class="storage-actions">
        <!--
          ⛔ 跟踪期间这个按钮**不能禁用**：观察窗的每一轮之间 pending 基本都是 true，
            禁用等于让用户在长达几十秒里既看不到进度、也停不掉跟踪、也没法自己查一次。
            跟踪态下它的语义变成「立即查询一次」（并打断自动跟踪），文案跟着变。
        -->
        <a-button
          size="small"
          :loading="pending && !formatWatching"
          :disabled="!props.canView || !hasChannel || props.channelOptions.length === 0 || (pending && !formatWatching)"
          :title="!hasChannel ? '该设备下暂无通道，无法查询' : '向设备查询存储卡状态（A.2.4.14）'"
          data-testid="storage-refresh"
          @click="refreshManually"
        >
          <template #icon><RefreshCcw :size="13" /></template>
          {{ formatWatching ? "立即查询一次" : pending ? "查询中" : "向设备查询" }}
        </a-button>
        <span class="storage-actions-hint">
          {{
            formatWatching
              ? "正在自动跟踪格式化进度（每轮都是一次真实查询），手动查询会打断它"
              : "优先读平台缓存；「向设备查询」才会下发 SDCardStatus 报文"
          }}
        </span>
      </div>
    </section>

    <!--
      ⛔ 弹窗放在 section **外面**：它在卡片之外独立存在，进去的是哪张卡由
        formatTarget 决定；放进 section 会被 storage-group 的 gap/padding 影响定位。
    -->
    <StorageCardFormatDialog
      v-model:visible="formatVisible"
      :channel-id="props.channelId"
      :card="formatTarget"
      :target-label="props.deviceLabel || ''"
      :channel-online="channelOnline"
      :can-format="Boolean(props.canFormat)"
      :watching="formatWatching"
      :watch-round="formatWatchRound"
      :watch-total="FORMAT_WATCH_ROUNDS"
      @submitted="onFormatSubmitted"
    />
  </div>
</template>

<style scoped>
.storage-panel {
  display: grid;
  gap: 12px;
}
.storage-group {
  display: grid;
  gap: 10px;
  padding: 12px 14px 14px;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 12px;
}
.storage-group-head {
  display: flex;
  gap: 10px;
  align-items: center;
  justify-content: space-between;
}
.storage-group-title {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  font-size: 12px;
  font-weight: 620;
  color: var(--uvp-text-secondary);
}
.storage-group-status {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.storage-group-status.pending {
  color: var(--uvp-warning);
}
.storage-group-status.error {
  color: var(--uvp-danger);
}
.storage-list {
  display: grid;
  gap: 12px;
}
.storage-item {
  display: grid;
  gap: 6px;
}
.storage-item-line {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
}
.storage-item-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 13px;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.storage-item-state {
  flex: none;
  font-size: 11.5px;
  font-style: normal;
  color: var(--uvp-text-tertiary);
}
.storage-item-state.state-ok {
  color: var(--uvp-brand-cyan);
}
.storage-item-state.state-formatting,
.storage-item-state.state-idle {
  color: var(--uvp-warning);
}
.storage-item-state.state-error {
  color: var(--uvp-danger);
}

/*
 * 卡片行里的「格式化」——破坏性动作，所以刻意**不做成主按钮**：
 * 它平时只是一个安静的次级入口，重量感留给弹窗里的「确认格式化」。
 * 无权限 / 卡正在格式化 / 通道离线时禁用，理由走 title。
 */
.storage-format-btn {
  display: inline-flex;
  flex: none;
  gap: 4px;
  align-items: center;
  padding: 3px 9px;
  font-size: 11.5px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: transparent;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 7px;
  transition:
    border-color 0.15s ease,
    color 0.15s ease;
}
.storage-format-btn:hover:not(:disabled) {
  color: var(--uvp-danger);
  border-color: var(--uvp-danger-border);
}
.storage-format-btn:disabled {
  color: var(--uvp-text-tertiary);
  cursor: not-allowed;
  opacity: 0.6;
}

/*
 * ⛔ 底轨 = 「可用空间」，它必须有**可见颜色**，这是这条进度条能不能读懂的关键。
 *   原来这里的 background 引用的是 --uvp-border 这个 token —— 而它**全仓从未定义过**
 *   （引用 13 处、定义 0 处）。background 引用未定义变量会在计算值阶段失效并回退成
 *   transparent ⇒ 底轨跟卡片底色一样白 ⇒ 整条只剩一段青色，「还剩多少空间」完全看不出来。
 *   改用 --uvp-meter-track（亮 #e2e8f0 / 暗 #33475c，两套主题分别定过）：
 *   实测过 --uvp-panel-border 在亮色下够用，但暗色下 Δ亮度只有 2%、几乎看不见，
 *   而底轨是大面积色块，弱一点点就整段读不出来，所以给它一个专用 token。
 *   （相邻那些把 --uvp-border 用在 border 上的地方之所以没人发现，是因为边框色失效后
 *     回退成 currentColor，线还在、只是跟着文字颜色，看起来「差不多」。）
 *   改动此处请一并跑 StorageCardStatusPanel.test.ts / deviceFactPanelTokens.test.ts。
 */
.storage-item-bar {
  position: relative;
  height: 8px;
  overflow: hidden;
  background: var(--uvp-meter-track);
  border-radius: 4px;
}

/* 实心段 = 已用。宽度就是已用占比，颜色由 data-level 决定。 */
.storage-bar-used {
  display: block;
  height: 100%;
  background: var(--uvp-brand-cyan);
  border-radius: 4px;
  transition: width 0.25s ease;
}
.storage-item[data-level="warning"] .storage-bar-used {
  background: var(--uvp-warning);
}
.storage-item[data-level="danger"] .storage-bar-used {
  background: var(--uvp-danger);
}

/*
 * 格式化进度条 —— 与上面那条「已用空间条」共用底轨，但**填充段另起一个 class**：
 * ⛔ 不复用 .storage-bar-used：那段被 [data-level] 的三条规则按「已用空间档位」改色，
 *    而格式化期间 level 恰好是 warning ⇒ 填充段会变成黄色，和「空间快满了」的告警态
 *    长得一模一样 —— 一条**动作进度**被读成一条**容量告警**，正好是要避免的那种误读。
 * 配色取 --uvp-warning（进行中），与状态标签「格式化中」同色系：
 * 同一个状态在卡片上只有一种颜色，用户不需要记两套映射。
 */
.storage-bar-format {
  display: block;
  height: 100%;
  background: var(--uvp-warning);
  border-radius: 4px;
  transition: width 0.25s ease;
}

/*
 * 设备没报进度：**不给宽度**（给宽度就是撒谎），改用脉冲表示「在动，但不知道到哪了」。
 * 这也是它跟「进度 0%」的空条在形态上的唯一区别。
 */
.storage-item-bar-format.is-indeterminate .storage-bar-format {
  width: 100%;
  animation: storage-format-pulse 1.3s ease-in-out infinite;
}

@keyframes storage-format-pulse {
  0%,
  100% {
    opacity: 0.22;
  }
  50% {
    opacity: 0.62;
  }
}

/* 与偏好减少动效的系统设置对齐（仓库既有做法，见 MediaWorkspaceShell）。 */
@media (prefers-reduced-motion: reduce) {
  .storage-item-bar-format.is-indeterminate .storage-bar-format {
    opacity: 0.45;
    animation: none;
  }
}
.storage-legend-format {
  font-weight: 620;
  color: var(--uvp-warning);
}
.storage-dot.dot-format {
  background: var(--uvp-warning);
}

/*
 * 图例：实心点 = 已占、空心点 = 空余 —— 形状本身说明语义，
 * 不依赖「两个颜色有多好区分」，也不依赖悬停 tooltip（触屏/截图时 tooltip 等于不存在）。
 */
.storage-item-meta,
.storage-item-foot {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 14px;
  align-items: center;
  justify-content: space-between;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.storage-legend {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  white-space: nowrap;
}

/* 总容量不属于任何一段，压暗一档，避免跟「已用/可用」抢读数。 */
.storage-legend-total {
  color: var(--uvp-text-tertiary);
  opacity: 0.78;
}
.storage-dot {
  flex: none;
  width: 8px;
  height: 8px;
  border-radius: 50%;
}
.storage-dot.dot-used {
  background: var(--uvp-brand-cyan);
}
.storage-item[data-level="warning"] .storage-dot.dot-used {
  background: var(--uvp-warning);
}
.storage-item[data-level="danger"] .storage-dot.dot-used {
  background: var(--uvp-danger);
}
.storage-dot.dot-free {
  background: transparent;
  box-shadow: inset 0 0 0 1.5px var(--uvp-secondary-action-border);
}
.storage-item-alert {
  font-weight: 620;
  color: var(--uvp-danger);
}
.storage-empty {
  margin: 0;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.storage-error {
  margin: 0;
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--uvp-danger);
  overflow-wrap: anywhere;
}
.storage-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}
.storage-actions-hint {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
</style>
