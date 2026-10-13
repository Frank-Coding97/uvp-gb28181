<template>
  <div class="snow-fill">
    <div class="snow-fill-inner uvp-page-shell-flat snapshot-library-shell">
      <a-alert v-if="!canView" class="library-notice" type="warning">
        当前账号没有图像抓拍权限，无法查看图像库。请联系管理员分配「图像抓拍」（gb28181:device:snapshot）权限。
      </a-alert>

      <template v-else>
        <s-layout-search>
          <template #fields>
            <!-- 名称搜索放在第一位：原来第一个框要手打 20 位国标编码，
                 不知道编码的人打开页面就卡住了。 -->
            <a-input
              v-model="filters.keyword"
              data-testid="library-keyword"
              allow-clear
              placeholder="设备名 / 通道名 / 编码片段"
              style="width: 228px"
              @press-enter="query"
            />
            <a-select
              v-model="filters.source"
              data-testid="library-source"
              allow-clear
              placeholder="全部来源"
              style="width: 140px"
            >
              <a-option v-for="option in SNAPSHOT_SOURCE_OPTIONS" :key="option.value" :value="option.value">
                {{ option.label }}
              </a-option>
            </a-select>
            <a-range-picker
              v-model="filters.range"
              data-testid="library-range"
              show-time
              value-format="YYYY-MM-DDTHH:mm:ssZ"
              allow-clear
              style="width: 330px"
            />
            <!-- ⛔ 编码框已移除（老板 2026-10-06）：keyword 走**后端模糊匹配**，
                 设备名/通道名/别名/编码片段一框全中，再摆两个"20 位编码"输入框
                 只会让人以为必须手打才能查。

                 ⛔ 「批量选择」放在**筛选区**（老板 2026-10-06）：
                 它是"这次要看多少张"的前置开关，属于查询条件的一部分；
                 放工具条上则要等勾了选才出现，而勾选框本身又只在批量模式显示 ——
                 鸡生蛋，用户根本找不到从哪进入批量模式。 -->
            <a-button :type="batchMode ? 'primary' : 'secondary'" data-testid="library-batch-toggle" @click="toggleBatchMode">
              <template #icon><CheckSquare :size="15" /></template>
              {{ batchMode ? "退出批量选择" : "批量选择" }}
            </a-button>
          </template>
          <template #actions>
            <a-button type="primary" data-testid="library-query" :loading="loading" @click="query">
              <template #icon><Search :size="15" /></template>
              查询
            </a-button>
            <a-button data-testid="library-reset" :disabled="loading" @click="reset">
              <template #icon><RotateCcw :size="15" /></template>
              重置
            </a-button>
          </template>
        </s-layout-search>

        <!-- 会话视图：设备详情里跑完一次抓拍跳过来看这一批图。
             会话本身只活在内存里，刷新即丢，而图已落库 —— 这是重新聚起一次抓拍的唯一线索。 -->
        <div v-if="filters.sessionId" class="library-session" data-testid="library-session">
          <a-tag color="arcoblue" closable @close="clearSessionFilter">本次抓拍会话 {{ filters.sessionId }}</a-tag>
          <span class="library-session-note">只显示这一次会话上传的图（会话在刷新后不再保留，图片仍然在）</span>
        </div>

        <a-alert v-if="errorMessage" class="library-notice" type="error" closable @close="errorMessage = ''">
          {{ errorMessage }}
        </a-alert>

        <!-- 批量工具条：进入批量模式就出现。
             ⛔ 原来只在"有选中"时出现，但勾选框自己也在批量模式才渲染 ——
             于是"没勾选→工具条不显示→看不到已选几��"，用户会以为自己没选上。
             ⛔ 不用「全选全部匹配项」：总数可能上万，前端只拿到当前这一页，
             勾选一个自己都拉不到的集合等于骗人。范围限定在「本页」。 -->
        <div v-if="batchMode" class="library-batch" data-testid="library-batch-bar">
          <span class="library-batch-count">已选 {{ selectedIds.size }} 张</span>
          <a-button size="small" data-testid="library-select-all" @click="toggleSelectAll">
            {{ allSelected ? "取消本页全选" : "全选本页" }}
          </a-button>
          <a-button size="small" data-testid="library-clear-selection" @click="clearSelection">清空</a-button>
          <a-button
            size="small"
            type="primary"
            data-testid="library-batch-download"
            :disabled="downloading"
            @click="downloadSelected"
          >
            <template #icon><Download :size="14" /></template>
            {{ downloading ? "正在下载…" : "批量下载" }}
          </a-button>
          <!-- ⛔ 这句说明刻意写短：原句太长会在窄屏把后面的分页器挤到换行，
               而"会不会自动合成压缩包"这种问题用半透明小字反而更难读。 -->
          <span class="library-batch-note">逐张下载，不合成压缩包</span>
          <!-- ⛔ 删除是**不可恢复**的（后端连磁盘 jpg 一起删），所以必须：
               ① 用 `a-popconfirm` 而不是 `Modal.confirm`（后者在暗色下样式突兀、
                  且记忆里已有「弹层 prop 名不统一」的坑，popconfirm 少一层风险）；
               ② 文案写明**会连磁盘文件一起删**、要**写清张数**（"删 3 张"比"确定吗？"可核对）；
               ③ 删完必须**退出批量模式并清空选择** —— 否则 selectedIds 里
                  留着已被删掉的 id，工具条会一直显示"已选 3 张"而列表只剩 2 张。 -->
          <a-popconfirm
            v-if="selectedIds.size"
            :content="`确定删除这 ${selectedIds.size} 张抓拍图？磁盘上的图片文件也会一并删除，无法恢复。`"
            type="warning"
            :data-testid="`library-batch-delete-confirm-${selectedIds.size}`"
            @ok="deleteSelected"
          >
            <template #icon><Delete :size="15" /></template>
            <a-button size="small" type="error" data-testid="library-batch-delete" :loading="deleting" :disabled="deleting">
              {{ deleting ? "正在删除…" : "批量删除" }}
            </a-button>
          </a-popconfirm>
        </div>

        <div class="library-body">
          <div v-if="loading && !rows.length" class="library-state" data-testid="library-loading">正在加载抓拍图片…</div>
          <a-empty v-else-if="!rows.length" class="library-state" data-testid="library-empty" :description="emptyDescription" />
          <div v-else class="library-grid" data-testid="library-grid">
            <article
              v-for="item in rows"
              :key="item.id"
              class="library-card"
              :class="{
                'library-card--selected': batchMode && selectedIds.has(item.id),
                'library-card--active': detail?.id === item.id
              }"
              :data-snapshot-id="item.id"
            >
              <button
                type="button"
                class="library-thumb"
                :title="`查看详情 · ${item.fileName}`"
                :aria-label="`查看 ${item.channelName || item.channelCode} 的抓拍图`"
                @click="openDetail(item)"
              >
                <!-- ⛔ 取图接口在鉴权组内，src 必须经 imageUrlFor 补 ?token=，直连会整页 401 破图。 -->
                <img
                  v-if="!failedIds.has(item.id)"
                  :src="imageUrlFor(item)"
                  :alt="item.fileName"
                  loading="lazy"
                  decoding="async"
                  @error="markFailed(item.id)"
                />
                <span v-else class="library-thumb-failed"><ImageOff :size="18" />取图失败</span>
              </button>

              <div class="library-card-body">
                <!-- ⛔ 标题优先用**别名**（用户自己起的名字），回退才用设备上报名。
                     现场人员嘴里的"东门那个枪机"只对应 alias；而 name 是厂家串
                     （"IPC-HFW2431S"），摆在第一位没人认得。 -->
                <a-tooltip :content="channelLabel(item)" position="top">
                  <strong class="library-title text-ellipsis">{{ channelLabel(item) }}</strong>
                </a-tooltip>

                <!-- ⛔ 卡片骨架照抄设备列表的通道卡片（`.channel-card-info`）：
                     `68px minmax(0,1fr)` 的 label:value 竖排 + `.mono` 等宽值。
                     ⛔ 别自己另发明一套 —— 两个页面里的卡片长得不一样会被当成"两套观感"。
                     ⛔ 但图片区保留 `contain`：通道卡片的快照是**引流图**（cover 无所谓），
                     抓拍图是**取证材料**，裁掉画面上下缘就看不出现场拍全没有。 -->
                <div class="library-card-info">
                  <div>
                    <span>设备 ID</span>
                    <a-tooltip :content="String(item.deviceId)" position="top">
                      <strong class="mono text-ellipsis">{{ item.deviceId }}</strong>
                    </a-tooltip>
                  </div>
                  <div>
                    <span>通道 ID</span>
                    <a-tooltip :content="String(item.channelId)" position="top">
                      <strong class="mono text-ellipsis">{{ item.channelId }}</strong>
                    </a-tooltip>
                  </div>
                  <div>
                    <span>所属设备</span>
                    <a-tooltip :content="deviceLabel(item)" position="top">
                      <strong class="text-ellipsis">{{ deviceLabel(item) }}</strong>
                    </a-tooltip>
                  </div>
                  <!-- ⛔ 上报名只在与别名**确实不同**时列：别名为空时它就是标题，
                       再列一遍等于同一张卡片里出现两次同一个名字。 -->
                  <div v-if="showReportedName(item)">
                    <span>上报名</span>
                    <a-tooltip :content="channelReportedName(item)" position="top">
                      <strong class="text-ellipsis">{{ channelReportedName(item) }}</strong>
                    </a-tooltip>
                  </div>
                  <div>
                    <span>拍摄时刻</span>
                    <!-- ⛔ 完整到秒：设备补传时接收时刻与拍摄时刻能差几小时，
                         只显示"日期+时分"会让人对不上后端筛的 `captured_at`。 -->
                    <a-tooltip :content="item.capturedAt" position="top">
                      <strong class="text-ellipsis">{{ formatCapturedAt(item.capturedAt) }}</strong>
                    </a-tooltip>
                  </div>
                  <div>
                    <span>来源 / 大小</span>
                    <strong class="text-ellipsis"
                      >{{ snapshotSourceLabel(item.source) }} · {{ formatSnapshotSize(item.size) }}</strong
                    >
                  </div>
                </div>

                <div class="card-actions library-card-actions">
                  <!-- ⛔ 勾选框只渲染在**批量模式**下（老板 2026-10-06）：
                       浏览态是默认场景，每张图上都挂一个复选框会让人以为
                       「进来就要做选择」，而绝大多数人只是来找图看图的。
                       ⛔ 位置在卡片**左下角**（原来压在图片上）—— 压图会盖掉
                       画面左上角，而那正是判断"拍到没有"的关键区域。

                       ⛔⛔ **外面不能包 `<label>`**（老板 2026-10-07 报"勾第一张第二张也跟着勾"）。
                       真实 Arco 的 `<a-checkbox>` 渲染出的 `<input>` **没有 id**，
                       而 `<label>` 也没有 `for` ⇒ 点「选这张」文字时浏览器找不到
                       关联控件，**回退激活页面里第一个可聚焦元素** ⇒ 表现为
                       "点 A 结果勾上了 B"。`@click.stop` 挡得住冒泡、挡不住这个默认行为。
                       ✅ 复现：checkbox 的 snapshot 出现 `checked=true` 的不是我点的那一个。
                       ✅ `a-checkbox` 自带 `<label>` 包裹（它内部就是 label>input 结构），
                       点文字本来就能勾选，外层再包一层纯属多余。 -->
                  <a-checkbox
                    v-if="batchMode"
                    class="library-pick"
                    :model-value="selectedIds.has(item.id)"
                    :data-testid="`library-pick-${item.id}`"
                    @change="toggleSelect(item.id)"
                  >
                    选这张
                  </a-checkbox>
                  <span v-else class="library-actions-spacer" />
                  <a-link
                    class="uvp-table-action--primary"
                    :data-testid="`library-download-${item.id}`"
                    @click.stop="downloadOne(item)"
                  >
                    下载
                  </a-link>
                </div>
              </div>
            </article>
          </div>
        </div>

        <footer class="library-footer uvp-pagination-bar">
          <a-pagination
            data-testid="library-pagination"
            :current="pagination.page"
            :page-size="pagination.pageSize"
            :total="pagination.total"
            :page-size-options="SNAPSHOT_LIBRARY_PAGE_SIZE_OPTIONS"
            :disabled="loading"
            show-total
            show-page-size
            show-jumper
            @change="changePage"
            @page-size-change="changePageSize"
          />
        </footer>
      </template>
    </div>

    <!-- 详情抽屉 ⛔ 必须留在 .snow-fill 这一个根里：路由过渡只给「单元素根」挂钩子，
         根变成 Fragment 时淡入淡出会静默失效（见 index.layout.test.ts 的单根断言）。 -->
    <a-drawer
      body-class="uvp-system-dialog__body"
      :visible="detailVisible"
      :width="380"
      :footer="false"
      :esc-to-close="true"
      unmount-on-close
      class="uvp-system-drawer library-detail-drawer"
      @cancel="closeDetail"
    >
      <template #title>
        <span class="library-detail-title">{{ detail ? detailTitle(detail) : "抓拍详情" }}</span>
      </template>
      <template v-if="detail">
        <div class="library-detail">
          <div class="library-detail-preview">
            <!-- ⛔ 大图用 contain 而不是 cover：抓拍图是取证材料，
                 裁掉画面等于让人没法判断现场是否摆正。 -->
            <img :src="imageUrlFor(detail)" :alt="detail.fileName" />
          </div>

          <div class="library-detail-row">
            <span class="library-detail-label">通道</span>
            <span class="library-detail-value">{{ channelLabel(detail) }}</span>
          </div>
          <div class="library-detail-row">
            <span class="library-detail-label">设备</span>
            <span class="library-detail-value">{{ deviceLabel(detail) }}</span>
          </div>
          <div class="library-detail-row">
            <span class="library-detail-label">来源</span>
            <span class="library-detail-value">
              <a-tag size="small">{{ snapshotSourceLabel(detail.source) }}</a-tag>
            </span>
          </div>
          <!-- ⛔ 平台主键：报障/对账时对方要的是"哪一条记录"，
               光有 20 位编码没法直接定位库行。 -->
          <div class="library-detail-row">
            <span class="library-detail-label">设备 ID</span>
            <span class="library-detail-value library-detail-mono">{{ detail.deviceId }}</span>
          </div>
          <div class="library-detail-row">
            <span class="library-detail-label">通道 ID</span>
            <span class="library-detail-value library-detail-mono">{{ detail.channelId }}</span>
          </div>
          <!-- ⛔ 上报名只在与别名**确实不同**时列出：两者相同时列出来只会让人
               以为是两个不同的设备。 -->
          <div v-if="showReportedName(detail)" class="library-detail-row">
            <span class="library-detail-label">上报名</span>
            <span class="library-detail-value">{{ channelReportedName(detail) }}</span>
          </div>
          <div class="library-detail-row">
            <span class="library-detail-label">拍摄时刻</span>
            <span class="library-detail-value">{{ formatCapturedAt(detail.capturedAt) }}</span>
          </div>
          <div class="library-detail-row">
            <span class="library-detail-label">文件大小</span>
            <span class="library-detail-value">{{ formatSnapshotSize(detail.size) }}</span>
          </div>
          <div class="library-detail-row">
            <span class="library-detail-label">文件名</span>
            <span class="library-detail-value">
              <span class="library-detail-mono" :title="detail.fileName">{{ detail.fileName || "-" }}</span>
              <a-link
                v-if="detail.fileName"
                class="uvp-table-action--primary"
                :data-testid="`library-copy-name-${detail.id}`"
                @click="copyText(detail.fileName)"
              >
                复制
              </a-link>
            </span>
          </div>
          <div class="library-detail-row">
            <span class="library-detail-label">MD5</span>
            <span class="library-detail-value">
              <!-- ⛔ 界面只露头尾，复制给的是全量：比对文件时短了就没法比。 -->
              <span class="library-detail-mono" :title="detail.md5">{{ formatSnapshotMd5(detail.md5) }}</span>
              <a-link
                v-if="detail.md5"
                class="uvp-table-action--primary"
                :data-testid="`library-copy-md5-${detail.id}`"
                @click="copyText(detail.md5)"
              >
                复制
              </a-link>
            </span>
          </div>
          <div class="library-detail-row">
            <span class="library-detail-label">通道编码</span>
            <span class="library-detail-value library-detail-mono">{{ detail.channelCode }}</span>
          </div>
          <div class="library-detail-row">
            <span class="library-detail-label">设备编码</span>
            <span class="library-detail-value library-detail-mono">{{ detail.deviceCode }}</span>
          </div>

          <div class="library-detail-actions">
            <a-button type="primary" long data-testid="library-detail-download" @click="downloadOne(detail)">
              <template #icon><Download :size="15" /></template>
              下载这一张
            </a-button>
            <!-- ⛔ 这里**不能**直接 `toggleSelect` 改选中态：批量模式下勾选框才是唯一入口，
                 若抽屉里这个按钮能在浏览态就改选中，会出现"批量工具条上写着已选 1 张、
                 但页面上一个勾选框都没有"的状态 —— 用户没法看到也��撤销。 -->
            <a-button v-if="batchMode" long data-testid="library-detail-toggle-pick" @click="toggleSelect(detail.id)">
              {{ selectedIds.has(detail.id) ? "从批量中移除" : "加入批量下载" }}
            </a-button>
            <a-button v-else long data-testid="library-detail-enter-batch" @click="enterBatchMode">
              <template #icon><CheckSquare :size="15" /></template>
              加入批量选择
            </a-button>
          </div>
        </div>
      </template>
    </a-drawer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Message } from "@arco-design/web-vue";
import { CheckSquare, Delete, Download, ImageOff, RotateCcw, Search } from "@lucide/vue";
import { getAccessToken } from "@/utils/auth";
import { getBaseUrl } from "@/api/utils";
import { useUserStoreHook } from "@/store/modules/user";
import { deleteSnapshots, listSnapshotLibrary, type SnapshotLibraryItem } from "@/api/gb28181";
import {
  emptySnapshotFilters,
  formatCapturedAt,
  formatSnapshotMd5,
  formatSnapshotSize,
  mayViewSnapshotLibrary,
  normalizeSnapshotQuery,
  snapshotChannelLabel,
  snapshotChannelReportedName,
  snapshotContentImageUrl,
  snapshotDeviceLabel,
  snapshotDownloadName,
  snapshotFiltersFromRoute,
  snapshotQueryFromFilters,
  snapshotShowsReportedName,
  snapshotSourceLabel,
  SNAPSHOT_LIBRARY_PAGE_SIZE,
  SNAPSHOT_LIBRARY_PAGE_SIZE_OPTIONS,
  SNAPSHOT_SOURCE_OPTIONS
} from "./snapshotLibraryState";

/**
 * 图像库：按设备/通道/来源/时间段翻历史抓拍图。
 *
 * 与「设备详情 → 设备控制 → 图像抓拍配置」的分工：
 * 那边是**发起一次抓拍**（下发 SnapshotConfig、轮询会话到 completed）；
 * 这边是**回看已经存下来的图** —— 会话一出页面就散了，图才是长期存在的那个东西。
 *
 * ⛔ 数据源是列表接口（只出元数据 + 取图地址），缩略图逐张按需拉：
 * 一个通道一年的图能上万张，列表内联图片字节会把响应打爆。
 * ⛔ 取图接口在鉴权组里，`<img>` 带不了 Authorization 头 ⇒ 地址必须补 `?token=`（imageUrlFor）。
 */
const route = useRoute();
const router = useRouter();
const userStore = useUserStoreHook();

// 权限码与菜单可见性、两个接口的 casbin 授权**同码**，三处必须一致。
const canView = computed(() => mayViewSnapshotLibrary(userStore.account.permissions));

const filters = reactive(snapshotFiltersFromRoute(route.query));
const rows = ref<SnapshotLibraryItem[]>([]);
const loading = ref(false);
const errorMessage = ref("");
const failedIds = ref(new Set<number>());
const accessToken = ref(getAccessToken()?.accessToken ?? "");
const pagination = reactive({ page: 1, pageSize: SNAPSHOT_LIBRARY_PAGE_SIZE, total: 0 });
const detailVisible = ref(false);
const detail = ref<SnapshotLibraryItem | null>(null);
const selectedIds = ref(new Set<number>());
const downloading = ref(false);
const deleting = ref(false);

/**
 * 批量选择模式开关（老板 2026-10-06）。
 *
 * ⛔ 勾选框**只在批量模式渲染**，不是默认就有：
 * 浏览态是主路径，每张图上挂一个复选框会让人以为"进来就得做选择"，
 * 而绝大多数人只是来找图看图的。
 * ⛔ 因此它**不能进 URL**：它是"这一次要不要选"的操作态，不是筛选条件 ——
 * 写进 URL 会让别人打开链接就进入一个莫名其妙的批量态。
 */
const batchMode = ref(false);

function toggleBatchMode() {
  if (batchMode.value) {
    exitBatchMode();
    return;
  }
  enterBatchMode();
}

function enterBatchMode() {
  batchMode.value = true;
}

function exitBatchMode() {
  batchMode.value = false;
  // ⛔ 退出时必须清空已选：下一次进入时若还留着上一次的 id，
  // 「已选 N 张」会凭空出现而用户没勾过任何一张 —— 批量下载直接下错文件。
  clearSelection();
}

/**
 * 空态文案分两种：条件太多 vs 一个都没有。
 * ⛔ 不加区分的话"没搜到"和"库里本来就没图"长成一句话，
 * 用户会去反复改筛选条件，而实际问题在部署侧。
 */
const emptyDescription = computed(() => {
  if (filters.keyword.trim() || filters.source || filters.range?.length) {
    return "没有符合当前条件的抓拍图片，试试放宽时间范围或清空关键词";
  }
  return "图像库还是空的：先在设备详情里发起一次图像抓拍";
});

// 请求序号：筛选/翻页连点时只认最后一次响应，否则慢的旧响应会把新结果盖掉。
let requestToken = 0;

// 本组件自己写 URL 时置位：watcher 看到它就跳过，避免"改条件 → 写 URL →
// watcher 认定是新深链 → 又查一遍"的**双请求**与结果抖动。
// ⛔ 声明必须排在 syncRouteFilters 之前：赋值点在这条链上先于声明会撞 TDZ
// （"Cannot access before initialization"），而且只在特定点击顺序下才复现。
let selfSyncedUrl = false;

function imageUrlFor(item: SnapshotLibraryItem) {
  return snapshotContentImageUrl(item.url, accessToken.value, getBaseUrl());
}

function markFailed(id: number) {
  const next = new Set(failedIds.value);
  next.add(id);
  failedIds.value = next;
}

function openDetail(item: SnapshotLibraryItem) {
  detail.value = item;
  detailVisible.value = true;
}

function closeDetail() {
  detailVisible.value = false;
}

function detailTitle(item: SnapshotLibraryItem) {
  return snapshotChannelLabel(item);
}

/** 卡片刻意露出**平台主键** deviceId/channelId：报障时对方要的是"哪一条记录"。 */
function channelLabel(item: SnapshotLibraryItem) {
  return snapshotChannelLabel(item);
}

function deviceLabel(item: SnapshotLibraryItem) {
  return snapshotDeviceLabel(item);
}

function showReportedName(item: SnapshotLibraryItem) {
  return snapshotShowsReportedName(item);
}

function channelReportedName(item: SnapshotLibraryItem) {
  return snapshotChannelReportedName(item);
}

function toggleSelect(id: number) {
  const next = new Set(selectedIds.value);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  selectedIds.value = next;
}

const allSelected = computed(() => rows.value.length > 0 && rows.value.every(item => selectedIds.value.has(item.id)));

function toggleSelectAll() {
  const next = new Set(selectedIds.value);
  if (allSelected.value) rows.value.forEach(item => next.delete(item.id));
  else rows.value.forEach(item => next.add(item.id));
  selectedIds.value = next;
}

/**
 * 批量删除已选抓拍图（老板 2026-10-07 要求）。
 *
 * ⛔ 不可恢复：后端连磁盘上的 jpg 一起删。
 * ⛔ 删完**必须退出批量模式 + 清空选择**：selectedIds 里若还留着已删掉的 id，
 * 工具条会一直显示"已选 3 张"而列表只剩 2 张，用户会以为删失败又去点删除。
 * ⛔ 只在**当前页**可见范围内选（selectedIds 本身就是本页勾选的），删完重新拉列表，
 * 不要前端自行 filter —— 后端才是权威（可能有越权行被后端跳过）。
 */
async function deleteSelected() {
  if (deleting.value) return;
  const ids = [...selectedIds.value];
  if (!ids.length) return;
  deleting.value = true;
  try {
    const res = await deleteSnapshots(ids);
    const result = res?.data;
    // ⛔ `filesFailed > 0` 要**单独提示**：库行删了但磁盘文件还在，
    // 用户以为清干净了，磁盘却在悄悄堆积。
    if (result?.filesFailed) {
      Message.warning(`已删除 ${result.deleted} 条记录，但有 ${result.filesFailed} 个图片文件未能删除（可在服务器上手动清理）`);
    } else {
      Message.success(`已删除 ${result?.deleted ?? ids.length} 张抓拍图`);
    }
    exitBatchMode();
    await loadRows();
  } catch (error) {
    // ⛔ 失败时**保留选择**：用户点重试就能再试一次，
    // 清空选择等于让他重新勾一遍。
    // ⛔ 这里在 `<script>` 里，必须写 `errorMessage.value`：模板里能自动解包，
    // 脚本里写 `errorMessage = ...` 是给 const 赋值 ⇒ TS2588 编译报错。
    errorMessage.value = (error as Error)?.message || "删除失败";
    Message.error(errorMessage.value);
  } finally {
    deleting.value = false;
  }
}

function clearSelection() {
  selectedIds.value = new Set();
}

/**
 * 浏览器原生下载。
 *
 * ⛔ 走 `<a download>` 而不是 fetch + blob：后者要在内存里攒下整张图，
 * 批量下载十几张时会把标签页内存吃满并可能让页面卡死。
 * ⛔ 必须带 token：取图接口在鉴权组里，而 `<a>` 同样带不了 Authorization 头，
 * 所以地址得经过 imageUrlFor（补 `?token=`）。
 * ⛔ 延时逐个触发：浏览器会把连续多次的下载当成弹窗拦掉，
 * 一次性点 N 个只会成功第一个。间隔一点才能让 N 张都落到下载目录。
 */
function triggerNativeDownload(url: string, fileName: string) {
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = fileName;
  anchor.rel = "noopener";
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
}

function downloadOne(item: SnapshotLibraryItem) {
  const url = imageUrlFor(item);
  if (!url) return;
  triggerNativeDownload(url, snapshotDownloadName(item));
}

async function downloadSelected() {
  const targets = rows.value.filter(item => selectedIds.value.has(item.id));
  if (!targets.length) return;
  downloading.value = true;
  try {
    for (const [index, item] of targets.entries()) {
      triggerNativeDownload(imageUrlFor(item), snapshotDownloadName(item));
      // 间隔 250ms：低于这个间隔浏览器会把后续下载判成"非用户触发"直接拦掉。
      if (index < targets.length - 1) await new Promise(resolve => window.setTimeout(resolve, 250));
    }
    Message.success(downloadingMessage(targets.length));
  } finally {
    downloading.value = false;
  }
}

function downloadingMessage(count: number) {
  return `已发起 ${count} 张图片的下载，请查看浏览器下载列表`;
}

/**
 * 复制到剪贴板。
 * ⛔ `navigator.clipboard` 只在**安全上下文**（https 或 localhost）可用：
 * 通过局域网 IP + http 打开时它是 undefined，直接调会抛异常，
 * 所以必须兜一个 textarea + execCommand 的老办法。
 */
async function copyText(value: string) {
  const text = value.trim();
  if (!text) return;
  try {
    if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      Message.success("已复制");
      return;
    }
    const area = document.createElement("textarea");
    area.value = text;
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.appendChild(area);
    area.select();
    document.execCommand("copy");
    area.remove();
    Message.success("已复制");
  } catch {
    // ⛔ 复制失败不能只弹提示然后让人猜：把值留在 title 里，用户还能手动选中。
    Message.warning("浏览器拒绝了剪贴板访问，请手动选中复制");
  }
}

async function loadRows() {
  if (!canView.value) return;
  const token = ++requestToken;
  loading.value = true;
  errorMessage.value = "";
  // 每次都重读 token：access token 刷新之后旧值会让整页缩略图 401。
  accessToken.value = getAccessToken()?.accessToken ?? "";
  failedIds.value = new Set();
  try {
    const response = await listSnapshotLibrary(normalizeSnapshotQuery(filters, pagination.page, pagination.pageSize));
    if (token !== requestToken) return;
    rows.value = response.data?.list ?? [];
    pagination.total = response.data?.total ?? 0;
  } catch (error) {
    if (token !== requestToken) return;
    rows.value = [];
    pagination.total = 0;
    errorMessage.value = "查询图像库失败，请稍后重试";
    console.error(error);
  } finally {
    if (token === requestToken) loading.value = false;
  }
}

/**
 * 查询：条件写回 URL。
 *
 * ⛔ 原来只有会话/编码进 URL，于是"把这批图发给同事"丢掉的正是对方最需要的
 * 那个条件（时间窗/来源/关键词）。刷新一次页面也会全部退回。
 */
function query() {
  pagination.page = 1;
  // ⛔ 改筛选条件**不**退出批量模式：用户的常见动作是"换一批范围继续勾"，
  // 退出批量会让他每次改条件都要重新点一次「批量选择」。
  // 但**已选必须清** —— 上一页的 id 在新结果里可能根本不存在。
  // ⛔ 与 `reset()` 的区别就在这里：重置是"从头开始"，才连批量模式一起退。
  clearSelection();
  void syncRouteFilters();
  void loadRows();
}

function syncRouteFilters() {
  const query = snapshotQueryFromFilters(filters);
  // ⛔ 页码也进 URL：只带条件不带页码时，刷新/分享停在第 1 页，
  // 而对方以为你给的就是你正在看的那一屏。
  if (pagination.page > 1) query.page = String(pagination.page);
  // 只在真的不一致时才 replace：相同 query 再 replace 一次会白跑一遍 watcher。
  const current = route.query as Record<string, unknown>;
  const same =
    Object.keys(query).length === Object.keys(current).length &&
    Object.entries(query).every(([key, value]) => firstRouteValue(current[key]) === value);
  if (same) return Promise.resolve();
  selfSyncedUrl = true;
  return router.replace({ query });
}

function firstRouteValue(value: unknown) {
  return Array.isArray(value) ? String(value[0] ?? "") : typeof value === "string" ? value : "";
}

function clearRouteFilters() {
  // 深链带进来的条件(会话/编码)在重置后要一起清掉，否则"重置了但 URL 还写着"
  // ——再分享出去的人会看到被你重置掉的筛选。
  if (Object.keys(route.query).length) return router.replace({ query: {} });
  return Promise.resolve();
}

function reset() {
  Object.assign(filters, emptySnapshotFilters());
  pagination.page = 1;
  // ⛔ 重置必须一并退出批量模式：重置的语义是"从头开始看"，
  // 而批量模式是一次性的操作态 —— 留着它会出现"筛选都清了、卡片上还挂着
  // 一排勾选框和『已选 7 张』"，用户以为重置没生效。
  exitBatchMode();
  void clearRouteFilters().then(() => loadRows());
}

function clearSessionFilter() {
  filters.sessionId = "";
  query();
}

function changePage(nextPage: number) {
  pagination.page = nextPage;
  // ⛔ 翻页**不清空**已选：批量下载的典型用法就是"在第1页勾几张、切到第3页再勾几张"。
  // 连带把页码写进 URL，刷新后停在原来那一页而不是跳回第一页。
  void syncRouteFilters();
  void loadRows();
}

function changePageSize(nextPageSize: number) {
  pagination.pageSize = nextPageSize;
  pagination.page = 1;
  clearSelection();
  void syncRouteFilters();
  void loadRows();
}

function applyRouteFilters() {
  Object.assign(filters, snapshotFiltersFromRoute(route.query));
  // 深链带页码时把它读回来，否则刷新"第3页的链接"会跳回第1页。
  const rawPage = Number(firstRouteValue(route.query.page));
  pagination.page = Number.isInteger(rawPage) && rawPage > 0 ? rawPage : 1;
}

// 已经在图像库页面上时又点了一次"按会话查看"（例如从设备详情返回）——路由 query 变化即重载。
// ⛔ 只在 query 里确实带了定位条件时才动，否则 reset()/clearSessionFilter() 里那次 replace
// 会被当成一次新查询，白跑一遍（甚至把刚重置的筛选又灌回去）。
watch(
  () => route.query,
  next => {
    if (selfSyncedUrl) {
      selfSyncedUrl = false;
      return;
    }
    // 判据扩到全部可分享条件：原来只看会话/编码，于是"把带时间窗的链接贴给别人"后
    // 本页自己那次 URL 变化反被当成外部深链，行为取决于从哪进来，不可预期。
    const keys = ["sessionId", "channelCode", "deviceCode", "source", "keyword", "from", "to"];
    if (!keys.some(key => next[key])) return;
    applyRouteFilters();
    clearSelection();
    void loadRows();
  }
);

onMounted(() => {
  applyRouteFilters();
  if (canView.value) void loadRows();
});
</script>

<style scoped>
.snapshot-library-shell {
  display: flex;
  flex-direction: column;
  gap: 12px;
  height: 100%;
  min-height: 0;
}
.library-notice {
  margin: 0;
}
.library-session {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}
.library-session-note {
  font-size: 11.5px;
  color: var(--uvp-text-tertiary);
}
.library-body {
  flex: 1;
  min-height: 0;
  overflow: auto;
}
.library-state {
  padding: 48px 0;
  color: var(--uvp-text-tertiary);
  text-align: center;
}

/* ⛔ 列宽/gap 对齐设备列表的通道卡片（`minmax(280px,1fr)` + `gap:12px`）：
   两个页面里的卡片网格宽度不同会明显看得出"这是另一个页面的卡片"。 */
.library-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  grid-auto-rows: max-content;
  gap: 12px;
  align-content: start;
  align-items: start;
  padding: 2px;
}
.library-batch {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
  padding: 8px 12px;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
}
.library-batch-count {
  font-size: 13px;
  font-weight: 500;
  color: var(--uvp-text-primary);
}
.library-batch-note {
  font-size: 11.5px;
  color: var(--uvp-text-tertiary);
}

/* ⛔ 外壳照抄设备列表的 `.device-card`：图片区与 body 之间**靠 border-bottom 分隔**，
   卡片本身不排 gap、不带 padding（padding 归 body）。自己另发明一套会让
   "两个页面的卡片"看起来像两种组件。 */
.library-card {
  position: relative;
  overflow: hidden;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 12px;
}
.library-card:hover {
  border-color: var(--uvp-primary);
}

/* ⛔ 选中态用左侧实心条而不是整块高亮底色：底色会盖住缩略图本身的明暗，
   而缩略图的对比度正是判断"这张图看不清"的关键依据。 */
.library-card--selected {
  border-color: var(--uvp-primary);
  box-shadow: inset 3px 0 0 var(--uvp-primary);
}
.library-card--active {
  border-color: var(--uvp-primary);
}

/* ⛔ 勾选框现在是**卡片左下角的行内元素**，不再是压在图片上的浮层
   （老板 2026-10-06）：压图会盖住画面左上角，而那正是判断"拍到没有"的关键区域。
   ⛔ 不再需要半透明底 + 白描边 + z-index —— 那些是为"浮在图上"才需要的。 */
.library-pick {
  display: inline-flex;
  align-items: center;
  cursor: pointer;
}
.library-actions-spacer {
  margin-right: auto;
}
.library-thumb {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;

  /* ⛔ 固定高 148px（对齐通道卡片的 `.channel-snapshot`）而不是 `aspect-ratio`：
     通道列表与图像库并排放在同一屏时，等比卡片会因为图幅不同而高低不齐。
     ⛔ 但**必须留白**（`contain`）：抓拍图是取证材料，cover 裁掉画面上下缘
     就看不出现场是否摆正、有没有拍全。 */
  height: 148px;
  padding: 0;
  overflow: hidden;

  /* ⛔⛔ 四边**必须**显式清零。图片区是 `<button>`，浏览器默认给
     `border: 2px outset rgb(255,255,255)` + `appearance: button`。
     ⛔ 只写 `border-bottom` 只覆盖**下边**，上/左/右仍是那圈白色 outset
     ⇒ 深色页面上卡片外围出现一圈刺眼白框（老板 2026-10-06 二次反馈）。
     ⭐ 判据：量 `getComputedStyle(btn).borderTop/Left/Right`，
     出现 `outset` / `2px` / 白色 ⇒ 就是这个坑。 */
  appearance: none;
  cursor: pointer;

  /* ⛔ 底色用**卡片底色**（`--uvp-panel-bg`），不是 `--uvp-shell-muted`：
     竖图（实测 2252×4000，AR 0.56）在 300×148 的横框里只能填 **28% 宽度**，
     左右各留约 36% 的空隙。空隙露出的底色若与卡片底色不同，深色下就表现为
     "卡片两侧各有一道竖直亮缝"，看着像卡片被切成三段。
     ⭐ 判据：算 `naturalWidth/naturalHeight` 与容器 AR 的比，填充率 <60% 就会看到留白。 */
  background: var(--uvp-panel-bg);
  border: 0;

  /* ⛔ 只留**下**边框作为"图片区与信息区分隔"（对齐 `.channel-snapshot` 的 `border-bottom`）。 */
  border-bottom: 1px solid var(--uvp-panel-border);
}

/* ⛔ 改成 contain 而不是 cover：抓拍图是取证材料，硬裁会切掉画面上下缘，
   判断"现场是否摆正 / 有没有拍全"就变成必须点开大图才知道。
   ⛔ **不要给 <img> 加 outline/border**：元素本身是 `width:100%` 的横框，
   `contain` 把画面缩在中间 ⇒ 描边画在**留白的外沿**，会在图片两侧各画出一条
   与边框同色的竖线，看着像"图片被切成三条"。 */
.library-thumb img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: contain;
}
.library-thumb-failed {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  font-size: 11.5px;
  color: var(--uvp-danger);
}

/* ⛔ 以下四块（body/标题/信息区/操作区）逐条对齐设备列表的 `.channel-card-*`：
   同一个平台里两种卡片长得不一样，会被当成"两套观感"。 */
.library-card-body {
  display: grid;
  gap: 10px;
  padding: 11px 12px 12px;
}
.library-title {
  min-width: 0;
  font-size: 14px;
  line-height: 20px;
  color: var(--uvp-text-primary);
}

/* label:value 竖排：68px 是设备列表实测值（中文标签 4 个字 + 冒号不断行）。 */
.library-card-info {
  display: grid;
  grid-template-columns: 68px minmax(0, 1fr);
  gap: 5px 8px;
  min-width: 0;
  font-size: 12px;
  line-height: 18px;
}
.library-card-info > div {
  display: contents;
}
.library-card-info span {
  color: var(--uvp-text-tertiary);
}
.library-card-info strong {
  min-width: 0;
  font-weight: 500;
  color: var(--uvp-text-primary);
}

/* ID 用等宽：报障时要逐位核对，"1/l"、"0/O" 在比例字体里看错。
   ⛔ 字体栈与设备列表 `.channel-card-info .mono` **逐字一致** ——
   两处字体栈不同时，同一个平台里两页的卡片粗细会差一档。
   ⛔ `text-ellipsis` 同样是 scoped 本地类（不是全局），必须在本文件重新定义，
   否则模板里那个 class 什么都不做（不报错，值直接铺出去撑破布局）。 */
.library-card-info .mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}
.library-card-info .text-ellipsis,
.library-title.text-ellipsis {
  display: block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.library-card-actions {
  display: flex;
  gap: 12px;
  align-items: center;

  /* ⛔ 不用 `justify-content: flex-end`：批量模式下左端是勾选框、
     右端是"下载"，两端分置靠 `margin-right:auto` 的占位撑开。
     保持 flex-end 会让两者都挤在右侧，勾选框被顶到中间。 */
  min-height: 24px;
}
.library-footer {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  justify-content: flex-end;
  padding-top: 2px;
}
.library-detail {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.library-detail-title {
  font-size: 15px;
  font-weight: 500;
  color: var(--uvp-text-primary);
}
.library-detail-preview {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 8px;
  background: var(--uvp-shell-muted);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
}
.library-detail-preview img {
  display: block;
  width: 100%;
  max-height: 260px;
  object-fit: contain;
}
.library-detail-row {
  display: flex;
  gap: 12px;
  align-items: baseline;
  justify-content: space-between;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--uvp-panel-border);
}
.library-detail-label {
  flex-shrink: 0;
  font-size: 12.5px;
  color: var(--uvp-text-tertiary);
}
.library-detail-value {
  display: flex;
  gap: 8px;
  align-items: baseline;
  justify-content: flex-end;
  min-width: 0;
  font-size: 13px;
  color: var(--uvp-text-primary);
  text-align: right;
  word-break: break-all;
}

/* 编码/md5 用等宽：它们要靠肉眼比对字符，逐个核对时比例字体会看错。 */
.library-detail-mono {
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: 12px;
}
.library-detail-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-top: 4px;
}

@media (width <= 900px) {
  .library-grid {
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  }
  .library-footer {
    justify-content: center;
  }
}
</style>
