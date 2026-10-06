<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { CircleStop, Download, Layers, RefreshCw, RotateCcw, Search, Star, Trash2 } from "@lucide/vue";
import { useUserStoreHook } from "@/store/modules/user";
import {
  cancelRecordCacheTask,
  createRecordCacheDownload,
  deleteRecordCacheTask,
  formatByteSize,
  formatDuration,
  getRecordCacheTask,
  isRecordCacheTaskActive,
  isRecordCacheTaskStoppable,
  listRecordCacheTasks,
  recordCacheDownloadContentPath,
  setRecordCacheFavorite,
  type RecordCacheFile,
  type RecordCacheState,
  type RecordCacheTask
} from "@/api/recordCache";

const userStore = useUserStoreHook();
const hasPermission = (permission: string) =>
  userStore.account.permissions.includes("*:*:*") || userStore.account.permissions.includes(permission);
const canCancel = computed(() => hasPermission("gb28181:record-cache:cancel"));
const canDownload = computed(() => hasPermission("gb28181:record-cache:download"));
const canDelete = computed(() => hasPermission("gb28181:record-cache:delete"));
const canFavorite = computed(() => hasPermission("gb28181:record-cache:favorite"));

/** 分页档位。与本仓统一口径一致（见 `src/style/model/pagination-unification.test.ts`）。 */
const RECORD_CACHE_PAGE_SIZE_OPTIONS = [10, 20, 50, 100];

const loading = ref(false);
const tasks = ref<RecordCacheTask[]>([]);
const filters = reactive({
  state: undefined as string | undefined,
  keyword: "",
  /**
   * 收藏筛选。
   *
   * ⛔ 必须能表达**三种**状态：`undefined` = 不筛、`"true"` = 只看收藏、
   *    `"false"` = 只看未收藏。用布尔（或把不筛也写成 false）的话，"不筛选"
   *    会悄悄变成"只看未收藏" —— 表现是刚打开页面一条都没有（缓存过的基本都被收藏了），
   *    而且没有任何报错。
   *
   * 取值用字符串而不是布尔：同仓搜索面板里的同类筛选（`onlineFilter`）就是这么做的，
   * 下拉框的值本来就是字符串，统一口径比在页面里各写一套强。
   */
  favorite: undefined as string | undefined
});
const pagination = reactive({ page: 1, size: RECORD_CACHE_PAGE_SIZE_OPTIONS[1], total: 0 });
const pendingTaskId = ref("");
const downloadingKey = ref("");
const favoritePendingId = ref("");

const stateOptions: Array<{ label: string; value: RecordCacheState }> = [
  { label: "排队中", value: "queued" },
  { label: "缓存中", value: "running" },
  { label: "整理中", value: "merging" },
  { label: "已完成", value: "succeeded" },
  { label: "失败", value: "failed" },
  { label: "已取消", value: "cancelled" },
  { label: "已过期", value: "expired" }
];

const favoriteOptions: Array<{ label: string; value: string }> = [
  { label: "已收藏", value: "true" },
  { label: "未收藏", value: "false" }
];

/** 把筛选框的字符串三态翻成接口要的布尔三态（`undefined` = 不筛，不传这个字段）。 */
function favoriteQueryValue(): boolean | undefined {
  if (filters.favorite === "true") return true;
  if (filters.favorite === "false") return false;
  return undefined;
}

const stateMeta: Record<RecordCacheState, { text: string; color: string }> = {
  queued: { text: "排队中", color: "arcoblue" },
  running: { text: "缓存中", color: "arcoblue" },
  // ⛔ 收尾整理**不是**「缓存中」：录像已经全部拉完，剩下的是后端把分片拼成一个文件。
  // 显示成「缓存中」会让用户以为还在耗流量/还在占设备通道。
  merging: { text: "整理中", color: "orange" },
  succeeded: { text: "已完成", color: "green" },
  failed: { text: "失败", color: "red" },
  cancelled: { text: "已取消", color: "gray" },
  expired: { text: "已过期", color: "gray" }
};

function stateText(state: RecordCacheState) {
  return stateMeta[state]?.text || state;
}

function stateColor(state: RecordCacheState) {
  return stateMeta[state]?.color || "gray";
}

function formatTime(value: string) {
  if (!value) return "-";
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return "-";
  const pad = (input: number) => String(input).padStart(2, "0");
  return `${parsed.getFullYear()}-${pad(parsed.getMonth() + 1)}-${pad(parsed.getDate())} ${pad(parsed.getHours())}:${pad(
    parsed.getMinutes()
  )}:${pad(parsed.getSeconds())}`;
}

// ⛔ Arco 的 `a-progress` 收的是 **0~1 的比值**，不是百分数（源码
//    `barStyle.width = `${percent * 100}%``）。传 0~100 会渲染成 `width: 1500%`，
//    被容器裁掉后就是"进度条永远顶满"。
//    判别依据与防回归见 `src/style/model/arco-progress-percent.test.ts`。
function progressRatio(task: RecordCacheTask) {
  return Math.min(1, task.progress || 0);
}

/**
 * 进度列的百分比文字。
 *
 * ⛔ 口径：**比值喂组件、百分数只用于文字**（见上面的契约用例）。
 *    这里必须自己渲染，而不是打开 `a-progress` 的 `show-text`：Arco 的内建文字宽度固定，
 *    失败态还会把数字换成 ✗ 图标，跟下面那行「已缓存 / 总时长」对不齐。
 *
 * 向下取整而不是四舍五入：0.999 说成 "100%" 会让用户以为已经收尾了（实际还在拉最后一片）。
 */
function progressPercent(task: RecordCacheTask) {
  return Math.floor(progressRatio(task) * 100);
}

function hasActiveTask() {
  return tasks.value.some(task => isRecordCacheTaskActive(task.state));
}

let pollTimer: number | null = null;

function stopPolling() {
  if (pollTimer !== null) {
    window.clearInterval(pollTimer);
    pollTimer = null;
  }
}

// ⛔ 轮询必须随组件卸载停掉：留在队列里的定时器会打到已卸载组件的 store 上，
// 在测试里表现为"上一个用例的请求把下一个用例的计数顶高"（同款假红成因见前端 flaky 专题）。
onUnmounted(() => {
  stopPolling();
});

function schedulePolling() {
  stopPolling();
  if (!hasActiveTask()) return;
  pollTimer = window.setInterval(() => {
    void loadTasks(true);
  }, 5000);
}

/**
 * 把「正在跑的任务」的实时进度补进列表。
 *
 * ⛔⛔ 列表接口**有意不带**实时进度：进度只在分片收尾时落库，实时部分要向媒体节点
 * 逐条查 —— 后端为此专门钉了一条用例（`TestListDoesNotQueryMediaServer`）禁止列表
 * 接口打 ZLM，理由是「N 条任务 = N 次外部调用，会把列表延迟绑在媒体节点上」。
 *
 * 所以"列表里也要看到进度在动"这件事只能在前端补：对**活动中的行**逐条问详情
 * （详情接口才做实时探测）。只对活动态发请求 —— 通常只有 1 条，终态行一个都不问。
 *
 * ⛔ 不这样做的话表现是：状态明明是「缓存中」、速率也有值，进度数字和进度条
 * 却在整片（最长 7.5 分钟墙钟）里纹丝不动，用户会以为卡死。
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
    // ⛔ 只覆盖「还在列表里、且仍然是活动态」的行：期间用户可能已经翻页/筛选，
    //    或者任务在两次轮询之间收了尾（那一轮列表请求拿到的是更新的状态，不能被旧值盖回去）。
    if (!target || !isRecordCacheTaskActive(target.state)) continue;
    Object.assign(target, live);
  }
}

async function loadTasks(silent = false) {
  if (!silent) loading.value = true;
  try {
    const response = await listRecordCacheTasks({
      page: pagination.page,
      size: pagination.size,
      state: filters.state || undefined,
      keyword: filters.keyword.trim() || undefined,
      // ⛔ 走 favoriteQueryValue()，不要写成 `filters.favorite || undefined` ——
      //    那会把「只看未收藏」（"false"）折成"不筛"，筛选条件静默失效。
      favorite: favoriteQueryValue()
    });
    const payload = response?.data;
    tasks.value = payload?.list || [];
    pagination.total = payload?.total || 0;
    await mergeLiveProgress();
  } catch {
    if (!silent) Message.error("加载录像缓存任务失败");
  } finally {
    if (!silent) loading.value = false;
    // 放在 finally：静默轮询偶发失败时不能让定时器断掉（否则列表从此不再刷新）。
    schedulePolling();
  }
}

function query() {
  pagination.page = 1;
  void loadTasks();
}

function reset() {
  filters.state = undefined;
  filters.keyword = "";
  // ⛔ 收藏筛选也要一起清掉：漏了它，"重置"之后列表仍是被筛过的样子，
  //    用户会以为任务丢了。
  filters.favorite = undefined;
  query();
}

function changePage(value: number) {
  pagination.page = value;
  void loadTasks();
}

function changePageSize(value: number) {
  pagination.page = 1;
  pagination.size = value;
  void loadTasks();
}

async function cancelTask(task: RecordCacheTask) {
  pendingTaskId.value = task.taskId;
  try {
    await cancelRecordCacheTask(task.taskId);
    Message.success("已取消缓存任务");
    await loadTasks();
  } catch {
    Message.error("取消失败");
  } finally {
    pendingTaskId.value = "";
  }
}

/**
 * 收藏 / 取消收藏。
 *
 * ⛔ 收藏的语义只有一条：**不参与保留期自动清理**。它不是"防手滑删除"，
 *    手动删除照样会把文件清掉（后端 `cleanupExpired` 只跳过自动那一轮）。
 * ⛔ 收藏**不刷新**过期时间 —— 那是"续期"，跟"跳过清理"是两件事，别混。
 * ⛔ 回写本地时以**后端回包**为准，不要只翻转本地字段：写库失败时界面已经显示
 *    "已收藏"、库里其实还是 0，过几天文件被清理掉 —— 而这个功能存在的全部意义
 *    就是防止那件事，而且它不会有任何报错来提醒你。
 */
async function toggleFavorite(task: RecordCacheTask) {
  if (favoritePendingId.value) return;
  const next = !task.favorite;
  favoritePendingId.value = task.taskId;
  try {
    const response = await setRecordCacheFavorite(task.taskId, next);
    task.favorite = response?.data?.favorite ?? next;
    Message.success(task.favorite ? "已收藏，该录像不会被自动清理" : "已取消收藏");
    // ⛔ 正在「只看收藏」时取消收藏，这一行已经不符合筛选条件了 ——
    //    留在列表里会让人以为"取消收藏没生效"。重新拉一次列表最诚实。
    if (filters.favorite === "true" && !task.favorite) await loadTasks();
  } catch {
    Message.error("操作失败，请重试");
  } finally {
    favoritePendingId.value = "";
  }
}

function confirmDelete(task: RecordCacheTask) {
  Modal.warning({
    title: "删除缓存",
    content: `将删除「${task.channelName || task.deviceName}」这段录像的服务器缓存文件，且不可恢复。`,
    hideCancel: false,
    okText: "删除",
    cancelText: "取消",
    onOk: async () => {
      pendingTaskId.value = task.taskId;
      try {
        await deleteRecordCacheTask(task.taskId);
        Message.success("已删除");
        await loadTasks();
      } catch {
        Message.error("删除失败");
      } finally {
        pendingTaskId.value = "";
      }
    }
  });
}

/**
 * 整段录像的下载文件名。
 *
 * ⛔ 后端对多分片会先合成一个文件再发，返回的名字是 `record-cache-<任务号前8位>.mp4`；
 * 那个名字对用户没有信息量，这里用「通道名 + 录像起点」重命名 —— 同时下几段录像时
 * 用户在下载目录里能直接对上号。保存名与原名不一致是允许的：anchor.download 优先。
 */
function mergedFileName(task: RecordCacheTask) {
  const name = task.channelName || task.channelCode || "录像缓存";
  const stamp = formatTime(task.startTime).replace(/[-: ]/g, "");
  return `${name}-${stamp}.mp4`;
}

/** 单片分段的文件名：跟整段放在同一目录时要能一眼看出是第几片。 */
function segmentFileName(task: RecordCacheTask, file: RecordCacheFile) {
  const base = (task.channelName || task.channelCode || "录像缓存") + "-" + formatTime(task.startTime).replace(/[-: ]/g, "");
  return `${base}-第${file.index + 1}段.mp4`;
}

/**
 * 交给浏览器自己下载（原生下载）。
 *
 * ⛔ 必须是"导航到同一个地址"而不是 XHR 取 Blob：只有前者会进浏览器的下载栏，
 * 有真实进度、能暂停/续传、大文件不会把标签页拖崩。
 * ⛔ 同源地址的 `<a download>` 会带上那张 HttpOnly 票据 cookie，所以这里不需要也不能加请求头。
 */
function triggerNativeDownload(contentUrl: string, fileName: string) {
  const anchor = document.createElement("a");
  anchor.href = contentUrl;
  anchor.download = fileName;
  anchor.rel = "noopener";
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
}

/**
 * 签发一次下载并把浏览器拉起来。
 *
 * index 不传 = 整段录像（后端在收尾期已经把分片拼好，点下去就是真实网速）。
 */
async function startDownload(task: RecordCacheTask, fileName: string, index?: number) {
  if (task.state === "merging") {
    // 整理中产品端还没就绪，后端也会回 409；这里先挡住，省得用户以为点了没反应。
    Message.warning("录像正在整理，请稍候再试");
    return;
  }
  downloadingKey.value = task.taskId;
  try {
    const response = await createRecordCacheDownload(task.taskId, index);
    const creation = response?.data;
    const expected = creation?.task?.taskId ? recordCacheDownloadContentPath(creation.task.taskId, index) : "";
    if (!creation?.contentUrl || creation.contentUrl !== expected) {
      // ⛔ 不比对就直接把地址交给浏览器：万一后端返回了外部地址，这张一次性凭据
      // 就会被带着去访问别的站点；比对失败时也只提示，不做任何兜底跳转。
      Message.error("下载地址异常，请重试");
      return;
    }
    triggerNativeDownload(creation.contentUrl, fileName);
    Message.success("已开始下载");
  } catch (error) {
    // ⛔ 服务端已经给出了可操作的原因时不要再盖一句笼统的「下载失败」——
    // 典型的可操作原因是「录像正在整理，请稍候再试」「该录像分段暂时无法合并成一个文件」，
    // 盖掉就等于把唯一的出路也藏了。HTTP 层已经把服务端原话弹出来了，
    // 这里只在**没有响应**（网络断 / 超时）时才兜底。
    if (!(error as { response?: unknown })?.response) Message.error("下载失败");
  } finally {
    downloadingKey.value = "";
  }
}

/**
 * 下载整段录像。
 *
 * ⛔ index 不传：多分片时后端在收尾期已经把各片无损拼成一个文件，
 * 逐个 index 去下只会拿到同一个完整文件的多个副本（每下一次都是几十上百 MB）。
 */
function downloadTask(task: RecordCacheTask) {
  if (!(task.files || []).length) {
    Message.warning("暂无可下载的文件");
    return;
  }
  void startDownload(task, mergedFileName(task));
}

/**
 * 只取其中一段。
 *
 * ⛔ 序号必须真的传到后端（写进下载地址的路径里）：这条入口存在的意义就是"合不出来时
 * 先拿走某一小段"，如果后端仍发整段录像，界面就是在说谎 —— 而且用户看到体积不对会
 * 反复点，每次都拉一遍整段。
 */
function downloadSegment(task: RecordCacheTask, file: RecordCacheFile) {
  if (!file.downloadable) {
    Message.warning("该分片暂不可下载");
    return;
  }
  void startDownload(task, segmentFileName(task, file), file.index);
}

onMounted(() => {
  void loadTasks();
});
</script>

<template>
  <div class="snow-fill record-cache-page">
    <div class="snow-fill-inner uvp-page-shell-flat record-cache-shell">
      <s-layout-search class="record-cache-search">
        <template #fields>
          <a-select v-model="filters.state" placeholder="任务状态" allow-clear style="width: 148px">
            <a-option v-for="option in stateOptions" :key="option.value" :value="option.value">{{ option.label }}</a-option>
          </a-select>
          <a-input
            v-model="filters.keyword"
            allow-clear
            placeholder="通道名 / 设备名 / 编码"
            style="width: 240px"
            @press-enter="query"
          />
          <a-select v-model="filters.favorite" placeholder="收藏状态" allow-clear style="width: 130px">
            <a-option v-for="option in favoriteOptions" :key="option.value" :value="option.value">{{ option.label }}</a-option>
          </a-select>
        </template>
        <template #actions>
          <a-button type="primary" :loading="loading" @click="query"
            ><template #icon><Search :size="15" /></template>查询</a-button
          >
          <a-button :disabled="loading" @click="reset"
            ><template #icon><RotateCcw :size="15" /></template>重置</a-button
          >
          <a-button :disabled="loading" title="刷新" @click="loadTasks(true)"
            ><template #icon><RefreshCw :size="15" /></template
          ></a-button>
        </template>
      </s-layout-search>

      <a-table
        class="uvp-data-table record-cache-table"
        :data="tasks"
        :loading="loading"
        :pagination="false"
        :bordered="false"
        :scroll="{ x: 2290, y: '100%' }"
        row-key="taskId"
      >
        <template #columns>
          <!-- 任务 ID 是排障时的唯一指代（日志、下载地址、后端排查都按它对齐），所以放最前
               并完整露出来，不做省略号截断。表格 `scroll.x` 有 2000+ 需要横向滚动，
               它是排障入口，不该滚出视野 ⇒ 钉在左侧。 -->
          <a-table-column title="任务 ID" :width="300" fixed="left">
            <template #cell="{ record }">
              <span class="mono" :title="record.taskId">{{ record.taskId }}</span>
            </template>
          </a-table-column>

          <!-- 「设备」和「通道」各自把**名称 + 国标编码**合成一格（上行名称、下行编码）。
               ⛔ 编码一行都不折：20 位国标编码按 mono 字体量下来 ~168px，加内边距要 180 起步，
                  所以这两列各留 220。名字再长就在上面那行 `ellipsis` 省略、hover 出title。
               ⛔ 通道那一格取的是 `channelCode`，**不是** `channelId` ——
                  `channelId` 是库里的自增主键（31 这种），在设备侧根本不存在这个号，
                  设备面板上的通道编码是 `channelCode`。同理设备那一格是 `deviceId`。 -->
          <a-table-column title="设备" :width="180">
            <template #cell="{ record }">
              <div class="record-cache-cell record-cache-cell--stack">
                <span class="record-cache-cell__title" :title="record.deviceName || record.deviceId">
                  {{ record.deviceName || record.deviceId || "-" }}
                </span>
                <span class="record-cache-cell__sub mono">{{ record.deviceId || "-" }}</span>
              </div>
            </template>
          </a-table-column>

          <a-table-column title="通道" :width="180">
            <template #cell="{ record }">
              <div class="record-cache-cell record-cache-cell--stack">
                <span class="record-cache-cell__title" :title="record.channelName || record.channelCode">
                  {{ record.channelName || record.channelCode || "-" }}
                </span>
                <span class="record-cache-cell__sub mono">{{ record.channelCode || "-" }}</span>
              </div>
            </template>
          </a-table-column>

          <!-- 录像区间只有"起 / 至"两行时间戳，内容宽度是固定的（`2026-10-06 12:53:49`），
               290 是当初随手给的富余值，实际只需 ~230 就不会折行。 -->
          <a-table-column title="录像区间" :width="180">
            <template #cell="{ record }">
              <div class="record-cache-cell record-cache-cell--stack">
                <span>{{ formatTime(record.startTime) }}</span>
                <span class="record-cache-cell__sub">至 {{ formatTime(record.endTime) }}</span>
              </div>
            </template>
          </a-table-column>

          <a-table-column title="状态" :width="100">
            <template #cell="{ record }">
              <a-tag :color="stateColor(record.state)" size="small">{{ stateText(record.state) }}</a-tag>
            </template>
          </a-table-column>

          <a-table-column title="进度" :width="220">
            <template #cell="{ record }">
              <div class="record-cache-cell record-cache-cell--stack">
                <div class="record-cache-progress" :data-state="record.state">
                  <!-- ⛔ 颜色统一由 CSS 按 `[data-state]` 取本仓 token，**不让组件自己猜**：
                       Arco 的 `computedStatus = status || (percent>=1 ? "success" : "normal")`
                       （progress.js）—— "到底走没走完"是**推断**，不是业务状态：
                       整理中/已取消时percent 也可能已经 1，那时条会变成"成功绿"。
                       ⛔ 也不传 `size`：它只决定 `strokeWidth`（3/4/8px），而 Arco 把高度
                       写成**行内 style**（`height: 3px`），CSS 压不过⇒ 只能靠 `:stroke-width` 传。
                       这里给 6px：Arco 默认 3px 下100px 圆角被压成 1.5px，看着是根硬线。 -->
                  <a-progress
                    class="record-cache-progress__bar"
                    :data-state="record.state"
                    :percent="progressRatio(record)"
                    :stroke-width="6"
                    :show-text="false"
                  />
                  <span class="record-cache-progress__percent">{{ progressPercent(record) }}%</span>
                </div>
                <span class="record-cache-cell__sub">
                  <template v-if="record.state === 'merging'">录像已全部缓存，正在整理成单个文件…</template>
                  <template v-else>
                    {{ formatDuration(record.cachedSeconds) }} / {{ formatDuration(record.totalSeconds) }}
                    <template v-if="record.state === 'running' && record.speedBytesPerSec > 0">
                      · {{ formatByteSize(record.speedBytesPerSec) }}/s
                    </template>
                  </template>
                </span>
              </div>
            </template>
          </a-table-column>

          <a-table-column title="大小" :width="110">
            <template #cell="{ record }">{{ formatByteSize(record.cachedBytes) }}</template>
          </a-table-column>

          <a-table-column title="倍速" :width="80">
            <template #cell="{ record }">{{ record.downloadSpeed }}×</template>
          </a-table-column>

          <a-table-column title="操作人" :width="110">
            <template #cell="{ record }">{{ record.createdByName || "-" }}</template>
          </a-table-column>

          <a-table-column title="创建时间" :width="180">
            <template #cell="{ record }">{{ formatTime(record.createdAt) }}</template>
          </a-table-column>

          <a-table-column title="过期时间" :width="180">
            <template #cell="{ record }">{{ formatTime(record.expiresAt) }}</template>
          </a-table-column>

          <!-- 「操作」列宽度按最挤的一行给足：取消收藏(4 字，最长的一档) + 下载 + 分段 + 删除。
               ⛔ 收藏原来是靠左的独立一列（老板 2026-10-06 要求并进来），
               独立列占掉一格宽度、且横向滚动时和操作区分处两端，两处都得找。
               并进操作列后它天然跟着 `fixed="right"` 钉住，位置也稳定（永远第一个）。 -->
          <a-table-column title="操作" :width="260" align="center" fixed="right">
            <template #cell="{ record }">
              <div class="uvp-table-actions">
                <!-- ⛔ 用 `a-link`（本仓操作列的唯一基准），别换成 `a-button`：Arco 按钮自带
                     padding / 圆角 / hover 底色，在同一张表里就成了第二套观感。
                     ⛔ 纯图标在操作列里认不出是什么（同一排还有星标以外的下载/删除），
                        必须带文字；收藏态**换文案**（收藏 ↔ 取消收藏）而不只是变色 ——
                        只变色的话，"点一下会取消"这件事得靠 tooltip 才知道。 -->
                <a-link
                  v-if="canFavorite"
                  class="uvp-table-action uvp-table-action--favorite"
                  :class="{ 'is-on': record.favorite }"
                  :disabled="favoritePendingId === record.taskId"
                  :title="record.favorite ? '已收藏，不会被自动清理' : '收藏后不会被自动清理'"
                  @click="toggleFavorite(record)"
                >
                  <template #icon><Star :size="14" /></template>
                  <span>{{ record.favorite ? "取消收藏" : "收藏" }}</span>
                </a-link>
                <!-- ⛔ 操作列一律用 `a-link`，不要用 `a-button type="text"`。
                     本仓 34 个页面的操作列都是 a-link（device-mgmt / cloud-recordings /
                     role / menu …），色调由 `uvp-table-action--*` 统一给。用 a-button 会带上
                     Arco 按钮自己的 padding / 圆角 / hover 底色，在同一张表里就成了第二套观感。
                     ⚠️ 别再拿"a-link 没有 loading"当理由：Arco Link **有** `loading` 与 `disabled`
                     （`@arco-design/web-vue/es/link/link.d.ts`），cloud-recordings 的「停止录像」
                     就是这么写的。 -->
                <a-link
                  v-if="isRecordCacheTaskStoppable(record.state) && canCancel"
                  class="uvp-table-action uvp-table-action--stop"
                  :loading="pendingTaskId === record.taskId"
                  :disabled="pendingTaskId === record.taskId"
                  @click="cancelTask(record)"
                >
                  <template #icon><CircleStop :size="13" /></template>
                  <span>取消</span>
                </a-link>
                <template v-else-if="record.state === 'succeeded' && canDownload">
                  <a-link
                    class="uvp-table-action uvp-table-action--download"
                    :loading="downloadingKey === record.taskId"
                    :disabled="downloadingKey === record.taskId"
                    @click="downloadTask(record)"
                  >
                    <template #icon><Download :size="13" /></template>
                    <span>下载</span>
                  </a-link>
                  <!-- ⛔ 多分片时后端在收尾期已经把各片无损拼成一个文件，所以主入口只有一个「下载」。
                       这个「分段」入口是"只要其中一段"用的（比如 3 小时的录像只想要中间那一段）：
                       序号会真的传到后端，下载地址与票据作用域都跟着变成那一段。 -->
                  <a-dropdown v-if="(record.files || []).length > 1" trigger="click">
                    <a-link class="uvp-table-action uvp-table-action--detail" :disabled="downloadingKey === record.taskId">
                      <template #icon><Layers :size="13" /></template>
                      <span>分段</span>
                    </a-link>
                    <template #content>
                      <a-doption
                        v-for="file in record.files"
                        :key="file.index"
                        :disabled="!file.downloadable"
                        @click="downloadSegment(record, file)"
                      >
                        第 {{ file.index + 1 }} 段 · {{ formatByteSize(file.size) }}
                      </a-doption>
                    </template>
                  </a-dropdown>
                </template>
                <a-link
                  v-if="canDelete && !isRecordCacheTaskStoppable(record.state)"
                  class="uvp-table-action uvp-table-action--delete"
                  @click="confirmDelete(record)"
                >
                  <template #icon><Trash2 :size="13" /></template>
                  <span>删除</span>
                </a-link>
              </div>
            </template>
          </a-table-column>
        </template>
        <template #empty>
          <a-empty description="还没有缓存任务。在「设备录像」页面选一段录像点「缓存到服务器」即可。" />
        </template>
      </a-table>

      <!-- ⛔ 独立分页条必须挂 `uvp-pagination-bar`：本仓分页条的统一基类，
           三开关（总数 / 每页条数 / 跳页）与档位口径见
           `src/style/model/pagination-unification.test.ts`。 -->
      <footer class="record-cache-pagination uvp-pagination-bar">
        <a-pagination
          :current="pagination.page"
          :page-size="pagination.size"
          :total="pagination.total"
          :page-size-options="RECORD_CACHE_PAGE_SIZE_OPTIONS"
          show-total
          show-page-size
          show-jumper
          @change="changePage"
          @page-size-change="changePageSize"
        />
      </footer>
    </div>
  </div>
</template>

<style scoped>
.record-cache-page {
  height: 100%;
  min-height: 0;
  overflow: hidden;
}

.record-cache-shell {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  padding: 0;
  overflow: hidden;
}

.record-cache-search {
  flex: none;
}

.record-cache-table {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

/* ⛔ 行高/边框统一由 `.uvp-data-table` 的标准块给，这里不再重复声明。 */
.record-cache-table :deep(.arco-table-container) {
  height: 100%;
}

.record-cache-pagination {
  display: flex;
  flex: none;
  flex-wrap: wrap;
  gap: 16px;
  align-items: center;
  justify-content: flex-end;
  margin-top: 12px;
  font-size: var(--zlm-fs-caption);
  color: var(--zlm-text-3);
}

.record-cache-cell {
  display: flex;
  line-height: 1.45;
}

.record-cache-cell--stack {
  flex-direction: column;
  gap: 2px;
}

/* 名称行（设备名 / 通道名）：定宽列里名字可能很长，单行省略 + hover 出 title，
   别让它折行把整行撑高。`*_sub`（国标编码）**不**省略 —— 编码要对得上。 */
.record-cache-cell__title {
  overflow: hidden;
  text-overflow: ellipsis;
  font-weight: 500;
  white-space: nowrap;
}

.record-cache-cell__sub {
  font-size: 12px;
  color: var(--zlm-text-3);
}

/* ⛔ 国标编码（20 位）与任务号一律等宽字体：比例字体里数字宽度不一，20 位编码
   根本对不齐，出问题时没法一眼比对。同仓其它页面（device-mgmt / playback-log）
   也是这么做的。 */
.mono {
  font-family: var(--zlm-font-mono);
}

/* ── 进度列：进度条 + 百分比数字 ───────────────────────────────────────── */
.record-cache-progress {
  display: flex;
  gap: 8px;
  align-items: center;
  min-width: 0;
}

/* ⛔ 进度条是 flex 子项，必须显式 `flex: 1` + `min-width: 0`。
   只给 flex 而不给 `min-width: 0` 时，内层 `.arco-progress-line` 的 `width: 100%`
   会把兄弟节点（百分比数字）挤出单元格 —— 同款"flex 行被挤爆"见 frontend-ui 专题的
   `.arco-spin { width: 100% }` 那条。 */
.record-cache-progress__bar {
  flex: 1;
  min-width: 0;
}

/* 底轨 = 还没缓存的那部分。它是**大面积色块**，必须两套主题都看得见 ⇒ 用系统里
   专为此留的 `--uvp-meter-track`（亮 #e2e8f0 / 暗 #33475c）。
   ⛔ 别用 Arco 自带的 `--color-fill-3`：那套是 Arco 自己的灰阶，跟本仓面板底色不同源，
      暗色下会和卡片底糊在一起。同款坑见 StorageCardStatusPanel 的 `--uvp-border` 教训。 */
.record-cache-progress__bar :deep(.arco-progress-line) {
  background: var(--uvp-meter-track);

  /* 胶囊：6px 高配999px 圆角 = 两端正好半圆。Arco 默认写死 100px，在 6px 上也够，
     这里仍显式写出来 —— 依赖 Arco 内部数值换算，下次它改 strokeWidth 就会变方角。 */
  border-radius: 999px;
  box-shadow: inset 0 0 0 1px rgb(15 23 42 / 4%);
}

/* 已缓存的那一段。颜色按 `data-state` 走本仓 token（不用 Arco 自带的状态色 ——
   见模板处注释：`percent>=1` 会被它自动判成"成功绿"，而整理中/已取消也可能已经 1）。 */
.record-cache-progress__bar :deep(.arco-progress-line-bar) {
  background: var(--uvp-brand);
  border-radius: 999px;
  transition:
    width 0.6s cubic-bezier(0.34, 0.69, 0.1, 1),
    background-color 0.3s ease;
}

/* 进行中：加一层极淡的高光，让"正在跑"的方向感更强（不加动画 —— 表格里20 行一起闪
   会让人没法读数字，而这块信息靠的是下面那行实时速率，不是动画）。 */
.record-cache-progress__bar[data-state="running"] :deep(.arco-progress-line-bar),
.record-cache-progress__bar[data-state="merging"] :deep(.arco-progress-line-bar) {
  background: linear-gradient(90deg, var(--uvp-brand) 0%, var(--uvp-brand-cyan) 100%);
}

/* 整理中：录像已拉完、正在拼文件，用琥珀色把它和"还在拉流"区分开。 */
.record-cache-progress__bar[data-state="merging"] :deep(.arco-progress-line-bar) {
  background: linear-gradient(90deg, var(--uvp-warning) 0%, #d97706 100%);
}

.record-cache-progress__bar[data-state="succeeded"] :deep(.arco-progress-line-bar) {
  background: var(--uvp-success);
}

.record-cache-progress__bar[data-state="failed"] :deep(.arco-progress-line-bar) {
  background: var(--uvp-danger);
}

/* 终态但没有进度可言（已取消 / 已过期）：条别再显示成"卡在某个百分比"的样子，
   压成灰、并且降到半透明，让它读起来是"这条不用再看了"。 */
.record-cache-progress__bar[data-state="cancelled"] :deep(.arco-progress-line),
.record-cache-progress__bar[data-state="expired"] :deep(.arco-progress-line) {
  opacity: 0.55;
}

.record-cache-progress__bar[data-state="cancelled"] :deep(.arco-progress-line-bar),
.record-cache-progress__bar[data-state="expired"] :deep(.arco-progress-line-bar) {
  background: var(--uvp-text-tertiary);
}

.record-cache-progress__percent {
  flex: none;
  min-width: 34px;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  color: var(--zlm-text-2);
  text-align: right;
}

/* 失败行的百分比也跟着变红 —— 数字和条不一致时（比如条是灰的、数字是黑的）
   用户会以为数字是另一件事。判据挂在**外层 flex 行**上（`:has()` 在 happy-dom 里
   不生效、且多一层依赖），所以模板里给这行也写了同一个 `data-state`。 */
.record-cache-progress[data-state="failed"] .record-cache-progress__percent {
  color: var(--uvp-danger);
}

@media (width <= 760px) {
  .record-cache-pagination {
    flex-direction: column;
    align-items: flex-start;
  }
}
</style>
