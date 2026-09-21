<script setup lang="ts">
/**
 * DeviceConfigOsdBlocks - OSD 组的**对象块**形态（时间戳 / 叠加文字 / 只读坐标画布）。
 *
 * ## 为什么不是通用字段表
 *
 * `deviceConfigGroups.ts` 的通用渲染是「一行一个字段：标签 + 控件」。那套形态适合
 * 基本参数、录像计划这类**平铺的参数表**，但 OSD 不是参数表 —— 它是**画面上摆的两样东西**：
 * 一个时间戳、若干行字。用户的任务是"把那行字挪个地方"，不是"改第 5 行的 X 值"。
 * 摊成 8 行字段的后果（2026-09-20 重做前）：
 *
 *   - 位置只能靠盲输：两个滑杆 + 每条文字各自一对 X/Y 输入框，画布上**一个标记都没有**；
 *   - 「窗口长度 / 窗口宽度」是**伪可编辑**字段（设备拒收平台改写，见 `buildOSD` 注释），
 *     留着就是让人点了没用；
 *   - 一个「时间戳」被拆到字段表的四处（开关 ①、格式 ②、位置 ⑤⑥，中间隔着两个画布尺寸）。
 *
 * ## 版式（2026-09-20 第二版：老板给了参考图，要求"规整、文字清晰"、"按钮放进时间戳面板"）
 *
 * 统一成**标签列 + 控件列**两栏：`格式 | <下拉框>`、`位置 | <按钮 + 坐标>`、
 * `画布 | <尺寸 + 只读>`。加粗对齐、统一行高，扫一眼能顺着标签列找到东西。
 *
 * ⛔ **时间戳格式从单选改下拉**（老板 2026-09-20）：三条竖排单选占掉整块面板的一半高度，
 *    而这是个"三选一"的枚举，下拉框是它本来的形态。样例照旧是**当前时刻的实例**
 *    （见 `refreshSamples`），选完在收起状态也能看见自己选的是什么。
 *
 * ⛔ **「调整位置 / 完成调整」按钮搬进「时间戳」的「位置」行**（2026-09-20）。
 *    它曾经在画面右上角（压画面，老板否掉），又搬到画面下方工具条（仍要视线来回跳）。
 *    真正的归属地就是这里：**改的是这一行的坐标**，按钮和坐标读数放在同一行，
 *    "点它 → 画面上出现标记 → 拖"三步在同一条视线上。
 *
 * ## 三条硬约束
 *
 * 1. ⛔ **不做"像真字"的预览**。标准 `OSDCfgType`（A.2.1.12）9 个字段里没有字体、
 *    字号、颜色、对齐 —— 画出来就是在承诺平台给不了的能力。而且设备**已经烧进码流**
 *    的时间戳就在播放器画面里，再叠一个假字会出现**两个时间戳**。
 *    所以这里只管「位置 + 内容 + 开关」，画布侧用锚点表示（见 PlayConsoleLinked 的 osd 层）。
 * 2. ⛔ **`Length` / `Width` 是只读事实**，不是可编辑配置。真机实测设备拒收平台改写
 *    （写 2560×1440 也回 200 OK、回读仍是 704×576），而它们是遮挡坐标的基准。
 * 3. ⛔ **开关关掉时字段灰显但不隐藏**：标准里 `TimeEnable=0` 时 `TimeX/TimeY/TimeType`
 *    照样在报文里（`buildOSD` 一律带全）。配置是"还在、只是不显示"，隐藏会让用户以为配置丢了。
 */
import { ChevronDown as ChevronDownIcon, CheckCircle2, Crosshair, Plus, Trash2 } from "@lucide/vue";
import { nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import {
  MAX_OSD_TEXT_LENGTH,
  OSD_TIME_TYPE_OPTIONS,
  POINT_AXES,
  type ConfigSelectOption,
  type ConfigTextItem
} from "./deviceConfigGroups";

const props = withDefaults(
  defineProps<{
    timeEnable: boolean;
    /** `TimeType`：`""` = 跟设备走（不发格式）、`"0"` / `"1"` = 两种格式。 */
    timeType: string;
    timeX: string;
    timeY: string;
    textEnable: boolean;
    items: ConfigTextItem[];
    /** 设备声明的坐标画布（`null` = 本次没读到 —— **不是**平台的 1920×1080 模板）。 */
    canvas: { width: number; height: number } | null;
    disabled?: boolean;
    maxItems: number;
    /** 本面板是否处在"画布上可拖动"的编辑模式（由画面侧持有，见 `PlayConsoleLinked`）。 */
    editing?: boolean;
    /**
     * 本面板外面**有没有一块可拖的画布**。
     *
     * ⛔ 不是所有宿主都有画布（`DeviceConfigDemo` 那个免登录预览页就只有面板）。
     *    没有画布还必须渲染「调整位置」，点下去画面上什么都不会发生 —— 一个"点了没用"
     *    的按钮比没有按钮更糟。没有画布时位置只能靠下面的精确数值改。
     */
    canvasLinked?: boolean;
    /**
     * 版式：`stack`（默认，侧栏里竖排） / `row`（2026-09-20 加，播放控制台底栏用）。
     *
     * ⭐ 底栏把这一整块摆在**画面下方**（老板要求"一个页面全展示"），那里是"矮而宽"的容器：
     *    竖排会把「叠加文字」挤到 148px 的高度之外。行版式把「时间戳」「叠加文字」并排，
     *    画布事实压成整行的一条 —— 同一块内容，两种排法。
     * ⛔ 别把行版式做成"另一套面板"：只换栅格，控件、写口、文案一个字都不换。
     */
    layout?: "stack" | "row";
  }>(),
  { disabled: false, canvas: null, editing: false, canvasLinked: false, layout: "stack" }
);

const emit = defineEmits<{
  "update:timeEnable": [value: boolean];
  "update:timeType": [value: string];
  "update:timeX": [value: string];
  "update:timeY": [value: string];
  "update:textEnable": [value: boolean];
  "update:items": [value: ConfigTextItem[]];
  /** 请画布把对应锚点闪一下（把视线引过去）。 */
  locate: [payload: { kind: "time" | "item"; index: number }];
  /** 进 / 出「调整位置」编辑模式（状态在画面侧持有）。 */
  toggleEdit: [];
}>();

const maxLength = MAX_OSD_TEXT_LENGTH;

/* ─────────────── 时间格式：把模式串渲染成**当前时刻的实例** ─────────────── */

function pad2(value: number): string {
  return String(value).padStart(2, "0");
}

/** `YYYY-MM-DD HH:mm:ss` 这类模板 → 传入时刻的实例。只认 `OSD_TIME_TYPE_OPTIONS.sample` 里的 token。 */
function sampleFromTemplate(template: string, at: Date): string {
  return template
    .replace("YYYY", String(at.getFullYear()))
    .replace("MM", pad2(at.getMonth() + 1))
    .replace("DD", pad2(at.getDate()))
    .replace("HH", pad2(at.getHours()))
    .replace("mm", pad2(at.getMinutes()))
    .replace("ss", pad2(at.getSeconds()));
}

function sampleText(template: string): string {
  return sampleFromTemplate(template, new Date());
}

/** 下拉项的文案：能渲染成实例的用实例，其余用声明里的原话。 */
function optionText(option: ConfigSelectOption): string {
  return option.sample ? sampleText(option.sample) : option.label;
}

/**
 * 秒级刷新**只动这几个 option 的文本节点**，不碰组件状态。
 *
 * ⛔ 别改成 `const now = ref(new Date())` + `setInterval(() => now.value = new Date())`：
 *    那会让整个块（含每行文字的输入框）每秒走一次重渲染，用户正在输入时会掉光标。
 *    这里要的是"这一秒的秒数变了"，不是"面板要重画"。
 * ⛔ 也不用 v-for 里的数组模板引用：那玩意儿在列表长度变化时不会自动收缩，
 *    而且真正的目标只是"找到这几个节点改文本"。
 */
const rootEl = ref<HTMLElement | null>(null);
let sampleTimer: number | undefined;

function refreshSamples() {
  const host = rootEl.value;
  if (!host) return;
  const at = new Date();
  // ⛔ 用 `Array.from` 而不是直接 for...of：本仓的 tsconfig 没开 `DOM.Iterable`，
  //    `NodeListOf` 上没有 `Symbol.iterator`（`vue-tsc` 会直接报 TS2488）。
  for (const el of Array.from(host.querySelectorAll<HTMLElement>(".osd-fmt-sample"))) {
    const template = el.dataset.sample;
    if (template) el.textContent = sampleFromTemplate(template, at);
  }
}

onMounted(() => {
  refreshSamples();
  sampleTimer = window.setInterval(refreshSamples, 1000);
});

onBeforeUnmount(() => {
  if (sampleTimer !== undefined) window.clearInterval(sampleTimer);
});

/* ─────────────── 位置行 ─────────────── */

/**
 * 精确数值默认收起 —— 有画布时主要操作是"在画面上拖"，数字是二级表示。
 * ⛔ 没有画布（预览页）时**没有拖这条路**，此时默认展开，否则用户没有别的入口改坐标。
 */
const posExpanded = ref(!props.canvasLinked);

function commitAxis(axis: "x" | "y", raw: string) {
  // ⛔ 原样传字符串、不在这里 clamp：空串与越界要能在 `buildOSD` 那一层被统一判掉，
  //    在这里夹取等于把"用户填了 99999"静默改写成 8192，界面上看起来还是他填的那个数。
  // ⛔ 两个事件分开 emit，别写成 `emit(axis === "x" ? "update:timeX" : "update:timeY", ...)`：
  //    类型化的 emits 收窄不了这个联合（`vue-tsc` 报 TS2769）。
  if (axis === "x") emit("update:timeX", String(raw).trim());
  else emit("update:timeY", String(raw).trim());
}

function posValue(axis: "x" | "y"): string {
  return axis === "x" ? props.timeX : props.timeY;
}

/* ─────────────── 叠加文字 ─────────────── */

function rows(): ConfigTextItem[] {
  return Array.isArray(props.items) ? props.items : [];
}

function commitRows(next: ConfigTextItem[]) {
  emit("update:items", next);
}

function setText(index: number, raw: string) {
  commitRows(rows().map((row, position) => (position === index ? { ...row, text: raw } : { ...row })));
}

/**
 * 加一行：**不预填坐标**，并显式标成「未定位」。
 *
 * ⛔ 坐标是使用者的决定，预填一个看起来合理的值会让人以为"平台帮我对齐了"。
 * ⛔ `placed: false` 是必须的：`0,0` 是合法坐标，光看数值分不出"摆了左上角"和
 *    "还没摆"，而 `buildOSD` 要靠它拦住未定位的行（否则设备上会多一行贴左上角的字）。
 */
async function addRow() {
  if (props.disabled || rows().length >= props.maxItems) return;
  const index = rows().length;
  commitRows([...rows(), { text: "", x: 0, y: 0, placed: false }]);
  // 焦点落到新行的输入框：点了「加一行字」却要用户再找一下输入框，是白丢一步。
  await nextTick();
  const input = document.querySelector<HTMLInputElement>(`[data-testid="osd-text-${index}"]`);
  input?.focus();
}

function removeRow(index: number) {
  commitRows(rows().filter((_, position) => position !== index));
}

function charCount(text: string): number {
  // 按**字符**数（不是字节）：标准写的是"长度 0~32"，中文一个字占 3 字节。
  return [...String(text ?? "")].length;
}

function isUnplaced(row: ConfigTextItem): boolean {
  return row.placed === false;
}

/**
 * 格式选项的 `data-testid` 后缀。
 *
 * ⛔ 空值（「跟设备走」）不能原样拼进 testid —— 会得到 `osd-fmt-` 这么个带空尾巴的定位符，
 *    用例里写出来谁也看不出它指的是哪一项。
 */
function fmtKey(value: string): string {
  return value === "" ? "follow" : value;
}
</script>

<template>
  <div ref="rootEl" class="osd-blocks" :class="{ 'is-disabled': disabled, 'is-row': layout === 'row' }">
    <!-- ═══════════ 对象块一：时间戳 ═══════════ -->
    <section class="osd-card" data-testid="osd-block-time">
      <header class="osd-card-head">
        <span class="osd-card-title">时间戳</span>
        <!-- ⛔ 关闭 ≠ 配置丢了：字段灰显保留，状态写在标题这一行。 -->
        <span v-if="!timeEnable" class="osd-card-off" data-testid="osd-time-off">已关闭</span>
        <button
          type="button"
          class="osd-switch"
          :class="{ 'is-on': timeEnable }"
          :disabled="disabled"
          :aria-pressed="timeEnable"
          aria-label="时间显示"
          data-testid="osd-time-switch"
          @click="emit('update:timeEnable', !timeEnable)"
        >
          <i />
        </button>
      </header>

      <div class="osd-card-body" :class="{ 'is-muted': !timeEnable }">
        <!-- 格式：三选一的枚举 → 下拉框。样例是**当前时刻的实例**，不是要用户背的模式串。 -->
        <div class="osd-row" data-testid="osd-time-format">
          <span class="osd-row-label">格式</span>
          <div class="osd-row-control">
            <span class="osd-select">
              <select
                :value="timeType"
                :disabled="disabled"
                aria-label="时间格式"
                data-testid="osd-fmt-select"
                @change="emit('update:timeType', ($event.target as HTMLSelectElement).value)"
              >
                <option
                  v-for="option in OSD_TIME_TYPE_OPTIONS"
                  :key="option.value"
                  :value="option.value"
                  :class="{ 'osd-fmt-sample': !!option.sample }"
                  :data-sample="option.sample || undefined"
                  :data-testid="`osd-fmt-${fmtKey(option.value)}`"
                >
                  {{ optionText(option) }}
                </option>
              </select>
              <ChevronDownIcon class="osd-select-caret" :size="12" />
            </span>
          </div>
        </div>

        <!-- 位置：「调整位置 / 完成调整」按钮就在坐标读数旁边 —— 它改的就是这一行。 -->
        <div class="osd-row" data-testid="osd-time-position">
          <span class="osd-row-label">位置</span>
          <div class="osd-row-control">
            <button
              v-if="canvasLinked"
              type="button"
              class="osd-adjust"
              :class="{ 'is-active': editing }"
              :disabled="disabled"
              :aria-pressed="editing"
              :data-active="editing ? '1' : '0'"
              :title="
                editing
                  ? '退出调整：画面上的标记重新隐藏（改动还在，点「下发」才写到设备）'
                  : '进入调整：画面上会显示这些标记，拖动即可改位置'
              "
              data-testid="osd-edit-toggle"
              @click="emit('toggleEdit')"
            >
              <CheckCircle2 v-if="editing" :size="11" />
              <Crosshair v-else :size="11" />
              <span>{{ editing ? "完成调整" : "调整位置" }}</span>
            </button>
            <span class="osd-pos-value" data-testid="osd-time-pos">
              X {{ posValue("x") || 0 }} · Y {{ posValue("y") || 0 }}
            </span>
            <button
              v-if="canvasLinked"
              type="button"
              class="osd-pos-toggle"
              :aria-expanded="posExpanded"
              data-testid="osd-pos-toggle"
              @click="posExpanded = !posExpanded"
            >
              精确数值
              <ChevronDownIcon :size="10" :class="{ 'is-open': posExpanded }" />
            </button>
          </div>
        </div>

        <div v-if="posExpanded || !canvasLinked" class="osd-exact" data-testid="osd-pos-exact">
          <label v-for="(axis, index) in POINT_AXES" :key="axis" class="osd-exact-axis">
            <span>{{ axis }}</span>
            <input
              class="osd-input is-num"
              type="text"
              inputmode="numeric"
              :disabled="disabled"
              :value="posValue(index === 0 ? 'x' : 'y')"
              :aria-label="`时间戳 ${axis}`"
              :data-testid="`osd-time-${index === 0 ? 'x' : 'y'}`"
              @change="commitAxis(index === 0 ? 'x' : 'y', ($event.target as HTMLInputElement).value)"
            />
          </label>
        </div>
      </div>
    </section>

    <!-- ═══════════ 对象块二：叠加文字 ═══════════ -->
    <section class="osd-card" data-testid="osd-block-text">
      <header class="osd-card-head">
        <span class="osd-card-title">叠加文字</span>
        <span class="osd-card-count" data-testid="osd-text-count">{{ rows().length }}/{{ maxItems }}</span>
        <span v-if="!textEnable" class="osd-card-off" data-testid="osd-text-off">已关闭</span>
        <button
          type="button"
          class="osd-switch"
          :class="{ 'is-on': textEnable }"
          :disabled="disabled"
          :aria-pressed="textEnable"
          aria-label="文字显示"
          data-testid="osd-text-switch"
          @click="emit('update:textEnable', !textEnable)"
        >
          <i />
        </button>
      </header>

      <div class="osd-card-body" :class="{ 'is-muted': !textEnable }">
        <p v-if="!rows().length" class="osd-empty" data-testid="osd-text-empty">未配置叠加文字</p>

        <div v-for="(row, index) in rows()" :key="index" class="osd-text-row" :data-testid="`osd-text-row-${index}`">
          <!-- 编号与画布锚点同源：画面上那个圆点里的数字就是这个 -->
          <span class="osd-index" :class="{ 'is-unplaced': isUnplaced(row) }">{{ index + 1 }}</span>
          <input
            class="osd-input"
            type="text"
            :value="row.text"
            :maxlength="maxLength"
            :disabled="disabled"
            :placeholder="`第 ${index + 1} 条文字`"
            :aria-label="`第 ${index + 1} 条叠加文字`"
            :data-testid="`osd-text-${index}`"
            @change="setText(index, ($event.target as HTMLInputElement).value)"
          />
          <!-- 字符计数就地显示：原实现要等 `buildOSD` 拒发才说「第 N 条超 32 个字符」，用户得回去找 -->
          <span class="osd-charcount" :data-testid="`osd-charcount-${index}`"> {{ charCount(row.text) }}/{{ maxLength }} </span>
          <button
            v-if="canvasLinked"
            type="button"
            class="osd-locate"
            :class="{ 'is-unplaced': isUnplaced(row) }"
            :disabled="disabled"
            :title="isUnplaced(row) ? '这一行还没在画面上定位，拖一下标记' : '在画面上定位并高亮这一行'"
            :data-testid="`osd-locate-${index}`"
            @click="emit('locate', { kind: 'item', index })"
          >
            <Crosshair :size="10" /><span>定位</span>
          </button>
          <span v-if="isUnplaced(row)" class="osd-unplaced-tag" :data-testid="`osd-unplaced-${index}`">未定位</span>
          <button
            type="button"
            class="osd-del"
            :disabled="disabled"
            :aria-label="`删除第 ${index + 1} 条`"
            :data-testid="`osd-del-${index}`"
            @click="removeRow(index)"
          >
            <Trash2 :size="11" />
          </button>
        </div>

        <button
          type="button"
          class="osd-add"
          :disabled="disabled || rows().length >= maxItems"
          data-testid="osd-add"
          @click="addRow"
        >
          <Plus :size="11" />加一行字
        </button>
      </div>
    </section>

    <!-- ═══════════ 只读事实：坐标画布 ═══════════
                     这两条原先是可编辑滑杆，实测设备**拒收**平台改写（写 2560×1440 也回 200 OK、
                     回读仍是 704×576），留着就是一个点了没用的控件。它们真正的身份是
                     **遮挡坐标的基准 + 下发这一组的必需值**，所以只能读它、照它算。 -->
    <div class="osd-row osd-canvas" :class="{ 'is-missing': !canvas }" data-testid="osd-canvas">
      <span class="osd-row-label">画布</span>
      <div class="osd-row-control">
        <span v-if="canvas" class="osd-canvas-value" data-testid="osd-canvas-value">
          {{ canvas.width }} × {{ canvas.height }}
        </span>
        <span v-else class="osd-canvas-value" data-testid="osd-canvas-value">—</span>
        <span class="osd-canvas-tag" data-testid="osd-canvas-tag">{{ canvas ? "只读" : "未读到" }}</span>
        <span v-if="!canvas" class="osd-canvas-hint">点「读取」向设备查询；平台不会凭空填一个数</span>
      </div>
    </div>
  </div>
</template>

<script lang="ts">
export default { name: "DeviceConfigOsdBlocks" };
</script>

<style scoped lang="scss">
.osd-blocks {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;

  &.is-disabled {
    opacity: 0.6;
  }
}

/* 行版式（2026-09-20）：底栏把整块摆到画面下方，容器是"矮而宽" ——
   时间戳 / 叠加文字并排，画布事实压成整行的最后一条。
   ⛔ 高度靠 `min-height: 0` + 面板体自己滚：宿主给的是定高格子（148px），
      不给这条链，文字行会把整格撑破、或反过来被静默裁掉
      （同本仓「嵌入形态宿主必须给定高」那条契约）。 */
.osd-blocks.is-row {
  display: grid;
  grid-template-rows: minmax(0, 1fr) auto;
  grid-template-columns: minmax(0, 0.9fr) minmax(0, 1.1fr);
  gap: 6px 8px;
  height: 100%;
  min-height: 0;
}
.osd-blocks.is-row > .osd-card {
  min-height: 0;
}
.osd-blocks.is-row > .osd-card > .osd-card-body {
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
}
.osd-blocks.is-row > .osd-canvas {
  grid-column: 1 / -1;
}
.osd-blocks.is-row .osd-card {
  gap: 3px;
}
.osd-blocks.is-row .osd-card-body {
  gap: 3px;
}

/* 窄列里把标签列收到 2 个字（格式 / 位置 / 画布）—— 省下的宽度全给下拉框。 */
.osd-blocks.is-row .osd-row-label {
  width: 22px;
}
.osd-blocks.is-row .osd-text-row {
  gap: 4px;
}

/* 画布没读到时的长句提示留给宽度够的地方（侧栏）：
   底栏里旁边那格「画面遮挡」就写着同一份基准，这里只剩「未读到」标签。 */
.osd-blocks.is-row .osd-canvas-hint {
  display: none;
}

.osd-card {
  display: flex;
  flex-direction: column;
  gap: 5px;
  padding: 7px 8px 8px;
  background: var(--uvp-dialog-control-bg, #ffffff);
  border: 1px solid var(--uvp-panel-border, #dbe4f0);
  border-radius: 6px;
}

.osd-card-head {
  display: flex;
  gap: 6px;
  align-items: center;
}

.osd-card-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-text-primary);
}

.osd-card-count {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}

.osd-card-off {
  margin-left: auto;
  font-size: 10.5px;
  color: var(--uvp-text-tertiary);
}

/* 开关跟在「已关闭」提示之后时不再吃 margin-left:auto */
.osd-card-off + .osd-switch {
  margin-left: 0;
}

.osd-switch {
  display: inline-flex;
  align-items: center;
  width: 28px;
  height: 16px;
  padding: 0 2px;
  margin-left: auto;
  cursor: pointer;
  background: var(--uvp-panel-border, #dbe4f0);
  border: none;
  border-radius: 8px;
  transition: background 0.15s;

  i {
    width: 12px;
    height: 12px;
    background: #ffffff;
    border-radius: 50%;
    transition: transform 0.15s;
  }

  &.is-on {
    background: var(--uvp-brand, #165dff);
  }

  &.is-on i {
    transform: translateX(12px);
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.5;
  }
}

.osd-card-body {
  display: flex;
  flex-direction: column;
  gap: 5px;
  min-width: 0;

  /* 开关关闭：灰显但**保留可读**（配置还在、只是不显示） */
  &.is-muted {
    opacity: 0.55;
  }
}

/* ─── 标签列 + 控件列：整块面板的统一骨架 ─── */

.osd-row {
  display: flex;
  gap: 8px;
  align-items: center;
  min-width: 0;
}

.osd-row-label {
  flex: none;
  width: 26px;
  font-size: 11.5px;
  color: var(--uvp-text-tertiary);
}

.osd-row-control {
  display: flex;
  flex: 1;
  gap: 6px;
  align-items: center;
  min-width: 0;
}

/* ─── 下拉框（原生 select + 自绘箭头） ─── */

.osd-select {
  position: relative;
  display: block;
  flex: 1;
  min-width: 0;

  select {
    width: 100%;
    height: 24px;
    padding: 0 22px 0 7px;
    font-family: ui-monospace, Menlo, monospace;
    font-size: 11.5px;
    color: var(--uvp-text-primary);
    appearance: none;
    cursor: pointer;
    background: var(--uvp-dialog-control-bg, #ffffff);
    border: 1px solid var(--uvp-panel-border, #dbe4f0);
    border-radius: 4px;

    &:focus {
      outline: none;
      border-color: var(--uvp-brand);
    }

    &:disabled {
      cursor: not-allowed;
    }
  }
}

.osd-select-caret {
  position: absolute;
  top: 50%;
  right: 6px;
  color: var(--uvp-text-tertiary);
  pointer-events: none;
  transform: translateY(-50%);
}

/* ─── 位置行 ─── */

.osd-pos-value {
  font-family: ui-monospace, Menlo, monospace;
  font-size: 11px;
  color: var(--uvp-text-secondary);
}

/* 「调整位置 / 完成调整」：它改的就是这一行的坐标，所以跟坐标读数同一行。
   ⛔ 不许再搬回画面上（老板 2026-09-20：常驻按钮会遮挡画面）。 */
.osd-adjust {
  display: inline-flex;
  flex: none;
  gap: 3px;
  align-items: center;
  height: 22px;
  padding: 0 8px;
  font-size: 11px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: var(--uvp-dialog-control-bg, #f8fbff);
  border: 1px solid var(--uvp-panel-border, #dbe4f0);
  border-radius: 4px;

  &:hover:not(:disabled) {
    color: var(--uvp-brand);
    border-color: var(--uvp-brand);
  }

  /* 编辑中必须一眼看出来 —— 否则用户忘了自己还开着，随手点画面就可能挪走某行字，
     而画面上看不出真字（真实效果由设备渲染）。 */
  &.is-active {
    color: #ffffff;
    background: var(--uvp-brand, #165dff);
    border-color: var(--uvp-brand, #165dff);
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.45;
  }
}

.osd-pos-toggle {
  display: inline-flex;
  flex: none;
  gap: 2px;
  align-items: center;
  margin-left: auto;
  font-size: 10.5px;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: none;
  border: none;

  svg {
    transition: transform 0.15s;
  }

  svg.is-open {
    transform: rotate(180deg);
  }

  &:hover {
    color: var(--uvp-brand);
  }
}

.osd-exact {
  display: flex;
  gap: 6px;
  padding-left: 34px;
}

.osd-exact-axis {
  display: inline-flex;
  gap: 3px;
  align-items: center;

  span {
    font-size: 10px;
    color: var(--uvp-text-tertiary);
  }
}

.osd-input {
  width: 100%;
  min-width: 0;
  height: 22px;
  padding: 0 7px;
  font-size: 12px;
  color: var(--uvp-text-primary);
  background: var(--uvp-dialog-control-bg, #ffffff);
  border: 1px solid var(--uvp-panel-border, #dbe4f0);
  border-radius: 4px;

  &:focus {
    outline: none;
    border-color: var(--uvp-brand);
  }

  &.is-num {
    width: 62px;
    text-align: right;
  }
}

/* ─── 叠加文字列表 ─── */

.osd-text-row {
  display: flex;
  gap: 5px;
  align-items: center;
  min-width: 0;
}

.osd-index {
  display: inline-flex;
  flex: none;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  font-size: 10px;
  font-weight: 600;
  color: #ffffff;
  background: var(--uvp-brand, #165dff);
  border-radius: 50%;

  /* 未定位 = 与画布上的琥珀虚线锚点同色 */
  &.is-unplaced {
    color: #b26a00;
    background: #fff3d6;
    border: 1px dashed #e08b00;
  }
}

.osd-charcount {
  flex: none;
  font-family: ui-monospace, Menlo, monospace;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}

.osd-locate {
  display: inline-flex;
  flex: none;
  gap: 3px;
  align-items: center;
  height: 22px;
  padding: 0 7px;
  font-size: 10.5px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: var(--uvp-dialog-control-bg, #f8fbff);
  border: 1px solid var(--uvp-panel-border, #dbe4f0);
  border-radius: 4px;

  &:hover:not(:disabled) {
    color: var(--uvp-brand);
    border-color: var(--uvp-brand);
  }

  &.is-unplaced {
    color: #b26a00;
    background: #fff8e8;
    border-color: #f0c26b;
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.45;
  }
}

.osd-unplaced-tag {
  flex: none;
  font-size: 10px;
  color: #b26a00;
}

.osd-del {
  display: inline-flex;
  flex: none;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: var(--uvp-dialog-control-bg, #f8fbff);
  border: 1px solid var(--uvp-panel-border, #dbe4f0);
  border-radius: 4px;

  &:hover:not(:disabled) {
    color: var(--uvp-brand);
    border-color: var(--uvp-brand);
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.45;
  }
}

.osd-add {
  display: inline-flex;
  gap: 3px;
  align-items: center;
  align-self: flex-start;
  height: 22px;
  padding: 0 8px;
  font-size: 11px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: var(--uvp-dialog-control-bg, #f8fbff);
  border: 1px dashed var(--uvp-panel-border, #dbe4f0);
  border-radius: 4px;

  &:hover:not(:disabled) {
    color: var(--uvp-brand);
    border-color: var(--uvp-brand);
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.45;
  }
}

.osd-empty {
  margin: 0;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}

/* ─── 只读事实：坐标画布 ───
   与上面两块共用「标签列 + 控件列」骨架，但不做成一个带边框的盒子 ——
   省下的高度留给后面要合并进来的视频编码面板（老板 2026-09-20）。 */
.osd-canvas {
  padding-top: 6px;
  border-top: 1px solid var(--uvp-panel-border, #dbe4f0);
}

.osd-canvas-value {
  font-family: ui-monospace, Menlo, monospace;
  font-size: 11.5px;
  color: var(--uvp-text-primary);
}

.osd-canvas-tag {
  flex: none;
  padding: 1px 5px;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-dialog-control-bg, #ffffff);
  border: 1px solid var(--uvp-panel-border, #dbe4f0);
  border-radius: 3px;
}

.osd-canvas-hint {
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}

/* 没读到设备声明时走琥珀态：让"这是平台猜的 / 还没拿到"一眼可见 */
.osd-canvas.is-missing {
  .osd-canvas-value {
    color: #b26a00;
  }

  .osd-row-label {
    color: #b26a00;
  }
}
</style>
