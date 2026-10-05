<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { HardDriveDownload } from "@lucide/vue";
import { useUserStoreHook } from "@/store/modules/user";
import {
  formatByteSize,
  formatDuration,
  getRecordCacheTask,
  isRecordCacheTaskActive,
  listRecordCacheTasks,
  type RecordCacheTask
} from "@/api/recordCache";

/**
 * 顶栏的「录像缓存任务」入口：角标 = 正在缓存的任务数，点开是一个小面板。
 *
 * 它替代了原来"点「转后台」就把人踹到录像缓存页"的交互 —— 缓存跑在服务端，
 * 用户关掉弹窗就该能继续干自己的事，任务进度挂在这个常驻入口上。
 *
 * ⛔ 与个人下拉里的「下载任务」（`RecordingDownloadCenter`）是**两回事**，别合并：
 *    那个是"浏览器正在下载到本机"（字节从服务端流向浏览器，关页面就断），
 *    这个是"服务端从设备把录像缓存到服务器"（关页面照跑）。混成一个面板会让用户
 *    分不清哪些任务关掉浏览器会丢。
 */
const props = defineProps<{
  /**
   * 触发按钮的形态：`tabs` = 桌面端标签栏（跟在全屏/主题切换后面），
   * `header` = 移动端头部那一排。
   *
   * ⛔ 尺寸不能靠宿主页面的 scoped 类（`.tabs-action` / `.header-display-action`）：
   *    那两套规则都带宿主的 `data-v-*`，而这里的按钮长在本组件里，属性对不上
   *    （而且外面还套着 `a-badge` 的 span），样式会静默失效 —— 表现是"顶栏多了一个
   *    没样式的裸图标"。所以两个形态的尺寸在本组件里自己声明。
   */
  variant?: "tabs" | "header";
}>();

const router = useRouter();
const permissions = computed(() => useUserStoreHook().account.permissions);
const canView = computed(() => permissions.value.includes("*:*:*") || permissions.value.includes("gb28181:record-cache:view"));

const variant = computed(() => props.variant || "tabs");
const triggerId = computed(() => (variant.value === "header" ? "system-header-record-cache" : "system-tabs-record-cache"));

const visible = ref(false);
const tasks = ref<RecordCacheTask[]>([]);

/**
 * 角标只统计"还在推进"的任务（含整理中）。
 *
 * ⛔ `merging` 必须算进去：那几十秒里录像已经拉完了、后端正在把分片拼成一个文件，
 *    漏掉它会让角标提前归零，用户以为任务已经结束、去下载却被告知"正在整理"。
 *    （这与后端 `RecordCacheTaskActive` 不同 —— 那个不含 merging，语义是"还占着设备通道"。）
 */
const activeTasks = computed(() => tasks.value.filter(task => isRecordCacheTaskActive(task.state)));
const activeCount = computed(() => activeTasks.value.length);

/** 一次取多少条。角标只要"最近的活跃任务"，没必要把整库翻一遍。 */
const PAGE_SIZE = 50;

/**
 * 轮询间隔分两档。
 *
 * ⛔ 不能只用一个间隔：面板关着的时候只有角标准确性重要（慢一点没人看得出来），
 *    但面板一打开，用户就盯着里面那根进度条 —— 15 秒才动一格会让人觉得"卡住了"
 *    （2026-10-05 用户实测反馈："这个进度它不会动呢"）。
 */
const POLL_INTERVAL_IDLE = 15000;
const POLL_INTERVAL_OPEN = 3000;
let pollTimer: number | null = null;

function stopPolling() {
  if (pollTimer !== null) {
    window.clearInterval(pollTimer);
    pollTimer = null;
  }
}

/** 按"面板此刻是否打开"重排轮询。可见性一变就要调，否则间隔不会跟着换。 */
function schedulePolling() {
  stopPolling();
  pollTimer = window.setInterval(
    () => {
      void refresh();
    },
    visible.value ? POLL_INTERVAL_OPEN : POLL_INTERVAL_IDLE
  );
}

/**
 * 把活动任务的**实时进度**补进面板。
 *
 * ⛔⛔ 不补的话面板里会一直显示 `0% · 0秒 / 6分2秒 · 0 B`，看着像卡死 ——
 *   原因有两个，都在后端且都是有意的设计：
 *   1. 列表接口**不打媒体节点**。后端专门钉了一条反向断言（`TestListDoesNotQueryMediaServer`）：
 *      N 条任务 = N 次对外调用，会把列表的响应时间绑死在 ZLM 上。
 *   2. 进度字段只在**整片收尾那一刻**才落库。一段最长要拉 7 分半钟（墙钟），
 *      在这期间 `progress` / `cachedSeconds` / `cachedBytes` 全是 0。
 *   ⇒ 实时部分只能由前端对**活动态的行**逐条问详情（详情接口才做实时探测）。
 *     通常只有 1 条任务在跑，终态的行一个都不问。
 */
async function mergeLiveProgress() {
  const active = tasks.value.filter(task => isRecordCacheTaskActive(task.state));
  if (!active.length) return;
  const responses = await Promise.all(
    active.map(task => getRecordCacheTask(task.taskId, { showErrorMessage: false }).catch(() => null))
  );
  for (const response of responses) {
    const live = response?.data;
    if (!live) continue;
    const target = tasks.value.find(task => task.taskId === live.taskId);
    // ⛔ 只覆盖「还在面板里、且仍然是活动态」的行：任务可能在两次请求之间收了尾，
    //    用这一轮的详情把它盖回去，会把已经完成的任务改回"缓存中"。
    if (!target || !isRecordCacheTaskActive(target.state)) continue;
    Object.assign(target, live);
  }
}

async function refresh() {
  // 后台标签页不轮询：用户看不见角标，这些请求纯属白烧数据库。
  if (document.hidden) return;
  try {
    const response = await listRecordCacheTasks({ page: 1, size: PAGE_SIZE }, { showErrorMessage: false });
    tasks.value = response?.data?.list || [];
    await mergeLiveProgress();
  } catch {
    // ⛔ 保留上一次的结果，不清空、不弹提示：轮询偶发失败时把角标归零，
    //    等于对用户说"任务没了"，比不刷新更糟。
  }
}

// ⛔ 定时器必须随组件卸载清掉：留下来的 interval 会打在已卸载组件的 ref 上，
// 在测试里表现为"上一个用例的请求把下一个用例的计数顶高"（同款假红见前端 flaky 专题）。
onMounted(() => {
  void refresh();
  schedulePolling();
});

onUnmounted(() => {
  stopPolling();
});

/**
 * 面板开／关都要动轮询：打开的那一下先刷一次（面板里必须是此刻的状态，而不是上一轮
 * 轮询的旧值），然后换成"打开档"的高频；关掉再降回"闲置档"。
 *
 * ⛔⛔ 这里**看 `visible` 本身**，不挂 `@popup-visible-change`：那个事件名与发射顺序
 *    都是 Arco 的实现细节，而 `visible` 是"面板此刻到底开着没"的唯一事实。
 *     反例（2026-10-05 踩过）：`a-popover` **没有 `visible` 这个 prop**（它用的是
 *     `popupVisible` / `update:popupVisible`），所以当时那句 `v-model:visible` 是个
 *     永远收不到赋值的死绑定 ——`visible` 恒为 `false`，于是：
 *       ① 面板打开了也永远按"闲置档"15 秒轮询（用户报的"开着时不会实时刷新"，
 *          后端访问日志里那 8 分钟严格 15 秒一格就是它）；
 *       ② 「全部任务」里的 `visible.value = false` 关不掉面板（跳页后它还挂在那）；
 *       ③ `aria-expanded` 恒为 `false`。
 *     绑定写错是**静默**的：没有告警、面板照开，只是"刷新慢半拍"。
 *     防回归：`RecordCacheTaskCenter.render.test.ts` 里用**真 Arco** 点开面板，
 *     断言 `aria-expanded` 真的翻转（桩测建模的是我们以为的 prop，抓不到这种错）。
 */
watch(visible, value => {
  if (value) void refresh();
  schedulePolling();
});

function goTaskList() {
  // 有了上面那条 `v-model:popup-visible`，这里才真的关得掉面板。
  visible.value = false;
  void router.push("/gb28181/record-cache");
}

// ⛔ Arco 的 `a-progress` 收的是 **0~1 的比值**（源码 `width = percent * 100%`），
//    传百分数会渲染成 `width: 1500%`，被容器裁掉后就是"进度条永远顶满"。
function progressRatio(task: RecordCacheTask) {
  return Math.min(1, task.progress || 0);
}

function progressPercent(task: RecordCacheTask) {
  return Math.floor(progressRatio(task) * 100);
}

function statusText(task: RecordCacheTask) {
  if (task.state === "queued") return "排队中";
  // 整理中不是「缓存中」：录像已经全部拉回来了，剩下的只是后端把分片拼成一个文件。
  if (task.state === "merging") return "整理中";
  return "缓存中";
}

function progressLabel(task: RecordCacheTask) {
  if (task.state === "merging") return "录像已全部缓存，正在整理成单个文件";
  const parts = [
    `${progressPercent(task)}%`,
    `${formatDuration(task.cachedSeconds)} / ${formatDuration(task.totalSeconds)}`,
    formatByteSize(task.cachedBytes)
  ];
  if (task.state === "running" && task.speedBytesPerSec > 0) {
    parts.push(`${formatByteSize(task.speedBytesPerSec)}/s`);
  }
  return parts.join(" · ");
}

function taskTitle(task: RecordCacheTask) {
  return task.channelName || task.channelCode || task.deviceName || "录像段";
}
</script>

<template>
  <a-popover
    v-if="canView"
    v-model:popup-visible="visible"
    trigger="click"
    position="br"
    content-class="record-cache-center__popup"
    arrow-class="record-cache-center__arrow"
    :content-style="{ padding: 0 }"
  >
    <a-badge class="record-cache-center__badge" :count="activeCount" :max-count="9">
      <button
        :id="triggerId"
        class="record-cache-center__trigger"
        :class="`record-cache-center__trigger--${variant}`"
        type="button"
        aria-label="录像缓存任务"
        :aria-expanded="visible"
      >
        <HardDriveDownload :size="18" />
      </button>
    </a-badge>

    <template #content>
      <div class="record-cache-center" data-testid="record-cache-center">
        <header class="record-cache-center__head">
          <span data-testid="record-cache-center-summary">
            {{ activeCount ? `${activeCount} 个正在缓存到服务器` : "暂无正在缓存的任务" }}
          </span>
          <a-link class="record-cache-center__all" @click="goTaskList">全部任务</a-link>
        </header>

        <a-empty v-if="!activeTasks.length" description="暂无正在缓存的任务" />
        <ul v-else class="record-cache-center__list">
          <li v-for="task in activeTasks" :key="task.taskId" class="record-cache-center__item">
            <div class="record-cache-center__title">
              <strong :title="taskTitle(task)">{{ taskTitle(task) }}</strong>
              <span class="record-cache-center__state">{{ statusText(task) }}</span>
            </div>
            <a-progress class="record-cache-center__bar" :percent="progressRatio(task)" size="small" :show-text="false" />
            <small class="record-cache-center__detail">{{ progressLabel(task) }}</small>
          </li>
        </ul>
      </div>
    </template>
  </a-popover>
</template>

<style scoped>
/* 两个形态的尺寸都与它们各自的邻居保持一致：
   tabs  = Tabs/index.vue 的 `.tabs-action`（28×28，跟全屏/主题切换同排）
   header= header-right/index.vue 的 `.header-display-action`（40×40，触屏友好）
   ⛔ 不能直接复用那两套类名：它们带宿主的 `data-v-*`，匹配不到本组件里的按钮。 */
.record-cache-center__trigger {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0;
  appearance: none;
  cursor: pointer;
  background: transparent;
  border: 0;
}

.record-cache-center__trigger--tabs {
  width: 28px;
  height: 28px;
  color: var(--color-text-2);
  border-radius: 6px;
}

.record-cache-center__trigger--tabs:hover {
  color: rgb(var(--primary-6));
  background: var(--color-primary-light-1);
}

.record-cache-center__trigger--header {
  width: 40px;
  height: 40px;
  color: var(--uvp-text-secondary);
  border-radius: 8px;
}

.record-cache-center__trigger--header:hover {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
}

/* 面板内容。⛔ 弹层被 teleport 到 body，但这里的选择器都带本组件的 scope 属性，
   而 scope 属性长在元素自己身上，所以照样命中（不命中的只有 Arco 自己的壳，
   那个用 `content-style` 调）。 */
.record-cache-center {
  display: flex;
  flex-direction: column;
  width: 300px;
  max-width: 78vw;
}

.record-cache-center__head {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  font-size: 12px;
  color: var(--uvp-text-secondary);
  border-bottom: 1px solid var(--uvp-border);
}

.record-cache-center__all {
  flex: none;
  font-size: 12px;
}

.record-cache-center__list {
  display: flex;
  flex-direction: column;
  max-height: 320px;
  padding: 0;
  margin: 0;
  overflow-y: auto;
  list-style: none;
}

.record-cache-center__item {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--uvp-border);
}

.record-cache-center__item:last-child {
  border-bottom: 0;
}

.record-cache-center__title {
  display: flex;
  gap: 8px;
  align-items: baseline;
  justify-content: space-between;
  min-width: 0;
}

.record-cache-center__title strong {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 13px;
  white-space: nowrap;
}

.record-cache-center__state {
  flex: none;
  font-size: 12px;
  color: var(--uvp-brand);
}

/* ⛔ 进度条是 flex 子项，`min-width: 0` 不能省：内层 `.arco-progress-line`
   的 `width: 100%` 会把这一行顶开（同款"flex 行被挤爆"见前端 UI 专题）。 */
.record-cache-center__bar {
  width: 100%;
  min-width: 0;
}

.record-cache-center__detail {
  font-size: 12px;
  color: var(--uvp-text-secondary);
  overflow-wrap: anywhere;
}
</style>

<!--
  ⛔ 这个块**故意不加 scoped**，两个原因：
    1. 角标由 Arco 自己渲染（`sup.arco-badge-number`），它跟触发按钮是**兄弟**而不是后代，
       `:deep()` 那种「带 scope 属性的祖先 + 后代」的写法锚不到它；
    2. 弹层被 teleport 到 body，那三个元素也不在本组件的子树里。
  作用域靠本组件独有的类名（`record-cache-center__*`）兜底，不会波及其它页面。
-->
<style>
/* 角标：Arco 默认是 20px 高 / 12px 字，底色 `rgb(var(--danger-6))`（红），
   外面还裹一圈 `--color-bg-2` 的描边。压在 28px 的图标上既盖住图标本身，
   深色顶栏下那圈描边还会变成一个不属于本主题的亮环（2026-10-05 用户实测反馈）。 */
.record-cache-center__badge .arco-badge-number {
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  font-size: 11px;
  font-weight: 600;
  line-height: 16px;

  /* ⛔ 这一对是**跨主题自动成立**的：`--uvp-brand-strong` 亮色是深蓝(#1d4ed8)、
     暗色是浅蓝(#93c5fd)；`--uvp-navigation-bg` 亮色近白、暗色近黑。
     写成「`--uvp-brand` 打底 + `#fff` 字」在暗色下就是浅蓝底配白字，直接糊掉。 */
  color: var(--uvp-navigation-bg);
  background-color: var(--uvp-brand-strong);

  /* 描边跟着顶栏底色，而不是 Arco 的 `--color-bg-2`（深色下那是个亮环）。 */
  box-shadow: 0 0 0 2px var(--uvp-navigation-bg);
}

/* 弹层表面。⛔ 必须显式声明：Arco 的 `.arco-popover-popup-content` 用的是
   `--color-bg-popup` + `--color-neutral-3` + 自己的阴影 —— 那套灰跟本仓的蓝黑面板
   摆在一起就是"两套皮肤"（同一排的个人下拉用的是 `--uvp-popconfirm-bg`）。
   这里复用**同一组浮层 token**，两处观感就齐了。
   `!important`：这些都是单类选择器、与 Arco 同优先级，谁后加载谁赢 —— 不能赌注入顺序。 */
.record-cache-center__popup {
  overflow: hidden;
  background-color: var(--uvp-popconfirm-bg) !important;
  border: 1px solid var(--uvp-popconfirm-border) !important;

  /* Arco 那边是 `var(--border-radius-medium)`，跟个人下拉的 12px 不是一个值。 */
  border-radius: 12px !important;
  box-shadow: var(--uvp-popconfirm-shadow) !important;
}

/* 箭头是独立元素（`.arco-popover-popup-arrow`），不跟着表面走。
   漏了它就会在面板边上留一个浅色小三角，比不修还显眼。 */
.record-cache-center__arrow {
  background-color: var(--uvp-popconfirm-bg) !important;
  border-color: var(--uvp-popconfirm-border) !important;
}
</style>
