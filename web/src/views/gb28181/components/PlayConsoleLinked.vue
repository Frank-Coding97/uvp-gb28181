<script setup lang="ts">
/**
 * PlayConsoleLinked - 播放控制台双区联动业务弹窗
 *
 * 设计定位:平台核心功能弹窗,承担点播 + 云台 + 探针 + 诊断 + 录制的一体化操作台
 *
 * 数据来源:点播、概况、探针、PTZ、能力、高级控制和对讲均走受保护后端接口。
 * 图像参数不属于 GB28181 标准 DeviceControl/ConfigDownload,不在此面板伪造控制入口。
 *
 * 视觉语言:深色为主,青色作强调,毛玻璃卡片,状态用色带 + 脉冲呼吸
 * 布局:右侧保留高频操作,播放器下方随 Tab 联动展示详情
 */
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch, type CSSProperties } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { copyTextToClipboard } from "@/utils/app";
import { formatToken, getAccessToken } from "@/utils/auth";
import type { PlaybackConsoleDisplayMode } from "@/store/modules/playback-console";
import { useUserStoreHook } from "@/store/modules/user";
import PlayWindow from "./PlayWindow.vue";
import DeviceConfigDrawer from "../device-mgmt/DeviceConfigDrawer.vue";
import DeviceConfigOsdBlocks from "../device-mgmt/DeviceConfigOsdBlocks.vue";
import { MAX_MASK_REGIONS, MIRROR_OPTIONS, type ConfigTextItem } from "../device-mgmt/deviceConfigGroups";
import ProbeTimelineDialog from "./ProbeTimelineDialog.vue";
import { buildProbeOverview, probeBucketHeight } from "../probeOverview";
import { resolvePlaybackSource, type PlaybackSource } from "../playbackProtocol";
import {
  assertPCMA8000,
  createAudioLevelMeter,
  preferPCMA8000,
  waitForIceGatheringComplete,
  type AudioLevelMeter
} from "./talkPublisher";
import {
  authorizeFixedPlayback,
  controlDevice,
  controlPtz,
  controlPtzCruise,
  createCruiseTrack,
  controlPtzPrecise,
  controlPtzScan,
  callPtzPreset,
  createTalkSession,
  createPtzPreset,
  deletePtzPreset,
  deleteTalkSession,
  fetchPTZDefaultSpeedConfig,
  getControlCapabilities,
  getChannelTargetTrack,
  getCruiseTrack,
  getChannelVideoParams,
  getHomePosition,
  getPtzOperation,
  getPtzPreciseStatus,
  getTalkSession,
  getStreamMonitor,
  listCruiseTracks,
  listPtzPresets,
  createStreamProbe,
  getStreamProbeOperation,
  startPlay,
  stopPlay,
  setChannelTargetTrack,
  updateHomePosition,
  type ControlCapability,
  type CruiseTrackDetailResource,
  type CruiseTrackPointResource,
  type DeviceControlCapabilities,
  type HomePositionConfig,
  type HomePositionPatch,
  type HomePositionResult,
  type HomePositionSupport,
  type PlayResult,
  type ProbeSnapshot,
  type PTZOperation,
  type PTZResourceFreshness,
  type StreamMonitorSnapshot,
  type TalkCreateResult,
  type TargetTrackArea,
  type TargetTrackIntent,
  type TargetTrackMode,
  type VideoParam,
  type VideoParamResult
} from "@/api/gb28181";
import { DEFAULT_PTZ_SPEED_LEVEL, levelToProtocolSpeed, normalizePtzSpeedLevel } from "../ptzSpeed";
import { buildTargetTrackArea } from "../targetTrackBox";
import { frameRateText, resolutionText, videoFormatText, type VideoParamCodecItem } from "../videoParamCodec";
import {
  Activity,
  AlertTriangle,
  Camera,
  CheckCircle2,
  ChevronDown,
  Circle,
  CircleSlash,
  Compass,
  Copy,
  Crosshair,
  FlipHorizontal,
  FlipVertical,
  Focus as FocusIcon,
  Frame,
  Gauge,
  Hash,
  Home,
  Info,
  Loader2,
  Maximize2,
  Mic,
  Move3d,
  MoveHorizontal,
  Navigation,
  Inbox,
  Pause,
  Play,
  PictureInPicture2,
  Plus,
  RadioTower,
  RefreshCcw,
  RotateCw,
  Route,
  Scan,
  ScanEye,
  Search,
  Settings,
  ShieldCheck,
  Signal,
  Square,
  Target,
  Trash2,
  Send,
  Video,
  X,
  ZoomIn,
  ZoomOut
} from "@lucide/vue";

/* ────────────────────────── Props / Emits ────────────────────────── */

interface PlaybackChannel {
  id: number;
  channelId: string;
  deviceId: string;
  name?: string;
  alias?: string;
  manufacturer?: string;
  model?: string;
  ptzType?: number;
  status: number;
  streamTransport?: string;
  /** 通道音频开关（点播是否接收音频），透传给播放器决定是否出声/显示音频控件 */
  audioEnabled?: boolean;
}

const props = withDefaults(
  defineProps<{
    visible: boolean;
    channel: PlaybackChannel | null;
    displayMode?: PlaybackConsoleDisplayMode;
  }>(),
  {
    displayMode: "expanded"
  }
);
const emit = defineEmits<{
  (event: "update:visible", value: boolean): void;
  (event: "update:displayMode", value: PlaybackConsoleDisplayMode): void;
}>();

const MINI_PLAYER_WIDTH = 400;
const MINI_PLAYER_HEADER_HEIGHT = 48;
const MINI_PLAYER_MARGIN = 16;
const isMinimized = computed(() => props.displayMode === "minimized");

function miniPlayerDimensions() {
  const viewportWidth = window.innerWidth > 0 ? window.innerWidth : 1024;
  const width = Math.min(MINI_PLAYER_WIDTH, Math.max(240, viewportWidth - MINI_PLAYER_MARGIN * 2));
  return { width, height: MINI_PLAYER_HEADER_HEIGHT + (width * 9) / 16 };
}

function renderedMiniPlayerDimensions() {
  const fallback = miniPlayerDimensions();
  const modal = document.querySelector<HTMLElement>(".play-console-modal--minimized");
  const rect = modal?.getBoundingClientRect();
  return {
    width: rect && rect.width > 0 ? rect.width : fallback.width,
    height: rect && rect.height > 0 ? rect.height : fallback.height
  };
}

function clampMiniPlayerPosition(x: number, y: number) {
  const viewportWidth = window.innerWidth > 0 ? window.innerWidth : 1024;
  const viewportHeight = window.innerHeight > 0 ? window.innerHeight : 768;
  const size = renderedMiniPlayerDimensions();
  const maxX = Math.max(MINI_PLAYER_MARGIN, viewportWidth - size.width - MINI_PLAYER_MARGIN);
  const maxY = Math.max(MINI_PLAYER_MARGIN, viewportHeight - size.height - MINI_PLAYER_MARGIN);
  return {
    x: Math.min(maxX, Math.max(MINI_PLAYER_MARGIN, x)),
    y: Math.min(maxY, Math.max(MINI_PLAYER_MARGIN, y))
  };
}

function defaultMiniPlayerPosition() {
  const viewportWidth = window.innerWidth > 0 ? window.innerWidth : 1024;
  const viewportHeight = window.innerHeight > 0 ? window.innerHeight : 768;
  const size = miniPlayerDimensions();
  return clampMiniPlayerPosition(
    viewportWidth - size.width - MINI_PLAYER_MARGIN,
    viewportHeight - size.height - MINI_PLAYER_MARGIN
  );
}

const miniPlayerPosition = ref(defaultMiniPlayerPosition());
let miniPlayerDrag: { startX: number; startY: number; originX: number; originY: number } | null = null;
const playbackModalWidth = computed(() =>
  isMinimized.value ? `${miniPlayerDimensions().width}px` : "min(1520px, calc(100vw - 32px))"
);
const miniPlayerModalStyle = computed<CSSProperties | undefined>(() =>
  isMinimized.value
    ? {
        position: "fixed",
        top: `${miniPlayerPosition.value.y}px`,
        left: `${miniPlayerPosition.value.x}px`,
        margin: "0",
        transform: "none"
      }
    : undefined
);
const miniPlayerBodyStyle = computed<CSSProperties | undefined>(() =>
  isMinimized.value
    ? {
        padding: "0",
        maxHeight: "none",
        overflow: "hidden"
      }
    : undefined
);

function updateMiniPlayerDrag(event: PointerEvent) {
  if (!miniPlayerDrag) return;
  miniPlayerPosition.value = clampMiniPlayerPosition(
    miniPlayerDrag.originX + event.clientX - miniPlayerDrag.startX,
    miniPlayerDrag.originY + event.clientY - miniPlayerDrag.startY
  );
}

function finishMiniPlayerDrag() {
  miniPlayerDrag = null;
  window.removeEventListener("pointermove", updateMiniPlayerDrag);
  window.removeEventListener("pointerup", finishMiniPlayerDrag);
  window.removeEventListener("pointercancel", finishMiniPlayerDrag);
}

function beginMiniPlayerDrag(event: PointerEvent) {
  if (!isMinimized.value || event.button !== 0) return;
  miniPlayerDrag = {
    startX: event.clientX,
    startY: event.clientY,
    originX: miniPlayerPosition.value.x,
    originY: miniPlayerPosition.value.y
  };
  window.addEventListener("pointermove", updateMiniPlayerDrag);
  window.addEventListener("pointerup", finishMiniPlayerDrag);
  window.addEventListener("pointercancel", finishMiniPlayerDrag);
  event.preventDefault();
}

function keepMiniPlayerInViewport() {
  miniPlayerPosition.value = clampMiniPlayerPosition(miniPlayerPosition.value.x, miniPlayerPosition.value.y);
}

/* ────────────────────────── 会话状态 ────────────────────────── */

type SessionPhase = "idle" | "requesting" | "playing" | "paused" | "error" | "stopping";
const phase = ref<SessionPhase>("idle");
const errorMessage = ref("");
const startedAt = ref<number | null>(null);
const now = ref(Date.now());
const playResult = ref<PlayResult | null>(null);
let timer: number | null = null;
let monitorTimer: number | null = null;
let sessionToken = 0;
// 关闭弹窗只做本地清理;记录阈值,阻止迟到的点播响应补发 stopPlay。
let localCleanupThroughToken = 0;

function channelContextKey(channel: PlaybackChannel | null = props.channel) {
  if (!channel) return "";
  return `${channel.id}:${channel.deviceId}:${channel.channelId}`;
}

function isCurrentChannelContext(channelId: number, token: number, contextKey: string) {
  return token === sessionToken && props.visible && props.channel?.id === channelId && channelContextKey() === contextKey;
}

/* ────────────────────────── Tab 切换 ────────────────────────── */

const userStore = useUserStoreHook();
const permissions = computed(() => userStore.account.permissions ?? []);
const hasPermission = (permission: string) => permissions.value.includes("*:*:*") || permissions.value.includes(permission);
const canStartPlayback = computed(() => hasPermission("gb28181:play:start"));
const canMonitorPlayback = computed(() => hasPermission("gb28181:play:monitor"));
const canDiagnosePlayback = computed(() => hasPermission("gb28181:play:diagnose"));
const canSharePlayback = computed(() => hasPermission("gb28181:play:share"));
const canViewPtz = computed(() => hasPermission("gb28181:ptz:view"));
const canControlPtz = computed(() => hasPermission("gb28181:ptz:control"));
const canSavePtzPreset = computed(() => hasPermission("gb28181:ptz:preset:save"));
const canCallPtzPreset = computed(() => hasPermission("gb28181:ptz:preset:call"));
const canDeletePtzPreset = computed(() => hasPermission("gb28181:ptz:preset:delete"));
const canControlPtzCruise = computed(() => hasPermission("gb28181:ptz:cruise"));
const canUpdatePtzHome = computed(() => hasPermission("gb28181:ptz:home"));
const canControlDevice = computed(() => hasPermission("gb28181:device:control"));
const canTalk = computed(() => hasPermission("gb28181:talk:control"));
const canReadPtzSpeed = computed(() => hasPermission("gb28181:sip:config:view"));
const canPtzPanel = computed(
  () =>
    canViewPtz.value ||
    canControlPtz.value ||
    canSavePtzPreset.value ||
    canCallPtzPreset.value ||
    canDeletePtzPreset.value ||
    canControlPtzCruise.value ||
    canUpdatePtzHome.value
);

/* "流信息"tab 已并入"视频探针":概览卡承担全部实时监视信息(媒体节点/流 ID/视频音频参数/
 * 数据速率/丢包/当前观看)。删掉独立 tab 让侧栏窄一档、层级也更清爽。
 *
 * "视频参数"2026-09-18 从"高级"里拆成独立 tab:它是 A.2.3.2 设备配置类
 * (读 A.2.4.7 ConfigDownload / 写 A.2.3.2.5 DeviceConfig),语义上是"设备侧配置"。
 * ⛔ 可见性用 `canViewPtz`(gb28181:ptz:view),**不是** device:control ——
 * 本卡每个加载函数(loadVideoParams / 回读轮询 / 下发)都以 `ptz:view` 为门禁,
 * 挂到 control 上会出现"有读权限却看不见"或"看得见但读不出"的错配。
 *
 * ⭐ "视频编码" tab 2026-09-20 **并回「画面设置」**,一级页签 5 → 4;同日再收掉"设备维护" ⇒ **3 个**。
 *   判据:它和图像叠加、画面镜像、隐私遮挡改的是**同一台设备的同一路画面**,
 *   却要占两个并列的顶级入口 —— 用户想"把画面调一下"得先猜是哪个页签。
 *   合并后「画面设置」= 侧栏一组(`video-param`) + 底栏四格
 *   (图像叠加 / 遮挡 / 镜像 / 参数对照)。
 *   ⛔ 别再给它单开一级页签,也别退回"侧栏两组二选一"的切换形态(老板 2026-09-20 明确否掉)。
 *   ⛔ 也别把参数对照挪回侧栏:两行(回读/实测)并排 + 一个判定标签才读得出"设备到底跟没跟",
 *      窄侧栏里会折成四行。
 *
 * ⭐ "高级"tab 2026-09-20 **整体消失**,三拨内容各有归属(控制台不再保留任何一处):
 *   ① 图像抓拍配置 → 设备管理页「设备详情」抽屉的「设备控制」页;
 *   ② 设备录制 / 布防撤防 / 报警复位 → 同上(它们是**设备侧动作**,不是"这一路流"的动作,
 *      关掉播放弹窗就没了,而设备详情是常驻入口);
 *   ③ 请求关键帧 → 云台控制侧栏底部(画面级即时操作,与摇杆/3D 拖拽同族)。
 *
 * ⭐ "录像存储" / "报警控制" 两个**整页签**同日也搬去了同一个「设备详情」抽屉
 *   (`record-plan` / `alarm-record` / `alarm-report` 三组走 DeviceConfigDrawer 的 config-only 通道)。
 *   判据与①②同源:录像计划、报警录像、报警上报写的都是**设备侧的配置**,
 *   与"这一路流现在播得怎么样"无关;控制台里它们只能靠 SideBar 里那一个共用面板承载,
 *   关掉弹窗就没入口。控制台侧不再保留第二处(详见 device-mgmt/index.vue 的页签契约测试)。
 *
 * ⭐ 同日稍晚 "设备维护" 这最后一个整页签也搬走了(改叫「基本参数」,同样落在那张抽屉里)。
 *   判据:**它从头到尾只有 `basic` 一组**(A.2.1.19 BasicParam:设备名称/注册有效期/心跳间隔/
 *   最大心跳超时次数),是纯粹的**设备侧配置下发**,与画面无关 —— 留在这里,
 *   "改个心跳间隔"要先打开某一路正在播的画面控制台,而设备详情抽屉本来就是设备维度的常驻入口。
 *   ⛔ 别把 `device: ["basic"]` 加回来:控制台侧那套 SideBar 只有"画面设置"一个归属了。
 *   ⛔ 也别以为它并进了「设备信息」页 —— 那一页显示注册状态/心跳间隔是**只读事实**,
 *      与"把值下发到设备"不是同一件事。 */
type TabKey = "ptz" | "probe" | "deviceconfig";

/**
 * operation 生命周期。⛔ 不要跟着「高级」页签一起删掉 —— 控制台里还有两个动作在用它：
 * 请求关键帧（iframe）与 3D 拖框缩放（drag_zoom_in/out），它们走的是同一套 operation 收敛。
 */
type AdvancedOperationPhase = "idle" | "queued" | "sent" | "accepted" | "rejected" | "timeout" | "unknown" | "cancelled";
const activeTab = ref<TabKey>("ptz");
const sideCollapsed = ref(false);
/** 画面设置底栏卡片里的遮挡区槽位（由 DeviceConfigDrawer 暴露，标准固定 4 个）。 */
interface PictureRegionSlot {
  seq: number;
  coords: number[];
  used: boolean;
}

/** DeviceConfigDrawer 暴露给底栏「画面卡片」的读写口。 */
interface DeviceConfigCardHandle {
  selectGroup: (key: string) => void;
  pictureEditable: boolean;
  pictureRegions: PictureRegionSlot[];
  pictureRegionCount: number;
  pictureCanAddRegion: boolean;
  pictureMaskOn: boolean;
  /** 设备**事实上**的总闸（回读值）：画布据此决定"画不画框"，不能用草稿值。 */
  pictureAppliedMaskOn: boolean;
  /** 设备"已停用但区域残留"的槽位数。 */
  pictureRetainedRegionCount: number;
  /** 本次草稿动过遮挡本身没有。 */
  pictureMaskDraftTouched: boolean;
  pictureMirror: string;
  /**
   * 设备**声明的图像坐标画布**（遮挡区坐标的基准）；`null` = 本次没读到 OSD 事实。
   *
   * ⛔ 不是画面解码尺寸：真机实测（海康，码流 2560×1440）遮挡坐标基准是 704×576。
   */
  pictureCanvasSize: { width: number; height: number } | null;
  pictureDirtyCount: number;
  pictureDirtyFields: string[];
  pictureFactsMissing: boolean;
  pictureAbsentTypes: string[];
  /** 画面组最近一次下发错误（含本地校验拦下的原因）。 */
  pictureError: string;
  /** 本次下发"确实发出去了、但结果不是你以为的那样"的说明（如：启用了却没带区域）。 */
  pictureMaskNotice: string;
  setPictureRegion: (seq: number, rect: { left: number; top: number; right: number; bottom: number }) => void;
  clearPictureRegion: (seq: number) => void;
  nextPictureRegionSeq: () => number;
  setPictureMaskOn: (value: boolean) => void;
  setPictureMirror: (value: string) => void;
  revertPictureGroup: () => void;
  readPictureGroup: () => void;
  applyPictureGroup: () => Promise<void>;
  /** 点开关时的即时下发：只发 `PictureMask` 这一块，不捎带镜像草稿。 */
  applyPictureMaskOnly: () => Promise<void>;
  // ── 图像叠加（OSD）：钉在 `osd` 组上，与画面组共用同一套读写口 ──
  osdEditable: boolean;
  /** 画布要画的锚点（时间戳 + 各条文字），含四态：已生效 / 待下发 / 未定位 / 已关闭。 */
  osdAnchors: OsdAnchorSlot[];
  osdDirtyCount: number;
  osdDirtyFields: string[];
  /** 还没在画面上定位的文字行数 —— 浮条据此把「下发」拦下来。 */
  osdUnplacedCount: number;
  osdFactsMissing: boolean;
  /** OSD 组最近一次下发/拒发的原因（与 `pictureError` 同一个机制）。 */
  osdError: string;
  /** 侧栏点「在画面上定位」→ 画布把对应锚点闪一下（带递增 `seq`，连点两次也会触发）。 */
  osdFocusToken: { kind: "time" | "item"; index: number; seq: number } | null;
  setOsdTimePosition: (axis: "x" | "y", value: number) => void;
  setOsdItemPosition: (index: number, x: number, y: number) => void;
  readOsdGroup: () => void;
  revertOsdGroup: () => void;
  applyOsdGroup: () => Promise<void>;
  // ── 底栏「图像叠加」整块面板专用（2026-09-20）──
  /**
   * 一整袋 props，直接 `v-bind` 给 `DeviceConfigOsdBlocks`（`layout="row"`）。
   *
   * ⛔ 宿主只渲染、不另存一份：值全部来自抽屉的 `familyValues.osd`，
   *    写口就下面三个（`setOsdFlag` / `setOsdItems` / `setOsdTimePosition`）。
   * ⛔ 袋里的 `editing` / `canvasLinked` 反映的是**抽屉自己的 props**；
   *    播放控制台里这两个由画面侧持有，所以渲染时会显式覆盖（见底栏 `picture-osd-cell`）。
   */
  osdBlocks: {
    timeEnable: boolean;
    timeType: string;
    timeX: string;
    timeY: string;
    textEnable: boolean;
    items: ConfigTextItem[];
    canvas: { width: number; height: number } | null;
    disabled: boolean;
    maxItems: number;
    editing: boolean;
    canvasLinked: boolean;
  };
  /** 开关 / 时间格式这类单值字段。 */
  setOsdFlag: (fieldKey: string, value: string | boolean) => void;
  /** 叠加文字整表（增删改都在宿主侧算好后整表回写）。 */
  setOsdItems: (rows: ConfigTextItem[]) => void;
  /** 点「在画面上定位」→ 让画布闪一下对应锚点。 */
  focusOsdAnchor: (payload: { kind: "time" | "item"; index: number }) => void;
  /** 浮条「下发」：画面 + OSD 的草稿**合并成一条报文**发出去。 */
  applyPictureAndOsdGroups: () => Promise<void>;
}

/** 画布上的一枚 OSD 标记（与 `DeviceConfigDrawer` 暴露的形状对应）。 */
interface OsdAnchorSlot {
  key: string;
  kind: "time" | "item";
  /** 文字行在数组里的下标；时间戳恒为 `-1`。 */
  index: number;
  /** 画在锚点上的标签：时间戳是「时间戳」，文字是「1 北门」。 */
  label: string;
  x: number;
  y: number;
  unplaced: boolean;
  draft: boolean;
  off: boolean;
}

const deviceConfigRef = ref<DeviceConfigCardHandle | null>(null);

/** 播放器句柄：画面真实解码尺寸是遮挡坐标的唯一参考系。 */
const playWindowRef = ref<{ refreshVideoSize?: () => void } | null>(null);
const configWorkspaceGroups: Partial<Record<TabKey, string[]>> = {
  // ⭐ 2026-09-20 第二次收敛（老板：「不想用切换的方式，想让它们都在一个页面上全部展示出来」）：
  //    侧栏**只挂 `video-param`**（视频编码），「图像叠加」整块搬到底栏第一格
  //    （`picture-osd-cell`，渲染 `DeviceConfigOsdBlocks` 的 `layout="row"`，数据走
  //    `DeviceConfigDrawer` 暴露的 `osdBlocks` 袋 + `setOsdFlag` / `setOsdItems`）。
  //    ⇒ 这一页现在一次看全：视频编码（侧栏） / 图像叠加 · 遮挡 · 镜像 · 参数对照（底栏）。
  // ⛔ 别把 `osd` 加回这个数组：加回来侧栏又长出 `dcg-nav` 切换，同一份
  //    `familyValues.osd` 就有了两个编辑面（底栏那份是新的）。
  // ⛔ 「画面处理」（镜像 + 隐私遮挡）仍下沉在底栏卡片，不在这里：侧栏再挂一份，
  //    同一份 `familyValues.picture` 就有两个编辑入口了。
  // ⛔ record / alarm / basic 三组 2026-09-20 已随各自的整页签搬到设备详情抽屉，别在这里加回来 ——
  //    加回来就等于控制台重新长出第二个配置入口，两边会各持一份通道上下文。
  deviceconfig: ["video-param"]
};
const activeConfigGroups = computed(() => configWorkspaceGroups[activeTab.value] ?? []);
const isConfigWorkspace = computed(() => activeConfigGroups.value.length > 0);

/**
 * 「画面设置」的侧栏分组自 2026-09-20 起**只有一组**（`video-param`）：图像叠加整块搬到了底栏。
 *
 * ⇒ 宿主不再需要持有"当前第几组"，也就不再往抽屉传 `v-model:active-group-key`。
 * ⛔ 那个口当初存在的唯一理由是**切离「图像叠加」组时退出 OSD 编辑模式**；现在整页同屏，
 *    这条联动自动消失 —— 退出编辑模式只剩：按钮 / Esc / 换页签 / 换通道（见 `exitOsdEditMode` 的调用点）。
 * ⛔ 别为了"以后可能要切组"把这个口留着：留着的代价是宿主与抽屉各持一份"当前组"，
 *    而没有任何一处会读它（本仓已经因为"两份状态"返工过三次）。
 */

const tabs: Array<{ key: TabKey; label: string; icon: any; description: string }> = [
  { key: "ptz", label: "云台控制", icon: Compass, description: "GB28181-2022 全能力" },
  // ⛔ description 只写**用户能找到东西的词**，不写"A.2.3.2 设备配置类"这类条款号。
  { key: "deviceconfig", label: "画面设置", icon: Camera, description: "视频编码 · 图像叠加 · 遮挡" },
  { key: "probe", label: "视频探针", icon: Activity, description: "实时监视 + 逐帧采样" }
];
const visibleTabs = computed(() =>
  tabs.filter(
    tab =>
      (tab.key === "ptz" && canPtzPanel.value) ||
      (tab.key === "probe" && (canMonitorPlayback.value || canDiagnosePlayback.value)) ||
      (Boolean(configWorkspaceGroups[tab.key]) && canViewPtz.value)
  )
);
watch(
  visibleTabs,
  nextTabs => {
    if (nextTabs.length > 0 && !nextTabs.some(tab => tab.key === activeTab.value)) activeTab.value = nextTabs[0].key;
  },
  { immediate: true }
);

/* ─────────────────── 详情区 ───────────────────
 * 详情条只有一个形态:一个侧栏 Tab 对应一块内容,切换侧栏就换一块。
 *
 * ⛔ 「设备状态」「存储卡状态」2026-09-20 起**只在设备管理页的「设备详情」抽屉里**
 *    (页签:设备信息 / 设备状态 / 存储卡),控制台侧不再有这两个入口 ——
 *    它们读的是通道级事实(/channel/:id/...),本来就属于"某个设备的详情",
 *    不是播放控制台该承载的东西。别在这里把它们加回来,也不要再造一套详情区页签机制。 */

/* ────────────────────────── 视频区状态 ────────────────────────── */

type StreamProtocol =
  | "ws-flv"
  | "http-flv"
  | "wss-flv"
  | "https-flv"
  | "ws-fmp4"
  | "http-fmp4"
  | "wss-fmp4"
  | "https-fmp4"
  | "hls"
  | "https-hls"
  | "ws-ts"
  | "http-ts"
  | "wss-ts"
  | "https-ts"
  | "webrtc"
  | "webrtcs"
  | "rtmp"
  | "rtmps"
  | "rtsp"
  | "rtsps";
type ProtocolURLMap = Record<StreamProtocol, string | null>;
type ProtocolOption = {
  value: StreamProtocol;
  label: string;
  browserPlayable: boolean;
  shortcut?: boolean;
};

const protocol = ref<StreamProtocol | "">("ws-flv");
const playbackSnapshot = ref<PlaybackSource | null>(null);
function protocolUrlsFor(result: PlayResult | null | undefined): ProtocolURLMap {
  return {
    "ws-flv": result?.urls?.wsFlv || result?.wsflvUrl || null,
    "http-flv": result?.urls?.httpFlv || result?.httpFlvUrl || null,
    "wss-flv": result?.urls?.wssFlv || null,
    "https-flv": result?.urls?.httpsFlv || null,
    "ws-fmp4": result?.urls?.wsFmp4 || null,
    "http-fmp4": result?.urls?.httpFmp4 || null,
    "wss-fmp4": result?.urls?.wssFmp4 || null,
    "https-fmp4": result?.urls?.httpsFmp4 || null,
    hls: result?.urls?.hls || result?.hlsUrl || null,
    "https-hls": result?.urls?.httpsHls || null,
    "ws-ts": result?.urls?.wsTs || null,
    "http-ts": result?.urls?.httpTs || null,
    "wss-ts": result?.urls?.wssTs || null,
    "https-ts": result?.urls?.httpsTs || null,
    webrtc: result?.urls?.webrtc || null,
    webrtcs: result?.urls?.webrtcs || null,
    rtmp: result?.urls?.rtmp || null,
    rtmps: result?.urls?.rtmps || null,
    rtsp: result?.urls?.rtsp || null,
    rtsps: result?.urls?.rtsps || null
  };
}
function hasPlaybackToken(rawURL: string): boolean {
  try {
    return new URL(rawURL).searchParams.has("play_token");
  } catch {
    return false;
  }
}
const protocolUrls = computed<ProtocolURLMap>(() => protocolUrlsFor(playResult.value));

const protocolOptions: ProtocolOption[] = [
  { value: "ws-flv", label: "WS-FLV", browserPlayable: true, shortcut: true },
  { value: "wss-flv", label: "WSS-FLV", browserPlayable: true },
  { value: "http-flv", label: "HTTP-FLV", browserPlayable: true, shortcut: true },
  { value: "https-flv", label: "HTTPS-FLV", browserPlayable: true },
  { value: "hls", label: "HLS", browserPlayable: true, shortcut: true },
  { value: "https-hls", label: "HTTPS-HLS", browserPlayable: true },
  { value: "ws-fmp4", label: "WS-fMP4", browserPlayable: false },
  { value: "wss-fmp4", label: "WSS-fMP4", browserPlayable: false },
  { value: "http-fmp4", label: "HTTP-fMP4", browserPlayable: false },
  { value: "https-fmp4", label: "HTTPS-fMP4", browserPlayable: false },
  { value: "ws-ts", label: "WS-TS", browserPlayable: false },
  { value: "wss-ts", label: "WSS-TS", browserPlayable: false },
  { value: "http-ts", label: "HTTP-TS", browserPlayable: false },
  { value: "https-ts", label: "HTTPS-TS", browserPlayable: false },
  { value: "webrtc", label: "WebRTC", browserPlayable: true, shortcut: true },
  { value: "webrtcs", label: "WebRTCS", browserPlayable: true },
  { value: "rtmp", label: "RTMP", browserPlayable: false },
  { value: "rtmps", label: "RTMPS", browserPlayable: false },
  { value: "rtsp", label: "RTSP", browserPlayable: false },
  { value: "rtsps", label: "RTSPS", browserPlayable: false }
];

const availableProtocolOptions = computed(() => protocolOptions.filter(option => Boolean(protocolUrls.value[option.value])));
const shortcutProtocolOptions = computed(() => protocolOptions.filter(option => option.shortcut));
const currentProtocolOption = computed(() => protocolOptions.find(option => option.value === protocol.value));

function toEasyPlayerWebRtcUrl(url: string) {
  return url.replace(/^https?:\/\//i, "webrtc://");
}
const currentProtocolUrl = computed(() => {
  if (!protocol.value) return "";
  if (playbackSnapshot.value?.protocol === protocol.value) return playbackSnapshot.value.url;
  const url = protocolUrls.value[protocol.value] || "";
  return protocol.value === "webrtc" || protocol.value === "webrtcs" ? toEasyPlayerWebRtcUrl(url) : url;
});
const currentProtocolUsesZlmWebRtc = computed(() =>
  playbackSnapshot.value?.protocol === protocol.value
    ? playbackSnapshot.value.zlmWebrtc
    : protocol.value === "webrtc" || protocol.value === "webrtcs"
);
function isBrowserPlayable(proto: StreamProtocol) {
  return protocolOptions.find(option => option.value === proto)?.browserPlayable === true;
}
function defaultProtocol(): StreamProtocol | "" {
  const secure = window.location.protocol === "https:";
  const preference = secure
    ? ["wss-flv", "https-flv", "https-hls"]
    : ["ws-flv", "http-flv", "hls", "wss-flv", "https-flv", "https-hls"];
  return (preference.find(value => protocolUrls.value[value as StreamProtocol]) as StreamProtocol | undefined) || "";
}

const title = computed(
  () => props.channel?.alias?.trim() || props.channel?.name?.trim() || props.channel?.channelId || "未选择通道"
);

const phaseHint = computed(() => {
  if (phase.value === "requesting") return "正在向设备发送 INVITE,等待 RTP 媒体建立";
  if (phase.value === "playing") return "媒体链路正常,画面已实时呈现";
  if (phase.value === "paused") return "已暂停,可随时恢复";
  if (phase.value === "error") return errorMessage.value || "无法建立播放链路";
  if (phase.value === "stopping") return "正在释放会话资源";
  return "点击左侧通道开始播放";
});

const sessionStatusText = computed(() => {
  if (phase.value === "requesting") return "建立中";
  if (phase.value === "paused") return "已暂停";
  if (phase.value === "stopping") return "停播中";
  if (phase.value === "error") return "异常";
  if (phase.value === "playing") {
    if (monitorState.value === "offline") return "媒体流已离线";
    if (monitorState.value === "stale") return "监控数据过期";
    return "播放中";
  }
  return "待播放";
});

const sessionStatusClass = computed(() => ({
  active: phase.value === "playing" && monitorState.value !== "offline" && monitorState.value !== "stale",
  loading: phase.value === "requesting" || phase.value === "stopping",
  error: phase.value === "error" || (phase.value === "playing" && monitorState.value === "offline"),
  paused: phase.value === "paused",
  warn: phase.value === "playing" && monitorState.value === "stale"
}));

const monitorAlive = ref<{ seconds: number; receivedAt: number } | null>(null);
const elapsedText = computed(() => {
  if (monitorAlive.value) {
    const seconds = monitorAlive.value.seconds + Math.floor(Math.max(0, now.value - monitorAlive.value.receivedAt) / 1000);
    return formatDuration(seconds * 1000);
  }
  return startedAt.value ? formatDuration(Math.max(0, now.value - startedAt.value)) : "00:00";
});
const monitorCollectedAtText = computed(() => {
  const value = monitorSnapshot.value?.collectedAt;
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleTimeString("zh-CN", { hour12: false });
});

/* ────────────────────────── 云台与设备能力 ────────────────────────── */

const capabilities = ref<DeviceControlCapabilities | null>(null);
function capability(key: keyof DeviceControlCapabilities): ControlCapability {
  const value = capabilities.value?.[key];
  return value || { state: "unknown", reason: "能力尚未读取" };
}
function capabilityActionTitle(key: keyof DeviceControlCapabilities, action: string) {
  const value = capability(key);
  if (value.state === "supported") return action;
  const stateText = value.state === "unsupported" ? "设备上报不支持" : "设备未明确声明支持";
  return `${action} · ${stateText}${value.reason ? ` (${value.reason})` : ""},仍可尝试,以设备响应为准`;
}
const isAudioCapable = computed(() => props.channel?.status === 1 && canTalk.value);
const talkAvailable = computed(() => props.channel?.status === 1 && canTalk.value);

const ptzMode = ref<"speed" | "precise">("speed"); // 速度模式 / 精准模式
const moveSpeed = ref(DEFAULT_PTZ_SPEED_LEVEL);
// 自动聚焦 / 自动光圈已摘除:那是厂商私有概念,GB/T 28181 的 FI 指令族(表 A.6)里
// 只有"光圈放大/缩小"和"聚焦近/远"四个动作 + 本族停止,没有任何自动档位。留一个点了
// 不发任何标准指令的开关,只会让人以为它在生效。
type JoystickDirection = "左上" | "上" | "右上" | "左" | "右" | "左下" | "下" | "右下";
const joystickDragging = ref(false);
const joystickPointerId = ref<number | null>(null);
const joystickDirection = ref<JoystickDirection | "">("");
const joystickOffsetX = ref(0);
const joystickOffsetY = ref(0);
const joystickHandleStyle = computed(() => ({
  transform: `translate(calc(-50% + ${joystickOffsetX.value}px), calc(-50% + ${joystickOffsetY.value}px))`
}));

// 精准 PTZ(2022)
const precisePan = ref(180); // 0-360
const preciseTilt = ref(0); // -90 to 90
const preciseZoom = ref(1); // 1-32x

// 预置位与巡航只展示后端返回的真实资源;摘要和抽屉的布局保持原设计。
type Preset = { id: number; name: string; setAt?: string };
const presets = ref<Preset[]>([]);
const activePresetId = ref<number | null>(null);
// 「保存当前位置」小对话框的草稿态:自动生成默认名,用户可改可不改
const presetDraft = ref<{ id: number; name: string; submitting: boolean } | null>(null);
const savePresetDialogVisible = ref(false);
const presetNameTouched = ref(false);
// 详情区固定为紧凑三行九格;溢出时保留八个预置位,将“更多”放在最后一格。
// 不按网格自身高度反推数量,避免内容撑高网格后又放入更多项的反馈回路。
const PRESET_GRID_SLOTS = 9;
const maxVisibleTiles = PRESET_GRID_SLOTS;
const hasMorePresets = computed(() => presets.value.length > maxVisibleTiles);
const visiblePresets = computed(() => (hasMorePresets.value ? presets.value.slice(0, maxVisibleTiles - 1) : presets.value));
const presetMoreVisible = ref(false);
// 「从设备同步」的状态。预置位和巡航这两张卡片展示的都是**设备侧资源**,平台只是
// 一份镜像 —— 设备上早就存在的预置位/巡航在界面上只有靠回读才可能出现。
const presetFreshness = ref<PTZResourceFreshness>("unknown");
const presetSyncing = ref(false);
const presetSyncError = ref("");
/** 「从设备同步」药丸的共用文案。预置位与巡航两张卡片的状态机必须**逐字一致**:
 *  同一种状态在两处叫法不同时,操作员会以为是两件事(「设备数据已同步」和「已同步」
 *  并排出现时,没人能判断哪个更权威)。所以这里只有一份措辞。
 *
 *  ⛔ 文案一律**短于 6 个汉字**。卡片是详情条三等分,头部一行还要并排放「添加」
 *  按钮;原来那句「数据过期,点击同步」9 个字会把动作标题挤出卡片(卡片 overflow:
 *  hidden,直接裁掉而不是换行)。"点击同步"这个意思由按钮形态本身和 tooltip 承担。 */
function resourceSyncPillLabel(state: {
  syncing: boolean;
  failed: boolean;
  freshness: PTZResourceFreshness;
  /** 从没回读过时的文案 —— 卡片上还没有任何设备数据,说的应该是"去同步"这个动作。 */
  fallback: string;
}): string {
  if (state.syncing) return "同步中";
  if (state.failed) return "同步失败";
  if (state.freshness === "fresh") return "已同步";
  if (state.freshness === "stale") return "数据过期";
  return state.fallback;
}
const presetSyncLabel = computed(() =>
  resourceSyncPillLabel({
    syncing: presetSyncing.value,
    failed: Boolean(presetSyncError.value),
    freshness: presetFreshness.value,
    fallback: "从设备同步"
  })
);
/** 药丸的 tooltip。**失败原因写在最前面** —— 药丸上放不下「设备未应答」这类
 *  具体原因(卡只有三分之一宽),但不说出来的话操作员只会反复点同一个按钮。 */
const presetSyncTitle = computed(() => {
  const reason = presetSyncError.value ? `${presetSyncError.value}。` : "";
  return `${reason}从设备重新读取预置位清单(预置位以设备为准,平台只存镜像)`;
});
function nextPresetId(): number {
  return presets.value.length ? Math.max(...presets.value.map(p => p.id)) + 1 : 1;
}

// 巡航轨迹(2022 CruiseTrackListQuery / CruiseTrackQuery)
type CruisePoint = { presetId: number; speed: number | null; dwellSec: number | null };
type CruiseTrack = {
  id: number;
  name: string;
  enabled: boolean;
  pending: boolean;
  points: CruisePoint[] | null;
  source: string;
};
const cruiseTracks = ref<CruiseTrack[]>([]);
const activeCruiseId = ref<number | null>(null);
// HTTP 成功只表示控制指令已发送，不能据此宣称设备正在运行。
const cruiseState = ref<"stopped" | "start-sent">("stopped");
const cruiseFreshness = ref<PTZResourceFreshness>("unknown");
const cruiseRefreshPending = ref(false);
const cruiseLoadError = ref("");
const cruiseRefreshError = ref("");
// 整个「从设备同步」流程是否在进行中(列表查询 → 等应答 → 重读 → 补点位链)。
// 与 cruiseRefreshPending 不是一回事:后者只表示"最近一次响应里说有一个查询还在飞"。
const cruiseSyncing = ref(false);
const unconfirmedCruiseCount = computed(() => cruiseTracks.value.filter(track => track.pending).length);
const cruiseSyncLabel = computed(() => {
  // ⛔ 「同步中」必须排在最前。原来的顺序把「N 条未验证」放在「正在同步」之前,
  //    于是刚好在同步进行中时,按钮显示的是上一轮的遗留结论,操作员看不到任何反馈。
  if (cruiseSyncing.value) return "同步中";
  if (cruiseLoadError.value) return "加载失败";
  if (cruiseRefreshError.value) return "同步失败";
  if (unconfirmedCruiseCount.value > 0) return `${unconfirmedCruiseCount.value} 条未验证`;
  if (cruiseRefreshPending.value) return "正在同步";
  return resourceSyncPillLabel({
    syncing: false,
    failed: false,
    freshness: cruiseFreshness.value,
    fallback: "从设备同步"
  });
});
/** 同 presetSyncTitle:失败原因前置,药丸上只放得下短状态词。 */
const cruiseSyncTitle = computed(() => {
  const reason = cruiseRefreshError.value || cruiseLoadError.value;
  return `${reason ? `${reason}。` : ""}从设备重新读取巡航轨迹清单,并逐条回读各自的点位链`;
});

// 自动扫描(PTZCmd 89H 开始/左边界/右边界,8AH 速度)
//
// ⭐ 扫描与巡航是两件不同的事,别做成「巡航的另一种形态」:
//    巡航 = 一串**有序预置位** + 停留时间,循环走;扫描 = 只有**左右两个边界**,在两点之间来回。
//    ⇒ 没有点位列表可回读,也不需要「从设备同步」:边界是设备上的一组状态,由操作员把云台
//      转到目标位置后下发 `scan_set_left` / `scan_set_right` 写入,而标准里扫描**没有查询命令**,
//      平台无从得知当前值。所以这张卡片是**纯下发**,不回读、不展示"设备上的扫描配置"。
type ScanAction = "scan_start" | "scan_stop" | "scan_set_left" | "scan_set_right" | "scan_set_speed";
const SCAN_GROUP_MIN = 0;
const SCAN_GROUP_MAX = 255;
/** 与巡航速度同一个量纲:8AH 的参数域是 12 位(01H-FFFH)。 */
const SCAN_SPEED_MIN = 1;
const SCAN_SPEED_MAX = 4095;
const scanGroup = ref<number>(1);
const scanSpeed = ref<number>(60);
/** 与巡航一致:HTTP 成功只表示指令已发出,不能据此宣称设备正在扫描。 */
const scanState = ref<"stopped" | "start-sent">("stopped");
const scanBusy = ref(false);
const scanError = ref("");
const scanActiveGroup = ref<number | null>(null);
const scanGroupInvalid = computed(
  () => !Number.isInteger(scanGroup.value) || scanGroup.value < SCAN_GROUP_MIN || scanGroup.value > SCAN_GROUP_MAX
);
const scanSpeedInvalid = computed(
  () => !Number.isInteger(scanSpeed.value) || scanSpeed.value < SCAN_SPEED_MIN || scanSpeed.value > SCAN_SPEED_MAX
);
const scanCanSend = computed(() => canControlPtz.value && Boolean(props.channel) && !scanBusy.value && !scanGroupInvalid.value);

const CRUISE_RECONCILE_DELAYS_MS = [1000, 2000, 4000, 8000] as const;
let cruiseReconcileTimer: number | null = null;
let cruiseReconcileAttempt = 0;
function clearCruiseReconcilePolling() {
  if (cruiseReconcileTimer !== null) window.clearTimeout(cruiseReconcileTimer);
  cruiseReconcileTimer = null;
  cruiseReconcileAttempt = 0;
}
function scheduleCruiseReconcilePolling(channelId: number, token: number) {
  clearCruiseReconcilePolling();
  const pollNext = () => {
    if (cruiseReconcileAttempt >= CRUISE_RECONCILE_DELAYS_MS.length) return;
    const delay = CRUISE_RECONCILE_DELAYS_MS[cruiseReconcileAttempt];
    cruiseReconcileTimer = window.setTimeout(async () => {
      cruiseReconcileTimer = null;
      if (token !== sessionToken || props.channel?.id !== channelId) return;
      await loadCruises(channelId, token, false);
      if (token !== sessionToken || props.channel?.id !== channelId) return;
      cruiseReconcileAttempt += 1;
      pollNext();
    }, delay);
  };
  pollNext();
}

/* ────────────────── 设备资源回读(「从设备同步」) ──────────────────
 *
 * 预置位与巡航这两张卡片画的都是**设备侧的资源**,平台库里存的是镜像。在此之前
 * 整个前端**没有任何地方发起过回读**:`listPtzPresets(channelId)` 与
 * `loadCruises(..., false)` 的 refresh 参数一律是 false,于是设备上早就存在的预置位
 * 和巡航轨迹在界面上永远不出现,而巡航卡片头顶那行小字还在一直说「缓存数据已过期」
 * —— 提示了一个问题,却不给任何可点的地方去解决它。
 *
 * ⛔ 回读是**两段式**的,不能只看那一个 HTTP 返回。
 *
 * `?refresh=true` 的语义是「把查询发给设备」,它的 HTTP 返回里带的是**查询之前**的
 * 缓存行 + 一个 `refreshOperationId`。设备应答是异步的 —— 它要走 SIP 命令的重试
 * 与截止时间。所以必须拿这个 id 去轮询 PTZ 操作直到终止态,再重读列表;只发不等的话,
 * 设备稍微慢一点界面就什么都不变,操作员会认为按钮坏了,然后反复点。
 */

const RESOURCE_SYNC_POLL_INTERVAL_MS = 250;
/** 等设备应答的总上限。SIP 命令超时(默认 5s)加三次重试的重试间隔之外再留余量。 */
const RESOURCE_SYNC_TIMEOUT_MS = 15000;
/** 连续几次轮询都失败就放弃等 —— 读取操作状态的接口被拒(如只读账号)时,
 *  不能把操作员按在一个 15 秒的转圈上。 */
const RESOURCE_SYNC_MAX_POLL_FAILURES = 3;
/** 一次同步最多补几条巡航轨迹的点位链。每条要一条独立的 SIP 查询
 *  (`CruiseTrackQuery`),清单查询的应答里**没有**点位集合,只能逐条问。 */
const CRUISE_DETAIL_SYNC_MAX = 9;

/** PTZ 操作的终止态。`unknown` **不算**终止 —— 它表示"还没拿到设备的最终说法",
 *  仍可能在截止前收到应答(见服务端 `queryOperationCanStillRespond`)。 */
const PTZ_TERMINAL_STATUSES: ReadonlySet<string> = new Set(["accepted", "rejected", "timeout", "cancelled"]);

function waitResourceSyncTick(ms: number) {
  return new Promise<void>(resolve => {
    window.setTimeout(resolve, ms);
  });
}

/** 等一次查询操作落地。只区分三件事:拿到设备应答(`settled`)、没拿到(`timeout`)、
 *  上下文已经换了(`stale` —— 切了通道或关了面板,调用方必须直接放弃)。
 *
 *  具体错误码对操作员没有意义:他没有办法因为 `rejected` 和 `timeout` 做不同的事,
 *  两种情况的下一步都是"再点一次同步"。 */
async function waitPTZOperationSettled(
  channelId: number,
  token: number,
  operationId: string
): Promise<"settled" | "timeout" | "stale"> {
  const deadline = Date.now() + RESOURCE_SYNC_TIMEOUT_MS;
  let failures = 0;
  for (;;) {
    if (token !== sessionToken || props.channel?.id !== channelId) return "stale";
    try {
      const response = await getPtzOperation(channelId, operationId);
      failures = 0;
      const status = String(response.data?.status ?? "");
      if (PTZ_TERMINAL_STATUSES.has(status)) {
        return status === "accepted" ? "settled" : "timeout";
      }
    } catch {
      failures += 1;
      if (failures >= RESOURCE_SYNC_MAX_POLL_FAILURES) return "timeout";
    }
    if (Date.now() >= deadline) return "timeout";
    await waitResourceSyncTick(RESOURCE_SYNC_POLL_INTERVAL_MS);
  }
}

/** 下拉一次预置位:发查询 → 等设备应答 → 重读列表。
 *
 *  ⛔ 顺序不能省。少了"等应答"这一步,重读拿到的还是查询前的缓存,按钮看起来
 *  什么都没干;而预置位的列表查询会比对设备实际报回的编号,把没报的标记为已删除,
 *  所以读早了还会看到一份短暂错误的清单。 */
async function syncPresets() {
  const channelId = props.channel?.id;
  const token = sessionToken;
  if (!canViewPtz.value || !channelId || presetSyncing.value) return;
  presetSyncing.value = true;
  presetSyncError.value = "";
  try {
    const response = await listPtzPresets(channelId, true);
    if (token !== sessionToken || props.channel?.id !== channelId) return;
    if (response.code !== 0 || !response.data) {
      presetSyncError.value = response.message || "同步预置位失败,请重试";
      return;
    }
    if (response.data.refreshError) presetSyncError.value = response.data.refreshError;
    const outcome = response.data.refreshOperationId
      ? await waitPTZOperationSettled(channelId, token, response.data.refreshOperationId)
      : "settled";
    if (outcome === "stale") return;
    if (outcome === "timeout" && !presetSyncError.value) {
      // 设备对 `PresetQuery` 不答,绝大多数情况是它根本没走到这一帧:
      // SIP 已经不可达,或者设备不认识这个命令。都不该说成"同步失败"就完事。
      presetSyncError.value = "设备未应答";
    }
    await loadPresets(channelId, token, false);
  } catch {
    if (token === sessionToken && props.channel?.id === channelId) {
      presetSyncError.value = "同步预置位失败,请重试";
    }
  } finally {
    if (token === sessionToken) presetSyncing.value = false;
  }
}

/** 从设备回读某一条巡航轨迹的点位链。返回这轮查询的 operationId(拿不到就是空串)。 */
async function requestCruiseDetail(channelId: number, trackId: number): Promise<string> {
  try {
    const response = await getCruiseTrack(channelId, trackId, true);
    if (response.code !== 0 || !response.data) return "";
    return response.data.refreshOperationId || "";
  } catch {
    return "";
  }
}

/** 下拉巡航轨迹:清单查询 → 等应答 → 重读 → 给点位未知的轨迹逐条补详情。
 *
 *  ⛔ 第 4 步不是可选优化。`CruiseTrackListQuery` 的设备应答里只有 `<Number/>` 和
 *  `<Name/>`(标准 A.2.6.13),**没有点位集合**;要是止步于重读列表,设备侧发现的
 *  轨迹会永远停在「点位待查询」,操作员看得到轨迹名却看不到它串了哪几个预置位 ——
 *  而"串了哪几个预置位"正是他点同步最想确认的事。
 *
 *  ⛔ 详情查询并发下发,不串行。每条都要走一轮 SIP 往返,串行 9 条最坏能拖到两分钟,
 *  操作员会以为卡死了。它们在设备侧是互相独立的分组,没有顺序依赖。 */
async function syncCruises() {
  const channelId = props.channel?.id;
  const token = sessionToken;
  if (!canViewPtz.value || !channelId || cruiseSyncing.value) return;
  cruiseSyncing.value = true;
  cruiseLoadError.value = "";
  cruiseRefreshError.value = "";
  try {
    const listed = await listCruiseTracks(channelId, true);
    if (token !== sessionToken || props.channel?.id !== channelId) return;
    if (listed.code !== 0 || !listed.data) {
      cruiseLoadError.value = listed.message || "加载巡航轨迹失败,请重试";
      return;
    }
    // ⛔ 结论要**留到最后再写**。中间的每一次重读(`loadCruises(..., false)`)都会把
    //    `cruiseRefreshError` 重置成响应里的空值;先写就等于被自己抹掉,
    //    操作员看到「同步中」闪一下又回到「已同步」,设备没答这件事整个消失。
    let failure = listed.data.refreshError || "";
    const outcome = listed.data.refreshOperationId
      ? await waitPTZOperationSettled(channelId, token, listed.data.refreshOperationId)
      : "settled";
    if (outcome === "stale") return;
    if (outcome === "timeout" && !failure) failure = "设备未应答";

    // 重读:设备侧新发现的轨迹到这一步才会出现(清单查询会把它们插进平台库)。
    await loadCruises(channelId, token, false);
    if (token !== sessionToken || props.channel?.id !== channelId) return;

    const needDetail = cruiseTracks.value.filter(track => track.points === null).slice(0, CRUISE_DETAIL_SYNC_MAX);
    if (needDetail.length > 0) {
      const operationIds = (await Promise.all(needDetail.map(track => requestCruiseDetail(channelId, track.id)))).filter(
        operationId => operationId !== ""
      );
      if (token !== sessionToken || props.channel?.id !== channelId) return;
      const detailOutcomes = await Promise.all(
        operationIds.map(operationId => waitPTZOperationSettled(channelId, token, operationId))
      );
      if (token !== sessionToken || props.channel?.id !== channelId) return;
      await loadCruises(channelId, token, false);
      if (token !== sessionToken || props.channel?.id !== channelId) return;
      if (!failure && detailOutcomes.every(item => item === "timeout")) {
        failure = "设备未回读点位";
      }
    }
    cruiseRefreshError.value = failure;
  } catch {
    if (token === sessionToken && props.channel?.id === channelId) {
      cruiseLoadError.value = "加载巡航轨迹失败,请重试";
    }
  } finally {
    if (token === sessionToken) cruiseSyncing.value = false;
  }
}
// 巡航卡片布局 1:1 复刻预置位 —— 3 列 × 3 行 = 9 格,溢出保留 8 条 tile + 1 「更多」chip
const CRUISE_GRID_SLOTS = 9;
const RESOURCE_TOOLTIP_ENTER_DELAY_MS = 80;
const maxVisibleCruiseTracks = CRUISE_GRID_SLOTS;
const hasMoreCruises = computed(() => cruiseTracks.value.length > maxVisibleCruiseTracks);
const visibleCruiseTracks = computed(() =>
  hasMoreCruises.value ? cruiseTracks.value.slice(0, maxVisibleCruiseTracks - 1) : cruiseTracks.value
);
const cruiseMoreVisible = ref(false);
function cruiseTileState(c: { id: number; enabled: boolean; pending: boolean }): "stop" | "idle" | "disabled" | "pending" {
  if (activeCruiseId.value === c.id && cruiseState.value === "start-sent") return "stop";
  if (c.pending) return "pending";
  if (!c.enabled) return "disabled";
  return "idle";
}
/** 悬浮提示 = 「这条轨迹是什么」+「现在点一下会发生什么」。
 *
 *  带点位链是刻意的:tile 只有一格宽(2 列网格 + 单行省略),放不下第二个信息行,
 *  而悬浮提示是操作员唯一能核对「设备上这条轨迹的点位顺序对不对」的地方。
 *
 *  文案刻意不写标准附录号(原来的「尚未返回 GB/T 28181-2022 巡航轨迹查询结果」)——
 *  操作员不读 A.2.6.13,他只想知道现在能不能点、点了会发生什么。
 */
function cruiseTileTitle(c: CruiseTrack): string {
  const state = cruiseTileState(c);
  const head = `#${c.id} ${c.name} · ${cruiseTrackDetailText(c)}`;
  if (state === "pending") return `${head}；配置已下发,设备还没回传轨迹内容。点一下可以试运行验证`;
  if (state === "disabled") return `${head}；设备报告这条轨迹已停用`;
  if (state === "stop") return `${head}；启动指令已下发,点一下停止`;
  return `${head}；点一下开始巡航`;
}

function parseCruiseDetail(raw: string | CruiseTrackDetailResource | null | undefined): {
  points: CruisePoint[] | null;
  source: string;
} {
  let detail: CruiseTrackDetailResource | null = null;
  if (typeof raw === "string" && raw.trim()) {
    try {
      detail = JSON.parse(raw) as CruiseTrackDetailResource;
    } catch {
      detail = null;
    }
  } else if (raw && typeof raw === "object") {
    detail = raw;
  }
  if (!detail) return { points: null, source: "" };

  const rawPoints = Array.isArray(detail.cruisePoints) ? detail.cruisePoints : Array.isArray(detail.stops) ? detail.stops : null;
  if (!rawPoints) return { points: null, source: detail.source || "" };

  // ⛔ 上限是 **4095**,不是 15。写侧允许 1-4095(`cruiseDraftError`、`a-input-number`
  //    的 :max),平台解析侧也按 1-4095 收(`manscdp.ParseCruiseTrackResponse`)——
  //    只有这里卡在 1-15。后果不是"少显示一个数":设备如实回显 128 时这里返回 null,
  //    界面上变成「速度未知」,看着像设备没回话,实际是前端把合法值丢了。
  const normalizeQuerySpeed = (value: unknown): number | null => {
    const speed = Number(value);
    return Number.isInteger(speed) && speed >= 1 && speed <= 4095 ? speed : null;
  };
  const globalSpeed = normalizeQuerySpeed(detail.speed);
  const globalDwell = Number.isFinite(Number(detail.dwellSec)) ? Number(detail.dwellSec) : null;
  const points = rawPoints.flatMap((item: CruiseTrackPointResource) => {
    const presetId = Number(item.presetIndex ?? item.presetId);
    if (!Number.isInteger(presetId) || presetId < 1 || presetId > 255) return [];
    const speed = normalizeQuerySpeed(item.speed) ?? globalSpeed;
    const dwell = Number.isFinite(Number(item.stayTime ?? item.dwellSec)) ? Number(item.stayTime ?? item.dwellSec) : globalDwell;
    return [{ presetId, speed, dwellSec: dwell }];
  });
  return { points, source: detail.source || "" };
}

/** 点位链最长显示几个预置位编号。32 个点的全链会占满一整行还把后面的
 *  「每点停留 / 速度」挤掉,而头几个点已足够认出这条轨迹是不是自己配的那条。
 *  截断时补「等 N 个」把真实数量交代清楚,不让人误以为只有 6 个点。 */
const CRUISE_CHAIN_HEAD = 6;

/** 把「巡航轨迹」的核心信息拼成一行:**点位链 + 顺序 + 单位齐全的参数**。
 *
 *  ⛔ 这里必须出现**点位链**。巡航轨迹的语义就是「按顺序走一串预置位」
 *  (见技能 uvp-gb28181-ptz-linkage),只报「3 个点位」等于把这条轨迹最可识别的
 *  信息丢了 —— 操作员无法据此判断设备上跑的到底是不是自己排的那条顺序。
 *  模拟器侧的设备 HUD 用同一种写法(`#1 ▶ 1→3→5`),两边看起来是一回事。
 *
 *  ⛔ 单位必须写出来:`0x86`/`0x87` 的参数是 12 位裸整数(1-4095),`128` 单独放着
 *  没人知道是档位还是百分比;停留时间也只有「秒」一种解释。同款坑平台侧修过
 *  (看守位的「空闲」字段),这里保持一致。
 */
function cruiseTrackDetailText(track: CruiseTrack): string {
  if (track.points === null) return "点位待查询";
  if (track.points.length === 0) return "设备上还没有点位";
  const ids = track.points.map(point => point.presetId);
  // 单点不画箭头链(「预置位 3」比「预置位 3→」好读);多点才需要让顺序显形。
  const chain =
    ids.length === 1
      ? `${ids[0]}`
      : ids.length <= CRUISE_CHAIN_HEAD
        ? ids.join("→")
        : `${ids.slice(0, CRUISE_CHAIN_HEAD).join("→")} 等 ${ids.length} 个`;
  const dwellValues = [...new Set(track.points.map(point => point.dwellSec).filter((value): value is number => value !== null))];
  const speedValues = [...new Set(track.points.map(point => point.speed).filter((value): value is number => value !== null))];
  const dwellText =
    dwellValues.length === 1 ? `每点停留 ${dwellValues[0]} 秒` : dwellValues.length > 1 ? "停留时间按点不同" : "停留时间未上报";
  const speedText = speedValues.length === 1 ? `速度 ${speedValues[0]}` : speedValues.length > 1 ? "速度按点不同" : "速度未上报";
  return `预置位 ${chain} · ${dwellText} · ${speedText}`;
}

function cruiseTrackMeta(track: CruiseTrack): string {
  const detail = cruiseTrackDetailText(track);
  return track.pending ? `${detail} · 已下发,未验证` : detail;
}

// 建立巡航 modal:trackId 使用国标 0-255，stops 按顺序引用已有预置位。
type CruiseDraftStop = { key: string; presetId: number };
type CruiseDraft = {
  trackId: number;
  name: string;
  speed: number;
  dwellSec: number;
  sendSpeed: boolean;
  sendDwell: boolean;
  stops: CruiseDraftStop[];
  replaceExisting: boolean;
  submitting: boolean;
  idempotencyKey: string;
};
const saveCruiseDialogVisible = ref(false);
const cruiseDraft = ref<CruiseDraft | null>(null);
const cruiseDraftTouched = ref(false);
const cruiseDraftSubmitError = ref("");
const cruiseStopsListEl = ref<HTMLElement | null>(null);
function nextCruiseTrackId(): number {
  if (cruiseTracks.value.length === 0) return 1;
  const used = new Set(cruiseTracks.value.map(track => track.id));
  const afterMax = Math.max(...used) + 1;
  if (afterMax <= 255) return afterMax;
  for (let id = 0; id <= 255; id++) {
    if (!used.has(id)) return id;
  }
  return 0;
}
function newCruiseStop(presetId: number): CruiseDraftStop {
  return { key: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`, presetId };
}
function newCruiseIdempotencyKey(): string {
  return `cruise-create-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}
const cruiseDraftError = computed(() => {
  if (!cruiseDraftTouched.value || !cruiseDraft.value) return "";
  const draft = cruiseDraft.value;
  if (!Number.isInteger(draft.trackId) || draft.trackId < 0 || draft.trackId > 255) return "巡航编号必须在 0-255 之间";
  if (!draft.replaceExisting && cruiseTracks.value.some(c => c.id === draft.trackId))
    return `巡航编号 #${draft.trackId} 已存在,请换一个或勾选清空重建`;
  if (draft.name.length > 32) return "名称最多 32 个字符";
  // 用词统一成「巡航点」:模态里原本「站点」「巡航点」混着说
  // (标签写「站点」、按钮写「添加巡航点」、提示写「巡航点」),同一样东西三个叫法。
  if (draft.stops.length === 0) return "至少选择 1 个巡航点";
  if (draft.stops.length > 32) return "巡航点最多 32 个";
  if (draft.stops.some(s => !presets.value.some(p => p.id === s.presetId))) return "选中的预置位已不存在,请重新选择";
  if (draft.sendSpeed && (!Number.isInteger(draft.speed) || draft.speed < 1 || draft.speed > 4095))
    return "速度必须在 1-4095 之间";
  if (draft.sendDwell && (!Number.isInteger(draft.dwellSec) || draft.dwellSec < 1 || draft.dwellSec > 4095))
    return "停留时间必须在 1-4095 秒之间";
  return "";
});
function openSaveCruiseDialog() {
  if (!canControlPtzCruise.value || !props.channel || presets.value.length === 0) return;
  const firstPreset = presets.value[0].id;
  cruiseDraft.value = {
    trackId: nextCruiseTrackId(),
    name: `巡航 ${nextCruiseTrackId()}`,
    speed: 128,
    dwellSec: 5,
    sendSpeed: true,
    sendDwell: true,
    stops: [newCruiseStop(firstPreset)],
    replaceExisting: false,
    submitting: false,
    idempotencyKey: newCruiseIdempotencyKey()
  };
  cruiseDraftTouched.value = false;
  cruiseDraftSubmitError.value = "";
  saveCruiseDialogVisible.value = true;
}
function closeSaveCruiseDialog() {
  if (cruiseDraft.value?.submitting) return;
  saveCruiseDialogVisible.value = false;
  cruiseDraft.value = null;
  cruiseDraftTouched.value = false;
  cruiseDraftSubmitError.value = "";
}
function canCloseSaveCruiseDialog() {
  return !cruiseDraft.value?.submitting;
}
async function addCruiseStop() {
  if (!cruiseDraft.value || cruiseDraft.value.submitting || cruiseDraft.value.stops.length >= 32) return;
  const usedPresetIds = new Set(cruiseDraft.value.stops.map(stop => stop.presetId));
  const fallback = presets.value.find(preset => !usedPresetIds.has(preset.id))?.id ?? presets.value[0]?.id;
  if (!fallback) return;
  cruiseDraft.value.stops.push(newCruiseStop(fallback));
  await nextTick();
  if (cruiseStopsListEl.value) cruiseStopsListEl.value.scrollTop = cruiseStopsListEl.value.scrollHeight;
}
function removeCruiseStop(index: number) {
  if (!cruiseDraft.value || cruiseDraft.value.submitting) return;
  cruiseDraft.value.stops.splice(index, 1);
}
function moveCruiseStop(index: number, delta: number) {
  if (!cruiseDraft.value || cruiseDraft.value.submitting) return;
  const stops = cruiseDraft.value.stops;
  const target = index + delta;
  if (target < 0 || target >= stops.length) return;
  const [item] = stops.splice(index, 1);
  stops.splice(target, 0, item);
}
async function handleSaveCruiseBeforeOk(done: (closable?: boolean) => void) {
  if (!canControlPtzCruise.value || !cruiseDraft.value || !props.channel) {
    done(false);
    return;
  }
  if (cruiseDraft.value.submitting) {
    done(false);
    return;
  }
  cruiseDraftTouched.value = true;
  if (cruiseDraftError.value) {
    done(false);
    return;
  }
  const draft = cruiseDraft.value;
  const channelId = props.channel.id;
  const token = sessionToken;
  draft.submitting = true;
  cruiseDraftSubmitError.value = "";
  try {
    const response = await createCruiseTrack(channelId, {
      trackId: draft.trackId,
      name: draft.name.trim(),
      speed: draft.sendSpeed ? draft.speed : 0,
      dwellSec: draft.sendDwell ? draft.dwellSec : 0,
      stops: draft.stops.map(s => ({ presetId: s.presetId })),
      replaceExisting: draft.replaceExisting,
      idempotencyKey: draft.idempotencyKey
    });
    if (token !== sessionToken || props.channel?.id !== channelId || cruiseDraft.value !== draft) {
      done(false);
      return;
    }
    if (response.code === 0) {
      Message.info(
        `巡航 #${draft.trackId} 配置已下发,等设备回传确认(${response.data?.completedStops ?? 0}/${response.data?.totalStops ?? 0} 个巡航点)`
      );
      await loadCruises(channelId, token, false);
      if (token !== sessionToken || props.channel?.id !== channelId || cruiseDraft.value !== draft) {
        done(false);
        return;
      }
      scheduleCruiseReconcilePolling(channelId, token);
      done(true);
      cruiseDraft.value = null;
      cruiseDraftTouched.value = false;
      cruiseDraftSubmitError.value = "";
      return;
    }
    const data = response.data;
    const completed = data?.completedStops ?? 0;
    const total = data?.totalStops ?? draft.stops.length;
    cruiseDraftSubmitError.value = `已有 ${completed}/${total} 个巡航点下发成功,请关闭后刷新设备状态再重建:${response.message || data?.error || "未知原因"}`;
    Message.warning(`巡航 #${draft.trackId} 只下发了 ${completed}/${total} 个巡航点,请先核对设备状态`);
    scheduleCruiseReconcilePolling(channelId, token);
    done(false);
  } catch (error: any) {
    if (token === sessionToken && props.channel?.id === channelId && cruiseDraft.value === draft) {
      cruiseDraftSubmitError.value = error?.message || "建立巡航失败";
      Message.error(cruiseDraftSubmitError.value);
    }
    done(false);
  } finally {
    if (cruiseDraft.value === draft) draft.submitting = false;
  }
}

type AssetManagerTab = "preset" | "cruise";
const assetManagerVisible = ref(false);
const assetManagerTab = ref<AssetManagerTab>("preset");
const assetSearch = ref("");
const filteredPresets = computed(() => {
  const keyword = assetSearch.value.trim().toLowerCase();
  if (!keyword) return presets.value;
  return presets.value.filter(item => item.name.toLowerCase().includes(keyword) || String(item.id).includes(keyword));
});
const filteredCruiseTracks = computed(() => {
  const keyword = assetSearch.value.trim().toLowerCase();
  if (!keyword) return cruiseTracks.value;
  return cruiseTracks.value.filter(item => item.name.toLowerCase().includes(keyword) || String(item.id).includes(keyword));
});

// 看守位(2022 HomePositionQuery):设备确认值与用户草稿必须独立。
type HomePositionPhase = "unknown" | "loading" | "pending" | "accepted" | "enabled" | "disabled" | "error";
type HomePositionPending = { kind: "control" | "refresh"; operationId: string | null; deadlineAt: string | null };
type HomePositionDraft = { enabled: boolean; presetId: number | null; resetTime: number | null };
type HomePositionPresentationState =
  | "unknown"
  | "loading"
  | "pending"
  | "unsupported"
  | "unconfigured"
  | "enabled"
  | "disabled"
  | "error"
  | "offline";

/** 设备返回的看守位配置 → 编辑草稿。
 *
 * ⛔ `presetId` 的 **0 必须归一成 `null`**。设备从未被配置过看守位时,标准没有定义
 * 「查不到」的应答形态,模拟器统一回 `Enabled=0 / ResetTime=0 / PresetIndex=0`,
 * 所以 0 在 wire 上是「尚未配置」的占位值,而不是「0 号预置位」。而 0 号在平台里
 * **永远不存在**:创建接口强制 `presetId > 0`(`controllers/device_ptz_resources.go`),
 * 读列表时前端也 `.filter(id > 0)`(`loadPresets`)。若把 0 原样塞进 draft,
 * 下拉里就会出现一个指向空气的选项、还能被原样提交回设备 —— 2026-09-17 用户上报的
 * 「#0 · 标准预置位 0」就是这么来的。
 * 非 0 的未知编号**保留**:那是设备真有的点位,由模板如实标注成「设备侧预置位」,
 * 不能替操作员抹掉。 */
function homeDraftFrom(config: HomePositionConfig | null): HomePositionDraft {
  if (!config) return { enabled: false, presetId: null, resetTime: 300 };
  const presetId =
    typeof config.presetId === "number" && Number.isInteger(config.presetId) && config.presetId > 0 ? config.presetId : null;
  return { enabled: config.enabled, presetId, resetTime: config.resetTime };
}

const unknownHomeSupport = (): HomePositionSupport => ({ status: "unknown", reason: "能力尚未确认" });
const homeConfirmed = ref<HomePositionConfig | null>(null);
const homeDraft = ref<HomePositionDraft>({ enabled: false, presetId: null, resetTime: 300 });
const homePhase = ref<HomePositionPhase>("unknown");
const homeControlSupport = ref<HomePositionSupport>(unknownHomeSupport());
const homeQuerySupport = ref<HomePositionSupport>(unknownHomeSupport());
const homePending = ref<HomePositionPending | null>(null);
const homeOperationId = ref<string | null>(null);
const homeError = ref("");
const homeMismatch = ref("");
const homeSettingsDialogVisible = ref(false);
const homeSettingsTouched = ref(false);
const homeSettingsSubmitting = ref(false);

const homePositionCanSave = computed(() => {
  if (!homeDraft.value.enabled) return true;
  const { presetId, resetTime } = homeDraft.value;
  // 看守位的语义是「空闲 ResetTime 秒后回到 PresetIndex 指向的那个预置位」——
  // 没有预置位就等于没有归位目标,下发一个平台和设备都不存在的编号毫无意义。
  // 判定以**平台预置位列表**为准:`presets` 在 loadPresets 里已经过滤掉
  // `id <= 0`,平台创建接口也强制 `presetId > 0`,所以 0 号在这里天然不合格。
  // ⛔ 只挡「启用」。查询与关闭各自有自己的闸门(homeCanRefresh / `!enabled` 时
  //    本函数直接放行),设备端本来就开着看守位时操作员必须还能查询和关掉它。
  if (!Number.isInteger(presetId) || !presets.value.some(p => p.id === presetId)) return false;
  return Number.isInteger(resetTime) && Number(resetTime) >= 10 && Number(resetTime) <= 3600;
});
// 三个闸门(homeCanConfigure / homeCanRefresh / homeCanClose)都自带
// `homePending.value === null`,模态内部另有 homeSettingsSubmitting —— 原来那个
// `homeControlPending` 只挡这三处,已经全被覆盖,留着就是死代码(vue-tsc 会报未使用)。
const homeCanSubmit = computed(() => props.channel?.status === 1 && homePending.value === null && homePositionCanSave.value);
const homeCanRefresh = computed(() => props.channel?.status === 1 && homePending.value === null);
const homeCanConfigure = computed(
  () =>
    canUpdatePtzHome.value &&
    props.channel?.status === 1 &&
    homePending.value === null &&
    homeControlSupport.value.status !== "unsupported" &&
    presets.value.length > 0
);
const homeCanClose = computed(
  () =>
    canUpdatePtzHome.value && props.channel?.status === 1 && homePending.value === null && homeConfirmed.value?.enabled === true
);
const homeIsBusy = computed(() => homePhase.value === "loading" || homePending.value !== null);
const homeConfirmedValuesText = computed(() => {
  const confirmed = homeConfirmed.value;
  if (!confirmed?.enabled) return "";
  const preset =
    Number.isInteger(confirmed.presetId) && Number(confirmed.presetId) > 0 ? `归位到 #${confirmed.presetId}` : "归位位置未配置";
  const resetTime =
    Number.isInteger(confirmed.resetTime) && Number(confirmed.resetTime) > 0
      ? `无操作 ${confirmed.resetTime} 秒后归位`
      : "等待时间未配置";
  return `${preset} · ${resetTime}`;
});
const homeConfirmedAtText = computed(() => {
  const value = homeConfirmed.value?.confirmedAt;
  if (!value) return "";
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) return "";
  return `设备确认于 ${new Date(timestamp).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false })}`;
});
const homeConfirmedOutsideEditableRange = computed(() => {
  const confirmed = homeConfirmed.value;
  if (!confirmed?.enabled) return false;
  return (
    confirmed.presetId == null ||
    !Number.isInteger(confirmed.presetId) ||
    confirmed.presetId <= 0 ||
    confirmed.presetId > 255 ||
    confirmed.resetTime == null ||
    !Number.isInteger(confirmed.resetTime) ||
    confirmed.resetTime < 10 ||
    confirmed.resetTime > 3600
  );
});
const homePresentationState = computed<HomePositionPresentationState>(() => {
  if (props.channel?.status !== 1) return "offline";
  if (homePhase.value === "loading") return "loading";
  if (homePending.value) return "pending";
  if (homeControlSupport.value.status === "unsupported" && homeQuerySupport.value.status === "unsupported") return "unsupported";
  if (homePhase.value === "error" || homePhase.value === "accepted" || homeError.value) return "error";
  if (homeConfirmed.value) return homeConfirmed.value.enabled ? "enabled" : "disabled";
  if (homeControlSupport.value.status === "supported" || homeQuerySupport.value.status === "supported") return "unconfigured";
  return "unknown";
});
const homePresentation = computed(() => {
  const state = homePresentationState.value;
  const copy = {
    unknown: { label: "尚未确认设备能力", description: "查询后可确认设备是否支持及当前配置", tone: "neutral" },
    loading: { label: "正在读取设备状态…", description: "", tone: "loading" },
    pending: {
      label: homePending.value?.kind === "refresh" ? "正在查询设备…" : "等待设备确认…",
      description: "",
      tone: "loading"
    },
    unsupported: { label: "设备不支持看守位", description: "", tone: "muted" },
    unconfigured: {
      label: homeControlSupport.value.status === "supported" ? "支持看守位，尚未配置" : "尚未配置看守位",
      description:
        homeControlSupport.value.status === "supported" ? "可设置回位预置位和空闲时间" : "设备可查询状态，暂不支持配置",
      tone: "neutral"
    },
    enabled: { label: "已启用", description: "", tone: "success" },
    disabled: { label: "已关闭", description: "", tone: "muted" },
    error: { label: "当前状态未确认", description: "", tone: "danger" },
    offline: { label: "设备离线", description: "连接恢复后可继续操作", tone: "muted" }
  } as const;
  return {
    state,
    ...copy[state],
    showQuery: state !== "unsupported" && state !== "loading" && state !== "offline",
    queryLabel: state === "error" ? "重试" : state === "pending" ? "查询中" : "查询设备",
    // 控制能力明确不支持时**不渲染**「配置」按钮。
    //
    // 只靠 `:disabled="!homeCanConfigure"` 拦不住观感:homeCanConfigure 已经把
    // `controlSupport === "unsupported"` 算进去了,按钮会以主子色渲染出来再被禁灰,
    // 操作员看到的是一个点不动、也没有理由的按钮。而 homePresentationState 在
    // 「控制不支持 + 查询未知 + 有一份已确认配置」这种**混合能力**下会落到
    // enabled/disabled,那条分支的 description 是空的,连解释都没有。
    // 直接不渲染,理由交给 home-diagnostics 的悬浮说明和状态行去讲。
    showConfigure: homeControlSupport.value.status !== "unsupported"
  };
});
const homeLastConfirmedText = computed(() => {
  if (!homeConfirmed.value || !["offline", "error"].includes(homePresentationState.value)) return "";
  if (!homeConfirmed.value.enabled) return "上次确认：已关闭";
  return `上次确认：已启用 · ${homeConfirmedValuesText.value}`;
});
const homeNoticeText = computed(() => {
  if (homeMismatch.value) return homeMismatch.value;
  if (!homeError.value) return "";
  const normalized = homeError.value.toUpperCase();
  if (normalized.includes("TIMEOUT") || normalized.includes("DEADLINE") || normalized.includes("超时")) {
    return "查询设备超时，请重试";
  }
  return homeConfirmed.value ? "设备状态可能已变化，请重新查询" : "暂时无法确认设备状态，请重试";
});
const homeSettingsActionLabel = computed(() => (homeConfirmed.value?.enabled ? "保存修改" : "启用看守位"));
const homeDiagnosticsTitle = computed(() => {
  const supportText = (support: HomePositionSupport) =>
    ({
      supported: "支持",
      unsupported: "不支持",
      unknown: "尚未确认"
    })[support.status];
  const lines = [
    `控制能力：${supportText(homeControlSupport.value)}${homeControlSupport.value.reason ? `（${homeControlSupport.value.reason}）` : ""}`,
    `查询能力：${supportText(homeQuerySupport.value)}${homeQuerySupport.value.reason ? `（${homeQuerySupport.value.reason}）` : ""}`
  ];
  if (homeOperationId.value) lines.push(`操作标识：${homeOperationId.value}`);
  if (homeError.value) lines.push(`技术信息：${homeError.value}`);
  return lines.join("\n");
});

function resetHomePositionState() {
  clearHomePositionPolling(true);
  homeConfirmed.value = null;
  homeDraft.value = { enabled: false, presetId: null, resetTime: 300 };
  homePhase.value = "unknown";
  homeControlSupport.value = unknownHomeSupport();
  homeQuerySupport.value = unknownHomeSupport();
  homePending.value = null;
  homeOperationId.value = null;
  homeError.value = "";
  homeMismatch.value = "";
  homeSettingsDialogVisible.value = false;
  homeSettingsTouched.value = false;
  homeSettingsSubmitting.value = false;
}

function applyHomePositionResult(result: HomePositionResult) {
  homeConfirmed.value = result.homePosition ? { ...result.homePosition } : null;
  homeDraft.value = homeDraftFrom(result.homePosition);
  homeControlSupport.value = result.controlSupport;
  homeQuerySupport.value = result.querySupport;
  homeError.value = "";
  homeMismatch.value = "";

  if (result.control.status === "pending" && result.control.operationId) {
    homePending.value = { kind: "control", operationId: result.control.operationId, deadlineAt: result.control.deadlineAt };
  } else if (result.refresh.status === "pending" && result.refresh.operationId) {
    homePending.value = { kind: "refresh", operationId: result.refresh.operationId, deadlineAt: result.refresh.deadlineAt };
  } else {
    homePending.value = null;
  }

  homeOperationId.value = homePending.value?.operationId ?? result.control.operationId ?? result.refresh.operationId;

  const terminalError = result.control.errorCode ?? result.refresh.errorCode;
  if (terminalError === "HOME_POSITION_RECONCILE_MISMATCH") {
    homeMismatch.value = "设备未按请求应用配置";
  } else if (terminalError) {
    homeError.value = terminalError;
  }

  if (homePending.value) {
    homePhase.value = "pending";
  } else if (result.control.status === "unknown") {
    homePhase.value = "unknown";
  } else if (["rejected", "timeout", "cancelled"].includes(result.control.status)) {
    homePhase.value = "error";
  } else if (!result.homePosition && ["failed", "timeout"].includes(result.refresh.status)) {
    homePhase.value = "error";
  } else if (result.homePosition) {
    homePhase.value = result.homePosition.enabled ? "enabled" : "disabled";
  } else {
    homePhase.value = "unknown";
  }
}

const HOME_POSITION_POLL_INTERVAL_MS = 1000;
const HOME_POSITION_INITIAL_QUEUE_MS = 5000;
const HOME_POSITION_DEADLINE_GRACE_MS = 2000;
let homePositionPollTimer: number | null = null;
let homePositionRequestDeadlineTimer: number | null = null;
let homePositionGeneration = 0;
let homePositionInsuranceDeadlineMs: number | null = null;
let homePositionDeadlineRecheckUsed = false;
let homePositionTrackedOperationId: string | null = null;
let homePositionIdempotencySequence = 0;

function clearHomePositionPolling(invalidate = false) {
  if (homePositionPollTimer !== null) window.clearTimeout(homePositionPollTimer);
  if (homePositionRequestDeadlineTimer !== null) window.clearTimeout(homePositionRequestDeadlineTimer);
  homePositionPollTimer = null;
  homePositionRequestDeadlineTimer = null;
  homePositionInsuranceDeadlineMs = null;
  homePositionDeadlineRecheckUsed = false;
  homePositionTrackedOperationId = null;
  if (invalidate) homePositionGeneration += 1;
}

function getHomePositionOperationWithDeadline(channelId: number, operationId: string) {
  const request = getPtzOperation(channelId, operationId);
  if (homePositionInsuranceDeadlineMs === null) return request;
  const remainingMs = homePositionInsuranceDeadlineMs - Date.now();
  if (remainingMs <= 0) return Promise.reject(new Error("操作状态请求超过服务端截止时间"));
  return new Promise<Awaited<ReturnType<typeof getPtzOperation>>>((resolve, reject) => {
    let settled = false;
    const deadlineTimer = window.setTimeout(() => {
      if (settled) return;
      settled = true;
      if (homePositionRequestDeadlineTimer === deadlineTimer) homePositionRequestDeadlineTimer = null;
      reject(new Error("操作状态请求超过服务端截止时间"));
    }, remainingMs);
    homePositionRequestDeadlineTimer = deadlineTimer;
    request.then(
      response => {
        if (settled) return;
        settled = true;
        window.clearTimeout(deadlineTimer);
        if (homePositionRequestDeadlineTimer === deadlineTimer) homePositionRequestDeadlineTimer = null;
        resolve(response);
      },
      error => {
        if (settled) return;
        settled = true;
        window.clearTimeout(deadlineTimer);
        if (homePositionRequestDeadlineTimer === deadlineTimer) homePositionRequestDeadlineTimer = null;
        reject(error);
      }
    );
  });
}

function beginHomePositionOperation() {
  clearHomePositionPolling(false);
  homePositionGeneration += 1;
  return homePositionGeneration;
}

function isCurrentHomePositionContext(channelId: number, token: number, generation: number) {
  return props.visible && token === sessionToken && generation === homePositionGeneration && props.channel?.id === channelId;
}

function nextHomePositionIdempotencyKey(kind: "control" | "refresh", channelId: number) {
  homePositionIdempotencySequence += 1;
  const nonce =
    globalThis.crypto?.randomUUID?.() ??
    `${Date.now()}-${homePositionIdempotencySequence}-${Math.random().toString(36).slice(2)}`;
  return `home-${kind}-${channelId}-${nonce}`;
}

function restoreHomeDraftFromConfirmed() {
  homeDraft.value = homeDraftFrom(homeConfirmed.value);
}

function prepareHomePositionOperation(operationId: string) {
  if (homePositionTrackedOperationId === operationId) return;
  homePositionTrackedOperationId = operationId;
  homePositionInsuranceDeadlineMs = null;
  homePositionDeadlineRecheckUsed = false;
}

function updateHomePositionDeadline(deadlineAt: string | null) {
  if (!deadlineAt) return false;
  const deadlineMs = Date.parse(deadlineAt);
  if (!Number.isFinite(deadlineMs) || deadlineMs <= Date.now()) return false;
  homePositionInsuranceDeadlineMs = deadlineMs + HOME_POSITION_DEADLINE_GRACE_MS;
  return true;
}

function markHomePositionUnknown(pending: HomePositionPending, message: string) {
  const operationId = pending.operationId;
  clearHomePositionPolling(false);
  homePending.value = null;
  homeOperationId.value = operationId;
  homePhase.value = "unknown";
  homeError.value = message;
  homeMismatch.value = "";
  if (pending.kind === "control") restoreHomeDraftFromConfirmed();
}

function finishHomePositionAcceptedReadFailure(pending: HomePositionPending, message: string) {
  const operationId = pending.operationId;
  clearHomePositionPolling(false);
  homePending.value = null;
  homeOperationId.value = operationId;
  homePhase.value = "accepted";
  homeError.value = `设备已确认，但确认状态读取失败，可重试${message ? `：${message}` : ""}`;
  homeMismatch.value = "";
}

function finishHomePositionFailure(pending: HomePositionPending, operation: PTZOperation) {
  clearHomePositionPolling(false);
  homePending.value = null;
  homeOperationId.value = operation.operationId;
  homeError.value = operation.errorCode || operation.errorMessage || `操作状态: ${operation.status}`;
  homeMismatch.value = "";
  if (pending.kind === "control") {
    restoreHomeDraftFromConfirmed();
    homePhase.value = "error";
    return;
  }
  homePhase.value = homeConfirmed.value ? (homeConfirmed.value.enabled ? "enabled" : "disabled") : "error";
}

function scheduleHomePositionPoll(channelId: number, token: number, generation: number) {
  if (!isCurrentHomePositionContext(channelId, token, generation)) return;
  if (homePositionPollTimer !== null) window.clearTimeout(homePositionPollTimer);
  const pending = homePending.value;
  if (!pending?.operationId) return;
  const nowMs = Date.now();
  if (homePositionInsuranceDeadlineMs !== null && nowMs >= homePositionInsuranceDeadlineMs) {
    markHomePositionUnknown(pending, "操作超过服务端截止时间，结果未知，可重试");
    return;
  }
  const delay =
    homePositionInsuranceDeadlineMs === null
      ? HOME_POSITION_POLL_INTERVAL_MS
      : Math.min(HOME_POSITION_POLL_INTERVAL_MS, homePositionInsuranceDeadlineMs - nowMs);
  homePositionPollTimer = window.setTimeout(() => {
    homePositionPollTimer = null;
    void pollHomePositionOperation(channelId, token, generation);
  }, delay);
}

async function recheckHomePositionDeadline(channelId: number, token: number, generation: number) {
  const pending = homePending.value;
  if (!pending?.operationId || !isCurrentHomePositionContext(channelId, token, generation)) return;
  if (homePositionDeadlineRecheckUsed) {
    markHomePositionUnknown(pending, "服务端未返回有效操作截止时间，结果未知，可重试");
    return;
  }
  homePositionDeadlineRecheckUsed = true;
  try {
    const response = await getHomePosition(channelId);
    if (!isCurrentHomePositionContext(channelId, token, generation)) return;
    if (response.code !== 0 || !response.data) throw new Error(response.message || "重读看守位状态失败");
    applyHomePositionResult(response.data);
    const latest = homePending.value;
    if (!latest?.operationId) {
      clearHomePositionPolling(false);
      return;
    }
    prepareHomePositionOperation(latest.operationId);
    if (!updateHomePositionDeadline(latest.deadlineAt)) {
      markHomePositionUnknown(latest, "服务端未返回有效操作截止时间，结果未知，可重试");
      return;
    }
    scheduleHomePositionPoll(channelId, token, generation);
  } catch (error: any) {
    if (!isCurrentHomePositionContext(channelId, token, generation)) return;
    const latest = homePending.value;
    if (!latest) return;
    if (homePositionInsuranceDeadlineMs !== null && Date.now() < homePositionInsuranceDeadlineMs) {
      scheduleHomePositionPoll(channelId, token, generation);
      return;
    }
    markHomePositionUnknown(latest, error?.message || "无法读取操作截止时间，结果未知，可重试");
  }
}

function resumeHomePositionPending(channelId: number, token: number, generation: number) {
  const pending = homePending.value;
  if (!pending?.operationId || !isCurrentHomePositionContext(channelId, token, generation)) {
    if (!pending) clearHomePositionPolling(false);
    return;
  }
  prepareHomePositionOperation(pending.operationId);
  if (!updateHomePositionDeadline(pending.deadlineAt)) {
    void recheckHomePositionDeadline(channelId, token, generation);
    return;
  }
  scheduleHomePositionPoll(channelId, token, generation);
}

async function refreshHomePositionAfterAccepted(channelId: number, token: number, generation: number) {
  const completedPending = homePending.value;
  if (!completedPending || !isCurrentHomePositionContext(channelId, token, generation)) return;
  try {
    const response = await getHomePosition(channelId);
    if (!isCurrentHomePositionContext(channelId, token, generation)) return;
    if (response.code !== 0 || !response.data) throw new Error(response.message || "读取确认状态失败");
    applyHomePositionResult(response.data);
    resumeHomePositionPending(channelId, token, generation);
  } catch (error: any) {
    if (!isCurrentHomePositionContext(channelId, token, generation)) return;
    if (completedPending.kind === "refresh" && homeConfirmed.value) {
      clearHomePositionPolling(false);
      homePending.value = null;
      homePhase.value = homeConfirmed.value.enabled ? "enabled" : "disabled";
      homeError.value = error?.message || "读取刷新结果失败";
      return;
    }
    if (completedPending.kind === "control") {
      finishHomePositionAcceptedReadFailure(completedPending, error?.message || "请重试");
      return;
    }
    markHomePositionUnknown(completedPending, error?.message || "设备已应答，但确认状态读取失败");
  }
}

async function pollHomePositionOperation(channelId: number, token: number, generation: number) {
  const pending = homePending.value;
  if (!pending?.operationId || !isCurrentHomePositionContext(channelId, token, generation)) return;
  if (homePositionInsuranceDeadlineMs !== null && Date.now() >= homePositionInsuranceDeadlineMs) {
    markHomePositionUnknown(pending, "操作超过服务端截止时间，结果未知，可重试");
    return;
  }
  try {
    const response = await getHomePositionOperationWithDeadline(channelId, pending.operationId);
    if (!isCurrentHomePositionContext(channelId, token, generation)) return;
    if (response.code !== 0 || !response.data) throw new Error(response.message || "查询操作状态失败");
    const operation = response.data;
    if (operation.operationId !== pending.operationId) {
      markHomePositionUnknown(pending, "服务端返回了不匹配的 operation ID");
      return;
    }
    homeOperationId.value = operation.operationId;
    homeError.value = "";
    if (operation.status === "queued" || operation.status === "sent") {
      homePending.value = { ...pending, deadlineAt: operation.deadlineAt };
      prepareHomePositionOperation(operation.operationId);
      if (!updateHomePositionDeadline(operation.deadlineAt)) {
        void recheckHomePositionDeadline(channelId, token, generation);
        return;
      }
      scheduleHomePositionPoll(channelId, token, generation);
      return;
    }
    if (operation.status === "accepted") {
      await refreshHomePositionAfterAccepted(channelId, token, generation);
      return;
    }
    if (operation.status === "unknown") {
      markHomePositionUnknown(pending, operation.errorCode || operation.errorMessage || "操作结果未知，可重试");
      return;
    }
    finishHomePositionFailure(pending, operation);
  } catch (error: any) {
    if (!isCurrentHomePositionContext(channelId, token, generation)) return;
    const latest = homePending.value;
    if (!latest) return;
    if (homePositionInsuranceDeadlineMs === null) {
      void recheckHomePositionDeadline(channelId, token, generation);
      return;
    }
    if (Date.now() >= homePositionInsuranceDeadlineMs) {
      markHomePositionUnknown(latest, "网络持续失败至服务端截止时间，结果未知，可重试");
      return;
    }
    homeError.value = error?.message || "操作状态暂时不可用，正在重试";
    scheduleHomePositionPoll(channelId, token, generation);
  }
}

/*

 * 视频参数属性(A.2.1.13 / A.2.4.7 / A.2.3.2.5)。
 *
 * ⛔ 与存储卡分开维护同一条理由(不同的协议命令、不同的节拍);但这里还多一层:
 * 写入的应答(A.2.6.8)没有任何回显,`Result=OK` 只说"收到并接受",所以本面板的
 * **权威值来自回读**,不是来自下发 —— 除了 list 还要存一份"最近一次回读的结论"。
 * 这正是左栏那句「不提供未接入的伪控制滑杆」的可执行版本。
 */
const videoParams = ref<VideoParam[]>([]);
/** 播放器下方对照条的最近一次平台快照。编辑与下发由嵌入式配置组件维护。 */
const videoParamsDraft = ref<VideoParamCodecItem[]>([]);
const videoParamRegisteredVersion = ref("");
const selectedVideoStream = ref(0);
/**
 * 码流选择的**受控出口**，喂给侧栏 `DeviceConfigDrawer` 的 `v-model:stream-profile`。
 *
 * ⛔ 真源只有 `selectedVideoStream`（number）这一个，这里只是把它翻成抽屉要的字符串 ——
 *    两个 ref 各存一份就会出现"底栏对着子码流、侧栏在改主码流"，而两边都不报错：
 *    对照卡上的数字和正在编辑的那条流根本不是同一条。
 */
const videoParamStreamProfile = computed({
  get: () => String(selectedVideoStream.value),
  set: value => {
    selectedVideoStream.value = Number(value) || 0;
  }
});
const advancedOperationPhase = ref<Record<string, AdvancedOperationPhase>>({});
const advancedOperationIds = ref<Record<string, string | null>>({});
const advancedOperationDeadline = ref<Record<string, string | null>>({});
const advancedOperationTimers = new Map<string, number>();
const advancedOperationRequestTimers = new Map<string, number>();
interface AdvancedOperationPollContext {
  operationId: string;
  deadlineMs: number | null;
}
const advancedOperationPollContexts = new Map<string, AdvancedOperationPollContext>();
const advancedStatusToken = ref(0);
const advancedPending = ref(new Set<string>());
const advancedOperationStatus = ref<Record<string, string>>({});
const dragZoomMode = ref(false);
const dragZoomAction = ref<"drag_zoom_in" | "drag_zoom_out">("drag_zoom_in");
const dragZoomStart = ref<{ x: number; y: number } | null>(null);
const dragZoomCurrent = ref<{ x: number; y: number } | null>(null);
let dragZoomPointerId: number | null = null;
const dragZoomBoxStyle = computed(() => {
  if (!dragZoomStart.value || !dragZoomCurrent.value) return {};
  const left = Math.min(dragZoomStart.value.x, dragZoomCurrent.value.x);
  const top = Math.min(dragZoomStart.value.y, dragZoomCurrent.value.y);
  const width = Math.abs(dragZoomCurrent.value.x - dragZoomStart.value.x);
  const height = Math.abs(dragZoomCurrent.value.y - dragZoomStart.value.y);
  return { left: `${left * 100}%`, top: `${top * 100}%`, width: `${width * 100}%`, height: `${height * 100}%` };
});

/* ────────────────────────── 流信息 ────────────────────────── */

const streamInfo = ref({
  streamId: "",
  ssrc: "",
  nodeId: 0,
  nodeName: "—",
  nodeHost: "—",
  videoCodec: "—",
  audioCodec: "—",
  audioSampleRate: 0,
  resolution: "—",
  videoFps: 0,
  urls: {}
});

/* ────────────────────────── 流概况 + 视频探针 ────────────────────────── */

type MonitorState = "idle" | "fresh" | "stale" | "offline";
const liveMetrics = ref<{ bitrate: number; videoLoss: number | null; audioLoss: number | null }>({
  bitrate: 0,
  videoLoss: null,
  audioLoss: null
});
const readerCount = ref(0);
const monitorSnapshot = ref<StreamMonitorSnapshot | null>(null);
const monitorState = ref<MonitorState>("idle");
/* monitorVideoTrack 曾在 stream tab 展示"视频帧数",tab 删除后失去引用;
 * monitorAudioTrack 探针概览卡音频栏"声道"仍在使用,保留。 */
const monitorAudioTrack = computed(() => monitorSnapshot.value?.tracks.find(track => track.kind === "audio"));
const totalReaderCount = computed(() => monitorSnapshot.value?.network.totalReaderCount ?? 0);

function formatBytes(value: number | undefined, suffix = "") {
  if (value == null || value < 0) return "—";
  if (value < 1024) return `${value} B${suffix}`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB${suffix}`;
  return `${(value / (1024 * 1024)).toFixed(2)} MB${suffix}`;
}

const monitorBytesSpeedText = computed(() => formatBytes(monitorSnapshot.value?.network.bytesSpeed, "/s"));
const monitorTotalBytesText = computed(() => formatBytes(monitorSnapshot.value?.network.totalBytes));

type ProbeState = "idle" | "sampling" | "complete";
const probeDurations = [
  { value: 3000, label: "3 秒" },
  { value: 10000, label: "10 秒" },
  { value: 60000, label: "60 秒" }
] as const;
const probeState = ref<ProbeState>("idle");
const probeDurationMs = ref(probeDurations[0].value);
const probeRemainingMs = ref(3000);
const probeFinishedAt = ref("");
const probeSnapshot = ref<ProbeSnapshot | null>(null);
let probeCountdownTimer: number | null = null;
let probeFinishTimer: number | null = null;
let probeToken = 0;
const probePollIntervalMs = 1000;

const probeStatusText = computed(() => {
  if (probeState.value === "sampling") return "采样中";
  if (probeState.value === "complete") return "检测完成";
  return "等待检测";
});
const probeButtonText = computed(() => {
  if (probeState.value === "sampling") return `检测中 ${(probeRemainingMs.value / 1000).toFixed(1)}s`;
  const durationLabel = probeDurations.find(item => item.value === probeDurationMs.value)?.label || "3 秒";
  if (probeState.value === "complete") return `重新检测 ${durationLabel}`;
  return `开始 ${durationLabel}检测`;
});
const probeResult = computed(() => probeSnapshot.value);

// 侧栏这一栏只有约 250px 可用宽,而 60 秒采样能到四千多帧 —— 逐帧渲染既放不下
// (每根柱子不足 0.1px)也会把 DOM 撑爆。所以这里只放按时间分桶的聚合概览,
// 逐帧明细进弹窗看。
const probeOverview = computed(() => buildProbeOverview(probeResult.value));
const probeTimelineDialogVisible = ref(false);

function openProbeTimelineDialog() {
  if (!probeOverview.value) return;
  probeTimelineDialogVisible.value = true;
}

const frameOverviewAriaLabel = computed(() => {
  const data = probeOverview.value;
  if (!data) return "帧到达概览：暂无数据";
  const tail = data.stallCount > 0 ? `${data.stallCount} 处到达断档，最长 ${Math.round(data.maxGapMs)} 毫秒` : "未发现到达断档";
  return `帧到达概览：共 ${data.totalFrames} 帧，${tail}；打开查看逐帧详情`;
});

const frameOverviewMeta = computed(() => {
  const data = probeOverview.value;
  if (!data) return probeState.value === "sampling" ? "采样中" : "无数据";
  return data.stallCount > 0 ? `${data.stallCount} 处断档` : "无断档";
});

function clearProbeTimers() {
  if (probeCountdownTimer) window.clearInterval(probeCountdownTimer);
  if (probeFinishTimer) window.clearTimeout(probeFinishTimer);
  probeCountdownTimer = null;
  probeFinishTimer = null;
}

async function startProbe() {
  if (!canDiagnosePlayback.value || phase.value !== "playing" || probeState.value === "sampling" || !playResult.value?.streamId)
    return;
  const streamId = playResult.value.streamId;
  const session = sessionToken;
  const token = ++probeToken;
  clearProbeTimers();
  probeState.value = "sampling";
  probeSnapshot.value = null;
  probeRemainingMs.value = probeDurationMs.value;
  probeCountdownTimer = window.setInterval(() => {
    probeRemainingMs.value = Math.max(0, probeRemainingMs.value - 100);
  }, 100);
  try {
    const response = await createStreamProbe(streamId, probeDurationMs.value);
    if (token !== probeToken || session !== sessionToken || playResult.value?.streamId !== streamId) return;
    if (response.code !== 0 || !response.data?.operationId) throw new Error(response.message || "视频探针任务创建失败");
    void pollProbeOperation(response.data.operationId, token, session, streamId);
  } catch (error: any) {
    if (token !== probeToken) return;
    failProbe(error?.message || "视频探针任务创建失败");
  }
}

function failProbe(message: string) {
  clearProbeTimers();
  probeRemainingMs.value = 0;
  probeState.value = "idle";
  Message.error(message);
}

async function pollProbeOperation(operationId: string, token: number, session: number, streamId: string) {
  try {
    const response = await getStreamProbeOperation(operationId);
    if (token !== probeToken || session !== sessionToken || playResult.value?.streamId !== streamId) return;
    if (response.code !== 0 || !response.data) throw new Error(response.message || "视频探针任务查询失败");
    const task = response.data;
    if (task.status === "completed") {
      if (!task.snapshot) throw new Error("视频探针任务缺少检测结果");
      clearProbeTimers();
      probeRemainingMs.value = 0;
      probeSnapshot.value = task.snapshot;
      probeState.value = "complete";
      probeFinishedAt.value = new Date(task.completedAt || task.snapshot.completedAt).toLocaleTimeString("zh-CN", {
        hour12: false
      });
      return;
    }
    if (task.status === "failed") throw new Error(task.error || "视频探针执行失败");
    if (task.status !== "queued" && task.status !== "sampling") throw new Error("视频探针任务状态异常");
    probeFinishTimer = window.setTimeout(() => {
      void pollProbeOperation(operationId, token, session, streamId);
    }, probePollIntervalMs);
  } catch (error: any) {
    if (token !== probeToken || session !== sessionToken || playResult.value?.streamId !== streamId) return;
    failProbe(error?.message || "视频探针任务查询失败");
  }
}

/* ────────────────────────── 通用工具 ────────────────────────── */

function formatDuration(ms: number) {
  const s = Math.floor(ms / 1000);
  return `${String(Math.floor(s / 60)).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}`;
}

function formatLoss(value: number | null) {
  if (value == null || value < 0) return "—";
  return `${(value * 100).toFixed(value > 0 && value < 0.01 ? 2 : 1)}%`;
}

function beginTimer() {
  if (timer) return;
  timer = window.setInterval(() => {
    now.value = Date.now();
  }, 1000);
}

function clearTimer() {
  if (timer) window.clearInterval(timer);
  timer = null;
  liveMetrics.value = { bitrate: 0, videoLoss: null, audioLoss: null };
}

function applyMonitor(snapshot: StreamMonitorSnapshot) {
  monitorSnapshot.value = snapshot;
  liveMetrics.value = {
    bitrate: Math.round(snapshot.quality.bitrateKbps * 10) / 10,
    videoLoss: snapshot.tracks.find(track => track.kind === "video")?.loss ?? null,
    audioLoss: snapshot.tracks.find(track => track.kind === "audio")?.loss ?? null
  };
  monitorState.value = snapshot.status === "online" ? "fresh" : "offline";
  monitorAlive.value = { seconds: snapshot.network.aliveSecond, receivedAt: Date.now() };
  readerCount.value = snapshot.network.readerCount;
  const video = snapshot.tracks.find(track => track.kind === "video");
  const audio = snapshot.tracks.find(track => track.kind === "audio");
  streamInfo.value = {
    ...streamInfo.value,
    nodeId: snapshot.node.id,
    nodeName: snapshot.node.name || snapshot.node.host,
    nodeHost: snapshot.node.host,
    videoCodec: video?.codec || "—",
    audioCodec: audio?.codec || "—",
    audioSampleRate: audio?.sampleRate || 0,
    resolution: video?.width && video?.height ? `${video.width}×${video.height}` : "—",
    videoFps: video?.fps || 0
  };
}

async function refreshMonitor() {
  const streamId = playResult.value?.streamId;
  const token = sessionToken;
  if (!canMonitorPlayback.value || !streamId || phase.value !== "playing") return;
  try {
    const response = await getStreamMonitor(streamId);
    if (token !== sessionToken || playResult.value?.streamId !== streamId) return;
    if (response.code === 0 && response.data) applyMonitor(response.data);
  } catch (error: any) {
    if (token !== sessionToken || playResult.value?.streamId !== streamId) return;
    const status = error?.response?.status;
    const message = String(error?.response?.data?.message || error?.message || "");
    if (status === 404 || message.includes("离线")) {
      monitorState.value = "offline";
      monitorSnapshot.value = null;
      monitorAlive.value = null;
      liveMetrics.value = { bitrate: 0, videoLoss: null, audioLoss: null };
      readerCount.value = 0;
    } else {
      monitorState.value = monitorSnapshot.value ? "stale" : "idle";
    }
  }
}

function beginMonitor() {
  if (!canMonitorPlayback.value || monitorTimer) return;
  void refreshMonitor();
  monitorTimer = window.setInterval(() => void refreshMonitor(), 2000);
}

function clearMonitor() {
  if (monitorTimer) window.clearInterval(monitorTimer);
  monitorTimer = null;
  monitorSnapshot.value = null;
  monitorState.value = "idle";
  monitorAlive.value = null;
}

/* ────────────────────────── 真实点播会话 ────────────────────────── */

async function startSession() {
  const channel = props.channel;
  if (!canStartPlayback.value || !channel || !props.visible) return;
  const previousStreamId = playResult.value?.streamId;
  const token = ++sessionToken;
  resetSessionState();
  await stopTalk();
  if (hasPermission("gb28181:play:stop") && previousStreamId) await stopPlay(previousStreamId).catch(() => undefined);
  if (token !== sessionToken || !props.visible || props.channel?.id !== channel.id) return;
  phase.value = "requesting";
  errorMessage.value = "";
  startedAt.value = Date.now();
  now.value = Date.now();
  beginTimer();
  try {
    const response = await startPlay(channel.deviceId, channel.channelId);
    if (token !== sessionToken || !props.visible) {
      if (hasPermission("gb28181:play:stop") && token > localCleanupThroughToken && response.data?.streamId) {
        await stopPlay(response.data.streamId).catch(() => undefined);
      }
      return;
    }
    if (response.code !== 0 || !response.data) throw new Error(response.message || "点播失败");
    playResult.value = response.data;
    streamInfo.value.streamId = response.data.streamId;
    streamInfo.value.ssrc = response.data.ssrc;
    streamInfo.value.urls = response.data.urls || {};
    playbackSnapshot.value = resolvePlaybackSource(response.data, window.location.protocol === "https:");
    protocol.value = playbackSnapshot.value?.protocol || defaultProtocol();
    phase.value = "playing";
    beginMonitor();
    void loadPanelData();
  } catch (error: any) {
    if (token !== sessionToken) return;
    phase.value = "error";
    errorMessage.value = error?.message || "点播失败,请检查设备在线状态";
    clearMonitor();
    clearTimer();
  }
}

function resetSessionState() {
  clearMonitor();
  clearTimer();
  playResult.value = null;
  playbackSnapshot.value = null;
  phase.value = "idle";
  startedAt.value = null;
  errorMessage.value = "";
  assetManagerVisible.value = false;
  assetSearch.value = "";
  presetDraft.value = null;
  savePresetDialogVisible.value = false;
  activePresetId.value = null;
  saveCruiseDialogVisible.value = false;
  cruiseDraft.value = null;
  cruiseDraftTouched.value = false;
  cruiseDraftSubmitError.value = "";
  activeCruiseId.value = null;
  cruiseState.value = "stopped";
  // 扫描是纯下发状态,换通道后旧的运行态必须清掉(否则会显示"上一路通道正在扫描")。
  scanState.value = "stopped";
  scanActiveGroup.value = null;
  scanBusy.value = false;
  scanError.value = "";
  cruiseFreshness.value = "unknown";
  cruiseRefreshPending.value = false;
  cruiseLoadError.value = "";
  cruiseRefreshError.value = "";
  cruiseMoreVisible.value = false;
  clearCruiseReconcilePolling();
  probeToken++;
  clearProbeTimers();
  probeState.value = "idle";
  probeDurationMs.value = probeDurations[0].value;
  probeRemainingMs.value = 3000;
  probeSnapshot.value = null;
  liveMetrics.value = { bitrate: 0, videoLoss: null, audioLoss: null };
  readerCount.value = 0;
  capabilities.value = null;
  presets.value = [];
  cruiseTracks.value = [];
  resetHomePositionState();
  clearAdvancedPolling();
  advancedOperationStatus.value = {};
  exitDragZoomMode();
  // 目标跟踪的意图是**按通道**存的：换通道后旧意图必须清掉，
  // 否则「平台最近一次下发」那行会显示上一路通道的跟踪模式（而它已经不在眼前了）。
  exitTargetTrackMode();
  targetTrackIntent.value = null;
  targetTrackLoaded.value = false;
  targetTrackError.value = "";
  targetTrackStatus.value = "";
}

function cleanupSessionLocally() {
  localCleanupThroughToken = Math.max(localCleanupThroughToken, sessionToken);
  sessionToken++;
  resetSessionState();
}

function reconnect() {
  void startSession();
}

function handleMinimize() {
  releaseContinuousControls();
  exitDragZoomMode();
  assetManagerVisible.value = false;
  savePresetDialogVisible.value = false;
  saveCruiseDialogVisible.value = false;
  keepMiniPlayerInViewport();
  emit("update:displayMode", "minimized");
}

function handleRestore() {
  finishMiniPlayerDrag();
  emit("update:displayMode", "expanded");
}

function handleClose() {
  finishMiniPlayerDrag();
  releasePtzControl();
  cleanupTalkLocally();
  cleanupSessionLocally();
  emit("update:visible", false);
}

/**
 * 关闭入口的草稿闸门（2026-09-19）。
 *
 * ⛔ 关闭是**真丢**：`a-modal` 带 `unmount-on-close`，控制台一关 DeviceConfigDrawer 就
 *    跟着卸载，草稿只活在它的 `familyValues` 里 —— 用户得从头再画一遍遮挡框。
 *    切页签丢不掉草稿（组件还在），所以那边只给角标；这里必须拦一次。
 *
 * 未下发时不给"先下发再关"作为默认动作：下发是异步 + 要等设备回执，把它塞进关闭链路
 * 会让"关不掉"变成一个看不见的卡顿。用户想下发就从「留在控制台」回去点浮条。
 */
function requestClose() {
  // ⛔ 合计（画面 + OSD）：只报画面组的数，用户点了确认才发现 OSD 的改动也一起丢了。
  const pending = draftTotalCount.value;
  if (pending === 0) {
    handleClose();
    return;
  }
  Modal.warning({
    title: "画面改动还没下发",
    content: `有 ${pending} 项画面改动（${pictureDraftSummary.value}）尚未下发，关闭控制台后需要重新框选。`,
    okText: "放弃并关闭",
    cancelText: "留在控制台",
    onOk: () => {
      handleClose();
    }
  });
}

function handlePlayerError(message: string) {
  if (phase.value !== "playing" && phase.value !== "paused") return;
  phase.value = "error";
  errorMessage.value = message || "播放器拉流失败";
  clearMonitor();
  clearTimer();
}

async function loadPanelData() {
  const channel = props.channel;
  if (!canViewPtz.value || !channel) return;
  const token = sessionToken;
  try {
    const response = await getControlCapabilities(channel.id);
    if (token === sessionToken && props.channel?.id === channel.id && response.code === 0 && response.data) {
      capabilities.value = response.data;
    }
  } catch {
    if (token === sessionToken) capabilities.value = null;
  }
  await Promise.all([
    loadPresets(channel.id, token),
    loadCruises(channel.id, token, false),
    loadHomePosition(channel.id, token),
    // 只读平台里"上次回读得到的配置"(refresh=false,不发 SIP 报文)。
    // ⛔ 与存储卡同一条口径:缓存为空时面板停在 never_read(表单禁用,
    // 不让用户在空白上猜数字);有缓存才展示,并带 freshness 说明不是刚问的。
    loadVideoParams(channel.id, token, false),
    // 目标跟踪：同样只读平台自己记的"已下发意图"。标准里没有"查设备在跟踪什么"
    // 这条命令，所以这个读接口不可能去问设备 —— 它读的永远是平台写过的那一行。
    loadTargetTrack(channel.id, token)
  ]);
}

async function loadPresets(channelId = props.channel?.id, token = sessionToken, refresh = false) {
  if (!canViewPtz.value || !channelId) return;
  try {
    const response = await listPtzPresets(channelId, refresh);
    if (token === sessionToken && props.channel?.id === channelId && response.code === 0 && response.data) {
      presetFreshness.value = response.data.freshness || "unknown";
      presets.value = response.data.list
        .map(item => ({
          id: Number(item.presetId ?? item.id),
          name: String(item.name || `预置位 ${item.presetId ?? item.id}`),
          setAt: item.updatedAt ? String(item.updatedAt) : undefined
        }))
        .filter(item => item.id > 0);
    }
  } catch {
    if (token === sessionToken) {
      presets.value = [];
      presetFreshness.value = "unknown";
    }
  }
}

async function loadCruises(channelId = props.channel?.id, token = sessionToken, refresh = true) {
  if (!canViewPtz.value || !channelId || token !== sessionToken || props.channel?.id !== channelId) return;
  cruiseRefreshPending.value = false;
  cruiseLoadError.value = "";
  cruiseRefreshError.value = "";
  try {
    const response = await listCruiseTracks(channelId, refresh);
    if (token !== sessionToken || props.channel?.id !== channelId) return;
    if (response.code !== 0 || !response.data) {
      cruiseLoadError.value = response.message || "加载巡航轨迹失败,请重试";
      return;
    }
    if (!Array.isArray(response.data.list)) {
      cruiseLoadError.value = "巡航轨迹数据格式错误,请重试";
      return;
    }
    cruiseFreshness.value = response.data.freshness || "unknown";
    cruiseRefreshPending.value = Boolean(response.data.refreshOperationId);
    cruiseRefreshError.value = response.data.refreshError || "";
    cruiseTracks.value = response.data.list
      .map(item => {
        const detail = parseCruiseDetail(item.detail);
        // 待对账标记必须同时仍是未知/禁用状态;标准列表确认存在后 enabled=true,
        // 即使详情 JSON 保留旧 source 也不能把设备已确认的轨迹继续锁死。
        const pending =
          item.enabled !== true && (detail.source === "reconcile-pending" || detail.source === "optimistic-pending");
        return {
          id: Number(item.trackId ?? item.id),
          name: String(item.name || `巡航轨迹 ${item.trackId ?? item.id}`),
          enabled: item.enabled !== false && !pending,
          pending,
          points: detail.points,
          source: detail.source
        };
      })
      .filter(item => Number.isInteger(item.id) && item.id >= 0 && item.id <= 255);
    if (refresh && response.data.refreshOperationId) scheduleCruiseReconcilePolling(channelId, token);
  } catch {
    if (token === sessionToken && props.channel?.id === channelId) {
      cruiseLoadError.value = "加载巡航轨迹失败,请重试";
      cruiseRefreshPending.value = false;
      cruiseRefreshError.value = "";
    }
  }
}

async function loadHomePosition(channelId = props.channel?.id, token = sessionToken) {
  if (!canViewPtz.value || !channelId) return;
  const generation = homePositionGeneration;
  if (token === sessionToken && generation === homePositionGeneration && props.channel?.id === channelId) {
    homePhase.value = "loading";
    homeError.value = "";
  }
  try {
    const response = await getHomePosition(channelId);
    if (token !== sessionToken || generation !== homePositionGeneration || props.channel?.id !== channelId) return;
    if (response.code !== 0 || !response.data) throw new Error(response.message || "加载看守位失败");
    applyHomePositionResult(response.data);
    resumeHomePositionPending(channelId, token, generation);
  } catch (error: any) {
    if (token === sessionToken && generation === homePositionGeneration && props.channel?.id === channelId) {
      clearHomePositionPolling(false);
      homePending.value = null;
      homePhase.value = "error";
      homeError.value = error?.message || "加载看守位失败";
    }
  }
}

/* ─────────────────── 视频参数属性(A.2.1.13 / A.2.4.7 / A.2.3.2.5) ─────────────────── */

/**
 * 回读结果 → 面板可编辑副本。
 *
 * ⛔ 逐格拷贝而不是把 `VideoParam` 直接当草稿:回读值是"设备怎么说的"(只读事实),
 * 草稿是"用户想改成什么"。两者混成一个对象后,"已改 N 项"与"还原"就没法算了。
 */
function videoParamsToDraft(params: VideoParam[]): VideoParamCodecItem[] {
  return params
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

/**
 * 对照区取哪一路码流:优先 0 号主码流,没有就用第一路。
 *
 * ⛔ 为什么必须同屏:"平台改分辨率 → 拉流实测分辨率变化(探针/ffprobe)"就是
 * A-5 的验收闭环。改在哪、验在哪必须挨着看,否则改完还要切到别的模块找验证点。
 * 实测值是**当前正在播的那一路**,所以只能跟"我们认定的主码流"比 ——
 * 这个对应关系要写在界面上,不能让人以为三行天然同源。
 */
const videoParamCompareStream = computed(() => {
  const rows = videoParamsDraft.value;
  if (!rows.length) return null;
  return rows.find(row => row.streamNumber === selectedVideoStream.value) ?? rows.find(row => row.streamNumber === 0) ?? rows[0];
});

function selectVideoStream(value: string) {
  // ⛔ 只改这一个真源：侧栏的「配置文件」下拉通过 `v-model:stream-profile` 跟着走
  //    （见 `videoParamStreamProfile`），不要再自己转发一次。
  selectedVideoStream.value = Number(value) || 0;
}

/**
 * 对照区里的「回读」一行取**设备最近一次回读**的值(不是草稿值)。
 *
 * ⛔ 必须用 `videoParams`(回读事实)而不是 `videoParamsDraft`(用户编辑中的值):
 * 否则用户一改分辨率,「回读」行就跟着变,三行对照立刻失去意义 ——
 * 那样看到的是"我改了什么",而不是"设备实际是什么"。
 */
function videoParamCompareReadRow(): VideoParam | undefined {
  const streamNumber = videoParamCompareStream.value?.streamNumber;
  if (streamNumber === undefined) return undefined;
  return videoParams.value.find(item => item.streamNumber === streamNumber);
}

/** `resolutionText` 那六个码值对应的像素尺寸（用来把"码值档位"与"实测像素"拉到同一把尺子上）。 */
const VIDEO_RESOLUTION_TIERS: Record<string, { width: number; height: number }> = {
  QCIF: { width: 176, height: 144 },
  CIF: { width: 352, height: 288 },
  "4CIF": { width: 704, height: 576 },
  D1: { width: 720, height: 576 },
  "720P": { width: 1280, height: 720 },
  "1080P": { width: 1920, height: 1080 }
};

/** `1920×1080` / `1920x1080` → `{1920, 1080}`；取不到两个数就返回 null。 */
function pixelsOf(text: string): { width: number; height: number } | null {
  const nums = (String(text ?? "").match(/\d+/g) ?? []).map(Number).slice(0, 2);
  return nums.length === 2 && nums[0] > 0 && nums[1] > 0 ? { width: nums[0], height: nums[1] } : null;
}

/** 编码格式归一：`H.264` / `H264` / `h264` 是同一个东西；中文（"未上报"）会被剥成空串 = 未知。 */
function normalizeCodecToken(text: string): string {
  return String(text ?? "")
    .replace(/[^A-Za-z0-9]/g, "")
    .toUpperCase();
}

/**
 * 「设备回读」与「画面实测」**逐项**是否对得上（2026-09-20 收成两行时补）。
 *
 * ⭐ 这是这张卡继续存在的理由：A-5 的验收问题就是"平台改了分辨率、设备回读也 OK，
 *    **画面到底变没变**"。原来三行并列却不下结论，要用户拿眼睛比 `1080P` 和 `1920×1080`。
 * ⛔ **不比码率**：实测那行的码率是 2 秒轮询的瞬时值，与设备里配的目标码率天然不等，
 *    比它必然"永远不一致" —— 假警报比不下判断更坏。
 * ⛔ 读不到回读（还没点「读取」）⇒ 三项全 `null`，界面写「未读取」，**不是**"不一致"。
 * ⛔ 分辨率按**像素**比、容差 ±20px：设备侧是码值档位（`D1` = 720×576），ZLM 报的是
 *    实际解码尺寸（704×576 也属同一档）—— 按码值硬比会天天误报。
 */
const videoParamDiffs = computed<{ codec: boolean | null; resolution: boolean | null; fps: boolean | null }>(() => {
  const unknown = { codec: null, resolution: null, fps: null };
  const read = videoParamCompareReadRow();
  if (!read || !videoParamCompareStream.value) return unknown;

  const readCodec = normalizeCodecToken(videoFormatText(read.videoFormat));
  const measuredCodec = normalizeCodecToken(streamInfo.value.videoCodec);
  const codec = readCodec && measuredCodec ? readCodec === measuredCodec : null;

  const readLabel = resolutionText(read.resolution);
  const readPixels = VIDEO_RESOLUTION_TIERS[readLabel] ?? pixelsOf(readLabel);
  const measuredPixels = pixelsOf(streamInfo.value.resolution);
  const resolution =
    readPixels && measuredPixels
      ? Math.abs(readPixels.width - measuredPixels.width) > 20 || Math.abs(readPixels.height - measuredPixels.height) > 20
      : null;

  const readFps = Number(read.frameRate) || 0;
  const measuredFps = Number(streamInfo.value.videoFps) || 0;
  const fps = readFps && measuredFps ? readFps !== measuredFps : null;

  return { codec, resolution, fps };
});

/**
 * 三项的汇总结论。⛔ 只在**比得出来的项**里下结论：比不出来的（`null`）不参与，
 * 三项都比不出来时写「未读取」—— 把"不知道"说成"不一致"会让人白跑一趟。
 */
const videoParamVerdict = computed(() => {
  const diffs = videoParamDiffs.value;
  const comparable = [diffs.codec, diffs.resolution, diffs.fps].filter(value => value !== null) as boolean[];
  if (!comparable.length) return { tone: "unknown", text: "未读取" };
  return comparable.some(Boolean) ? { tone: "differ", text: "与画面不一致" } : { tone: "same", text: "与画面一致" };
});

/**
 * 结论标签的悬浮说明：把**比了哪三项、为什么不比码率**写在旁边。
 * ⭐ 一个"不一致"的结论必须能被质疑 —— 用户点开就知道平台到底比了什么。
 */
const videoParamVerdictTitle = computed(() => {
  if (videoParamVerdict.value.tone === "unknown") return "还没读到设备参数 —— 点「读取」后才有对照";
  const diffs = videoParamDiffs.value;
  const parts: string[] = [];
  if (diffs.codec === true) parts.push("编码格式");
  if (diffs.resolution === true) parts.push("分辨率");
  if (diffs.fps === true) parts.push("帧率");
  return parts.length
    ? `画面实际在播的${parts.join(" / ")}与设备回读对不上`
    : "编码格式 / 分辨率 / 帧率三项与设备回读一致（码率是瞬时采样值，不参与比对）";
});

/**
 * 把一次回读应答落进面板。
 *
 * ⛔ 参数类型直接用接口的 `VideoParamResult`，不再手抄一份子集 ——
 * 手抄的子集会在后端加字段时**静默落后**（本次新增 registeredVersion 时就撞上了）。
 */
function applyVideoParamResult(data: VideoParamResult) {
  videoParams.value = data.list || [];
  videoParamsDraft.value = videoParamsToDraft(videoParams.value);
  videoParamRegisteredVersion.value = data.registeredVersion || "";
}

/**
 * 详情条只读取平台缓存事实。设备读取、编辑、下发和对账由右侧嵌入式配置组件负责。
 */
async function loadVideoParams(channelId = props.channel?.id, token = sessionToken, refresh = false) {
  const contextKey = channelContextKey();
  if (!channelId || !isCurrentChannelContext(channelId, token, contextKey)) return;
  try {
    const response = await getChannelVideoParams(channelId, refresh);
    if (!isCurrentChannelContext(channelId, token, contextKey)) return;
    if (response.code !== 0 || !response.data) throw new Error(response.message || "读取视频参数失败");
    applyVideoParamResult(response.data);
  } catch {
    if (!isCurrentChannelContext(channelId, token, contextKey)) return;
    videoParams.value = [];
    videoParamsDraft.value = [];
  }
}

/* ────────────────────────── 云台指令 ────────────────────────── */

const ptzActions: Record<string, string> = {
  左上: "left_up",
  上: "up",
  右上: "right_up",
  左: "left",
  右: "right",
  左下: "left_down",
  下: "down",
  右下: "right_down",
  放大: "zoom_in",
  缩小: "zoom_out",
  远焦: "focus_far",
  近焦: "focus_near",
  "光圈+": "iris_open",
  "光圈-": "iris_close",
  停止: "stop",
  // FI 族(聚焦/光圈)有**自己的**停止码:方向族停 0x00、FI 族停 0x40(GB/T 28181 表 A.6)。
  // 松手时对 FI 发 0x00 设备不会停 —— 它只认本族的停止指令。
  镜头停止: "lens_stop"
};
let activePtzAction = "";

async function sendPtz(action: string) {
  if (!canControlPtz.value || !props.channel) return;
  if (action === "停止" || action === "镜头停止") activePtzAction = "";
  else activePtzAction = action;
  try {
    const response = await controlPtz(props.channel.id, {
      action: ptzActions[action] || action,
      speed: levelToProtocolSpeed(moveSpeed.value),
      idempotencyKey: `${props.channel.id}-${action}-${Date.now()}`
    });
    if (response.code !== 0) throw new Error(response.message || "云台指令失败");
  } catch (error: any) {
    Message.error(error?.message || `云台指令 ${action} 失败`);
  }
}

async function loadDefaultPtzSpeed() {
  moveSpeed.value = DEFAULT_PTZ_SPEED_LEVEL;
  if (!canReadPtzSpeed.value) return;
  try {
    const response = await fetchPTZDefaultSpeedConfig();
    if (response.code === 0 && response.data) moveSpeed.value = normalizePtzSpeedLevel(response.data.level);
  } catch {
    // 配置读取失败时保持默认 6 档，不阻断播放和云台控制。
  }
}

const joystickDirectionMap: Record<string, JoystickDirection> = {
  ArrowUp: "上",
  ArrowRight: "右",
  ArrowDown: "下",
  ArrowLeft: "左"
};
const joystickDiagonalDirection = (dx: number, dy: number): JoystickDirection => {
  const angle = ((Math.atan2(dx, -dy) * 180) / Math.PI + 360) % 360;
  if (angle < 22.5 || angle >= 337.5) return "上";
  if (angle < 67.5) return "右上";
  if (angle < 112.5) return "右";
  if (angle < 157.5) return "右下";
  if (angle < 202.5) return "下";
  if (angle < 247.5) return "左下";
  if (angle < 292.5) return "左";
  return "左上";
};
function stopJoystickMotion() {
  if (joystickDirection.value) void sendPtz("停止");
  joystickDirection.value = "";
}
function resetJoystickPosition() {
  joystickDragging.value = false;
  joystickPointerId.value = null;
  joystickOffsetX.value = 0;
  joystickOffsetY.value = 0;
}
function updateJoystick(event: PointerEvent, stage: HTMLElement) {
  const rect = stage.getBoundingClientRect();
  const centerX = rect.left + rect.width / 2;
  const centerY = rect.top + rect.height / 2;
  const maxRadius = Math.max(12, Math.min(rect.width, rect.height) / 2 - 36);
  let dx = event.clientX - centerX;
  let dy = event.clientY - centerY;
  const distance = Math.hypot(dx, dy);
  const clampedDistance = Math.min(distance, maxRadius);
  if (distance > maxRadius) {
    dx = (dx / distance) * maxRadius;
    dy = (dy / distance) * maxRadius;
  }
  joystickOffsetX.value = dx;
  joystickOffsetY.value = dy;
  if (clampedDistance < 8) {
    stopJoystickMotion();
    return;
  }
  const direction = joystickDiagonalDirection(dx, dy);
  if (direction !== joystickDirection.value) {
    joystickDirection.value = direction;
    void sendPtz(direction);
  }
}
function startJoystick(event: PointerEvent) {
  if (!canControlPtz.value || !props.channel) return;
  const stage = event.currentTarget as HTMLElement;
  joystickDragging.value = true;
  joystickPointerId.value = event.pointerId;
  stage.setPointerCapture?.(event.pointerId);
  updateJoystick(event, stage);
}
function moveJoystick(event: PointerEvent) {
  if (!joystickDragging.value || joystickPointerId.value !== event.pointerId) return;
  updateJoystick(event, event.currentTarget as HTMLElement);
}
function endJoystick(event?: PointerEvent) {
  if (event && joystickPointerId.value !== event.pointerId) return;
  stopJoystickMotion();
  if (event) {
    const stage = event.currentTarget as HTMLElement;
    if (stage.hasPointerCapture?.(event.pointerId)) stage.releasePointerCapture(event.pointerId);
  }
  resetJoystickPosition();
}
function handleJoystickKeydown(event: KeyboardEvent) {
  if (!canControlPtz.value) return;
  const direction = joystickDirectionMap[event.key];
  if (!direction) return;
  event.preventDefault();
  joystickDragging.value = true;
  if (direction !== joystickDirection.value) {
    joystickDirection.value = direction;
    void sendPtz(direction);
  }
}
function handleJoystickKeyup(event: KeyboardEvent) {
  if (!joystickDirectionMap[event.key]) return;
  event.preventDefault();
  endJoystick();
}
function releasePtzControl() {
  if (joystickDragging.value || joystickDirection.value) endJoystick();
  else if (activePtzAction) void sendPtz("停止");
}

async function sendPrecise() {
  if (!canControlPtz.value || !props.channel) return;
  try {
    const response = await controlPtzPrecise(props.channel.id, {
      pan: precisePan.value,
      tilt: preciseTilt.value,
      zoom: preciseZoom.value
    });
    if (response.code !== 0) throw new Error(response.message || "精准定位失败");
    Message.success("精准定位请求已受理");
  } catch (error: any) {
    Message.error(error?.message || "精准定位失败");
  }
}

async function callPreset(id: number) {
  if (!canCallPtzPreset.value || !props.channel) return;
  try {
    const response = await callPtzPreset(props.channel.id, id);
    if (response.code !== 0) throw new Error(response.message || "调用预置位失败");
    activePresetId.value = id;
  } catch (error: any) {
    Message.error(error?.message || "调用预置位失败");
  }
}
function openSavePresetDialog() {
  if (!canSavePtzPreset.value || !props.channel) return;
  const nextId = nextPresetId();
  presetDraft.value = { id: nextId, name: `预置位 ${nextId}`, submitting: false };
  presetNameTouched.value = false;
  savePresetDialogVisible.value = true;
}
function closeSavePresetDialog() {
  savePresetDialogVisible.value = false;
  presetDraft.value = null;
  presetNameTouched.value = false;
}
const presetNameError = computed(() => {
  if (!presetNameTouched.value || !presetDraft.value) return "";
  const name = presetDraft.value.name.trim();
  if (!name) return "请填写预置位名称";
  if (name.length > 16) return "名称最多 16 个字符";
  return "";
});
async function handleSavePresetBeforeOk(done: (closable?: boolean) => void) {
  if (!canSavePtzPreset.value || !presetDraft.value || !props.channel) {
    done(false);
    return;
  }
  presetNameTouched.value = true;
  if (presetNameError.value) {
    done(false);
    return;
  }
  const name = presetDraft.value.name.trim() || `预置位 ${presetDraft.value.id}`;
  const presetId = presetDraft.value.id;
  presetDraft.value.submitting = true;
  try {
    const response = await createPtzPreset(props.channel.id, { presetId, name });
    if (response.code !== 0) throw new Error(response.message || "设置预置位失败");
    Message.success(`预置位 #${presetId} 请求已受理`);
    presetDraft.value = null;
    presetNameTouched.value = false;
    await loadPresets();
    done(true);
  } catch (error: any) {
    Message.error(error?.message || "设置预置位失败");
    if (presetDraft.value) presetDraft.value.submitting = false;
    done(false);
  }
}
async function deletePreset(id: number) {
  if (!canDeletePtzPreset.value || !props.channel) return;
  Modal.warning({
    title: `确认删除预置位 #${id}?`,
    content: "删除命令会下发到设备。",
    okText: "确认删除",
    cancelText: "取消",
    onOk: async () => {
      if (!hasPermission("gb28181:ptz:preset:delete") || !props.channel) return;
      try {
        const response = await deletePtzPreset(props.channel!.id, id);
        if (response.code !== 0) throw new Error(response.message || "删除预置位失败");
        if (activePresetId.value === id) activePresetId.value = null;
        await loadPresets();
      } catch (error: any) {
        Message.error(error?.message || "删除预置位失败");
      }
    }
  });
}

async function executeCruiseToggle(id: number) {
  if (!canControlPtzCruise.value || !props.channel) return;
  const isCurrent = activeCruiseId.value === id;
  const action = isCurrent && cruiseState.value === "start-sent" ? "stop" : "start";
  const unconfirmed = cruiseTracks.value.some(track => track.id === id && track.pending);
  const channelId = props.channel.id;
  const token = sessionToken;
  try {
    const response = await controlPtzCruise(channelId, { action, trackId: id });
    if (token !== sessionToken || props.channel?.id !== channelId) return;
    if (response.code !== 0) throw new Error(response.message || "巡航指令失败");
    if (["rejected", "timeout", "cancelled"].includes(String(response.data?.status))) {
      throw new Error(response.message || `巡航${action === "stop" ? "停止" : "启动"}未被设备接受`);
    }
    if (action === "stop") {
      cruiseState.value = "stopped";
      activeCruiseId.value = null;
      Message.info(`巡航 #${id} 停止指令已发送`);
    } else {
      activeCruiseId.value = id;
      cruiseState.value = "start-sent";
      Message.info(unconfirmed ? `巡航 #${id} 试运行指令已发送,请观察设备是否开始巡航` : `巡航 #${id} 启动指令已发送,待设备确认`);
    }
  } catch (error: any) {
    Message.error(error?.message || "巡航指令失败");
  }
}
function toggleCruise(id: number) {
  if (!canControlPtzCruise.value) return;
  const isCurrent = activeCruiseId.value === id && cruiseState.value === "start-sent";
  const unconfirmed = cruiseTracks.value.some(track => track.id === id && track.pending);
  if (!isCurrent && unconfirmed) {
    Modal.warning({
      title: "试运行未验证轨迹",
      content: `轨迹 #${id} 还没收到设备回执,设备上实际存的内容可能跟这里显示的不一致(同编号轨迹可能是之前配的)。试运行会让云台真的动起来,请先确认现场安全。`,
      hideCancel: false,
      okText: "继续试运行",
      cancelText: "取消",
      onOk: () => (hasPermission("gb28181:ptz:cruise") ? executeCruiseToggle(id) : undefined)
    });
    return;
  }
  return executeCruiseToggle(id);
}
async function stopCruise() {
  const trackId = activeCruiseId.value;
  if (!canControlPtzCruise.value || !props.channel || trackId === null) return;
  const channelId = props.channel.id;
  const token = sessionToken;
  try {
    const response = await controlPtzCruise(channelId, { action: "stop", trackId });
    if (token !== sessionToken || props.channel?.id !== channelId) return;
    if (response.code !== 0) throw new Error(response.message || "停止巡航失败");
    cruiseState.value = "stopped";
    activeCruiseId.value = null;
    Message.info(`巡航 #${trackId} 停止指令已发送`);
  } catch (error: any) {
    Message.error(error?.message || "停止巡航失败");
  }
}
/**
 * 下发一条扫描指令(89H / 8AH)。
 *
 * ⛔ 「停止」没有专用指令码:字节 4-7 全零就是全族通用的停止帧,与巡航停止同一形态。
 * ⛔ 边界设置是**就地写入当前云台位置**,平台拿不到设备的当前边界值 —— 所以这两个按钮
 *    只做「把现在这个朝向记为左/右边界」,不做任何回显或确认。
 */
async function sendScanCommand(action: ScanAction) {
  if (!canControlPtz.value || !props.channel || scanBusy.value) return;
  if (scanGroupInvalid.value) {
    Message.error(`扫描组号必须在 ${SCAN_GROUP_MIN}-${SCAN_GROUP_MAX} 之间`);
    return;
  }
  if (action === "scan_set_speed" && scanSpeedInvalid.value) {
    Message.error(`扫描速度必须在 ${SCAN_SPEED_MIN}-${SCAN_SPEED_MAX} 之间`);
    return;
  }
  const group = scanGroup.value;
  const value = action === "scan_set_speed" ? scanSpeed.value : undefined;
  const channelId = props.channel.id;
  const token = sessionToken;
  scanBusy.value = true;
  scanError.value = "";
  try {
    const payload: { action: ScanAction; id: number; value?: number } = { action, id: group };
    if (typeof value === "number") payload.value = value;
    const response = await controlPtzScan(channelId, payload);
    if (token !== sessionToken || props.channel?.id !== channelId) return;
    if (response.code !== 0) throw new Error(response.message || "扫描指令失败");
    if (["rejected", "timeout", "cancelled"].includes(String(response.data?.status))) {
      throw new Error(response.message || "扫描指令未被设备接受");
    }
    if (action === "scan_start") {
      scanState.value = "start-sent";
      scanActiveGroup.value = group;
      Message.info(`扫描组 #${group} 启动指令已发送,请观察设备是否开始扫描`);
    } else if (action === "scan_stop") {
      scanState.value = "stopped";
      scanActiveGroup.value = null;
      Message.info(`扫描组 #${group} 停止指令已发送`);
    } else if (action === "scan_set_left") {
      Message.success(`已把当前朝向设为扫描组 #${group} 的左边界`);
    } else if (action === "scan_set_right") {
      Message.success(`已把当前朝向设为扫描组 #${group} 的右边界`);
    } else {
      Message.success(`扫描组 #${group} 速度已下发(${value})`);
    }
  } catch (error: any) {
    if (token !== sessionToken) return;
    scanError.value = error?.message || "扫描指令失败";
    Message.error(scanError.value);
  } finally {
    if (token === sessionToken) scanBusy.value = false;
  }
}
function toggleScan() {
  if (!canControlPtz.value) return;
  return sendScanCommand(scanState.value === "start-sent" ? "scan_stop" : "scan_start");
}

async function deleteCruise(id: number) {
  if (!canControlPtzCruise.value || !props.channel) return;
  const channelId = props.channel.id;
  const token = sessionToken;
  Modal.warning({
    title: "删除巡航轨迹",
    content: `确认删除巡航轨迹 #${id}?此操作会下发到设备,不可撤销。`,
    hideCancel: false,
    okText: "删除",
    cancelText: "取消",
    onOk: async () => {
      if (!hasPermission("gb28181:ptz:cruise") || !props.channel) return;
      try {
        const response = await controlPtzCruise(channelId, { action: "delete", trackId: id });
        if (token !== sessionToken || props.channel?.id !== channelId) return;
        if (response.code !== 0) throw new Error(response.message || "删除巡航失败");
        if (activeCruiseId.value === id) {
          activeCruiseId.value = null;
          cruiseState.value = "stopped";
        }
        Message.info(`巡航 #${id} 删除指令已下发,等设备回传确认`);
        await loadCruises(channelId, token, false);
      } catch (error: any) {
        Message.error(error?.message || "删除巡航失败");
      }
    }
  });
}

// 抽屉打开入口暂时被 tile grid + 更多 popover 替代,函数保留供未来管理面板复用
function openAssetManager(tab: AssetManagerTab) {
  assetManagerTab.value = tab;
  assetSearch.value = "";
  assetManagerVisible.value = true;
}
void openAssetManager;

function switchAssetManagerTab(tab: AssetManagerTab) {
  assetManagerTab.value = tab;
  assetSearch.value = "";
}

function closeAssetManager() {
  assetManagerVisible.value = false;
  assetSearch.value = "";
}

function openHomeSettingsDialog() {
  if (!homeCanConfigure.value) return;
  const draft = homeDraftFrom(homeConfirmed.value);
  homeDraft.value = {
    enabled: true,
    presetId: draft.presetId,
    resetTime:
      Number.isInteger(draft.resetTime) && Number(draft.resetTime) >= 10 && Number(draft.resetTime) <= 3600
        ? draft.resetTime
        : 300
  };
  homeSettingsTouched.value = false;
  homeSettingsDialogVisible.value = true;
}

function closeHomeSettingsDialog() {
  if (homeSettingsSubmitting.value) return;
  homeSettingsDialogVisible.value = false;
  homeSettingsTouched.value = false;
  restoreHomeDraftFromConfirmed();
}

async function submitHomeSettings() {
  homeSettingsTouched.value = true;
  if (!homePositionCanSave.value || homeSettingsSubmitting.value) return;
  homeSettingsSubmitting.value = true;
  const submitted = await saveHomePosition();
  homeSettingsSubmitting.value = false;
  if (submitted) {
    homeSettingsDialogVisible.value = false;
    homeSettingsTouched.value = false;
  }
}

async function closeHomePosition() {
  if (!homeCanClose.value) return;
  homeDraft.value = { enabled: false, presetId: null, resetTime: null };
  await saveHomePosition();
}

async function saveHomePosition() {
  if (!canUpdatePtzHome.value || !props.channel || !homeCanSubmit.value) return false;
  const channelId = props.channel.id;
  const token = sessionToken;
  const generation = beginHomePositionOperation();
  const data: HomePositionPatch = homeDraft.value.enabled
    ? { enabled: true as const, resetTime: homeDraft.value.resetTime!, presetId: homeDraft.value.presetId! }
    : { enabled: false as const };
  const idempotencyKey = nextHomePositionIdempotencyKey("control", channelId);
  homePending.value = { kind: "control", operationId: null, deadlineAt: null };
  homeOperationId.value = null;
  homePhase.value = "pending";
  homeError.value = "";
  homeMismatch.value = "";
  try {
    const response = await updateHomePosition(channelId, data, idempotencyKey);
    if (!isCurrentHomePositionContext(channelId, token, generation)) return;
    if (response.code !== 0 || !response.data?.operationId) throw new Error(response.message || "保存看守位失败");
    const operationId = response.data.operationId;
    homePending.value = { kind: "control", operationId, deadlineAt: null };
    homeOperationId.value = operationId;
    prepareHomePositionOperation(operationId);
    if (response.data.status === "accepted") {
      await refreshHomePositionAfterAccepted(channelId, token, generation);
    } else if (response.data.status === "unknown") {
      markHomePositionUnknown(homePending.value, "操作结果未知，可重试");
    } else if (["rejected", "timeout", "cancelled"].includes(response.data.status)) {
      finishHomePositionFailure(homePending.value, {
        operationId,
        status: response.data.status,
        errorCode: null,
        errorMessage: response.message || null,
        completedAt: null,
        deadlineAt: null
      });
    } else {
      homePositionInsuranceDeadlineMs = Date.now() + HOME_POSITION_INITIAL_QUEUE_MS + HOME_POSITION_DEADLINE_GRACE_MS;
      Message.info("看守位请求已受理，等待设备确认");
      scheduleHomePositionPoll(channelId, token, generation);
    }
    return !["rejected", "timeout", "cancelled"].includes(response.data.status);
  } catch (error: any) {
    if (!isCurrentHomePositionContext(channelId, token, generation)) return;
    clearHomePositionPolling(false);
    homePending.value = null;
    homePhase.value = "error";
    homeError.value = error?.message || "保存看守位失败";
    restoreHomeDraftFromConfirmed();
    Message.error(homeError.value);
    return false;
  }
}

async function refreshHomePosition() {
  if (!canViewPtz.value || !props.channel || !homeCanRefresh.value) return;
  const channelId = props.channel.id;
  const token = sessionToken;
  const generation = beginHomePositionOperation();
  const idempotencyKey = nextHomePositionIdempotencyKey("refresh", channelId);
  homePending.value = { kind: "refresh", operationId: null, deadlineAt: null };
  homeOperationId.value = null;
  homePhase.value = "pending";
  homeError.value = "";
  homeMismatch.value = "";
  try {
    const response = await getHomePosition(channelId, true, idempotencyKey);
    if (!isCurrentHomePositionContext(channelId, token, generation)) return;
    if (response.code !== 0 || !response.data) throw new Error(response.message || "刷新看守位失败");
    applyHomePositionResult(response.data);
    resumeHomePositionPending(channelId, token, generation);
  } catch (error: any) {
    if (!isCurrentHomePositionContext(channelId, token, generation)) return;
    clearHomePositionPolling(false);
    homePending.value = null;
    homeError.value = error?.message || "刷新看守位失败";
    homePhase.value = homeConfirmed.value ? (homeConfirmed.value.enabled ? "enabled" : "disabled") : "error";
    Message.error(homeError.value);
  }
}

async function readPreciseStatus() {
  if (!canViewPtz.value || !props.channel) return;
  try {
    const response = await getPtzPreciseStatus(props.channel.id, true);
    if (response.code !== 0) throw new Error(response.message || "读取当前位置失败");
    const state = response.data?.state;
    if (!state) {
      Message.info("已下发查询，设备状态尚未回传");
      return;
    }
    if (state.pan != null) precisePan.value = Number(state.pan);
    if (state.tilt != null) preciseTilt.value = Number(state.tilt);
    if (state.zoom != null) preciseZoom.value = Number(state.zoom);
  } catch (error: any) {
    Message.error(error?.message || "读取当前位置失败");
  }
}

function advancedPendingKey(action: string) {
  if (action === "record_start" || action === "record_stop") return "record";
  if (action === "guard_set" || action === "guard_reset") return "guard";
  return action;
}

function isAdvancedPending(action: string) {
  return advancedPending.value.has(advancedPendingKey(action));
}

function setAdvancedPending(action: string, pending: boolean) {
  const next = new Set(advancedPending.value);
  const key = advancedPendingKey(action);
  if (pending) next.add(key);
  else next.delete(key);
  advancedPending.value = next;
}

function clearAdvancedPolling() {
  for (const timerId of advancedOperationTimers.values()) window.clearTimeout(timerId);
  for (const timerId of advancedOperationRequestTimers.values()) window.clearTimeout(timerId);
  advancedOperationTimers.clear();
  advancedOperationRequestTimers.clear();
  advancedOperationPollContexts.clear();
  advancedStatusToken.value += 1;
  advancedOperationIds.value = {};
  advancedOperationDeadline.value = {};
  advancedOperationPhase.value = {};
  advancedPending.value = new Set();
}

function setAdvancedPhase(action: string, phase: AdvancedOperationPhase, message?: string) {
  advancedOperationPhase.value = { ...advancedOperationPhase.value, [action]: phase };
  if (message) advancedOperationStatus.value = { ...advancedOperationStatus.value, [action]: message };
}

/**
 * 设备已确认这条动作。
 *
 * ⛔ 这里**只**写结果文案,不再写任何本地"事实"：原先它还会把 record/guard 状态改成
 *    设备回传的值、并追一次 DeviceStatus 重读 —— 那两个动作 2026-09-20 已搬到
 *    设备详情抽屉的「设备控制」页,"当前是录着还是布着"的事实也随它一起搬走
 *    （抽屉那边自己读、自己写）。控制台剩下的 iframe / drag_zoom 没有可落的事实。
 */
function applyAdvancedAccepted(action: string) {
  setAdvancedPhase(action, "accepted", "设备已确认");
}

function advancedOperationError(action: string, status: AdvancedOperationPhase, operation: PTZOperation) {
  setAdvancedPhase(action, status, operation.errorCode || operation.errorMessage || `操作${status}`);
  if (status === "unknown") Message.warning(`${action} 操作结果未知,请刷新设备状态后确认`);
  else Message.error(operation.errorCode || operation.errorMessage || `${action} 操作未被设备接受`);
}

const ADVANCED_OPERATION_POLL_INTERVAL_MS = 1000;
const ADVANCED_OPERATION_FINAL_READ_GRACE_MS = 250;
const ADVANCED_OPERATION_NO_DEADLINE_READ_TIMEOUT_MS = 5000;

function parseAdvancedOperationDeadline(deadlineAt?: string | null) {
  const deadlineMs = deadlineAt ? Date.parse(deadlineAt) : Number.NaN;
  return Number.isFinite(deadlineMs) ? deadlineMs : null;
}

function clearAdvancedOperationPoll(action: string) {
  const pollTimer = advancedOperationTimers.get(action);
  if (pollTimer !== undefined) window.clearTimeout(pollTimer);
  const requestTimer = advancedOperationRequestTimers.get(action);
  if (requestTimer !== undefined) window.clearTimeout(requestTimer);
  advancedOperationTimers.delete(action);
  advancedOperationRequestTimers.delete(action);
  advancedOperationPollContexts.delete(action);
}

function hasCurrentAdvancedOperationPoll(action: string, operationId: string) {
  return advancedOperationPollContexts.get(action)?.operationId === operationId;
}

function beginAdvancedOperationPoll(action: string, operationId: string, deadlineAt?: string | null) {
  clearAdvancedOperationPoll(action);
  advancedOperationPollContexts.set(action, {
    operationId,
    deadlineMs: parseAdvancedOperationDeadline(deadlineAt)
  });
}

function updateAdvancedOperationPollDeadline(action: string, operationId: string, deadlineAt?: string | null) {
  const context = advancedOperationPollContexts.get(action);
  if (!context || context.operationId !== operationId) return null;
  const deadlineMs = parseAdvancedOperationDeadline(deadlineAt);
  if (deadlineMs !== null) context.deadlineMs = deadlineMs;
  return context.deadlineMs;
}

function advancedOperationUnknown(action: string, message: string) {
  clearAdvancedOperationPoll(action);
  setAdvancedPending(action, false);
  setAdvancedPhase(action, "unknown", message);
}

function getAdvancedOperationWithInsurance(action: string, channelId: number, operationId: string) {
  if (!canControlDevice.value) return Promise.reject(new Error("当前账号没有设备控制权限"));
  const context = advancedOperationPollContexts.get(action);
  if (!context || context.operationId !== operationId) {
    return Promise.reject(new Error("操作轮询已失效"));
  }
  const timeoutMs =
    context.deadlineMs === null
      ? ADVANCED_OPERATION_NO_DEADLINE_READ_TIMEOUT_MS
      : Math.max(
          ADVANCED_OPERATION_FINAL_READ_GRACE_MS,
          context.deadlineMs - Date.now() + ADVANCED_OPERATION_FINAL_READ_GRACE_MS
        );
  const request = getPtzOperation(channelId, operationId);
  return new Promise<Awaited<ReturnType<typeof getPtzOperation>>>((resolve, reject) => {
    let settled = false;
    const timeout = window.setTimeout(() => {
      if (settled) return;
      settled = true;
      if (advancedOperationRequestTimers.get(action) === timeout) advancedOperationRequestTimers.delete(action);
      reject(
        new Error(
          context.deadlineMs === null
            ? "服务端未返回操作截止时间，补读一次后结果未知"
            : "操作状态读取超过服务端截止时间，结果未知"
        )
      );
    }, timeoutMs);
    advancedOperationRequestTimers.set(action, timeout);
    request.then(
      response => {
        if (settled) return;
        settled = true;
        window.clearTimeout(timeout);
        if (advancedOperationRequestTimers.get(action) === timeout) advancedOperationRequestTimers.delete(action);
        resolve(response);
      },
      error => {
        if (settled) return;
        settled = true;
        window.clearTimeout(timeout);
        if (advancedOperationRequestTimers.get(action) === timeout) advancedOperationRequestTimers.delete(action);
        reject(error);
      }
    );
  });
}

function scheduleAdvancedOperationPoll(
  action: string,
  operationId: string,
  channelId: number,
  token: number,
  contextKey: string
) {
  const context = advancedOperationPollContexts.get(action);
  if (!context || context.operationId !== operationId) return;
  const existing = advancedOperationTimers.get(action);
  if (existing !== undefined) window.clearTimeout(existing);
  const delay =
    context.deadlineMs === null
      ? ADVANCED_OPERATION_POLL_INTERVAL_MS
      : Math.max(0, Math.min(ADVANCED_OPERATION_POLL_INTERVAL_MS, context.deadlineMs - Date.now()));
  const timerId = window.setTimeout(() => {
    advancedOperationTimers.delete(action);
    void pollAdvancedOperation(action, operationId, channelId, token, contextKey);
  }, delay);
  advancedOperationTimers.set(action, timerId);
}

async function pollAdvancedOperation(action: string, operationId: string, channelId: number, token: number, contextKey: string) {
  if (
    !canControlDevice.value ||
    !isCurrentChannelContext(channelId, token, contextKey) ||
    !hasCurrentAdvancedOperationPoll(action, operationId)
  )
    return;
  const activeToken = advancedStatusToken.value;
  try {
    const response = await getAdvancedOperationWithInsurance(action, channelId, operationId);
    if (
      !isCurrentChannelContext(channelId, token, contextKey) ||
      activeToken !== advancedStatusToken.value ||
      !hasCurrentAdvancedOperationPoll(action, operationId)
    )
      return;
    if (response.code !== 0 || !response.data || response.data.operationId !== operationId) {
      advancedOperationUnknown(action, "操作状态不匹配,结果未知");
      return;
    }
    const operation = response.data;
    advancedOperationPhase.value = { ...advancedOperationPhase.value, [action]: operation.status };
    const deadlineMs = updateAdvancedOperationPollDeadline(action, operationId, operation.deadlineAt);
    if (deadlineMs !== null) {
      advancedOperationDeadline.value = { ...advancedOperationDeadline.value, [action]: new Date(deadlineMs).toISOString() };
    }
    if (operation.status === "sent" && !advancedActionRequiresDeviceResult(action, operation.responseRequired)) {
      clearAdvancedOperationPoll(action);
      setAdvancedPending(action, false);
      setAdvancedPhase(action, "sent", "已下发（该命令无需设备回执）");
      return;
    }
    if (operation.status === "queued" || operation.status === "sent") {
      if (deadlineMs === null) {
        advancedOperationUnknown(action, "服务端未返回操作截止时间，补读一次后结果未知");
        return;
      }
      if (Date.now() >= deadlineMs) {
        advancedOperationUnknown(action, "操作超过服务端截止时间，结果未知");
        return;
      }
      setAdvancedPending(action, true);
      scheduleAdvancedOperationPoll(action, operationId, channelId, token, contextKey);
      return;
    }
    clearAdvancedOperationPoll(action);
    setAdvancedPending(action, false);
    if (operation.status === "accepted") {
      if (!advancedActionRequiresDeviceResult(action, operation.responseRequired)) {
        setAdvancedPhase(action, "sent", "已下发（该命令无需设备回执）");
        return;
      }
      applyAdvancedAccepted(action);
      return;
    }
    advancedOperationError(action, operation.status, operation);
  } catch (error: any) {
    if (
      !isCurrentChannelContext(channelId, token, contextKey) ||
      activeToken !== advancedStatusToken.value ||
      !hasCurrentAdvancedOperationPoll(action, operationId)
    )
      return;
    advancedOperationUnknown(action, error?.message || "操作状态读取失败,结果未知");
  }
}

/**
 * 控制台里"要等设备结论"的动作名单 —— **现在是空的**。
 *
 * ⛔ 不要照抄 `record_start / guard_set / alarm_reset` 回来：那五个动作 2026-09-20 已搬到
 *    设备详情抽屉的「设备控制」页，判定也在那边（`DeviceControlPanel.vue` 的 operation 轮询）。
 *    控制台只剩 `iframe` / `drag_zoom_*`，它们是**送达即止**的 —— 附录 A 没有对应的查询命令，
 *    设备执行结果平台本来就不可能知道。
 *    ⛔ 别把"HTTP 200"写成"已生效"。
 *    ⭐ 但状态词也**不许**说"设备执行结果未回传"（2026-09-20 改）：那是否定式，读起来像
 *    "该收的没收到"，用户会以为下发失败、要重试 —— 而事实是这类命令协议里**根本没有应答**。
 *    统一说"已下发（该命令无需设备回执）"，把"没有回传"从缺陷翻成协议事实。
 */
const ACTIONS_REQUIRING_DEVICE_RESULT: string[] = [];

/**
 * 这个动作需不需要等设备结论：服务端给了 `responseRequired` 就以它为准，没给时按动作名兜底
 * （见上面那张名单，当前为空）。
 */
function advancedActionRequiresDeviceResult(action: string, responseRequired?: boolean) {
  if (typeof responseRequired === "boolean") return responseRequired;
  return ACTIONS_REQUIRING_DEVICE_RESULT.includes(action);
}

function dragZoomPlaybackRect(layer: HTMLElement) {
  const playbackElement = layer.parentElement?.querySelector<HTMLElement>(".play-window");
  const playbackRect = playbackElement?.getBoundingClientRect();
  if (playbackRect && playbackRect.width > 0 && playbackRect.height > 0) return playbackRect;
  return layer.getBoundingClientRect();
}

function pointInDragLayer(event: PointerEvent) {
  const layer = event.currentTarget as HTMLElement;
  const rect = dragZoomPlaybackRect(layer);
  return {
    x: Math.max(0, Math.min(1, (event.clientX - rect.left) / Math.max(rect.width, 1))),
    y: Math.max(0, Math.min(1, (event.clientY - rect.top) / Math.max(rect.height, 1)))
  };
}

/**
 * 退出拉框态（再点同向按钮 / Esc / 控制权被收回 / 会话结束 / 最小化 —— 共用这一处）。
 *
 * ⭐ 2026-09-20：**下完一次框选不再自动退出**。一次拉框常常不够 —— 操作员要先看清
 *    放大结果、再决定往哪补一刀；旧行为逼着他每次回侧栏重按一遍按钮。
 *    代价是"退出"变成**显式动作**，所以它必须处处可达、且只有一处实现：
 *    谁也不许再把 `dragZoomMode` 单独置回 false（漏掉拖拽残留 —— 起点 / 指针 id ——
 *    下一次进拉框态就会画出一个凭空出现的框）。
 */
function exitDragZoomMode() {
  dragZoomMode.value = false;
  dragZoomStart.value = null;
  dragZoomCurrent.value = null;
  dragZoomPointerId = null;
}

function toggleDragZoomMode(action: "drag_zoom_in" | "drag_zoom_out") {
  if (!canControlDevice.value) return;
  if (dragZoomMode.value && dragZoomAction.value === action) {
    exitDragZoomMode();
    return;
  }
  // ⛔ 画面上"拖一把"的模式现在有**四个**（拉框变焦 / 遮挡框选 / OSD 调位置 / 目标跟踪框选）：
  //    同一个按下动作在两层里各有一套解释，用户没法预期、平台也说不清画出来的是哪一个。
  //    进拉框前把另三个关掉；反方向由 startMaskDraw / enterOsdEditMode / toggleTargetTrackMode
  //    各自负责（见那三处）—— 四处一起构成闭环，加第五个模式时四处都要改。
  if (maskDrawMode.value) cancelMaskDraw();
  if (osdEditMode.value) exitOsdEditMode();
  if (targetTrackMode.value) exitTargetTrackMode();
  if (maskDrawMode.value) cancelMaskDraw();
  if (osdEditMode.value) exitOsdEditMode();
  dragZoomAction.value = action;
  dragZoomMode.value = true;
  dragZoomStart.value = null;
  dragZoomCurrent.value = null;
  dragZoomPointerId = null;
}

function beginDragZoom(event: PointerEvent) {
  if (!dragZoomMode.value || event.button !== 0) return;
  // ⭐ 拉框态现在**跨多次框选持续**，于是会撞上"上一次还在下发就拖第二次"：
  //    `runAdvancedAction` 对同 action 的并发请求是**静默丢弃**的
  //    （`if (isAdvancedPending(action)) return`），不在这里挡住的话，用户会画出一个
  //    跟着消失、命令却没有的框。此时提示条已经在说"正在下发…"，所以拦得下来。
  if (isAdvancedPending(dragZoomAction.value)) return;
  if (dragZoomPointerId !== null && dragZoomPointerId !== event.pointerId) return;
  const layer = event.currentTarget as HTMLElement;
  const point = pointInDragLayer(event);
  dragZoomPointerId = event.pointerId;
  dragZoomStart.value = point;
  dragZoomCurrent.value = point;
  layer.setPointerCapture?.(event.pointerId);
}

function updateDragZoom(event: PointerEvent) {
  if (!dragZoomStart.value || dragZoomPointerId !== event.pointerId) return;
  dragZoomCurrent.value = pointInDragLayer(event);
}

function cancelDragZoom(event?: PointerEvent) {
  if (event && dragZoomPointerId !== null && dragZoomPointerId !== event.pointerId) return;
  dragZoomStart.value = null;
  dragZoomCurrent.value = null;
  dragZoomPointerId = null;
}

async function finishDragZoom(event: PointerEvent) {
  if (!canControlDevice.value || !dragZoomStart.value || dragZoomPointerId !== event.pointerId) return;
  updateDragZoom(event);
  const layer = event.currentTarget as HTMLElement;
  const rect = dragZoomPlaybackRect(layer);
  const start = dragZoomStart.value;
  const current = dragZoomCurrent.value || start;
  dragZoomStart.value = null;
  dragZoomCurrent.value = null;
  dragZoomPointerId = null;
  const left = Math.min(start.x, current.x);
  const top = Math.min(start.y, current.y);
  const width = Math.abs(current.x - start.x);
  const height = Math.abs(current.y - start.y);
  if (width < 0.02 || height < 0.02) {
    Message.warning("拖框范围太小,请重新选择");
    return;
  }
  // DragZoom coordinates are defined in the actual displayed window, not source video metadata.
  const length = Math.max(1, Math.round(rect.width));
  const windowWidth = Math.max(1, Math.round(rect.height));
  const action = dragZoomAction.value;
  // 兜底（正常由 `beginDragZoom` 挡住）：拖的途中上一次下发才刚变成 pending 时别静默丢命令。
  if (isAdvancedPending(action)) {
    Message.warning("上一次框选还在下发中,请稍候再拖");
    return;
  }
  await runAdvancedAction(action, {
    length,
    width: windowWidth,
    midPointX: Math.round((left + width / 2) * length),
    midPointY: Math.round((top + height / 2) * windowWidth),
    lengthX: Math.max(1, Math.round(width * length)),
    lengthY: Math.max(1, Math.round(height * windowWidth))
  });
  // ⭐ 这里**刻意不退**出拉框态：连拉两刀是常态（先放大看结果、再决定往哪补）。
  //    退出只走 `exitDragZoomMode` 那几条显式路径，别把这条加回来。
}

function onDragZoomKeydown(event: KeyboardEvent) {
  if (dragZoomMode.value && consumeCanvasEscape(event)) exitDragZoomMode();
}

// Esc 只在拉框态期间接管：常驻监听会跟弹窗自己的 Esc 行为打架（与 OSD 编辑同一个理由）。
//
// ⛔⛔ 光挂一个监听**不够** —— 控制台自己就是个 `a-modal`（`esc-to-close`），Arco 另在
//    `document.documentElement` 上挂了一份**全局** keydown。两边是**并行**监听，
//    不是"谁冒泡到谁"：控制台是最上层弹窗时，那一下 Esc 会先把**控制台整个关掉**
//    （`requestClose` 还会弹"改动未下发"确认），用户看到的现象就是"按 Esc 弹窗直接没了"。
//    ⇒ 靠**两层**挡住（真机实测过，见 `consumeCanvasEscape` 的 KDoc）：
//      ① 图层监听 `stopPropagation()` 把这一下 Esc 从事件流里拿掉（**主闸门**）；
//      ② 模板上模式期间 `esc-to-close=false`（兜底，覆盖"图层没吃到"的边角情况）。
//    只做 ② 会失效：我们退出模式后 `escToClose` 在**同一个事件派发内**就变回 `true`，
//    Arco 随后才跑、读到 `true` ⇒ 照关。
watch(dragZoomMode, (active, _previous, onCleanup) => {
  if (!active) return;
  // ⛔ 第三个参数 `true` = capture，别删：要在弹窗把自己关掉**之前**判"上面有没有弹窗"，
  //    见 `consumeCanvasEscape`（晚了必然漏判，那一下 Esc 会连拉框态一起收掉）。
  window.addEventListener("keydown", onDragZoomKeydown, true);
  onCleanup(() => window.removeEventListener("keydown", onDragZoomKeydown, true));
});

// ⛔ 控制权没了（设备转了离线 / 换到没 `device:control` 的通道）就退出拉框态：出口按钮挂在
//    `canControlDevice` 上、侧栏整块也随权限消失，留在拉框态就是**画面被一层吃事件的膜盖住、
//    却找不到任何退出入口**。这与侧栏那两处"自己挡 canControlDevice"的注释是同一条约束。
watch(canControlDevice, available => {
  if (!available && dragZoomMode.value) exitDragZoomMode();
  if (!available && targetTrackMode.value) exitTargetTrackMode();
});

/* ─────────── 目标跟踪（GB/T 28181-2022 A.2.3.1.14） ─────────── */

/**
 * 目标跟踪的三件事，都**只有在这块画面上**做得成，所以它长在控制台而不是设备详情抽屉里：
 *
 *   1. 手动跟踪要求平台给出「播放窗口长度/宽度像素值」（A.2.3.1.14 注释原文），
 *      而"播放窗口"就是这里的 `.play-window` 渲染出来的那块矩形；
 *   2. 框是操作员**在画面上圈目标**圈出来的，换一个页面就没有同一块画面可比对；
 *   3. 控件的可用性依赖 `canControlDevice` 与实时画面是否起播，与侧栏 PTZ 同族。
 *
 * ## ⛔⛔ 措辞纪律：这是本能力最容易写错的地方
 *
 * 目标跟踪在 9.3.1 d) 里被明列为**无应答命令**，表 1 序号 13 的"应答"一栏写作「（无）」；
 * 而且 2022 全文里**没有**任何"目标跟踪状态查询/上报"的命令（对比：存储卡格式化至少
 * 还能事后再查一次 SDCardStatus 做对账）。两个后果必须写进界面：
 *
 *   - `status=sent` **就是终态**，别去轮询 operation 等它变成 accepted —— 它不会变，
 *     用户会一直等一个永远不来的回执（同族的 `ACTIONS_REQUIRING_DEVICE_RESULT` 名单为空，
 *     但那套机制管不到本路由，本路由压根不经过 `runAdvancedAction`）。
 *   - 屏幕上的文字只能是「已下发（设备未回执）」，**不许**写「正在跟踪 XX」——
 *     平台根本无从得知设备在跟踪什么，写出来就是在承诺一个查不到的事实。
 */
const targetTrackMode = ref(false);
const targetTrackStart = ref<{ x: number; y: number } | null>(null);
const targetTrackCurrent = ref<{ x: number; y: number } | null>(null);
let targetTrackPointerId: number | null = null;
const targetTrackPending = ref(false);
/** 平台**最近一次下发**的意图（不是"设备现在在跟踪什么"，见上面的措辞纪律）。 */
const targetTrackIntent = ref<TargetTrackIntent | null>(null);
const targetTrackLoaded = ref(false);
const targetTrackError = ref("");
/** 最近一次下发的结论文案，单独一行显示（成功/失败都说清是"下发"而不是"生效"）。 */
const targetTrackStatus = ref("");

const targetTrackBoxStyle = computed(() => {
  if (!targetTrackStart.value || !targetTrackCurrent.value) return {};
  const left = Math.min(targetTrackStart.value.x, targetTrackCurrent.value.x);
  const top = Math.min(targetTrackStart.value.y, targetTrackCurrent.value.y);
  const width = Math.abs(targetTrackCurrent.value.x - targetTrackStart.value.x);
  const height = Math.abs(targetTrackCurrent.value.y - targetTrackStart.value.y);
  return { left: `${left * 100}%`, top: `${top * 100}%`, width: `${width * 100}%`, height: `${height * 100}%` };
});

/**
 * 退出「框选目标」态。
 *
 * ⭐ 与拉框变焦不同，这里**下完一次就退出**：跟踪是"选定一个目标"，不是"连续调整"。
 *    留着态会让用户以为还要再框第二刀，而第二条手动跟踪指令会直接覆盖第一条的框。
 */
function exitTargetTrackMode() {
  targetTrackMode.value = false;
  targetTrackStart.value = null;
  targetTrackCurrent.value = null;
  targetTrackPointerId = null;
}

/**
 * 进入 / 退出框选态。
 *
 * ⛔ 必须与另三个"画面上拖"的模式互斥（拉框变焦 / 遮挡框选 / OSD 调位置）：
 *    同一个按下动作在两层里各有一套解释，用户没法预期、平台也说不清画出来的是哪一个。
 *    反方向由那三处各自负责关掉本模式（`toggleDragZoomMode` / `startMaskDraw` /
 *    `enterOsdEditMode`）—— 四处一起构成闭环，加第五个模式时四处都要改。
 */
function toggleTargetTrackMode() {
  if (!canControlDevice.value) return;
  if (targetTrackMode.value) {
    exitTargetTrackMode();
    return;
  }
  exitDragZoomMode();
  exitOsdEditMode();
  cancelMaskDraw();
  targetTrackMode.value = true;
  targetTrackStart.value = null;
  targetTrackCurrent.value = null;
  targetTrackPointerId = null;
}

function beginTargetTrackDraw(event: PointerEvent) {
  if (!targetTrackMode.value || event.button !== 0) return;
  if (targetTrackPending.value) return;
  if (targetTrackPointerId !== null && targetTrackPointerId !== event.pointerId) return;
  const layer = event.currentTarget as HTMLElement;
  const point = pointInDragLayer(event);
  targetTrackPointerId = event.pointerId;
  targetTrackStart.value = point;
  targetTrackCurrent.value = point;
  layer.setPointerCapture?.(event.pointerId);
}

function updateTargetTrackDraw(event: PointerEvent) {
  if (!targetTrackStart.value || targetTrackPointerId !== event.pointerId) return;
  targetTrackCurrent.value = pointInDragLayer(event);
}

function cancelTargetTrackDraw(event?: PointerEvent) {
  if (event && targetTrackPointerId !== null && targetTrackPointerId !== event.pointerId) return;
  targetTrackStart.value = null;
  targetTrackCurrent.value = null;
  targetTrackPointerId = null;
}

/**
 * 画完 → 换算 → 下发 `Manual`。
 *
 * ⛔ 换算全程走 `buildTargetTrackArea`（纯函数，配了单测），不在模板/事件里就地算：
 *    目标是**无应答命令**，落点算错在设备侧完全不可观测 —— 平台看到的是"已下发"，
 *    而画面上什么也没跟踪上，连"错了"这个信号都不存在。
 */
async function finishTargetTrackDraw(event: PointerEvent) {
  if (!canControlDevice.value || !targetTrackStart.value || targetTrackPointerId !== event.pointerId) return;
  updateTargetTrackDraw(event);
  const layer = event.currentTarget as HTMLElement;
  const rect = dragZoomPlaybackRect(layer);
  const start = targetTrackStart.value;
  const current = targetTrackCurrent.value || start;
  // ⛔ 先收干净拖拽状态再下发（与拉框变焦同一条）：留在半路状态上的话，
  //    下发失败后用户看到的是一个"停在那儿的框"，会以为它已经发出去了。
  targetTrackStart.value = null;
  targetTrackCurrent.value = null;
  targetTrackPointerId = null;

  const built = buildTargetTrackArea({
    start,
    end: current,
    // ⛔ 基准是**画面渲染矩形**（= 标准的"播放窗口像素值"），不是视频原始分辨率。
    renderedWidth: rect.width,
    renderedHeight: rect.height
  });
  if (!built.ok) {
    Message.warning(built.reason);
    return;
  }
  // 选定目标即退出框选态：跟踪是"选一个目标"，不是"连续调整"（见 exitTargetTrackMode）。
  exitTargetTrackMode();
  await submitTargetTrack("Manual", built.area);
}

async function submitTargetTrack(mode: TargetTrackMode, area?: TargetTrackArea) {
  const channel = props.channel;
  if (!canControlDevice.value || !channel) return;
  if (targetTrackPending.value) return;
  const token = sessionToken;
  const channelId = channel.id;
  targetTrackPending.value = true;
  targetTrackError.value = "";
  targetTrackStatus.value = "";
  try {
    const response = await setChannelTargetTrack(channelId, {
      mode,
      ...(area ? { area } : {}),
      idempotencyKey: `${channelId}-target-track-${mode}-${Date.now()}`
    });
    if (token !== sessionToken || props.channel?.id !== channelId) return;
    if (response.code !== 0) throw new Error(response.message || "下发目标跟踪失败");
    // 同一个响应里带回落库后的意图：不必再打一次读接口，也就没有"下发成功但界面还挂着上一条"的中间态。
    if (response.data?.intent !== undefined) {
      targetTrackIntent.value = response.data.intent ?? null;
      targetTrackLoaded.value = true;
    }
    // ⛔ 不轮询：`status=sent` 就是这条命令的终态（无应答）。文案也只说到"已下发"。
    targetTrackStatus.value = `${targetTrackModeText(mode)}${
      area ? `（框 ${area.lengthX}×${area.lengthY} @ ${area.midPointX},${area.midPointY}）` : ""
    }已下发，设备未回执（该命令无应答，平台无法得知设备实际状态）`;
    Message.info("已下发（该命令无需设备回执）");
  } catch (submitError: any) {
    if (token !== sessionToken) return;
    targetTrackError.value = submitError?.message || "下发目标跟踪失败";
  } finally {
    if (token === sessionToken) targetTrackPending.value = false;
  }
}

/** 读平台**已下发**的意图。纯本地读，不产生 SIP 报文，所以不走 pending 态。 */
async function loadTargetTrack(channelId = props.channel?.id, token = sessionToken) {
  if (!canViewPtz.value || !channelId) return;
  try {
    const response = await getChannelTargetTrack(channelId);
    if (token !== sessionToken || props.channel?.id !== channelId) return;
    if (response.code !== 0 || !response.data) {
      targetTrackError.value = response.message || "读取目标跟踪指令失败";
      return;
    }
    targetTrackError.value = "";
    targetTrackLoaded.value = true;
    targetTrackIntent.value = response.data.intent ?? null;
  } catch {
    if (token !== sessionToken) return;
    targetTrackError.value = "读取目标跟踪指令失败";
  }
}

function targetTrackModeText(mode: TargetTrackMode) {
  if (mode === "Auto") return "自动跟踪";
  if (mode === "Manual") return "手动跟踪";
  return "停止跟踪";
}

/**
 * 界面上那句"平台最近一次下发的是什么"。
 *
 * ⛔ 主语永远是**平台**（"平台已下发…"），不是设备。写成"设备正在…"就是在替设备说话，
 *    而这条命令既没有应答也没有查询手段 —— 那句话永远无法被证伪，也永远无法被发现是错的。
 */
const targetTrackIntentText = computed(() => {
  if (targetTrackError.value) return "读取失败";
  if (!targetTrackLoaded.value) return "尚未读取平台已下发的指令";
  const intent = targetTrackIntent.value;
  if (!intent) return "平台还没有向这台设备下发过目标跟踪";
  const text = `平台最近一次下发：${targetTrackModeText(intent.mode as TargetTrackMode)}`;
  const box = targetTrackBoxText(intent);
  return box ? `${text}（${box}）` : text;
});

/** 意图里的框选坐标文本；`Auto`/`Stop` 下六项整体缺席（不是 0），所以返回空串。 */
function targetTrackBoxText(intent: TargetTrackIntent) {
  const { areaLengthX, areaLengthY, areaMidPointX, areaMidPointY } = intent;
  if (areaLengthX == null || areaLengthY == null) return "";
  return `框 ${areaLengthX}×${areaLengthY} @ ${areaMidPointX ?? 0},${areaMidPointY ?? 0}`;
}

/* ─────────── 画面设置底栏卡片：遮挡框选 + 镜像 ─────────── */

/**
 * 画面设置页签的两张卡片。
 *
 * ⛔ 卡片**不是第二份状态**：`deviceConfigRef` 暴露的读写口直接落在 DeviceConfigDrawer 的
 *    `familyValues.picture` 上，脏值统计、下发、回读对账全部复用那一套。
 * ⛔ 遮挡坐标的参考系是**画面真实解码尺寸**（播放器 `getVideoInfo()`）。通道目录里声明的
 *    码流分辨率与 `video-params` 回读的码值在同一通道上会互相矛盾，而用户是在画面上画的 ——
 *    只有解码尺寸与他在屏幕上看到的东西同源。
 */

/** 播放器报出的画面真实解码尺寸；未起播时为 null。 */
const playerVideoSize = ref<{ width: number; height: number } | null>(null);

function onPlayerVideoSize(size: { width: number; height: number } | null) {
  playerVideoSize.value = size;
}

/** 遮挡框选模式：点「新建」进入，画完或 Esc 退出。 */
const maskDrawMode = ref(false);
/** 本次绘制要写入的槽位编号（1~4）。 */
const maskDrawSeq = ref(0);
const maskDrawStart = ref<{ x: number; y: number } | null>(null);
const maskDrawCurrent = ref<{ x: number; y: number } | null>(null);
let maskDrawPointerId: number | null = null;

const pictureRegions = computed<PictureRegionSlot[]>(() => deviceConfigRef.value?.pictureRegions ?? []);
const pictureEditable = computed(() => deviceConfigRef.value?.pictureEditable === true);
const pictureMaskOn = computed(() => deviceConfigRef.value?.pictureMaskOn === true);
const pictureAppliedMaskOn = computed(() => deviceConfigRef.value?.pictureAppliedMaskOn === true);
const pictureRetainedRegionCount = computed(() => deviceConfigRef.value?.pictureRetainedRegionCount ?? 0);
const pictureMaskDraftTouched = computed(() => deviceConfigRef.value?.pictureMaskDraftTouched === true);

/**
 * 总闸的**草稿值**与**设备事实**不一致 ⇒ 这次开关改动还没下发到设备。
 *
 * ⛔ 卡片上那个开关按钮显示的是**草稿**（用户点了就该有反馈，否则像点不动），
 *    但它旁边那行字极易被读成"设备现在就是这样"。2026-09-19 现场：用户只点了「启用」、
 *    没点画布下方的「下发」，看到按钮写着"已启用"就以为设备已经生效了 —— 而设备落库
 *    仍是 `{"on":0}`。
 *    ⇒ pending 时文案必须换成「将启用 / 将停用」，把"还没到设备"写在字面上，
 *      只靠颜色或悬浮提示不够（用户不会去 hover）。
 */
const pictureMaskPending = computed(() => pictureMaskOn.value !== pictureAppliedMaskOn.value);

/** 开关按钮文案。pending 时说「将…」——「已…」只能用在设备真的已经是那个状态时。 */
const pictureMaskSwitchText = computed(() => {
  if (pictureMaskPending.value) return pictureMaskOn.value ? "将启用" : "将停用";
  return pictureMaskOn.value ? "已启用" : "已停用";
});

/** 开关按钮悬浮提示：pending 时必须给出"下一步点哪儿"，否则用户只知道没生效。 */
const pictureMaskSwitchTitle = computed(() => {
  if (pictureMaskPending.value) {
    return `已改为${pictureMaskOn.value ? "启用" : "停用"}，还没写到设备 —— 点画布下方「下发」才生效`;
  }
  return pictureMaskOn.value ? "遮挡当前已启用，点击停用" : "遮挡当前已停用，点击启用";
});

/**
 * 卡片处于「已停用、但设备保留着区域」的说明态。
 *
 * ⛔ 抽成一个名字而不是在模板里重复写两个条件的与：头部计数徽章与正文说明块
 *    必须**同时**切换，否则会出现"徽章写着 1、正文说已停用"的自相矛盾。
 */
const pictureRetainedMode = computed(() => pictureRetainedRegionCount.value > 0 && !pictureMaskDraftTouched.value);
const pictureMirror = computed(() => deviceConfigRef.value?.pictureMirror ?? "0");

/**
 * 设备**声明的图像坐标画布** —— 遮挡区 `Point` 的基准。
 *
 * ⭐ 2026-09-19 真机定因（海康 IPC，主码流 2560×1440）：遮挡坐标的基准**不是**画面解码
 * 尺寸，而是设备在 `OSDConfig` 里声明的 `Length/Width`（该机 704×576，且**拒收**平台改写）。
 * 抓图量证：发 `Point=0,0,640,360` ⇒ 黑块落在 x 0~90.8% / y 0~62.6%（= 640/704、360/576）。
 * ⛔ 拿解码尺寸当基准，遮挡块会整体右移放大 —— 就是"我画的框挡住的是别的地方"。
 *
 * 同时确认过：**与镜像无关**（`FrameMirror` 2=上下 / 1=左右 / 0 关闭，遮挡都停在同一个
 * 屏幕位置，只有画面内容自己翻）。
 */
const pictureCanvasSize = computed(() => deviceConfigRef.value?.pictureCanvasSize ?? null);

/**
 * 换算遮挡坐标用的基准：设备声明的画布优先；**没读到就退回画面解码尺寸**。
 *
 * ⛔ 为什么不"读不到就禁止框选"：返回值可能是另一套设备的合法事实 ——
 *    自研模拟器（`uvp-gb28181-sim`）把遮挡烧进 GL FBO，FBO 尺寸**就是编码尺寸**，
 *    它的基准确实等于解码尺寸。所以这里必须给得出去，但**必须同时说明基准是什么**
 *    （见 [pictureCoordinateText]），否则用户分不清"画上去就准"和"画上去碰运气"。
 */
const maskCanvasSize = computed(() => pictureCanvasSize.value ?? playerVideoSize.value);
/** 基准是不是设备声明的（false = 退回了画面尺寸，落点有风险）。 */
const maskCanvasFromDevice = computed(() => pictureCanvasSize.value !== null);
/** 基准的短文本（`704×576`），给框选提示条这类窄地方用。 */
const maskCanvasText = computed(() => {
  const canvas = maskCanvasSize.value;
  return canvas ? `${canvas.width}×${canvas.height}` : "—";
});
const pictureDirtyCount = computed(() => deviceConfigRef.value?.pictureDirtyCount ?? 0);
const pictureFactsMissing = computed(() => deviceConfigRef.value?.pictureFactsMissing === true);
const pictureAbsentTypes = computed<string[]>(() => deviceConfigRef.value?.pictureAbsentTypes ?? []);
const pictureError = computed(() => deviceConfigRef.value?.pictureError ?? "");
const pictureMaskNotice = computed(() => deviceConfigRef.value?.pictureMaskNotice ?? "");
const pictureCanAddRegion = computed(() => deviceConfigRef.value?.pictureCanAddRegion === true);
const pictureUsedCount = computed(() => pictureRegions.value.filter(region => region.used).length);

/** 本次草稿改到了哪几个字段（`mask1`..`mask4` / `maskOn` / `mirror`）。 */
const pictureDirtyFields = computed<string[]>(() => deviceConfigRef.value?.pictureDirtyFields ?? []);

/* ─────────── 图像叠加（OSD）的画布锚点层 ───────────
 *
 * 形态取**锚点 + 内容标签**，不是"像真字的预览"。三条依据：
 *
 * 1. ⛔ 标准 `OSDCfgType`（A.2.1.12）9 个字段里**没有**字体、字号、颜色、对齐 ——
 *    画一个看起来像真的字，等于承诺平台给不了的能力，用户下一条就会问"字号能不能调"。
 * 2. ⛔ 设备**已经烧进码流**的时间戳就在播放器画面里。再叠一个假字 = 画面上
 *    **两个时间戳**，是最难解释的一类假象。
 * 3. ⭐ 锚点有一笔意外收益：设备那个真字就在眼前，用户可以**直接对着它拖锚点对齐**
 *    —— 免费的精确校准，而"假字"方案做不到（假字会把真字盖住）。
 */
const osdEditable = computed(() => deviceConfigRef.value?.osdEditable === true);
const osdAnchors = computed<OsdAnchorSlot[]>(() => deviceConfigRef.value?.osdAnchors ?? []);
const osdDirtyCount = computed(() => deviceConfigRef.value?.osdDirtyCount ?? 0);
const osdDirtyFields = computed<string[]>(() => deviceConfigRef.value?.osdDirtyFields ?? []);
const osdUnplacedCount = computed(() => deviceConfigRef.value?.osdUnplacedCount ?? 0);
const osdFactsMissing = computed(() => deviceConfigRef.value?.osdFactsMissing === true);
const osdError = computed(() => deviceConfigRef.value?.osdError ?? "");
const osdFocusToken = computed(() => deviceConfigRef.value?.osdFocusToken ?? null);

/**
 * OSD 锚点的**编辑模式**开关（默认关），由侧栏「时间戳」面板里的按钮驱动
 * （抽屉的 `:osd-editing` 只读显示开关态、`@toggle-osd-edit` 只发意图，状态留在这里）。
 *
 * ⛔ 默认必须是关的：锚点层盖着的就是播放器 —— 如果随时可拖，用户想点一下画面
 *    （暂停 / 全屏 / 起手拉框变焦）就可能顺手把某行字挪走。这类误操作**在画面上看不出来**
 *    （真实效果由设备决定，平台不渲染真字），只会等到下发后设备上真的换了位置才被发现。
 *    ⇒ 拖动是**显式动作**：先在侧栏点「调整位置」把自己放进编辑模式。
 *
 * ⛔⛔ 关掉时**连锚点都不画**（2026-09-20 老板第二次反馈："这个为啥还是默认显示呢，
 *    不是说的点击调整位置之后才出现吗…… 现在这个 OSD 过于混乱"）。
 *    第一版做的是"锚点照画、只降噪"，理由是"位置信息不该因为不可操作就消失" —— 这条
 *    在**侧栏**里由 `X 289 · Y 256` 那行数字已经履行了，画面上再摊四五个带引线的标签
 *    就是纯噪声，而且它盖住的正是要看的视频。⇒ 画面上的标记只服务于"正在摆位置"这件事。
 */
const osdEditMode = ref(false);

/** 拖拽状态：`key` = 正在拖的锚点（空串 = 没在拖）；`pos` = 拖动中的实时**画布像素**。 */
const osdDragKey = ref("");
const osdDragPos = ref<{ x: number; y: number } | null>(null);
let osdDragMoved = false;

/** 锚点层本身：拖动换算要拿它的 `.play-window` 命中矩形做基准。 */
const osdLayerRef = ref<HTMLElement | null>(null);

/** 侧栏点「在画面上定位」→ 对应锚点闪一下，把视线引过去。 */
const osdFocusKey = ref("");
let osdFocusTimer: number | undefined;

watch(osdFocusToken, token => {
  if (!token) return;
  osdFocusKey.value = token.kind === "time" ? "time" : `item-${token.index}`;
  // ⭐ 侧栏那个「在画面上拖动」**本身就是显式意图**（用户已经点名要挪这一行），
  //    所以顺手进入编辑模式 —— 否则会出现"点了按钮、锚点闪了一下、却拖不动"，
  //    而用户刚刚才被告知"拖动即可改位置"。
  enterOsdEditMode();
  if (osdFocusTimer !== undefined) window.clearTimeout(osdFocusTimer);
  osdFocusTimer = window.setTimeout(() => {
    osdFocusKey.value = "";
  }, 1400);
});

/**
 * 坐标 → 画布内的百分比。
 *
 * ⛔ 超出画布的值要**夹住**：设备回读的坐标可能比它自己声明的画布还大，不夹的话
 *    锚点会画到画面外面，表现出来就是"这行字不见了"（而配置其实是有的）。
 */
function canvasRatio(value: number, max: number): number {
  if (!Number.isFinite(max) || max <= 0) return 0;
  return Math.max(0, Math.min(1, (Number(value) || 0) / max)) * 100;
}

const osdOverlayAnchors = computed(() => {
  // ⛔ 没读到设备事实时**一个锚点都不画**：`familyValues` 里躺着的是平台的空白模板
  //    （`timeX = 10` 这类），画出来等于把平台初值摆成"设备此刻就是这样"。
  if (osdFactsMissing.value) return [];
  // ⛔ 与遮挡**同一把尺**（`maskCanvasSize`）：OSD 的 `Length/Width` 本来就是遮挡坐标的
  //    基准，在这里另造第二个基准，同一屏上两个覆盖层就会各按各的尺画。
  const size = maskCanvasSize.value;
  if (!size) return [];
  return osdAnchors.value.map(anchor => {
    // 拖动中的那一个用实时值：`pointermove` 期间**不写草稿**（脏值统计会一路抖动），
    // 但用户必须看到它跟着手指走。
    const live = osdDragKey.value === anchor.key ? osdDragPos.value : null;
    const x = live ? live.x : Number(anchor.x) || 0;
    const y = live ? live.y : Number(anchor.y) || 0;
    return {
      ...anchor,
      dragging: osdDragKey.value === anchor.key,
      coords: `${Math.round(x)}, ${Math.round(y)}`,
      style: {
        left: `${canvasRatio(x, size.width)}%`,
        top: `${canvasRatio(y, size.height)}%`
      } as CSSProperties
    };
  });
});

/**
 * 锚点的悬浮说明。
 *
 * ⛔ 那句"平台无法预览真实效果"必须在这里：标量只定义「位置 + 内容 + 开关」，
 *    字体字号由设备决定 —— 说成"这就是效果"就是在承诺平台给不了的能力。
 *
 * ⭐ 不再按编辑模式分支：这一层**只在编辑模式存在**（`v-if="osdEditMode"`），
 *    那句"点哪里哪里才能拖"的只读态文案连同它的载体一起没了。
 *
 * ⛔ 曾经这里挂过一段"首次引导"（`localStorage` 记一次、按钮闪三下）。按钮搬进侧栏
 *    「时间戳」面板的「位置」行之后**删掉了**：它当时要救的是"按钮在画面角落、看不见
 *    也就不存在"，而现在按钮就贴在它控制的那个坐标读数旁边，标签自己会说话。
 *    再加一层闪烁只是又一处要解释、要换存储键、还会跟模式对不上的东西。
 */
function osdAnchorTitle(anchor: OsdAnchorSlot): string {
  const head = anchor.kind === "time" ? "时间戳" : `文字：${anchor.label}`;
  const state = anchor.unplaced ? "（还没定位）" : anchor.draft ? "（待下发）" : "";
  return [
    `${head}${state}`,
    `拖动可改位置 —— 落点写的是设备画布坐标 (${anchor.x}, ${anchor.y})。`,
    "标记只表示位置，字体与字号由设备自己决定，平台无法预览真实效果。"
  ].join("\n");
}

/**
 * 浮条主文案：把"几项"翻成人话（`时间戳位置 · 2 条文字 · 遮挡 1 处`）。
 *
 * ⛔ 别写「待下发 N 项」就完事：N=1 可能是总闸、也可能是第 3 区，用户看不出改的是什么，
 *    只能回侧栏逐个核对 —— 浮条的作用就没了。翻译一次，摆在用户正在看的地方。
 * ⭐ OSD 与遮挡**共用这一条**：两条浮条会重演本仓已被点名的「同一屏两套同名按钮」。
 *    OSD 排在前面（它更靠近画面上的字），遮挡在后。
 */
const pictureDraftSummary = computed(() => {
  const parts: string[] = [];
  // ── OSD ──
  if (osdDirtyFields.value.includes("timeX") || osdDirtyFields.value.includes("timeY")) parts.push("时间戳位置");
  if (osdDirtyFields.value.includes("timeType")) parts.push("时间格式");
  if (osdDirtyFields.value.includes("timeEnable")) parts.push("时间戳开关");
  if (osdDirtyFields.value.includes("textEnable")) parts.push("文字开关");
  // 逐行数，不是"items 变了就报全部"：改第 2 行却报 3 条，用户会去核对不存在的问题。
  const textChanges = osdAnchors.value.filter(anchor => anchor.kind === "item" && anchor.draft).length;
  if (textChanges) parts.push(`${textChanges} 条文字`);
  // ── 遮挡 / 镜像 ──
  const regions = pictureDirtyFields.value.filter(field => /^mask[1-4]$/.test(field)).length;
  if (regions) parts.push(`遮挡 ${regions} 处`);
  if (pictureDirtyFields.value.includes("maskOn")) parts.push(pictureMaskOn.value ? "启用遮挡" : "停用遮挡");
  if (pictureDirtyFields.value.includes("mirror")) parts.push("镜像");
  return parts.length ? parts.join(" · ") : "画面改动";
});

/** 浮条上的计数 = 画面组 + OSD 组的草稿项数（合一条浮条，就得报合计）。 */
const draftTotalCount = computed(() => pictureDirtyCount.value + osdDirtyCount.value);

/**
 * 遮挡坐标的参考系说明：把口径摆在卡片上，别让用户猜这些数是什么的单位。
 *
 * ⛔ 基准有两种来源，**必须在文案上分开**：设备声明的画布（精确）与退回的画面尺寸
 *    （可能对不上）。写成同一句"坐标基于 W×H"，用户在第二种情况下就会拿"画上去
 *    位置不对"来质疑功能，而看不出平台已经提示过基准没拿到。
 */
const pictureCoordinateText = computed(() => {
  const canvas = maskCanvasSize.value;
  if (!canvas) return "等待画面尺寸…";
  const base = `${canvas.width}×${canvas.height}`;
  return maskCanvasFromDevice.value
    ? `坐标基准 ${base}（设备声明的图像尺寸）`
    : `坐标基准 ${base}（按画面尺寸；未读到设备声明，落点可能偏）`;
});

/** 镜像四个方向各自的图标：方向是图形的事，不该用文字下拉问。 */
function mirrorChoiceIcon(value: string) {
  if (value === "1") return FlipHorizontal;
  if (value === "2") return FlipVertical;
  if (value === "3") return RotateCw;
  return Frame;
}

/** 画面上已有遮挡框的位置（归一化百分比），让用户直接看到"现在挡了哪儿"。 */
const maskOverlayBoxes = computed(() => {
  // ⛔ 归一化基准取**设备画布**（真机实测口径见 [pictureCanvasSize]）：设备存的是
  //    画布像素，拿画面尺寸去除就会把框画歪 —— 而且歪的方向和用户抱怨的
  //    "画的框挡在别处"是同一个错。
  const size = maskCanvasSize.value;
  if (!size) return [];
  return (
    pictureRegions.value
      .filter(region => region.used)
      // ⛔ 设备侧遮挡**已停用**时，设备保留的那些区域**不画** —— 此刻画面上本来
      //    就没有遮挡，画出来只会让用户以为"没清掉"（2026-09-19 现场：
      //    设备 `On=0` 但保留了 RegionList，框照旧画着）。
      //    ⭐ 但"生效中"和"即将生效"都要画：用户手动开了总闸（草稿 `maskOn=true`，
      //    还没下发）时，他需要看到"启用后会挡住哪"；同理关了闸但设备还没停用时，
      //    框也必须继续画着。所以判据是**事实或草稿任一为启用**，外加本次改过的槽位。
      .filter(
        region => pictureAppliedMaskOn.value || pictureMaskOn.value || pictureDirtyFields.value.includes(`mask${region.seq}`)
      )
      .map(region => {
        const [left, top, right, bottom] = region.coords.map(value => Number(value) || 0);
        return {
          seq: region.seq,
          // 这一槽位本次被改过（新画 / 挪过 / 删了重画）⇒ 它只活在草稿里，
          // 设备上还没有。画布上必须与"设备上就是这样"的框区分开，否则用户
          // 分不清哪块已经生效、哪块还得下发。
          draft: pictureDirtyFields.value.includes(`mask${region.seq}`),
          style: {
            left: `${(left / size.width) * 100}%`,
            top: `${(top / size.height) * 100}%`,
            width: `${(Math.max(right - left, 0) / size.width) * 100}%`,
            height: `${(Math.max(bottom - top, 0) / size.height) * 100}%`
          } as CSSProperties
        };
      })
  );
});

const maskDrawBoxStyle = computed<CSSProperties>(() => {
  if (!maskDrawStart.value || !maskDrawCurrent.value) return {};
  const left = Math.min(maskDrawStart.value.x, maskDrawCurrent.value.x);
  const top = Math.min(maskDrawStart.value.y, maskDrawCurrent.value.y);
  return {
    left: `${left * 100}%`,
    top: `${top * 100}%`,
    width: `${Math.abs(maskDrawCurrent.value.x - maskDrawStart.value.x) * 100}%`,
    height: `${Math.abs(maskDrawCurrent.value.y - maskDrawStart.value.y) * 100}%`
  };
});

/** 框选坐标基准取 `.play-window`（与用户看到的画面一致），拿不到时退回层自身。 */
function maskDrawRect(layer: HTMLElement) {
  const playWindow = layer.parentElement?.querySelector<HTMLElement>(".play-window");
  const rect = playWindow?.getBoundingClientRect();
  if (rect && rect.width > 0 && rect.height > 0) return rect;
  return layer.getBoundingClientRect();
}

function maskDrawPoint(event: PointerEvent) {
  const layer = event.currentTarget as HTMLElement;
  const rect = maskDrawRect(layer);
  return {
    x: Math.max(0, Math.min(1, (event.clientX - rect.left) / Math.max(rect.width, 1))),
    y: Math.max(0, Math.min(1, (event.clientY - rect.top) / Math.max(rect.height, 1)))
  };
}

/* ─────────── OSD 锚点拖拽 ─────────── */

/**
 * 画面上的一个指针位置 → **设备画布像素**。
 *
 * ⛔ 基准必须是设备画布：锚点画在 `left = x / Length` 上，落点当然要按同一个 `Length`
 *    反算 —— 否则"拖到哪"和"存进去是多少"是两把尺，松手那一刻锚点会自己跳走。
 */
function osdPointToCanvas(event: PointerEvent): { x: number; y: number } | null {
  const size = maskCanvasSize.value;
  if (!size) return null;
  const layer = osdLayerRef.value;
  if (!layer) return null;
  const rect = maskDrawRect(layer);
  if (!rect || rect.width <= 0 || rect.height <= 0) return null;
  const ratioX = Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width));
  const ratioY = Math.max(0, Math.min(1, (event.clientY - rect.top) / rect.height));
  return { x: Math.round(ratioX * size.width), y: Math.round(ratioY * size.height) };
}

function beginOsdDrag(event: PointerEvent, anchor: OsdAnchorSlot) {
  // ⛔ 三道闸门：① 必须先**显式进入编辑模式**（默认关，见 `osdEditMode`）；
  //    ② 与遮挡框选**互斥**（两层都吃 `pointerdown`，同时开着会"一边画框一边把某行字挪走"）；
  //    ③ 设备事实没读到就不许改（平台初值不是设备现状）。
  if (!osdEditMode.value || maskDrawMode.value || !osdEditable.value) return;
  if (event.button !== 0) return;
  const el = event.currentTarget as HTMLElement;
  osdDragKey.value = anchor.key;
  osdDragPos.value = { x: Number(anchor.x) || 0, y: Number(anchor.y) || 0 };
  osdDragMoved = false;
  // 合成事件（测试）里 `setPointerCapture` 可能抛 `NotFoundError`：包住，
  // 否则 handler 会中断在这里，而外面看起来只是"拖了没反应"。
  try {
    el.setPointerCapture?.(event.pointerId);
  } catch {
    /* 拿不到捕获也照常拖：指针离开锚点后 `pointerup` 可能丢，但落点已经算过了。 */
  }
  event.preventDefault();
}

/** 拖动中只更新**本地临时值**：`pointermove` 里写草稿会让脏值统计一路抖动。 */
function updateOsdDrag(event: PointerEvent) {
  if (!osdDragKey.value) return;
  const point = osdPointToCanvas(event);
  if (!point) return;
  osdDragPos.value = point;
  osdDragMoved = true;
}

function finishOsdDrag(event: PointerEvent) {
  const key = osdDragKey.value;
  if (!key) return;
  updateOsdDrag(event);
  const point = osdDragPos.value;
  const moved = osdDragMoved;
  osdDragKey.value = "";
  osdDragPos.value = null;
  osdDragMoved = false;
  // 只是点了一下（没移动）不该改任何东西 —— 否则"点一下看看"会把某行字挪到脚下。
  if (!moved || !point) return;
  const anchor = osdAnchors.value.find(item => item.key === key);
  if (!anchor) return;
  if (anchor.kind === "time") {
    deviceConfigRef.value?.setOsdTimePosition("x", point.x);
    deviceConfigRef.value?.setOsdTimePosition("y", point.y);
  } else {
    // ⛔ 这一句同时写入坐标**和**「已定位」标记：拖动是"用户摆了位置"的唯一证据，
    //    不标的话 `buildOSD` 会一直拒发这一行（而用户看起来明明已经摆好了）。
    deviceConfigRef.value?.setOsdItemPosition(anchor.index, point.x, point.y);
  }
}

function cancelOsdDrag() {
  osdDragKey.value = "";
  osdDragPos.value = null;
  osdDragMoved = false;
}

/**
 * 进入 / 退出 OSD 编辑模式（侧栏「时间戳」面板里的「调整位置 / 完成调整」按钮）。
 *
 * ⭐ 进编辑模式要**顺手把遮挡框选关掉**：两者都是"在画面上拖"，同时开着的结果是
 *    同一个按下动作既可能画框、也可能挪字 —— 用户没法预期，平台也说不清是哪一种。
 *    反方向由 `startMaskDraw` 负责（见那里）。
 * ⭐ 2026-09-20 起 `dragZoomMode` 不再"用一次就退"，于是它也成了常驻的"画面上拖"模式，
 *    同样在这里被关掉（四者的互斥闭环：本函数 / `startMaskDraw` / `toggleDragZoomMode` /
 *    `toggleTargetTrackMode`）。
 *
 * ⛔ 设备事实没读到（`osdEditable` 假）时**不许进**：这时 `familyValues` 里躺的是平台
 *    空白模板（`timeX = 10` 这类），进去拖一把就等于把平台初值当成设备现状改了。
 *    侧栏那个按钮此时本来是禁用的，这里再拦一道是因为抽屉只发"意图"，
 *    真正落状态的是这里 —— 闸门不能只看按钮的 `disabled`。
 */
function enterOsdEditMode() {
  if (!osdEditable.value) return;
  if (maskDrawMode.value) cancelMaskDraw();
  if (dragZoomMode.value) exitDragZoomMode();
  if (targetTrackMode.value) exitTargetTrackMode();
  osdEditMode.value = true;
}

function exitOsdEditMode() {
  osdEditMode.value = false;
  // 拖到一半退出（按钮 / Esc / 换页签）：拖拽状态也要收干净，
  // 否则下次进编辑模式，锚点会带着上一次那个"拖到一半"的临时坐标。
  cancelOsdDrag();
}

function toggleOsdEditMode() {
  if (osdEditMode.value) exitOsdEditMode();
  else enterOsdEditMode();
}

function onOsdEditKeydown(event: KeyboardEvent) {
  if (consumeCanvasEscape(event)) exitOsdEditMode();
}

// Esc 只在编辑模式期间接管：常驻监听会跟弹窗自己的 Esc 行为打架。
watch(osdEditMode, (active, _previous, onCleanup) => {
  if (!active) return;
  // ⛔ capture 见 `consumeCanvasEscape`：晚于弹窗处理就会漏判"上面有弹窗"。
  window.addEventListener("keydown", onOsdEditKeydown, true);
  onCleanup(() => window.removeEventListener("keydown", onOsdEditKeydown, true));
});

/**
 * 图像叠加面板（`DeviceConfigOsdBlocks`）的 props 袋与写口，全部来自 `DeviceConfigDrawer`
 * 的 `defineExpose`（2026-09-20 底栏化）。
 *
 * ⛔ **不要**在宿主侧另建一份 OSD 状态：底栏与抽屉共用同一份 `familyValues.osd`，
 *    这里只是把值搬过去渲染、把动作原样转发回去。
 * ⛔ props 袋是 computed（`deviceConfigRef` 挂上/换通道时它会变），所以要 `v-if` 判空 ——
 *    直接 `v-bind="undefined"` 会让必填 prop 缺失、渲染出一块空面板。
 */
const osdBlocksBag = computed(() => deviceConfigRef.value?.osdBlocks);

/** 点「新建」：占一个空槽位并进入框选。 */
function startMaskDraw() {
  if (!pictureEditable.value) return;
  const seq = deviceConfigRef.value?.nextPictureRegionSeq() ?? 0;
  if (!seq) {
    Message.warning(`遮挡区最多 ${MAX_MASK_REGIONS} 个，请先删掉一个再新建`);
    return;
  }
  // ⛔ 没读到设备声明的画布**照样让画**，但必须当场说清"基准是画面尺寸、落点可能偏"：
  //    遮挡坐标的基准是设备声明的图像尺寸（真机 704×576），退回解码尺寸只是
  //    "另一套设备的合法事实"（自研模拟器的基准就等于编码尺寸）。
  //    这里只警告不拦 —— 拦住只会让"点了新建没反应"，而用户无法自己判断
  //    到底是设备不认还是平台不给。
  if (!maskCanvasFromDevice.value) {
    Message.warning("没读到设备声明的图像坐标画布，本次按画面尺寸换算，遮挡落点可能偏");
  }
  maskDrawSeq.value = seq;
  // ⛔ 与 OSD 编辑模式互斥（反方向见 `enterOsdEditMode`）：开始框选就把锚点锁回去，
  //    否则同一个按下动作在两层里各有一套解释。拉框变焦同理（2026-09-20 它也会常驻了）；
  //    目标跟踪框选同理（2026-09-21 加的第四个"画面上拖"模式）。
  exitOsdEditMode();
  if (dragZoomMode.value) exitDragZoomMode();
  if (targetTrackMode.value) exitTargetTrackMode();
  maskDrawMode.value = true;
  maskDrawStart.value = null;
  maskDrawCurrent.value = null;
  maskDrawPointerId = null;
  // 播放器每秒报一次尺寸，这里再主动问一次：刚起播就画时不能拿空基准去换算。
  playWindowRef.value?.refreshVideoSize?.();
}

function cancelMaskDraw() {
  maskDrawMode.value = false;
  maskDrawSeq.value = 0;
  maskDrawStart.value = null;
  maskDrawCurrent.value = null;
  maskDrawPointerId = null;
}

function beginMaskDraw(event: PointerEvent) {
  if (!maskDrawMode.value || event.button !== 0) return;
  if (maskDrawPointerId !== null && maskDrawPointerId !== event.pointerId) return;
  const layer = event.currentTarget as HTMLElement;
  const point = maskDrawPoint(event);
  maskDrawPointerId = event.pointerId;
  maskDrawStart.value = point;
  maskDrawCurrent.value = point;
  layer.setPointerCapture?.(event.pointerId);
}

function updateMaskDraw(event: PointerEvent) {
  if (!maskDrawStart.value || maskDrawPointerId !== event.pointerId) return;
  maskDrawCurrent.value = maskDrawPoint(event);
}

function cancelMaskDrawPointer(event?: PointerEvent) {
  if (event && maskDrawPointerId !== null && maskDrawPointerId !== event.pointerId) return;
  maskDrawStart.value = null;
  maskDrawCurrent.value = null;
  maskDrawPointerId = null;
}

function finishMaskDraw(event: PointerEvent) {
  if (!maskDrawStart.value || maskDrawPointerId !== event.pointerId) return;
  updateMaskDraw(event);
  const start = maskDrawStart.value;
  const current = maskDrawCurrent.value ?? start;
  maskDrawStart.value = null;
  maskDrawCurrent.value = null;
  maskDrawPointerId = null;

  const left = Math.min(start.x, current.x);
  const top = Math.min(start.y, current.y);
  const right = Math.max(start.x, current.x);
  const bottom = Math.max(start.y, current.y);
  if (right - left < 0.02 || bottom - top < 0.02) {
    Message.warning("框选范围太小，请重新拖动");
    return;
  }
  // ⛔ 归一化 → **设备画布像素**（不是画面像素）。基准来源与判据见 [pictureCanvasSize]。
  const size = maskCanvasSize.value;
  if (!size) {
    Message.warning("还没拿到画面尺寸，请等画面出现后再框选");
    return;
  }
  // 归一化 → 编码像素。⛔ 两个角点**分别**换算（角点空间），不是位置 + 宽高。
  const toPx = (ratio: number, max: number) => Math.max(0, Math.min(max, Math.round(ratio * max)));
  const pxLeft = toPx(left, size.width);
  const pxTop = toPx(top, size.height);
  // 退化矩形（宽或高为 0）在设备侧处理不一致，这里至少留 1 像素。
  const pxRight = Math.min(Math.max(pxLeft + 1, toPx(right, size.width)), size.width);
  const pxBottom = Math.min(Math.max(pxTop + 1, toPx(bottom, size.height)), size.height);
  deviceConfigRef.value?.setPictureRegion(maskDrawSeq.value, {
    left: pxLeft,
    top: pxTop,
    right: pxRight,
    bottom: pxBottom
  });
  maskDrawMode.value = false;
  maskDrawSeq.value = 0;
}

function onMaskDrawKeydown(event: KeyboardEvent) {
  if (consumeCanvasEscape(event)) cancelMaskDraw();
}

watch(maskDrawMode, (active, _previous, onCleanup) => {
  if (!active) return;
  // ⛔ capture 见 `consumeCanvasEscape`：晚于弹窗处理就会漏判"上面有弹窗"。
  window.addEventListener("keydown", onMaskDrawKeydown, true);
  onCleanup(() => window.removeEventListener("keydown", onMaskDrawKeydown, true));
});

function onTargetTrackKeydown(event: KeyboardEvent) {
  if (consumeCanvasEscape(event)) exitTargetTrackMode();
}

// Esc 只在框选态期间接管：常驻监听会跟弹窗自己的 Esc 行为打架（与拉框变焦/OSD 同一个理由）。
watch(targetTrackMode, (active, _previous, onCleanup) => {
  if (!active) return;
  // ⛔ capture 见 `consumeCanvasEscape`：晚于弹窗处理就会漏判"上面有弹窗"。
  window.addEventListener("keydown", onTargetTrackKeydown, true);
  onCleanup(() => window.removeEventListener("keydown", onTargetTrackKeydown, true));
});

/* ─────────── Esc 的归属：四个"画面上拖"的模式 vs 弹窗 ─────────── */

/**
 * 是否有"在画面上拖一把"的模式正开着（拉框变焦 / 遮挡框选 / OSD 调位置 / 目标跟踪框选）。
 *
 * 四者互斥（进任一个都会关掉另三个，见 `toggleDragZoomMode` / `enterOsdEditMode` /
 * `startMaskDraw` / `toggleTargetTrackMode`）。唯一的用处是模板上 `<a-modal :esc-to-close>`
 * 的条件：模式开着时那一下 Esc 归图层（见 `consumeCanvasEscape`），控制台不许跟着关。
 */
const canvasModeArmed = computed(() => dragZoomMode.value || maskDrawMode.value || osdEditMode.value || targetTrackMode.value);

/**
 * 控制台自己开的弹窗（Esc 的第一语义 = 关掉最上面那个弹窗）。
 *
 * ⛔ 这几个 `a-modal` 都带 `esc-to-close`，而 Arco 只让**最上层**那个弹窗响应 Esc
 *    （内部 `isLastDialog()`）。我们的图层监听挂在 `window` 上，拿不到这个判断 ——
 *    不在这里挡一道，用户按 Esc 关弹窗时会把拉框态也顺手收掉：关完弹窗回来，
 *    按钮已经变回「3D 放大」，刚才摆好的下一刀白拖。
 * ⛔ 取值必须发生在**事件冒泡到我们之前**，所以三个图层监听都用 capture 注册（见 `consumeCanvasEscape`）。
 * ⛔ 别改成"去 DOM 里找有没有别的弹窗"：单测环境把 `teleport` 裁掉了（弹窗内容原地渲染、
 *    `.arco-modal-container` 一个都不在 `document` 里），那种写法在测试里恒为假 —— 等于没护栏。
 *    （因此新增弹窗时要记得补进这个列表。）
 */
const consoleDialogOpen = computed(
  () =>
    homeSettingsDialogVisible.value ||
    savePresetDialogVisible.value ||
    saveCruiseDialogVisible.value ||
    probeTimelineDialogVisible.value
);

/**
 * 这一下 Esc 该不该由"画面上拖"的图层消费；该，就**当场把事件吃掉**再返回 true。
 * （调用方负责确认自己那个模式还开着。）
 *
 * 判据两条：① 是 Esc；② 没有别的弹窗开着（有 ⇒ 那一下归它，见 `consoleDialogOpen`，
 * 此时**绝不能**吃事件，否则那个弹窗就关不掉了）。
 *
 * ⛔⛔ **`stopPropagation()` 是这套修复的核心，不是保险丝**（2026-09-20 真实浏览器实测）：
 *    只把模板上的 `esc-to-close` 在模式期间置假是**不够的**。我们在 `window` 的 capture 里
 *    先退出模式 ⇒ `canvasModeArmed` 变假 ⇒ **在同一个事件派发过程中** `escToClose` 就变回了
 *    `true`；Arco 挂在 `document.documentElement` 上的那份全局监听**随后才跑**，读到的已经是
 *    `true`，于是照关不误。实测相位：
 *      `win-capture` → `esc-to-close=false`、图层在；
 *      `doc-capture` → 已经是 `true`、图层已退 ⇒ Arco 随后 `handleCancel`。
 *    调用栈坐实：`requestClose → handleClose → update:visible → consoleStore.close()`，
 *    用户看到的就是"按一下 Esc，播放控制台整个弹窗没了"。
 *    ⇒ 图层既然"接管"了这一下 Esc，就得把它**从事件流里拿掉**；不能指望另一边自觉。
 *
 * ⛔ 三个图层监听都必须用 **capture** 注册：要抢在弹窗自己处理之前判"上面有没有弹窗"。
 *    只在 `window` 上挂、又不 capture，就会晚于弹窗处理而漏判。
 * ⛔ 别改成"去 DOM 里找有没有别的弹窗"：单测环境把 `teleport` 裁掉了（`.arco-modal-container`
 *    一个都不在 `document` 里），那种写法在测试里恒为假 —— 等于没护栏。所以用 `consoleDialogOpen`
 *    这个显式列表，**新增控制台内弹窗时要记得补进去**。
 */
function consumeCanvasEscape(event: KeyboardEvent): boolean {
  if (event.key !== "Escape") return false;
  if (consoleDialogOpen.value) return false;
  event.stopPropagation();
  return true;
}

// 换页签或换通道时退出框选：草稿还停在画面上而目标已经变了，很容易写错设备。
watch([activeTab, () => props.channel?.id], ([, channelId], [, previousChannelId]) => {
  if (maskDrawMode.value) cancelMaskDraw();
  // 编辑模式同理退出：换到别的页签后画面上的锚点已经不在了，
  // 留着一个"看得见是开着、却无处可拖"的模式只会让人以为坏了。
  if (osdEditMode.value) exitOsdEditMode();
  // ── 切通道：草稿会真丢 ──
  // 草稿存在 DeviceConfigDrawer 的 `familyValues` 里，换通道时它连同基准一起复位
  // （见 Drawer 的 `watch(props.channelId)`）—— 草稿属于那个通道，这是对的。
  // ⛔ 但拦不住：切通道的入口在设备列表（`consoleStore.open(...)`），不在本组件内，
  //    这里能拿到的只是"已经变了"。所以退一步：**如实告知**，别让改动静默消失。
  if (previousChannelId !== undefined && channelId !== previousChannelId) {
    // ⛔ 合计（画面 + OSD）：只报画面组的数，用户点了确认才发现 OSD 的改动也一起丢了。
    const pending = draftTotalCount.value;
    if (pending > 0) {
      Message.warning(`已切换到其它通道，上一个通道 ${pending} 项未下发的画面改动已放弃`);
    }
  }
});

/** 遮挡区坐标摘要：`左X,左Y → 右X,右Y`。 */
function regionCoordsText(coords: number[]): string {
  const [left, top, right, bottom] = coords.map(value => Math.round(Number(value) || 0));
  return `${left},${top} → ${right},${bottom}`;
}

function removeMaskRegion(seq: number) {
  if (!pictureEditable.value) return;
  deviceConfigRef.value?.clearPictureRegion(seq);
}

/**
 * 总闸开关：**点即下发**，不再攒成草稿等浮条的「下发」。
 *
 * 2026-09-19 产品决定 + 真机实测支撑：
 * - 协议里**不存在**"独立的启用信令"——`<On>` 只是 `PictureMask` 里的一个字段，
 *   和区域列表挤在同一条 DeviceConfig 报文里。所以"只调用启用"必须落成"发一条只带
 *   `PictureMask` 的 DeviceConfig"。
 * - 这个请求是可执行的：实测发 `<On>1</On><SumNum>0</SumNum>`（不带 `RegionList`），
 *   设备回读 `On=1`，且不会凭空创建区域 ⇒ 没有理由让用户先画区域才能点启用。
 * - ⛔ 只发遮挡这一块（`applyPictureMaskOnly`），不捎带镜像草稿 —— 用户点的是遮挡。
 * - 发完就地推基准（Drawer 侧 `commitPictureMaskDraft`），否则按钮会一直停在"将启用"、
 *   浮条也一直挂着"未下发"，看起来像没发出去。
 */
async function togglePictureMaskOn(value: boolean) {
  if (!pictureEditable.value) return;
  deviceConfigRef.value?.setPictureMaskOn(value);
  await deviceConfigRef.value?.applyPictureMaskOnly();
}

/* ─────────── 下发浮条（取代原底栏「提交」卡片）─────────── */

/**
 * 2026-09-19 流程重做：下发入口从底栏第三张卡搬进**画布内浮条**。
 *
 * 原样（「画完 → 跑到右下角卡片 → 点下发」）的问题是**视线断点**：用户刚拖完框，
 * 注意力全在画面上，而下发按钮在另一个角落，很容易直接走开。
 *
 * 所以浮条贴在**用户正在看的东西**上面（画面底部居中），并且只在真有草稿时出现 ——
 * 没改动时它是零存在的，不占地方也不制造"这里是不是该点一下"的干扰。
 *
 * ⛔ 浮条只在「画面设置」页签露头。跨页签常驻会让云台/探针页面上凭空多一条
 *    "遮挡 2 处待下发"，用户会以为那是当前页的操作 —— 跨页签的提醒交给页签角标。
 * ⛔ 框选过程中不露头：那一层是全屏吃指针的，浮条会被它盖住，也会挡住拖拽起点。
 * ⭐ 画面组与 OSD 组**共用这一条**：两条浮条会重演本仓已被点名的
 *    「同一屏两套同名按钮」（`play-console-ux-architecture.md` B3）。
 */
const pictureDraftBarVisible = computed(
  () => activeTab.value === "deviceconfig" && draftTotalCount.value > 0 && !maskDrawMode.value
);

/**
 * 浮条上的「下发」为什么点不动。
 *
 * ⛔ 未定位的行**不许下发**：`0,0` 是合法坐标，`buildOSD` 从值上判不出"用户还没摆"，
 *    判据只能是草稿里的定位标记。放行的话设备上会真的多出一行贴左上角的字，
 *    而界面看起来一切正常 —— 这类"静默写坏设备"是本仓最贵的错。
 * ⛔ 要按**参与下发的组**分别判可编辑性，不能用 `pictureEditable` 一刀切：
 *    只改了 OSD、而画面组没读到事实时，`pictureEditable` 是 false，会误把按钮禁掉。
 */
const draftBlockedReason = computed(() => {
  if (osdUnplacedCount.value > 0) {
    return `有 ${osdUnplacedCount.value} 行文字还没在画面上定位 —— 拖动它的标记再下发`;
  }
  if (pictureDirtyCount.value > 0 && !pictureEditable.value) return "画面组需要先读到设备配置才能下发";
  if (osdDirtyCount.value > 0 && !osdEditable.value) return "图像叠加需要先读到设备配置才能下发";
  return "";
});

/** 浮条上要显示的下发/拒发原因：两组哪一组有话就显示哪一组。 */
const draftError = computed(() => pictureError.value || osdError.value);

async function applyPictureDraft() {
  if (draftBlockedReason.value) return;
  // ⭐ 两组草稿**合并成一条报文**发出去：协议上 `OSDConfig` 与 `PictureMask` 本来就是
  //    `DeviceConfig` 里的兄弟元素，分两次发只是把"两个按钮"的问题挪到网络层。
  await deviceConfigRef.value?.applyPictureAndOsdGroups();
}

function revertPictureDraft() {
  if (pictureDirtyCount.value > 0) revertPictureCard();
  if (osdDirtyCount.value > 0) deviceConfigRef.value?.revertOsdGroup();
}

function choosePictureMirror(value: string) {
  if (!pictureEditable.value) return;
  deviceConfigRef.value?.setPictureMirror(value);
}

function readPictureCard() {
  deviceConfigRef.value?.readPictureGroup();
}

function revertPictureCard() {
  deviceConfigRef.value?.revertPictureGroup();
}

async function runAdvancedAction(action: string, region?: Record<string, number>) {
  if (!canControlDevice.value || !props.channel) return;
  if (isAdvancedPending(action)) return;
  const execute = async () => {
    const channelId = props.channel!.id;
    const token = sessionToken;
    const contextKey = channelContextKey();
    const statusToken = advancedStatusToken.value;
    setAdvancedPending(action, true);
    setAdvancedPhase(action, "queued", "正在发送");
    try {
      const response = await controlDevice(channelId, {
        action,
        idempotencyKey: `${channelId}-${action}-${Date.now()}`,
        ...((action === "drag_zoom_in" || action === "drag_zoom_out") && region ? { region } : {})
      });
      if (!isCurrentChannelContext(channelId, token, contextKey) || statusToken !== advancedStatusToken.value) return;
      if (response.code !== 0) throw new Error(response.message || "设备控制失败");
      const operation = response.data;
      const operationId = operation?.operationId;
      const operationStatus = String(operation?.status || "sent") as AdvancedOperationPhase;
      advancedOperationIds.value = { ...advancedOperationIds.value, [action]: operationId || null };
      advancedOperationDeadline.value = { ...advancedOperationDeadline.value, [action]: operation?.deadlineAt || null };
      setAdvancedPhase(action, operationStatus, operationId ? `已受理 · ${operationId}` : "已发送,等待设备应答");
      if (operationStatus === "accepted") {
        setAdvancedPending(action, false);
        // Only a device-confirmed result may change the local fact state.
        if (advancedActionRequiresDeviceResult(action, operation?.responseRequired)) {
          applyAdvancedAccepted(action);
        } else {
          setAdvancedPhase(action, "sent", "已下发（该命令无需设备回执）");
          Message.info("已下发（该命令无需设备回执）");
        }
        return;
      }
      if (["rejected", "timeout", "unknown", "cancelled"].includes(operationStatus)) {
        setAdvancedPending(action, false);
        if (operation)
          advancedOperationError(action, operationStatus, {
            operationId: operationId || "",
            status: operationStatus as PTZOperation["status"],
            errorCode: operation.errorCode || null,
            errorMessage: operation.errorMessage || response.message || null,
            completedAt: operation.completedAt || null,
            deadlineAt: operation.deadlineAt || null,
            deviceResult: operation.deviceResult || null
          });
        return;
      }
      if (operationStatus === "sent" && !advancedActionRequiresDeviceResult(action, operation?.responseRequired)) {
        setAdvancedPending(action, false);
        setAdvancedPhase(action, "sent", "已下发（该命令无需设备回执）");
        Message.info(operation?.deduplicated ? "请求已合并到设备级操作" : "已下发（该命令无需设备回执）");
        return;
      }
      if (operationId) {
        if (operationStatus === "queued" || operationStatus === "sent") {
          setAdvancedPending(action, true);
          beginAdvancedOperationPoll(action, operationId, operation?.deadlineAt);
          scheduleAdvancedOperationPoll(action, operationId, channelId, token, contextKey);
        } else {
          setAdvancedPending(action, false);
        }
      } else {
        // Key-frame and DragZoom are SIP-delivery operations; no fake accepted state.
        setAdvancedPending(action, false);
        Message.info(operation?.deduplicated ? "请求已合并到设备级操作" : "已下发（该命令无需设备回执）");
      }
    } catch (error: any) {
      if (!isCurrentChannelContext(channelId, token, contextKey) || statusToken !== advancedStatusToken.value) return;
      setAdvancedPending(action, false);
      setAdvancedPhase(action, "unknown", error?.message || "设备控制失败");
      Message.error(error?.message || "设备控制失败");
    }
  };
  await execute();
}

/* ────────────────────────── 语音对讲 ────────────────────────── */

type TalkState = "idle" | "permission" | "publishing" | "signaling" | "talking" | "stopping";
const talkState = ref<TalkState>("idle");
const talkMode = ref<"broadcast" | "talk">("broadcast");
const talkSession = ref<TalkCreateResult | null>(null);
let talkConnection: RTCPeerConnection | null = null;
let talkStream: MediaStream | null = null;
let talkSessionChannelId: number | null = null;
let talkPollTimer: number | null = null;
let talkToken = 0;

/**
 * 「正在说话」的实时电平（0..1），由采集流的 AnalyserNode 驱动，用于按钮上的波形反馈。
 * 拿不到 AudioContext 时恒为 0 —— 波形退化成静态起伏，不影响对讲本身。
 */
const talkLevel = ref(0);
let talkMeter: AudioLevelMeter | null = null;

function stopTalkMeter() {
  talkMeter?.stop();
  talkMeter = null;
  talkLevel.value = 0;
}

function startTalkMeter() {
  stopTalkMeter();
  if (!talkStream) return;
  talkMeter = createAudioLevelMeter(talkStream, level => {
    talkLevel.value = level;
  });
}

const talkButtonText = computed(() => {
  const label = talkMode.value === "broadcast" ? "广播" : "对讲";
  if (talkState.value === "permission") return "正在申请麦克风";
  if (talkState.value === "publishing") return "正在发布音源";
  if (talkState.value === "signaling") return `正在建立${label}`;
  // ⭐ 说话中显示的是**动作**（再点一下就停），不是「松开结束」——对讲已改成点击开关，
  // 见 toggleTalk。
  if (talkState.value === "talking") return `停止${label}`;
  if (talkState.value === "stopping") return "正在停止";
  return `开始${label}`;
});

/**
 * ⭐ 对讲是**点击开关**，不是长按。
 *
 * 原来的长按版本用 `pointerdown` 起、`pointerup/pointerleave/pointercancel` 收尾，在触屏上
 * 手指稍微滑出按钮就断话，而且「松开即停」意味着没法一边说话一边去点别处。
 * 现在：点一下开始（按钮文字变成「停止广播 / 停止对讲」），再点一下停止。
 *
 * ⛔ 过渡态（`permission` / `publishing` / `signaling`）里的第二次点击是**取消**，不能忽略：
 * 麦克风授权弹窗可能久等不来、WHIP/信令可能卡住，那是操作员唯一的退出口。
 * 取消走 `stopTalk` → `cleanupTalkLocally` 里的 `talkToken++`，还在途中的建会话流程会自己放弃
 * （`startTalk` 每一段 await 后都比对 token）。
 */
function toggleTalk() {
  if (!canTalk.value) return;
  if (talkState.value === "stopping") return;
  if (talkState.value === "idle") {
    void startTalk();
    return;
  }
  void stopTalk();
}

function clearTalkPoll() {
  if (talkPollTimer) window.clearInterval(talkPollTimer);
  talkPollTimer = null;
}

async function waitTalkActive(channelId: number, sessionId: string, token: number) {
  if (!hasPermission("gb28181:talk:control")) return false;
  talkState.value = "signaling";
  for (let attempt = 0; attempt < 100; attempt++) {
    if (!hasPermission("gb28181:talk:control") || token !== talkToken) return false;
    const response = await getTalkSession(channelId, sessionId);
    if (!hasPermission("gb28181:talk:control") || token !== talkToken) return false;
    const state = response.data?.state;
    if (state === "active") return true;
    if (["failed", "expired", "ended"].includes(String(state))) throw new Error(response.data?.error || `对讲会话已${state}`);
    if (!hasPermission("gb28181:talk:control") || token !== talkToken) return false;
    await new Promise(resolve => window.setTimeout(resolve, 300));
  }
  throw new Error("等待设备对讲信令超时");
}

function beginTalkPoll(channelId: number, sessionId: string) {
  if (!hasPermission("gb28181:talk:control")) return;
  clearTalkPoll();
  talkPollTimer = window.setInterval(async () => {
    if (!hasPermission("gb28181:talk:control")) {
      await stopTalk();
      return;
    }
    try {
      const response = await getTalkSession(channelId, sessionId);
      if (!hasPermission("gb28181:talk:control")) {
        await stopTalk();
        return;
      }
      if (["failed", "expired", "ended"].includes(String(response.data?.state))) await stopTalk();
    } catch {
      // 短暂轮询失败不打断正在说话，租约超时由后端最终收敛。
    }
  }, 10000);
}

async function startTalk() {
  if (!canTalk.value) return;
  if (talkState.value !== "idle") return;
  if (!isAudioCapable.value) {
    Message.warning("设备离线，无法建立语音会话");
    return;
  }
  if (!props.channel) return;
  const channelId = props.channel.id;
  const token = ++talkToken;
  talkState.value = "permission";
  let acquiredStream: MediaStream | null = null;
  try {
    if (!navigator.mediaDevices?.getUserMedia) throw new Error("当前浏览器不支持麦克风采集");
    acquiredStream = await navigator.mediaDevices.getUserMedia({
      audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true }
    });
    const audioTrack = acquiredStream.getAudioTracks()[0];
    if (!audioTrack) throw new Error("没有可用的麦克风音轨");
    acquiredStream.getTracks().forEach(track => {
      track.enabled = false;
    });
    if (token !== talkToken) {
      acquiredStream.getTracks().forEach(track => track.stop());
      return;
    }
    talkStream = acquiredStream;
    talkState.value = "publishing";

    const response = await createTalkSession(channelId, talkMode.value);
    if (response.code !== 0 || !response.data) throw new Error(response.message || "对讲会话创建失败");
    if (token !== talkToken) {
      acquiredStream.getTracks().forEach(track => track.stop());
      await deleteTalkSession(channelId, response.data.sessionId).catch(() => undefined);
      return;
    }
    talkSession.value = response.data;
    talkSessionChannelId = channelId;
    // ICE 服务器由平台下发，且只在节点声明了 rtc.externIP（跨网段）时才下发：
    // 那种场景必须靠 STUN 拿到 NAT 上的映射地址，只产 host candidate 连不上。
    // 同网段部署平台不下发，浏览器只产 host candidate，也就不会去等一个可能不可达的 STUN。
    const uplink = response.data.uplink;
    talkConnection = new RTCPeerConnection({ iceServers: uplink?.iceServers || [] });
    const transceiver = talkConnection.addTransceiver(audioTrack, { direction: "sendonly", streams: [talkStream] });
    const audioCodecs = typeof RTCRtpSender === "undefined" ? [] : RTCRtpSender.getCapabilities?.("audio")?.codecs || [];
    preferPCMA8000(transceiver, audioCodecs);
    const offer = await talkConnection.createOffer();
    await talkConnection.setLocalDescription(offer);
    // 收集超时不致命：按已收集到的候选继续发布，同网段靠 host candidate 就能建连。
    const iceGathered = await waitForIceGatheringComplete(talkConnection);
    if (!iceGathered) {
      console.warn("[talk] ICE 收集未在预算内完成，按已有候选发布（STUN 可能不可达）");
    }
    const offerSdp = talkConnection.localDescription?.sdp || "";
    assertPCMA8000(offerSdp);
    if (token !== talkToken) return;
    // 上行发给平台自己，由平台转发到媒体节点：媒体节点地址与自签证书不再经手浏览器。
    // 带上 Authorization 是因为平台侧要认人，而这条裸 fetch 不走 http 封装。
    const access = getAccessToken();
    const answer = await fetch(uplink.url, {
      method: "POST",
      headers: {
        "Content-Type": uplink.contentType || "application/sdp",
        ...(access?.accessToken ? { Authorization: formatToken(access.accessToken) } : {})
      },
      body: offerSdp
    });
    if (!answer.ok) throw new Error(`WHIP 发布失败(${answer.status})`);
    const answerSdp = await answer.text();
    assertPCMA8000(answerSdp);
    await talkConnection.setRemoteDescription({ type: "answer", sdp: answerSdp });
    if (!(await waitTalkActive(channelId, response.data.sessionId, token))) return;
    if (token !== talkToken) return;
    talkStream.getAudioTracks().forEach(track => {
      track.enabled = true;
    });
    talkState.value = "talking";
    startTalkMeter();
    beginTalkPoll(channelId, response.data.sessionId);
  } catch (error: any) {
    if (token !== talkToken) {
      acquiredStream?.getTracks().forEach(track => track.stop());
      return;
    }
    await stopTalk();
    Message.error(error?.message || "对讲启动失败");
  }
}

function cleanupTalkLocally() {
  talkToken++;
  clearTalkPoll();
  stopTalkMeter();
  if (talkStream)
    talkStream.getTracks().forEach(track => {
      track.enabled = false;
      track.stop();
    });
  talkStream = null;
  talkConnection?.close();
  talkConnection = null;
  talkSession.value = null;
  talkSessionChannelId = null;
}

async function stopTalk() {
  const session = talkSession.value;
  const channelId = talkSessionChannelId;
  talkState.value = "stopping";
  // ⛔ 顺序要紧：先静音、再拆会话、最后才停流关 PeerConnection。
  // 反过来（先 close 掉 pc）媒体面先死，设备发现收不到 RTP 会**抢在我们的 BYE
  // 之前**发它自己的 BYE，形成交叉 BYE —— 对端不会回我们 200，拆除事务只能重传
  // 到超时（实测 15s）：用户看到 504，平台侧 ZLM 释放也被跳过。
  // 静音只是 track.enabled=false，RTP 仍在流（送静音），不会触发设备端 BYE。
  if (talkStream)
    talkStream.getAudioTracks().forEach(track => {
      track.enabled = false;
    });
  if (hasPermission("gb28181:talk:control") && channelId && session?.sessionId)
    await deleteTalkSession(channelId, session.sessionId).catch(() => undefined);
  cleanupTalkLocally();
  talkState.value = "idle";
}

function switchProtocol(proto: StreamProtocol) {
  if (proto === protocol.value) return;
  if (!protocolUrls.value[proto]) {
    Message.warning("当前节点未返回该协议地址");
    return;
  }
  if (!isBrowserPlayable(proto)) {
    Message.warning("该协议仅供外部客户端使用，浏览器内不可直接播放");
    return;
  }
  playbackSnapshot.value = null;
  protocol.value = proto;
}

async function copyProtocolUrl(proto: StreamProtocol) {
  if (!canSharePlayback.value) return;
  let url = protocolUrls.value[proto];
  if (!url) {
    Message.warning("当前协议地址不可用");
    return;
  }

  const channel = props.channel;
  const result = playResult.value;
  const fixedStreamID = channel ? `${channel.deviceId}_${channel.channelId}` : "";
  if (channel && result?.streamId === fixedStreamID && hasPlaybackToken(url)) {
    try {
      const response = await authorizeFixedPlayback(channel.deviceId, channel.channelId);
      const authorizedURL = response.code === 0 && response.data ? protocolUrlsFor(response.data)[proto] : null;
      if (!authorizedURL) {
        Message.error("播放地址授权失败，请重试");
        return;
      }
      url = authorizedURL;
    } catch {
      Message.error("播放地址授权失败，请重试");
      return;
    }
  }
  await copyTextToClipboard(url);
}

function releaseContinuousControls() {
  releasePtzControl();
  void stopTalk();
}

function handleVisibilityChange() {
  if (document.hidden) releaseContinuousControls();
}

/* ────────────────────────── 生命周期 ────────────────────────── */

watch(
  [() => props.visible, () => channelContextKey()],
  ([visible]) => {
    if (visible && props.channel) {
      void loadDefaultPtzSpeed();
      void startSession();
    } else {
      releasePtzControl();
      void stopTalk();
      cleanupSessionLocally();
    }
  },
  { immediate: true }
);

watch(
  () => props.displayMode,
  mode => {
    if (mode === "minimized") {
      releaseContinuousControls();
      exitDragZoomMode();
      void nextTick(keepMiniPlayerInViewport);
    } else {
      finishMiniPlayerDrag();
    }
  }
);

onMounted(() => {
  window.addEventListener("blur", releaseContinuousControls);
  window.addEventListener("resize", keepMiniPlayerInViewport);
  document.addEventListener("visibilitychange", handleVisibilityChange);
});

onBeforeUnmount(() => {
  window.removeEventListener("blur", releaseContinuousControls);
  window.removeEventListener("resize", keepMiniPlayerInViewport);
  document.removeEventListener("visibilitychange", handleVisibilityChange);
  finishMiniPlayerDrag();
  // OSD 锚点层剩余的那个定时器：不清理的话，组件销毁后回调仍会写 ref（Vue 会警告、测试会串味）。
  // ⛔ 以前这里是两个 —— 首次引导那个（`osdGuideTimer`）随引导一起删了，见 `osdAnchorTitle` 注释。
  if (osdFocusTimer !== undefined) window.clearTimeout(osdFocusTimer);
  clearProbeTimers();
  clearTimer();
  clearMonitor();
  void stopTalk();
  cleanupSessionLocally();
});
</script>

<template>
  <a-modal
    :class="{ 'play-console-container--minimized': isMinimized }"
    :visible="visible"
    :width="playbackModalWidth"
    :footer="false"
    :mask="!isMinimized"
    :mask-closable="false"
    :align-center="!isMinimized"
    :closable="false"
    :esc-to-close="!isMinimized && !canvasModeArmed"
    :modal-style="miniPlayerModalStyle"
    :body-style="miniPlayerBodyStyle"
    unmount-on-close
    :modal-class="['uvp-system-dialog', 'play-console-modal', { 'play-console-modal--minimized': isMinimized }]"
    @cancel="requestClose"
  >
    <template #title>
      <div
        class="console-title"
        :class="{ 'is-minimized': isMinimized }"
        data-testid="play-console-drag-handle"
        @pointerdown="beginMiniPlayerDrag"
      >
        <span class="title-icon"><RadioTower :size="18" /></span>
        <div class="title-text">
          <strong>{{ isMinimized ? title : "播放控制台" }}</strong>
          <span v-if="!isMinimized">{{ title }} · {{ channel?.channelId || "未选择通道" }}</span>
        </div>
        <span v-if="!isMinimized" class="session-badge" :class="sessionStatusClass">
          <span class="dot"></span>{{ sessionStatusText }}
          <em v-if="phase === 'playing'" class="session-elapsed mono">{{ elapsedText }}</em>
        </span>
        <div class="console-window-actions">
          <template v-if="!isMinimized">
            <button
              type="button"
              class="console-window-action is-minimize"
              data-testid="play-console-minimize"
              title="切换为小窗播放"
              aria-label="切换为小窗播放"
              @pointerdown.stop
              @click.stop="handleMinimize"
            >
              <PictureInPicture2 :size="15" aria-hidden="true" />
              <span>小窗</span>
            </button>
            <button
              type="button"
              class="console-window-action is-close"
              data-testid="play-console-close"
              title="关闭并停止播放"
              aria-label="关闭并停止播放"
              @pointerdown.stop
              @click.stop="requestClose"
            >
              <X :size="15" aria-hidden="true" />
              <span>关闭</span>
            </button>
          </template>
          <template v-else>
            <button
              type="button"
              class="console-window-action is-compact"
              data-testid="play-console-restore"
              title="恢复播放控制台"
              aria-label="恢复播放控制台"
              @pointerdown.stop
              @click.stop="handleRestore"
            >
              <Maximize2 :size="15" />
            </button>
            <button
              type="button"
              class="console-window-action is-close is-compact"
              data-testid="play-console-close-mini"
              title="关闭并停止播放"
              aria-label="关闭并停止播放"
              @pointerdown.stop
              @click.stop="requestClose"
            >
              <X :size="15" />
            </button>
          </template>
        </div>
      </div>
    </template>

    <div class="console-body" :class="{ 'is-minimized': isMinimized }" data-testid="play-console-body">
      <!-- 一级页签 2026-09-21 已回到右侧属性栏顶部（老板：「把播放控制台的三个菜单放回以前的位置」）——
           原来那列 136px 的左侧竖排导航随 `.workbench-nav` 一起退役，宽度还给画面。
           ⛔ 别再往这里插导航：`.video-frame` / `.linked-info-bar` / `.sidebar` 的 `grid-column`
              索引全都按「两列」写死了，多一列要同步改四处（漏一处整块错位）。 -->
      <!-- 主区(视频 + 控制条 + 会话链路) -->
      <section class="stage" :class="{ 'stage-wide': sideCollapsed }">
        <!-- 视频画面 -->
        <div class="video-frame">
          <!-- 真实播放器；无地址时仍保留稳定的加载/空/错误态布局 -->
          <div class="video-canvas">
            <!-- 拉框变焦层（2026-09-20：一次框选下发完**不再退出**，可以连着拉）
                 ⛔ 蓝罩只挂在 `.is-dragging`（真的按着拖）上：常驻的 12% 色罩会给刚下发的
                    画面整体染色，而操作员下一步正是要看清"到底放大成了什么样"。
                 ⭐ 于是"当前处于拉框态"的标识换成两个常驻物：这条提示条 + 按钮上的「取消 3D 放大」。
                 两个都要留着 —— 少了提示条，用户会以为按钮点坏了（画面看着没变化）。 -->
            <div
              v-if="dragZoomMode"
              class="drag-zoom-layer"
              :class="{ 'is-dragging': dragZoomStart !== null, 'is-busy': isAdvancedPending(dragZoomAction) }"
              data-testid="drag-zoom-layer"
              @pointerdown="beginDragZoom"
              @pointermove="updateDragZoom"
              @pointerup="finishDragZoom"
              @pointercancel="cancelDragZoom"
            >
              <span class="drag-zoom-box" :style="dragZoomBoxStyle"></span>
              <span class="drag-zoom-hint">
                <template v-if="isAdvancedPending(dragZoomAction)">正在下发上一次框选…</template>
                <template v-else
                  >拖动选择 3D {{ dragZoomAction === "drag_zoom_out" ? "缩小" : "放大" }}区域 · 可连续框选,Esc 退出</template
                >
              </span>
            </div>

            <!-- 目标跟踪框选层（GB/T 28181-2022 A.2.3.1.14）
                 ⛔ 与拉框变焦**刻意不同**的一点：这里下完一次就退出（见 exitTargetTrackMode）——
                    跟踪是"选定一个目标"，不是"连续调整"；留着态会让用户以为还要再框第二刀，
                    而第二条手动跟踪指令会直接覆盖第一条的框。
                 ⛔ 提示条里的两个词都写死了：动作是「下发」不是「生效」，受益方是**平台**要不要跟踪。
                    设备会不会真的去跟，标准没有给平台任何查询手段。 -->
            <div
              v-if="targetTrackMode"
              class="target-track-layer"
              :class="{ 'is-dragging': targetTrackStart !== null, 'is-busy': targetTrackPending }"
              data-testid="target-track-layer"
              @pointerdown="beginTargetTrackDraw"
              @pointermove="updateTargetTrackDraw"
              @pointerup="finishTargetTrackDraw"
              @pointercancel="cancelTargetTrackDraw"
            >
              <span class="target-track-box" :style="targetTrackBoxStyle"></span>
              <span class="target-track-hint">
                <template v-if="targetTrackPending">正在下发…</template>
                <template v-else>在画面上框住要跟踪的目标 · Esc 退出</template>
              </span>
            </div>
            <template v-if="phase === 'playing' || phase === 'paused'">
              <PlayWindow
                ref="playWindowRef"
                :url="currentProtocolUrl"
                :zlm-webrtc="currentProtocolUsesZlmWebRtc"
                :has-audio="channel?.audioEnabled === true"
                @error="handlePlayerError"
                @videosize="onPlayerVideoSize"
              />
              <div v-if="phase === 'paused'" class="paused-mask"><Pause :size="42" /><span>已暂停</span></div>
            </template>
            <template v-else-if="phase === 'requesting'">
              <div class="placeholder">
                <div class="pulse"><Loader2 :size="42" class="spin" /></div>
                <strong>正在建立国标媒体会话</strong>
                <span>{{ phaseHint }}</span>
              </div>
            </template>
            <template v-else-if="phase === 'error'">
              <div class="placeholder error">
                <AlertTriangle :size="42" />
                <strong>点播未完成</strong>
                <span>{{ phaseHint }}</span>
                <button class="btn-primary player-retry" @click="reconnect"><RefreshCcw :size="14" />重试</button>
              </div>
            </template>
            <template v-else>
              <div class="placeholder">
                <div class="pulse idle"><Video :size="42" /></div>
                <strong>准备就绪</strong>
                <span>{{ phaseHint }}</span>
              </div>
            </template>
            <div
              v-if="joystickDirection && (phase === 'playing' || phase === 'paused')"
              class="ptz-direction-indicator"
              data-testid="ptz-direction-indicator"
              :data-direction="joystickDirection"
              role="status"
              :aria-label="`云台正在向${joystickDirection}移动`"
            >
              <span class="ptz-direction-stack">
                <span class="ptz-direction-chevron front" aria-hidden="true"></span>
                <span class="ptz-direction-chevron middle" aria-hidden="true"></span>
                <span class="ptz-direction-chevron back" aria-hidden="true"></span>
              </span>
            </div>

            <!-- 已有遮挡区的画面投影：只作参照，不吃指针事件 -->
            <div
              v-if="activeTab === 'deviceconfig' && maskOverlayBoxes.length"
              class="mask-overlay-layer"
              data-testid="mask-overlay-layer"
            >
              <span
                v-for="box in maskOverlayBoxes"
                :key="box.seq"
                class="mask-overlay-box"
                :class="{ 'is-draft': box.draft }"
                :data-seq="box.seq"
                :data-draft="box.draft ? '1' : '0'"
                :style="box.style"
              >
                <!-- 标签把状态写在框上：只换颜色的话，色弱用户和"记不住哪个色是什么"
                                     的人一样分不出来，而这块框发不发出去正是这里唯一要说的信息。 -->
                <em class="mask-overlay-tag">{{ box.draft ? `#${box.seq} 待下发` : `#${box.seq}` }}</em>
              </span>
            </div>

            <!-- 图像叠加（OSD）的锚点层：**只在编辑模式存在**（2026-09-20 老板再反馈）。
                 ⛔ 默认不画（`v-if="osdEditMode"`）：它不是"位置信息的载体"（那个由侧栏的
                    `X 289 · Y 256` 承担），而是"正在摆位置"这件事的操作面。常驻的后果就是
                    老板看到的那一幕 —— 四五个带引线的标签摊在视频上，把要看的画面盖住了。
                 ⛔ 形态是**锚点 + 内容标签**，不是"像真字的预览" —— 标准 `OSDCfgType` 里
                    没有字体/字号/颜色，画出来就是在承诺平台给不了的能力；而且设备**已经烧进
                    码流**的时间戳就在这个画面里，再叠一个假字会出现**两个时间戳**。
                 ⭐ 标签用引线偏到坐标点右上 8px（见样式），刻意**不压住**设备那个真字 ——
                    用户可以对着真字拖锚点对齐，等于免费的精确校准。
                 ⛔ 拖动是**显式动作**（`osdEditMode`）：锚点层盖着播放器，随时可拖的话
                    "想点一下画面"就会顺手挪走某行字，而且画面上看不出来（平台不渲染真字）。 -->
            <div
              v-if="activeTab === 'deviceconfig' && osdEditMode"
              ref="osdLayerRef"
              class="osd-overlay-layer"
              data-testid="osd-overlay-layer"
            >
              <span
                v-for="anchor in osdOverlayAnchors"
                :key="anchor.key"
                class="osd-anchor"
                :class="{
                  'is-draft': anchor.draft,
                  'is-unplaced': anchor.unplaced,
                  'is-off': anchor.off,
                  'is-dragging': anchor.dragging,
                  'is-focus': osdFocusKey === anchor.key
                }"
                :data-anchor="anchor.key"
                :data-kind="anchor.kind"
                :data-draft="anchor.draft ? '1' : '0'"
                :data-unplaced="anchor.unplaced ? '1' : '0'"
                :data-off="anchor.off ? '1' : '0'"
                :style="anchor.style"
                :title="osdAnchorTitle(anchor)"
                @pointerdown="beginOsdDrag($event, anchor)"
                @pointermove="updateOsdDrag"
                @pointerup="finishOsdDrag"
                @pointercancel="cancelOsdDrag"
              >
                <span class="osd-anchor-lead"></span>
                <span class="osd-anchor-dot"></span>
                <span class="osd-anchor-tag">
                  {{ anchor.label }}
                  <!-- 状态写在标签上，不只靠颜色：色弱用户和"记不住哪个色是什么"的人
                       一样分不出来，而这块标发不发得出去正是这里唯一要说的信息。 -->
                  <em v-if="anchor.unplaced" class="is-unplaced">未定位</em>
                  <em v-else-if="anchor.draft" class="is-draft">待下发</em>
                  <em v-if="anchor.off" class="is-off">已关闭</em>
                </span>
                <span v-if="anchor.dragging" class="osd-anchor-coords" data-testid="osd-anchor-coords">
                  {{ anchor.coords }}
                </span>
              </span>

              <!-- 操作说明：这层只在编辑模式存在，所以它也就是"编辑模式的说明"。
                   ⛔ 指向必须写清**按钮在侧栏**（2026-09-20 起按钮搬进「时间戳」面板）——
                     这一版之前它在画面下方工具条，照旧文案会让用户低头找。 -->
              <span class="osd-layer-hint" data-testid="osd-layer-hint">
                拖动这些标记就能挪位置 · 点侧栏「完成调整」或按 Esc 退出
              </span>
            </div>

            <!-- 新建遮挡区：点卡片「新建」后在这一层上拖框 -->
            <div
              v-if="maskDrawMode"
              class="mask-draw-layer"
              data-testid="mask-draw-layer"
              @pointerdown="beginMaskDraw"
              @pointermove="updateMaskDraw"
              @pointerup="finishMaskDraw"
              @pointercancel="cancelMaskDrawPointer"
            >
              <span class="mask-draw-box" :style="maskDrawBoxStyle"></span>
              <!-- 基准写在用户正在框选的那一刻：偏差要在画之前就知道，
                                 而不是下发之后对着画面比。 -->
              <span class="mask-draw-hint">
                在画面上拖出要遮挡的区域（第 {{ maskDrawSeq }} 区）· 基准 {{ maskCanvasText }} · Esc 取消
              </span>
            </div>

            <!-- 下发浮条（取代原底栏「提交」卡片）：贴在用户正在看的画面上，只留一个主动作。
                             ⛔ 不放「再画一块」：遮挡在协议上是**整块替换**（SumNum + RegionList 一次发全量），
                                草稿期本来就允许连续拖多块，再拖一下就是第 N+1 块，不需要一个模式按钮。
                             ⛔ 清空/撤销放次要位：按钮一多，"右下角提交卡被忽略"就会以"浮条被忽略"的形态复发。 -->
            <div
              v-if="pictureDraftBarVisible"
              class="picture-draft-bar"
              data-testid="picture-draft-bar"
              role="status"
              :title="pictureCoordinateText"
            >
              <!-- 拒发/下发失败的原因必须在这里露面：浮条是用户按下「下发」的地方，
                                 ⛔ 把原因只写在侧栏 Drawer 里，用户看到的就是"点了没反应"。 -->
              <p v-if="draftError" class="picture-draft-error" data-testid="picture-draft-error">
                {{ draftError }}
              </p>
              <div class="picture-draft-row">
                <span class="picture-draft-text">
                  <strong>{{ draftTotalCount }}</strong>
                  <span>项画面改动未下发</span>
                  <em class="picture-draft-summary" data-testid="picture-draft-summary">{{ pictureDraftSummary }}</em>
                </span>
                <span class="picture-draft-actions">
                  <button
                    type="button"
                    class="picture-draft-revert"
                    data-testid="picture-draft-revert"
                    title="丢弃本次改动，回到设备当前值"
                    @click="revertPictureDraft"
                  >
                    还原
                  </button>
                  <button
                    type="button"
                    class="picture-draft-submit"
                    data-testid="picture-draft-apply"
                    :disabled="Boolean(draftBlockedReason)"
                    :title="draftBlockedReason || `下发到设备：${pictureDraftSummary}`"
                    @click="applyPictureDraft"
                  >
                    <Send :size="12" /><span>下发</span>
                  </button>
                </span>
              </div>
            </div>
          </div>

          <!-- 多协议切换器(底部) -->
          <div class="protocol-switcher">
            <div class="switcher-left">
              <span class="kicker">播放协议</span>
              <a-select
                :model-value="protocol"
                :style="{ width: '160px' }"
                :placeholder="currentProtocolOption?.label || '无可播放地址'"
                size="small"
                :trigger-props="{ autoFitPopupWidth: false }"
                @change="switchProtocol"
              >
                <a-option
                  v-for="opt in availableProtocolOptions"
                  :key="opt.value"
                  :value="opt.value"
                  :label="opt.label"
                  :disabled="!opt.browserPlayable"
                >
                  <div class="protocol-option">
                    <strong>{{ opt.label }}:</strong>
                    <span class="protocol-url" :title="protocolUrls[opt.value] || ''">{{ protocolUrls[opt.value] }}</span>
                    <button
                      v-if="canSharePlayback"
                      type="button"
                      class="protocol-copy-btn"
                      :title="`复制 ${opt.label} 地址`"
                      :aria-label="`复制 ${opt.label} 地址`"
                      @mousedown.stop.prevent
                      @click.stop="copyProtocolUrl(opt.value)"
                    >
                      <Copy :size="13" />
                    </button>
                  </div>
                </a-option>
              </a-select>
            </div>
            <div class="switcher-right">
              <button
                v-for="opt in shortcutProtocolOptions"
                :key="opt.value"
                class="proto-btn"
                :class="{ active: protocol === opt.value }"
                :disabled="!protocolUrls[opt.value]"
                @click="switchProtocol(opt.value)"
              >
                {{ opt.label }}
              </button>
            </div>

            <!-- ⛔ 这里**不再**挂 OSD 的「调整位置」按钮（2026-09-20 老板第三次调整）：
                 它先在画面右上角（遮画面）→ 再搬到这条工具条（仍要视线来回跳）→
                 现在归位到侧栏「时间戳」面板的「位置」行，见 `DeviceConfigOsdBlocks`。
                 按钮改的就是那一行的坐标，跟坐标读数放在一起才是它本来的位置。 -->
          </div>
        </div>

        <!-- 双区联动详情:所有 Tab 共用下方详情区，保持结构与高度稳定 -->
        <div
          v-if="phase === 'playing' || isConfigWorkspace"
          class="stream-info-bar linked-info-bar"
          :class="{ 'config-detail-bar': isConfigWorkspace }"
        >
          <div v-if="canPtzPanel" v-show="activeTab === 'ptz'" class="linked-detail" data-testid="linked-detail-ptz">
            <div class="linked-ptz-layout">
              <section class="linked-section linked-card">
                <header class="linked-card-hd">
                  <span class="section-title"
                    ><Hash :size="13" />预置位<em v-if="presets.length" class="preset-count">{{ presets.length }}</em></span
                  >
                  <span class="linked-card-actions">
                    <button
                      class="resource-sync-btn"
                      data-testid="preset-sync-btn"
                      :class="{ syncing: presetSyncing }"
                      :disabled="presetSyncing"
                      :title="presetSyncTitle"
                      @click="syncPresets"
                    >
                      <Loader2 v-if="presetSyncing" :size="11" class="resource-sync-spin" />
                      <RefreshCcw v-else :size="11" />
                      <span>{{ presetSyncLabel }}</span>
                    </button>
                    <button class="preset-save-btn" data-testid="preset-save-btn" @click="openSavePresetDialog">
                      <Plus :size="12" /><span>添加</span>
                    </button>
                  </span>
                </header>
                <div v-if="presets.length === 0" class="preset-empty" data-testid="preset-empty">
                  <Inbox :size="24" class="preset-empty-glyph" />
                  <p class="preset-empty-line">暂无预置位</p>
                  <p class="preset-empty-hint">设备上已有的可用「同步」读回</p>
                </div>
                <div v-else class="preset-grid">
                  <div v-for="p in visiblePresets" :key="p.id" class="preset-tile" :class="{ active: activePresetId === p.id }">
                    <a-tooltip
                      :content="`#${p.id} ${p.name}`"
                      position="top"
                      :mouse-enter-delay="RESOURCE_TOOLTIP_ENTER_DELAY_MS"
                      :data-testid="`preset-tile-tooltip-${p.id}`"
                    >
                      <button class="preset-tile-hit preset-item" @click="callPreset(p.id)">
                        <span class="preset-idx">#{{ p.id }}</span>
                        <span class="preset-name">{{ p.name }}</span>
                      </button>
                    </a-tooltip>
                    <button class="preset-tile-del" :title="`删除 #${p.id}`" @click.stop="deletePreset(p.id)">
                      <X :size="11" />
                    </button>
                  </div>
                  <a-popover
                    v-if="hasMorePresets"
                    v-model:popup-visible="presetMoreVisible"
                    trigger="click"
                    position="bottom"
                    :content-style="{ padding: 0 }"
                    class="preset-more-popover-trigger"
                  >
                    <button class="preset-tile-more" data-testid="preset-more-btn">
                      <span>更多 · {{ presets.length }}</span>
                      <ChevronDown :size="11" />
                    </button>
                    <template #content>
                      <div class="preset-popover" data-testid="preset-popover">
                        <header class="preset-popover-hd">
                          <span
                            ><Hash :size="12" />全部预置位<em class="preset-count">{{ presets.length }}</em></span
                          >
                        </header>
                        <div class="preset-popover-list">
                          <div
                            v-for="p in presets"
                            :key="p.id"
                            class="preset-popover-row"
                            :class="{ active: activePresetId === p.id }"
                            data-testid="preset-popover-row"
                          >
                            <span class="preset-popover-idx">#{{ p.id }}</span>
                            <a-tooltip
                              :content="`#${p.id} ${p.name}`"
                              position="top"
                              :mouse-enter-delay="RESOURCE_TOOLTIP_ENTER_DELAY_MS"
                              :data-testid="`preset-popover-tooltip-${p.id}`"
                            >
                              <span class="preset-popover-name">{{ p.name }}</span>
                            </a-tooltip>
                            <div class="preset-popover-actions">
                              <button class="preset-popover-call" title="调用此预置位" @click="callPreset(p.id)">
                                <Navigation :size="11" />
                              </button>
                              <button class="preset-popover-del" title="删除此预置位" @click="deletePreset(p.id)">
                                <Trash2 :size="11" />
                              </button>
                            </div>
                          </div>
                        </div>
                      </div>
                    </template>
                  </a-popover>
                </div>
              </section>

              <section class="linked-section linked-card">
                <header class="linked-card-hd">
                  <span class="section-title">
                    <Route :size="13" />巡航轨迹<em v-if="cruiseTracks.length" class="preset-count">{{ cruiseTracks.length }}</em>
                    <button
                      v-if="activeCruiseId !== null && cruiseState !== 'stopped'"
                      class="cruise-running-chip"
                      data-testid="cruise-running-chip"
                      :title="`巡航 #${activeCruiseId} 启动指令已发送,点击停止`"
                      @click="stopCruise"
                    >
                      <span class="cruise-running-dot" :class="cruiseState" />
                      启动已下发
                      <Square :size="10" />
                    </button>
                  </span>
                  <span class="linked-card-actions">
                    <!-- 「同步」从标题里的一行小字改成了动作区的按钮。
                                             它原来是个 <small>,既不可点、又挤在标题中间,
                                             而它写的偏偏是「缓存数据已过期」这种**要求你
                                             去做点什么**的话 —— 提示了问题却不给入口。
                                             现在与预置位卡片的药丸同一位置、同一套文案。 -->
                    <button
                      class="resource-sync-btn"
                      data-testid="cruise-sync-btn"
                      :class="{ syncing: cruiseSyncing }"
                      :disabled="cruiseSyncing"
                      :title="cruiseSyncTitle"
                      @click="syncCruises"
                    >
                      <Loader2 v-if="cruiseSyncing" :size="11" class="resource-sync-spin" />
                      <RefreshCcw v-else :size="11" />
                      <span>{{ cruiseSyncLabel }}</span>
                    </button>
                    <button
                      class="preset-save-btn"
                      data-testid="cruise-add-btn"
                      :disabled="presets.length === 0"
                      :title="presets.length === 0 ? '需要先添加预置位才能新建巡航轨迹' : '新建巡航轨迹(按顺序串联多个预置位)'"
                      @click="openSaveCruiseDialog"
                    >
                      <Plus :size="12" /><span>添加</span>
                    </button>
                  </span>
                </header>
                <div v-if="cruiseLoadError" class="preset-empty" data-testid="cruise-load-error">
                  <AlertTriangle :size="24" class="preset-empty-glyph" />
                  <p class="preset-empty-line">{{ cruiseLoadError }}</p>
                  <p class="preset-empty-hint">点「同步」重试</p>
                </div>
                <div v-else-if="cruiseTracks.length === 0" class="preset-empty" data-testid="cruise-empty">
                  <Inbox :size="24" class="preset-empty-glyph" />
                  <p class="preset-empty-line">暂无巡航轨迹</p>
                  <p class="preset-empty-hint">设备上已有的可用「同步」读回</p>
                </div>
                <div v-else class="preset-grid">
                  <a-tooltip
                    v-for="c in visibleCruiseTracks"
                    :key="c.id"
                    :content="cruiseTileTitle(c)"
                    position="top"
                    :mouse-enter-delay="RESOURCE_TOOLTIP_ENTER_DELAY_MS"
                    :data-testid="`cruise-tile-tooltip-${c.id}`"
                  >
                    <div
                      class="preset-tile cruise-tile"
                      :class="{
                        active: activeCruiseId === c.id && cruiseState !== 'stopped',
                        disabled: !c.enabled && !c.pending,
                        pending: c.pending
                      }"
                      :data-testid="`cruise-tile-${c.id}`"
                    >
                      <button
                        class="preset-tile-hit cruise-item"
                        :disabled="!c.enabled && !c.pending"
                        @click="toggleCruise(c.id)"
                      >
                        <Square v-if="cruiseTileState(c) === 'stop'" :size="10" class="cruise-tile-icon" />
                        <Play v-else :size="10" class="cruise-tile-icon" />
                        <span class="preset-name">{{ c.name }}</span>
                        <span v-if="c.pending" class="cruise-status-badge">未验证</span>
                      </button>
                      <button class="preset-tile-del" :title="`删除巡航 #${c.id}`" @click.stop="deleteCruise(c.id)">
                        <X :size="11" />
                      </button>
                    </div>
                  </a-tooltip>
                  <a-popover
                    v-if="hasMoreCruises"
                    v-model:popup-visible="cruiseMoreVisible"
                    trigger="click"
                    position="bottom"
                    :content-style="{ padding: 0 }"
                    class="preset-more-popover-trigger"
                  >
                    <button class="preset-tile-more" data-testid="cruise-more-btn">
                      <span>更多 · {{ cruiseTracks.length }}</span>
                      <ChevronDown :size="11" />
                    </button>
                    <template #content>
                      <div class="preset-popover" data-testid="cruise-popover">
                        <header class="preset-popover-hd">
                          <span
                            ><Route :size="12" />全部巡航轨迹<em class="preset-count">{{ cruiseTracks.length }}</em></span
                          >
                        </header>
                        <div class="preset-popover-list">
                          <a-tooltip
                            v-for="c in cruiseTracks"
                            :key="c.id"
                            :content="cruiseTileTitle(c)"
                            position="top"
                            :mouse-enter-delay="RESOURCE_TOOLTIP_ENTER_DELAY_MS"
                            :data-testid="`cruise-popover-tooltip-${c.id}`"
                          >
                            <div
                              class="preset-popover-row"
                              :class="{ active: activeCruiseId === c.id && cruiseState !== 'stopped' }"
                              data-testid="cruise-popover-row"
                            >
                              <span class="preset-popover-idx">#{{ c.id }}</span>
                              <span class="preset-popover-name">{{ c.name }}</span>
                              <div class="preset-popover-actions">
                                <button
                                  class="preset-popover-call"
                                  :disabled="!c.enabled && !c.pending"
                                  @click="toggleCruise(c.id)"
                                >
                                  <Square v-if="cruiseTileState(c) === 'stop'" :size="11" />
                                  <Play v-else :size="11" />
                                </button>
                                <button class="preset-popover-del" title="删除此巡航轨迹" @click="deleteCruise(c.id)">
                                  <Trash2 :size="11" />
                                </button>
                              </div>
                            </div>
                          </a-tooltip>
                        </div>
                      </div>
                    </template>
                  </a-popover>
                </div>
              </section>

              <section class="linked-section linked-card" data-testid="home-card">
                <div class="section-hd first">
                  <span class="section-title"
                    ><Home :size="13" />看守位<span class="tag-2022" title="GB/T 28181-2022 扩展能力">2022</span></span
                  >
                  <div class="home-header-actions">
                    <span
                      class="home-diagnostics"
                      :title="homeDiagnosticsTitle"
                      aria-label="看守位能力诊断"
                      data-testid="home-diagnostics"
                    >
                      <Info :size="12" />
                    </span>
                  </div>
                </div>
                <div
                  class="home-config"
                  :class="`state-${homePresentation.tone}`"
                  data-testid="home-status"
                  aria-live="polite"
                  :aria-busy="homeIsBusy"
                >
                  <div class="home-state-row">
                    <span class="home-state-icon" data-testid="home-state-icon" :data-icon="homePresentation.state">
                      <Loader2
                        v-if="homePresentation.state === 'loading' || homePresentation.state === 'pending'"
                        :size="15"
                        class="spin"
                      />
                      <CircleSlash v-else-if="homePresentation.state === 'unsupported'" :size="15" />
                      <CheckCircle2 v-else-if="homePresentation.state === 'enabled'" :size="15" />
                      <AlertTriangle
                        v-else-if="homePresentation.state === 'error' || homePresentation.state === 'offline'"
                        :size="15"
                      />
                      <Circle v-else-if="homePresentation.state === 'disabled'" :size="15" />
                      <Info v-else :size="15" />
                    </span>
                    <div class="home-state-copy">
                      <strong data-testid="home-phase">{{ homePresentation.label }}</strong>
                      <span v-if="homePresentation.description">{{ homePresentation.description }}</span>
                      <span v-if="homeLastConfirmedText" data-testid="home-confirmed-values">{{ homeLastConfirmedText }}</span>
                      <span v-else-if="homeConfirmedValuesText" data-testid="home-confirmed-values">{{
                        homeConfirmedValuesText
                      }}</span>
                      <span v-if="homeConfirmedAtText && !homeLastConfirmedText" class="home-confirmed-at">{{
                        homeConfirmedAtText
                      }}</span>
                    </div>
                  </div>
                  <p v-if="homeConfirmedOutsideEditableRange" class="home-warning" data-testid="home-range-warning">
                    设备返回的归位配置不完整，修改后才能再次启用。
                  </p>
                  <p v-if="homeNoticeText" class="home-error" data-testid="home-notice">{{ homeNoticeText }}</p>
                  <p
                    v-if="presets.length === 0 && homeControlSupport.status !== 'unsupported'"
                    class="home-hint"
                    data-testid="home-preset-required"
                  >
                    请先添加预置位，再配置看守位。
                  </p>
                  <div class="home-card-actions">
                    <button
                      v-if="
                        homePresentation.showConfigure &&
                        (homePresentation.state === 'enabled' ||
                          homePresentation.state === 'disabled' ||
                          homePresentation.state === 'unconfigured' ||
                          (homePresentation.state === 'error' && homeConfirmed))
                      "
                      class="btn-primary sm"
                      data-testid="home-configure"
                      :disabled="!homeCanConfigure"
                      :title="presets.length === 0 ? '请先添加预置位' : undefined"
                      @click="openHomeSettingsDialog"
                    >
                      <Settings :size="12" />{{ homeConfirmed?.enabled ? "修改设置" : "配置并启用" }}
                    </button>
                    <button
                      v-if="homeConfirmed?.enabled"
                      class="btn-ghost sm home-close-btn"
                      data-testid="home-close"
                      :disabled="!homeCanClose"
                      @click="closeHomePosition"
                    >
                      <CircleSlash :size="12" />关闭
                    </button>
                    <button
                      v-if="homePresentation.showQuery"
                      class="btn-ghost sm uvp-refresh-btn"
                      data-testid="home-refresh"
                      :disabled="!homeCanRefresh"
                      @click="refreshHomePosition"
                    >
                      <RefreshCcw :size="12" />{{ homePresentation.queryLabel }}
                    </button>
                  </div>
                </div>
              </section>

              <section class="linked-section linked-card" data-testid="scan-card">
                <header class="linked-card-hd">
                  <span class="section-title">
                    <MoveHorizontal :size="13" />自动扫描
                    <!-- 「启动已下发」与巡航同款 chip:HTTP 成功只表示指令发出去了,
                         设备到底扫没扫只能看画面,所以这里说的是"已下发"而不是"扫描中"。 -->
                    <button
                      v-if="scanState === 'start-sent'"
                      class="cruise-running-chip"
                      data-testid="scan-running-chip"
                      :title="`扫描组 #${scanActiveGroup} 启动指令已发送,点击停止`"
                      @click="sendScanCommand('scan_stop')"
                    >
                      <span class="cruise-running-dot" />
                      启动已下发
                      <Square :size="10" />
                    </button>
                  </span>
                </header>
                <div class="scan-panel" data-testid="scan-panel">
                  <div class="scan-row">
                    <label class="scan-label" for="scan-group-input">组号</label>
                    <input
                      id="scan-group-input"
                      v-model.number="scanGroup"
                      class="scan-number"
                      type="number"
                      :min="SCAN_GROUP_MIN"
                      :max="SCAN_GROUP_MAX"
                      data-testid="scan-group-input"
                      :title="`扫描组号 ${SCAN_GROUP_MIN}-${SCAN_GROUP_MAX}(89H 的字节5)`"
                    />
                    <button
                      class="btn-primary sm scan-toggle"
                      data-testid="scan-toggle"
                      :disabled="!scanCanSend"
                      :title="
                        !canControlPtz
                          ? '没有云台控制权限'
                          : scanState === 'start-sent'
                            ? '停止扫描(字节 4-7 全零的通用停止帧)'
                            : '开始扫描(89H)'
                      "
                      @click="toggleScan"
                    >
                      <Play v-if="scanState === 'stopped'" :size="11" /><Square v-else :size="11" />
                      <span>{{ scanState === "start-sent" ? "停止扫描" : "开始扫描" }}</span>
                    </button>
                  </div>
                  <div class="scan-row">
                    <button
                      class="btn-ghost sm scan-bound-btn"
                      data-testid="scan-set-left"
                      :disabled="!scanCanSend"
                      title="把云台「当前朝向」写入左边界(89H 字节6=01H)"
                      @click="sendScanCommand('scan_set_left')"
                    >
                      设左边界
                    </button>
                    <button
                      class="btn-ghost sm scan-bound-btn"
                      data-testid="scan-set-right"
                      :disabled="!scanCanSend"
                      title="把云台「当前朝向」写入右边界(89H 字节6=02H)"
                      @click="sendScanCommand('scan_set_right')"
                    >
                      设右边界
                    </button>
                  </div>
                  <div class="scan-row">
                    <label class="scan-label" for="scan-speed-input">速度</label>
                    <input
                      id="scan-speed-input"
                      v-model.number="scanSpeed"
                      class="scan-number"
                      type="number"
                      :min="SCAN_SPEED_MIN"
                      :max="SCAN_SPEED_MAX"
                      data-testid="scan-speed-input"
                      :title="`扫描速度 ${SCAN_SPEED_MIN}-${SCAN_SPEED_MAX}(8AH,12 位)`"
                    />
                    <button
                      class="btn-ghost sm"
                      data-testid="scan-set-speed"
                      :disabled="!scanCanSend || scanSpeedInvalid"
                      title="下发扫描速度(8AH)"
                      @click="sendScanCommand('scan_set_speed')"
                    >
                      下发
                    </button>
                  </div>
                  <p v-if="scanError" class="scan-error" data-testid="scan-error">{{ scanError }}</p>
                  <p v-else class="scan-hint" data-testid="scan-hint">
                    扫描只在左右边界之间来回，与预置位无关；边界需先把云台转到目标位置再设置
                  </p>
                </div>
              </section>
            </div>
          </div>

          <!-- 画面设置：功能拆成并列卡片铺在底栏（原「图像叠加/画面处理」两个二级 tab 取消） -->
          <div
            v-if="canViewPtz"
            v-show="activeTab === 'deviceconfig'"
            class="linked-detail linked-detail-actions"
            data-testid="linked-detail-picture"
          >
            <div class="linked-picture-layout">
              <!-- ① 图像叠加（2026-09-20 从侧栏整块搬来，老板：「不想用切换的方式，要一页全展示」）
                   ⛔ 不套 `.linked-card` 外壳：里面那两块（时间戳 / 叠加文字）本来就有自己的框，
                      再套一层就等于为了一个标题吃掉 24px —— 而这一格的预算只有 148px。
                   ⛔ 值 / 写口全部来自 `deviceConfigRef`（`osdBlocks` 袋 + 三个写口），
                      宿主**不另存一份** OSD 状态。
                   ⛔ `:editing` 与 `:canvas-linked` 显式覆盖袋里的值：控制台里这两个由**画面侧**持有
                      （`osdEditMode` / 有画布），抽屉那侧在控制台没接这两个 prop。
                      漏了 `canvas-linked` ⇒「调整位置」按钮不渲染，而它正是这块面板的主操作。 -->
              <section class="linked-section picture-osd-cell" data-testid="picture-osd-cell">
                <DeviceConfigOsdBlocks
                  v-if="osdBlocksBag"
                  v-bind="osdBlocksBag"
                  :editing="osdEditMode"
                  :canvas-linked="true"
                  layout="row"
                  @update:time-enable="deviceConfigRef?.setOsdFlag('timeEnable', $event)"
                  @update:time-type="deviceConfigRef?.setOsdFlag('timeType', $event)"
                  @update:time-x="deviceConfigRef?.setOsdTimePosition('x', Number($event))"
                  @update:time-y="deviceConfigRef?.setOsdTimePosition('y', Number($event))"
                  @update:text-enable="deviceConfigRef?.setOsdFlag('textEnable', $event)"
                  @update:items="deviceConfigRef?.setOsdItems($event)"
                  @locate="deviceConfigRef?.focusOsdAnchor($event)"
                  @toggle-edit="toggleOsdEditMode"
                />
              </section>

              <!-- ② 画面遮挡 -->
              <section class="linked-section linked-card" data-testid="picture-mask-card">
                <header class="linked-card-hd">
                  <span class="section-title">
                    <Scan :size="13" />画面遮挡
                    <em v-if="pictureUsedCount && !pictureRetainedMode" class="preset-count">{{ pictureUsedCount }}</em>
                  </span>
                  <span class="linked-card-actions">
                    <button
                      class="mask-switch"
                      :class="{ on: pictureMaskOn, pending: pictureMaskPending }"
                      :disabled="!pictureEditable"
                      data-testid="picture-mask-switch"
                      :title="pictureMaskSwitchTitle"
                      @click="togglePictureMaskOn(!pictureMaskOn)"
                    >
                      {{ pictureMaskSwitchText }}
                    </button>
                    <button
                      class="resource-sync-btn"
                      data-testid="picture-read-btn"
                      title="重新向设备查询画面配置"
                      @click="readPictureCard"
                    >
                      <RefreshCcw :size="11" /><span>读取</span>
                    </button>
                    <button
                      class="preset-save-btn"
                      data-testid="picture-mask-add-btn"
                      :disabled="!pictureEditable || !pictureCanAddRegion"
                      :title="
                        !pictureEditable
                          ? '需要先读到设备配置才能编辑'
                          : pictureCanAddRegion
                            ? '在播放画面上框选新的遮挡区'
                            : `遮挡区最多 ${MAX_MASK_REGIONS} 个`
                      "
                      @click="startMaskDraw"
                    >
                      <Plus :size="12" /><span>新建</span>
                    </button>
                  </span>
                </header>

                <p v-if="pictureError" class="picture-card-error" data-testid="picture-card-error">
                  {{ pictureError }}
                </p>

                <!-- 提示（不是错误）：请求发出去了、设备也接受了，
                                     但结果不是"启用了就有遮挡"——就地说明，别让用户再猜一次。 -->
                <p v-else-if="pictureMaskNotice" class="picture-card-notice" data-testid="picture-mask-notice">
                  {{ pictureMaskNotice }}
                </p>

                <div v-if="pictureFactsMissing" class="preset-empty" data-testid="picture-mask-unread">
                  <Inbox :size="24" class="preset-empty-glyph" />
                  <p class="preset-empty-line">
                    {{ pictureAbsentTypes.length ? "设备未返回遮挡配置" : "尚未读取设备遮挡配置" }}
                  </p>
                  <p class="preset-empty-hint">
                    {{ pictureAbsentTypes.length ? "该设备没有这个配置类型（2016 版无遮挡）" : "点「读取」先看设备当前挡在哪儿" }}
                  </p>
                </div>
                <!-- 设备已停用、但区域还残留着：不能摆成"当前遮挡"（用户会以为没清掉），
                                     也不能当它们不存在（下次启用会一起活过来）。给一个说明态。 -->
                <div v-else-if="pictureRetainedMode" class="preset-empty" data-testid="picture-mask-retained">
                  <CircleSlash :size="24" class="preset-empty-glyph" />
                  <p class="preset-empty-line">遮挡已停用</p>
                  <p class="preset-empty-hint">
                    设备仍保留 {{ pictureRetainedRegionCount }} 个区域 —— 上次「停用」没能把它们清掉（国标里 `On`
                    与区域列表是独立节点，设备有权保留）；再点「启用」会让它们重新生效。
                  </p>
                </div>
                <div v-else-if="pictureUsedCount === 0" class="preset-empty" data-testid="picture-mask-blank">
                  <Inbox :size="24" class="preset-empty-glyph" />
                  <p class="preset-empty-line">未设置遮挡区</p>
                  <p class="preset-empty-hint">点「新建」，然后在画面上拖出要遮挡的范围</p>
                </div>
                <div v-else class="mask-slot-grid">
                  <div
                    v-for="region in pictureRegions"
                    :key="region.seq"
                    class="mask-slot"
                    :class="{ used: region.used }"
                    :data-testid="`picture-mask-slot-${region.seq}`"
                  >
                    <span class="mask-slot-idx">#{{ region.seq }}</span>
                    <span v-if="region.used" class="mask-slot-coords">{{ regionCoordsText(region.coords) }}</span>
                    <span v-else class="mask-slot-blank">空位</span>
                    <button
                      v-if="region.used"
                      class="mask-slot-del"
                      :title="`删除遮挡区 ${region.seq}`"
                      :data-testid="`picture-mask-del-${region.seq}`"
                      @click="removeMaskRegion(region.seq)"
                    >
                      <X :size="11" />
                    </button>
                  </div>
                </div>
                <!-- 坐标基准：槽位里那串数字到底是"什么尺子上的数"。 -->
                <p
                  v-if="maskCanvasSize"
                  class="mask-canvas-note"
                  :class="{ 'is-unverified': !maskCanvasFromDevice }"
                  data-testid="picture-mask-base"
                >
                  {{ pictureCoordinateText }}
                </p>
              </section>

              <!-- ② 画面镜像 -->
              <section class="linked-section linked-card" data-testid="picture-mirror-card">
                <header class="linked-card-hd">
                  <span class="section-title"><FlipHorizontal :size="13" />画面镜像</span>
                  <span class="linked-card-actions">
                    <span v-if="!pictureEditable" class="linked-card-note">不可编辑</span>
                  </span>
                </header>
                <div class="mirror-choice-grid">
                  <button
                    v-for="option in MIRROR_OPTIONS"
                    :key="option.value"
                    type="button"
                    class="mirror-choice"
                    :class="{ active: pictureMirror === option.value }"
                    :disabled="!pictureEditable"
                    :data-testid="`picture-mirror-${option.value}`"
                    :title="`${option.label}（值 ${option.value}）`"
                    @click="choosePictureMirror(option.value)"
                  >
                    <component :is="mirrorChoiceIcon(option.value)" :size="16" />
                    <span>{{ option.shortLabel || option.label }}</span>
                  </button>
                </div>
              </section>

              <!-- ③ 参数对照（2026-09-20 入住）：原「视频编码」一级页签底部那一整块。
                   ⭐ 它落在这里是**填空**：这一格从 2026-09-19 起就空着（原「提交」卡的下发入口
                      搬去了画布浮条），而参数对照本来就该跟"正在编辑的编码参数"同屏。
                   三行**不同源**，对应关系必须写在界面上，不能让人以为天然同源：
                     · 下发 = 本次提交的期望值（草稿）
                     · 回读 = 设备最近一次回读事实（不是草稿，否则一改就跟着变）
                     · 实测 = 当前正在播的那一路的采样（探针 / ffprobe）
                   ⭐ 这里就是 A-5 的验收闭环：平台改分辨率 → 拉流实测跟着变。
                   ⛔ 留在底栏、别挪回侧栏：三行并排才读得出"设备到底跟没跟"，窄侧栏里会折成六行。
                   ⛔ 卡上**不再放**读取 / 还原 / 下发三颗按钮：侧栏抽屉的参数头已经有同一排
                      （`dcg-embedded-actions`），同一屏两套同名按钮是本仓点过名的坑。
                      这里只留「码流」——它与侧栏「配置文件」下拉同一个真源（`selectedVideoStream`）。 -->
              <section class="linked-section linked-card" data-testid="video-param-compare-card">
                <header class="linked-card-hd">
                  <span class="section-title"><Video :size="13" />参数对照</span>
                  <span class="linked-card-actions">
                    <em
                      class="vpc-verdict"
                      :class="`is-${videoParamVerdict.tone}`"
                      data-testid="video-param-compare-verdict"
                      :title="videoParamVerdictTitle"
                      >{{ videoParamVerdict.text }}</em
                    >
                    <label class="linked-inline-select">
                      <span>码流</span>
                      <select
                        data-testid="video-param-bottom-stream"
                        :value="String(videoParamCompareStream?.streamNumber ?? 0)"
                        @change="selectVideoStream(($event.target as HTMLSelectElement).value)"
                      >
                        <option v-for="row in videoParamsDraft" :key="row.streamNumber" :value="row.streamNumber">
                          {{ row.streamNumber === 0 ? "主码流" : `子码流 ${row.streamNumber}` }}
                        </option>
                      </select>
                    </label>
                  </span>
                </header>
                <!-- ⭐ 2026-09-20 三行 → **两行**（老板：这张卡"缩小一下"）。
                     ⛔ 删掉的是「下发」（= 侧栏表单里的草稿值，本来就看得见，且侧栏
                        `dcg-params-foot` 写了「待下发 N 项」）；
                        留下的是**只有这里**才比得出来的那一对：设备声明的 vs 画面在播的。 -->
                <div v-if="videoParamCompareStream" class="vpc-grid" data-testid="video-param-compare">
                  <div class="vpc-row" data-testid="video-param-compare-read">
                    <span>设备回读</span>
                    <strong
                      >{{ videoFormatText(videoParamCompareReadRow()?.videoFormat) }} ·
                      {{ resolutionText(videoParamCompareReadRow()?.resolution) }} ·
                      {{ frameRateText(videoParamCompareReadRow()?.frameRate) }}</strong
                    >
                  </div>
                  <div class="vpc-row" data-testid="video-param-compare-measured">
                    <span>画面实测</span>
                    <strong>
                      <i :class="{ 'is-differ': videoParamDiffs.codec === true }">{{ streamInfo.videoCodec }}</i> ·
                      <i :class="{ 'is-differ': videoParamDiffs.resolution === true }">{{ streamInfo.resolution }}</i> ·
                      <i :class="{ 'is-differ': videoParamDiffs.fps === true }">{{
                        streamInfo.videoFps ? `${streamInfo.videoFps} fps` : "—"
                      }}</i>
                      <em v-if="liveMetrics.bitrate" class="vpc-bitrate">{{ liveMetrics.bitrate }} kbps</em>
                    </strong>
                  </div>
                </div>
                <p v-else class="vpc-empty" data-testid="video-param-compare-empty">
                  还没有回读值 —— 点左侧「画面遮挡」卡上的「读取」，拿到设备参数后这里显示两行对照。
                </p>
              </section>
            </div>
          </div>

          <div
            v-if="canMonitorPlayback || canDiagnosePlayback"
            v-show="activeTab === 'probe'"
            class="linked-detail"
            data-testid="linked-detail-probe"
          >
            <div class="linked-probe-layout">
              <!-- 轨道明细:视频音频合成一张卡。
                                 音频原来的「采样率」「声道」是从 monitorSnapshot 借来的、不是探针数据,
                                 现在流信息块的音频栏已经在显示,这里删掉;codec 同理(流信息已有编码)。
                                 去重后音频只剩 2 项,再单独占半个详情条就太空了,所以合并。 -->
              <section class="linked-section probe-detail-card">
                <div class="section-hd first">
                  <span class="section-title"><Video :size="13" />轨道明细</span>
                  <span class="section-meta">{{
                    probeResult ? `${[probeResult.video, probeResult.audio].filter(Boolean).length} 条轨道` : "待采样"
                  }}</span>
                </div>
                <div class="probe-track-merged">
                  <div class="probe-track-row video">
                    <span class="probe-track-kind"><Video :size="12" />视频</span>
                    <div class="probe-track-cells">
                      <div>
                        <span>精确 FPS</span
                        ><strong>{{ probeResult?.video?.fps == null ? "—" : probeResult.video.fps.toFixed(1) }}</strong>
                      </div>
                      <div>
                        <span>采样帧</span><strong>{{ probeResult?.video?.frameCount ?? "—" }}</strong>
                      </div>
                      <div>
                        <span>关键帧</span><strong>{{ probeResult?.video?.keyFrameCount ?? "—" }}</strong>
                      </div>
                      <div>
                        <span>GOP</span
                        ><strong>{{ probeResult?.video?.gop == null ? "—" : `${probeResult.video.gop.toFixed(1)} 帧` }}</strong>
                      </div>
                    </div>
                  </div>
                  <div class="probe-track-row audio">
                    <span class="probe-track-kind"><Activity :size="12" />音频</span>
                    <div class="probe-track-cells">
                      <div>
                        <span>采样帧</span><strong>{{ probeResult?.audio?.frameCount ?? "—" }}</strong>
                      </div>
                      <div>
                        <span>帧间隔</span
                        ><strong>{{
                          probeResult?.audio?.averageIntervalMs == null
                            ? "—"
                            : `${probeResult.audio.averageIntervalMs.toFixed(1)} ms`
                        }}</strong>
                      </div>
                    </div>
                  </div>
                </div>
              </section>

              <!-- 时间戳监控:从侧栏移到这里。它是采样结果而不是操作器,
                                 按"侧栏放操作、详情条放结果"的分工本来就该在下面。 -->
              <section class="linked-section probe-detail-card">
                <div class="section-hd first">
                  <span class="section-title"><Gauge :size="13" />时间戳监控</span>
                  <span class="section-meta" :class="{ good: probeResult?.health.status === 'ok' }">
                    {{ probeResult ? (probeResult.health.status === "ok" ? "平稳" : "需关注") : "待检测" }}
                  </span>
                </div>
                <div class="probe-health-grid">
                  <div>
                    <span>视频 DTS 间隔</span
                    ><strong>{{
                      probeResult?.timestamps.videoDtsIntervalMeanMs == null
                        ? "—"
                        : `${probeResult.timestamps.videoDtsIntervalMeanMs.toFixed(1)} ms`
                    }}</strong
                    ><em>均值</em>
                  </div>
                  <div>
                    <span>帧到达抖动</span
                    ><strong>{{
                      probeResult?.timestamps.arrivalJitterMs == null
                        ? "—"
                        : `${probeResult.timestamps.arrivalJitterMs.toFixed(1)} ms`
                    }}</strong
                    ><em>标准差</em>
                  </div>
                  <div>
                    <span>PTS-DTS</span
                    ><strong>{{
                      probeResult?.timestamps.ptsDtsMaxMs == null ? "—" : `${probeResult.timestamps.ptsDtsMaxMs.toFixed(1)} ms`
                    }}</strong
                    ><em>最大值</em>
                  </div>
                  <div>
                    <span>音视频交织</span
                    ><strong>{{
                      probeResult?.timestamps.avArrivalSkewMaxMs == null
                        ? "—"
                        : `${probeResult.timestamps.avArrivalSkewMaxMs.toFixed(1)} ms`
                    }}</strong
                    ><em>最大偏差</em>
                  </div>
                </div>
              </section>

              <!-- 概览层:按时间分桶看全量帧的到达密度与断档位置。
                                 逐帧散点在弹窗里看 —— 侧栏这一栏约 250px 宽,放不下也不该放。 -->
              <section class="linked-section probe-detail-card">
                <div class="section-hd first">
                  <span class="section-title"><Signal :size="13" />帧到达时间线</span>
                  <span class="section-meta" :class="{ good: probeOverview && probeOverview.stallCount === 0 }">{{
                    frameOverviewMeta
                  }}</span>
                </div>
                <button
                  type="button"
                  class="frame-overview"
                  :class="{ muted: !probeOverview }"
                  :disabled="!probeOverview"
                  data-testid="probe-timeline-open"
                  :aria-label="frameOverviewAriaLabel"
                  @click="openProbeTimelineDialog"
                >
                  <template v-if="probeOverview">
                    <div class="frame-overview-bars">
                      <span
                        v-for="(bucket, index) in probeOverview.buckets"
                        :key="index"
                        class="frame-overview-bar"
                        :class="{ stalled: bucket.stalled, empty: bucket.count === 0 }"
                        :style="{ height: `${probeBucketHeight(bucket)}%` }"
                      ></span>
                    </div>
                    <div class="frame-overview-foot">
                      <span
                        >{{ probeOverview.totalFrames }} 帧<template v-if="probeOverview.truncated">
                          · 明细含末尾 {{ probeOverview.sampledFrames }} 帧</template
                        ></span
                      >
                      <span class="frame-overview-cta">查看逐帧详情<Maximize2 :size="11" /></span>
                    </div>
                  </template>
                  <!-- 空态与采样中态:柱状区中央的引导层。 -->
                  <div v-else class="frame-overview-empty">
                    <template v-if="probeState === 'sampling'">
                      <Loader2 :size="18" class="spin" />
                      <span>正在采集帧到达数据</span>
                    </template>
                    <template v-else>
                      <Activity :size="18" />
                      <span>启动检测后展示全量帧到达概览</span>
                    </template>
                  </div>
                </button>
              </section>
            </div>
          </div>

          <!-- 「视频编码」页签的底栏块 2026-09-20 已并入上方「画面设置」底栏第三格（参数对照卡）。
               ⛔ 别在这里恢复一份：同一屏两套「参数对照」就又要靠人猜哪份是准的。 -->
        </div>
      </section>
      <!-- 右侧功能栏 -->
      <aside
        v-if="!sideCollapsed && visibleTabs.length"
        class="sidebar"
        :class="{
          'sidebar-probe': activeTab === 'probe',
          'sidebar-deviceconfig': isConfigWorkspace
        }"
      >
        <!-- 一级页签：横向均分整行宽（`grid-auto-flow: column` + `minmax(0, 1fr)`），
             加减页签只改 `tabs` 数组、不用回来调 CSS。
             ⚠️ 这个位置只放得下 3 个页签（每个 ~112px）；再多就得换回竖排/滚动。
             原来那行「当前页名」标题（`workspace-heading`）随页签一起删了 ——
             高亮的页签本身就是"现在在哪一页"，再写一遍是重复。 -->
        <nav v-if="visibleTabs.length" class="tabs" aria-label="播放工作区">
          <button
            v-for="tab in visibleTabs"
            :key="tab.key"
            type="button"
            class="tab"
            :class="{ active: activeTab === tab.key }"
            :aria-current="activeTab === tab.key ? 'page' : undefined"
            :data-testid="`linked-tab-${tab.key}`"
            :title="tab.description"
            @click="activeTab = tab.key"
          >
            <component :is="tab.icon" :size="14" />
            <span>{{ tab.label }}</span>
            <!-- 未下发的画面改动：浮条只在「画面设置」页露头，离开后就没了入口
                 （草稿还在，只是看不见了）。这枚角标补上那段盲区 —— 零打扰，
                 但用户切到任何页签都带着它回来。
                 ⛔ 只在该页签**不是当前页**时显示：人在画面设置页时浮条已经把这事
                    说清楚了，角标再亮一次是重复提醒。 -->
            <em
              v-if="tab.key === 'deviceconfig' && activeTab !== 'deviceconfig' && pictureDirtyCount > 0"
              class="tab-draft-dot"
              data-testid="linked-tab-draft-dot"
              :title="`${pictureDraftSummary} 未下发`"
            ></em>
          </button>
        </nav>

        <!-- Tab 面板容器 -->
        <div class="panels">
          <!-- ═══════════ 云台控制 ═══════════ -->
          <div v-if="canPtzPanel" v-show="activeTab === 'ptz'" class="panel" data-testid="linked-side-ptz">
            <!-- 模式切换:速度控制 / 精准控制(2022) -->
            <div class="mode-switch">
              <button :class="{ active: ptzMode === 'speed' }" @click="ptzMode = 'speed'"><Compass :size="13" />速度控制</button>
              <button data-testid="ptz-mode-precise" :class="{ active: ptzMode === 'precise' }" @click="ptzMode = 'precise'">
                <Crosshair :size="13" />精准定位<span class="tag-2022">2022</span>
              </button>
            </div>

            <!-- 速度模式:拖拽摇杆 + 变倍 + 速度 -->
            <div v-show="ptzMode === 'speed'" class="ptz-speed">
              <div
                class="joystick-stage"
                :class="{ active: joystickDragging }"
                role="group"
                tabindex="0"
                aria-label="云台方向摇杆"
                @pointerdown.prevent="startJoystick"
                @pointermove.prevent="moveJoystick"
                @pointerup.prevent="endJoystick"
                @pointercancel.prevent="endJoystick"
                @keydown="handleJoystickKeydown"
                @keyup="handleJoystickKeyup"
              >
                <div class="joystick-base"></div>
                <div class="joystick-dots" aria-hidden="true">
                  <span class="joystick-dot dot-top"></span>
                  <span class="joystick-dot dot-top-right"></span>
                  <span class="joystick-dot dot-right"></span>
                  <span class="joystick-dot dot-bottom-right"></span>
                  <span class="joystick-dot dot-bottom"></span>
                  <span class="joystick-dot dot-bottom-left"></span>
                  <span class="joystick-dot dot-left"></span>
                  <span class="joystick-dot dot-top-left"></span>
                </div>
                <span class="joystick-label top">上</span>
                <span class="joystick-label diagonal top-right">右上</span>
                <span class="joystick-label right">右</span>
                <span class="joystick-label diagonal bottom-right">右下</span>
                <span class="joystick-label bottom">下</span>
                <span class="joystick-label diagonal bottom-left">左下</span>
                <span class="joystick-label left">左</span>
                <span class="joystick-label diagonal top-left">左上</span>
                <div class="joystick-handle" :style="joystickHandleStyle" aria-hidden="true">
                  <span></span>
                </div>
              </div>

              <div class="talk-mode-switch" aria-label="对讲模式">
                <button
                  :class="{ active: talkMode === 'broadcast' }"
                  :disabled="talkState !== 'idle' || !talkAvailable"
                  :title="capabilityActionTitle('broadcast', '广播')"
                  @click="talkMode = 'broadcast'"
                >
                  广播
                </button>
                <button
                  :class="{ active: talkMode === 'talk' }"
                  :disabled="talkState !== 'idle' || !talkAvailable"
                  :title="capabilityActionTitle('talk', 'Talk')"
                  @click="talkMode = 'talk'"
                >
                  Talk
                </button>
              </div>
              <button
                class="talk-button"
                data-testid="talk-button"
                :class="{ active: talkState !== 'idle' }"
                :disabled="!isAudioCapable"
                :title="capabilityActionTitle(talkMode, talkMode === 'broadcast' ? '广播对讲' : '双向对讲')"
                :aria-pressed="talkState === 'talking'"
                @click="toggleTalk"
              >
                <Mic :size="14" />
                <span
                  v-if="talkState === 'talking'"
                  class="talk-wave"
                  data-testid="talk-wave"
                  :style="{ '--talk-level': String(talkLevel) }"
                  aria-hidden="true"
                >
                  <i v-for="bar in 4" :key="bar" :style="{ animationDelay: `${(bar - 1) * -0.17}s` }"></i>
                </span>
                <span>{{ talkButtonText }}</span>
              </button>

              <div class="speed-row">
                <label>
                  <span><Gauge :size="12" />移动速度</span>
                  <input v-model.number="moveSpeed" type="range" min="1" max="10" />
                  <em>{{ moveSpeed }}</em>
                </label>
              </div>

              <div class="lens-grid">
                <div class="lens-item">
                  <span class="lens-label"><ZoomIn :size="12" />变倍</span>
                  <div class="lens-btns">
                    <button
                      title="放大"
                      @pointerdown.prevent="sendPtz('放大')"
                      @pointerup.prevent="sendPtz('停止')"
                      @pointerleave="sendPtz('停止')"
                      @pointercancel="sendPtz('停止')"
                    >
                      <ZoomIn :size="14" />
                    </button>
                    <button
                      title="缩小"
                      @pointerdown.prevent="sendPtz('缩小')"
                      @pointerup.prevent="sendPtz('停止')"
                      @pointerleave="sendPtz('停止')"
                      @pointercancel="sendPtz('停止')"
                    >
                      <ZoomOut :size="14" />
                    </button>
                  </div>
                </div>
                <div class="lens-item">
                  <span class="lens-label"><FocusIcon :size="12" />聚焦</span>
                  <div class="lens-btns">
                    <button
                      title="远焦(按住连续)"
                      @pointerdown.prevent="sendPtz('远焦')"
                      @pointerup.prevent="sendPtz('镜头停止')"
                      @pointerleave="sendPtz('镜头停止')"
                      @pointercancel="sendPtz('镜头停止')"
                    >
                      远
                    </button>
                    <button
                      title="近焦(按住连续)"
                      @pointerdown.prevent="sendPtz('近焦')"
                      @pointerup.prevent="sendPtz('镜头停止')"
                      @pointerleave="sendPtz('镜头停止')"
                      @pointercancel="sendPtz('镜头停止')"
                    >
                      近
                    </button>
                  </div>
                </div>
                <div class="lens-item">
                  <span class="lens-label"><Circle :size="12" />光圈</span>
                  <div class="lens-btns">
                    <button
                      title="开大(按住连续)"
                      @pointerdown.prevent="sendPtz('光圈+')"
                      @pointerup.prevent="sendPtz('镜头停止')"
                      @pointerleave="sendPtz('镜头停止')"
                      @pointercancel="sendPtz('镜头停止')"
                    >
                      +
                    </button>
                    <button
                      title="缩小(按住连续)"
                      @pointerdown.prevent="sendPtz('光圈-')"
                      @pointerup.prevent="sendPtz('镜头停止')"
                      @pointerleave="sendPtz('镜头停止')"
                      @pointercancel="sendPtz('镜头停止')"
                    >
                      −
                    </button>
                  </div>
                </div>
              </div>
            </div>

            <!-- 精准控制模式(2022 新增):Pan/Tilt/Zoom 绝对定位 -->
            <div v-show="ptzMode === 'precise'" class="ptz-precise">
              <div class="precise-hint">
                <Info :size="12" />
                <span>基于 <strong>PTZPreciseCtrl</strong>(2022),精准角度定位。需设备支持精准 PTZ 协议。</span>
              </div>
              <div class="axis-row">
                <label>
                  <span>Pan 水平角(°)</span>
                  <div class="axis-ctrl">
                    <input v-model.number="precisePan" type="range" min="0" max="360" step="0.1" />
                    <input v-model.number="precisePan" type="number" min="0" max="360" step="0.1" class="axis-num" />
                  </div>
                </label>
              </div>
              <div class="axis-row">
                <label>
                  <span>Tilt 俯仰角(°)</span>
                  <div class="axis-ctrl">
                    <input v-model.number="preciseTilt" type="range" min="-90" max="90" step="0.1" />
                    <input v-model.number="preciseTilt" type="number" min="-90" max="90" step="0.1" class="axis-num" />
                  </div>
                </label>
              </div>
              <div class="axis-row">
                <label>
                  <span>Zoom 变倍(x)</span>
                  <div class="axis-ctrl">
                    <input v-model.number="preciseZoom" type="range" min="1" max="32" step="0.1" />
                    <input v-model.number="preciseZoom" type="number" min="1" max="32" step="0.1" class="axis-num" />
                  </div>
                </label>
              </div>
              <div class="precise-actions">
                <button class="btn-primary sm" data-testid="ptz-precise-apply" @click="sendPrecise">
                  <Target :size="13" />应用定位
                </button>
                <button class="btn-ghost sm" @click="readPreciseStatus"><Navigation :size="13" />读取当前位置</button>
              </div>
            </div>

            <!-- 3D 拖拽(2026-09-20 从「高级」详情区的「画面控制」卡搬来)
                 ① 位置:3D 放大/缩小本质就是"框选区域做变倍+定位",放在镜头组(变倍/聚焦/
                    光圈)这一族底下最自洽;详情区那 4 张卡都是"设备侧带编号/开关的能力",
                    塞进去语义不齐。
                 ② ⛔ 刻意放在 .ptz-speed / .ptz-precise **之外**:它是画面级手势,与
                    "速度控制 / 精准定位"正交。塞进 .ptz-speed 里的话,一旦切到精准定位模式
                    按钮就消失,而 dragZoomMode 还开着 —— 画面停在拖框态却找不到取消入口。
                 ③ ⛔ 自己挡 canControlDevice:动作侧 toggleDragZoomMode 第一句就是
                    `if (!canControlDevice.value) return`,而本面板的可见性门禁是
                    canPtzPanel(一堆 ptz:* 权限)。不挡就会出现"有 ptz 权限、没 device:control"
                    的账号看得见按钮却点不动 —— 死按钮比看不见更糟。(原「高级」里那张卡
                    本来就没挡,本次一并修掉。) -->
            <div v-if="canControlDevice" class="ptz-drag-zoom" data-testid="ptz-drag-zoom">
              <span class="lens-label"><Move3d :size="12" />3D 拖拽</span>
              <div class="drag-zoom-switch" aria-label="3D 拖拽方向">
                <button
                  :class="{ active: dragZoomMode && dragZoomAction === 'drag_zoom_in' }"
                  data-testid="ptz-drag-zoom-in"
                  :title="capabilityActionTitle('dragZoom', '3D 放大')"
                  :disabled="isAdvancedPending('drag_zoom_in')"
                  :aria-pressed="dragZoomMode && dragZoomAction === 'drag_zoom_in'"
                  @click="toggleDragZoomMode('drag_zoom_in')"
                >
                  {{ dragZoomMode && dragZoomAction === "drag_zoom_in" ? "取消 3D 放大" : "3D 放大" }}
                </button>
                <button
                  :class="{ active: dragZoomMode && dragZoomAction === 'drag_zoom_out' }"
                  data-testid="ptz-drag-zoom-out"
                  :title="capabilityActionTitle('dragZoom', '3D 缩小')"
                  :disabled="isAdvancedPending('drag_zoom_out')"
                  :aria-pressed="dragZoomMode && dragZoomAction === 'drag_zoom_out'"
                  @click="toggleDragZoomMode('drag_zoom_out')"
                >
                  {{ dragZoomMode && dragZoomAction === "drag_zoom_out" ? "取消 3D 缩小" : "3D 缩小" }}
                </button>
              </div>
            </div>

            <!-- 请求关键帧(2026-09-20 从「高级」详情区的「媒体控制」卡搬来)
                 ① 位置:它是"点一下发一条、不回传"的画面级即时操作,与摇杆/变倍/3D 拖拽同族,
                    留在侧栏;详情区那 4 张卡都是"带编号/开关的设备侧能力",塞进去语义不齐。
                 ② ⛔ 与 3D 拖拽一样自己挡 canControlDevice:动作侧 runAdvancedAction 第一句
                    就是 `if (!canControlDevice.value || !props.channel) return`,而本面板的
                    可见性门禁是 canPtzPanel(一堆 ptz:* 权限)。不挡就会出现"有 ptz 权限、
                    没 device:control"的账号看得见按钮却点不动 —— 死按钮比看不见更糟。
                 ③ 文案沿用标准口径:IFameCmd 只有"送达"没有"执行结论"(附录 A 无回读手段),
                    所以状态词只说"已发送",别写成"已生效"。 -->
            <div v-if="canControlDevice" class="ptz-iframe" data-testid="ptz-iframe">
              <span class="lens-label"><Video :size="12" />关键帧</span>
              <button
                data-testid="ptz-iframe-request"
                :title="capabilityActionTitle('iFrame', '请求关键帧')"
                :disabled="isAdvancedPending('iframe')"
                @click="runAdvancedAction('iframe')"
              >
                <Loader2 v-if="isAdvancedPending('iframe')" :size="12" class="spin" /><Video v-else :size="12" />
                <span>请求关键帧</span>
              </button>
            </div>

            <!-- 目标跟踪（GB/T 28181-2022 A.2.3.1.14）
                 ① 位置:与 3D 拖拽/关键帧同族 —— 都是"对着这块画面发一条即时命令",
                    而手动跟踪的框只能在这块画面上圈出来(见 targetTrackMode 的 KDoc)。
                 ② ⛔ 自己挡 canControlDevice:动作侧 toggleTargetTrackMode / submitTargetTrack
                    第一句就是 `if (!canControlDevice.value) return`,而本面板的可见性门禁是
                    canPtzPanel。不挡就会出现"有 ptz 权限、没 device:control"的账号看得见按钮
                    却点不动 —— 死按钮比看不见更糟。
                 ③ ⛔⛔ 状态词只能是「已下发」:目标跟踪是**无应答命令**(9.3.1 d) + 表 1 序号 13),
                    且 2022 全文没有"查设备在跟踪什么"的命令 ⇒ 平台查不到、也不该假装查得到。
                    下面这行文字的主语永远是"平台",不是"设备"。 -->
            <div v-if="canControlDevice" class="ptz-target-track" data-testid="ptz-target-track">
              <span class="lens-label"><ScanEye :size="12" />目标跟踪<span class="tag-2022">2022</span></span>
              <div class="target-track-switch" aria-label="目标跟踪方式">
                <button
                  data-testid="ptz-target-track-auto"
                  :title="capabilityActionTitle('targetTrack', '自动跟踪')"
                  :disabled="targetTrackPending"
                  @click="submitTargetTrack('Auto')"
                >
                  <Loader2 v-if="targetTrackPending" :size="12" class="spin" /><ScanEye v-else :size="12" />
                  <span>自动跟踪</span>
                </button>
                <button
                  :class="{ active: targetTrackMode }"
                  data-testid="ptz-target-track-manual"
                  :title="capabilityActionTitle('targetTrack', '手动框选目标')"
                  :disabled="targetTrackPending"
                  :aria-pressed="targetTrackMode"
                  @click="toggleTargetTrackMode"
                >
                  <Square v-if="targetTrackMode" :size="12" /><ScanEye v-else :size="12" />
                  <span>{{ targetTrackMode ? "取消框选" : "框选跟踪" }}</span>
                </button>
                <button
                  data-testid="ptz-target-track-stop"
                  :title="capabilityActionTitle('targetTrack', '停止跟踪')"
                  :disabled="targetTrackPending"
                  @click="submitTargetTrack('Stop')"
                >
                  <CircleSlash :size="12" />
                  <span>停止跟踪</span>
                </button>
              </div>
              <!-- ⛔ 这三行文字合起来才是完整口径,少一行都会被读成"设备在做某事":
                    「平台最近一次下发…」(全知的一侧) + 「已下发,设备未回执」(没有回执) +
                    「平台无法得知设备实际状态」(所以别问平台设备现在在跟踪什么)。 -->
              <p class="target-track-state" data-testid="target-track-intent">
                {{ targetTrackIntentText }}
              </p>
              <p v-if="targetTrackStatus" class="target-track-status" data-testid="target-track-status">
                {{ targetTrackStatus }}
              </p>
              <p v-else class="target-track-tip" data-testid="target-track-tip">
                「自动跟踪」「停止跟踪」一键下发；「框选跟踪」要在画面上框住目标（坐标按画面实际渲染尺寸换算）。
              </p>
              <p v-if="targetTrackError" class="target-track-error" data-testid="target-track-error">
                {{ targetTrackError }}
              </p>
            </div>
          </div>
          <!-- ═══════════ 视频探针 ═══════════ -->
          <div
            v-if="canMonitorPlayback || canDiagnosePlayback"
            v-show="activeTab === 'probe'"
            class="panel probe-panel"
            data-testid="linked-side-probe"
          >
            <!-- 流信息:2 秒轮询的实时指标。和探针放同一个面板 —— 两者回答的是同一个问题
                             ("这路流健康吗"),区别只在一个持续刷新、一个手动采样。所以两块的标题上
                             都写明刷新语义,免得把十分钟前那次采样的数字当成当下的值。 -->
            <section class="stream-brief probe-card" data-testid="stream-brief">
              <div class="section-hd first">
                <span class="section-title"><Signal :size="13" />概览</span>
                <span class="section-meta">2 秒刷新 · {{ monitorCollectedAtText }}</span>
              </div>
              <div class="stream-brief-overview">
                <div>
                  <span>当前观看</span>
                  <strong>{{ readerCount }}</strong>
                  <small>累计 {{ totalReaderCount }}</small>
                </div>
                <div>
                  <span>数据速率</span>
                  <strong>{{ monitorBytesSpeedText }}</strong>
                  <small>累计 {{ monitorTotalBytesText }}</small>
                </div>
                <div>
                  <span>媒体节点</span>
                  <strong :title="streamInfo.nodeName">{{ streamInfo.nodeName }}</strong>
                  <small :title="streamInfo.nodeHost">{{ streamInfo.nodeHost }}</small>
                </div>
                <div>
                  <span>流 ID</span>
                  <strong :title="streamInfo.streamId || '—'">{{ streamInfo.streamId || "—" }}</strong>
                  <small :title="`SSRC ${streamInfo.ssrc || '—'} · APP ${playResult?.app || '—'}`">
                    SSRC {{ streamInfo.ssrc || "—" }} · APP {{ playResult?.app || "—" }}
                  </small>
                </div>
              </div>

              <div class="stream-brief-split">
                <section class="stream-brief-kind video">
                  <header><Video :size="12" />视频</header>
                  <div class="stream-brief-rows">
                    <div>
                      <span>编码</span><strong>{{ streamInfo.videoCodec }}</strong>
                    </div>
                    <div>
                      <span>分辨率</span><strong>{{ streamInfo.resolution }}</strong>
                    </div>
                    <div>
                      <span>帧率</span><strong>{{ streamInfo.videoFps || "—" }}</strong>
                    </div>
                    <div>
                      <span>丢包</span>
                      <strong :class="{ warn: (liveMetrics.videoLoss ?? 0) > 0.005, err: (liveMetrics.videoLoss ?? 0) > 0.02 }">{{
                        formatLoss(liveMetrics.videoLoss)
                      }}</strong>
                    </div>
                  </div>
                </section>
                <section class="stream-brief-kind audio">
                  <header><Activity :size="12" />音频</header>
                  <div class="stream-brief-rows">
                    <div>
                      <span>编码</span><strong>{{ streamInfo.audioCodec }}</strong>
                    </div>
                    <div>
                      <span>采样率</span
                      ><strong>{{ streamInfo.audioSampleRate ? `${streamInfo.audioSampleRate} Hz` : "—" }}</strong>
                    </div>
                    <div>
                      <span>声道</span><strong>{{ monitorAudioTrack?.channels || "—" }}</strong>
                    </div>
                    <div>
                      <span>丢包</span>
                      <strong :class="{ warn: (liveMetrics.audioLoss ?? 0) > 0.005, err: (liveMetrics.audioLoss ?? 0) > 0.02 }">{{
                        formatLoss(liveMetrics.audioLoss)
                      }}</strong>
                    </div>
                  </div>
                </section>
              </div>
            </section>

            <section class="probe-card" data-testid="probe-check">
              <div class="section-hd first">
                <span class="section-title"><Activity :size="13" />逐帧健康检测</span>
                <span class="probe-status" :class="probeState"> <span class="dot"></span>{{ probeStatusText }} </span>
              </div>

              <div v-if="canDiagnosePlayback" class="probe-action-row">
                <button
                  class="probe-action"
                  data-testid="probe-start"
                  :disabled="phase !== 'playing' || probeState === 'sampling'"
                  @click="startProbe"
                >
                  <Loader2 v-if="probeState === 'sampling'" :size="14" class="spin" />
                  <Play v-else :size="14" />
                  <span>{{ probeButtonText }}</span>
                </button>
                <a-select
                  v-model="probeDurationMs"
                  class="probe-duration"
                  data-testid="probe-duration"
                  aria-label="采样时长"
                  :disabled="probeState === 'sampling'"
                >
                  <a-option v-for="duration in probeDurations" :key="duration.value" :value="duration.value">{{
                    duration.label
                  }}</a-option>
                </a-select>
              </div>

              <div class="probe-summary" :class="{ muted: probeState !== 'complete' }">
                <div>
                  <span>采样时长</span>
                  <strong>{{ probeResult ? (probeResult.summary.sampleDurationMs / 1000).toFixed(2) : "—" }}<em>s</em></strong>
                </div>
                <div>
                  <span>采集帧数</span>
                  <strong>{{ probeResult?.summary.frameCount ?? "—" }}<em>帧</em></strong>
                </div>
                <div>
                  <span>采样流量</span>
                  <strong>{{ probeResult ? Math.round(probeResult.summary.totalBytes / 1024) : "—" }}<em>KB</em></strong>
                </div>
              </div>

              <!-- 结论条压成单行:采样完成时它是唯一新增内容,原来两行 + padding 约 45px,
                                 完成态一到就把整个侧栏撑破画面高度。副信息(完成时间/具体问题)挪到 title。 -->
              <div
                v-if="probeState === 'complete'"
                class="probe-verdict"
                :class="{ warning: probeResult?.health.status === 'warning', error: probeResult?.health.status === 'error' }"
                :title="`完成于 ${probeFinishedAt} · ${probeResult?.health.issues?.[0]?.message || '未发现异常帧间隔'}`"
              >
                <CheckCircle2 v-if="probeResult?.health.status === 'ok'" :size="14" />
                <AlertTriangle v-else :size="14" />
                <strong>{{ probeResult?.health.status === "ok" ? "流健康，帧序与时间戳连续" : "检测发现需要关注的问题" }}</strong>
              </div>
            </section>
          </div>
          <!-- ═══════════ 视频参数 ═══════════
                         2026-09-18 从"高级"拆出:它是 A.2.3.2 设备配置类(读 A.2.4.7 / 写 A.2.3.2.5),
                         与云台预置位/巡航不是一类。可见性用 canViewPtz(见 TabKey 处的注释)。
                        2026-09-20 原"高级"页签整体退役:请求关键帧并入云台控制侧栏,
                        设备录制/布防撤防/报警复位与图像抓拍配置迁入设备详情抽屉;
                        同日"录像存储/报警控制"两个整页签、"设备维护"整页签(→ 基本参数)
                        也整体迁入同一抽屉。
                        ⛔ 留在控制台的配置页**只剩「画面设置」**了,判据:它改的是**这一路流
                            正在播的时候才有意义**的东西(编码/叠加/遮挡/镜像)。设备侧的配置
                            (基本参数/录像/报警)与"这一路流现在播得怎么样"无关,一律归设备详情抽屉。
                        `can-apply` 一律带 canControlPtz —— 写的是 /device-configs 与 /video-params,
                        种子把它绑在 ptz:control 上,只判"通道在线"会给没有写权限的账号一个点不动的
                        下发按钮(旧版就是这样)。 -->
          <div
            v-if="canViewPtz"
            v-show="isConfigWorkspace"
            class="panel sidebar-deviceconfig-panel"
            data-testid="linked-side-deviceconfig"
          >
            <!-- 「画面设置」的侧栏 = **一个**抽屉、**只挂视频编码**这一组
                 （图像叠加 2026-09-20 整块搬到底栏 `picture-osd-cell`，同一页全展示，不再切组）。
                 ⛔ 不要退回"两个抽屉 + 两个一级页签"：那正是 2026-09-20 合并掉的东西。
                 ⛔ 也不要传 `config-only` —— 它的语义是"排除 video-param 组"，与这里的意图相反。
                 ⛔ `v-model:stream-profile` **必须绑**：底栏对照卡的码流下拉与这里「配置文件」
                    必须是同一路（真源只有 `selectedVideoStream`）。
                 ⛔ 不再绑 `v-model:active-group-key` / `:osd-editing` / `@toggle-osd-edit`：
                    OSD 面板不在这条侧栏里了（底栏那份自己接），留着就是没人读的第二份状态。
                    抽屉这两个 prop 仍保留给**内联渲染 OSD 的宿主**（如免登录的设备配置预览页）。 -->
            <DeviceConfigDrawer
              ref="deviceConfigRef"
              :visible="isConfigWorkspace"
              embedded
              :group-keys="activeConfigGroups"
              v-model:stream-profile="videoParamStreamProfile"
              :device-code="props.channel?.deviceId || ''"
              :online="props.channel?.status === 1"
              :effective-version="videoParamRegisteredVersion"
              :channel-id="props.channel?.id ?? null"
              :channel-name="props.channel?.alias?.trim() || props.channel?.name?.trim() || ''"
              :can-read="canViewPtz"
              :can-apply="canControlPtz && props.channel?.status === 1"
            />
          </div>
        </div>
      </aside>

      <Transition name="asset-drawer">
        <div v-if="canPtzPanel && assetManagerVisible" class="asset-manager-layer" data-testid="asset-manager">
          <button class="asset-manager-mask" aria-label="关闭资源管理" @click="closeAssetManager"></button>
          <aside class="asset-manager-drawer" role="dialog" aria-modal="true" aria-label="云台资源管理">
            <header class="asset-manager-header">
              <div>
                <span>云台资源管理</span>
                <strong>{{ assetManagerTab === "preset" ? "预置位管理" : "巡航轨迹管理" }}</strong>
              </div>
              <button data-testid="asset-manager-close" title="关闭" @click="closeAssetManager"><X :size="16" /></button>
            </header>

            <div class="asset-manager-tabs">
              <button
                data-testid="asset-manager-tab-preset"
                :class="{ active: assetManagerTab === 'preset' }"
                @click="switchAssetManagerTab('preset')"
              >
                <Hash :size="13" /><span>预置位</span><em>{{ presets.length }}</em>
              </button>
              <button
                data-testid="asset-manager-tab-cruise"
                :class="{ active: assetManagerTab === 'cruise' }"
                @click="switchAssetManagerTab('cruise')"
              >
                <Route :size="13" /><span>巡航轨迹</span><em>{{ cruiseTracks.length }}</em>
              </button>
            </div>

            <label class="asset-manager-search">
              <Search :size="14" />
              <input
                v-model="assetSearch"
                type="search"
                :placeholder="assetManagerTab === 'preset' ? '搜索预置位名称或编号' : '搜索巡航名称或编号'"
              />
            </label>

            <div v-if="assetManagerTab === 'preset'" class="asset-manager-inline-actions">
              <button class="asset-manager-add" data-testid="asset-manager-add-preset" @click="openSavePresetDialog">
                <Plus :size="12" /><span>添加预置位</span>
              </button>
            </div>

            <div class="asset-manager-list">
              <template v-if="assetManagerTab === 'preset'">
                <div
                  v-for="p in filteredPresets"
                  :key="p.id"
                  class="asset-manager-row"
                  :class="{ active: activePresetId === p.id }"
                  data-testid="asset-manager-row"
                >
                  <span class="asset-manager-index">#{{ p.id }}</span>
                  <a-tooltip
                    :content="`#${p.id} ${p.name}`"
                    position="top"
                    :mouse-enter-delay="RESOURCE_TOOLTIP_ENTER_DELAY_MS"
                    :data-testid="`preset-manager-tooltip-${p.id}`"
                  >
                    <div class="asset-manager-info">
                      <strong>{{ p.name }}</strong>
                      <small>{{ p.setAt || "尚未记录更新时间" }}</small>
                    </div>
                  </a-tooltip>
                  <div class="asset-manager-actions">
                    <button class="btn-ghost xs" @click="callPreset(p.id)"><Navigation :size="11" />调用</button>
                    <button class="asset-manager-delete" title="删除预置位" @click="deletePreset(p.id)">
                      <Trash2 :size="12" />
                    </button>
                  </div>
                </div>
                <div
                  v-if="filteredPresets.length === 0"
                  class="asset-manager-empty preset-empty-large"
                  data-testid="asset-manager-preset-empty"
                >
                  <Inbox :size="28" class="preset-empty-glyph" />
                  <p class="preset-empty-line-primary">{{ assetSearch ? "没有匹配的预置位" : "暂无预置位" }}</p>
                </div>
              </template>

              <template v-else>
                <a-tooltip
                  v-for="c in filteredCruiseTracks"
                  :key="c.id"
                  :content="cruiseTileTitle(c)"
                  position="top"
                  :mouse-enter-delay="RESOURCE_TOOLTIP_ENTER_DELAY_MS"
                  :data-testid="`cruise-manager-tooltip-${c.id}`"
                >
                  <div
                    class="asset-manager-row"
                    :class="{ active: activeCruiseId === c.id, disabled: !c.enabled && !c.pending, pending: c.pending }"
                    data-testid="asset-manager-row"
                  >
                    <span class="asset-manager-index">#{{ c.id }}</span>
                    <div class="asset-manager-info">
                      <strong>{{ c.name }}</strong>
                      <small>{{ cruiseTrackMeta(c) }} · {{ c.pending ? "可试运行" : c.enabled ? "可调用" : "已禁用" }}</small>
                    </div>
                    <button class="btn-ghost xs" :disabled="!c.enabled && !c.pending" @click="toggleCruise(c.id)">
                      <Square v-if="activeCruiseId === c.id && cruiseState === 'start-sent'" :size="11" />
                      <Play v-else :size="11" />
                      {{ activeCruiseId === c.id && cruiseState === "start-sent" ? "停止" : c.pending ? "试运行" : "启动" }}
                    </button>
                  </div>
                </a-tooltip>
                <div v-if="filteredCruiseTracks.length === 0" class="asset-manager-empty">没有匹配的巡航轨迹</div>
              </template>
            </div>

            <footer class="asset-manager-footer">
              <span>{{
                assetManagerTab === "preset"
                  ? `${filteredPresets.length} 个预置位`
                  : `${filteredCruiseTracks.length} 条巡航轨迹 · ${cruiseSyncLabel}`
              }}</span>
              <button
                v-if="assetManagerTab === 'cruise' && activeCruiseId !== null && cruiseState !== 'stopped'"
                class="btn-ghost xs"
                @click="stopCruise"
              >
                <Square :size="11" />停止全部
              </button>
            </footer>
          </aside>
        </div>
      </Transition>

      <!-- 帧到达时间线详情:侧栏概览条点击后打开。
                 逐帧散点需要的横向空间比概览条大得多,所以单独给弹窗。 -->
      <ProbeTimelineDialog v-if="canDiagnosePlayback" v-model:visible="probeTimelineDialogVisible" :snapshot="probeResult" />

      <a-modal
        v-if="homeSettingsDialogVisible"
        v-model:visible="homeSettingsDialogVisible"
        title="设置看守位"
        modal-class="uvp-system-dialog home-settings-modal"
        :width="430"
        :footer="false"
        :mask-closable="!homeSettingsSubmitting"
        :closable="!homeSettingsSubmitting"
        :esc-to-close="!homeSettingsSubmitting"
        unmount-on-close
        @cancel="closeHomeSettingsDialog"
        @close="closeHomeSettingsDialog"
      >
        <div class="home-settings-form" data-testid="home-settings-dialog">
          <div class="home-settings-field">
            <label for="home-position-preset">归位预置位 <span>*</span></label>
            <select
              id="home-position-preset"
              v-model.number="homeDraft.presetId"
              data-testid="home-preset"
              :disabled="homeSettingsSubmitting"
              @change="homeSettingsTouched = true"
            >
              <option :value="null">请选择预置位</option>
              <option v-for="p in presets" :key="p.id" :value="p.id">#{{ p.id }} · {{ p.name }}</option>
            </select>
          </div>
          <div class="home-settings-field">
            <label for="home-position-reset-time">无云台操作后 <span>*</span></label>
            <div class="home-settings-time">
              <input
                id="home-position-reset-time"
                v-model.number="homeDraft.resetTime"
                data-testid="home-reset-time"
                type="number"
                min="10"
                max="3600"
                :disabled="homeSettingsSubmitting"
                @input="homeSettingsTouched = true"
              />
              <span>秒自动归位</span>
            </div>
          </div>
          <p class="home-settings-description">连续无云台操作达到指定时间后，设备将自动返回所选预置位。</p>
          <p v-if="homeSettingsTouched && !homePositionCanSave" class="home-settings-error" data-testid="home-validation">
            请选择一个已存在的预置位，等待时间必须是 10 至 3600 秒整数。
          </p>
          <div class="home-settings-actions">
            <button class="btn-ghost sm" :disabled="homeSettingsSubmitting" @click="closeHomeSettingsDialog">取消</button>
            <button
              class="btn-primary sm"
              data-testid="home-dialog-submit"
              :disabled="homeSettingsSubmitting || !homePositionCanSave"
              @click="submitHomeSettings"
            >
              <Loader2 v-if="homeSettingsSubmitting" :size="13" class="spin" />
              <ShieldCheck v-else :size="13" />
              {{ homeSettingsActionLabel }}
            </button>
          </div>
        </div>
      </a-modal>

      <a-modal
        v-if="canSavePtzPreset && savePresetDialogVisible"
        v-model:visible="savePresetDialogVisible"
        title="保存预置位"
        ok-text="保存"
        cancel-text="取消"
        modal-class="uvp-system-dialog preset-save-modal"
        :width="380"
        :mask-closable="false"
        :ok-loading="presetDraft?.submitting || false"
        :on-before-ok="handleSavePresetBeforeOk"
        unmount-on-close
        @cancel="closeSavePresetDialog"
        @close="closeSavePresetDialog"
      >
        <div v-if="presetDraft" class="preset-save-form" data-testid="preset-save-dialog">
          <div class="preset-save-row">
            <label class="preset-save-label">编号</label>
            <span class="preset-save-index">#{{ presetDraft.id }}</span>
          </div>
          <div class="preset-save-row">
            <label class="preset-save-label">名称</label>
            <div class="preset-save-field">
              <a-input
                v-model="presetDraft.name"
                allow-clear
                :max-length="16"
                :placeholder="`预置位 ${presetDraft.id}`"
                :disabled="presetDraft.submitting"
                :error="!!presetNameError"
                data-testid="preset-save-name-input"
                @blur="presetNameTouched = true"
                @press-enter="presetNameTouched = true"
              >
                <template #suffix>
                  <span
                    class="preset-save-count"
                    :class="{ ok: presetDraft.name.trim().length > 0 && presetDraft.name.trim().length <= 16 }"
                  >
                    {{ presetDraft.name.length }}/16
                  </span>
                </template>
              </a-input>
              <p v-if="presetNameError" class="preset-save-error">{{ presetNameError }}</p>
              <p v-else class="preset-save-hint">留空将使用默认名「预置位 {{ presetDraft.id }}」</p>
            </div>
          </div>
        </div>
      </a-modal>

      <a-modal
        v-if="canControlPtzCruise && saveCruiseDialogVisible"
        v-model:visible="saveCruiseDialogVisible"
        title="新建巡航轨迹"
        ok-text="创建并下发"
        cancel-text="取消"
        modal-class="uvp-system-dialog cruise-save-modal"
        :width="480"
        :mask-closable="false"
        :closable="!cruiseDraft?.submitting"
        :esc-to-close="!cruiseDraft?.submitting"
        :cancel-button-props="{ disabled: cruiseDraft?.submitting || false }"
        :ok-loading="cruiseDraft?.submitting || false"
        :on-before-ok="handleSaveCruiseBeforeOk"
        :on-before-cancel="canCloseSaveCruiseDialog"
        unmount-on-close
        @cancel="closeSaveCruiseDialog"
        @close="closeSaveCruiseDialog"
      >
        <div v-if="cruiseDraft" class="cruise-save-form" data-testid="cruise-save-dialog">
          <div class="cruise-save-notice">
            <Info :size="14" />
            <span
              >设备会按下面列出的顺序依次走到每个预置位、各停一会儿,然后循环执行。配置会直接写进设备;部分老设备不回传确认,下发后可用「试运行」核对。</span
            >
          </div>
          <div class="cruise-save-row">
            <label class="cruise-save-label">名称</label>
            <div class="cruise-save-field">
              <a-input
                v-model="cruiseDraft.name"
                allow-clear
                :max-length="32"
                :placeholder="`巡航 ${cruiseDraft.trackId}`"
                :disabled="cruiseDraft.submitting"
                data-testid="cruise-save-name-input"
                @blur="cruiseDraftTouched = true"
              >
                <template #suffix>
                  <span
                    class="preset-save-count"
                    :class="{ ok: cruiseDraft.name.trim().length > 0 && cruiseDraft.name.trim().length <= 32 }"
                  >
                    {{ cruiseDraft.name.length }}/32
                  </span>
                </template>
              </a-input>
              <p class="preset-save-hint">名称仅在本平台显示,不会同步到设备。</p>
            </div>
          </div>
          <div class="cruise-save-row">
            <label class="cruise-save-label">巡航点</label>
            <div class="cruise-save-field">
              <div ref="cruiseStopsListEl" class="cruise-stops-list" data-testid="cruise-stops-list">
                <div
                  v-for="(stop, index) in cruiseDraft.stops"
                  :key="stop.key"
                  class="cruise-stop-row"
                  data-testid="cruise-stop-row"
                >
                  <span class="cruise-stop-idx">{{ index + 1 }}</span>
                  <a-select
                    v-model="stop.presetId"
                    :style="{ flex: '1 1 auto', minWidth: '0' }"
                    :disabled="cruiseDraft.submitting"
                    data-testid="cruise-stop-select"
                    placeholder="选择预置位"
                  >
                    <a-option v-for="p in presets" :key="p.id" :value="p.id">#{{ p.id }} · {{ p.name }}</a-option>
                  </a-select>
                  <button
                    class="cruise-stop-move"
                    :disabled="cruiseDraft.submitting || index === 0"
                    title="上移"
                    aria-label="上移巡航点"
                    @click="moveCruiseStop(index, -1)"
                  >
                    ↑
                  </button>
                  <button
                    class="cruise-stop-move"
                    :disabled="cruiseDraft.submitting || index === cruiseDraft.stops.length - 1"
                    title="下移"
                    aria-label="下移巡航点"
                    @click="moveCruiseStop(index, 1)"
                  >
                    ↓
                  </button>
                  <button
                    class="cruise-stop-del"
                    :disabled="cruiseDraft.submitting || cruiseDraft.stops.length <= 1"
                    title="删除该巡航点"
                    aria-label="删除巡航点"
                    @click="removeCruiseStop(index)"
                  >
                    <X :size="11" />
                  </button>
                </div>
              </div>
              <button
                class="cruise-stop-add"
                data-testid="cruise-stop-add-btn"
                :disabled="cruiseDraft.submitting || cruiseDraft.stops.length >= 32"
                @click="addCruiseStop"
              >
                <Plus v-if="cruiseDraft.stops.length < 32" :size="14" />
                <span>{{ cruiseDraft.stops.length >= 32 ? "已达到 32 站上限" : "添加巡航点" }}</span>
                <small v-if="cruiseDraft.stops.length < 32" class="cruise-stop-add-count"
                  >还可添加 {{ 32 - cruiseDraft.stops.length }} 个</small
                >
              </button>
              <p class="preset-save-hint">
                已选 {{ cruiseDraft.stops.length }} 个巡航点。列表从上到下就是设备实际走的顺序,可用 ↑ ↓ 调整。
              </p>
            </div>
          </div>
          <div class="cruise-save-row">
            <label class="cruise-save-label">巡航速度</label>
            <div class="cruise-save-field">
              <div class="cruise-param-line">
                <a-input-number
                  v-model="cruiseDraft.speed"
                  :min="1"
                  :max="4095"
                  :step="1"
                  :style="{ width: '112px' }"
                  :disabled="cruiseDraft.submitting || !cruiseDraft.sendSpeed"
                  data-testid="cruise-save-speed"
                  @blur="cruiseDraftTouched = true"
                />
                <span class="cruise-param-mode">
                  <a-switch
                    v-model="cruiseDraft.sendSpeed"
                    size="small"
                    :disabled="cruiseDraft.submitting"
                    aria-label="是否下发巡航速度设置"
                    data-testid="cruise-send-speed"
                  />
                  <span>{{ cruiseDraft.sendSpeed ? "下发设置" : "不下发" }}</span>
                </span>
              </div>
              <p class="preset-save-hint">
                {{
                  cruiseDraft.sendSpeed
                    ? "取值范围 1-4095,整条轨迹共用一个值(协议按组下发,不支持逐点设置)。快慢由设备自己解释,没有统一物理单位,不同厂家同一个数的实际转速可能不同。"
                    : "本次不下发速度设置,设备保持当前设置。"
                }}
              </p>
            </div>
          </div>
          <div class="cruise-save-row">
            <label class="cruise-save-label">每站停留</label>
            <div class="cruise-save-field">
              <div class="cruise-param-line">
                <a-input-number
                  v-model="cruiseDraft.dwellSec"
                  :min="1"
                  :max="4095"
                  :step="1"
                  :style="{ width: '112px' }"
                  :disabled="cruiseDraft.submitting || !cruiseDraft.sendDwell"
                  data-testid="cruise-save-dwell"
                  @blur="cruiseDraftTouched = true"
                />
                <span class="cruise-save-unit">秒</span>
                <span class="cruise-param-mode">
                  <a-switch
                    v-model="cruiseDraft.sendDwell"
                    size="small"
                    :disabled="cruiseDraft.submitting"
                    aria-label="是否下发巡航停留时间设置"
                    data-testid="cruise-send-dwell"
                  />
                  <span>{{ cruiseDraft.sendDwell ? "下发设置" : "不下发" }}</span>
                </span>
              </div>
              <p class="preset-save-hint">
                {{
                  cruiseDraft.sendDwell
                    ? "单位是秒,范围 1-4095(最长约 68 分钟)。每个预置位停多久由这一个值决定 —— 整条轨迹共用,不支持逐点设置。"
                    : "本次不下发停留时间设置,设备保持当前设置。"
                }}
              </p>
            </div>
          </div>
          <details class="cruise-save-advanced">
            <summary><Settings :size="13" />高级设置</summary>
            <div class="cruise-save-advanced-body">
              <div class="cruise-save-row">
                <label class="cruise-save-label">编号</label>
                <div class="cruise-save-field">
                  <a-input-number
                    v-model="cruiseDraft.trackId"
                    :min="0"
                    :max="255"
                    :step="1"
                    :style="{ width: '96px' }"
                    :disabled="cruiseDraft.submitting"
                    data-testid="cruise-save-track-id"
                    @blur="cruiseDraftTouched = true"
                  />
                  <p class="preset-save-hint">轨迹编号由平台自动分配,通常无需修改。</p>
                </div>
              </div>
              <div class="cruise-save-row">
                <label class="cruise-save-label">覆盖</label>
                <div class="cruise-save-field">
                  <label class="cruise-save-replace">
                    <input
                      v-model="cruiseDraft.replaceExisting"
                      type="checkbox"
                      :disabled="cruiseDraft.submitting"
                      data-testid="cruise-save-replace"
                    />
                    <span>覆盖同编号轨迹</span>
                  </label>
                  <p class="preset-save-hint">启用后会先清空设备中的同编号轨迹,此操作不可撤销。</p>
                </div>
              </div>
            </div>
          </details>
          <p v-if="cruiseDraftError || cruiseDraftSubmitError" class="preset-save-error" data-testid="cruise-save-error">
            {{ cruiseDraftError || cruiseDraftSubmitError }}
          </p>
        </div>
      </a-modal>
    </div>
  </a-modal>
</template>

<style scoped lang="scss">
/* 主体壳子 —— 具体面板样式在后续 chunk 中追加 */
:global(.play-console-container--minimized) {
  pointer-events: none;
}
:global(.play-console-container--minimized .arco-modal-wrapper) {
  overflow: visible;
  pointer-events: none;
}
:global(.play-console-container--minimized .play-console-modal--minimized) {
  overflow: hidden;
  pointer-events: auto;
  border: 1px solid rgb(148 163 184 / 24%);
  border-radius: 12px;
  box-shadow: 0 18px 48px rgb(2 6 23 / 42%);
}
:global(.play-console-modal--minimized .arco-modal-header) {
  height: 48px;
  padding: 0 10px 0 12px;
  background: var(--uvp-panel-bg);
}

.play-console-modal :deep(.arco-modal-body) {
  max-height: calc(100vh - 96px);
  padding: 14px 18px 18px;
  overflow-y: auto;
}

.console-title {
  display: flex;
  gap: 10px;
  align-items: center;
  width: 100%;
  min-width: 0;
}
.console-title.is-minimized {
  cursor: grab;
  user-select: none;
}
.console-title.is-minimized:active {
  cursor: grabbing;
}
.title-icon {
  display: inline-grid;
  place-items: center;
  width: 32px;
  height: 32px;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-radius: 9px;
}
.title-text {
  display: grid;
  gap: 2px;
  min-width: 0;
}
.title-text strong {
  font-size: 14px;
  color: var(--uvp-text-primary);
}
.title-text span {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}

.console-window-actions {
  display: inline-flex;
  flex: 0 0 auto;
  gap: 6px;
  align-items: center;
  margin-left: 4px;
}
.console-title.is-minimized .console-window-actions {
  margin-left: auto;
}
.console-window-action {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  justify-content: center;
  min-width: 58px;
  height: 30px;
  padding: 0 9px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1;
  color: var(--uvp-text-secondary);
  white-space: nowrap;
  cursor: pointer;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
  transition:
    color 0.15s ease,
    background 0.15s ease,
    border-color 0.15s ease,
    box-shadow 0.15s ease,
    transform 0.15s ease;
}
.console-window-action svg {
  flex: 0 0 auto;
}
.console-window-action.is-minimize {
  color: var(--uvp-brand);
  background: color-mix(in srgb, var(--uvp-brand) 7%, var(--uvp-panel-bg));
  border-color: color-mix(in srgb, var(--uvp-brand) 24%, var(--uvp-panel-border));
}
.console-window-action.is-minimize:hover {
  color: var(--uvp-brand-strong);
  background: var(--uvp-brand-soft);
  border-color: var(--uvp-brand);
  box-shadow: 0 4px 12px color-mix(in srgb, var(--uvp-brand) 14%, transparent);
}
.console-window-action.is-config {
  color: var(--uvp-brand-strong, var(--uvp-brand));
  background: color-mix(in srgb, var(--uvp-brand) 5%, var(--uvp-panel-bg));
  border-color: color-mix(in srgb, var(--uvp-brand) 22%, var(--uvp-panel-border));
}
.console-window-action.is-config:hover {
  color: var(--uvp-brand-strong);
  background: var(--uvp-brand-soft);
  border-color: var(--uvp-brand);
  box-shadow: 0 4px 12px color-mix(in srgb, var(--uvp-brand) 14%, transparent);
}
.console-window-action.is-close {
  color: color-mix(in srgb, var(--uvp-danger) 76%, var(--uvp-text-secondary));
  background: color-mix(in srgb, var(--uvp-danger) 4%, var(--uvp-panel-bg));
  border-color: color-mix(in srgb, var(--uvp-danger) 18%, var(--uvp-panel-border));
}
.console-window-action.is-close:hover {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
  box-shadow: 0 4px 12px color-mix(in srgb, var(--uvp-danger) 12%, transparent);
}
.console-window-action:focus-visible {
  outline: 2px solid color-mix(in srgb, var(--uvp-brand) 48%, transparent);
  outline-offset: 2px;
}
.console-window-action:active {
  transform: translateY(1px);
}
.console-window-action.is-compact {
  gap: 0;
  width: 28px;
  min-width: 28px;
  height: 28px;
  padding: 0;
}

.session-badge {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  padding: 4px 10px;
  margin-left: auto;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 999px;
}
.session-badge .dot {
  width: 6px;
  height: 6px;
  background: var(--uvp-text-tertiary);
  border-radius: 50%;
}
.session-badge.active {
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 12%, transparent);
  border-color: color-mix(in srgb, var(--uvp-brand-cyan) 30%, transparent);
}
.session-badge.active .dot {
  background: var(--uvp-brand-cyan);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-brand-cyan) 20%, transparent);
  animation: pulse 1.6s ease-in-out infinite;
}
.session-badge.loading {
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
  border-color: var(--uvp-warning-border);
}
.session-badge.loading .dot {
  background: var(--uvp-warning);
}
.session-badge.error {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
}
.session-badge.error .dot {
  background: var(--uvp-danger);
}
.session-badge.paused {
  color: #a78bfa;
  background: rgb(167 139 250 / 12%);
  border-color: rgb(167 139 250 / 28%);
}
.session-badge.warn {
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
  border-color: var(--uvp-warning-border);
}
.session-badge.warn .dot {
  background: var(--uvp-warning);
}
.session-elapsed {
  padding-left: 8px;
  margin-left: 2px;
  font-size: 10.5px;
  font-style: normal;
  font-weight: 600;
  color: color-mix(in srgb, currentcolor 70%, transparent);
  border-left: 1px solid color-mix(in srgb, currentcolor 24%, transparent);
}

/* 两列 = 画面 + 右侧属性栏。2026-09-21 前这里是 `136px minmax(0, 1fr) 360px`，
 * 第一列是左侧竖排的一级页签；页签回到属性栏顶部后该列退役，宽度还给画面。
 * ⛔ 列数是"唯一真源"：改列数要同步 `.video-frame` / `.stage-wide .video-frame` /
 *    `.linked-info-bar` / `.sidebar` 以及 ≤1080px 分支的 `grid-column`，共 5 处。 */
.console-body {
  position: relative;
  display: grid;
  grid-template-columns: minmax(0, 1fr) 360px;
  gap: 14px;
  min-height: 0;
  isolation: isolate;
}
.console-body.is-minimized {
  display: block;
}
.console-body.is-minimized .stage {
  display: block;
}
.console-body.is-minimized .video-frame {
  display: block;
  width: 100%;
  overflow: hidden;
  border: 0;
  border-radius: 0;
  box-shadow: none;
}
.console-body.is-minimized .video-canvas {
  width: 100%;
  aspect-ratio: 16 / 9;
}
.console-body.is-minimized .protocol-switcher,
.console-body.is-minimized .linked-info-bar,
.console-body.is-minimized .sidebar,
.console-body.is-minimized .asset-manager-layer {
  display: none;
}

.asset-manager-layer {
  position: absolute;
  inset: 0;
  z-index: 20;
  display: flex;
  justify-content: flex-end;
  overflow: hidden;
  border-radius: 12px;
}
.asset-manager-mask {
  position: absolute;
  inset: 0;
  padding: 0;
  cursor: pointer;
  background: rgb(15 23 42 / 34%);
  border: 0;
}
.asset-manager-drawer {
  position: relative;
  z-index: 1;
  display: grid;
  grid-template-rows: auto auto auto auto minmax(0, 1fr) auto;
  width: min(430px, 100%);
  min-width: 0;
  height: 100%;
  background: var(--uvp-panel-bg);
  border-left: 1px solid var(--uvp-panel-border);
  box-shadow: -18px 0 38px -24px rgb(15 23 42 / 52%);
}
.asset-manager-header {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px;
  border-bottom: 1px solid var(--uvp-panel-border);
}
.asset-manager-header > div {
  display: grid;
  gap: 2px;
}
.asset-manager-header span {
  font-size: 9.5px;
  color: var(--uvp-text-tertiary);
}
.asset-manager-header strong {
  font-size: 14px;
  color: var(--uvp-text-primary);
}
.asset-manager-header > button {
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: transparent;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 7px;
}
.asset-manager-header > button:hover {
  color: var(--uvp-text-primary);
  background: var(--uvp-list-toolbar-bg);
}
.asset-manager-tabs {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 4px;
  padding: 4px;
  margin: 12px 16px 0;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.asset-manager-tabs button {
  display: grid;
  grid-template-columns: auto 1fr auto;
  gap: 6px;
  align-items: center;
  padding: 7px 9px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  text-align: left;
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 6px;
}
.asset-manager-tabs button.active {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
}
.asset-manager-tabs em {
  min-width: 20px;
  padding: 1px 5px;
  font-size: 9px;
  font-style: normal;
  color: inherit;
  text-align: center;
  background: color-mix(in srgb, currentcolor 8%, transparent);
  border-radius: 4px;
}
.asset-manager-search {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 8px;
  align-items: center;
  height: 34px;
  padding: 0 10px;
  margin: 10px 16px 0;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 7px;
}
.asset-manager-search:focus-within {
  color: var(--uvp-brand);
  border-color: var(--uvp-brand);
}
.asset-manager-search input,
.asset-manager-create input {
  min-width: 0;
  font-size: 11px;
  color: var(--uvp-text-primary);
  outline: none;
  background: transparent;
  border: 0;
}
.asset-manager-search input::placeholder {
  color: var(--uvp-text-tertiary);
}

/* 抽屉内「保存当前位置」按钮,弹弹窗提交 */
.asset-manager-inline-actions {
  display: flex;
  justify-content: flex-end;
  margin: 10px 16px 0;
}
.asset-manager-add {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  padding: 5px 12px;
  font-size: 11px;
  color: #ffffff;
  cursor: pointer;
  background: var(--uvp-brand);
  border: 0;
  border-radius: 6px;
  transition: background 0.12s ease;
}
.asset-manager-add:hover:not(:disabled) {
  background: color-mix(in srgb, var(--uvp-brand) 90%, #000000);
}
.asset-manager-add:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

/* 保存预置位对话框 */
.preset-save-form {
  display: grid;
  gap: 14px;
  padding: 4px 2px 0;
}
.preset-save-row {
  display: grid;
  grid-template-columns: 48px minmax(0, 1fr);
  gap: 12px;
  align-items: start;
}
.preset-save-label {
  padding-top: 6px;
  font-size: 12px;
  color: var(--uvp-text-secondary);
}
.preset-save-index {
  display: inline-flex;
  align-items: center;
  padding: 4px 10px;
  font-family: ui-monospace, Menlo, monospace;
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-radius: 5px;
}
.preset-save-field {
  display: grid;
  gap: 4px;
}
.preset-save-count {
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.preset-save-count.ok {
  font-weight: 600;
  color: #059669;
}
.preset-save-hint {
  margin: 0;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.preset-save-error {
  margin: 0;
  font-size: 11px;
  color: var(--uvp-danger);
}

/* 新建巡航轨迹对话框 */
.cruise-save-form {
  display: grid;
  gap: 14px;
  max-height: calc(100dvh - 180px);
  padding: 4px 6px 2px 2px;
  overflow-y: auto;
  scrollbar-gutter: stable;
  overscroll-behavior: contain;
}
.cruise-save-notice {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  padding: 9px 10px;
  font-size: 11.5px;
  line-height: 1.5;
  color: var(--uvp-text-secondary);
  background: var(--uvp-warning-soft);
  border: 1px solid var(--uvp-warning-border);
  border-radius: 6px;
}
.cruise-save-notice > svg {
  flex: 0 0 auto;
  margin-top: 1px;
  color: var(--uvp-warning);
}
.cruise-save-row {
  display: grid;
  grid-template-columns: 66px minmax(0, 1fr);
  gap: 12px;
  align-items: start;
}
.cruise-save-label {
  padding-top: 6px;
  font-size: 12px;
  color: var(--uvp-text-secondary);
}
.cruise-save-field {
  display: grid;
  gap: 4px;
  min-width: 0;
}
.cruise-param-line {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  min-height: 32px;
}
.cruise-param-mode {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  margin-left: auto;
  font-size: 11.5px;
  color: var(--uvp-text-secondary);
  white-space: nowrap;
}
.cruise-save-field > .preset-save-hint {
  line-height: 1.45;
}
.cruise-save-unit {
  font-size: 11.5px;
  color: var(--uvp-text-secondary);
}
.cruise-save-replace {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  font-size: 11.5px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
}
.cruise-save-replace input {
  accent-color: var(--uvp-brand-cyan);
}
.cruise-stops-list {
  display: grid;
  gap: 5px;
  max-height: clamp(168px, 30vh, 260px);
  padding-right: 3px;
  overflow-y: auto;
  scrollbar-gutter: stable;
  overscroll-behavior: contain;
}
.cruise-stop-row {
  display: flex;
  gap: 6px;
  align-items: center;
  padding: 4px 6px;
  background: color-mix(in srgb, var(--uvp-brand-cyan) 3%, var(--uvp-panel-bg));
  border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 16%, var(--uvp-panel-border));
  border-radius: 6px;
}
.cruise-stop-idx {
  display: inline-grid;
  place-items: center;
  width: 22px;
  height: 22px;
  font-family: ui-monospace, Menlo, monospace;
  font-size: 11px;
  font-weight: 600;
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent);
  border-radius: 50%;
}
.cruise-stop-move,
.cruise-stop-del {
  display: inline-grid;
  place-items: center;
  width: 24px;
  height: 24px;
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 5px;
  transition:
    color 0.12s ease,
    background 0.12s ease,
    border-color 0.12s ease;
}
.cruise-stop-move:hover:not(:disabled) {
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent);
}
.cruise-stop-del:hover:not(:disabled) {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
}
.cruise-stop-move:disabled,
.cruise-stop-del:disabled {
  cursor: not-allowed;
  opacity: 0.35;
}
.cruise-stop-add {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  justify-content: center;
  width: 100%;
  min-height: 44px;
  padding: 8px 12px;
  margin-top: 3px;
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-brand);
  cursor: pointer;
  background: var(--uvp-brand-soft);
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 44%, var(--uvp-panel-border));
  border-radius: 6px;
  transition:
    background 0.18s ease,
    border-color 0.18s ease,
    color 0.18s ease;
}
.cruise-stop-add:hover:not(:disabled) {
  color: #ffffff;
  background: var(--uvp-brand);
  border-color: var(--uvp-brand);
}
.cruise-stop-add:focus-visible {
  outline: 2px solid color-mix(in srgb, var(--uvp-brand) 52%, transparent);
  outline-offset: 2px;
}
.cruise-stop-add:disabled {
  cursor: not-allowed;
  opacity: 0.65;
}
.cruise-stop-add-count {
  margin-left: auto;
  font-size: 10px;
  font-weight: 400;
  color: currentColor;
  opacity: 0.72;
}
.cruise-save-advanced {
  padding-top: 8px;
  border-top: 1px solid var(--uvp-panel-border);
}
.cruise-save-advanced > summary {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  font-size: 11.5px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
  cursor: pointer;
}
.cruise-save-advanced > summary::marker {
  display: none;
}
.cruise-save-advanced-body {
  display: grid;
  gap: 12px;
  padding-top: 12px;
}

@media (width <= 560px) {
  .cruise-save-form {
    max-height: calc(100dvh - 210px);
  }
  .cruise-save-row {
    grid-template-columns: minmax(0, 1fr);
    gap: 5px;
  }
  .cruise-save-label {
    padding-top: 0;
  }
}
.asset-manager-list {
  min-height: 0;
  padding: 0 16px;
  margin-top: 10px;
  overflow-y: auto;
  scrollbar-color: color-mix(in srgb, var(--uvp-text-tertiary) 28%, transparent) transparent;
  scrollbar-width: thin;
}
.asset-manager-row {
  display: grid;
  grid-template-columns: 34px minmax(0, 1fr) auto;
  gap: 8px;
  align-items: center;
  min-height: 48px;
  padding: 7px 0;
  border-bottom: 1px solid var(--uvp-panel-border);
}
.asset-manager-row.active {
  background: color-mix(in srgb, var(--uvp-brand) 5%, transparent);
}
.asset-manager-row.disabled {
  opacity: 0.56;
}
.asset-manager-index {
  font-family: ui-monospace, Menlo, monospace;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}
.asset-manager-info {
  display: grid;
  gap: 2px;
  min-width: 0;
}
.asset-manager-info strong {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 11.5px;
  font-weight: 550;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.asset-manager-info small {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 9.5px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}
.asset-manager-actions {
  display: flex;
  gap: 4px;
  align-items: center;
}
.asset-manager-delete {
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 5px;
}
.asset-manager-delete:hover {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
}
.asset-manager-empty {
  padding: 40px 12px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  text-align: center;
}
.asset-manager-empty.preset-empty-large {
  padding: 44px 16px 28px;
}
.preset-empty-glyph {
  display: block;
  margin: 0 auto 6px;
  color: var(--uvp-text-tertiary);
  opacity: 0.55;
}
.preset-empty-line-primary {
  margin: 0;
  font-size: 12px;
  color: var(--uvp-text-tertiary);
}
.asset-manager-footer {
  display: flex;
  gap: 10px;
  align-items: center;
  justify-content: space-between;
  min-height: 42px;
  padding: 8px 16px;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-list-toolbar-bg);
  border-top: 1px solid var(--uvp-panel-border);
}
.asset-drawer-enter-active,
.asset-drawer-leave-active {
  transition: opacity 0.18s ease;
}
.asset-drawer-enter-active .asset-manager-drawer,
.asset-drawer-leave-active .asset-manager-drawer {
  transition: transform 0.18s ease;
}
.asset-drawer-enter-from,
.asset-drawer-leave-to {
  opacity: 0;
}
.asset-drawer-enter-from .asset-manager-drawer,
.asset-drawer-leave-to .asset-manager-drawer {
  transform: translateX(100%);
}

/* 主区(视频):stage 仅保留语义,子项直接参与外层网格 */
.stage {
  display: contents;
}
.stage.stage-wide .video-frame {
  grid-column: 1 / -1;
}

.video-frame {
  position: relative;
  grid-row: 1;
  grid-column: 1;
  align-self: start;
  overflow: hidden;
  background: #060b14;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 14px;
  box-shadow: 0 12px 32px -18px rgb(0 0 0 / 60%);
}
.video-canvas {
  position: relative;
  display: grid;
  place-items: center;
  width: 100%;
  aspect-ratio: 16 / 9;
  background: radial-gradient(circle at 50% 45%, rgb(30 41 59 / 40%) 0%, rgb(2 6 23 / 96%) 72%), #020617;
}
.video-canvas :deep(.play-window) {
  width: 100%;
  height: 100%;
  min-height: 100%;
  aspect-ratio: auto;
  border: 0;
  border-radius: 0;
}
.ptz-direction-indicator {
  --ptz-direction-rotation: 0deg;

  position: absolute;
  top: 50%;
  left: 50%;
  z-index: 5;
  display: grid;
  place-items: center;
  width: clamp(72px, 12%, 104px);
  aspect-ratio: 1;
  pointer-events: none;
  transform: translate(-50%, -50%) rotate(var(--ptz-direction-rotation));
}
.ptz-direction-stack {
  position: relative;
  display: block;
  width: 100%;
  height: 100%;
  animation: ptz-direction-flow 0.95s ease-in-out infinite;
  will-change: opacity, transform;
}
.ptz-direction-chevron {
  position: absolute;
  left: 50%;
  display: block;
  width: 76%;
  height: 34%;
  background: rgb(96 165 250 / 82%);
  clip-path: polygon(0 68%, 50% 0, 100% 68%, 80% 100%, 50% 58%, 20% 100%);
  transform: translateX(-50%);
}
.ptz-direction-chevron.front {
  top: 4%;
  filter: drop-shadow(0 2px 8px rgb(15 23 42 / 42%)) drop-shadow(0 0 9px rgb(59 130 246 / 34%));
}
.ptz-direction-chevron.middle {
  top: 32%;
  width: 66%;
  background: rgb(147 197 253 / 48%);
}
.ptz-direction-chevron.back {
  top: 58%;
  width: 56%;
  background: rgb(191 219 254 / 22%);
}
.ptz-direction-indicator[data-direction="右上"] {
  --ptz-direction-rotation: 45deg;
}
.ptz-direction-indicator[data-direction="右"] {
  --ptz-direction-rotation: 90deg;
}
.ptz-direction-indicator[data-direction="右下"] {
  --ptz-direction-rotation: 135deg;
}
.ptz-direction-indicator[data-direction="下"] {
  --ptz-direction-rotation: 180deg;
}
.ptz-direction-indicator[data-direction="左下"] {
  --ptz-direction-rotation: 225deg;
}
.ptz-direction-indicator[data-direction="左"] {
  --ptz-direction-rotation: 270deg;
}
.ptz-direction-indicator[data-direction="左上"] {
  --ptz-direction-rotation: 315deg;
}

@keyframes ptz-direction-flow {
  0%,
  100% {
    opacity: 0.48;
    transform: translateY(5px) scale(0.94);
  }
  50% {
    opacity: 1;
    transform: translateY(-4px) scale(1);
  }
}
.drag-zoom-layer {
  position: absolute;
  inset: 0;
  z-index: 6;
  touch-action: none;
  cursor: crosshair;
}

/* ⛔ 色罩只在**按着拖**时上：拉框态现在会常驻（下完一刀不退），常驻色罩会把刚下发的画面
   整体染蓝，而操作员下一步就是判读放大结果 —— 那正是被这层色罩干扰的地方。 */
.drag-zoom-layer.is-dragging {
  background: rgb(8 47 73 / 12%);
}

/* 上一次框选还在下发：此时 `beginDragZoom` 会拒收新的按下，游标必须跟着说"等一下"。 */
.drag-zoom-layer.is-busy {
  cursor: progress;
}
.drag-zoom-box {
  position: absolute;
  box-sizing: border-box;
  display: block;
  background: rgb(34 211 238 / 14%);
  border: 1px solid var(--uvp-brand-cyan);
  box-shadow: 0 0 0 1px rgb(34 211 238 / 22%);
}
.drag-zoom-hint {
  position: absolute;
  top: 10px;
  left: 50%;
  padding: 4px 8px;
  font-size: 10px;
  color: var(--uvp-brand-cyan);
  background: rgb(2 6 23 / 72%);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 4px;
  transform: translateX(-50%);
}

/* 目标跟踪框选层：与拉框变焦同构，但**配色刻意不同**（琥珀而非青）。
 * ⛔ 它俩虽然都是"在画面上拖"，语义完全不同：拉框变焦改的是**镜头**（可逆、立即看得见效果），
 *    目标跟踪是**让设备去跟一个目标**（无回执、画面上不会有任何反馈）。
 *    同一个颜色会让人以为刚才那一刀也是变焦 —— 而跟踪下完之后画面是**不会有变化**的。
 * ⛔ 色罩同样只挂在 .is-dragging 上：跟踪下完**不会**在画面上留下任何东西，
 *    常驻色罩会被读成"框还在"，而其实平台只是发了一条指令。 */
.target-track-layer {
  position: absolute;
  inset: 0;
  z-index: 6;
  touch-action: none;
  cursor: crosshair;
}
.target-track-layer.is-dragging {
  background: rgb(120 53 15 / 14%);
}
.target-track-layer.is-busy {
  cursor: progress;
}
.target-track-box {
  position: absolute;
  box-sizing: border-box;
  display: block;
  background: rgb(251 191 36 / 12%);
  border: 1px dashed var(--uvp-warning);
  box-shadow: 0 0 0 1px rgb(251 191 36 / 20%);
}
.target-track-hint {
  position: absolute;
  top: 10px;
  left: 50%;
  padding: 4px 8px;
  font-size: 10px;
  color: var(--uvp-warning);
  background: rgb(2 6 23 / 72%);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 4px;
  transform: translateX(-50%);
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
}

.paused-mask {
  position: absolute;
  inset: 0;
  display: grid;
  gap: 10px;
  place-items: center;
  align-content: center;
  color: #dbeafe;
  background: rgb(2 6 23 / 68%);
  backdrop-filter: blur(4px);
}
.paused-mask span {
  font-size: 13px;
  letter-spacing: 0.04em;
}

.placeholder {
  display: grid;
  gap: 10px;
  place-items: center;
  align-content: center;
  padding: 30px;
  color: var(--uvp-text-tertiary);
  text-align: center;
}
.placeholder strong {
  font-size: 14px;
  color: #dbeafe;
}
.placeholder span {
  max-width: 420px;
  font-size: 12px;
  line-height: 1.55;
  color: #94a3b8;
}
.placeholder.error strong {
  color: #fecaca;
}
.placeholder.error {
  color: #fca5a5;
}
.pulse {
  display: grid;
  place-items: center;
  width: 76px;
  height: 76px;
  color: var(--uvp-brand);
  background: radial-gradient(circle at 50% 50%, rgb(96 165 250 / 22%) 0%, transparent 65%);
  border-radius: 50%;
}
.pulse.idle {
  color: rgb(148 163 184 / 62%);
  background: radial-gradient(circle at 50% 50%, rgb(148 163 184 / 12%) 0%, transparent 65%);
}
.spin {
  animation: spin 1.1s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

/* 播放器控制条(悬浮画面下)的「重试」按钮:在通用按钮基础上多加 4px 上边距。
 *
 * ⛔ 选择器**必须**带 `.player-retry` 这一层 class,不能只写 `.btn-primary`。
 *    这里原来是一份完整的 `.btn-primary` 定义(display/gap/padding/radius/font… +
 *    `margin-top: 4px`),但它在源序上排在下方「通用按钮」区**之前**,两者特异性
 *    同为 0,1,0 —— 于是除 `margin-top` 以外的声明全被后者覆盖掉,实际只有
 *    `margin-top: 4px` 生效,并且泄漏到了**页面上每一个** `.btn-primary`
 *    (后面那两次重定义都没声明 margin,等于没覆盖)。看守位卡片的「保存」按钮
 *    因此被往下顶 4px:它在 32dp 行里顶到行首、底部多出 4px,和同一行的 28dp
 *    输入框不再居中对齐。2026-09-17 由用户截图上报,复现页实测:
 *    泄漏时按钮顶 - 输入框顶 = 0、底 - 底 = +4;收窄后上差 -2、下差 +2(居中)。
 *
 * 既然原本生效的只有那一条,这里就只保留它 —— 其余交给下方「通用按钮」区的
 * 唯一真源,既不泄漏,也不改变「重试」按钮自身的观感(它此前用的就是通用那套值)。 */
.btn-primary.player-retry {
  margin-top: 4px;
}

/* 多协议切换器 */

/* 底部两角要自己写,不能只靠父级 .video-frame 的 border-radius + overflow: hidden:
 * 本元素带 backdrop-filter,会自建 backdrop root,它绘制的背景在 Chromium/WebKit 下
 * 会逃出祖先的圆角裁剪 —— 表现就是画面顶部是圆角、这条栏底部却是直角。
 * 13px = 父级 14px 圆角减去 1px 边框,和外框严丝合缝。 */
.protocol-switcher {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  padding: 10px 14px;
  background: rgb(15 23 42 / 92%);
  border-top: 1px solid rgb(255 255 255 / 8%);
  border-bottom-right-radius: 13px;
  border-bottom-left-radius: 13px;
  backdrop-filter: blur(12px);
}
.switcher-left {
  display: flex;
  gap: 10px;
  align-items: center;
}
.switcher-left .kicker {
  font-size: 11px;
  color: rgb(203 213 225 / 72%);
  letter-spacing: 0.03em;
  white-space: nowrap;
}
.switcher-right {
  display: flex;
  gap: 6px;
}
.proto-btn {
  padding: 5px 12px;
  font-size: 11px;
  font-weight: 500;
  color: rgb(219 234 254 / 68%);
  cursor: pointer;
  background: rgb(255 255 255 / 4%);
  border: 1px solid rgb(255 255 255 / 8%);
  border-radius: 6px;
  transition: all 0.15s ease;
}
.proto-btn:hover {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-color: color-mix(in srgb, var(--uvp-brand) 32%, transparent);
}
.proto-btn.active {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-color: color-mix(in srgb, var(--uvp-brand) 42%, transparent);
}

/* 流信息(协议切换器内) */
.stream-info {
  display: flex;
  gap: 8px;
  align-items: center;
  padding-left: 16px;
  margin-left: auto;
  font-size: 11px;
  border-left: 1px solid rgb(255 255 255 / 8%);
}

.protocol-option {
  box-sizing: border-box;
  display: grid;
  grid-template-columns: max-content minmax(0, 1fr) 28px;
  gap: 8px;
  align-items: center;
  width: min(600px, calc(100vw - 80px));
  padding: 2px 0;
}
.protocol-option strong {
  min-width: 0;
  font-size: 11px;
  font-weight: 500;
  line-height: 1.4;
}
.protocol-url {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-family: var(--uvp-font-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  font-size: 11px;
  line-height: 1.4;
  color: var(--uvp-text-secondary);
  white-space: nowrap;
}
.protocol-copy-btn {
  position: relative;
  z-index: 1;
  display: inline-grid;
  place-items: center;
  width: 28px;
  height: 28px;
  padding: 0;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 5px;
  transition:
    color 0.15s ease,
    background 0.15s ease;
}
.protocol-copy-btn:hover {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
}

/* 播放器下方的运行信息面板 */
.stream-info-bar {
  display: grid;
  gap: 14px;
  padding: 14px 16px 16px;
  font-size: 11px;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
  box-shadow: 0 8px 24px -18px rgb(0 0 0 / 45%);
}

/* 双区联动版:随 Tab 切换的全宽等高详情 —— 干掉外层白面板,4 张卡片直接躺在 tab 里 */
.linked-info-bar {
  --linked-detail-height: 148px;

  grid-row: 2;
  grid-column: 1 / -1;
  gap: 0;
  padding: 0;
  overflow: hidden;
  background: transparent;
  border: 0;
  border-radius: 0;
  box-shadow: none;
}
.linked-detail {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  gap: 6px;
  height: var(--linked-detail-height);
  padding: 0;
  overflow: hidden;
}

/* 视频参数与设备配置的底部是第二操作区，不是只读详情栏。 */
.linked-detail-actions {
  /* 与云台、视频探针共用同一条底部工作区基线，避免切换页签时布局跳高。 */
  height: var(--linked-detail-height);
}

.linked-detail-actions .linked-deviceconfig-actions {
  height: 100%;
}

.linked-detail-actions .linked-card {
  min-height: 0;
}

.linked-inline-select {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}

.linked-inline-select select {
  height: 24px;
  padding: 0 5px;
  font-size: 10px;
  color: var(--uvp-text-secondary);
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 4px;
}

.linked-deviceconfig-actions {
  display: grid;
  min-height: 0;
}

.deviceconfig-action-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
  align-content: start;
  min-height: 0;
}

.deviceconfig-action {
  display: grid;
  gap: 5px;
  min-width: 0;
  padding: 10px;
  color: var(--uvp-text-secondary);
  text-align: left;
  cursor: pointer;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 6px;
}

.deviceconfig-action:hover {
  color: var(--uvp-brand);
  border-color: var(--uvp-brand);
}

.deviceconfig-action span {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 11px;
  font-weight: 600;
  white-space: nowrap;
}

.deviceconfig-action small {
  font-size: 9px;
  color: var(--uvp-text-tertiary);
}

.deviceconfig-action[data-state="ready"] small {
  color: var(--uvp-success, #059669);
}

.deviceconfig-action-note {
  margin: auto 0 0;
  font-size: 10px;
  line-height: 1.5;
  color: var(--uvp-text-tertiary);
}
.linked-detail > .linked-ptz-layout,
.linked-detail > .linked-probe-layout,
.linked-detail > .linked-image-layout {
  flex: 1 1 0;
  min-height: 0;
}
.linked-detail-hint {
  padding: 0;
  margin: 0 0 8px;
  font-size: 10.5px;
  line-height: 1.4;
  color: var(--uvp-text-tertiary);
}

/* 列数必须跟实际渲染的卡片数一致(预置位 / 巡航轨迹 / 看守位 / 自动扫描 = 4 张)。
 * 之前写的是 4 列而只有 3 张卡,多出来的那一列空着,卡片只占满 3/4 宽度,右侧留一条空白;
 * 现在补上「自动扫描」第 4 张卡,列数才对得上。 */
.linked-ptz-layout {
  box-sizing: border-box;
  display: grid;
  grid-template-rows: minmax(0, 1fr);
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 10px;
  align-items: stretch;
  height: 100%;
  min-height: 0;
}

/* ── 画面设置底栏卡片（图像叠加 / 遮挡 / 镜像 / 参数对照）──
 * ⭐ 2026-09-20 起**四格**：图像叠加整块从侧栏搬来（最宽），遮挡/镜像按老板要求缩窄，
 *    参数对照压到只放两行。与云台底栏共用同一条高度基线（148px），切页签画布不重排。
 * ⛔ 底栏可用宽**已变成弹窗内容全宽**（2026-09-21 页签回到属性栏顶部、左侧 136px 那列退役；
 *    此前口径是「约 940px，栅格 `136px | 1fr | 360px`，不是弹窗全宽」，比现在窄 136px+）。
 *    按 4.65fr 分四格。⚠️ 宽度变大只让每格更宽松，但**别因此塞第五张卡** ——
 *    窄屏（≤1080px 单列）时底栏仍会回到全宽下的紧凑形态，照样会把坐标压到省略号截断。 */
.linked-picture-layout {
  box-sizing: border-box;
  display: grid;
  grid-template-rows: minmax(0, 1fr);
  grid-template-columns: minmax(0, 1.9fr) minmax(0, 1fr) minmax(0, 0.7fr) minmax(0, 1.05fr);
  gap: 10px;
  align-items: stretch;
  height: 100%;
  min-height: 0;
}

/* 图像叠加这一格不是"卡片"而是"两块面板的容器"（时间戳 + 叠加文字并排），
 * 所以不复用 `.linked-card` 的实线框，只保证高度链完整。 */
.picture-osd-cell {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  overflow: hidden;
}

.linked-card-note {
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}

/* 「启用遮挡」总闸：标准里 `On` 与区域列表是两块数据，总闸为 0 时画了也不生效。 */
.mask-switch {
  height: 20px;
  padding: 0 7px;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
}

.mask-switch.on {
  color: #059669;
  background: rgb(5 150 105 / 12%);
  border-color: rgb(5 150 105 / 40%);
}

/* 草稿与设备事实不一致 ⇒ 这次改动还没下发。用琥珀色盖掉 `.on` 的绿：
   绿色在这里会被读成"设备上就是这个状态"，而实际上一个字节都还没发出去。
   ⛔ 必须写在 `.mask-switch.on` **之后**（同优先级，靠顺序覆盖）。 */
.mask-switch.pending {
  color: var(--uvp-warning, #b66b12);
  background: color-mix(in srgb, var(--uvp-warning, #b66b12) 14%, transparent);
  border-color: color-mix(in srgb, var(--uvp-warning, #b66b12) 46%, var(--uvp-panel-border));
}

.mask-switch:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

/* 遮挡区槽位：标准固定 4 个。2026-09-20 起改**单列、一行一个**（原来是 2×2）——
 * 底栏把这一格的宽度收到约 195px（腾给图像叠加），2×2 时每格只剩 ~90px，
 * 而槽位里那串坐标（`0,0,704,576`）是这张卡唯一的信息：`.mask-slot-coords` 会被省略号
 * 截断 ⇒ 卡片等于白放。单列后每行约 180px，坐标读得全；4 行 × 约 24px 仍装得进 148px。 */
.mask-slot-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  grid-auto-rows: minmax(0, 1fr);
  gap: 4px;
  min-height: 0;
  overflow-y: auto;
}

.mask-slot {
  position: relative;
  display: flex;
  gap: 5px;
  align-items: center;
  min-width: 0;
  padding: 4px 6px;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-list-toolbar-bg);
  border: 1px dashed var(--uvp-panel-border);
  border-radius: 6px;
}

.mask-slot.used {
  color: var(--uvp-text-secondary);
  background: color-mix(in srgb, var(--uvp-brand) 6%, var(--uvp-panel-bg));
  border-color: color-mix(in srgb, var(--uvp-brand) 32%, var(--uvp-panel-border));
  border-style: solid;
}

.mask-slot-idx {
  flex: none;
  font-weight: 600;
  color: var(--uvp-brand);
}

.mask-slot-coords {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 10px;
  white-space: nowrap;
}

.mask-slot-blank {
  flex: 1 1 auto;
  color: var(--uvp-text-tertiary);
}

.mask-slot-del {
  display: grid;
  flex: none;
  place-items: center;
  width: 16px;
  height: 16px;
  padding: 0;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 4px;
}

.mask-slot-del:hover {
  color: #d03050;
  background: rgb(208 48 80 / 12%);
}

/* 遮挡坐标的基准（设备声明的图像尺寸）。
 * ⛔ 这是"画上去会不会准"的唯一凭据，必须常驻在卡片上：放在 title 悬浮提示里，
 *    用户是在"发现落点不对"之后才会去找它，已经晚了。 */
.mask-canvas-note {
  margin: 6px 0 0;
  font-size: 10px;
  line-height: 1.5;
  color: var(--uvp-text-tertiary);
}

/* 基准没拿到设备声明时（退回画面尺寸）：琥珀色 —— 与"将启用/将停用"同一套语义，
 * 都表示"这一步存在偏差、且平台已知"。 */
.mask-canvas-note.is-unverified {
  color: #fbbf24;
}

/* 镜像方向：图标 + 文字副标题，四个并列 */
.mirror-choice-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px;
  align-content: start;
  min-height: 0;
}

.mirror-choice {
  display: flex;
  gap: 4px;
  align-items: center;
  justify-content: center;
  min-width: 0;

  /* ⭐ 2026-09-20：这一格收窄到约 137px（老板要求"宽度可以缩小一些"），
   * 2×2 时每格只剩约 55px —— 图标 16 + 2 字标签 21 已经把格子占满，
   * 空档与内边距必须一起收，否则标签被挤出格（`原图 / 左右 / 上下 / 中心`）。 */
  padding: 6px 4px;
  font-size: 10.5px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 6px;
}

.mirror-choice:hover:not(:disabled) {
  color: var(--uvp-brand);
  border-color: color-mix(in srgb, var(--uvp-brand) 45%, var(--uvp-panel-border));
}

.mirror-choice.active {
  color: var(--uvp-brand);
  background: color-mix(in srgb, var(--uvp-brand) 10%, var(--uvp-panel-bg));
  border-color: var(--uvp-brand);
}

.mirror-choice:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

/* ── 下发浮条（取代原底栏「提交」卡片，2026-09-19）──
 * 贴在画面底部**居中**：用户刚在画面上拖完框，视线还在画面里，下发入口就该落在视线
 * 落点上，而不是另一个角落 —— "画完直接走开、忘了下发"就是这么发生的。
 * ⛔ 居中而不是右对齐：右下角是 `PtzThumbnail` 缩略图的地盘，右对齐会压在它上面。 */
.picture-draft-bar {
  position: absolute;
  bottom: 14px;
  left: 50%;
  z-index: 6;
  display: flex;
  flex-direction: column;
  gap: 6px;
  align-items: stretch;
  max-width: calc(100% - 24px);
  padding: 6px 8px 6px 12px;
  font-size: 11px;
  color: var(--uvp-text-secondary);
  background: color-mix(in srgb, var(--uvp-panel-bg) 92%, transparent);
  border: 1px solid color-mix(in srgb, var(--uvp-warning, #b66b12) 46%, var(--uvp-panel-border));
  border-radius: 8px;
  box-shadow: 0 6px 18px rgb(0 0 0 / 28%);
  transform: translateX(-50%);
}

/* 拒发原因：整行铺开，别被右侧按钮挤成省略号 —— 这句话本身就是解决办法。 */
.picture-draft-error {
  max-width: 420px;
  margin: 0;
  font-size: 11px;
  line-height: 1.45;
  color: var(--uvp-danger, #d14343);
}

.picture-draft-row {
  display: flex;
  gap: 10px;
  align-items: center;
  min-width: 0;
}

/* 卡片内的同一句话：浮条会随草稿消失，卡片是它的常驻位置。
   底色用 color-mix 兑出来而不是 `--uvp-danger-soft` —— 那个是给浅色侧栏用的，铺在画面上太亮。 */
.picture-card-error {
  padding: 6px 8px;
  margin: 0 0 8px;
  font-size: 11px;
  line-height: 1.45;
  color: var(--uvp-danger, #d14343);
  background: color-mix(in srgb, var(--uvp-danger, #d14343) 14%, transparent);
  border-radius: 6px;
}

/* 与上行成对：错误是"没发出去"，提示是"发出去了但结果和你想的不一样"。
   ⛔ 不能沿用错误色 —— 那会让用户以为失败了，而这次设备确实收到了。 */
.picture-card-notice {
  padding: 6px 8px;
  margin: 0 0 8px;
  font-size: 11px;
  line-height: 1.45;
  color: var(--uvp-warning, #b66b12);
  background: color-mix(in srgb, var(--uvp-warning, #b66b12) 14%, transparent);
  border-radius: 6px;
}

.picture-draft-text {
  display: inline-flex;
  gap: 4px;
  align-items: baseline;
  min-width: 0;
}

.picture-draft-text strong {
  font-size: 13px;
  color: var(--uvp-warning, #b66b12);
}

.picture-draft-summary {
  overflow: hidden;
  text-overflow: ellipsis;
  font-style: normal;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}

.picture-draft-summary::before {
  margin-right: 4px;
  content: "·";
}

.picture-draft-actions {
  display: inline-flex;
  flex: none;
  gap: 6px;
}

.picture-draft-revert,
.picture-draft-submit {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  justify-content: center;
  height: 24px;
  padding: 0 10px;
  font-size: 11px;
  cursor: pointer;
  border-radius: 5px;
}

.picture-draft-revert {
  color: var(--uvp-text-secondary);
  background: transparent;
  border: 1px solid var(--uvp-panel-border);
}

.picture-draft-submit {
  color: #ffffff;
  background: var(--uvp-brand);
  border: 1px solid var(--uvp-brand);
}

.picture-draft-revert:disabled,
.picture-draft-submit:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

/* 画面上已有遮挡区的投影：只作参照，不吃指针事件 */
.mask-overlay-layer {
  position: absolute;
  inset: 0;
  z-index: 4;
  pointer-events: none;
}

.mask-overlay-box {
  position: absolute;
  box-sizing: border-box;
  display: block;
  background: rgb(251 191 36 / 12%);
  border: 1px dashed rgb(251 191 36 / 78%);
}

.mask-overlay-tag {
  position: absolute;
  top: 0;
  left: 0;
  padding: 0 4px;
  font-size: 9px;
  font-style: normal;
  line-height: 14px;
  color: #0b1220;
  background: rgb(251 191 36 / 88%);
}

/* 草稿框：本次改过、**设备上还没有**的那几块（2026-09-19）。
 * ⛔ 颜色 + 线型 + 标签文字三处同时区分，不能只换颜色：
 *    色弱用户分不出色相差异，而"这块到底发出去没有"正是这里唯一要说的信息。 */
.mask-overlay-box.is-draft {
  background: color-mix(in srgb, var(--uvp-brand) 22%, transparent);
  border: 1px solid var(--uvp-brand);
}

.mask-overlay-box.is-draft .mask-overlay-tag {
  font-weight: 600;
  color: #ffffff;
  background: var(--uvp-brand);
}

/* ─────────── 图像叠加（OSD）的锚点层（2026-09-20）───────────
 *
 * 形态选「锚点 + 内容标签」而不是"像真字的预览"：标准里没有字体/字号/颜色，
 * 画得像真字就是在承诺平台给不了的能力；而且设备已烧进码流的那个时间戳就在画面里，
 * 再叠一个假字会出现**两个时间戳**。
 * ⭐ 标签刻意偏到坐标点**右上 8px**（引线连过去），不压住真字的第一个字 ——
 *    这样用户能对着真字拖锚点对齐，等于免费的精确校准。
 */
.osd-overlay-layer {
  position: absolute;
  inset: 0;
  z-index: 5;

  /* ⛔ 层本身不吃指针：它盖在整个画面上，吃掉点击会让原有操作（点画面、拖变焦）失灵。
     只有圆点和标签两个小目标可点。
     ⛔ 整层现在**只在编辑模式存在**（模板里的 `v-if="osdEditMode"`），所以不再需要
     "非编辑态把子元素的 pointer-events 关掉"那组规则 —— 只读态的锚点已经不存在了。 */
  pointer-events: none;
}

/* ⛔ 「调整位置」按钮**不在这个文件里**（2026-09-20 最终落点）：
     画面右上角 → 画面下方工具条 → 侧栏「时间戳」面板的「位置」行。
   它改的就是那一行的坐标，跟坐标读数放在一起才是它本来的位置，
   见 `DeviceConfigOsdBlocks.vue` 的 `.osd-adjust`。 */

.osd-anchor {
  position: absolute;
  width: 0;
  height: 0;
  color: var(--uvp-brand);

  /* 坐标点本身：锚点"钉"在哪儿，一眼可见 */
  .osd-anchor-dot {
    position: absolute;
    top: -5px;
    left: -5px;
    box-sizing: border-box;
    width: 10px;
    height: 10px;
    pointer-events: auto;
    cursor: grab;
    background: var(--uvp-brand);
    border: 1.5px solid #ffffff;
    border-radius: 50%;
    box-shadow: 0 0 0 1px color-mix(in srgb, var(--uvp-brand) 60%, transparent);
  }

  /* 引线：把标签从坐标点斜引到右上，避免盖住设备真实 OSD 的第一个字 */
  .osd-anchor-lead {
    position: absolute;
    top: -4px;
    left: 4px;
    width: 11px;
    height: 1px;
    background: currentcolor;
    opacity: 0.6;
    transform: rotate(-42deg);
    transform-origin: left center;
  }

  .osd-anchor-tag {
    position: absolute;
    top: -23px;
    left: 13px;
    display: inline-flex;
    gap: 4px;
    align-items: center;
    padding: 2px 6px;
    font-size: 10.5px;
    font-weight: 600;
    color: #ffffff;
    white-space: nowrap;
    pointer-events: auto;
    cursor: grab;
    background: var(--uvp-brand);
    border-radius: 4px;
    box-shadow: 0 1px 4px rgb(0 0 0 / 30%);

    em {
      font-style: normal;
      font-weight: 500;
      opacity: 0.9;
    }
  }

  /* 拖动中的实时坐标：`pointermove` 期间不写草稿，但用户必须看到它跟着手指走 */
  .osd-anchor-coords {
    position: absolute;
    top: 3px;
    left: 13px;
    padding: 1px 5px;
    font-family: ui-monospace, Menlo, monospace;
    font-size: 10px;
    color: #ffffff;
    white-space: nowrap;
    pointer-events: none;
    background: rgb(12 22 38 / 82%);
    border-radius: 3px;
  }

  /* 待下发 / 未定位：虚线 + 琥珀。⛔ 状态同时写进标签文字，不只靠颜色 ——
     色弱用户分不出色相差异，而"这一块发出去没有"正是这里唯一要说的信息。 */
  &.is-draft,
  &.is-unplaced {
    color: #b26a00;

    .osd-anchor-dot {
      background: #fff3d6;
      border-color: #e08b00;
      border-style: dashed;
      box-shadow: 0 0 0 1px rgb(224 139 0 / 50%);
    }

    .osd-anchor-tag {
      color: #b26a00;
      background: #fff3d6;
      border: 1px dashed #e08b00;
    }
  }

  /* ⛔ 所属开关已关闭 → **淡显**，不是隐藏：与遮挡侧"设备已停用但保留区域"同构，
     隐藏会让用户以为配置丢了（而它下次启用会一起活过来）。 */
  &.is-off {
    opacity: 0.45;
  }

  &.is-dragging {
    z-index: 6;

    .osd-anchor-dot,
    .osd-anchor-tag {
      cursor: grabbing;
    }
  }

  /* 侧栏点「在画面上定位」→ 闪一下，把视线引过去 */
  &.is-focus .osd-anchor-tag {
    animation: osd-anchor-pulse 1.4s ease-out 1;
  }
}

/* ⛔ 「只读态锚点用中性玻璃」那组规则**已删**（2026-09-20）：那一版是"锚点常驻、只降噪"，
   老板看下来仍然是"默认就显示 + OSD 太乱"。现在只读态**一个锚点都不画**，锚点只在
   编辑模式存在 —— 编辑模式就是"正在摆位置"，此时品牌蓝是**对的**信号（可拖、是操作目标）。
   留下一组永远匹配不到的选择器只会让下一个人以为还存在第二种锚点外观。 */

/* 锚点层的操作说明（这层只在编辑模式存在，所以它也就是"编辑模式的说明"）。
   ⛔ 放**左上角**：左下角会被下发浮条（底部居中、最长铺满整行）压住 ——
      "刚拖完想确认怎么退出，说明却被盖住"是最不需要它出现的时刻。 */
.osd-layer-hint {
  position: absolute;
  top: 10px;
  left: 10px;
  padding: 4px 8px;
  font-size: 11px;
  color: #dbe6f5;
  pointer-events: none;
  background: rgb(12 22 38 / 72%);
  border-radius: 6px;
}

@keyframes osd-anchor-pulse {
  0% {
    box-shadow: 0 0 0 0 color-mix(in srgb, var(--uvp-brand) 55%, transparent);
    transform: scale(1);
  }

  60% {
    box-shadow: 0 0 0 8px rgb(22 93 255 / 0%);
    transform: scale(1.18);
  }

  100% {
    box-shadow: 0 0 0 0 rgb(22 93 255 / 0%);
    transform: scale(1);
  }
}

/* 新建遮挡区的框选层：与拖拽变焦同一套手势形态 */
.mask-draw-layer {
  position: absolute;
  inset: 0;
  z-index: 7;
  touch-action: none;
  cursor: crosshair;
  background: rgb(8 47 73 / 12%);
}

.mask-draw-box {
  position: absolute;
  box-sizing: border-box;
  display: block;
  background: rgb(251 191 36 / 18%);
  border: 1px solid rgb(251 191 36 / 92%);
  box-shadow: 0 0 0 1px rgb(251 191 36 / 26%);
}

.mask-draw-hint {
  position: absolute;
  top: 10px;
  left: 50%;
  padding: 4px 8px;
  font-size: 10px;
  color: #fbbf24;
  background: rgb(2 6 23 / 72%);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 4px;
  transform: translateX(-50%);
}
.linked-section {
  min-width: 0;
  padding: 0;
}
.linked-section .section-hd.compact {
  margin-top: 12px;
}

/* 3 张 PTZ 卡片统一容器:实线淡蓝框 + 微蓝底,header 定高 + 主体 flex-1 填充,主体 overflow: hidden 保护 */
.linked-card {
  box-sizing: border-box;
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
  height: 100%;
  min-height: 0;
  padding: 8px 10px 10px;
  overflow: hidden;
  background: color-mix(in srgb, var(--uvp-brand) 3%, var(--uvp-panel-bg));
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 22%, var(--uvp-panel-border));
  border-radius: 8px;
}
.linked-card > .linked-card-hd,
.linked-card > .section-hd {
  flex: 0 0 auto;
  margin: 0;
}
.linked-card > .section-hd.first {
  margin-top: 0;
}
.linked-section .preset-grid {
  display: grid;
  flex: 1 1 auto;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  grid-auto-rows: min-content;
  gap: 5px;
  align-content: start;
  min-height: 0;
  overflow: hidden;
}
.linked-section .preset-add {
  grid-column: 1 / -1;
}
.preset-go {
  display: inline-grid;
  place-items: center;
  color: var(--uvp-text-tertiary);
}

/* 紧凑胶囊 tile:名字省略 + 右侧红色 X 删除 */
.preset-tile {
  box-sizing: border-box;
  display: inline-flex;
  align-items: stretch;
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 5px;
  transition: border-color 0.12s ease;
}
.preset-tile:hover:not(.disabled) {
  border-color: color-mix(in srgb, var(--uvp-brand) 40%, var(--uvp-panel-border));
}
.preset-tile.active {
  background: var(--uvp-brand-soft);
  border-color: color-mix(in srgb, var(--uvp-brand) 45%, var(--uvp-panel-border));
}
.preset-tile.disabled {
  opacity: 0.5;
}
.preset-tile .preset-tile-hit,
.preset-tile > .preset-tile-hit.preset-item,
.preset-tile > .preset-tile-hit.cruise-item {
  display: inline-flex;
  flex: 1 1 auto;
  grid-template-columns: unset;
  gap: 4px;
  align-items: center;
  min-width: 0;
  max-width: none;
  padding: 3px 6px;
  font-size: 10.5px;
  line-height: 1.4;
  color: var(--uvp-text-primary);
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 0;
  transition: none;
}
.preset-tile > .preset-tile-hit.preset-item:hover:not(:disabled),
.preset-tile > .preset-tile-hit.cruise-item:hover:not(:disabled) {
  background: transparent;
  border-color: transparent;
}
.preset-tile > .preset-tile-hit.preset-item:disabled,
.preset-tile > .preset-tile-hit.cruise-item:disabled {
  cursor: not-allowed;
  opacity: 1;
}
.preset-tile.active .preset-tile-hit {
  color: var(--uvp-brand);
}
.preset-tile .preset-idx {
  flex-shrink: 0;
  font-family: ui-monospace, Menlo, monospace;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}
.preset-tile.active .preset-idx {
  color: var(--uvp-brand);
}
.preset-tile .preset-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.preset-tile-del {
  display: inline-grid;
  place-items: center;
  width: 20px;
  padding: 0;
  color: color-mix(in srgb, var(--uvp-danger) 65%, var(--uvp-text-tertiary));
  cursor: pointer;
  background: transparent;
  border: 0;
  border-left: 1px solid var(--uvp-panel-border);
  transition:
    color 0.12s ease,
    background 0.12s ease;
}
.preset-tile-del:hover:not(:disabled) {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
}
.preset-tile-del:disabled {
  cursor: not-allowed;
  opacity: 0.4;
}

/* 「更多」chip:作为 preset-grid 的最后一个 cell,只在预置位溢出(> 9)时出现 */
.preset-more-popover-trigger {
  box-sizing: border-box;
  display: block;
  width: 100%;
  min-width: 0;
}
.preset-tile-more {
  box-sizing: border-box;
  display: inline-flex;
  gap: 4px;
  align-items: center;
  justify-content: center;
  width: 100%;
  padding: 3px 6px;
  font-size: 10.5px;
  line-height: 1.4;
  color: var(--uvp-brand);
  cursor: pointer;
  background: var(--uvp-brand-soft);
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 22%, var(--uvp-panel-border));
  border-radius: 5px;
  transition:
    background 0.12s ease,
    border-color 0.12s ease;
}
.preset-tile-more:hover {
  border-color: var(--uvp-brand);
}
.preset-popover {
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  min-width: 240px;
  max-width: 320px;
}
.preset-popover-hd {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  font-size: 11.5px;
  font-weight: 600;
  color: var(--uvp-text-primary);
  background: var(--uvp-list-toolbar-bg);
  border-bottom: 1px solid var(--uvp-panel-border);
}
.preset-popover-hd > span {
  display: inline-flex;
  gap: 5px;
  align-items: center;
}
.preset-popover-list {
  max-height: 280px;
  overflow-y: auto;
  scrollbar-color: color-mix(in srgb, var(--uvp-text-tertiary) 28%, transparent) transparent;
  scrollbar-width: thin;
}
.preset-popover-row {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  gap: 8px;
  align-items: center;
  padding: 6px 12px;
  border-bottom: 1px solid var(--uvp-panel-border);
  transition: background 0.12s ease;
}
.preset-popover-row:last-child {
  border-bottom: 0;
}
.preset-popover-row:hover {
  background: color-mix(in srgb, var(--uvp-brand) 4%, transparent);
}
.preset-popover-row.active {
  background: color-mix(in srgb, var(--uvp-brand) 8%, transparent);
}
.preset-popover-idx {
  font-family: ui-monospace, Menlo, monospace;
  font-size: 10.5px;
  color: var(--uvp-text-tertiary);
}
.preset-popover-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 11.5px;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.preset-popover-actions {
  display: inline-flex;
  gap: 4px;
}
.preset-popover-call,
.preset-popover-del {
  display: inline-grid;
  place-items: center;
  width: 24px;
  height: 24px;
  cursor: pointer;
  background: transparent;
  border: 1px solid transparent;
  border-radius: 5px;
  transition:
    color 0.12s ease,
    background 0.12s ease,
    border-color 0.12s ease;
}
.preset-popover-call {
  color: var(--uvp-brand);
}
.preset-popover-call:hover:not(:disabled) {
  background: var(--uvp-brand-soft);
  border-color: color-mix(in srgb, var(--uvp-brand) 30%, transparent);
}
.preset-popover-del {
  color: color-mix(in srgb, var(--uvp-danger) 65%, var(--uvp-text-tertiary));
}
.preset-popover-del:hover:not(:disabled) {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
}
.preset-popover-call:disabled,
.preset-popover-del:disabled {
  cursor: not-allowed;
  opacity: 0.4;
}

/* 预置位/巡航卡片头部行(标题 + 动作),复用 .linked-card 提供的实线蓝框容器。
 *
 * 头部现在承载**两个**动作(「同步」药丸 + 「添加」按钮),而卡片是详情条三等分
 * (148px 高的条里塞三张卡),一行放不下时必须换行而不是被裁掉 —— 卡片是
 * overflow: hidden,溢出的部分会直接消失,按钮点都点不到。
 * 头部本身是 flex: 0 0 auto,换行只会吃掉卡片主体的高度(主体本来就 overflow:hidden),
 * 不会把卡片撑破。 */
.linked-card-hd {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  row-gap: 4px;
  align-items: center;
  justify-content: space-between;
}
.linked-card-hd .section-title {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  min-width: 0;
  font-size: 11.5px;
  font-weight: 600;
  color: var(--uvp-text-primary);
}

/* 动作区整体不参与收缩:宁可让标题在极窄卡片上换行,也不能把按钮压成一条线。 */
.linked-card-actions {
  display: inline-flex;
  flex: 0 0 auto;
  gap: 6px;
  align-items: center;
}
.preset-count {
  display: inline-flex;
  align-items: center;
  padding: 1px 6px;
  margin-left: 4px;
  font-family: ui-monospace, Menlo, monospace;
  font-size: 10px;
  font-style: normal;
  font-weight: 600;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-radius: 999px;
}
.preset-save-btn {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  padding: 3px 9px;
  font-size: 10.5px;
  font-weight: 500;
  color: var(--uvp-brand);
  cursor: pointer;
  background: transparent;
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 40%, var(--uvp-panel-border));
  border-radius: 5px;
  transition:
    background 0.12s ease,
    border-color 0.12s ease;
}
.preset-save-btn:hover:not(:disabled) {
  color: #ffffff;
  background: var(--uvp-brand);
  border-color: var(--uvp-brand);
}

/* 「设备配置」入口:同款描边小按钮,压在动作区最右。 */
.device-config-open-btn {
  font-weight: 500;
  white-space: nowrap;
}
.preset-save-btn:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

/* 「从设备同步」药丸:预置位与巡航两张卡片共用同一个形态,位置也相同(动作区最左)。
 *
 * ⛔ 字号压到 9.5px、文案限长到 6 个汉字,是被版面逼出来的:卡片宽度是详情条的
 *    1/3,头部还要并排放「添加」。原来那句「数据过期,点击同步」9 个字会让整个
 *    动作区越界,而卡片是 overflow: hidden —— 结果不是换行,是「添加」按钮被裁掉。
 *    "点我"这层意思交给按钮形态和 tooltip,不占字宽。 */
.resource-sync-btn {
  display: inline-flex;
  gap: 3px;
  align-items: center;
  padding: 2px 6px;
  font-size: 9.5px;
  font-weight: 500;
  line-height: 1.6;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
  cursor: pointer;
  background: transparent;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 5px;
  transition:
    color 0.12s ease,
    background 0.12s ease,
    border-color 0.12s ease;
}
.resource-sync-btn:hover:not(:disabled) {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-color: color-mix(in srgb, var(--uvp-brand) 40%, var(--uvp-panel-border));
}
.resource-sync-btn:disabled {
  cursor: progress;
}

/* 同步中:药丸本身点亮,和「什么都不做」区分开。转圈用 Loader2 + 旋转,
   不用 CSS 动画换图标 —— 换图标会在旋转中闪。 */
.resource-sync-btn.syncing {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-color: color-mix(in srgb, var(--uvp-brand) 32%, var(--uvp-panel-border));
}
.resource-sync-spin {
  animation: resource-sync-rotate 0.9s linear infinite;
}

@keyframes resource-sync-rotate {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}
.preset-empty {
  display: grid;
  gap: 2px;
  place-items: center;

  /* ⛔ 上下的 padding 是**卡片高度预算**的一部分:详情条总高 148px,三张卡片等分,
        空态能用的余量只有个位数像素。这里每加一行文字都必须重算,否则最后一行会被
        卡片自己的 overflow: hidden 裁掉(表现在"设备上已有的可用「同步」读回"这行
        只露上半截)。 */
  padding: 18px 12px 14px;
}

/* 空态的第二行:交代"平台没记录 ≠ 设备上没有"。原来的「暂无预置位」只说了前半句,
   操作员看到设备明明有预置位、界面却说没有,会直接判定平台坏了。 */
.preset-empty-hint {
  margin: 0;
  font-size: 10px;
  line-height: 1.4;
  color: var(--uvp-text-tertiary);
  text-align: center;
  opacity: 0.85;
}
.resource-summary-action {
  display: flex;
  gap: 6px;
  align-items: center;
  justify-content: space-between;
  min-height: 22px;
  padding: 2px 7px;
  font-size: 9.5px;
  color: var(--uvp-brand);
  cursor: pointer;
  background: var(--uvp-brand-soft);
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 24%, var(--uvp-panel-border));
  border-radius: 6px;
}
.preset-grid > .resource-summary-action {
  grid-column: 1 / -1;
}
.resource-summary-action:hover {
  border-color: var(--uvp-brand);
}
.linked-detail .home-config {
  gap: 5px;
  padding-top: 2px;
}

/* stretch 而不是 center:两栏撑满详情条高度,跟 .linked-ptz-layout 保持一致。
 * 用 center 的话,删掉提示行让出来的高度只会变成上下留白,内容一点没多。 */

/* 三栏:轨道明细 / 时间戳监控 / 帧到达时间线。
 * 不等分 —— 时间线是横向柱状图,32 根柱子三等分只剩约 285px(每根不到 9px),
 * 帧间隔异常会看不出来。给它 1.4fr(约 357px,每根约 11px)。 */
.linked-probe-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) minmax(0, 1.4fr);
  gap: 12px;
  align-items: stretch;
  min-height: 0;
}

/* 每栏各自纵向撑满,内部再把余量交给主体(轨道 / 指标网格 / 柱状图) */
.linked-probe-layout > .linked-section {
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.linked-probe-layout > .linked-section > .section-hd {
  flex: 0 0 auto;
}
.linked-probe-layout > .linked-section > .probe-track-merged,
.linked-probe-layout > .linked-section > .probe-health-grid,
.linked-probe-layout > .linked-section > .frame-overview {
  flex: 1 1 0;
  min-height: 0;
}

/* 底部三块套上跟侧栏 .probe-card 同款卡片外壳(浅底 + 描边 + 圆角)。
 * 加了外壳后,内部原来那层 .probe-track-merged / .probe-health-grid / .frame-overview 的
 * 独立底色和边框会跟卡片形成"套框",逐一去掉,只留骨架。
 * 相邻两卡之间的分隔线也一并去掉 —— 卡片本身的间距和边框已经足够表达"这是三块"。 */
.probe-detail-card {
  padding: 8px 12px 10px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
}
.probe-detail-card > .section-hd.first {
  margin-top: 0;
}
.probe-detail-card > .probe-track-merged,
.probe-detail-card > .probe-health-grid,
.probe-detail-card > .frame-overview {
  padding: 0;
  background: transparent;
  border: 0;
}

/* 轨道明细里的"视频/音频"分割线原来是靠 border-top,套进卡片后保留就好,
 * 因为它是"两条轨道之间的分隔"而不是"跟卡片外的分隔"。 */

/* 概览条的柱高不再写死:交给 .frame-overview 的 1fr 行按剩余空间分配,
 * 下限见 .frame-overview-bars 的 min-height,避免详情条被压缩时糊成一条线。 */
@media (width <= 720px) {
  .linked-detail-hint {
    margin-bottom: 6px;
  }
  .linked-detail {
    height: auto;
    overflow: visible;
  }

  /* 窄屏堆成单列。三栏的探针详情条在 720px 下横向排不开,柱状图会糊掉。 */
  .linked-ptz-layout,
  .linked-probe-layout {
    grid-template-columns: 1fr;
  }
  .linked-detail > .linked-ptz-layout {
    flex: 0 0 auto;
    grid-template-rows: none;
    height: auto;
  }
  .linked-section {
    padding: 12px 0;
  }
  .linked-section:first-child {
    padding-top: 0;
  }
  .linked-section:last-child {
    padding-bottom: 0;
  }
  .linked-section + .linked-section {
    border-top: 1px solid var(--uvp-panel-border);
    border-left: 0;
  }
  .linked-ptz-layout > .linked-card,
  .linked-ptz-layout > .linked-card + .linked-card {
    height: auto;
    padding: 8px 10px 10px;
    border: 1px solid color-mix(in srgb, var(--uvp-brand) 22%, var(--uvp-panel-border));
  }
  .asset-manager-layer {
    position: fixed;
    inset: 12px;
    border: 1px solid var(--uvp-panel-border);
  }
  .asset-manager-drawer {
    width: 100%;
    border-left: 0;
  }
}

/* ═══════════ 右侧栏 ═══════════ */

/* 两行:tab 条按内容高,面板盒吃掉剩余的全部高度,底边和画面区对齐。
 * align-content: stretch 是关键,不然面板盒只按内容撑开,底部会留一块空白。 */

/* overflow: hidden 是兜底 —— CSS Grid 规范下,子内容超出 track 时默认 visible,
 * 会一路撑到 body,进而撑大 modal。设 hidden 后 grid item 的 min-content 收敛到 0,
 * 侧栏高度严格按画面 aspect-ratio 决定的行高走,内部超出交给 .panels 的 overflow-y: auto 滚动。 */
.sidebar {
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  grid-row: 1;
  grid-column: 2;
  gap: 10px;
  align-content: stretch;
  min-height: 0;
  max-height: none;
  padding-right: 2px;
  overflow: hidden;
}
.sidebar::-webkit-scrollbar {
  width: 6px;
}
.sidebar::-webkit-scrollbar-thumb {
  background: color-mix(in srgb, var(--uvp-text-tertiary) 30%, transparent);
  border-radius: 3px;
}

/* Tab 切换。auto-fit + minmax(0, 1fr) 会按 tabs 数组实际条数平分整行宽度,
 * 不用再跟 v-for 长度绑死。以后要加/减 tab 只改数组、不用回头调 CSS。 */
.tabs {
  display: grid;
  grid-auto-columns: minmax(0, 1fr);
  grid-auto-flow: column;
  gap: 4px;
  padding: 4px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
}
.tab {
  position: relative;
  display: inline-flex;
  flex-direction: column;
  gap: 3px;
  align-items: center;
  justify-content: center;
  padding: 8px 4px;
  font-size: 10.5px;
  color: var(--uvp-text-tertiary);
  letter-spacing: 0.02em;
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 7px;
  transition: all 0.15s ease;
}
.tab:hover {
  color: var(--uvp-text-secondary);
  background: color-mix(in srgb, var(--uvp-brand) 6%, transparent);
}
.tab.active {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--uvp-brand) 30%, transparent);
}

/* Panel 容器 */
.panels {
  padding: 14px;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 12px;
}

/* 高度吃满后内容可能反过来超出(比如云台面板一整列按钮遇上矮屏),
 * 给一个纵向滚动兜底,不要顶破面板。 */
.sidebar .panels {
  box-sizing: border-box;
  height: 100%;
  min-height: 0;
  padding: 12px 14px;
  overflow-y: auto;
}

/* 探针 tab 已经用两张独立 .probe-card 分块了,外层大卡片显得多余(卡里套卡)。
 * 用 activeTab 联动的 .sidebar-probe class 精确关掉,不动其他 tab 共用的样式。 */
.sidebar-probe .panels,
.sidebar-deviceconfig .panels {
  padding: 0;
  background: transparent;
  border-color: transparent;
  box-shadow: none;
}
.sidebar-deviceconfig-panel {
  height: 100%;
  min-height: 0;
}

.sidebar-deviceconfig-panel :deep(.dcg-window--embedded) {
  min-height: 0;
}
.panel {
  display: grid;
  gap: 10px;
}

.section-hd {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
  margin-top: 10px;
}
.section-hd.first {
  margin-top: 0;
}
.section-title {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  font-size: 11.5px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
}
.section-title .tag-2022 {
  padding: 1px 5px;
  margin-left: 4px;
  font-size: 9px;
  font-weight: 700;
  color: var(--uvp-brand-cyan);
  letter-spacing: 0.06em;
  background: color-mix(in srgb, var(--uvp-brand-cyan) 14%, transparent);
  border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 30%, transparent);
  border-radius: 4px;
}
.section-meta {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  font-size: 10.5px;
  color: var(--uvp-text-tertiary);
}
.section-meta .dot {
  width: 6px;
  height: 6px;
  background: var(--uvp-text-tertiary);
  border-radius: 50%;
}

/* ═══════════ 云台面板 ═══════════ */
.mode-switch {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 4px;
  padding: 4px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.mode-switch button {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  justify-content: center;
  padding: 6px 8px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 6px;
  transition: all 0.15s ease;
}
.mode-switch button.active {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--uvp-brand) 26%, transparent);
}
.tag-2022 {
  padding: 1px 4px;
  font-size: 8.5px;
  font-weight: 700;
  color: var(--uvp-brand-cyan);
  letter-spacing: 0.05em;
  background: color-mix(in srgb, var(--uvp-brand-cyan) 14%, transparent);
  border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 30%, transparent);
  border-radius: 3px;
}

/* 拖拽摇杆 */
.joystick-stage {
  position: relative;
  width: min(176px, 100%);
  aspect-ratio: 1;
  margin: 10px auto 6px;
  touch-action: none;
  cursor: grab;
  user-select: none;
  border-radius: 50%;
}
.joystick-stage.active {
  cursor: grabbing;
}
.joystick-stage:focus-visible {
  outline: 2px solid var(--uvp-brand);
  outline-offset: 3px;
}
.joystick-base {
  position: absolute;
  inset: 0;
  background: radial-gradient(
    circle at 36% 28%,
    color-mix(in srgb, white 18%, var(--uvp-brand-soft)) 0%,
    var(--uvp-brand-soft) 46%,
    color-mix(in srgb, var(--uvp-text-primary) 12%, var(--uvp-list-toolbar-bg)) 100%
  );
  border: 2px solid color-mix(in srgb, var(--uvp-brand) 30%, var(--uvp-panel-border));
  border-radius: 50%;
  box-shadow:
    inset 0 3px 4px color-mix(in srgb, white 14%, transparent),
    inset 0 -8px 14px color-mix(in srgb, var(--uvp-text-primary) 14%, transparent),
    0 8px 18px color-mix(in srgb, var(--uvp-brand) 15%, transparent),
    0 2px 4px color-mix(in srgb, var(--uvp-text-primary) 15%, transparent);
}
.joystick-base::before {
  position: absolute;
  inset: 25px;
  content: "";
  background: radial-gradient(
    circle at 44% 38%,
    color-mix(in srgb, var(--uvp-brand-soft) 30%, var(--uvp-list-toolbar-bg)) 0%,
    var(--uvp-list-toolbar-bg) 62%,
    color-mix(in srgb, var(--uvp-text-primary) 8%, var(--uvp-list-toolbar-bg)) 100%
  );
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 18%, var(--uvp-panel-border));
  border-radius: 50%;
  box-shadow:
    inset 0 5px 10px color-mix(in srgb, var(--uvp-text-primary) 11%, transparent),
    inset 0 -2px 4px color-mix(in srgb, white 8%, transparent),
    0 1px 0 color-mix(in srgb, white 10%, transparent);
}
.joystick-base::after {
  position: absolute;
  inset: 31px;
  content: "";
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 14%, var(--uvp-panel-border));
  border-radius: 50%;
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--uvp-text-primary) 4%, transparent);
}
.joystick-dots {
  position: absolute;
  inset: 0;
  z-index: 2;
  pointer-events: none;
}
.joystick-dot {
  position: absolute;
  width: 7px;
  height: 7px;
  background: radial-gradient(
    circle at 34% 28%,
    color-mix(in srgb, white 42%, var(--uvp-text-tertiary)) 0 18%,
    var(--uvp-text-tertiary) 58%,
    color-mix(in srgb, var(--uvp-text-primary) 35%, var(--uvp-text-tertiary)) 100%
  );
  border: 1px solid color-mix(in srgb, var(--uvp-text-primary) 12%, transparent);
  border-radius: 50%;
  box-shadow:
    inset 0 1px 1px color-mix(in srgb, white 26%, transparent),
    0 1px 2px color-mix(in srgb, var(--uvp-text-primary) 24%, transparent);
}
.joystick-dot.dot-top {
  top: 22px;
  left: 50%;
  transform: translateX(-50%);
}
.joystick-dot.dot-top-right {
  top: 38px;
  right: 38px;
}
.joystick-dot.dot-right {
  top: 50%;
  right: 22px;
  transform: translateY(-50%);
}
.joystick-dot.dot-bottom-right {
  right: 38px;
  bottom: 38px;
}
.joystick-dot.dot-bottom {
  bottom: 22px;
  left: 50%;
  transform: translateX(-50%);
}
.joystick-dot.dot-bottom-left {
  bottom: 38px;
  left: 38px;
}
.joystick-dot.dot-left {
  top: 50%;
  left: 22px;
  transform: translateY(-50%);
}
.joystick-dot.dot-top-left {
  top: 38px;
  left: 38px;
}
.joystick-label {
  position: absolute;
  z-index: 3;
  font-size: 9px;
  line-height: 1;
  color: var(--uvp-text-tertiary);
  pointer-events: none;
}
.joystick-label.top {
  top: 9px;
  left: 50%;
  transform: translateX(-50%);
}
.joystick-label.top-right {
  top: 8px;
  right: 8px;
}
.joystick-label.right {
  top: 50%;
  right: 9px;
  transform: translateY(-50%);
}
.joystick-label.bottom-right {
  right: 8px;
  bottom: 8px;
}
.joystick-label.bottom {
  bottom: 9px;
  left: 50%;
  transform: translateX(-50%);
}
.joystick-label.bottom-left {
  bottom: 8px;
  left: 8px;
}
.joystick-label.left {
  top: 50%;
  left: 9px;
  transform: translateY(-50%);
}
.joystick-label.top-left {
  top: 8px;
  left: 8px;
}
.joystick-handle {
  position: absolute;
  top: 50%;
  left: 50%;
  z-index: 4;
  display: grid;
  place-items: center;
  width: 52px;
  height: 52px;
  pointer-events: none;
  background: radial-gradient(
    circle at 34% 26%,
    color-mix(in srgb, white 88%, var(--uvp-brand)) 0 6%,
    color-mix(in srgb, white 30%, var(--uvp-brand)) 20%,
    var(--uvp-brand) 56%,
    var(--uvp-brand-strong) 100%
  );
  border: 5px solid color-mix(in srgb, white 58%, var(--uvp-brand));
  border-radius: 50%;
  box-shadow:
    inset 4px 4px 8px color-mix(in srgb, white 34%, transparent),
    inset -6px -8px 11px color-mix(in srgb, black 24%, transparent),
    0 9px 16px color-mix(in srgb, var(--uvp-brand) 34%, transparent),
    0 3px 4px color-mix(in srgb, black 28%, transparent),
    0 0 0 4px var(--uvp-brand-soft),
    0 0 0 5px color-mix(in srgb, var(--uvp-brand) 30%, transparent);
  transition: transform 0.22s cubic-bezier(0.2, 0.8, 0.2, 1);
}
.joystick-stage.active .joystick-handle {
  box-shadow:
    inset 3px 3px 7px color-mix(in srgb, white 28%, transparent),
    inset -5px -6px 9px color-mix(in srgb, black 28%, transparent),
    0 5px 10px color-mix(in srgb, var(--uvp-brand) 28%, transparent),
    0 2px 3px color-mix(in srgb, black 24%, transparent),
    0 0 0 4px var(--uvp-brand-soft),
    0 0 0 5px color-mix(in srgb, var(--uvp-brand) 38%, transparent);
  transition: none;
}
.joystick-handle span {
  position: absolute;
  top: 9px;
  left: 11px;
  width: 17px;
  height: 9px;
  background: linear-gradient(145deg, color-mix(in srgb, white 74%, transparent), transparent);
  border-radius: 50%;
  opacity: 0.82;
  filter: blur(0.2px);
}

@media (prefers-reduced-motion: reduce) {
  .joystick-handle {
    transition: none;
  }
  .ptz-direction-stack {
    opacity: 0.82;
    animation: none;
  }
}

.talk-mode-switch {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  width: 100%;
  max-width: 200px;
  padding: 2px;
  margin: 6px auto 0;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 7px;
}
.talk-mode-switch button {
  height: 22px;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 5px;
}
.talk-mode-switch button.active {
  font-weight: 600;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
}
.talk-mode-switch button:disabled {
  cursor: not-allowed;
  opacity: 0.42;
}

.talk-button {
  display: flex;
  gap: 7px;
  align-items: center;
  justify-content: center;
  width: 100%;
  max-width: 200px;
  height: 34px;
  margin: 8px auto 2px;
  font-size: 11px;
  font-weight: 600;
  color: var(--uvp-brand);
  touch-action: none;
  cursor: pointer;
  user-select: none;
  background: var(--uvp-brand-soft);
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 34%, var(--uvp-panel-border));
  border-radius: 8px;
  transition: all 0.15s ease;
}
.talk-button:hover:not(:disabled) {
  border-color: var(--uvp-brand);
}
.talk-button.active {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger-border);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--uvp-danger) 10%, transparent);
}
.talk-button:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

/* 「正在说话」的波形：4 根 bar 相位错开各自起伏，整体幅度再由采集电平（--talk-level，0..1）缩放。
 * 静态动画保证「一直在动」，电平缩放保证「动得和声音有关」。拿不到 AudioContext 时电平恒为 0，
 * 波形仍以 0.4 倍显示，不会变成空按钮。 */
.talk-wave {
  display: inline-flex;
  flex: none;
  gap: 2px;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 13px;
  transform: scaleY(calc(0.4 + var(--talk-level, 0) * 0.6));
  transform-origin: center;
  transition: transform 0.08s linear;
}
.talk-wave i {
  width: 2px;
  height: 100%;
  background: currentColor;
  border-radius: 1px;
  animation: talk-wave-pulse 0.9s ease-in-out infinite;
}

@keyframes talk-wave-pulse {
  0%,
  100% {
    transform: scaleY(0.32);
  }
  50% {
    transform: scaleY(1);
  }
}

@media (prefers-reduced-motion: reduce) {
  .talk-wave i {
    transform: scaleY(0.8);
    animation: none;
  }
}

.speed-row {
  padding: 6px 2px;
}
.speed-row label {
  display: grid;
  grid-template-columns: auto 1fr auto;
  gap: 8px;
  align-items: center;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.speed-row label > span {
  display: inline-flex;
  gap: 4px;
  align-items: center;
}
.speed-row input[type="range"] {
  accent-color: var(--uvp-brand);
}
.speed-row em {
  font-family: ui-monospace, Menlo, monospace;
  font-style: normal;
  font-weight: 600;
  color: var(--uvp-brand);
}

/* 镜头(变倍/聚焦/光圈) */
.lens-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 6px;
  margin-top: 6px;
}
.lens-grid.disabled {
  pointer-events: none;
  opacity: 0.42;
}
.lens-item {
  display: grid;
  gap: 6px;
  padding: 8px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.lens-label {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  font-size: 10.5px;
  color: var(--uvp-text-tertiary);
}
.lens-btns {
  display: flex;
  gap: 4px;
}
.lens-btns button {
  flex: 1;
  height: 26px;
  padding: 0;
  font-size: 11px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: transparent;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 5px;
}
.lens-btns button:hover:not(:disabled) {
  color: var(--uvp-brand);
  border-color: var(--uvp-brand);
}

/* 3D 拖拽(2026-09-20 从「高级」搬来):外盒沿用 .lens-item 的形态,让它在侧栏里
 * 和"变倍/聚焦/光圈"读成同一族;里面的分段按钮沿用 .lens-btns button 的尺寸语言。
 * 分段而不是两个独立按钮:放大/缩小是互斥的二选一(点另一个会换方向而不是叠加),
 * 分段控件把"只有一个生效"这件事直接画出来。 */
.ptz-drag-zoom {
  display: grid;
  gap: 6px;
  padding: 8px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.drag-zoom-switch {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 4px;
}
.drag-zoom-switch button {
  height: 26px;
  padding: 0;
  font-size: 11px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: transparent;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 5px;
}
.drag-zoom-switch button:hover:not(:disabled) {
  color: var(--uvp-brand);
  border-color: var(--uvp-brand);
}
.drag-zoom-switch button.active {
  font-weight: 600;
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-color: var(--uvp-brand);
}
.drag-zoom-switch button:disabled {
  cursor: not-allowed;
  opacity: 0.42;
}

/* 请求关键帧:与 3D 拖拽同款"标签 + 动作"盒,但只有一个动作,故走两列(标签/按钮)排一行。 */
.ptz-iframe {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 8px;
  align-items: center;
  padding: 8px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.ptz-iframe button {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  justify-content: center;
  height: 26px;
  padding: 0 8px;
  font-size: 11px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: transparent;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 5px;
}
.ptz-iframe button:hover:not(:disabled) {
  color: var(--uvp-brand);
  border-color: var(--uvp-brand);
}
.ptz-iframe button:disabled {
  cursor: not-allowed;
  opacity: 0.42;
}

/* 目标跟踪（A.2.3.1.14）:三段"动作"而不是"方向" —— 但它们同样是三选一里的"当前态"
 * （框选进行中时「框选跟踪」是唯一亮着的那个),所以沿用分段控件的视觉。
 * ⛔ 三个按钮都是**独立动作**而不是切换开关:「自动跟踪」每点一次都是一条新指令,
 *    不要把「自动」画成某种"已开启"的常驻开关 —— 无应答命令没有"当前态"可言。 */
.ptz-target-track {
  display: grid;
  gap: 6px;
  padding: 8px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.target-track-switch {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 4px;
}
.target-track-switch button {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  justify-content: center;
  height: 26px;
  padding: 0 4px;
  font-size: 11px;
  color: var(--uvp-text-secondary);
  cursor: pointer;
  background: transparent;
  border: 1px solid var(--uvp-panel-border);
  border-radius: 5px;
}
.target-track-switch button:hover:not(:disabled) {
  color: var(--uvp-brand);
  border-color: var(--uvp-brand);
}
.target-track-switch button.active {
  font-weight: 600;
  color: var(--uvp-warning);
  background: color-mix(in srgb, var(--uvp-warning) 14%, transparent);
  border-color: var(--uvp-warning);
}
.target-track-switch button:disabled {
  cursor: not-allowed;
  opacity: 0.42;
}

/* 状态三行:意图 / 下发结果 / 说明。⛔ 字号与颜色都往"事实"靠,不要做成告警条 ——
 * 无回执是**协议事实**而不是异常,把它画成警告色会让用户以为出错了。 */
.target-track-state,
.target-track-status,
.target-track-tip,
.target-track-error {
  margin: 0;
  font-size: 10.5px;
  line-height: 1.5;
  overflow-wrap: anywhere;
}
.target-track-state {
  color: var(--uvp-text-secondary);
}
.target-track-status {
  font-weight: 600;
  color: var(--uvp-warning);
}
.target-track-tip {
  color: var(--uvp-text-tertiary);
}
.target-track-error {
  color: var(--uvp-danger);
}

/* 精准 PTZ */

/* 面板盒填满高度后,内容若仍挤在顶部就会留出一块空腔。让当前模式的内容块
 * 占满面板并把纵向余量平均匀到各组之间(方向盘、对讲、速度、镜头),
 * 而不是把某一块拉长 —— 方向盘按钮拉高会很怪。
 * 余量为负(内容比盒子高)时 space-between 退化为顶部对齐,由 .panels 滚动兜底。 */

/* 云台面板是"模式切换 + 当前模式内容 + 3D 拖拽 + 关键帧 + 目标跟踪"五行:模式切换、3D 拖拽、
 * 关键帧与目标跟踪都按内容高,当前模式内容吃掉剩余高度。
 * 3D 拖拽/关键帧/目标跟踪刻意都排在两个模式块**之外**(见各自注释):它们是画面级即时动作,
 * 与速度/精准正交,切模式时不能跟着消失。
 * ⛔ 加一行画面级动作块时**这里也要跟着加一个 `auto`**:漏了的话多余的块会被塞进
 *    `minmax(0, 1fr)` 那一行(与当前模式内容挤在一起),表现为"方向盘被压扁"。 */
[data-testid="linked-side-ptz"] {
  grid-template-rows: auto minmax(0, 1fr) auto auto auto;
  height: 100%;
  min-height: 0;
}
.ptz-speed,
.ptz-precise {
  display: grid;
  gap: 10px;
  align-content: space-between;
  min-height: 0;
}

/* 探针 tab 两张卡:概览拿余量,检测按内容自然高。
 * 概览内容多(2×2 指标 + 视频/音频块),检测卡内容少(按钮 + 摘要 + 结论),
 * 让检测卡按 auto 收敛,不再被 1fr 拉平 —— 那样会让检测卡显得空、同时把整个侧栏顶高。 */
[data-testid="linked-side-probe"] {
  grid-template-rows: minmax(0, 1fr) auto;
  height: 100%;
  min-height: 0;
}

/* 卡片拉伸后,内部子块也要跟着分空间,否则内容挤顶部、卡里冒出新的空白。
 *
 * 概览卡:标题 auto,概览指标 auto,视频/音频分栏拿余量。
 * 检测卡:标题 / 按钮 / 结论 auto,采样摘要三格拿余量 —— 而不是按钮或标题拉高,
 * 那样会显得整卡在"注水"。 */
[data-testid="probe-check"] {
  grid-auto-rows: min-content;
  min-height: 0;
}
[data-testid="probe-check"] .probe-summary {
  align-self: stretch;
}
[data-testid="probe-check"] .probe-summary > div {
  align-content: center;
}
.precise-hint {
  display: flex;
  gap: 6px;
  align-items: flex-start;
  padding: 8px 10px;
  font-size: 10.5px;
  line-height: 1.5;
  color: var(--uvp-text-tertiary);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 8%, transparent);
  border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 24%, transparent);
  border-radius: 8px;
}
.precise-hint strong {
  font-weight: 600;
  color: var(--uvp-brand-cyan);
}
.axis-row label {
  display: grid;
  gap: 4px;
}
.axis-row label > span {
  font-size: 10.5px;
  color: var(--uvp-text-tertiary);
}
.axis-ctrl {
  display: grid;
  grid-template-columns: 1fr 60px;
  gap: 6px;
  align-items: center;
}
.axis-ctrl input[type="range"] {
  accent-color: var(--uvp-brand);
}
.axis-num {
  height: 26px;
  padding: 0 6px;
  font-family: ui-monospace, Menlo, monospace;
  font-size: 11px;
  color: var(--uvp-text-secondary);
  text-align: right;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 5px;
}
.precise-actions {
  display: flex;
  gap: 6px;
}
.precise-actions button {
  flex: 1;
}

/* 预置位 */
.preset-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 5px;
}
.preset-item {
  display: grid;
  grid-template-columns: auto 1fr auto;
  gap: 6px;
  align-items: center;
  padding: 8px 10px;
  font-size: 11px;
  color: var(--uvp-text-secondary);
  text-align: left;
  cursor: pointer;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 7px;
  transition: all 0.15s ease;
}
.preset-item:hover:not(:disabled) {
  border-color: color-mix(in srgb, var(--uvp-brand) 40%, var(--uvp-panel-border));
}
.preset-item.active {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-color: color-mix(in srgb, var(--uvp-brand) 40%, var(--uvp-panel-border));
}
.preset-item:disabled {
  cursor: not-allowed;
  opacity: 0.42;
}
.preset-idx {
  font-family: ui-monospace, Menlo, monospace;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}
.preset-item.active .preset-idx {
  color: var(--uvp-brand);
}
.preset-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.preset-del {
  display: inline-grid;
  place-items: center;
  width: 20px;
  height: 20px;
  color: var(--uvp-text-tertiary);
  border-radius: 4px;
  transition: all 0.15s ease;
}
.preset-del:hover {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
}
.preset-add {
  display: grid;
  grid-template-columns: 1fr auto;
  grid-column: 1 / -1;
  gap: 6px;
  padding-top: 6px;
  margin-top: 2px;
  border-top: 1px dashed var(--uvp-panel-border);
}
.preset-add input {
  height: 28px;
  padding: 0 8px;
  font-size: 11px;
  color: var(--uvp-text-secondary);
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 6px;
}
.preset-add input:focus {
  outline: none;
  border-color: var(--uvp-brand);
}

/* 巡航 */

/* 巡航 tile:复用 .preset-tile 尺寸/结构,active 用 brand-cyan 与预置位区分 */
.preset-tile.cruise-tile.active {
  background: color-mix(in srgb, var(--uvp-brand-cyan) 8%, var(--uvp-panel-bg));
  border-color: color-mix(in srgb, var(--uvp-brand-cyan) 45%, var(--uvp-panel-border));
}
.preset-tile.cruise-tile.active .preset-tile-hit {
  color: var(--uvp-brand-cyan);
}
.preset-tile.cruise-tile.active .cruise-tile-icon {
  color: var(--uvp-brand-cyan);
}
.preset-tile.cruise-tile:hover:not(.disabled) {
  border-color: color-mix(in srgb, var(--uvp-brand-cyan) 40%, var(--uvp-panel-border));
}
.preset-tile.cruise-tile.pending {
  background: var(--uvp-warning-soft);
  border-color: var(--uvp-warning-border);
}
.cruise-tile-icon {
  flex-shrink: 0;
  color: var(--uvp-text-tertiary);
}
.cruise-status-badge {
  flex: 0 0 auto;
  padding: 0 4px;
  font-size: 8.5px;
  font-weight: 600;
  line-height: 1.5;
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
  border: 1px solid var(--uvp-warning-border);
  border-radius: 3px;
}

/* 卡片头运行中标签:brand-cyan chip + 呼吸点,点击停止全部巡航 */
.cruise-running-chip {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  padding: 1px 7px 1px 6px;
  margin-left: 6px;
  font-size: 10px;
  font-weight: 600;
  line-height: 1.5;
  color: var(--uvp-brand-cyan);
  cursor: pointer;
  background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 32%, transparent);
  border-radius: 999px;
  transition:
    background 0.12s ease,
    border-color 0.12s ease;
}
.cruise-running-chip:hover {
  background: color-mix(in srgb, var(--uvp-brand-cyan) 18%, transparent);
  border-color: var(--uvp-brand-cyan);
}
.cruise-running-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  background: var(--uvp-brand-cyan);
  border-radius: 50%;
}
.cruise-running-dot.start-sent {
  animation: cruise-pulse 1.4s ease-in-out infinite;
}

@keyframes cruise-pulse {
  0%,
  100% {
    box-shadow: 0 0 0 0 color-mix(in srgb, var(--uvp-brand-cyan) 50%, transparent);
    opacity: 1;
  }
  50% {
    box-shadow: 0 0 0 4px color-mix(in srgb, var(--uvp-brand-cyan) 0%, transparent);
    opacity: 0.55;
  }
}

/* 看守位 */
.home-header-actions {
  display: inline-flex;
  gap: 8px;
  align-items: center;
}
.home-diagnostics {
  display: inline-grid;
  place-items: center;
  width: 20px;
  height: 20px;
  color: var(--uvp-text-tertiary);
  cursor: help;
}
.home-config {
  display: grid;
  gap: 7px;
  padding-top: 4px;
  container-type: inline-size;
}
.home-state-row {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  min-width: 0;
}
.home-state-icon {
  display: inline-grid;
  flex: 0 0 26px;
  place-items: center;
  width: 26px;
  height: 26px;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-list-toolbar-bg);
  border-radius: 6px;
}
.home-state-copy {
  display: grid;
  gap: 1px;
  min-width: 0;
  line-height: 1.4;
}
.home-state-copy strong {
  font-size: 11px;
  color: var(--uvp-text-primary);
}
.home-state-copy span {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 9.5px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}
.home-state-copy [data-testid="home-confirmed-values"] {
  color: var(--uvp-text-secondary);
  white-space: normal;
}
.home-confirmed-at {
  font-size: 9px !important;
}
.home-config.state-success .home-state-icon {
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 10%, transparent);
}
.home-config.state-danger .home-state-icon {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
}
.home-config.state-loading .home-state-icon {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
}

/* `.home-hint` 与警告/错误同一排版,只是语气不同:提示是「还缺前置条件」,
 * 不是「你填错了」,所以用次级文字色而不是红/黄,免得跟真正的校验失败混在一起。 */
.home-warning,
.home-error,
.home-hint {
  margin: 0;
  font-size: 9.5px;
  line-height: 1.5;
  overflow-wrap: anywhere;
}
.home-warning {
  color: var(--uvp-warning);
}
.home-error {
  color: var(--uvp-danger);
}
.home-hint {
  color: var(--uvp-text-tertiary);
}
.home-card-actions {
  display: flex;
  gap: 6px;
  min-width: 0;
  margin-top: auto;
}
.home-card-actions button {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  justify-content: center;
  min-width: 0;
  white-space: nowrap;
}
.home-card-actions .uvp-refresh-btn {
  margin-left: auto;
}
.home-close-btn {
  color: var(--uvp-danger);
  border-color: var(--uvp-danger-border);
}
.home-close-btn:hover:not(:disabled) {
  color: var(--uvp-danger);
  background: var(--uvp-danger-soft);
  border-color: var(--uvp-danger);
}

/* ── 自动扫描卡片(89H 开始/边界,8AH 速度)──
 *
 * ⛔ 卡片是详情条的四等分之一 —— 加了第 4 张卡之后每张更窄了。所以每行都允许换行:
 *    组号/速度输入框固定窄宽,按钮吃掉剩余空间;窄到放不下时整行 wrap,
 *    绝不能让按钮被压成一条线(卡片是 overflow: hidden,压不下就**裁掉**而不是换行)。
 * ⛔ 高度同样是硬预算:详情条 148px,三行控件 + 一行提示刚好卡在上限内,
 *    再加一行就会被裁(见 .preset-empty 那条注释里的算法)。 */
.scan-panel {
  display: grid;
  gap: 5px;
  align-content: start;
  height: 100%;
  padding-top: 2px;
}
.scan-row {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
  align-items: center;
}
.scan-label {
  flex: 0 0 auto;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}
.scan-number {
  box-sizing: border-box;
  flex: 0 0 auto;
  width: 52px;
  height: 20px;
  padding: 0 5px;
  font-family: ui-monospace, Menlo, monospace;
  font-size: 10.5px;
  color: var(--uvp-text-primary);
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 4px;
}
.scan-number:focus {
  outline: none;
  border-color: var(--uvp-brand);
}
.scan-toggle {
  margin-left: auto;
}
.scan-bound-btn {
  flex: 1 1 auto;
}
.scan-hint,
.scan-error {
  margin: 0;
  font-size: 10px;
  line-height: 1.4;
  color: var(--uvp-text-tertiary);
}
.scan-error {
  color: var(--uvp-danger);
}

.home-settings-form {
  display: grid;
  gap: 18px;
  padding-top: 4px;
}
.home-settings-field {
  display: grid;
  gap: 7px;
}
.home-settings-field > label {
  font-size: 12px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
}
.home-settings-field > label > span {
  color: var(--uvp-danger);
}
.home-settings-field select,
.home-settings-field input {
  box-sizing: border-box;
  width: 100%;
  height: 36px;
  padding: 0 10px;
  font-size: 12px;
  color: var(--uvp-text-primary);
  outline: none;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 6px;
}
.home-settings-field select:focus,
.home-settings-field input:focus {
  border-color: var(--uvp-brand);
}
.home-settings-time {
  display: grid;
  grid-template-columns: 110px minmax(0, 1fr);
  gap: 10px;
  align-items: center;
}
.home-settings-time > span {
  font-size: 12px;
  color: var(--uvp-text-secondary);
}
.home-settings-description,
.home-settings-error {
  margin: -4px 0 0;
  font-size: 11px;
  line-height: 1.6;
}
.home-settings-description {
  color: var(--uvp-text-tertiary);
}
.home-settings-error {
  color: var(--uvp-danger);
}
.home-settings-actions {
  display: flex;
  gap: 8px;
  justify-content: flex-end;
  padding-top: 2px;
}
.home-settings-actions button {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  justify-content: center;
  min-width: 86px;
}

/* ═══════════ 探针面板 ═══════════ */
.probe-panel {
  gap: 12px;
}
.probe-header {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
}

/* 探针面板里的独立卡片:概览 / 逐帧健康检测。
 * 各自有底色和边框,两卡之间靠 .probe-panel 的 gap(12px) 拉开距离。
 * 底色用 list-toolbar-bg(比 panel-bg 略深),这样即使外层 .panels 是白底,
 * 两张卡的边界仍然一眼可辨,不至于糊成一片。 */
.probe-card {
  display: grid;
  gap: 7px;
  padding: 9px 11px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 10px;
}

/* 概览卡:标题 / 概览指标 auto,视频/音频分栏拿余量 —— 但只是外壳拉伸,
 * 内部行距保持紧凑。之前用 flex:1 1 auto 让 stream-brief-rows 吸收余量,
 * 结果把编码/分辨率/帧率/丢包之间的间距顶得太大。改成外壳等高、内容顶到上方。
 *
 * 概览卡自身已经用 .stream-brief 的 gap 排版了,不必再叠一层内边距 gap。 */
.probe-card.stream-brief {
  grid-template-rows: auto auto minmax(0, 1fr);
  gap: 7px;
  min-height: 0;
}

/* 卡内的 section-hd.first 不再需要 margin-top:0 的特殊值,顶部内边距已经交给 .probe-card 处理 */
.probe-card .section-hd.first {
  margin-top: 0;
}

/* ═══════════ 流信息(探针面板顶部) ═══════════ */
.stream-brief {
  display: grid;
  gap: 7px;
}
.stream-brief-overview {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.stream-brief-overview > div {
  display: grid;
  gap: 2px;
  min-width: 0;
  padding: 7px 9px;
}

/* 2×2 网格的十字分隔线:偶数列(2n)加左线,第 3 格起(n+3)加顶线。
 * 这样布局与格子数解耦 —— 以后要加"码率"「延迟」等,自动流入下一行也照样有线。 */
.stream-brief-overview > div:nth-child(2n) {
  border-left: 1px solid var(--uvp-panel-border);
}
.stream-brief-overview > div:nth-child(n + 3) {
  border-top: 1px solid var(--uvp-panel-border);
}
.stream-brief-overview span {
  font-size: 9.5px;
  color: var(--uvp-text-tertiary);
}
.stream-brief-overview strong {
  overflow: hidden;
  text-overflow: ellipsis;
  font-family: ui-monospace, Menlo, monospace;
  font-size: 14px;
  font-weight: 650;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.stream-brief-overview small {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 9px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}

/* 音频在左、视频在右。左侧色条沿用探针轨道卡的配色(视频=品牌蓝、音频=青),
 * 同一种媒体在面板里始终是同一个颜色,不用读标题也能对上。 */
.stream-brief-split {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px;
  align-items: stretch;
  min-height: 0;
}
.stream-brief-kind {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  padding: 7px 9px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.stream-brief-kind.audio {
  border-left: 2px solid var(--uvp-brand-cyan);
}
.stream-brief-kind.video {
  border-left: 2px solid var(--uvp-brand);
}
.stream-brief-kind header {
  display: inline-flex;
  gap: 4px;
  align-items: center;
  margin-bottom: 5px;
  font-size: 10px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
}
.stream-brief-rows {
  display: grid;
  gap: 3px;
}
.stream-brief-rows > div {
  display: flex;
  gap: 6px;
  align-items: baseline;
  justify-content: space-between;
  min-width: 0;
}
.stream-brief-rows span {
  flex-shrink: 0;
  font-size: 9.5px;
  color: var(--uvp-text-tertiary);
}
.stream-brief-rows strong {
  overflow: hidden;
  text-overflow: ellipsis;
  font-family: ui-monospace, Menlo, monospace;
  font-size: 10.5px;
  font-weight: 600;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.stream-brief-rows strong.warn {
  color: var(--uvp-warning);
}
.stream-brief-rows strong.err {
  color: var(--uvp-danger);
}
.probe-status {
  display: inline-flex;
  flex-shrink: 0;
  gap: 5px;
  align-items: center;
  padding: 3px 7px;
  font-size: 9.5px;
  color: var(--uvp-text-tertiary);
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 999px;
}
.probe-status .dot {
  width: 5px;
  height: 5px;
  background: currentcolor;
  border-radius: 50%;
}
.probe-status.sampling {
  color: var(--uvp-warning);
  border-color: var(--uvp-warning-border);
}
.probe-status.sampling .dot {
  animation: pulse 1s ease-in-out infinite;
}
.probe-status.complete {
  color: var(--uvp-brand-cyan);
  border-color: color-mix(in srgb, var(--uvp-brand-cyan) 30%, var(--uvp-panel-border));
}

/* 主操作按钮 : 采样时长选择器 = 7 : 3。
 * ⚠️ 下拉框必须走 :deep() —— a-select 的根节点是 Arco 内部渲染的 <span>,
 * 拿不到本组件的 scoped 属性(实测 hasScopeAttr=false),写成 .probe-duration
 * 能编译通过却永远匹配不到它,那时它只剩 Arco 自带的 width:100%,
 * 会把按钮挤成竖排窄条(只剩 padding 撑出的 24px)。
 * 两侧都显式 min-width:0 —— flex item 默认 min-width:auto 会被内容顶住。 */
.probe-action-row {
  display: flex;
  gap: 6px;
  align-items: stretch;
}
.probe-action {
  display: inline-flex;
  flex: 7 1 0;
  gap: 6px;
  align-items: center;
  justify-content: center;
  width: auto;
  min-width: 0;
  min-height: 30px;
  padding: 0 12px;
  font-size: 11.5px;
  font-weight: 600;
  color: #ffffff;
  cursor: pointer;
  background: var(--uvp-brand);
  border: 0;
  border-radius: 7px;
  transition: all 0.15s ease;
}
.probe-action:hover:not(:disabled) {
  background: var(--uvp-brand-strong);
}
.probe-action:disabled {
  cursor: not-allowed;
  opacity: 0.56;
}

/* 这里只管布局:边框/底色/圆角/字号统一由 styles/arco-overrides.scss 的
 * .arco-select-view 提供,避免两处规则打架 */
:deep(.probe-duration) {
  flex: 3 1 0;
  min-width: 0;
}
.probe-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 8px;
}
.probe-summary > div {
  display: grid;
  gap: 3px;
  min-width: 0;
  padding: 9px 10px;
}
.probe-summary > div + div {
  border-left: 1px solid var(--uvp-panel-border);
}
.probe-summary span,
.probe-health-grid span {
  font-size: 9.5px;
  color: var(--uvp-text-tertiary);
}
.probe-summary strong {
  font-family: ui-monospace, Menlo, monospace;
  font-size: 15px;
  font-weight: 650;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.probe-summary em {
  margin-left: 2px;
  font-size: 9px;
  font-style: normal;
  font-weight: 400;
  color: var(--uvp-text-tertiary);
}
.probe-summary.muted {
  opacity: 0.56;
}

/* 结论条单行紧凑:高度从两行 45px 降到单行约 26px,完成态不再撑破侧栏。
 * 副信息(完成时间/具体问题)通过 title 承载,鼠标悬停可查。 */
.probe-verdict {
  display: flex;
  gap: 6px;
  align-items: center;
  padding: 5px 10px;
  color: var(--uvp-brand-cyan);
  background: color-mix(in srgb, var(--uvp-brand-cyan) 7%, transparent);
  border: 1px solid color-mix(in srgb, var(--uvp-brand-cyan) 24%, var(--uvp-panel-border));
  border-radius: 8px;
}
.probe-verdict > svg {
  flex-shrink: 0;
}
.probe-verdict strong {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 10.5px;
  font-weight: 600;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.section-meta.good {
  color: var(--uvp-brand-cyan);
}
.sidebar .probe-panel {
  gap: 8px;
}

/* 检测卡三格摘要:紧凑内边距,让概览卡有更多空间放 2×2 指标。
 * 采样数字降一档(13px),避开跟概览"当前观看"「数据速率」14px 的主视觉。 */
.sidebar .probe-summary > div {
  padding: 5px 8px;
}
.sidebar .probe-summary strong {
  font-size: 13px;
}
.sidebar .probe-verdict {
  padding: 7px 10px;
}

/* 轨道明细(视频 + 音频合并卡)。左侧类型条沿用全局配色约定:视频=品牌蓝、音频=青,
 * 跟流信息块的两栏一致,同一种媒体在整个面板里始终是同一个颜色。
 *
 * 两行改为等分并 stretch,单元格垂直居中:轨道明细内容天生比"时间戳监控/时间线"少
 * (视频 4 + 音频 2 = 6 单元格),按内容 auto 会在卡片下方堆一大片空白。
 * 让两行拉伸吃满卡高度,视频/音频块内部单元格垂直居中,空白变成两行之间的自然呼吸。 */
.probe-track-merged {
  display: grid;
  grid-template-rows: 1fr 1fr;
  gap: 6px;
  min-height: 0;
}
.probe-track-row {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 8px;
  align-content: center;
  align-items: center;
  min-height: 0;
}
.probe-track-row + .probe-track-row {
  padding-top: 6px;
  border-top: 1px solid var(--uvp-panel-border);
}
.probe-track-kind {
  display: inline-flex;
  flex-shrink: 0;
  gap: 4px;
  align-items: center;
  padding-left: 6px;
  font-size: 10px;
  font-weight: 600;
  color: var(--uvp-text-secondary);
  white-space: nowrap;
  border-left: 2px solid var(--uvp-panel-border);
}
.probe-track-row.video .probe-track-kind {
  border-left-color: var(--uvp-brand);
}
.probe-track-row.audio .probe-track-kind {
  border-left-color: var(--uvp-brand-cyan);
}
.probe-track-cells {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 2px 10px;
  min-width: 0;
}
.probe-track-cells > div {
  display: flex;
  gap: 6px;
  align-items: baseline;
  justify-content: space-between;
  min-width: 0;
}
.probe-track-cells span {
  flex-shrink: 0;
  font-size: 9.5px;
  color: var(--uvp-text-tertiary);
}
.probe-track-cells strong {
  overflow: hidden;
  text-overflow: ellipsis;
  font-family: ui-monospace, Menlo, monospace;
  font-size: 10.5px;
  color: var(--uvp-text-primary);
  white-space: nowrap;
}
.probe-health-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px;
}
.probe-health-grid > div {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 2px 6px;
  align-items: baseline;
  padding: 8px 9px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 7px;
}
.probe-health-grid strong {
  font-family: ui-monospace, Menlo, monospace;
  font-size: 10.5px;
  color: var(--uvp-text-primary);
}
.probe-health-grid em {
  grid-column: 1 / -1;
  font-size: 9px;
  font-style: normal;
  color: var(--uvp-text-tertiary);
}

/* 帧到达概览条:按时间分桶的密度条,点击进弹窗看逐帧。
 * 桶数固定(见 probeOverview.ts 的 PROBE_OVERVIEW_BUCKETS),所以帧再多也不会溢出 ——
 * 旧版逐帧一根柱子,60 秒采样下 min-width 会把整栏撑到九千多像素。
 * 空桶只留一条底线:空白本身就是「这段时间没有帧到达」。 */
.frame-overview {
  display: grid;
  grid-template-rows: minmax(0, 1fr) auto;
  gap: 6px;
  width: 100%;
  padding: 4px 6px;
  text-align: left;
  cursor: pointer;
  background: transparent;
  border: 0;
  border-radius: 8px;
  transition: background 0.14s ease;
}
.frame-overview:hover:not(:disabled) {
  background: var(--uvp-brand-soft);
}
.frame-overview:disabled {
  cursor: default;
}
.frame-overview.muted {
  opacity: 0.46;
}
.frame-overview-bars {
  display: flex;
  gap: 1px;
  align-items: flex-end;
  min-height: 46px;
  padding-bottom: 1px;
  border-bottom: 1px solid var(--uvp-panel-border);
}
.frame-overview-bar {
  flex: 1 1 0;
  min-width: 0;
  background: color-mix(in srgb, var(--uvp-brand) 62%, transparent);
  border-radius: 1px 1px 0 0;
  transition: height 0.2s ease;
}
.frame-overview-bar.empty {
  background: var(--uvp-panel-border);
}
.frame-overview-bar.stalled {
  background: var(--uvp-warning);
}
.frame-overview-foot {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
  font-size: 10px;
  color: var(--uvp-text-tertiary);
}
.frame-overview-cta {
  display: inline-flex;
  gap: 3px;
  align-items: center;
  color: var(--uvp-brand);
}
.frame-overview:hover:not(:disabled) .frame-overview-cta {
  text-decoration: underline;
}

/* 空态引导:柱状区中央的图标 + 一句说明。muted 状态下父级会整体淡化,
 * 引导条自身不用再降透明度;font-size 跟其他 meta 一档保持层级一致。 */
.frame-overview-empty {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: center;
  width: 100%;
  min-height: 46px;
  font-size: 10.5px;
  color: var(--uvp-text-tertiary);
}
.frame-overview-empty > svg {
  color: var(--uvp-text-tertiary);
}

/* ═══════════ 录制面板 ═══════════ */
.empty {
  padding: 24px;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
  text-align: center;
}

/* ─── 视频参数属性(A.2.1.13/A.2.4.7/A.2.3.2.5) ───
   与存储卡/抓拍配置同族配色,单独一套类名:三张卡各自独立开关,
   复用同一个类名会让"只改这一张卡"变成"三张一起动"。 */
.video-param-config {
  display: grid;
  grid-column: 1 / -1;
  gap: 8px;
  padding: 10px;
  background: color-mix(in srgb, var(--uvp-brand) 5%, transparent);
  border: 1px solid color-mix(in srgb, var(--uvp-brand) 20%, transparent);
  border-radius: 7px;
}
.video-param-heading {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
}
.video-param-heading span {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  font-size: 11px;
  font-weight: 600;
  color: var(--uvp-text-primary);
}
.video-param-heading em {
  margin-left: auto;
  font-size: 9px;
  font-style: normal;
  color: var(--uvp-brand-cyan);
}

/* ⛔ 语气分级靠类名而不是行内样式:mismatch 是"黄"不是"红"(设备没做错)。 */
.video-param-reconcile {
  margin: 0;
  font-size: 9.5px;
  line-height: 1.45;
}
.video-param-reconcile.reconcile-idle {
  color: var(--uvp-text-tertiary);
}
.video-param-reconcile.reconcile-busy {
  color: var(--uvp-text-secondary);
}
.video-param-reconcile.reconcile-ok {
  color: var(--uvp-brand-cyan);
}
.video-param-reconcile.reconcile-warn {
  color: var(--uvp-warning);
}
.video-param-reconcile.reconcile-error {
  color: var(--uvp-danger);
}
.video-param-mismatch {
  margin: 0;
  font-size: 9px;
  line-height: 1.45;
  color: var(--uvp-warning);
}
.video-param-absent {
  margin: 0;
  font-size: 9px;
  line-height: 1.45;
  color: var(--uvp-text-tertiary);
}
.video-param-streams {
  margin: 0;
  font-size: 9px;
  line-height: 1.45;
  color: var(--uvp-text-tertiary);
}
.video-param-streams[data-source="设备声明"] {
  color: var(--uvp-text-secondary);
}
.video-param-list {
  display: grid;
  gap: 8px;
}
.video-param-row {
  display: grid;
  gap: 5px;
  padding: 7px;
  background: var(--uvp-panel-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 6px;
}
.video-param-row[data-dirty="1"] {
  border-color: color-mix(in srgb, var(--uvp-warning) 45%, transparent);
}
.video-param-row-hd {
  display: flex;
  gap: 8px;
  align-items: center;
  justify-content: space-between;
  font-size: 10px;
  color: var(--uvp-text-secondary);
}
.video-param-row-hd em {
  font-size: 9px;
  font-style: normal;
  color: var(--uvp-warning);
}
.video-param-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px;
}
.video-param-fields label {
  display: grid;
  gap: 4px;
  min-width: 0;
  font-size: 9px;
  color: var(--uvp-text-tertiary);
}
.video-param-fields input,
.video-param-fields select {
  width: 100%;
  min-width: 0;
  padding: 6px 7px;
  color: var(--uvp-text-primary);
  background: var(--uvp-bg);
  border: 1px solid var(--uvp-border);
  border-radius: 5px;
}
.video-param-fields input:disabled,
.video-param-fields select:disabled {
  color: var(--uvp-text-tertiary);
  cursor: not-allowed;
  opacity: 0.7;
}
.video-param-hints {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 8px;
  font-size: 9px;
  color: var(--uvp-text-tertiary);
}

/* 来源徽标:回读命中=设备,设备没给=缺省。⛔ 不是装饰 —— 条件必选字段的缺席
   必须一眼看得出,否则用户会把"设备没给"读成"设备给了个 0"。 */
.video-param-hints span[data-source="设备"] {
  color: var(--uvp-text-secondary);
}
.video-param-hints span[data-source="缺省"] {
  color: var(--uvp-warning);
}
.video-param-empty {
  margin: 0;
  font-size: 9.5px;
  color: var(--uvp-text-tertiary);
}

/* 「参数对照」区(2026-09-20 从「视频编码」页签底栏搬进「画面设置」底栏第三格)。
   148px 定高下只放一张卡:横向三行对照。
   ⛔ 三行并排是这块的全部价值 —— 塞回侧栏窄卡会折成六行,读者就分不清
      哪一行是设备事实、哪一行是自己刚改的草稿了。
   ⛔ 卡片容器直接用 `.linked-section.linked-card`(它本身就是 flex column),
      不要再套一层专用的 layout 壳。 */
.vpc-grid {
  display: grid;
  flex: 1 1 0;
  gap: 5px;
  align-content: start;
  min-height: 0;
  padding: 8px 10px;
  overflow-y: auto;
  background: color-mix(in srgb, var(--uvp-brand-cyan) 5%, transparent);
  border-radius: 6px;
}
.vpc-hd {
  font-size: 9.5px;
  color: var(--uvp-text-tertiary);
}
.vpc-row {
  display: grid;
  grid-template-columns: 48px minmax(0, 1fr);
  gap: 6px;
  align-items: baseline;
}
.vpc-row span {
  font-size: 10px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}
.vpc-row strong {
  min-width: 0;
  font-size: 11px;
  font-weight: 500;
  color: var(--uvp-text-secondary);
  overflow-wrap: anywhere;
}

/* 逐项差异：只标「画面实测」那一侧 —— 设备回读是基准，画面是待验证的一方。
 * ⛔ 不把两行都染色：两边都变色就没人分得清"谁跟谁不一样"。 */
.vpc-row strong i {
  font-style: normal;
}
.vpc-row strong i.is-differ {
  color: var(--uvp-warning);
}
.vpc-bitrate {
  margin-left: 4px;
  font-style: normal;
  color: var(--uvp-text-tertiary);
}

/* 结论标签（2026-09-20 补）：这张卡原来只摆数据、不下结论，用户得自己拿眼睛比
 * `1080P` 和 `1920×1080`。判定规则见 `videoParamVerdict` 的注释（含为什么不比码率）。 */
.vpc-verdict {
  padding: 1px 5px;
  font-size: 9.5px;
  font-style: normal;
  white-space: nowrap;
  border-radius: 4px;
}
.vpc-verdict.is-same {
  color: var(--uvp-success);
  background: var(--uvp-success-soft);
}
.vpc-verdict.is-differ {
  color: var(--uvp-warning);
  background: var(--uvp-warning-soft);
}
.vpc-verdict.is-unknown {
  color: var(--uvp-text-tertiary);
  background: var(--uvp-list-toolbar-bg);
}
.vpc-empty {
  padding: 10px 2px;
  margin: 0;
  font-size: 10.5px;
  line-height: 1.5;
  color: var(--uvp-text-tertiary);
}
.video-param-error {
  margin: 0;
  font-size: 9px;
  line-height: 1.4;
  color: var(--uvp-danger);
}
.video-param-offline {
  margin: 0;
  font-size: 9px;
  color: var(--uvp-text-tertiary);
}
.video-param-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}
.video-param-submit {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  justify-content: center;
  min-height: 30px;
  cursor: pointer;
  border: 0;
  border-radius: 5px;
}
.video-param-dirty {
  font-size: 9px;
  color: var(--uvp-warning);
}

.image-adjust {
  display: grid;
  gap: 6px;
  margin-top: 4px;
}
.image-adjust label {
  display: grid;
  grid-template-columns: 60px 1fr;
  gap: 8px;
  align-items: center;
  font-size: 11px;
  color: var(--uvp-text-tertiary);
}
.image-adjust input[type="range"] {
  accent-color: var(--uvp-brand);
}

/* ═══════════ 通用按钮 ═══════════
 * ⚠️ `.btn-primary` 在本文件里被定义过多次(这里 / 悬浮播放器的 `.player-retry` /
 *    全局 `styles/arco-overrides.scss`),各自的 border-radius 是 7px / 8px / 10px ——
 *    同特异性下**后写的赢**,实际生效的是这一份的 7px,另外两个值是死代码。
 *    要改按钮圆角请改这里,别只改 arco-overrides(不生效,会白排查一轮)。
 * ⚠️ transition 只列具体属性,不用 `all`:暗色主题下按钮背景是 `linear-gradient`,
 *    `all` 会把 `background-image` 和 `box-shadow` 一起纳入过渡 —— 渐变不可插值时
 *    Chromium 会退化成整帧重绘,叠加 transform 后的合成层容易留下半张画面的残影。 */
.btn-primary,
.btn-danger,
.btn-ghost {
  display: inline-flex;
  gap: 5px;
  align-items: center;
  justify-content: center;
  padding: 7px 12px;
  font-size: 11.5px;
  font-weight: 600;
  color: #ffffff;
  cursor: pointer;
  background: var(--uvp-brand);
  border: 0;
  border-radius: 7px;
  transition:
    color 0.15s ease,
    background-color 0.15s ease,
    border-color 0.15s ease,
    opacity 0.15s ease,
    transform 0.15s ease;
}
.btn-primary:hover:not(:disabled) {
  background: var(--uvp-brand-strong);
}
.btn-primary:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}
.btn-primary.sm,
.btn-ghost.sm {
  padding: 5px 10px;
  font-size: 11px;
}
.btn-primary.xs,
.btn-ghost.xs {
  padding: 4px 8px;
  font-size: 10.5px;
}
.btn-danger {
  background: var(--uvp-danger);
}
.btn-danger:hover:not(:disabled) {
  background: color-mix(in srgb, var(--uvp-danger) 85%, #000000);
}
.btn-ghost {
  color: var(--uvp-text-secondary);
  background: transparent;
  border: 1px solid var(--uvp-panel-border);
}
.btn-ghost:hover:not(:disabled) {
  color: var(--uvp-brand);
  background: var(--uvp-brand-soft);
  border-color: var(--uvp-brand);
}
.btn-primary.block,
.btn-ghost.block,
.btn-danger.block {
  width: 100%;
}

/* 一级页签的版式就是上面那份 `.tabs` / `.tab`（属性栏顶部横排均分）。
 * ⛔ 2026-09-20～09-21 一度存在过一套 `.workbench-nav`（左列竖排、独立占一列 136px），
 *    已整体删除：留着它等于给"页签在哪一列"第二个答案，后来人改列数必然漏掉一处。 */

/* 「画面设置」页签上的未下发角标：浮条只在那一页出现，离开后草稿就"看不见入口"了 ——
 * 这枚点把"还有改动没下发"钉在页签上，用户切到任何页都带着它，且不打断任何操作。 */
.tab-draft-dot {
  position: absolute;
  top: 6px;
  right: 8px;
  width: 6px;
  height: 6px;
  background: var(--uvp-warning, #b66b12);
  border-radius: 50%;
}
.config-detail-bar .config-detail {
  height: auto;
  max-height: 310px;
  overflow-y: auto;
}
.config-detail .linked-card {
  background: transparent;
  border: 0;
  border-radius: 0;
  box-shadow: none;
}
.config-detail .deviceconfig-action-grid {
  display: flex;
  flex-wrap: wrap;
  margin-bottom: 8px;
}
.config-detail .deviceconfig-action {
  min-width: 150px;
}
.config-shortcuts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.config-shortcut {
  display: flex;
  gap: 10px;
  align-items: center;
  justify-content: space-between;
  min-width: 0;
  padding: 9px 10px;
  background: var(--uvp-list-toolbar-bg);
  border: 1px solid var(--uvp-panel-border);
  border-radius: 6px;
}
.config-shortcut-copy {
  display: grid;
  gap: 3px;
  min-width: 0;
}
.config-shortcut-copy strong {
  font-size: 11px;
  color: var(--uvp-text-secondary);
}
.config-shortcut-copy span {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 9px;
  color: var(--uvp-text-tertiary);
  white-space: nowrap;
}

/* 响应式:窄屏堆叠 */
@media (width <= 1080px) {
  .console-body {
    grid-template-columns: 1fr;
  }

  /* 单列堆叠顺序：画面 → 属性栏（页签在它顶部）→ 底栏 */
  .stage.stage-wide .video-frame {
    grid-column: 1;
  }
  .video-frame {
    grid-row: 1;
    grid-column: 1;
  }
  .sidebar {
    grid-row: 2;
    grid-column: 1;
    max-height: none;
  }
  .linked-info-bar {
    grid-row: 3;
    grid-column: 1;
  }
}

@media (width <= 640px) {
  .console-title {
    flex-wrap: wrap;
  }
  .console-title:not(.is-minimized) .console-window-action {
    width: 30px;
    min-width: 30px;
    padding: 0;
  }
  .console-title:not(.is-minimized) .console-window-action span {
    display: none;
  }

  /* 窄屏保持等分行为,不再硬编码列数(3 个 tab 也可能变);grid-auto-columns 会按 tabs 数量平分。 */
  .lens-grid {
    grid-template-columns: 1fr;
  }
  .linked-section .preset-grid {
    grid-template-columns: 1fr;
  }
}
</style>
