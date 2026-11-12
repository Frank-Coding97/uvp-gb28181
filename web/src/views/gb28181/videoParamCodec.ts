/**
 * 「视频参数属性」的码值与校验 —— 全部是纯函数，便于单测。
 *
 * ⛔ 为什么单独成模块而不是写在组件里：本仓的判据是"协议相关的取值转换必须有单测"，
 * 而组件的 setup 体量太大、依赖太多，塞进去的转换函数就没人测了。
 *
 * ============ 取值唯一出处：GB/T 28181-2022 附录 G 的 SDP `f` 字段（标准页 130）===
 *
 *  VideoFormat   1=MPEG-4 2=H.264 3=SVAC 4=3GP 5=H.265
 *  Resolution    1=QCIF  2=CIF   3=4CIF 4=D1  5=720P 6=1080P；其余一律用 `WxH`
 *  FrameRate     0~99
 *  BitRateType   1=CBR   2=VBR
 *  VideoBitRate  0~100000，单位 kb/s
 *
 * ⛔ 后端**原样存码值字符串**，人读串只在这里做。对账比的是"下发值 vs 回读值"，
 * 一旦某一侧被转成人读串，比的就是两套表示了。
 */

export type VideoParamReconcileStateName = "never_read" | "pending" | "read_ok" | "type_absent" | "mismatch" | "failed";

export interface VideoParamCodecItem {
  streamNumber: number;
  videoFormat: string;
  resolution: string;
  frameRate: string;
  bitRateType: string;
  videoBitRate?: string | null;
}

// ---- 码值 → 人读串（只用于展示）----
//
// ⭐ 三张表的**真源是字典**（`sys_dict`：video_format / video_resolution / bit_rate_type）。
// 开发阶段字典数据**直接补在开发库**，不再维护 `seeds/*.jsonl`（见技能 `uvp-dict-rollout`）。
// 这里导出的 `*_LABEL_FALLBACK` 只是**字典未加载 / 查不到时的兜底**，值域由 GB/T 28181 固定，
// 有单测逐条锁死（改这里必然红，必须显式改用例）。
//
// ⛔ 为什么做成"可注入的查表"而不是在本模块里直接读 store：
//    本模块是**纯函数模块**（见文件头：协议取值转换必须有单测）。一旦 import Vue/pinia，
//    这里的 30 多个用例就全得先起一个 pinia。所以字典由调用方（`useVideoParamDict`）
//    注入进来，纯函数只认一张表。
//
// ⛔ 字典只管"叫什么"，**不管"合不合法"** —— 值域校验（`isValidVideoFormat` 等）
//    仍然是写死的白名单，那校验的是"值合不合法"。

/** 字典 code。 */
export const DICT_CODE_VIDEO_FORMAT = "video_format";
/** 字典 code。 */
export const DICT_CODE_VIDEO_RESOLUTION = "video_resolution";
/** 字典 code。 */
export const DICT_CODE_BIT_RATE_TYPE = "bit_rate_type";

/** 展示用查表（`value` → 展示名）。`useDictLabelMap` 的产物即此形态。 */
export type VideoParamLabelTable = Readonly<Record<string, string>>;

/** `video_format` 兜底（GB/T 28181 值域，单测逐条锁定）。 */
export const VIDEO_FORMAT_LABEL_FALLBACK: VideoParamLabelTable = {
  "1": "MPEG-4",
  "2": "H.264",
  "3": "SVAC",
  "4": "3GP",
  "5": "H.265"
};

/** `video_resolution` 兜底（GB/T 28181 值域，单测逐条锁定）。 */
export const RESOLUTION_LABEL_FALLBACK: VideoParamLabelTable = {
  "1": "QCIF",
  "2": "CIF",
  "3": "4CIF",
  "4": "D1",
  "5": "720P",
  "6": "1080P"
};

/** `bit_rate_type` 兜底（GB/T 28181 值域，单测逐条锁定）。 */
export const BIT_RATE_TYPE_LABEL_FALLBACK: VideoParamLabelTable = {
  "1": "CBR",
  "2": "VBR"
};

export function videoFormatText(
  value: string | null | undefined,
  labels: VideoParamLabelTable = VIDEO_FORMAT_LABEL_FALLBACK
): string {
  const code = String(value ?? "").trim();
  if (!code) return "未上报";
  return labels[code] ?? `未知(${code})`;
}

/**
 * 分辨率：六个码值走映射，其余原样展示 `WxH`。
 * ⛔ 不认识的码值原样带出来而不是显示"未知" —— 设备回了什么就得让人看见什么，
 * 否则排障时无法区分"设备给了个怪值"与"我们没解析"。
 */
export function resolutionText(
  value: string | null | undefined,
  labels: VideoParamLabelTable = RESOLUTION_LABEL_FALLBACK
): string {
  const raw = String(value ?? "").trim();
  if (!raw) return "未上报";
  return labels[raw] ?? raw;
}

export function bitRateTypeText(
  value: string | null | undefined,
  labels: VideoParamLabelTable = BIT_RATE_TYPE_LABEL_FALLBACK
): string {
  const code = String(value ?? "").trim();
  if (!code) return "未上报";
  return labels[code] ?? `未知(${code})`;
}

// ---- 码值 → 归一化 token / 像素尺寸（⛔ 逻辑用，与字典文案无关）----
//
// ⭐ 这两张表是**给对账逻辑用的**，不是展示。此前它们是从 `videoFormatText` /
// `resolutionText` 的**输出文案**反推的（`normalizeCodecToken(videoFormatText(...))`、
// `VIDEO_RESOLUTION_TIERS[resolutionText(...)]`）—— 那等于把"字典里叫什么"变成了
// 逻辑输入：现场把 `2` 的名字从 `H.264` 改成别串，编码对比就会静默失效（显示"未读取"）。
// 故这里改成**按码值直接查**，与展示彻底解耦。

/** 附录 G `VideoFormat` 码值 → 归一化编码 token（与 ZLM 实测编码同尺子）。 */
export const VIDEO_FORMAT_CODEC_TOKENS: Readonly<Record<string, string>> = {
  "1": "MPEG4",
  "2": "H264",
  "3": "SVAC",
  "4": "3GP",
  "5": "H265"
};

/** 附录 G `Resolution` 六个码值 → 像素尺寸。容量容差比对用（见 `videoParamDiffs`）。 */
export const RESOLUTION_TIERS_BY_CODE: Readonly<Record<string, { width: number; height: number }>> = {
  "1": { width: 176, height: 144 },
  "2": { width: 352, height: 288 },
  "3": { width: 704, height: 576 },
  "4": { width: 720, height: 576 },
  "5": { width: 1280, height: 720 },
  "6": { width: 1920, height: 1080 }
};

/** `1920×1080` / `1920x1080` → `{1920, 1080}`；取不到两个数就返回 null。 */
export function pixelsOf(text: string | null | undefined): { width: number; height: number } | null {
  const nums = (String(text ?? "").match(/\d+/g) ?? []).map(Number).slice(0, 2);
  return nums.length === 2 && nums[0] > 0 && nums[1] > 0 ? { width: nums[0], height: nums[1] } : null;
}

/** 编码格式归一：`H.264` / `H264` / `h264` 是同一个东西；中文会被剥成空串 = 未知。 */
export function normalizeCodecToken(text: string | null | undefined): string {
  return String(text ?? "")
    .replace(/[^A-Za-z0-9]/g, "")
    .toUpperCase();
}

/**
 * 回读的编码格式码值 → 归一化 token（**只认码值**，不经过字典文案）。
 * ⛔ 返回空串表示"这不是附录 G 的码值"，调用方据此判 `null`（比不出来），而不是判"不一致"。
 */
export function videoFormatCodecToken(value: string | null | undefined): string {
  return VIDEO_FORMAT_CODEC_TOKENS[String(value ?? "").trim()] ?? "";
}

/**
 * 回读的分辨率 → 像素尺寸（**先认码值，再认 `WxH` 原文**）。
 * ⛔ 不查字典：字典改个名不该让"画面到底变没变"这个判断失效。
 */
export function resolutionPixels(value: string | null | undefined): { width: number; height: number } | null {
  const raw = String(value ?? "").trim();
  if (!raw) return null;
  return RESOLUTION_TIERS_BY_CODE[raw] ?? pixelsOf(raw);
}

export function frameRateText(value: string | null | undefined): string {
  const raw = String(value ?? "").trim();
  return raw ? `${raw} fps` : "未上报";
}

/**
 * 码率的展示文本。
 * ⛔ "缺席"与"值为 0"必须区分：VBR 下 `VideoBitRate` 本就该缺席（条件必选），
 * 而"设备报了个 0"是另一件事。两者都显示成 `0 kb/s` 会掩盖问题。
 */
export function videoBitRateText(value: string | null | undefined): string {
  const raw = String(value ?? "").trim();
  if (!raw) return "未提供";
  return `${raw} kb/s`;
}

// ---- 合法性与必填性（与后端 manscdp.ValidateVideoParamItems 同一条规则）----

/** 允许的六个分辨率码值，其余必须写成 `WxH`（小写 x）。 */
export function isValidResolutionCode(value: string | null | undefined): boolean {
  const raw = String(value ?? "").trim();
  if (!raw) return false;
  if (/^[1-6]$/.test(raw)) return true;
  return /^[1-9][0-9]*x[1-9][0-9]*$/.test(raw);
}

export function isValidFrameRate(value: string | null | undefined): boolean {
  const raw = String(value ?? "").trim();
  if (!/^[0-9]+$/.test(raw)) return false;
  const numeric = Number(raw);
  return numeric >= 0 && numeric <= 99;
}

export function isValidVideoBitRate(value: string | null | undefined): boolean {
  const raw = String(value ?? "").trim();
  if (!/^[0-9]+$/.test(raw)) return false;
  const numeric = Number(raw);
  return numeric >= 0 && numeric <= 100000;
}

export function isValidVideoFormat(value: string | null | undefined): boolean {
  return /^[1-5]$/.test(String(value ?? "").trim());
}

export function isValidBitRateType(value: string | null | undefined): boolean {
  return /^[1-2]$/.test(String(value ?? "").trim());
}

/**
 * `VideoBitRate` 是**条件必选**：仅 CBR(1) 时必填，VBR(2) 时**不该出现**。
 * 返回 true 表示这一格必须填值。
 */
export function videoBitRateRequired(bitRateType: string | null | undefined): boolean {
  return String(bitRateType ?? "").trim() === "1";
}

/**
 * 单条码流的完整校验。返回 `null` = 通过，否则是给人看的错误文案。
 *
 * ⛔ 逐条报错而不是布尔：面板上有 5 格，只说"不合法"等于没说。
 * ⛔ VBR 带码率**报错**而不是静默丢弃：丢弃会让用户以为填了生效了，
 * 而报文里其实没有这个元素（后端同样拒发）。
 */
export function validateVideoParamItem(item: VideoParamCodecItem): string | null {
  const streamLabel = `码流 ${item.streamNumber}`;
  if (!isValidVideoFormat(item.videoFormat)) {
    return `${streamLabel}：视频编码格式取值必须是 1-5（附录 G）`;
  }
  if (!isValidResolutionCode(item.resolution)) {
    return `${streamLabel}：分辨率必须是 1-6 的码值，或形如 1920x1080 的宽高`;
  }
  if (!isValidFrameRate(item.frameRate)) {
    return `${streamLabel}：帧率必须是 0-99 的整数`;
  }
  if (!isValidBitRateType(item.bitRateType)) {
    return `${streamLabel}：码率类型必须是 1（CBR）或 2（VBR）`;
  }
  const bitRate = String(item.videoBitRate ?? "").trim();
  if (videoBitRateRequired(item.bitRateType)) {
    if (!bitRate) return `${streamLabel}：固定码率(CBR)必须填码率`;
    if (!isValidVideoBitRate(bitRate)) return `${streamLabel}：码率必须是 0-100000 的整数（kb/s）`;
    return null;
  }
  if (bitRate) return `${streamLabel}：可变码率(VBR)不应填码率，该元素在报文里不出现`;
  return null;
}

/** 整批校验；返回第一条错误文案或 null。 */
export function validateVideoParamItems(items: VideoParamCodecItem[]): string | null {
  const seen = new Set<number>();
  for (const item of items) {
    if (!Number.isInteger(item.streamNumber) || item.streamNumber < 0) {
      return "码流编号必须是不小于 0 的整数";
    }
    if (seen.has(item.streamNumber)) {
      return `码流 ${item.streamNumber} 重复`;
    }
    seen.add(item.streamNumber);
    const failure = validateVideoParamItem(item);
    if (failure) return failure;
  }
  return null;
}

// ---- 目录属性 StreamNumberList ----

/**
 * 解析目录 `<Info>` 里的 `StreamNumberList`（形如 `0/1` 或 `0/1/2`）。
 *
 * ⛔ 这才是"面板按几段码流渲染"的出处，**不是 `VideoParamOpt`** ——
 * 后者只有 DownloadSpeed + Resolution 两个字段，跟码流数量无关。
 * 返回空数组 = 设备本次未上报，调用方应退化成"按已回读到的行渲染"。
 */
export function parseStreamNumberList(raw: string | null | undefined): number[] {
  const text = String(raw ?? "").trim();
  if (!text) return [];
  const out: number[] = [];
  const seen = new Set<number>();
  for (const part of text.split("/")) {
    const token = part.trim();
    if (!/^[0-9]+$/.test(token)) continue;
    const numeric = Number(token);
    if (seen.has(numeric)) continue;
    seen.add(numeric);
    out.push(numeric);
  }
  return out.sort((left, right) => left - right);
}

// ---- 四态文案（§十④）----

export interface ReconcileTextOptions {
  /**
   * 设备**当前生效**的协议版本（`gb_device.effective_version`，可能来自注册声明 /
   * 手工 override / 历史 / 默认 2016）。
   * ⛔ 只用来选提示措辞，**绝不参与任何门禁判断** ——
   * 登记成 2022 的设备也可能没实现（厂商没做），登记成 2016 的也可能支持（提前实现）。
   * 唯一可靠的判据是回读结果本身。
   */
  registeredVersion?: string | null;
}

/**
 * 四态（+ 过渡态）文案。
 *
 * ⛔ `mismatch` 的文案必须是「已接受但值未生效」而不是「失败」：
 * 典型来源是设备能力边界（下发 1080P、实际 720P），设备没做错。
 *
 * ⛔ "按 2016 版处理"用的是**当前生效版本**而不是"设备声明的版本"：
 * 这个值有 `default:2016`，设备从未声明时也会是 2016。说成"设备声明了 2016"
 * 会在排障时把人带偏（明明设备什么都没说）。
 */
export function videoParamReconcileText(state: VideoParamReconcileStateName, options: ReconcileTextOptions = {}): string {
  const registered = String(options.registeredVersion ?? "").trim();
  switch (state) {
    case "never_read":
      return "尚未读取设备视频参数";
    case "pending":
      return "正在读取设备视频参数…";
    case "read_ok":
      return "已读取设备当前配置";
    case "type_absent":
      return registered === "2016"
        ? "设备未返回此配置类型（平台按 2016 版处理，标准未定义该类型的应答行为）"
        : "设备未返回此配置类型（厂商未实现该类型）";
    case "mismatch":
      return "设备已接受命令，但值未生效";
    case "failed":
      return registered === "2016" ? "设备未响应（平台按 2016 版处理，可能不支持该配置类型）" : "读取失败：设备未响应或被拒绝";
    default:
      return "尚未读取设备视频参数";
  }
}

/**
 * 文案的严重程度，给 CSS 用。
 * ⛔ `mismatch` 是 `warn` 不是 `error`：设备已接受命令，只是值没照做。
 */
export function videoParamReconcileTone(state: VideoParamReconcileStateName): "idle" | "busy" | "ok" | "warn" | "error" {
  switch (state) {
    case "pending":
      return "busy";
    case "read_ok":
      return "ok";
    case "type_absent":
    case "mismatch":
      return "warn";
    case "failed":
      return "error";
    default:
      return "idle";
  }
}

/**
 * 表单为空时的占位文案。
 *
 * ⛔ 不能一律说"尚未读取设备视频参数"：`type_absent` 时列表**同样是空的**
 * （设备从没给出过数据，所以一行都没落库），但那时设备已经明确回过"我没有这个
 * 配置类型"。把能力问题说成操作问题，正是 §十④ 要避免的那类误判 ——
 * 现场会去反复点"读取设备参数"，而真正该做的是换设备/换配置方式。
 *
 * 占位文案与顶部四态文案必须来自同一套判据（都是 reconcile state），
 * 否则两处会各说一套。
 */
export function videoParamEmptyText(state: VideoParamReconcileStateName, options: { pending?: boolean } = {}): string {
  if (options.pending) return "正在读取设备视频参数…";
  switch (state) {
    case "pending":
      return "正在读取设备视频参数…";
    case "type_absent":
      return "设备未返回该配置类型的参数";
    case "failed":
      return "未取到设备视频参数";
    case "never_read":
      return "尚未读取设备视频参数";
    default:
      // read_ok / mismatch 却一行都没有：设备回了该类型但没给可用的码流条目。
      return "设备未返回该码流的参数";
  }
}

/**
 * 面板该不该让用户改 + 下发。
 *
 * 判据只有两条，都跟"有没有可改的东西"有关，**与设备支不支持无关**：
 *  1. 手里得有值（`hasRows`）—— 空表单上让用户猜数字没有意义
 *     （`DeviceConfig` 的应答没有回显，这是 §五 设计理由 1）；
 *  2. 不能有一批读取还在飞（`pending`），否则用户是在旧值上改、下发却是新的。
 *
 * ⛔ 刻意**不**按 `type_absent` / `mismatch` / `failed` 禁用：那是设备的结论，
 * 不是"用户不许试"的理由。被误登记成 2016 的真 2022 设备，不试一次就永远用不了。
 * 页面要做的只是把结论说清楚（文案走 `videoParamReconcileText`），而不是把按钮收走。
 */
export function canEditVideoParams(state: VideoParamReconcileStateName, hasRows: boolean): boolean {
  if (state === "pending") return false;
  return hasRows;
}
