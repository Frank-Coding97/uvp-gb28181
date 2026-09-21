<script setup lang="ts">
/**
 * DeviceConfigDrawer - 设备配置中心（专业客户端形态）
 *
 * 形态对标海康/大华的「摄像头属性」窗口：左栏实时预览 + 操作按钮排 + 云台，
 * 中栏分组导航，右栏参数区（滑杆配数字微调）。目的是把 GB/T 28181 的
 * `DeviceConfig` 家族收进**一个入口**，而不是每项各挂一张小卡。
 *
 * ⛔ 两条边界（别被"顺手简化"）：
 *   1. 中栏分组是**标准 ConfigType 家族**，不是摄像机的 ISP 参数。平台管不到
 *      ISP，照搬那套会做出一个点不动的假界面。
 *   2. 面板覆盖**两条独立通道**（这是刻意的，不是待办）：
 *        · 「视频参数属性」→ `video-params`（按码流分行、按行对账，独立落库表）
 *        · 其余 6 组      → `device-configs`（通用容器、按 config_type 整块落快照）
 *      两条通道的读法/写法/对账粒度都不同，所以下面用 `activeXxx` 这一组计算属性
 *      把"当前这一组该走哪条通道"收成一处，模板只认 `activeXxx`。
 *      ⛔ 不要让模板里出现 `activeGroup.key === 'video-param' ? a : b` 的判断散落各处 ——
 *         漏一处就会出现"读到的是 A 通道、下发的是 B 通道"这种最难查的错。
 */
import { computed, onBeforeUnmount, reactive, ref, watch, type Component } from "vue";
import {
  Camera,
  ChevronRight,
  FlipHorizontal,
  FlipVertical,
  Frame,
  Info,
  Minus,
  Plus,
  RefreshCcw,
  RotateCcw,
  RotateCw,
  Send,
  Wifi,
  WifiOff,
  X
} from "@lucide/vue";
import {
  applyChannelDeviceConfigs,
  applyChannelVideoParams,
  getChannelDeviceConfigs,
  getChannelVideoParams,
  type ApplyVideoParamItem,
  type DeviceConfigEntry,
  type VideoParam,
  type VideoParamReconcile
} from "@/api/gb28181";
import {
  isValidResolutionCode,
  parseStreamNumberList,
  resolutionText,
  validateVideoParamItems,
  videoParamEmptyText,
  type VideoParamCodecItem
} from "../videoParamCodec";
import DeviceConfigOsdBlocks from "./DeviceConfigOsdBlocks.vue";
import DeviceConfigSlider from "./DeviceConfigSlider.vue";
import DeviceConfigTextItems from "./DeviceConfigTextItems.vue";
import DeviceConfigWeekPlan from "./DeviceConfigWeekPlan.vue";
import {
  CONFIG_GROUPS,
  MAX_MASK_REGIONS,
  MAX_OSD_TEXT_ITEMS,
  MAX_OSD_TEXT_LENGTH,
  configGroupDefaults,
  findConfigGroup,
  type ConfigField,
  type ConfigFieldKind,
  type ConfigGroup,
  type ConfigScheduleDay,
  type ConfigSelectOption,
  type ConfigTextItem,
  type FieldValue
} from "./deviceConfigGroups";
import {
  buildDeviceConfigBlocks,
  changedFieldKeys,
  readDeviceConfigPayload,
  type BuildResult,
  type FormValues
} from "./deviceConfigPayload";

const props = withDefaults(
  defineProps<{
    visible: boolean;
    /** 在播放控制台的视频参数标签内直接渲染，不使用遮罩和弹窗层。 */
    embedded?: boolean;
    deviceName?: string;
    deviceCode?: string;
    online?: boolean;
    effectiveVersion?: string;
    channelId?: number | null;
    channelName?: string;
    canRead?: boolean;
    canApply?: boolean;
    /** 设备配置标签只展示非视频参数配置族。 */
    configOnly?: boolean;
    groupKeys?: string[];
    /** 嵌入工作台时，把复杂编辑器挂到宿主底部的目标节点。 */
    detailTarget?: string | HTMLElement | null;
    /**
     * OSD 是否正处在「调整位置」编辑模式（**只读**，用来渲染按钮的开 / 关态）。
     *
     * ⛔ 状态**必须由画面侧持有**，不能落在抽屉里：编辑模式控制的是**画布上那层锚点**
     *    的生死，抽屉里存一份就只能单向通知，画面自己的退出路径（Esc / 换页签 / 换通道）
     *    会把两份状态搞岔。所以这里**不写 `v-model`**，只发一个"我要切一下"的意图
     *    （`toggle-osd-edit`），真正的切换走画面侧的 `toggleOsdEditMode()` ——
     *    那里还要顺手关掉遮挡框选、取消拖到一半的临时坐标。
     */
    osdEditing?: boolean;
    /**
     * 抽屉外面有没有一块可拖画的 OSD 画布。
     *
     * ⛔ 默认 `false`：挂在**没有画面的宿主**上时（如设备详情里那种用法），
     *    渲染「调整位置」就是个点了没反应的按钮。
     */
    osdCanvasLinked?: boolean;
    /**
     * 当前选中的码流（`0` 主码流 / `1` 子码流…）。传入即为**受控**。
     *
     * ⛔ 播放控制台底栏那格「参数对照」和侧栏的「配置文件」下拉指的是**同一路**：
     *    两个下拉各持一份状态，就会出现"底栏对着子码流、侧栏显示主码流"，
     *    而两边都不报错。所以真源交给宿主，用 `v-model:stream-profile` 双向绑定。
     * ⛔ 不传（设备详情抽屉那种用法）时回落内部状态，行为与以前完全一致。
     */
    streamProfile?: string;
    /**
     * 当前选中的分组 key（见 `CONFIG_GROUPS`）。传入即为**受控**，理由同 `streamProfile`。
     *
     * ⭐ 宿主需要知道"现在停在哪一组"是为了做**跨组联动**：画面侧只允许在「图像叠加」
     *    组上留一个 OSD 编辑模式，切到「视频编码」之后那层锚点已经无处可拖，
     *    必须跟着退出去（`v-model:active-group-key`）。
     */
    activeGroupKey?: string;
  }>(),
  {
    embedded: false,
    deviceName: "",
    deviceCode: "",
    online: false,
    effectiveVersion: "",
    channelId: null,
    channelName: "",
    canRead: true,
    canApply: true,
    configOnly: false,
    detailTarget: null,
    osdEditing: false,
    osdCanvasLinked: false
  }
);

const emit = defineEmits<{
  "update:visible": [value: boolean];
  /** 请求切换 OSD 编辑模式（进 / 出由宿主决定，见 `osdEditing` 的注释）。 */
  toggleOsdEdit: [];
  "update:streamProfile": [value: string];
  "update:activeGroupKey": [value: string];
}>();

/**
 * 把 catch 到的东西翻成给操作员看的一句话。
 *
 * ⛔ 不能直接取 `error.message`：axios 在 HTTP 4xx/5xx 上的 message 是
 *    「Request failed with status code 400」这种**传输层原文**，操作员看不懂，
 *    还把后端已经算好的业务原因（例：「未知的配置类型: BasicParam」）丢了。
 *    后端业务消息挂在 `error.response.data.message` 上。
 *    两者都取不到时一律回落 fallback —— 宁可说「读取失败」，也不把状态码丢到屏幕上。
 */
function readErrorMessage(error: unknown, fallback: string): string {
  const candidate = error as { response?: { data?: { message?: unknown } }; message?: unknown } | null;
  const business = candidate?.response?.data?.message;
  if (typeof business === "string" && business.trim()) return business.trim();
  const transport = candidate?.message;
  if (typeof transport === "string" && transport.trim() && !/Request failed with status code/i.test(transport)) {
    return transport.trim();
  }
  return fallback;
}

const PTZ_STEPS = [1, 2, 3, 4, 5, 6, 7, 8];

const VIDEO_FORMAT_OPTIONS: ConfigSelectOption[] = [
  { value: "1", label: "MPEG-4" },
  { value: "2", label: "H.264" },
  { value: "4", label: "3GP" },
  { value: "5", label: "H.265" }
];

const RESOLUTION_OPTIONS: ConfigSelectOption[] = [
  { value: "1", label: "QCIF" },
  { value: "2", label: "CIF" },
  { value: "3", label: "4CIF" },
  { value: "4", label: "D1" },
  { value: "5", label: "720P" },
  { value: "6", label: "1080P" }
];

const BIT_RATE_TYPE_OPTIONS: ConfigSelectOption[] = [
  { value: "1", label: "CBR" },
  { value: "2", label: "VBR" }
];

const CUSTOM_RESOLUTION = "__custom__";

/* ─────────────────────── 分组导航 ─────────────────────── */

const configGroups = computed(() =>
  CONFIG_GROUPS.filter(
    group => (!props.configOnly || group.key !== "video-param") && (!props.groupKeys || props.groupKeys.includes(group.key))
  )
);
/**
 * 当前分组。宿主传了 `activeGroupKey` 时以宿主为真源（见该 prop 的注释）。
 *
 * ⛔ getter 里做一次**合法性兜底**：宿主那份状态可能指向本实例没挂的组
 *    （设备详情抽屉只挂 record / alarm 两组，宿主却在看别的组）。
 *    直接返回非法 key 会让 `dcg-nav` 一个高亮都没有，看着像"导航坏了"。
 * ⛔ 用 `||` 而不是 `??`：空串是"没传"，不是"要选空组"。
 */
const internalActiveGroupKey = ref<string>(props.configOnly ? "osd" : "video-param");
const activeGroupKey = computed<string>({
  get: () => {
    const candidate = props.activeGroupKey || internalActiveGroupKey.value;
    if (configGroups.value.some(group => group.key === candidate)) return candidate;
    return configGroups.value[0]?.key ?? candidate;
  },
  set: value => {
    internalActiveGroupKey.value = value;
    emit("update:activeGroupKey", value);
  }
});
watch(
  configGroups,
  groups => {
    if (groups.length && !groups.some(group => group.key === activeGroupKey.value)) activeGroupKey.value = groups[0].key;
  },
  { immediate: true }
);
const activeGroup = computed<ConfigGroup>(() => findConfigGroup(activeGroupKey.value) ?? (CONFIG_GROUPS[0] as ConfigGroup));

/**
 * 参数区标题（`dcg-group-title`）。
 *
 * ⛔ 嵌入形态且只剩一组时用 `navLabel`，理由是**没有别的地方显示它了**：分组导航
 *    `dcg-nav` 只在 `configGroups.length > 1` 时渲染，2026-09-20 图像叠加搬去底栏后
 *    控制台侧栏只剩 `video-param` 一组 ⇒ 导航整块消失，`navLabel`（老板嘴里的「视频编码」）
 *    也跟着从界面上消失，只剩协议学名「视频参数属性」。
 * ⛔ 非嵌入形态、或多组时照旧 `label` —— 标准名一个字不改；两种情况下 `title` 都挂着标准全名。
 */
const activeGroupTitle = computed(() =>
  props.embedded && configGroups.value.length <= 1
    ? (activeGroup.value.navLabel ?? activeGroup.value.label)
    : activeGroup.value.label
);

/**
 * 渲染用的扁平字段形状：把联合类型拍平，模板里就不必做类型窄化。
 * 少了这层，`field.min` 在 `select` 分支上过不了 vue-tsc。
 */
interface RenderField {
  kind: ConfigFieldKind;
  key: string;
  label: string;
  hint: string;
  options: ConfigSelectOption[];
  min: number;
  max: number;
  step: number;
  unit: string;
  placeholder: string;
  maxlength?: number;
  /** 坐标轴名（coords 专用）：**必须逐字段带上**，遮挡区是角点、不是 X/Y/W/H。 */
  axes: string[];
  /** 变长列表的上限（texts 专用）。 */
  maxItems: number;
  /** 该值在协议里是可选元素（空 = 不下发该元素）。 */
  optional: boolean;
}

function toRenderField(field: ConfigField): RenderField {
  const base: RenderField = {
    kind: field.kind,
    key: field.key,
    label: field.label,
    hint: field.hint ?? "",
    options: [],
    min: 0,
    max: 100,
    step: 1,
    unit: "",
    placeholder: "",
    axes: [],
    maxItems: 0,
    optional: false
  };
  switch (field.kind) {
    case "select":
    case "mirror":
      return { ...base, options: field.options };
    case "slider":
      return {
        ...base,
        min: field.min,
        max: field.max,
        step: field.step ?? 1,
        unit: field.unit ?? "",
        optional: field.optional === true
      };
    case "text":
      return { ...base, placeholder: field.placeholder ?? "", maxlength: field.maxlength };
    case "coords":
      return { ...base, axes: field.axes };
    case "texts":
      return { ...base, maxItems: field.maxItems };
    default:
      return base;
  }
}

const activeFields = computed<RenderField[]>(() => activeGroup.value.fields.map(toRenderField));

/**
 * 渲染用的字段提示 = 声明里的静态文案 + 依赖**设备事实**的动态补充。
 *
 * 目前只有一处动态补充：画面处理组的遮挡坐标，得把基准画布写出来。
 * ⛔ 那几个数字框是"设备画布像素"，不是"画面像素"；不写基准，
 *    用户在输入框里手填的数就只能靠猜（同一个 `640` 在 704 画布上是 91% 宽、
 *    在 2560 画布上只有 25% 宽）。
 */
function fieldHint(field: RenderField, groupKey: string): string {
  if (field.kind !== "coords" || groupKey !== PICTURE_GROUP_KEY) return field.hint;
  const canvas = pictureCanvasSize.value;
  if (!canvas) return field.hint;
  const note = `坐标基准 ${canvas.width}×${canvas.height}（设备声明的图像尺寸）`;
  return field.hint ? `${field.hint} · ${note}` : note;
}

/**
 * 镜像取值 → 图标。
 *
 * ⛔ 这张表只回答「哪个值该画成什么方向」，**不重新定义取值** ——
 *    0/1/2/3 的唯一真源仍是 `MIRROR_OPTIONS`。
 *    映射缺失时回落成一个中性方框，而不是"猜一个方向"：猜错等于把方向说反，比没图标更糟。
 */
const MIRROR_ICONS: Record<string, Component> = {
  "0": Frame,
  "1": FlipHorizontal,
  "2": FlipVertical,
  "3": RotateCw
};

function mirrorIcon(value: string): Component {
  return MIRROR_ICONS[value] ?? Frame;
}

const DETAIL_FIELD_KINDS = new Set<ConfigFieldKind>(["coords", "texts", "schedules"]);
const detailFields = computed(() =>
  props.embedded && props.detailTarget ? activeFields.value.filter(field => DETAIL_FIELD_KINDS.has(field.kind)) : []
);
const primaryFields = computed(() =>
  props.embedded && props.detailTarget
    ? activeFields.value.filter(field => !DETAIL_FIELD_KINDS.has(field.kind))
    : activeFields.value
);

/* ─────────────────────── 配置家族的表单值 ─────────────────────── */

/**
 * 表单值袋：`{ 分组key: { 字段id: 值 } }`。
 *
 * ⛔ 这里的键是**表单键**（`mask1` / `items` / `schedules`…），不是协议键。
 *    两张表之间的翻译只在 `deviceConfigPayload.ts` 里发生。
 */
const familyValues = reactive<Record<string, FormValues>>({});
for (const group of CONFIG_GROUPS) familyValues[group.key] = configGroupDefaults(group.key);

/**
 * 最近一次回读到的值（脏值统计的基准）。
 *
 * ⛔ 基准必须是**回读值**而不是"打开面板时的快照"：下发之后设备可能被别的
 *    网管改掉，拿旧快照当基准会把"设备已经不同了"算成"用户没改"。
 */
const familyBaseline = reactive<Record<string, FormValues>>({});
for (const group of CONFIG_GROUPS) familyBaseline[group.key] = configGroupDefaults(group.key);

function sv(groupKey: string, fieldKey: string): FieldValue | undefined {
  return familyValues[groupKey]?.[fieldKey];
}

/**
 * 读**最近一次回读**的值（设备事实），与 `sv`（草稿）分开。
 *
 * ⛔ 展示"设备此刻是什么样"必须走这里。用草稿值会让"用户已经点了但还没下发"的
 *    编辑被当成事实 —— 最典型的是总闸：用户点了停用、还没下发，设备上遮挡仍然生效，
 *    画布却会立刻把框擦掉，等于骗用户说已经清掉了。
 */
function baselineSv(groupKey: string, fieldKey: string): FieldValue | undefined {
  return familyBaseline[groupKey]?.[fieldKey];
}

function setSv(groupKey: string, fieldKey: string, value: FieldValue) {
  const bucket = familyValues[groupKey] ?? (familyValues[groupKey] = {});
  bucket[fieldKey] = value;
}

function textValue(groupKey: string, fieldKey: string): string {
  return String(sv(groupKey, fieldKey) ?? "");
}

function boolValue(groupKey: string, fieldKey: string): boolean {
  return sv(groupKey, fieldKey) === true;
}

function numberValue(groupKey: string, fieldKey: string, fallback: number): number {
  const parsed = Number(sv(groupKey, fieldKey));
  return Number.isFinite(parsed) ? parsed : fallback;
}

function coordsValue(groupKey: string, fieldKey: string): number[] {
  const raw = sv(groupKey, fieldKey);
  if (!Array.isArray(raw)) return [0, 0, 0, 0];
  return raw.map(item => (typeof item === "number" ? item : Number(item) || 0));
}

function setCoordValue(groupKey: string, fieldKey: string, index: number, raw: string) {
  const next = coordsValue(groupKey, fieldKey).slice();
  const parsed = Number(raw);
  next[index] = Number.isFinite(parsed) ? parsed : 0;
  setSv(groupKey, fieldKey, next);
}

function textItemsValue(groupKey: string, fieldKey: string): ConfigTextItem[] {
  const raw = sv(groupKey, fieldKey);
  return Array.isArray(raw) ? (raw as ConfigTextItem[]) : [];
}

function setTextItems(groupKey: string, fieldKey: string, value: ConfigTextItem[]) {
  setSv(groupKey, fieldKey, value);
}

function schedulesValue(groupKey: string, fieldKey: string): ConfigScheduleDay[] {
  const raw = sv(groupKey, fieldKey);
  return Array.isArray(raw) ? (raw as ConfigScheduleDay[]) : [];
}

function setSchedules(groupKey: string, fieldKey: string, value: ConfigScheduleDay[]) {
  setSv(groupKey, fieldKey, value);
}

/** 把某组的"当前值"整组重置为基准值（还原按钮用）。 */
function resetGroup(groupKey: string) {
  familyValues[groupKey] = { ...familyBaseline[groupKey] };
}

/** 本组相对最近回读值改了几项。 */
function dirtyFieldCount(groupKey: string): number {
  return changedFieldKeys(familyValues[groupKey] ?? {}, familyBaseline[groupKey] ?? {}).length;
}

/* ─────────────────────── 视频参数属性（A-5，真实读写）─────────────────────── */

const videoRows = ref<VideoParam[]>([]);
const videoDraft = ref<VideoParamCodecItem[]>([]);
const videoReconcile = ref<VideoParamReconcile | null>(null);
const videoFreshness = ref("");

/** 后端 freshness 枚举的中文口径 —— 状态条是给操作员看的，别把英文枚举丢出去。 */
const FRESHNESS_TEXT: Record<string, string> = {
  fresh: "数据有效",
  stale: "数据已过期",
  unknown: "尚无回读"
};

const freshnessText = computed(() => {
  const raw = activeFreshness.value;
  if (!raw) return "";
  return FRESHNESS_TEXT[raw] ?? raw;
});
const videoStreamNumberList = ref("");
const videoRegisteredVersion = ref("");
const videoObservedAt = ref("");
const videoLoading = ref(false);
const videoApplying = ref(false);
const videoError = ref("");
/**
 * 当前码流。宿主传了 `streamProfile` 时以宿主为真源（见该 prop 的注释）。
 *
 * ⛔ 用 `||` 而不是 `??`：`selectValue` 在取值异常时会给出空串，
 *    空串是"没传"，不是"要看 0 号以外的某一路"。
 */
const internalStreamProfile = ref("0");
const streamProfile = computed<string>({
  get: () => props.streamProfile || internalStreamProfile.value,
  set: value => {
    internalStreamProfile.value = value;
    emit("update:streamProfile", value);
  }
});
const ptzStep = ref(5);

let pollTimer: number | undefined;
let pollCount = 0;

function toDraft(rows: VideoParam[]): VideoParamCodecItem[] {
  return rows
    .slice()
    .sort((left, right) => left.streamNumber - right.streamNumber)
    .map(row => ({
      streamNumber: row.streamNumber,
      videoFormat: String(row.videoFormat ?? ""),
      resolution: String(row.resolution ?? ""),
      frameRate: String(row.frameRate ?? ""),
      bitRateType: String(row.bitRateType ?? ""),
      videoBitRate: row.videoBitRate ?? null
    }));
}

const videoDirtyCount = computed(() => {
  const original = new Map(videoRows.value.map(row => [row.streamNumber, row]));
  let changed = 0;
  for (const draft of videoDraft.value) {
    const source = original.get(draft.streamNumber);
    if (!source) {
      changed += 1;
      continue;
    }
    if (
      String(source.videoFormat ?? "") !== draft.videoFormat ||
      String(source.resolution ?? "") !== draft.resolution ||
      String(source.frameRate ?? "") !== draft.frameRate ||
      String(source.bitRateType ?? "") !== draft.bitRateType ||
      String(source.videoBitRate ?? "") !== String(draft.videoBitRate ?? "")
    ) {
      changed += 1;
    }
  }
  return changed;
});

const videoFieldsDisabled = computed(() => videoLoading.value || videoApplying.value || !props.canApply);

function rowChanged(row: VideoParamCodecItem): boolean {
  const source = videoRows.value.find(item => item.streamNumber === row.streamNumber);
  if (!source) return true;
  return (
    String(source.videoFormat ?? "") !== row.videoFormat ||
    String(source.resolution ?? "") !== row.resolution ||
    String(source.frameRate ?? "") !== row.frameRate ||
    String(source.bitRateType ?? "") !== row.bitRateType ||
    String(source.videoBitRate ?? "") !== String(row.videoBitRate ?? "")
  );
}

const visibleVideoRows = computed(() => {
  if (!videoDraft.value.length) return [];
  const target = Number(streamProfile.value);
  const matched = videoDraft.value.filter(row => row.streamNumber === target);
  return matched.length ? matched : videoDraft.value;
});

function streamNumberText(streamNumber: number): string {
  if (streamNumber === 0) return "主码流";
  return `子码流 ${streamNumber}`;
}

function videoBitRateDisabled(row: VideoParamCodecItem): boolean {
  return String(row.bitRateType ?? "") !== "1";
}

/** 每格的来源徽标：`设备` = 本次回读命中了这一格；`缺省` = 设备没给这个元素。 */
function cellBadge(
  row: VideoParamCodecItem,
  field: "videoFormat" | "resolution" | "frameRate" | "bitRateType" | "videoBitRate"
): string {
  const source = videoRows.value.find(item => item.streamNumber === row.streamNumber);
  if (!source) return "缺省";
  const raw = source[field];
  return raw === null || raw === undefined || String(raw).trim() === "" ? "缺省" : "设备";
}

/**
 * 徽标的悬浮解释 —— 徽标本身只有两个字（对齐整列宽度），完整口径放 title。
 * 「不发」比来源更强：码率类型为 VBR 时这一格根本不参与下发。
 */
const BADGE_TITLE: Record<string, string> = {
  设备: "本次回读拿到了这一格，值来自设备",
  缺省: "设备未上报此元素，按缺省处理",
  不发: "码率类型为 VBR 时不发此元素"
};

function badgeTitle(badge: string): string {
  return BADGE_TITLE[badge] ?? "";
}

/** 码率格的徽标：VBR 是「不发」，优先于来源判断。 */
function bitRateBadge(row: VideoParamCodecItem): string {
  return videoBitRateDisabled(row) ? "不发" : cellBadge(row, "videoBitRate");
}

/** 视频参数组的五格 —— 徽标、对账、汇总都按这个顺序走，别再各处重列。 */
const VIDEO_CELL_FIELDS = ["videoFormat", "resolution", "frameRate", "bitRateType", "videoBitRate"] as const;

/** 本组已由设备上报的格数 —— 与徽标「设备」同源，避免两处口径漂移。 */
const deviceCellCount = computed(() =>
  visibleVideoRows.value.reduce(
    (total, row) =>
      total +
      VIDEO_CELL_FIELDS.filter(
        field => !(field === "videoBitRate" && videoBitRateDisabled(row)) && cellBadge(row, field) === "设备"
      ).length,
    0
  )
);

function resolutionSelectValue(row: VideoParamCodecItem): string {
  return isValidResolutionCode(row.resolution) ? row.resolution : CUSTOM_RESOLUTION;
}

function selectValue(event: unknown): string {
  if (typeof event === "string" || typeof event === "number") return String(event);
  if (!event || typeof event !== "object") return "";
  const value = (event as { value?: unknown }).value;
  if (value !== undefined) return String(value);
  const target = (event as { target?: { value?: unknown } }).target;
  return target?.value === undefined ? "" : String(target.value);
}

function onResolutionSelect(row: VideoParamCodecItem, event: unknown) {
  const value = selectValue(event);
  if (value === CUSTOM_RESOLUTION) {
    row.resolution = isValidResolutionCode(row.resolution) ? "" : row.resolution;
    return;
  }
  row.resolution = value;
}

const RECONCILE_TONE: Record<string, string> = {
  read_ok: "ok",
  pending: "pending",
  never_read: "idle",
  type_absent: "warn",
  mismatch: "warn",
  failed: "error"
};

/**
 * 当前分组对应哪条回读通道。
 *
 * ⛔ 这一组计算属性是**唯一**的通道选择点：模板只认 `activeXxx`，不自己判分组。
 *    两条通道的读法/写法/对账粒度都不同，判断散落各处一定会出现
 *    "读到的是 A 通道、下发的是 B 通道"这种最难查的错。
 */
const activeIsVideo = computed(() => activeGroup.value.key === "video-param");
const activeIsReady = computed(() => activeGroup.value.state === "ready");

const activeReconcile = computed<VideoParamReconcile | null>(() =>
  activeIsVideo.value ? videoReconcile.value : familyReconcile.value
);

const activeLoading = computed(() => (activeIsVideo.value ? videoLoading.value : familyLoading.value));
const activeApplying = computed(() => (activeIsVideo.value ? videoApplying.value : familyApplying.value));
const activeError = computed(() => (activeIsVideo.value ? videoError.value : familyError.value));
const activeObservedAt = computed(() => (activeIsVideo.value ? videoObservedAt.value : familyObservedAt.value));
const activeFreshness = computed(() => (activeIsVideo.value ? videoFreshness.value : familyFreshness.value));
const activeDirtyCount = computed(() => (activeIsVideo.value ? videoDirtyCount.value : dirtyFieldCount(activeGroupKey.value)));

/* ─────────────────────── 未下发差异（差异抽屉）─────────────────────── */

/**
 * 差异抽屉是否展开。
 *
 * ⛔ 切组时收起：抽屉里列的是**这一组**的改动，跨组保持展开会让用户
 *    对着 A 组的清单去判断 B 组的按钮。
 */
const pendingExpanded = ref(false);
watch(activeGroupKey, () => {
  pendingExpanded.value = false;
});

/** 深拷贝一个字段值：撤销时把基准值原样交回去，不能交出引用（数组会被改穿）。 */
function cloneFieldValue(value: FieldValue | undefined): FieldValue {
  if (Array.isArray(value)) return JSON.parse(JSON.stringify(value)) as FieldValue;
  return value ?? "";
}

/** 一个值在差异清单里的人读形态。 */
function pendingValueText(field: RenderField | undefined, value: FieldValue | undefined): string {
  if (value === undefined || value === null || value === "") return "—";
  if (typeof value === "boolean") return value ? "开" : "关";
  if (Array.isArray(value)) {
    if (!value.length) return "—";
    // 坐标是角点空间的一串整数，原样列出；其余（OSD 文本行 / 周计划）只报条数
    if (value.every(item => typeof item === "number")) return (value as number[]).join(", ");
    return `${value.length} 项`;
  }
  if (field?.kind === "select") {
    const option = field.options.find(item => item.value === String(value));
    if (option) return option.label;
  }
  return String(value);
}

interface PendingChange {
  key: string;
  label: string;
  from: string;
  to: string;
  revert: () => void;
}

/**
 * 本组相对最近一次回读值的逐项差异（「设备当前值 → 将下发」）。
 *
 * ⛔ 与 footer 的计数**同源**（都出自 `changedFieldKeys`）：各算各的必然出现
 *    「说 3 项、只列出 2 行」这种自相矛盾。
 * ⛔ 撤销单项 ≠ 设备恢复出厂：这里只是把草稿退回最近一次回读值。
 */
const activePendingChanges = computed<PendingChange[]>(() => {
  if (activeIsVideo.value) return [];
  const groupKey = activeGroupKey.value;
  const fields = new Map(activeFields.value.map(field => [field.key, field]));
  const changed = changedFieldKeys(familyValues[groupKey] ?? {}, familyBaseline[groupKey] ?? {});
  return changed.map(fieldKey => {
    const field = fields.get(fieldKey);
    const before = familyBaseline[groupKey]?.[fieldKey];
    return {
      key: fieldKey,
      label: field?.label ?? fieldKey,
      from: pendingValueText(field, before),
      to: pendingValueText(field, familyValues[groupKey]?.[fieldKey]),
      revert: () => setSv(groupKey, fieldKey, cloneFieldValue(before))
    };
  });
});
const activeRegisteredVersion = computed(() =>
  activeIsVideo.value ? videoRegisteredVersion.value : familyRegisteredVersion.value
);

const reconcileTone = computed(() => RECONCILE_TONE[activeReconcile.value?.state ?? "never_read"] ?? "idle");

const reconcileText = computed(() => {
  const reconcile = activeReconcile.value;
  const label = activeGroup.value.label;
  if (!reconcile) return `尚未读取「${label}」`;
  switch (reconcile.state) {
    case "never_read":
      return `尚未读取「${label}」`;
    case "pending":
      return `正在向设备读取「${label}」…`;
    case "read_ok":
      return "回读成功：以下为设备当前生效值";
    case "type_absent":
      return props.embedded
        ? "设备未返回该配置，可能暂不支持此功能"
        : `设备未返回该配置类型（可判为不支持 ${activeGroupConfigTypeText.value}）`;
    case "mismatch":
      return `设备已接受命令，但值未生效${reconcile.errorMessage ? `：${reconcile.errorMessage}` : ""}`;
    case "failed":
      return `读取或下发失败${reconcile.deviceError ? `：${reconcile.deviceError}` : ""}`;
    default:
      return `尚未读取「${label}」`;
  }
});

/** 本组覆盖的 ConfigType 名，逗号连接（提示文案用）。 */
const activeGroupConfigTypeText = computed(() =>
  activeGroup.value.configTypes.length ? activeGroup.value.configTypes.join(" / ") : "VideoParamAttribute"
);

const emptyText = computed(() =>
  activeIsVideo.value
    ? videoParamEmptyText(videoReconcile.value?.state ?? "never_read", { pending: videoLoading.value })
    : familyEmptyText.value
);

const versionNotice = computed(() => {
  const version = activeRegisteredVersion.value;
  if (version && version !== "2022") {
    // ⛔ 只是"提示措辞"：被误登记成 2016 的真 2022 设备不试一次就永远用不了这功能。
    // 版本不参与任何门禁，唯一可靠判据是回读结果本身。
    return `平台按 ${version} 版处理该设备（登记版本仅供参考，以回读结果为准）`;
  }
  return "";
});

function resetVideoDraft() {
  videoDraft.value = toDraft(videoRows.value);
}

function clearVideoPoll() {
  if (pollTimer !== undefined) {
    window.clearTimeout(pollTimer);
    pollTimer = undefined;
  }
}

async function loadVideoParams(refresh: boolean) {
  if (!props.channelId) {
    videoError.value = "未选择通道，无法读取视频参数。";
    return;
  }
  videoLoading.value = true;
  videoError.value = "";
  try {
    const response = await getChannelVideoParams(props.channelId, refresh);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "读取设备视频参数失败");
    const data = response.data;
    videoRows.value = data.list ?? [];
    videoDraft.value = toDraft(videoRows.value);
    videoReconcile.value = data.reconcile ?? null;
    videoFreshness.value = String(data.freshness ?? "");
    videoStreamNumberList.value = data.streamNumberList ?? "";
    videoRegisteredVersion.value = data.registeredVersion ?? "";
    videoObservedAt.value = data.observedAt ?? "";
    if (data.refreshOperationId) scheduleVideoPoll();
  } catch (error) {
    videoError.value = readErrorMessage(error, "读取设备视频参数失败");
  } finally {
    videoLoading.value = false;
  }
}

/** 下发后平台会自动回读对账，必须轮询到 reconcile 终态 —— 200 不是终态。 */
function scheduleVideoPoll() {
  clearVideoPoll();
  pollCount = 0;
  const tick = async () => {
    if (!props.visible || !props.channelId) return;
    pollCount += 1;
    await loadVideoParams(false);
    const state = videoReconcile.value?.state;
    const settled = state === "read_ok" || state === "type_absent" || state === "mismatch" || state === "failed";
    if (!settled && pollCount < 10) pollTimer = window.setTimeout(tick, 1500);
  };
  pollTimer = window.setTimeout(tick, 1200);
}

async function applyVideoParams() {
  if (!props.channelId) return;
  const invalid = validateVideoParamItems(videoDraft.value);
  if (invalid) {
    videoError.value = invalid;
    return;
  }
  videoApplying.value = true;
  videoError.value = "";
  try {
    const items: ApplyVideoParamItem[] = videoDraft.value.map(row => ({
      streamNumber: row.streamNumber,
      videoFormat: row.videoFormat,
      resolution: row.resolution,
      frameRate: row.frameRate,
      bitRateType: row.bitRateType,
      videoBitRate: row.videoBitRate
    }));
    const response = await applyChannelVideoParams(props.channelId, items, `device-config-${Date.now()}`);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "下发失败");
    if (response.data.reconcilePending) scheduleVideoPoll();
  } catch (error) {
    videoError.value = readErrorMessage(error, "下发失败");
  } finally {
    videoApplying.value = false;
  }
}

/* ─────────────────────── 配置家族（通用通道，真实读写）─────────────────────── */

/**
 * 一次读取要问的配置类型 = 各分组声明的 ConfigType 并集（去重）。
 *
 * ⛔ 一次问全而不是"读哪组问哪组"：A.2.4.7 本来就允许一次查询多个类型，
 *    拆成按组问会让"切分组"这个纯界面动作产生 SIP 报文，也会让 6 组之间
 *    的对账时刻各不相同（面板上同一屏显示的两个值来自不同时刻）。
 */
const FAMILY_CONFIG_TYPES = Array.from(new Set(CONFIG_GROUPS.flatMap(group => group.configTypes)));

const familyEntries = ref<DeviceConfigEntry[]>([]);
const familyAbsent = ref<string[]>([]);
const familyReconcile = ref<VideoParamReconcile | null>(null);
const familyFreshness = ref("");
const familyObservedAt = ref("");
const familyRegisteredVersion = ref("");
const familyLoading = ref(false);
const familyApplying = ref(false);
const familyError = ref("");
/**
 * `familyError` 归属的分组 key。
 *
 * ⛔ `familyError` 是**跨分组共用一个 ref**，而播放控制台的画面卡片只该显示 picture 组的错。
 *    不记归属的话，用户在「录像计划」组撞的错会跑到画面卡片的提示条上，把人往错的方向带。
 *    归属在**设置错误时**记录（见 [applyGroupConfig]），不从 `activeGroup` 反推 ——
 *    下发是在后台完成的，用户完全可能已经切到别的组了。
 */
const familyErrorGroup = ref("");
/**
 * 画面组最近一次下发留下的**提示**（不是错误）。
 *
 * 目前只有一处来源：`On=1` 但本次没带区域 —— 请求发得出去、设备也接受（真机实测），
 * 只是画面上多半不会有可见的遮挡。
 *
 * ⛔ 为什么不复用 `familyError` 那条通道：
 *    ① 那条是**错误**语义（渲染成危险色、走 `clearPictureError`），而这里什么都没失败；
 *    ② `clearPictureError` 在用户一「还原」草稿时就清空，可这条提示要回答的是
 *       "刚才那次下发为什么没效果"，还原草稿并不让它过期。
 */
const pictureMaskNotice = ref("");
let familyPollTimer: number | undefined;
let familyPollCount = 0;

/**
 * 把回读到的 payload 折进表单，并把同一份值设成脏值基准。
 *
 * ⛔ 某组**一块都没回读到时不覆盖表单**：一次失败的读取不该把用户还没下发的
 *    编辑清掉 —— 那样用户只看到"填的东西没了"，且归因不到"刚才那次读取失败"。
 */
function applyFamilyEntries(entries: DeviceConfigEntry[]) {
  for (const group of CONFIG_GROUPS) {
    if (!group.configTypes.length) continue;
    const merged: FormValues = configGroupDefaults(group.key);
    let filled = false;
    for (const configType of group.configTypes) {
      const entry = entries.find(item => item.configType === configType);
      if (!entry) continue;
      Object.assign(merged, readDeviceConfigPayload(configType, entry.payload ?? {}));
      filled = true;
    }
    if (!filled) continue;
    familyValues[group.key] = merged;
    familyBaseline[group.key] = { ...merged };
  }
}

const familyEmptyText = computed(() => {
  if (familyLoading.value) return "正在读取设备配置…";
  switch (familyReconcile.value?.state ?? "never_read") {
    case "pending":
      return "正在读取设备配置…";
    case "type_absent":
      return "设备未返回该配置类型";
    case "failed":
      return "未取到设备配置";
    case "never_read":
      return "尚未读取设备配置";
    default:
      // read_ok / mismatch 却一行都没有：设备回了该类型，但这组里没有一个可用字段。
      return "设备返回了该配置类型，但没有可展示的字段";
  }
});

/** 当前分组里、本次读取**设备明确没返回**的配置类型。 */
const activeGroupAbsentTypes = computed(() =>
  activeGroup.value.configTypes.filter(configType => familyAbsent.value.includes(configType))
);

/**
 * 本组**是否还没拿到设备事实**（本次读取里一个属于本组的块都没回读到）。
 *
 * ⛔ 「没读到」≠「设备是默认值」。读失败或没读过时，表单里躺的是
 *    `CONFIG_GROUP_DEFAULTS` 这份**空白模板**；把它当设备值展示、还允许在上面改，
 *    等于让用户在没有任何事实依据的情况下盲写设备（改一项 → 下发按钮就亮了）。
 *    所以：没有事实 → 字段不给控件、只显示「—」。
 *
 * 视频参数属性走独立通道，本次不在此闸范围内（见 docs/picture-settings-design.md §6）。
 */
const activeGroupFactsMissing = computed(() => {
  if (activeIsVideo.value) return false;
  if (!activeGroup.value.configTypes.length) return false;
  return !activeGroup.value.configTypes.some(configType => familyEntries.value.some(entry => entry.configType === configType));
});

/**
 * 缺事实时状态条下方的解释文案。
 *
 * 分场景说清「为什么没有值」，而不是统一一句「暂无数据」——
 * pending / type_absent 分别由 reconcileText 与 absent-notice 负责，这里不重复。
 */
const noFactsNotice = computed(() => {
  if (!activeGroupFactsMissing.value) return "";
  switch (activeReconcile.value?.state ?? "never_read") {
    case "pending":
      return "";
    case "type_absent":
      return "";
    case "failed":
      return "未取到本组设备值，字段暂不可编辑；可重试「读取」。";
    case "never_read":
      return "尚未读取设备值，字段暂不可编辑 —— 点「读取」向设备查询。";
    default:
      return "设备返回了该配置类型，但没有本组可用的字段，字段暂不可编辑。";
  }
});

/** 配置家族字段是否禁用：读取/下发进行中时不让人改，避免把值改在半路上。 */
const familyFieldsDisabled = computed(
  () => familyLoading.value || familyApplying.value || !props.canApply || !activeIsReady.value || activeGroupFactsMissing.value
);

async function loadDeviceConfigs(refresh: boolean) {
  if (!props.channelId) {
    familyError.value = "未选择通道，无法读取设备配置。";
    return;
  }
  familyLoading.value = true;
  familyError.value = "";
  try {
    const response = await getChannelDeviceConfigs(props.channelId, {
      refresh,
      configTypes: FAMILY_CONFIG_TYPES
    });
    if (response.code !== 0 || !response.data) throw new Error(response.message || "读取设备配置失败");
    const data = response.data;
    familyEntries.value = data.list ?? [];
    familyAbsent.value = data.absentTypes ?? [];
    familyReconcile.value = data.reconcile ?? null;
    familyFreshness.value = String(data.freshness ?? "");
    familyRegisteredVersion.value = data.registeredVersion ?? "";
    // ⛔ 用回读到的最大值兜底而不是 local now：面板显示"回读于"就必须是
    //    设备那次应答的时刻，用前端时钟会显示成"刚刚"，看起来像刚读到。
    familyObservedAt.value = data.observedAt ?? "";
    applyFamilyEntries(familyEntries.value);
    if (data.refreshOperationId) scheduleFamilyPoll();
  } catch (error) {
    familyError.value = readErrorMessage(error, "读取设备配置失败");
  } finally {
    familyLoading.value = false;
  }
}

/** 下发后平台会自动回读对账，必须轮询到 reconcile 终态 —— 写入 ack 不是终态。 */
function scheduleFamilyPoll() {
  clearFamilyPoll();
  familyPollCount = 0;
  const tick = async () => {
    if (!props.visible || !props.channelId) return;
    familyPollCount += 1;
    await loadDeviceConfigs(false);
    const state = familyReconcile.value?.state;
    const settled = state === "read_ok" || state === "type_absent" || state === "mismatch" || state === "failed";
    if (!settled && familyPollCount < 10) familyPollTimer = window.setTimeout(tick, 1500);
  };
  familyPollTimer = window.setTimeout(tick, 1200);
}

function clearFamilyPoll() {
  if (familyPollTimer !== undefined) {
    window.clearTimeout(familyPollTimer);
    familyPollTimer = undefined;
  }
}

/**
 * 下发**指定分组**（只发这一组覆盖的块，不把别的组的表单值捎带出去）。
 *
 * ⛔ 分组 key 显式传入而不是内部直接读 `activeGroup`：播放控制台底部的画面卡片
 *    钉死在 `picture` 组上，而侧栏此刻可能停在别的组 —— 用 activeGroup 会把卡片
 *    的下发打到用户没在看的那一组。
 */
async function applyGroupConfigs(groupKeys: string[]) {
  if (!props.channelId) return;
  // ⛔ 没有设备事实的组直接跳过：那组的表单里躺的是 `CONFIG_GROUP_DEFAULTS` 的空白模板
  //    （`1920×1080` 这类），发出去等于把平台初值当设备值写进设备。
  const keys = groupKeys.filter(key => !groupFactsMissing(key));
  const blocks: Record<string, unknown> = {};
  for (const key of keys) {
    const built = buildDeviceConfigBlocks(key, familyValues[key] ?? {});
    // ⛔ 本地先拦一道（协议层同样会拦）：平台自己发出的越界值会被对端静默当 0 处理，
    //    那种错在回读对账里只表现为"设备没照做"，归因成本极高。
    if (built.error) {
      familyError.value = built.error;
      familyErrorGroup.value = key;
      return;
    }
    Object.assign(blocks, built.blocks);
    // 走浮条「下发」时也要给同样的提示：`buildPicture` 现在放行"启用 + 无区域"，
    // 少了这句，那条路径会静默发出一个画面上看不见的遮挡（2026-09-19 现场问的就是这个）。
    if (key === PICTURE_GROUP_KEY) pictureMaskNotice.value = pictureMaskApplyNotice(built);
  }
  if (!Object.keys(blocks).length) return;

  familyApplying.value = true;
  familyError.value = "";
  familyErrorGroup.value = keys[0] ?? "";
  try {
    const response = await applyChannelDeviceConfigs(props.channelId, blocks, `device-config-${Date.now()}`);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "下发失败");
    if (response.data.reconcilePending) scheduleFamilyPoll();
  } catch (error) {
    familyError.value = readErrorMessage(error, "下发失败");
    familyErrorGroup.value = keys[0] ?? "";
  } finally {
    familyApplying.value = false;
  }
}

/**
 * 下发**一个或多个分组**的草稿。
 *
 * ⛔ 分组 key 显式传入而不是内部直接读 `activeGroup`：播放控制台底部的画面卡片与下发浮条
 *    钉死在 `picture` / `osd` 上，而侧栏此刻可能停在别的组 —— 用 activeGroup 会把
 *    卡片的下发打到用户没在看的那一组。
 *
 * ⭐ 多组一次下发是**协议本来允许的形态**：`DeviceConfig` 一条报文里 `OSDConfig` 与
 *    `PictureMask` 就是兄弟元素（A.2.3.2），`buildDeviceConfigBlocks` 只产出块、不关心
 *    这次发几组。合并成一条报文，比让画布上出现两条同名浮条安全得多
 *    —— 后者会重演本仓已被点名的「同一屏两套同名按钮」。
 */
async function applyGroupConfig(groupKey: string) {
  await applyGroupConfigs([groupKey]);
}

/** 浮条「下发」：把画面组与 OSD 组的草稿**一次**发出去。 */
async function applyPictureAndOsdGroups() {
  await applyGroupConfigs([PICTURE_GROUP_KEY, OSD_GROUP_KEY]);
}

/** 下发侧栏当前分组（`applyGroupConfig` 的薄包装）。 */
async function applyActiveGroupConfig() {
  await applyGroupConfig(activeGroup.value.key);
}

/* ─────────────────────── 活动分组的读写入口 ─────────────────────── */

function resetActiveGroup() {
  if (activeIsVideo.value) resetVideoDraft();
  else resetGroup(activeGroupKey.value);
}

function readActiveGroup() {
  if (activeIsVideo.value) void loadVideoParams(true);
  else void loadDeviceConfigs(true);
}

function applyActiveGroup() {
  if (activeIsVideo.value) void applyVideoParams();
  else void applyActiveGroupConfig();
}

/* ─────────── 控制台底部「画面卡片」（遮挡 / 镜像）的读写口 ─────────── */

/**
 * 底部画面卡片**不是第二份状态**：它读写的就是 `familyValues.picture`。
 *
 * ⛔ 一切判据都按 `picture` 组算，不能复用 `activeGroup` —— 卡片钉死在画面组上，
 *    而侧栏此刻可能停在 OSD 或基本参数，复用会把「另一组读到了没有」当成
 *    「画面组读到了没有」，闸门就失效了。
 */
const PICTURE_GROUP_KEY = "picture";

/**
 * OSD 组。它的 `length/width` 不是普通表单字段 —— 那是**设备声明的图像坐标画布**，
 * 遮挡区坐标的基准（见 [pictureCanvasSize]）。所以这一组要按分组的 key 单独点出来。
 */
const OSD_GROUP_KEY = "osd";

/** 某组本次读取里是否一个块都没回读到（= 没有设备事实）。 */
function groupFactsMissing(groupKey: string): boolean {
  const group = findConfigGroup(groupKey);
  if (!group || !group.configTypes.length) return false;
  return !group.configTypes.some(configType => familyEntries.value.some(entry => entry.configType === configType));
}

/** 某组本次读取里设备明确没返回的配置类型。 */
function groupAbsentTypes(groupKey: string): string[] {
  const group = findConfigGroup(groupKey);
  if (!group) return [];
  return group.configTypes.filter(configType => familyAbsent.value.includes(configType));
}

const pictureFactsMissing = computed(() => groupFactsMissing(PICTURE_GROUP_KEY));
const pictureAbsentTypes = computed(() => groupAbsentTypes(PICTURE_GROUP_KEY));

/* ─────────────────────── OSD 组（A.2.1.12）：画布锚点 ─────────────────────── */

/**
 * 画布上的一枚标记。
 *
 * ⛔ 形态是**锚点 + 内容标签**，不是"像真字的预览"：标准 `OSDCfgType` 里没有字体、
 *    字号、颜色 —— 画出来就是在承诺平台给不了的能力；而且设备**已经烧进码流**的时间戳
 *    就在播放器画面里，再叠一个假字会出现**两个时间戳**（见 `DeviceConfigOsdBlocks` 头注）。
 *    ⭐ 锚点还有一笔意外收益：用户可以**直接对着画面上那个真字拖锚点对齐**，免费的精确校准。
 */
interface OsdAnchor {
  /** 稳定标识：`time` 或 `item-0`… —— 当 `v-for` 的 key 用，别用坐标（拖动时坐标在变）。 */
  key: string;
  kind: "time" | "item";
  /** 文字行在数组里的下标；时间戳恒为 `-1`（它没有数组下标）。 */
  index: number;
  /** 画在锚点上的编号：时间戳是「时间戳」，文字是 1..8 —— **与侧栏列表编号同源**。 */
  label: string;
  x: number;
  y: number;
  /** 新增还没在画面上摆过（`0,0` 是合法坐标，只能靠 `placed` 判，见 `ConfigTextItem.placed`）。 */
  unplaced: boolean;
  /** 本次草稿动过它 ⇒ 还没下发到设备。 */
  draft: boolean;
  /** 所属开关已关闭 ⇒ 画面上不显示（**淡显**，不是隐藏：配置还在）。 */
  off: boolean;
}

/** OSD 组是否可编辑 —— 与画面组同一套闸门，只是按 osd 组算。 */
const osdEditable = computed(
  () =>
    !familyLoading.value &&
    !familyApplying.value &&
    props.canApply &&
    findConfigGroup(OSD_GROUP_KEY)?.state === "ready" &&
    !groupFactsMissing(OSD_GROUP_KEY)
);

const osdFactsMissing = computed(() => groupFactsMissing(OSD_GROUP_KEY));

const osdDirtyFields = computed(() => changedFieldKeys(familyValues[OSD_GROUP_KEY] ?? {}, familyBaseline[OSD_GROUP_KEY] ?? {}));
const osdDirtyCount = computed(() => osdDirtyFields.value.length);

/** OSD 组最近一次下发错误（与 `pictureError` 同一个机制、同一份 `familyError` 槽）。 */
const osdError = computed(() => (familyErrorGroup.value === OSD_GROUP_KEY ? familyError.value : ""));

/** 时间戳位置这一槽是否被改过。⛔ 逐轴比，别用 `osdDirtyCount > 0`——改格式也会让计数 >0。 */
const osdTimeDraft = computed(
  () =>
    String(textValue(OSD_GROUP_KEY, "timeX")) !== String(baselineSv(OSD_GROUP_KEY, "timeX")) ||
    String(textValue(OSD_GROUP_KEY, "timeY")) !== String(baselineSv(OSD_GROUP_KEY, "timeY"))
);

/**
 * 某一行文字是否被改过（逐行比，不是"整组 items 变了就全算改过"）。
 *
 * ⛔ 粒度必须是**行**：`changedFieldKeys` 只给到 `items` 这一级，用它的话用户改第 2 行、
 *    画布上所有行都会挂上「待下发」—— 那份"哪些还没下发"的提示就废了。
 */
function osdItemDraft(row: ConfigTextItem, index: number): boolean {
  const baselineRows = (familyBaseline[OSD_GROUP_KEY]?.items as ConfigTextItem[] | undefined) ?? [];
  const base = baselineRows[index];
  // 新增的行（基准里没有这一条）一律算待下发。
  if (!base) return true;
  return (
    String(row.text) !== String(base.text) ||
    Number(row.x) !== Number(base.x) ||
    Number(row.y) !== Number(base.y) ||
    // 新增未定位 → 定位 也算改过（设备上还没有这个位置）。
    (row.placed === false) !== (base.placed === false)
  );
}

const osdAnchors = computed<OsdAnchor[]>(() => {
  const anchors: OsdAnchor[] = [
    {
      key: "time",
      kind: "time",
      index: -1,
      label: "时间戳",
      x: Number(textValue(OSD_GROUP_KEY, "timeX")) || 0,
      y: Number(textValue(OSD_GROUP_KEY, "timeY")) || 0,
      unplaced: false,
      draft: osdTimeDraft.value,
      off: !boolValue(OSD_GROUP_KEY, "timeEnable")
    }
  ];
  for (const [position, row] of textItemsValue(OSD_GROUP_KEY, "items").entries()) {
    anchors.push({
      key: `item-${position}`,
      kind: "item",
      index: position,
      label: `${position + 1} ${row.text.trim() || "未命名"}`,
      x: Number(row.x) || 0,
      y: Number(row.y) || 0,
      unplaced: row.placed === false,
      draft: osdItemDraft(row, position),
      off: !boolValue(OSD_GROUP_KEY, "textEnable")
    });
  }
  return anchors;
});

/** 未定位的行数 —— 浮条据此把「下发」拦下来（`buildOSD` 那一层同样会拦）。 */
const osdUnplacedCount = computed(() => osdAnchors.value.filter(anchor => anchor.unplaced).length);

/**
 * 侧栏点「在画面上定位」→ 请画布把对应锚点闪一下。
 *
 * ⛔ 带着递增 `seq` 而不是只有 `{kind,index}`：连续点同一行两次，值不变的话
 *    watch 不会触发、第二次就"点了没反应"。计数是给 watch 用的，不是给界面用的。
 */
const osdFocusToken = ref<{ kind: "time" | "item"; index: number; seq: number } | null>(null);
let osdFocusSeq = 0;

function focusOsdAnchor(payload: { kind: "time" | "item"; index: number }) {
  osdFocusSeq += 1;
  osdFocusToken.value = { ...payload, seq: osdFocusSeq };
}

/**
 * 画布拖动落点 → 草稿。
 *
 * ⛔ 坐标的合法区间在 `buildOSD` 那一层收口（0~8192），这里只做"不给人负数"的兜底 ——
 *    在这里夹到画布尺寸会把"设备画布比坐标范围小"这件事悄悄改写掉。
 */
function setOsdTimePosition(axis: "x" | "y", value: number) {
  if (!osdEditable.value) return;
  const safe = Math.max(0, Math.min(8192, Math.round(Number(value) || 0)));
  setSv(OSD_GROUP_KEY, axis === "x" ? "timeX" : "timeY", String(safe));
}

/**
 * 画布拖动某一行文字 → 草稿，并把它标成**已定位**。
 *
 * ⛔ `placed: true` 是这一步的一半工作：拖动是"用户摆了位置"的唯一证据，
 *    不标的话 `buildOSD` 会一直拒发这一行（而用户看起来明明已经摆好了）。
 */
function setOsdItemPosition(index: number, x: number, y: number) {
  if (!osdEditable.value) return;
  const safeX = Math.max(0, Math.min(8192, Math.round(Number(x) || 0)));
  const safeY = Math.max(0, Math.min(8192, Math.round(Number(y) || 0)));
  const rows = textItemsValue(OSD_GROUP_KEY, "items").map((row, position) =>
    position === index ? { ...row, x: safeX, y: safeY, placed: true } : { ...row }
  );
  setTextItems(OSD_GROUP_KEY, "items", rows);
}

function readOsdGroup() {
  // 与 `readPictureGroup` 同一条路：一次读取拉回本族的全部配置类型，
  // 所以没有"只读 OSD 一组"的接口，也别为此新开一条（那会让两组的对账基准分叉）。
  void loadDeviceConfigs(true);
}

function revertOsdGroup() {
  resetGroup(OSD_GROUP_KEY);
  if (familyErrorGroup.value === OSD_GROUP_KEY) familyError.value = "";
}

async function applyOsdGroup() {
  await applyGroupConfig(OSD_GROUP_KEY);
}

/**
 * 设备**声明的图像坐标画布**（`width`=OSD 的 `Length`，`height`=OSD 的 `Width`）。
 *
 * ⭐ 为什么遮挡坐标的基准是它、而不是画面解码尺寸（2026-09-19 真机实测，海康 IPC
 * 192.168.10.203，码流 2560×1440，设备 `OSDConfig` 回读 `Length=704 Width=576`）：
 *   直接给设备发 `Point=0,0,640,360`，用 ZLM 抓图量出来的黑块是
 *   **x 0~90.8% / y 0~62.6%** —— 正好是 `640/704` 与 `360/576`。
 *   即：`Point` 的单位是像素，但基准是设备自己那把"图像尺"（国标 A.2.1.17 只说
 *   "单位像素"，没给基准；A.2.1.12 的 `Length/Width` 才是设备对"配置窗口像素数"的声明）。
 *   ⛔ 拿解码尺寸（2560×1440）当基准算坐标 ⇒ 发出去的数被设备按 704 画布解读
 *   ⇒ 遮挡块**整体右移并放大**，操作员看到的是"我画的框挡住的是别的块地方"。
 *   ⛔ 这两个字段**设备拒收**：写 2560×1440 或 720×576 都 200 OK，但回读仍是 704×576
 *   —— 平台不能"把画布改成真实图像尺寸"，只能**读它、照它算**。
 *
 * ⛔ 没读到 OSD 事实时必须回 `null`：`familyBaseline` 平时躺着的是
 *    `CONFIG_GROUP_DEFAULTS` 的 `1920×1080` 模板 —— 那是**平台的初值、不是设备值**，
 *    拿它当画布比拿解码尺寸更糟（它会伪装成"设备声明"，且错得没有任何征兆）。
 */
const pictureCanvasSize = computed<{ width: number; height: number } | null>(() => {
  if (groupFactsMissing(OSD_GROUP_KEY)) return null;
  // `Length` = 配置窗口长度（水平像素数）、`Width` = 配置窗口宽度（垂直像素数）。
  const length = Number(baselineSv(OSD_GROUP_KEY, "length"));
  const width = Number(baselineSv(OSD_GROUP_KEY, "width"));
  if (!Number.isFinite(length) || length <= 0 || !Number.isFinite(width) || width <= 0) return null;
  return { width: length, height: width };
});

/**
 * 画面组最近一次下发错误，供播放控制台的画面卡片/下发浮条给出反馈。
 *
 * ⛔ 必须有这条通道：浮条的「下发」按钮**可能被本地守卫拦下**（`buildPicture` 拒发
 *    「启用遮挡 + 没有区域」这个设备不认的组合）。那些守卫只把原因写进 `familyError`，
 *    而 `familyError` 只渲染在**侧栏 Drawer 自己的模板**里 —— 播放控制台的浮条拿不到，
 *    表现出来就是"点了下发，什么都不发生"。把归属挑明，浮条才解释得清为什么没发出去。
 */
const pictureError = computed(() => (familyErrorGroup.value === PICTURE_GROUP_KEY ? familyError.value : ""));

/**
 * 卡片是否可编辑 —— 与侧栏字段同一套闸门，只是按画面组算：
 * 读取/下发进行中不给改、没拿到设备事实不给改、通道离线不给改。
 */
const pictureEditable = computed(
  () =>
    !familyLoading.value &&
    !familyApplying.value &&
    props.canApply &&
    findConfigGroup(PICTURE_GROUP_KEY)?.state === "ready" &&
    !pictureFactsMissing.value
);

/** 遮挡区槽位。标准固定 4 个（`Seq` 1~4），`used` = 在设备画面上**真的占了一块面积**。 */
interface PictureRegion {
  seq: number;
  coords: number[];
  used: boolean;
}

/**
 * 一组坐标在画面上有没有面积（`右>左 && 下>上`）。
 *
 * ⛔ **不能用"四个数不全为 0"代替**：零面积（`lx==rx` 或 `ly==ry`）正是**删除**的表达 ——
 *    平台停用遮挡时就是把 `Seq 1..4` 全铺成 `0,0,0,0` 让设备删（见后端
 *    `PictureMaskClearPoint`），而设备会把它按**自己的画布**回读回来
 *    （2026-09-19 实测这台海康回的是 `704,576,704,576`，`Num` 仍是 1）。
 *    按"非全零"判 ⇒ 画面上什么都没有、卡片却列着一条区域、画布上还投影一个 0×0 的框 ——
 *    与现场报过的"设备已清除、页面还有一条数据"是同一个症状。
 */
function rectHasArea(coords: unknown): boolean {
  if (!Array.isArray(coords) || coords.length < 4) return false;
  const [left, top, right, bottom] = coords.map(value => Number(value) || 0);
  return right > left && bottom > top;
}

const pictureRegions = computed<PictureRegion[]>(() => {
  const regions: PictureRegion[] = [];
  for (let seq = 1; seq <= MAX_MASK_REGIONS; seq += 1) {
    const coords = coordsValue(PICTURE_GROUP_KEY, `mask${seq}`);
    regions.push({ seq, coords, used: rectHasArea(coords) });
  }
  return regions;
});

const pictureRegionCount = computed(() => pictureRegions.value.filter(region => region.used).length);
const pictureCanAddRegion = computed(() => pictureRegionCount.value < MAX_MASK_REGIONS);
const pictureMaskOn = computed(() => boolValue(PICTURE_GROUP_KEY, "maskOn"));

/**
 * 设备**事实上**的遮挡总闸（最近一次回读的 `On`，不含草稿）。
 *
 * ⛔ 与 `pictureMaskOn`（草稿值）必须分家：画布上画的是"设备此刻生效的遮挡"，
 *    判据只能是事实。用户刚点停用、还没下发时，设备上遮挡仍在生效，框必须继续画着；
 *    反过来，设备已停用而草稿没动时，框就不该再画 —— 否则用户会以为"没清掉"。
 */
const pictureAppliedMaskOn = computed(() => baselineSv(PICTURE_GROUP_KEY, "maskOn") === true);

/**
 * 设备侧「已停用、但区域还残留着」的槽位数。
 *
 * 国标 A.2.1.17 里 `RegionList` 是 `minOccurs=0` 的**独立**节点，**没有**"关闭时必须一并
 * 清空区域"这条规定 —— 所以设备保留区域本身是合法形态（2026-09-19 真机落库实证
 * `{"on":0,"regions":[{"seq":1,…}]}`，且设备端画面确实已无遮挡）。
 *
 * ⚠️ 平台是**会**尝试清掉的：`On=0` 的报文里 `Seq 1..4` 全铺零面积（后端
 *    `pictureMaskClearRegions`）。所以这里数出 >0，意思是**这次停用没能把区域清掉**
 *    （设备忽略、或回读还是旧的），不是"平台停用只关开关"。
 *
 * 这些区域此刻不生效，但下次「启用」会一起活过来（草稿从设备回读播种，启用时会把它们
 * 原样发上去）—— 界面既不能把它们摆成"当前遮挡"（用户会以为没清掉），也不能当它们不存在。
 */
const pictureRetainedRegionCount = computed(() => {
  if (pictureAppliedMaskOn.value) return 0;
  let count = 0;
  for (let seq = 1; seq <= MAX_MASK_REGIONS; seq += 1) {
    // 同上：零面积是"删除"的表达，不是"还留着一条"。
    if (rectHasArea(baselineSv(PICTURE_GROUP_KEY, `mask${seq}`))) count += 1;
  }
  return count;
});
const pictureMirror = computed(() => textValue(PICTURE_GROUP_KEY, "mirror"));
const pictureDirtyCount = computed(() => dirtyFieldCount(PICTURE_GROUP_KEY));

/**
 * 画面组里到底哪几个字段脏了（`mask1`..`mask4` / `maskOn` / `mirror`）。
 *
 * ⛔ 画布要靠它区分「草稿框」与「生效框」：只看脏值**总数**是分不出是哪一块的。
 *    没有这一层，画面上所有框只能同一种颜色，用户会以为"框全都得重发一遍"，
 *    反倒不敢下发；有了它，只有真正改过的那几块才换色，改动一眼可见。
 */
const pictureDirtyFields = computed(() =>
  changedFieldKeys(familyValues[PICTURE_GROUP_KEY] ?? {}, familyBaseline[PICTURE_GROUP_KEY] ?? {})
);

/**
 * 本次草稿有没有动过遮挡本身（区域或总闸），不管镜像。
 *
 * ⛔ 卡片要靠它决定"露不露槽位"：设备已停用但区域残留时，卡片显示的是
 *    「遮挡已停用」的说明态；可用户一旦开始画框 / 开总闸，就必须把槽位网格让出来，
 *    否则他改完看不到自己改了什么。（只算 `pictureDirtyCount > 0` 不行 ——
 *    改一下镜像也会把说明态顶掉。）
 */
const pictureMaskDraftTouched = computed(() =>
  pictureDirtyFields.value.some(field => field === "maskOn" || /^mask[1-4]$/.test(field))
);

/** 写入某个遮挡槽位的两个角点。内部一律在**角点空间**运算（不是位置 + 宽高）。 */
function setPictureRegion(seq: number, rect: { left: number; top: number; right: number; bottom: number }) {
  if (!Number.isInteger(seq) || seq < 1 || seq > MAX_MASK_REGIONS) return;
  // ⛔ 必须整组替换而不是原地改数组：`configGroupDefaults` 只做浅拷贝，
  //    各槽位的数组可能与基准值共享同一引用，原地改会连基准一起污染。
  setSv(PICTURE_GROUP_KEY, `mask${seq}`, [rect.left, rect.top, rect.right, rect.bottom]);
  // 画了区域却忘开总闸，下发出去等于没画 —— 顺手打开。
  if (!boolValue(PICTURE_GROUP_KEY, "maskOn")) setSv(PICTURE_GROUP_KEY, "maskOn", true);
}

/**
 * 清掉一个遮挡槽位。
 *
 * ⛔ 只清坐标、**不重排 `Seq`**：设备侧「区域 3」的语义就是编号 3，
 *    删掉 2 之后把 3/4 前移会让设备上的区域被静默改名，而对账看起来还可能一致。
 *
 * ⭐ 与 [setPictureRegion] 的"画了区域顺手开总闸"**对称**：清掉**最后一个**区域时
 *    顺手把总闸关掉。"没有区域的遮挡"本来就等于没有遮挡 —— 而且这里**不能**留着总闸：
 *    报文层会把"没给的槽位"补成零面积删除（后端 `pictureMaskFullRegions`），
 *    所以 `{on:1,regions:[]}` 的后果不是"什么都没发生"，而是"把设备里所有区域清掉"。
 *    清完最后一个区域还让开关停在"开"，等于给用户看一个自相矛盾的状态。
 *
 * ⛔ 与 [setPictureRegion] 共用同一条换算/下发链路，所以这里的"清掉"**会真的下发成删除**：
 *    删除一个区域靠的是"给那个 `Seq` 显式发零面积"，而不是"报文里不出现它" ——
 *    后者在设备侧是"别碰它"（2026-09-20 真机：删掉 Seq4 后下发，设备回读 `Num="4"`、
 *    Seq4 原样还在，界面表现为"遮挡区删不掉"且额外报一次对账不一致）。
 */
function clearPictureRegion(seq: number) {
  if (!Number.isInteger(seq) || seq < 1 || seq > MAX_MASK_REGIONS) return;
  setSv(PICTURE_GROUP_KEY, `mask${seq}`, [0, 0, 0, 0]);
  // 先清完当前槽位再判，否则会把自己算成"还在用"。
  const stillUsed = pictureRegions.value.some(region => region.used);
  if (!stillUsed && boolValue(PICTURE_GROUP_KEY, "maskOn")) {
    setSv(PICTURE_GROUP_KEY, "maskOn", false);
  }
}

/** 下一个空槽位编号；4 个都在用则返回 0。 */
function nextPictureRegionSeq(): number {
  const spare = pictureRegions.value.find(region => !region.used);
  return spare ? spare.seq : 0;
}

function setPictureMaskOn(value: boolean) {
  setSv(PICTURE_GROUP_KEY, "maskOn", value);
}

function setPictureMirror(value: string) {
  setSv(PICTURE_GROUP_KEY, "mirror", value);
}

/** 把画面组还原成最近一次回读值（丢弃本组草稿）。 */
function revertPictureGroup() {
  resetGroup(PICTURE_GROUP_KEY);
  // 草稿都丢了，那句"为什么没发出去"就过期了 —— 留着会让人以为现在这个状态仍然发不出去。
  clearPictureError();
  // 提示同理过期：它解释的是"刚才那次下发为什么没效果"，草稿已还原，不再指任何状态。
  pictureMaskNotice.value = "";
}

/**
 * 清掉画面组的下发错误（只清归 picture 组的那条）。
 *
 * ⛔ 不能直接 `familyError.value = ""`：那是跨组共用的槽，别的组正显示着自己的错时
 *    会被这一下抹掉，用户就再也看不到那条了。
 */
function clearPictureError() {
  if (familyErrorGroup.value !== PICTURE_GROUP_KEY) return;
  familyError.value = "";
  familyErrorGroup.value = "";
}

function readPictureGroup() {
  void loadDeviceConfigs(true);
}

async function applyPictureGroup() {
  await applyGroupConfig(PICTURE_GROUP_KEY);
}

/** 遮挡相关的表单键（总闸 + 4 个槽位）。 */
function pictureMaskFieldKeys(): string[] {
  const keys = ["maskOn"];
  for (let index = 0; index < MAX_MASK_REGIONS; index += 1) keys.push(`mask${index + 1}`);
  return keys;
}

/**
 * 把**遮挡相关**的草稿提交成新基准 —— 点开关即时下发成功后的收尾。
 *
 * ⛔ 只推 `maskOn` 与 `mask1..mask4`，**不碰 `mirror`**：即时下发只发了 `PictureMask`
 *    这一块（见 [applyPictureMaskOnly]），镜像草稿还在用户手里等着统一下发。
 *    用整组的 `resetGroup` 会把没发出去的镜像改动一并当成"已下发"，浮条也会凭空消失。
 */
function commitPictureMaskDraft() {
  const values = familyValues[PICTURE_GROUP_KEY];
  const baseline = familyBaseline[PICTURE_GROUP_KEY];
  if (!values || !baseline) return;
  for (const key of pictureMaskFieldKeys()) baseline[key] = values[key];
}

/**
 * 组织"这次遮挡下发要跟用户说的一句话"（无话可说时返回空串）。
 *
 * ⛔ 必须分两段拼：`buildPicture` 只知道"本次没带区域"，而"设备里那几块旧区域会因此
 *    被清掉"只有这里知道（要读设备事实 `pictureRetainedRegionCount`）。
 *    少了后半句，用户会在"点了个启用，结果设备上的遮挡全没了"时以为平台乱发了数据。
 *
 * ⚠️ 2026-09-20 口径变更（真机定因"省略 Seq ≠ 删除"）：这一句原来是「设备里还留着 N 个
 *    旧遮挡区域，**启用后它们会一起生效**」—— 那是"报文里不带区域 ⇒ 设备保留原样"时代的
 *    说法。现在报文层会把没给的槽位补成零面积，`On=1` + 无区域的真实后果是**删除**，
 *    所以提示必须改成"会被清掉"，否则就是拿一句反话去解释一次真实的数据丢失。
 *
 *    ⛔ 但**常规**的"重新启用"路径不受影响：草稿是从设备回读播种的（`applyFamilyEntries`），
 *    设备里保留的那几个区域本来就在草稿里，点启用会把它们**原样发上去**（那时
 *    `built.notice` 为空、这里根本不会执行）—— 卡片上「再点启用会让它们重新生效」仍然成立。
 */
function pictureMaskApplyNotice(built: BuildResult): string {
  // `built.notice` 非空 ⟺ 本次是 `On=1` 且**没带任何区域**（见 [buildPicture]），
  // 两个后缀都只在这种情形下才成立，所以不再重复判 `on`。
  if (!built.notice) return "";
  return (
    built.notice +
    (pictureRetainedRegionCount.value > 0
      ? `设备里还留着 ${pictureRetainedRegionCount.value} 个旧遮挡区域，本次没带区域，它们会被一并清掉。`
      : "画面上不会有任何变化 —— 要设置遮挡请点「新建」框选范围。")
  );
}

/**
 * **只**下发「画面遮挡」这一块 —— 播放控制台卡片上点总闸开关时的即时下发。
 *
 * ⛔ 不复用 [applyGroupConfig]：那一组同时含 `FrameMirror`，而用户点的是"启用遮挡"，
 *    不该顺手把还没点下发的镜像草稿一起提交出去。点开关 = 只提交遮挡，
 *    收尾也相应只用 [commitPictureMaskDraft] 推遮挡那几格。
 *
 * ⛔ 成功后就地把草稿推成基准，不能干等回读：否则浮条会一直挂着"1 项画面改动未下发"、
 *    按钮也一直停在"将启用"（草稿 `on` ≠ 回读 `on`），用户会以为根本没发出去。
 *    设备真值仍由回读兜底 —— 有对账就轮询，跑完会用回读值覆盖这里。
 */
async function applyPictureMaskOnly() {
  if (!props.channelId) return;
  const built = buildDeviceConfigBlocks(PICTURE_GROUP_KEY, familyValues[PICTURE_GROUP_KEY] ?? {});
  if (built.error) {
    familyError.value = built.error;
    familyErrorGroup.value = PICTURE_GROUP_KEY;
    return;
  }
  const mask = built.blocks.pictureMask;
  if (!mask) return;
  familyApplying.value = true;
  familyError.value = "";
  familyErrorGroup.value = "";
  // 提示与这次下发绑定：每次都重新判定，不留上一轮的陈旧提示。
  pictureMaskNotice.value = pictureMaskApplyNotice(built);
  try {
    const response = await applyChannelDeviceConfigs(props.channelId, { pictureMask: mask }, `device-config-mask-${Date.now()}`);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "下发失败");
    commitPictureMaskDraft();
    if (response.data.reconcilePending) scheduleFamilyPoll();
  } catch (error) {
    familyError.value = readErrorMessage(error, "下发失败");
    familyErrorGroup.value = PICTURE_GROUP_KEY;
    pictureMaskNotice.value = "";
  } finally {
    familyApplying.value = false;
  }
}

/* ─────────────────────── 生命周期 ─────────────────────── */

const titleText = computed(() => props.channelName?.trim() || props.deviceName?.trim() || "未选择设备");

const previewResolution = computed(() => {
  const row = videoRows.value.find(item => item.streamNumber === Number(streamProfile.value));
  const text = resolutionText(row?.resolution);
  return text && text !== "—" ? text : "1280x720";
});

function close() {
  clearVideoPoll();
  clearFamilyPoll();
  emit("update:visible", false);
}

watch(
  () => props.visible,
  visible => {
    if (!visible) {
      clearVideoPoll();
      clearFamilyPoll();
      return;
    }
    if (!props.channelId) return;
    if (!videoRows.value.length) void loadVideoParams(false);
    if (!familyEntries.value.length) void loadDeviceConfigs(false);
  },
  { immediate: true }
);

watch(
  () => props.channelId,
  () => {
    videoRows.value = [];
    videoDraft.value = [];
    videoReconcile.value = null;
    videoStreamNumberList.value = "";
    videoObservedAt.value = "";
    videoError.value = "";
    familyEntries.value = [];
    familyAbsent.value = [];
    familyReconcile.value = null;
    familyObservedAt.value = "";
    familyError.value = "";
    familyErrorGroup.value = "";
    pictureMaskNotice.value = "";
    // 换通道时把表单连同基准一起复位：留着上一个通道的回读值会让"待下发 N 项"
    // 把两个设备的值算成一组差异。
    for (const group of CONFIG_GROUPS) {
      familyValues[group.key] = configGroupDefaults(group.key);
      familyBaseline[group.key] = configGroupDefaults(group.key);
    }
    if (props.visible && props.channelId) {
      void loadVideoParams(false);
      void loadDeviceConfigs(false);
    }
  }
);

function selectGroup(key: string) {
  if (configGroups.value.some(group => group.key === key)) activeGroupKey.value = key;
}

function selectStream(value: string) {
  streamProfile.value = value;
}

/**
 * 图像叠加面板的 **props 袋**（2026-09-20，给播放控制台底栏用）。
 *
 * ⭐ 场景：底栏把「图像叠加」整块摆到画面下方（老板要求"一个页面全展示"），
 *    而侧栏那一刻停在 `video-param`、不再切到 `osd` 组 ⇒ 面板不能靠"当前分组"驱动，
 *    改为把这一组的值整袋交给宿主渲染。
 *
 * ⛔ 宿主**不要**自己从 `familyValues` 拼这些值、也不要另存一份：下面三个写口是唯一入口，
 *    底栏与抽屉共用同一份 `familyValues[osd]`（两份状态必然分叉，本仓已踩过三次）。
 * ⛔ `canvas` / `pictureCanvasSize` 是**设备声明的图像坐标画布**，不是画面解码尺寸 ——
 *    真机实测口径见 [pictureCanvasSize] 的注释。
 */
const osdBlocks = computed(() => ({
  timeEnable: boolValue(OSD_GROUP_KEY, "timeEnable"),
  timeType: textValue(OSD_GROUP_KEY, "timeType"),
  timeX: textValue(OSD_GROUP_KEY, "timeX"),
  timeY: textValue(OSD_GROUP_KEY, "timeY"),
  textEnable: boolValue(OSD_GROUP_KEY, "textEnable"),
  items: textItemsValue(OSD_GROUP_KEY, "items"),
  canvas: pictureCanvasSize.value,
  // ⛔ 用 `!osdEditable` 而**不是** `familyFieldsDisabled`：后者读的是 `activeGroup`
  //    （抽屉当前分组），而底栏这块现在常驻渲染、抽屉却停在 `video-param` ——
  //    `activeGroupFactsMissing` 在 `activeIsVideo` 时直接返回 false，于是「设备没回
  //    OSDConfig」时底栏控件反而是可编辑的，等于把平台空白模板（timeX=10 这类）
  //    当成设备现状让人盲写。`osdEditable` 才是按 osd 组自身算的那一套闸门。
  disabled: !osdEditable.value,
  maxItems: MAX_OSD_TEXT_ITEMS,
  editing: props.osdEditing,
  canvasLinked: props.osdCanvasLinked
}));

/** 开关 / 时间格式这类单值字段（底栏 `v-bind` 之后再接一个写口就够）。 */
function setOsdFlag(fieldKey: string, value: FieldValue) {
  setSv(OSD_GROUP_KEY, fieldKey, value);
}

/** 叠加文字整表（增 / 删 / 改文字都在宿主侧算好后整表回写，与内联渲染同一条路）。 */
function setOsdItems(rows: ConfigTextItem[]) {
  setTextItems(OSD_GROUP_KEY, "items", rows);
}

defineExpose({
  selectGroup,
  selectStream,
  // 这三个入口按**当前分组**走对应通道：外部（播放控制台标签页的按钮）不需要知道
  // 现在这组是哪条通道。
  read: () => readActiveGroup(),
  reset: () => resetActiveGroup(),
  apply: () => applyActiveGroup(),
  // 播放控制台底部「画面卡片」专用：钉在 picture 组上，读写同一份 familyValues。
  pictureEditable,
  pictureRegions,
  pictureRegionCount,
  pictureCanAddRegion,
  pictureMaskOn,
  /** 设备**事实上**的总闸（回读值）—— 画布决定"画不画框"要用它，不能用草稿值。 */
  pictureAppliedMaskOn,
  /** 设备"已停用但区域残留"的槽位数 —— 卡片据此给出说明而不是摆成当前遮挡。 */
  pictureRetainedRegionCount,
  /** 本次草稿动过遮挡本身没有 —— 动过就得把槽位网格让出来。 */
  pictureMaskDraftTouched,
  pictureMirror,
  /**
   * 设备声明的图像坐标画布（`null` = 本次没读到 OSD 事实）。
   *
   * ⛔ 播放控制台**必须**用它、而不是画面解码尺寸去换算遮挡坐标 ——
   *    真机实测口径见 [pictureCanvasSize] 的注释。
   */
  pictureCanvasSize,
  pictureDirtyCount,
  pictureDirtyFields,
  pictureFactsMissing,
  pictureAbsentTypes,
  pictureError,
  /** 本次下发"发出去了但你要知道效果"的说明（如：启用了却没带区域）。 */
  pictureMaskNotice,
  setPictureRegion,
  clearPictureRegion,
  nextPictureRegionSeq,
  setPictureMaskOn,
  setPictureMirror,
  revertPictureGroup,
  readPictureGroup,
  applyPictureGroup,
  /** 点开关时的即时下发：只发 `PictureMask` 这一块。 */
  applyPictureMaskOnly,
  // ── 播放控制台画布上的 OSD 锚点层专用：钉在 osd 组上，与画面组同一套读写口 ──
  osdEditable,
  /** 画布要画的锚点（时间戳 + 各条文字），含四态：已生效 / 待下发 / 未定位 / 已关闭。 */
  osdAnchors,
  osdDirtyCount,
  osdDirtyFields,
  osdUnplacedCount,
  osdFactsMissing,
  osdError,
  /** 侧栏点「在画面上定位」→ 画布把对应锚点闪一下（带递增 seq，连点两次也会触发）。 */
  osdFocusToken,
  setOsdTimePosition,
  setOsdItemPosition,
  readOsdGroup,
  revertOsdGroup,
  applyOsdGroup,
  // ── 底栏「图像叠加」整块面板用（2026-09-20）：props 袋 + 三个写口 ──
  /** 直接 `v-bind` 到 `DeviceConfigOsdBlocks` 上；改值走下面三个写口。 */
  osdBlocks,
  setOsdFlag,
  setOsdItems,
  /** 底栏点「在画面上定位」→ 画布闪一下对应锚点（与侧栏同一个口）。 */
  focusOsdAnchor,
  /** 浮条「下发」：画面 + OSD 的草稿**合并成一条报文**发出去。 */
  applyPictureAndOsdGroups
});

onBeforeUnmount(() => {
  clearVideoPoll();
  clearFamilyPoll();
});
</script>

<template>
  <div
    v-if="visible || embedded"
    class="dcg-host"
    :class="{ 'dcg-host--embedded': embedded }"
    @click.self="embedded ? undefined : close()"
  >
    <section
      class="dcg-window"
      :class="{ 'dcg-window--embedded': embedded }"
      role="dialog"
      :aria-modal="embedded ? undefined : true"
      :aria-label="`设备配置 · ${titleText}`"
    >
      <header v-if="!embedded" class="dcg-titlebar">
        <span class="dcg-titlebar-mark" />
        <span class="dcg-titlebar-text">设备配置</span>
        <span class="dcg-titlebar-sep">·</span>
        <span class="dcg-titlebar-target" data-testid="dcg-title-target">{{ titleText }}</span>
        <span class="dcg-titlebar-code">{{ deviceCode || "—" }}</span>
        <span v-if="effectiveVersion || videoRegisteredVersion" class="dcg-titlebar-ver"
          >GB/T 28181-{{ effectiveVersion || videoRegisteredVersion }}</span
        >
        <button type="button" class="dcg-close" aria-label="关闭" data-testid="dcg-close" @click="close">
          <X :size="14" />
        </button>
      </header>

      <div class="dcg-body">
        <aside v-if="!embedded" class="dcg-preview">
          <div class="dcg-screen">
            <div class="dcg-screen-empty">
              <Camera :size="26" />
              <p>实时预览</p>
              <em>接入取流后在此显示画面</em>
            </div>
            <span class="dcg-screen-badge" :class="online ? 'is-on' : 'is-off'">
              <Wifi v-if="online" :size="11" />
              <WifiOff v-else :size="11" />
              {{ online ? "在线" : "离线" }}
            </span>
            <span class="dcg-screen-res">{{ previewResolution }}</span>
          </div>

          <div class="dcg-actionbar">
            <button
              type="button"
              class="dcg-btn"
              data-testid="dcg-reset"
              :disabled="activeApplying || !activeDirtyCount"
              @click="resetActiveGroup"
            >
              <RotateCcw :size="12" />还原
            </button>
            <button
              type="button"
              class="dcg-btn"
              data-testid="dcg-read"
              :disabled="activeLoading || !canRead || !online || !activeIsReady"
              @click="readActiveGroup"
            >
              <RefreshCcw :size="12" />读取
            </button>
            <button
              type="button"
              class="dcg-btn is-primary"
              data-testid="dcg-apply"
              :disabled="activeApplying || !activeDirtyCount || !canApply || !activeIsReady"
              @click="applyActiveGroup"
            >
              <Send :size="12" />下发
            </button>
          </div>

          <div class="dcg-ptz">
            <div class="dcg-ptz-pad" aria-hidden="true">
              <span class="dcg-ptz-arrow is-up" />
              <span class="dcg-ptz-arrow is-down" />
              <span class="dcg-ptz-arrow is-left" />
              <span class="dcg-ptz-arrow is-right" />
              <span class="dcg-ptz-center" />
            </div>
            <div class="dcg-ptz-zoom">
              <div class="dcg-zoom-row">
                <button type="button" class="dcg-step-btn" aria-label="变倍减小"><Minus :size="11" /></button>
                <span>变倍</span>
                <button type="button" class="dcg-step-btn" aria-label="变倍增大"><Plus :size="11" /></button>
              </div>
              <div class="dcg-zoom-row">
                <button type="button" class="dcg-step-btn" aria-label="变焦减小"><Minus :size="11" /></button>
                <span>变焦</span>
                <button type="button" class="dcg-step-btn" aria-label="变焦增大"><Plus :size="11" /></button>
              </div>
            </div>
          </div>

          <div class="dcg-steprow">
            <span>步长</span>
            <a-select
              :model-value="String(ptzStep)"
              class="dcg-select is-mini"
              size="small"
              aria-label="云台步长"
              @change="ptzStep = Number(selectValue($event))"
            >
              <a-option v-for="step in PTZ_STEPS" :key="step" :value="String(step)">{{ step }}</a-option>
            </a-select>
          </div>
        </aside>

        <nav v-if="configGroups.length > 1" class="dcg-nav" aria-label="配置分组">
          <button
            v-for="group in configGroups"
            :key="group.key"
            type="button"
            class="dcg-nav-item"
            :class="{ 'is-active': group.key === activeGroupKey }"
            :data-testid="`dcg-nav-${group.key}`"
            :data-state="group.state"
            :title="group.navLabel ? group.label : undefined"
            @click="activeGroupKey = group.key"
          >
            <ChevronRight :size="10" class="dcg-nav-caret" />
            <span class="dcg-nav-text">
              <span class="dcg-nav-label">{{ group.navLabel ?? group.label }}</span>
              <span v-if="!embedded" class="dcg-nav-sub">{{ group.std }}</span>
            </span>
            <i
              v-if="!embedded"
              class="dcg-nav-flag"
              :class="`is-${group.state}`"
              :title="group.state === 'ready' ? '已接入后端' : '未接入后端（静态形态）'"
            />
          </button>
        </nav>

        <section class="dcg-params">
          <header class="dcg-params-head">
            <div v-if="!embedded || configGroups.length <= 1" class="dcg-params-title-wrap">
              <span
                class="dcg-params-title"
                data-testid="dcg-group-title"
                :title="activeGroupTitle === activeGroup.label ? undefined : activeGroup.label"
                >{{ activeGroupTitle }}</span
              >
              <span v-if="!embedded && activeGroup.since === '2022'" class="dcg-standard-badge" data-testid="dcg-standard-badge"
                >GB/T 28181-2022</span
              >
            </div>
            <span v-if="!embedded" class="dcg-params-std">{{ activeGroup.std }}</span>
            <span v-if="!embedded" class="dcg-params-state" :class="`is-${activeGroup.state}`" data-testid="dcg-group-state">
              {{ activeGroup.state === "ready" ? "已接入" : "未接入" }}
            </span>
            <div
              v-if="embedded && !detailTarget && activeIsReady"
              class="dcg-embedded-actions"
              data-testid="dcg-embedded-actions"
            >
              <button
                type="button"
                class="dcg-btn"
                data-testid="dcg-reset"
                title="还原为最近回读值"
                :disabled="activeApplying || !activeDirtyCount"
                @click="resetActiveGroup"
              >
                <RotateCcw :size="12" />还原
              </button>
              <button
                type="button"
                class="dcg-btn"
                data-testid="dcg-read"
                :disabled="activeLoading || !canRead || !online"
                @click="readActiveGroup"
              >
                <RefreshCcw :size="12" />读取
              </button>
              <button
                type="button"
                class="dcg-btn is-primary"
                data-testid="dcg-apply"
                :disabled="activeApplying || !activeDirtyCount || !canApply"
                @click="applyActiveGroup"
              >
                <Send :size="12" />下发
              </button>
            </div>
            <label v-if="activeIsVideo" class="dcg-params-profile">
              <span>配置文件</span>
              <a-select
                :model-value="streamProfile"
                size="small"
                :disabled="activeGroup.state !== 'ready'"
                aria-label="配置文件"
                style="width: 112px"
                @change="streamProfile = selectValue($event)"
              >
                <a-option value="0">主码流</a-option>
                <a-option value="1">子码流 1</a-option>
                <a-option value="2">子码流 2</a-option>
              </a-select>
            </label>
          </header>

          <p v-if="!embedded" class="dcg-params-summary">
            <Info :size="11" />
            <span>{{ activeGroup.summary }}</span>
          </p>

          <div class="dcg-params-body" :class="{ 'is-static': activeGroup.state === 'static' }">
            <template v-if="activeGroup.key === 'video-param'">
              <div class="dcg-reconcile" :class="`is-${reconcileTone}`" data-testid="dcg-reconcile">
                <span class="dcg-reconcile-dot" />
                <span>{{ reconcileText }}</span>
              </div>
              <p v-if="versionNotice && !embedded" class="dcg-notice" data-testid="dcg-version-notice">{{ versionNotice }}</p>
              <p v-if="videoError" class="dcg-error" data-testid="dcg-error">{{ videoError }}</p>

              <div v-if="!visibleVideoRows.length" class="dcg-empty" data-testid="dcg-empty">
                <p>{{ emptyText }}</p>
                <em>点击「读取」获取当前编码参数</em>
              </div>

              <div
                v-for="row in visibleVideoRows"
                :key="row.streamNumber"
                class="dcg-stream"
                :data-testid="`dcg-stream-${row.streamNumber}`"
              >
                <div class="dcg-stream-hd">
                  <span>码流 {{ row.streamNumber }}</span>
                  <em>{{ streamNumberText(row.streamNumber) }}</em>
                  <i v-if="rowChanged(row)" class="dcg-stream-dirty">已改</i>
                </div>

                <div class="dcg-field-group" data-testid="dcg-field-group" data-group="encoding">
                  <div class="dcg-field-group-head"><strong>编码</strong><span>VideoFormat · Resolution</span></div>
                  <!-- ⭐ 2026-09-20：编码格式与分辨率**同一行**。这两项本来就是同一件事的两半
                       （拉流放不放得出来），拆两行只是白占一行 30px —— 老板原话「把空间节省出来」。
                       ⛔ 一行里仍**各自带来源徽标**：对账粒度是"每个格子来自设备还是缺省"，
                          挤掉徽标等于把对账能力换成省出来的那点宽度。
                       ⛔ 自定义分辨率走 `flex-wrap` 折到第二行（见 `.dcg-pair-cell` 样式），
                          不在 90px 里跟下拉框抢位置。 -->
                  <div class="dcg-row is-pair" data-testid="dcg-row-encoding">
                    <div class="dcg-pair-cell">
                      <span class="dcg-row-label">编码格式</span>
                      <div class="dcg-row-control">
                        <a-select
                          :model-value="row.videoFormat"
                          class="dcg-select"
                          size="small"
                          :disabled="videoFieldsDisabled"
                          :data-testid="`dcg-format-${row.streamNumber}`"
                          @change="row.videoFormat = selectValue($event)"
                        >
                          <a-option v-for="option in VIDEO_FORMAT_OPTIONS" :key="option.value" :value="option.value">
                            {{ option.label }}
                          </a-option>
                        </a-select>
                      </div>
                      <span
                        class="dcg-row-hint"
                        :data-source="cellBadge(row, 'videoFormat')"
                        :title="badgeTitle(cellBadge(row, 'videoFormat'))"
                        >{{ cellBadge(row, "videoFormat") }}</span
                      >
                    </div>

                    <div class="dcg-pair-cell">
                      <span class="dcg-row-label">分辨率</span>
                      <div class="dcg-row-control">
                        <a-select
                          :model-value="resolutionSelectValue(row)"
                          class="dcg-select"
                          size="small"
                          :disabled="videoFieldsDisabled"
                          :data-testid="`dcg-resolution-${row.streamNumber}`"
                          @change="onResolutionSelect(row, $event)"
                        >
                          <a-option v-for="option in RESOLUTION_OPTIONS" :key="option.value" :value="option.value">
                            {{ option.label }}
                          </a-option>
                          <a-option :value="CUSTOM_RESOLUTION">自定义…</a-option>
                        </a-select>
                        <input
                          v-if="!isValidResolutionCode(row.resolution)"
                          class="dcg-input is-narrow"
                          :value="row.resolution"
                          :disabled="videoFieldsDisabled"
                          placeholder="1920x1080"
                          aria-label="自定义分辨率"
                          :data-testid="`dcg-resolution-custom-${row.streamNumber}`"
                          @change="row.resolution = ($event.target as HTMLInputElement).value"
                        />
                      </div>
                      <span
                        class="dcg-row-hint"
                        :data-source="cellBadge(row, 'resolution')"
                        :title="badgeTitle(cellBadge(row, 'resolution'))"
                        >{{ cellBadge(row, "resolution") }}</span
                      >
                    </div>
                  </div>
                </div>

                <div class="dcg-field-group" data-testid="dcg-field-group" data-group="picture">
                  <div class="dcg-field-group-head"><strong>画面</strong><span>FrameRate · 0–99 fps</span></div>
                  <div class="dcg-row">
                    <span class="dcg-row-label">帧率</span>
                    <div class="dcg-row-control">
                      <DeviceConfigSlider
                        :model-value="row.frameRate"
                        :min="0"
                        :max="99"
                        label="帧率"
                        :disabled="videoFieldsDisabled"
                        :data-testid="`dcg-frame-rate-${row.streamNumber}`"
                        @update:model-value="row.frameRate = $event"
                      />
                    </div>
                    <span
                      class="dcg-row-hint"
                      :data-source="cellBadge(row, 'frameRate')"
                      :title="badgeTitle(cellBadge(row, 'frameRate'))"
                      >{{ cellBadge(row, "frameRate") }}</span
                    >
                  </div>
                </div>

                <div class="dcg-field-group" data-testid="dcg-field-group" data-group="bitrate">
                  <div class="dcg-field-group-head"><strong>码率</strong><span>BitRateType · VideoBitRate</span></div>
                  <div class="dcg-row">
                    <span class="dcg-row-label">码率类型</span>
                    <div class="dcg-row-control">
                      <div class="dcg-segment" role="group" aria-label="码率类型">
                        <button
                          v-for="option in BIT_RATE_TYPE_OPTIONS"
                          :key="option.value"
                          type="button"
                          :class="{ 'is-on': row.bitRateType === option.value }"
                          :disabled="videoFieldsDisabled"
                          :data-testid="`dcg-bit-rate-type-${row.streamNumber}-${option.value}`"
                          @click="row.bitRateType = option.value"
                        >
                          {{ option.label }}
                        </button>
                      </div>
                    </div>
                    <span
                      class="dcg-row-hint"
                      :data-source="cellBadge(row, 'bitRateType')"
                      :title="badgeTitle(cellBadge(row, 'bitRateType'))"
                      >{{ cellBadge(row, "bitRateType") }}</span
                    >
                  </div>

                  <div class="dcg-row">
                    <span class="dcg-row-label">码率</span>
                    <div class="dcg-row-control">
                      <DeviceConfigSlider
                        :model-value="row.videoBitRate ?? ''"
                        :min="0"
                        :max="100000"
                        :step="100"
                        unit="kb/s"
                        label="码率"
                        :disabled="videoFieldsDisabled || videoBitRateDisabled(row)"
                        :data-testid="`dcg-bit-rate-${row.streamNumber}`"
                        @update:model-value="row.videoBitRate = $event"
                      />
                    </div>
                    <span class="dcg-row-hint" :data-source="bitRateBadge(row)" :title="badgeTitle(bitRateBadge(row))">{{
                      bitRateBadge(row)
                    }}</span>
                  </div>
                  <p class="dcg-field-group-note">CBR 时必填，单位 kb/s；VBR 时不发送 VideoBitRate。</p>
                </div>
              </div>
            </template>

            <template v-else>
              <template v-if="activeIsReady">
                <div class="dcg-reconcile" :class="`is-${reconcileTone}`" data-testid="dcg-reconcile">
                  <span class="dcg-reconcile-dot" />
                  <span>{{ reconcileText }}</span>
                </div>
                <p v-if="versionNotice && !embedded" class="dcg-notice" data-testid="dcg-version-notice">{{ versionNotice }}</p>
                <p v-if="activeError" class="dcg-error" data-testid="dcg-error">{{ activeError }}</p>
                <p v-if="noFactsNotice" class="dcg-notice" data-testid="dcg-no-facts-notice">
                  <Info :size="11" />
                  <span>{{ noFactsNotice }}</span>
                </p>
                <p v-if="activeGroupAbsentTypes.length" class="dcg-notice" data-testid="dcg-absent-notice">
                  <Info :size="11" />
                  <span v-if="embedded">设备未返回部分配置，其他字段仍按设备回读值显示；未返回的那部分暂不可编辑。</span>
                  <span v-else>
                    本次读取设备未返回 {{ activeGroupAbsentTypes.join(" / ") }}：多半是该设备不认识这个配置类型（2016
                    设备上它不存在）。本组其余类型仍按回读值显示；未返回的那部分暂不可编辑。
                  </span>
                </p>
              </template>
              <p v-else class="dcg-hintline" data-testid="dcg-static-note">
                <Info :size="11" />
                <span>{{
                  embedded
                    ? "当前设备暂不支持此项配置。"
                    : "该分组尚未接入后端，本页为静态形态预览。控件与字段口径已按标准定型，接入后即可读写。"
                }}</span>
              </p>

              <div v-if="activeIsReady && !activeDirtyCount && !embedded" class="dcg-hintline">
                <Info :size="11" />
                <span>面板显示的是最近一次回读到的设备生效值；改任一项后可下发。</span>
              </div>

              <!-- ═══ OSD：对象块形态（时间戳 / 叠加文字 / 只读坐标画布）═══
                   这一组不是"平铺的参数表"，而是"画面上摆的两样东西"。通用字段行渲染不出
                   对象块形态 —— 硬塞就得把位置拆回两个滑杆，而那正是要抹平的落差。 -->
              <div v-if="activeGroup.key === OSD_GROUP_KEY" class="dcg-osd-blocks" data-testid="dcg-osd-blocks">
                <DeviceConfigOsdBlocks
                  :time-enable="boolValue(OSD_GROUP_KEY, 'timeEnable')"
                  :time-type="textValue(OSD_GROUP_KEY, 'timeType')"
                  :time-x="textValue(OSD_GROUP_KEY, 'timeX')"
                  :time-y="textValue(OSD_GROUP_KEY, 'timeY')"
                  :text-enable="boolValue(OSD_GROUP_KEY, 'textEnable')"
                  :items="textItemsValue(OSD_GROUP_KEY, 'items')"
                  :canvas="pictureCanvasSize"
                  :disabled="familyFieldsDisabled"
                  :max-items="MAX_OSD_TEXT_ITEMS"
                  :editing="osdEditing"
                  :canvas-linked="osdCanvasLinked"
                  @update:time-enable="setSv(OSD_GROUP_KEY, 'timeEnable', $event)"
                  @update:time-type="setSv(OSD_GROUP_KEY, 'timeType', $event)"
                  @update:time-x="setOsdTimePosition('x', Number($event))"
                  @update:time-y="setOsdTimePosition('y', Number($event))"
                  @update:text-enable="setSv(OSD_GROUP_KEY, 'textEnable', $event)"
                  @update:items="setTextItems(OSD_GROUP_KEY, 'items', $event)"
                  @locate="focusOsdAnchor"
                  @toggle-edit="emit('toggleOsdEdit')"
                />
              </div>

              <template v-else>
                <div v-for="field in primaryFields" :key="field.key" class="dcg-row" :data-testid="`dcg-field-${field.key}`">
                  <span class="dcg-row-label">{{ field.label }}</span>
                  <!-- ⛔ 没有设备事实时不给控件：空白模板上的数字会被当成设备值，改一下还能下发 -->
                  <div v-if="activeGroupFactsMissing" class="dcg-row-control">
                    <span class="dcg-row-unknown" :data-testid="`dcg-field-unknown-${field.key}`">—</span>
                  </div>
                  <div v-else class="dcg-row-control">
                    <a-select
                      v-if="field.kind === 'select'"
                      class="dcg-select"
                      :model-value="textValue(activeGroup.key, field.key)"
                      size="small"
                      :disabled="familyFieldsDisabled"
                      :aria-label="field.label"
                      @change="setSv(activeGroup.key, field.key, selectValue($event))"
                    >
                      <a-option v-for="option in field.options" :key="option.value" :value="option.value">
                        {{ option.label }}
                      </a-option>
                    </a-select>

                    <div v-else-if="field.kind === 'mirror'" class="dcg-mirror" :data-testid="`dcg-mirror-${field.key}`">
                      <!-- ⛔ 图标不能替代文字：对账比的是值，不是画面对不对 -->
                      <button
                        v-for="option in field.options"
                        :key="option.value"
                        type="button"
                        class="dcg-mirror-btn"
                        :class="{ 'is-active': textValue(activeGroup.key, field.key) === option.value }"
                        :disabled="familyFieldsDisabled"
                        :aria-pressed="textValue(activeGroup.key, field.key) === option.value"
                        :title="`${option.label}（值 ${option.value}）`"
                        :data-testid="`dcg-mirror-${field.key}-${option.value}`"
                        @click="setSv(activeGroup.key, field.key, option.value)"
                      >
                        <component :is="mirrorIcon(option.value)" :size="16" />
                        <span>{{ option.shortLabel ?? option.label }}</span>
                      </button>
                    </div>

                    <DeviceConfigSlider
                      v-else-if="field.kind === 'slider'"
                      :model-value="numberValue(activeGroup.key, field.key, field.min)"
                      :min="field.min"
                      :max="field.max"
                      :step="field.step"
                      :unit="field.unit"
                      :label="field.label"
                      :disabled="familyFieldsDisabled"
                      @update:model-value="setSv(activeGroup.key, field.key, $event)"
                    />

                    <button
                      v-else-if="field.kind === 'switch'"
                      type="button"
                      class="dcg-switch"
                      :class="{ 'is-on': boolValue(activeGroup.key, field.key) }"
                      :disabled="familyFieldsDisabled"
                      :aria-label="field.label"
                      @click="setSv(activeGroup.key, field.key, !boolValue(activeGroup.key, field.key))"
                    >
                      <i />
                    </button>

                    <div v-else-if="field.kind === 'coords'" class="dcg-coords">
                      <label v-for="(axis, index) in field.axes" :key="axis">
                        <span>{{ axis }}</span>
                        <input
                          class="dcg-input is-coord"
                          type="text"
                          inputmode="numeric"
                          :disabled="familyFieldsDisabled"
                          :aria-label="`${field.label} ${axis}`"
                          :value="coordsValue(activeGroup.key, field.key)[index]"
                          @change="setCoordValue(activeGroup.key, field.key, index, ($event.target as HTMLInputElement).value)"
                        />
                      </label>
                    </div>

                    <DeviceConfigTextItems
                      v-else-if="field.kind === 'texts'"
                      :model-value="textItemsValue(activeGroup.key, field.key)"
                      :max-items="field.maxItems"
                      :max-length="MAX_OSD_TEXT_LENGTH"
                      :disabled="familyFieldsDisabled"
                      @update:model-value="setTextItems(activeGroup.key, field.key, $event)"
                    />

                    <DeviceConfigWeekPlan
                      v-else-if="field.kind === 'schedules'"
                      :model-value="schedulesValue(activeGroup.key, field.key)"
                      :disabled="familyFieldsDisabled"
                      @update:model-value="setSchedules(activeGroup.key, field.key, $event)"
                    />

                    <input
                      v-else
                      class="dcg-input"
                      type="text"
                      :disabled="familyFieldsDisabled"
                      :value="textValue(activeGroup.key, field.key)"
                      :placeholder="field.placeholder"
                      :maxlength="field.maxlength"
                      :aria-label="field.label"
                      @change="setSv(activeGroup.key, field.key, ($event.target as HTMLInputElement).value)"
                    />
                  </div>
                  <span v-if="fieldHint(field, activeGroup.key) && !embedded" class="dcg-row-hint">{{
                    fieldHint(field, activeGroup.key)
                  }}</span>
                </div>
              </template>
            </template>
          </div>

          <Teleport v-if="embedded && visible && detailTarget && (detailFields.length || activeIsReady)" :to="detailTarget" defer>
            <section class="dcg-detail-fields" data-testid="dcg-detail-fields">
              <header class="dcg-detail-fields-head">
                <span class="dcg-detail-fields-title">{{ activeGroup.label }}</span>
                <div v-if="activeIsReady" class="dcg-embedded-actions" data-testid="dcg-embedded-actions">
                  <button
                    type="button"
                    class="dcg-btn"
                    data-testid="dcg-reset"
                    title="还原为最近回读值"
                    :disabled="activeApplying || !activeDirtyCount"
                    @click="resetActiveGroup"
                  >
                    <RotateCcw :size="12" />还原
                  </button>
                  <button
                    type="button"
                    class="dcg-btn"
                    data-testid="dcg-read"
                    :disabled="activeLoading || !canRead || !online"
                    @click="readActiveGroup"
                  >
                    <RefreshCcw :size="12" />读取
                  </button>
                  <button
                    type="button"
                    class="dcg-btn is-primary"
                    data-testid="dcg-apply"
                    :disabled="activeApplying || !activeDirtyCount || !canApply"
                    @click="applyActiveGroup"
                  >
                    <Send :size="12" />下发
                  </button>
                </div>
              </header>
              <div v-if="detailFields.length" class="dcg-detail-fields-body">
                <div
                  v-for="field in detailFields"
                  :key="field.key"
                  class="dcg-detail-row"
                  :data-testid="`dcg-detail-field-${field.key}`"
                >
                  <span class="dcg-detail-row-label">{{ field.label }}</span>
                  <div v-if="activeGroupFactsMissing" class="dcg-detail-row-control">
                    <span class="dcg-row-unknown" :data-testid="`dcg-detail-unknown-${field.key}`">—</span>
                  </div>
                  <div v-else class="dcg-detail-row-control">
                    <div v-if="field.kind === 'coords'" class="dcg-coords">
                      <label v-for="(axis, index) in field.axes" :key="axis">
                        <span>{{ axis }}</span>
                        <input
                          class="dcg-input is-coord"
                          type="text"
                          inputmode="numeric"
                          :disabled="familyFieldsDisabled"
                          :aria-label="`${field.label} ${axis}`"
                          :value="coordsValue(activeGroup.key, field.key)[index]"
                          @change="setCoordValue(activeGroup.key, field.key, index, ($event.target as HTMLInputElement).value)"
                        />
                      </label>
                    </div>
                    <DeviceConfigTextItems
                      v-else-if="field.kind === 'texts'"
                      :model-value="textItemsValue(activeGroup.key, field.key)"
                      :max-items="field.maxItems"
                      :max-length="MAX_OSD_TEXT_LENGTH"
                      :disabled="familyFieldsDisabled"
                      @update:model-value="setTextItems(activeGroup.key, field.key, $event)"
                    />
                    <DeviceConfigWeekPlan
                      v-else-if="field.kind === 'schedules'"
                      :model-value="schedulesValue(activeGroup.key, field.key)"
                      :disabled="familyFieldsDisabled"
                      @update:model-value="setSchedules(activeGroup.key, field.key, $event)"
                    />
                  </div>
                  <span v-if="fieldHint(field, activeGroup.key) && !embedded" class="dcg-detail-row-hint">{{
                    fieldHint(field, activeGroup.key)
                  }}</span>
                </div>
              </div>
            </section>
          </Teleport>

          <p class="dcg-params-foot" data-testid="dcg-params-foot">
            <template v-if="activeIsVideo">
              <span>本组 5 项 × {{ visibleVideoRows.length }} 路码流</span>
              <span class="dcg-foot-sep">·</span>
              <span>设备上报 {{ deviceCellCount }} 项</span>
              <span class="dcg-foot-sep">·</span>
              <span :class="{ 'is-dirty': videoDirtyCount > 0 }">待下发 {{ videoDirtyCount }} 项</span>
            </template>
            <template v-else-if="activeIsReady">
              <span>本组 {{ activeFields.length }} 项</span>
              <span class="dcg-foot-sep">·</span>
              <template v-if="!embedded">
                <span>{{ activeGroupConfigTypeText }}</span>
                <span class="dcg-foot-sep">·</span>
              </template>
              <button
                v-if="activeDirtyCount > 0"
                type="button"
                class="dcg-foot-dirty is-dirty"
                data-testid="dcg-pending-toggle"
                :aria-expanded="pendingExpanded"
                @click="pendingExpanded = !pendingExpanded"
              >
                {{ activeDirtyCount }} 项未下发
                <span class="dcg-foot-caret">{{ pendingExpanded ? "▴" : "▾" }}</span>
              </button>
              <span v-else>无未下发的改动</span>
            </template>
            <template v-else>
              <span>本组 {{ activeFields.length }} 项</span>
              <span class="dcg-foot-sep">·</span>
              <span>{{ embedded ? "当前不可用" : "静态形态，接入后即可读写" }}</span>
            </template>
          </p>

          <!-- 差异抽屉：把「N 项未下发」从一个数字变成可核对的清单 -->
          <div
            v-if="activeIsReady && pendingExpanded && activePendingChanges.length"
            class="dcg-pending"
            data-testid="dcg-pending"
          >
            <header class="dcg-pending-head">
              <span>未下发的改动</span>
              <em>撤销单项 ≠ 把设备恢复出厂，只是退回最近一次回读值</em>
            </header>
            <div class="dcg-pending-cols">
              <span>配置项</span>
              <span>设备当前值</span>
              <span>将下发</span>
              <span />
            </div>
            <div
              v-for="change in activePendingChanges"
              :key="change.key"
              class="dcg-pending-row"
              :data-testid="`dcg-pending-row-${change.key}`"
            >
              <span class="dcg-pending-label">{{ change.label }}</span>
              <span class="dcg-pending-from">{{ change.from }}</span>
              <span class="dcg-pending-to">{{ change.to }}</span>
              <button type="button" class="dcg-pending-revert" :data-testid="`dcg-revert-${change.key}`" @click="change.revert()">
                撤销
              </button>
            </div>
          </div>
        </section>
      </div>

      <footer v-if="!embedded" class="dcg-statusbar">
        <span class="dcg-status-item">
          <i class="dcg-status-dot" :class="`is-${activeGroup.state}`" />
          {{ activeGroup.state === "ready" ? "本组已接入后端，可读可写" : "本组为静态形态，尚未接入后端" }}
        </span>
        <span v-if="activeIsReady" class="dcg-status-item">
          已改 {{ activeDirtyCount }} 项 · 回读 {{ activeObservedAt || "—" }}
        </span>
        <span v-if="activeIsVideo && videoStreamNumberList" class="dcg-status-item">
          码流声明 {{ parseStreamNumberList(videoStreamNumberList).join(" / ") || "—" }}
        </span>
        <span class="dcg-status-item is-right" :title="activeFreshness ? `后端 freshness=${activeFreshness}` : ''">
          {{ freshnessText || "—" }}
        </span>
      </footer>
    </section>
  </div>
</template>

<style scoped lang="scss">
.dcg-host {
  position: fixed;
  inset: 0;
  z-index: 1200;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgb(15 23 42 / 42%);
}

.dcg-host--embedded {
  position: static;
  inset: auto;
  display: block;
  background: transparent;
}

.dcg-window {
  display: flex;
  flex-direction: column;
  width: min(1100px, 94vw);
  height: min(580px, 88vh);
  overflow: hidden;
  background: var(--uvp-dialog-bg, #ffffff);
  border: 1px solid var(--uvp-dialog-border, #e6edf7);
  border-radius: 8px;
  box-shadow: var(--uvp-dialog-shadow, 0 24px 72px -32px rgb(15 23 42 / 34%));
}

.dcg-window--embedded {
  width: 100%;
  height: 100%;
  min-height: 0;
  background: transparent;
  border: 0;
  border-radius: 0;
  box-shadow: none;
}

/* ── 标题栏 ── */
.dcg-titlebar {
  display: flex;
  flex: none;
  gap: 8px;
  align-items: center;
  height: 36px;
  padding: 0 8px 0 12px;
  background: var(--uvp-dialog-header-bg, #fbfdff);
  border-bottom: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-titlebar-mark {
  width: 3px;
  height: 14px;
  background: var(--uvp-brand, #2563eb);
  border-radius: 2px;
}

.dcg-titlebar-text {
  font-size: 13px;
  font-weight: 500;
  color: var(--uvp-text-primary);
}

.dcg-titlebar-sep,
.dcg-titlebar-code,
.dcg-titlebar-ver {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}

.dcg-titlebar-target {
  font-size: 12px;
  color: var(--uvp-text-secondary);
}

.dcg-titlebar-code {
  padding: 1px 6px;
  font-family: var(--uvp-font-mono, ui-monospace, monospace);
  background: var(--uvp-dialog-control-bg, #f8fbff);
  border: 1px solid var(--uvp-dialog-border, #e6edf7);
  border-radius: 3px;
}

.dcg-titlebar-ver {
  padding: 1px 6px;
  margin-left: auto;
  color: var(--uvp-brand, #2563eb);
  background: var(--uvp-brand-soft, #e8f2ff);
  border-radius: 3px;
}

.dcg-close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  padding: 0;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: transparent;
  border: none;
  border-radius: 4px;

  &:hover {
    color: var(--uvp-danger, #d14343);
    background: var(--uvp-danger-soft, #fff2f2);
  }
}

/* ── 三栏主体 ── */
.dcg-body {
  display: grid;
  flex: 1 1 auto;
  grid-template-columns: minmax(340px, 380px) 250px minmax(0, 1fr);
  min-height: 0;
}

.dcg-window--embedded .dcg-body {
  grid-template-rows: auto minmax(0, 1fr);
  grid-template-columns: minmax(0, 1fr);
}

/* ⛔ 嵌入形态的分组导航**横排 + 按组数等分**，不要写死列数。
  原来是 `grid-template-columns: repeat(4, minmax(0, 1fr))`：只有 2 个组时
  第 3、4 列永远空着，看上去像"导航坏了半排"。宿主传几组是它自己的事
  （播放控制台侧栏 1 组、设备详情抽屉 2~3 组），所以用 flex 让子项自己分。
  ⛔⛔ `flex-direction: row` **必须显式写**：基类 `.dcg-nav` 是 `flex-direction: column`
    （全窗口形态的竖排导航），`display: flex` 会把那个方向一起继承过来 ——
    `display: grid` 时代它被隐式盖掉，换成 flex 后不写就变成**竖排导航**
    （实测：2 个组各 592px 宽、上下摞着，整条导航高 47px）。这是"改一个属性、坏另一个"
    的典型，只有真在浏览器里量过才看得见。
  ⛔ 子项保留 `min-width: 0`（见 .dcg-nav-item）：组多了靠收缩 + 省略号收场，
    不许把导航撑出横向滚动。 */
.dcg-window--embedded .dcg-nav {
  display: flex;
  flex-direction: row;
  gap: 2px;
  min-width: 0;
  padding: 6px 8px;
  overflow: hidden;
  background: var(--uvp-dialog-control-bg, #f8fbff);
  border-right: 0;
  border-bottom: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-window--embedded .dcg-nav > .dcg-nav-item {
  flex: 1 1 0;
}

.dcg-window--embedded .dcg-params-head {
  flex-wrap: wrap;
  height: auto;
  min-height: 42px;
  padding: 7px 10px;
  border-top: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-window--embedded .dcg-params-body {
  padding: 10px;
  background: var(--uvp-dialog-bg, #ffffff);
}

.dcg-window--embedded .dcg-params {
  min-width: 0;
  min-height: 0;
}

.dcg-window--embedded .dcg-nav-item {
  width: auto;
  min-width: 0;
  height: 30px;
  padding: 0 9px;
  border: 1px solid transparent;
  border-radius: 4px;
}

.dcg-window--embedded .dcg-nav-item.is-active {
  color: var(--uvp-brand, #2563eb);
  background: var(--uvp-brand-soft, #e8f2ff);
  border-color: color-mix(in srgb, var(--uvp-brand, #2563eb) 30%, var(--uvp-dialog-border, #e6edf7));
}

.dcg-window--embedded .dcg-nav-caret,
.dcg-window--embedded .dcg-nav-sub,
.dcg-window--embedded .dcg-nav-flag {
  display: none;
}

.dcg-window--embedded .dcg-nav-label {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
}

.dcg-window--embedded .dcg-params-profile {
  order: 5;
  width: 100%;
  margin-left: 0;
}

.dcg-window--embedded .dcg-params-profile .dcg-select {
  flex: 1 1 auto;
  width: auto;
  max-width: none;
}

.dcg-window--embedded .dcg-params-summary {
  padding: 6px 10px;
}

.dcg-window--embedded .dcg-field-group-head > span,
.dcg-window--embedded .dcg-field-group-note {
  display: none;
}

.dcg-window--embedded .dcg-statusbar {
  flex-wrap: wrap;
  gap: 8px;
  height: auto;
  min-height: 28px;
  padding: 5px 10px;
}

.dcg-window--embedded .dcg-status-item:nth-child(n + 3) {
  display: none;
}

.dcg-window--embedded .dcg-row {
  grid-template-columns: minmax(88px, 96px) minmax(0, 1fr) auto;
  gap: 8px;
}

/* 成对字段行（编码格式 + 分辨率同一行，2026-09-20）。
 * ⛔ 选择器必须带 `.is-pair` 写成两条（含嵌入态那条）—— 上面 `.dcg-window--embedded .dcg-row`
 *    是两列栅格，光写 `.dcg-row.is-pair` 在嵌入态会被它按顺序盖掉，表现为"还是两行"。
 * 每格 3 列 = 标签 / 控件 / 来源徽标：约 168px 里留给下拉框约 90px，
 * 「1920×1080」在 10–11px 字号下放得下。 */
.dcg-row.is-pair,
.dcg-window--embedded .dcg-row.is-pair {
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.dcg-pair-cell {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  gap: 5px;
  align-items: center;
  min-width: 0;
}
.dcg-window--embedded .dcg-row.is-pair .dcg-row-label {
  text-align: left;
}
.dcg-window--embedded .dcg-row.is-pair .dcg-row-hint {
  max-width: 26px;
}

/* 自定义分辨率折到第二行：不在 90px 的格子里跟下拉框抢位置（选了「自定义…」才会出现）。 */
.dcg-pair-cell .dcg-row-control {
  flex-wrap: wrap;
}
.dcg-pair-cell .dcg-row-control > .dcg-input {
  flex: 1 1 100%;
  min-width: 0;
}

.dcg-window--embedded .dcg-row-label {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 11px;
  text-align: right;
  white-space: nowrap;
}

.dcg-window--embedded .dcg-row-hint {
  max-width: 56px;
  padding: 1px 3px;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 9px;
}

.dcg-window--embedded :deep(.cfg-slider) {
  gap: 3px;
  max-width: none;
}

.dcg-window--embedded :deep(.cfg-slider-step) {
  width: 18px;
}

.dcg-window--embedded :deep(.cfg-slider-track) {
  min-width: 20px;
}

.dcg-window--embedded :deep(.cfg-slider-input) {
  width: 42px;
}

.dcg-preview {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px;
  background: var(--uvp-dialog-control-bg, #f8fbff);
  border-right: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-screen {
  position: relative;
  flex: 1 1 auto;
  min-height: 208px;
  overflow: hidden;
  background: #10161f;
  border: 1px solid #0b1118;
  border-radius: 4px;
}

.dcg-screen-empty {
  display: flex;
  flex-direction: column;
  gap: 4px;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: #64748b;

  p {
    margin: 0;
    font-size: 12px;
    color: #94a3b8;
  }

  em {
    font-size: 10px;
    font-style: normal;
    color: #556274;
  }
}

.dcg-screen-badge {
  position: absolute;
  top: 6px;
  right: 6px;
  display: inline-flex;
  gap: 3px;
  align-items: center;
  padding: 1px 6px;
  font-size: 10px;
  border-radius: 3px;

  &.is-on {
    color: #d1fae5;
    background: rgb(16 185 129 / 22%);
  }

  &.is-off {
    color: #fecaca;
    background: rgb(239 68 68 / 22%);
  }
}

.dcg-screen-res {
  position: absolute;
  bottom: 6px;
  left: 6px;
  padding: 1px 5px;
  font-family: var(--uvp-font-mono, ui-monospace, monospace);
  font-size: 10px;
  color: #94a3b8;
  background: rgb(0 0 0 / 42%);
  border-radius: 3px;
}

.dcg-actionbar {
  display: grid;
  flex: none;
  grid-template-columns: repeat(3, 1fr);
  gap: 6px;
}

.dcg-btn {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  justify-content: center;
  height: 26px;
  padding: 0 8px;
  font-size: 12px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: var(--uvp-secondary-action-bg, #ffffff);
  border: 1px solid var(--uvp-secondary-action-border, #dbe4f0);
  border-radius: 4px;

  &:hover:not(:disabled) {
    color: var(--uvp-brand);
    border-color: var(--uvp-brand);
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.5;
  }

  &.is-primary:not(:disabled) {
    color: #ffffff;
    background: var(--uvp-brand, #2563eb);
    border-color: var(--uvp-brand-strong, #1d4ed8);
  }

  &.is-primary:hover:not(:disabled) {
    color: #ffffff;
    background: var(--uvp-brand-strong, #1d4ed8);
  }
}

.dcg-ptz {
  display: flex;
  flex: none;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  padding-top: 4px;
}

.dcg-ptz-pad {
  position: relative;
  width: 84px;
  height: 84px;
  background: linear-gradient(180deg, #ffffff 0%, #eef2f8 100%);
  border: 1px solid var(--uvp-secondary-action-border, #cdd8e6);
  border-radius: 50%;
  box-shadow: inset 0 1px 2px rgb(15 23 42 / 8%);
}

.dcg-ptz-arrow {
  position: absolute;
  width: 0;
  height: 0;
  border: 5px solid transparent;
  opacity: 0.62;

  &.is-up,
  &.is-down {
    left: 50%;
    margin-left: -5px;
  }

  &.is-up {
    top: 9px;
    border-bottom-color: #64748b;
  }

  &.is-down {
    bottom: 9px;
    border-top-color: #64748b;
  }

  &.is-left,
  &.is-right {
    top: 50%;
    margin-top: -5px;
  }

  &.is-left {
    left: 9px;
    border-right-color: #64748b;
  }

  &.is-right {
    right: 9px;
    border-left-color: #64748b;
  }
}

.dcg-ptz-center {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 20px;
  height: 20px;
  margin: -10px 0 0 -10px;
  background: linear-gradient(180deg, #ffffff 0%, #e2e8f0 100%);
  border: 1px solid #c3cede;
  border-radius: 50%;
}

.dcg-ptz-zoom {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.dcg-zoom-row {
  display: flex;
  gap: 8px;
  align-items: center;

  span {
    min-width: 28px;
    font-size: 12px;
    color: var(--uvp-text-secondary);
    text-align: center;
  }
}

.dcg-step-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  padding: 0;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: var(--uvp-secondary-action-bg, #ffffff);
  border: 1px solid var(--uvp-secondary-action-border, #cdd8e6);
  border-radius: 50%;

  &:hover {
    color: var(--uvp-brand);
    border-color: var(--uvp-brand);
  }
}

.dcg-steprow {
  display: flex;
  flex: none;
  gap: 8px;
  align-items: center;
  padding-top: 2px;

  > span {
    font-size: 12px;
    color: var(--uvp-text-secondary);
  }
}

/* ── 中栏：分组导航 ── */
.dcg-nav {
  display: flex;
  flex-direction: column;
  padding: 8px 0;
  overflow-y: auto;
  background: var(--uvp-dialog-control-bg, #f8fbff);
  border-right: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-nav-item {
  display: flex;
  gap: 6px;
  align-items: center;
  height: 40px;
  padding: 0 10px;
  font-size: 12px;
  color: var(--uvp-text-secondary);
  text-align: left;
  cursor: pointer;
  background: transparent;
  border: none;

  &:hover {
    background: var(--uvp-table-row-hover-bg, #f8fbff);
  }

  &.is-active {
    color: #ffffff;
    background: var(--uvp-brand, #2563eb);

    .dcg-nav-caret {
      transform: rotate(90deg);
    }
  }
}

.dcg-nav-caret {
  flex: none;
  transition: transform 0.15s;
}

.dcg-nav-label {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dcg-nav-text {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  gap: 1px;
  min-width: 0;
}

/* 条款号：借用标准编号当副标题，中栏不至于只有一列光秃秃的分组名 */
.dcg-nav-sub {
  font-family: var(--uvp-font-mono, ui-monospace, monospace);
  font-size: 9.5px;
  color: var(--uvp-text-tertiary);
  opacity: 0.85;
}

.dcg-nav-item.is-active .dcg-nav-sub {
  color: rgb(255 255 255 / 70%);
}

.dcg-nav-flag {
  flex: none;
  width: 6px;
  height: 6px;
  border-radius: 50%;

  &.is-ready {
    background: #10b981;
  }

  &.is-static {
    background: #cbd5e1;
  }
}

.dcg-nav-item.is-active .dcg-nav-flag.is-static {
  background: rgb(255 255 255 / 55%);
}

/* ── 右栏：参数区 ── */
.dcg-params {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.dcg-params-head {
  display: flex;
  flex: none;
  gap: 8px;
  align-items: center;
  height: 38px;
  padding: 0 12px;
  border-bottom: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-params-title {
  font-size: 13px;
  font-weight: 500;
  color: var(--uvp-text-primary);
}

.dcg-params-title-wrap {
  display: inline-flex;
  gap: 7px;
  align-items: center;
  min-width: 0;
}

.dcg-standard-badge {
  padding: 1px 5px;
  font-size: 9px;
  font-weight: 500;
  color: var(--uvp-brand, #2563eb);
  white-space: nowrap;
  background: var(--uvp-brand-soft, #e8f2ff);
  border: 1px solid color-mix(in srgb, var(--uvp-brand, #2563eb) 24%, transparent);
  border-radius: 3px;
}

.dcg-params-std {
  padding: 1px 5px;
  font-family: var(--uvp-font-mono, ui-monospace, monospace);
  font-size: 10px;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-dialog-control-bg, #f8fbff);
  border-radius: 3px;
}

.dcg-params-state {
  padding: 1px 6px;
  font-size: 10px;
  border-radius: 3px;

  &.is-ready {
    color: #047857;
    background: #d1fae5;
  }

  &.is-static {
    color: var(--uvp-text-tertiary);
    background: #eef1f6;
  }
}

.dcg-params-profile {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  margin-left: auto;

  > span {
    font-size: 11px;
    color: var(--uvp-text-secondary);
  }
}

.dcg-embedded-actions {
  display: inline-flex;
  gap: 5px;
  margin-left: auto;
}

.dcg-embedded-actions .dcg-btn {
  height: 24px;
  padding: 0 7px;
  font-size: 10.5px;
}

.dcg-detail-fields {
  width: 100%;
  min-width: 0;
  background: var(--uvp-dialog-bg, #ffffff);
  border-top: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-detail-fields-head {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
  min-height: 34px;
  padding: 5px 10px;
  background: var(--uvp-dialog-control-bg, #f8fbff);
  border-bottom: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-detail-fields-title {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 12px;
  font-weight: 500;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}

.dcg-detail-fields-body {
  display: grid;
  gap: 8px;
  padding: 8px 10px 10px;
}

.dcg-detail-row {
  display: grid;
  grid-template-columns: minmax(72px, 96px) minmax(0, 1fr);
  gap: 8px;
  align-items: start;
  min-width: 0;
}

.dcg-detail-row-label {
  padding-top: 4px;
  font-size: 11px;
  color: var(--uvp-text-secondary);
  text-align: right;
  white-space: nowrap;
}

.dcg-detail-row-control {
  min-width: 0;
}

.dcg-detail-row-hint {
  grid-column: 2;
  margin-top: -3px;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}

.dcg-detail-fields .dcg-embedded-actions {
  flex: none;
  margin-left: auto;
}

.dcg-params-summary {
  display: flex;
  flex: none;
  gap: 5px;
  align-items: flex-start;
  padding: 7px 12px;
  margin: 0;
  font-size: 11px;
  line-height: 1.5;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-dialog-control-bg, #f8fbff);
  border-bottom: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-params-body {
  flex: 1 1 auto;
  min-height: 0;
  padding: 12px 14px 16px;
  overflow-y: auto;
}

/* 静态组的控件没有真实读写，视觉上必须比可用的组弱一档 */
.dcg-params-body.is-static .dcg-row-control {
  opacity: 0.7;
}

/* 参数区底部汇总：内容少时也把底边压实，不留空档 */
.dcg-params-foot {
  display: flex;
  flex: none;
  gap: 6px;
  align-items: center;
  padding: 8px 14px;
  margin: 0;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  border-top: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-foot-sep {
  color: var(--uvp-panel-border, #dbe4f0);
}

.dcg-params-foot .is-dirty {
  color: var(--uvp-warning, #b66b12);
}

/* 「N 项未下发」是抽屉开关，不是纯文本 —— 去掉按钮默认外观但要保住命中区。 */
.dcg-foot-dirty {
  display: inline-flex;
  gap: 3px;
  align-items: center;
  padding: 1px 5px;
  font: inherit;
  color: var(--uvp-warning, #b66b12);
  cursor: pointer;
  background: none;
  border: 1px solid transparent;
  border-radius: 4px;

  &:hover {
    border-color: var(--uvp-warning, #b66b12);
  }
}

.dcg-foot-caret {
  font-size: 9px;
  line-height: 1;
}

/*
 * 差异抽屉。
 *
 * ⛔ 三列固定宽度（配置项 / 设备当前值 / 将下发）：值长短差异很大，
 *    用 left 布局会让「将下发」整列歪斜，用户没法竖着比对。
 */
.dcg-pending {
  flex: none;
  margin: 0 14px 8px;
  overflow: hidden;
  border: 1px solid var(--uvp-dialog-border, #e6edf7);
  border-radius: 6px;
}

.dcg-pending-head {
  display: flex;
  gap: 8px;
  align-items: baseline;
  padding: 6px 10px;
  font-size: 11px;
  background: var(--uvp-dialog-control-bg, #f8fbff);
  border-bottom: 1px solid var(--uvp-dialog-border, #e6edf7);

  em {
    font-size: 10px;
    font-style: normal;
    color: var(--uvp-text-tertiary);
  }
}

.dcg-pending-cols,
.dcg-pending-row {
  display: grid;
  grid-template-columns: minmax(72px, 1fr) minmax(0, 1.2fr) minmax(0, 1.2fr) 44px;
  gap: 8px;
  align-items: center;
  padding: 5px 10px;
  font-size: 11px;
}

.dcg-pending-cols {
  font-size: 10px;
  color: var(--uvp-text-tertiary);
  border-bottom: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-pending-row + .dcg-pending-row {
  border-top: 1px solid var(--uvp-dialog-border, #eef3fa);
}

.dcg-pending-label {
  color: var(--uvp-text-secondary);
}

.dcg-pending-from {
  color: var(--uvp-text-tertiary);
  overflow-wrap: anywhere;
}

.dcg-pending-to {
  color: var(--uvp-warning, #b66b12);
  overflow-wrap: anywhere;
}

.dcg-pending-revert {
  padding: 2px 6px;
  font-size: 10px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: none;
  border: 1px solid var(--uvp-panel-border, #dbe4f0);
  border-radius: 4px;

  &:hover {
    color: var(--uvp-brand);
    border-color: var(--uvp-brand);
  }
}

.dcg-reconcile {
  display: flex;
  gap: 6px;
  align-items: center;
  padding: 6px 8px;
  margin-bottom: 8px;
  font-size: 11.5px;
  color: var(--uvp-text-secondary);
  background: var(--uvp-dialog-control-bg, #f8fbff);
  border-radius: 4px;

  .dcg-reconcile-dot {
    flex: none;
    width: 6px;
    height: 6px;
    background: #94a3b8;
    border-radius: 50%;
  }

  &.is-ok {
    color: #047857;
    background: #ecfdf5;

    .dcg-reconcile-dot {
      background: #10b981;
    }
  }

  &.is-pending {
    color: var(--uvp-brand, #2563eb);
    background: var(--uvp-brand-soft, #e8f2ff);

    .dcg-reconcile-dot {
      background: var(--uvp-brand, #2563eb);
    }
  }

  &.is-warn {
    color: var(--uvp-warning, #b66b12);
    background: var(--uvp-warning-soft, #fff7ed);

    .dcg-reconcile-dot {
      background: var(--uvp-warning, #b66b12);
    }
  }

  &.is-error {
    color: var(--uvp-danger, #d14343);
    background: var(--uvp-danger-soft, #fff2f2);

    .dcg-reconcile-dot {
      background: var(--uvp-danger, #d14343);
    }
  }
}

.dcg-notice,
.dcg-error {
  padding: 5px 8px;
  margin: 0 0 8px;
  font-size: 11px;
  border-radius: 4px;
}

.dcg-notice {
  color: var(--uvp-warning, #b66b12);
  background: var(--uvp-warning-soft, #fff7ed);
}

.dcg-error {
  color: var(--uvp-danger, #d14343);
  background: var(--uvp-danger-soft, #fff2f2);
}

.dcg-empty {
  padding: 26px 0;
  text-align: center;

  p {
    margin: 0 0 4px;
    font-size: 12px;
    color: var(--uvp-text-secondary);
  }

  em {
    font-size: 11px;
    font-style: normal;
    color: var(--uvp-text-tertiary);
  }
}

.dcg-stream {
  padding: 0;
  margin-bottom: 12px;
  overflow: hidden;
  background: var(--uvp-panel-bg, #ffffff);
  border: 1px solid var(--uvp-dialog-border, #e6edf7);
  border-radius: 6px;
}

.dcg-stream-hd {
  display: flex;
  gap: 6px;
  align-items: center;
  min-height: 34px;
  padding: 0 10px;
  background: var(--uvp-dialog-control-bg, #f8fbff);
  border-bottom: 1px solid var(--uvp-dialog-border, #e6edf7);

  span {
    font-size: 12px;
    font-weight: 500;
    color: var(--uvp-text-primary);
  }

  em {
    font-size: 11px;
    font-style: normal;
    color: var(--uvp-text-tertiary);

    &::before {
      content: "· ";
    }
  }
}

.dcg-field-group {
  padding: 7px 10px 8px;

  & + & {
    border-top: 1px solid var(--uvp-dialog-border, #e6edf7);
  }
}

.dcg-field-group-head {
  display: flex;
  gap: 8px;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 4px;

  strong {
    font-size: 11px;
    font-weight: 600;
    color: var(--uvp-text-primary);
  }

  span {
    overflow: hidden;
    text-overflow: ellipsis;
    font-family: var(--uvp-font-mono, ui-monospace, monospace);
    font-size: 8.5px;
    color: var(--uvp-text-tertiary);
    white-space: nowrap;
  }
}

.dcg-field-group-note {
  margin: 4px 0 0 86px;
  font-size: 9px;
  line-height: 1.45;
  color: var(--uvp-text-tertiary);
}

.dcg-stream-dirty {
  padding: 1px 5px;
  margin-left: auto;
  font-size: 10px;
  font-style: normal;
  color: var(--uvp-warning, #b66b12);
  background: var(--uvp-warning-soft, #fff7ed);
  border-radius: 3px;
}

.dcg-row {
  display: grid;
  grid-template-columns: 76px minmax(0, 1fr) auto;
  gap: 10px;
  align-items: center;
  min-height: 30px;
}

.dcg-row-label {
  font-size: 12px;
  color: var(--uvp-text-secondary);
  text-align: right;
}

.dcg-row-control {
  display: flex;
  gap: 6px;
  align-items: center;
  min-width: 0;
}

/**
 * 缺设备事实时的值占位。
 *
 * ⛔ 用「—」而不是把控件留着显示 0/false：0 是**合法设备值**，
 *    和「没读到」在屏幕上长得一样就成了视觉谎言。
 */
.dcg-row-unknown {
  font-size: 12px;
  color: var(--uvp-text-tertiary);
  letter-spacing: 1px;
}

/* 镜像的方向按钮：一行的四个方向，点哪个一眼可见 —— 下拉框做不到这件事。 */
.dcg-mirror {
  display: flex;
  gap: 4px;
}

.dcg-mirror-btn {
  display: inline-flex;
  flex-direction: column;
  gap: 2px;
  align-items: center;
  min-width: 48px;
  padding: 4px 6px;
  font-size: 10px;
  line-height: 1.2;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: var(--uvp-dialog-control-bg, #ffffff);
  border: 1px solid var(--uvp-panel-border, #dbe4f0);
  border-radius: 6px;

  &:hover:not(:disabled) {
    color: var(--uvp-brand);
    border-color: var(--uvp-brand);
  }

  &.is-active {
    color: var(--uvp-brand);
    background: #eef4ff;
    border-color: var(--uvp-brand);
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.55;
  }
}

.dcg-row-hint {
  padding: 1px 5px;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
  border-radius: 3px;
}

.dcg-row-hint[data-source="设备"] {
  color: #047857;
  background: #ecfdf5;
}

.dcg-row-hint[data-source="缺省"] {
  color: #64748b;
  background: #f1f5f9;
}

.dcg-row-hint[data-source="不发"] {
  color: var(--uvp-warning, #b66b12);
  background: var(--uvp-warning-soft, #fff7ed);
}

.dcg-select,
.dcg-input {
  min-width: 0;
  height: 24px;
  font-size: 12px;
  color: var(--uvp-text-primary);
  background: var(--uvp-dialog-control-bg, #ffffff);
  border: 1px solid var(--uvp-panel-border, #dbe4f0);
  border-radius: 4px;

  &:focus {
    outline: none;
    border-color: var(--uvp-brand);
  }

  &:disabled {
    color: var(--uvp-text-tertiary);
    cursor: not-allowed;
  }
}

.dcg-select {
  flex: 1 1 auto;
  max-width: 240px;
  padding: 0 4px;

  &.is-mini {
    flex: none;
    width: 88px;
  }
}

.dcg-row-control > .dcg-select {
  width: 100%;
}

.dcg-window--embedded .dcg-row-control > .dcg-select {
  max-width: none;
}

/* Arco 的根节点由子组件渲染，嵌入模式用 deep 确保宽度规则能落到真实控件。 */
:deep(.dcg-row-control > .dcg-select) {
  width: 100%;
  max-width: 240px;
}

:deep(.dcg-window--embedded .dcg-row-control > .dcg-select) {
  max-width: none;
}

:deep(.dcg-select.is-mini) {
  flex: none;
  width: 88px;
  max-width: 88px;
}

.dcg-window--embedded .dcg-row-control > .dcg-input {
  max-width: none;
}

.dcg-input {
  flex: 1 1 auto;
  max-width: 240px;
  padding: 0 6px;

  &.is-narrow {
    flex: none;
    width: 106px;
  }
}

.dcg-segment {
  display: inline-flex;
  overflow: hidden;
  border: 1px solid var(--uvp-panel-border, #dbe4f0);
  border-radius: 4px;

  button {
    min-width: 48px;
    height: 22px;
    padding: 0 10px;
    font-size: 11.5px;
    color: var(--uvp-text-secondary);
    cursor: pointer;
    background: var(--uvp-dialog-control-bg, #ffffff);
    border: none;

    & + button {
      border-left: 1px solid var(--uvp-panel-border, #dbe4f0);
    }

    &.is-on {
      color: #ffffff;
      background: var(--uvp-brand, #2563eb);
    }
  }
}

.dcg-switch {
  position: relative;
  width: 34px;
  height: 18px;
  padding: 0;
  cursor: pointer;
  background: #cbd5e1;
  border: none;
  border-radius: 9px;
  transition: background 0.15s;

  i {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 14px;
    height: 14px;
    background: #ffffff;
    border-radius: 50%;
    box-shadow: 0 1px 2px rgb(15 23 42 / 25%);
    transition: transform 0.15s;
  }

  &.is-on {
    background: var(--uvp-brand, #2563eb);

    i {
      transform: translateX(16px);
    }
  }

  &:disabled {
    cursor: not-allowed;
    opacity: 0.6;
  }
}

.dcg-coords {
  display: flex;
  gap: 6px;
  align-items: center;

  label {
    display: inline-flex;
    gap: 3px;
    align-items: center;

    span {
      font-size: 10.5px;
      color: var(--uvp-text-tertiary);
    }
  }
}

.dcg-input.is-coord {
  flex: none;
  width: 52px;
  text-align: right;
}

.dcg-hintline {
  display: flex;
  gap: 5px;
  align-items: flex-start;
  padding: 7px 9px;
  margin: 12px 0 0;
  font-size: 11px;
  line-height: 1.55;
  color: var(--uvp-text-tertiary);
  background: #f6f8fb;
  border: 1px dashed var(--uvp-panel-border, #dbe4f0);
  border-radius: 4px;
}

/* ── 底部状态条 ── */
.dcg-statusbar {
  display: flex;
  flex: none;
  gap: 14px;
  align-items: center;
  height: 26px;
  padding: 0 12px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-dialog-footer-bg, #fbfdff);
  border-top: 1px solid var(--uvp-dialog-border, #e6edf7);
}

.dcg-status-item {
  display: inline-flex;
  gap: 5px;
  align-items: center;

  &.is-right {
    margin-left: auto;
    white-space: nowrap;
  }
}

.dcg-status-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;

  &.is-ready {
    background: #10b981;
  }

  &.is-static {
    background: #cbd5e1;
  }
}
</style>
