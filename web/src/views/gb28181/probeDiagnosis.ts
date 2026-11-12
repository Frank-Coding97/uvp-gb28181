import type { ProbeSnapshot } from "@/api/gb28181";

/**
 * 逐帧健康检测的「诊断结论层」。
 *
 * 后端 Analyze() 其实已经把结论算完了:health.status 加 health.issues,每条还带
 * thresholdMs 与 observedMs。但侧栏只用了 issues[0] 且塞在 title 提示里 —— 一次采样
 * 同时报「到达大间隔 + 缺关键帧」时,后面的条目会被静默吞掉,后端算好的阈值对照也
 * 一条都没用上。这里把整份 health 摊平成可渲染的视图模型,并补两层后端没有的东西:
 *
 * 1. 每条 issue 的「该往哪儿查」——后端 message 是给日志读的,不指向排查方向。
 * 2. 节奏对照与定责:后端把 DTS(源端编码节奏)和 RecvStamp(链路到达节奏)分开算了,
 *    却没做对比。两者一对照,才能把「设备侧」和「传输链路」分开 —— 这是其他任何
 *    单项指标(在线状态、码率、FPS 均值)都做不到的。
 */

export type ProbeDiagnosisStatus = "ok" | "warning" | "error" | "unknown";

export type ProbeArrivalLevel = "steady" | "slight" | "rough" | "unknown";

export type ProbeIssueView = {
  code: string;
  title: string;
  /** 后端原始 message,作为标题的补充说明。 */
  message: string;
  /** 「实测 1160 ms ／ 阈值 500 ms」;两端都缺时为 null。 */
  evidence: string | null;
  /** 该问题的排查方向。 */
  focus: string;
  /** no_frames 直接置 error,其余由 warn() 产生,都是 warning。 */
  severity: "warning" | "error";
};

export type ProbeRhythmView = {
  /** 编码节奏:源端 DTS 间隔均值,毫秒。 */
  encodeMs: number | null;
  /** 到达节奏:到达间隔标准差,毫秒。 */
  arrivalMs: number | null;
  /** 到达抖动 ÷ 编码间隔;两侧都有正值时才成立。 */
  ratio: number | null;
  level: ProbeArrivalLevel;
  label: string;
};

export type ProbeVerdict = {
  level: "ok" | "notice" | "suspect";
  title: string;
  detail: string;
};

export type ProbeDiagnosis = {
  status: ProbeDiagnosisStatus;
  statusLabel: string;
  issues: ProbeIssueView[];
  rhythm: ProbeRhythmView;
  verdict: ProbeVerdict;
};

/**
 * 后端 issue.code → 该问题的**排查方向**。
 *
 * ⛔ 这张表**刻意不进字典**（与标题分开看待）：一条 issue 在后端是「一个码 + 两条文案」，
 * 而 `sys_dict_item` 只有 `id/name/value/status/dict_id` 五列、**没有第二文案列** ⇒
 * 硬塞的话只能带走标题、把排查方向留在代码里。既然注定要拆，就按性质拆：
 * - **标题**（"这个码叫什么"）→ 字典 `probe_issue_code`，运维可改；
 * - **排查方向**（一整句处置建议）→ 留代码，与 §3.4「长句状态不并入字典」同口径。
 * ⚠️ 两张表的 key 必须同步 —— `probeDiagnosis.test.ts` 有一条用例拿着全量码值
 *    同时断言"四个码都有专属标题、也都有专属方向"，防止拆开后单边漂移。
 *
 * 用 Map 而不是 Record<string, …>:后者是索引签名,开了 noUncheckedIndexedAccess 时
 * 取值类型会带 undefined,Map.get 则天然是 `| undefined`,两种情况都不用额外断言。
 */
const ISSUE_FOCUS = new Map<string, string>([
  ["no_frames", "先确认设备在推流、平台已收到该路流"],
  ["large_arrival_gap", "指向传输链路:带宽不足或链路抖动"],
  ["missing_keyframe", "指向设备编码配置:GOP 过长或未发 IDR"],
  ["timestamp_regression", "指向设备编码器:出帧时间戳错乱"]
]);

/* -------------------------------------------------------------------------- *
 * 字典化（纯展示）
 *
 * 本文件三处文案都由 `sys_dict` 驱动，但**都不进「前端只读」名单**：它们只影响
 * 弹窗上怎么说话，既不参与判定、也不下发设备（判定走的是 `issue.code` 本身）。
 * 注入层见 `useProbeDiagnosisDict.ts` —— ⛔ 本模块不 import vue/pinia，
 * 只认"传进来的一张表"，默认值即下面的兜底表。
 * -------------------------------------------------------------------------- */

export const DICT_CODE_PROBE_STATUS = "probe_diagnosis_status";
export const DICT_CODE_PROBE_ARRIVAL = "probe_arrival_level";
export const DICT_CODE_PROBE_ISSUE_CODE = "probe_issue_code";

/** 兜底口径 —— 按后端 `health.status` 值域写死，与字典项逐条对齐。 */
export const PROBE_STATUS_LABEL_FALLBACK: Readonly<Record<string, string>> = {
  ok: "平稳",
  warning: "需关注",
  error: "异常",
  unknown: "未知"
};

/** 兜底口径 —— 按 `ProbeArrivalLevel` 值域写死。⚠️ 与上面同值 `unknown` 但说法不同，别并。 */
export const PROBE_ARRIVAL_LABEL_FALLBACK: Readonly<Record<string, string>> = {
  steady: "平稳",
  slight: "轻微波动",
  rough: "明显不匀",
  unknown: "无法判断"
};

/**
 * 兜底口径 —— 按 `ISSUE_FOCUS` 的 code 写死（两张表 key 必须一一对应）。
 * ⛔ 码值未命中时**回显后端 `message`**（见 `toIssueView`），不吃兜底 ——
 *    后端加了新码，排障要看见它自己的原话，而不是被吞成一句编造的标题。
 */
export const PROBE_ISSUE_TITLE_FALLBACK: Readonly<Record<string, string>> = {
  no_frames: "采样期间未收到媒体帧",
  large_arrival_gap: "帧到达出现连续大间隔",
  missing_keyframe: "采样窗口内没有关键帧",
  timestamp_regression: "媒体时间戳倒退"
};

/** 三张表的打包形态 —— 注入层给一份，纯函数侧默认用兜底。 */
export type ProbeDiagnosisLabels = {
  status: Readonly<Record<string, string>>;
  arrival: Readonly<Record<string, string>>;
  issueTitle: Readonly<Record<string, string>>;
};

const DEFAULT_LABELS: ProbeDiagnosisLabels = {
  status: PROBE_STATUS_LABEL_FALLBACK,
  arrival: PROBE_ARRIVAL_LABEL_FALLBACK,
  issueTitle: PROBE_ISSUE_TITLE_FALLBACK
};

/** 查表：调用方给的表优先，缺项回落本模块兜底表，再缺才回显原码值（排障要看得见）。 */
function pick(labels: Readonly<Record<string, string>>, fallback: Readonly<Record<string, string>>, key: string): string {
  return labels[key] || fallback[key] || key;
}

/**
 * 到达节奏的定性分级阈值。
 *
 * ⚠️ 这两个值是**启发式**,只用来给弹窗里「到达节奏匀不匀」一个说法,**不参与后端告警**
 * —— 后端只报 large_arrival_gap(单个到达间隔 > 500ms),拿不到「每个间隔都偏大但都不
 * 越线」这种整体性劣化。这里用「抖动 ÷ 编码间隔」做无量纲化:抖动达到一个帧周期
 * (比值 1),说明到达节奏已经维持不住帧序。
 */
const STEADY_RATIO = 0.5;
const ROUGH_RATIO = 1;

function normalizeStatus(raw: string | undefined): ProbeDiagnosisStatus {
  if (raw === "ok" || raw === "warning" || raw === "error") return raw;
  return "unknown";
}

/** 拼「实测 vs 阈值」。后端这两项是按问题类型选择性下发的,缺哪个就只说哪个。 */
function issueEvidence(thresholdMs?: number, observedMs?: number): string | null {
  if (observedMs == null && thresholdMs == null) return null;
  if (observedMs == null) return `阈值 ${thresholdMs} ms`;
  if (thresholdMs == null) return `实测 ${observedMs} ms`;
  return `实测 ${observedMs} ms ／ 阈值 ${thresholdMs} ms`;
}

function toIssueView(
  issue: { code: string; message: string; thresholdMs?: number; observedMs?: number },
  labels: ProbeDiagnosisLabels
): ProbeIssueView {
  return {
    code: issue.code,
    // 字典与兜底只回答「认识的码叫什么」；**不认识的码回显后端原话**，不被吞成编造标题。
    title: labels.issueTitle[issue.code] || PROBE_ISSUE_TITLE_FALLBACK[issue.code] || issue.message || issue.code,
    message: issue.message,
    evidence: issueEvidence(issue.thresholdMs, issue.observedMs),
    focus: ISSUE_FOCUS.get(issue.code) ?? "建议结合逐帧曲线复核",
    // ⛔ severity 取的是 code 本身，不是文案 ⇒ 字典改名不影响它，别改成按标题判。
    severity: issue.code === "no_frames" ? "error" : "warning"
  };
}

function buildRhythm(snapshot: ProbeSnapshot, labels: ProbeDiagnosisLabels): ProbeRhythmView {
  const encodeMs = snapshot.timestamps?.videoDtsIntervalMeanMs ?? null;
  const arrivalMs = snapshot.timestamps?.arrivalJitterMs ?? null;
  const ratio = encodeMs != null && encodeMs > 0 && arrivalMs != null ? arrivalMs / encodeMs : null;
  const level: ProbeArrivalLevel =
    ratio == null ? "unknown" : ratio >= ROUGH_RATIO ? "rough" : ratio >= STEADY_RATIO ? "slight" : "steady";
  return { encodeMs, arrivalMs, ratio, level, label: pick(labels.arrival, PROBE_ARRIVAL_LABEL_FALLBACK, level) };
}

/**
 * 定责结论。按「先设备侧硬证据、再链路侧、最后泛化」的顺序判 ——
 * timestamp_regression 是编码器自己出帧错乱,链路不会造成这一点,所以优先级最高。
 */
function buildVerdict(status: ProbeDiagnosisStatus, codes: Set<string>, rhythm: ProbeRhythmView): ProbeVerdict {
  if (codes.has("no_frames")) {
    return {
      level: "suspect",
      title: "采样期间一帧未到",
      detail: "先确认设备是否在推流、平台是否已收到该路流,此时还谈不上链路质量。"
    };
  }
  if (codes.has("timestamp_regression")) {
    return {
      level: "suspect",
      title: "源端时间戳倒退,指向设备侧",
      detail: "编码节奏本身已经错乱(媒体 DTS 倒退),传输链路不会造成这一点,应查摄像头编码器。"
    };
  }
  if (codes.has("large_arrival_gap")) {
    return {
      level: "suspect",
      title: "编码节奏正常、到达节奏异常",
      detail: "帧到达出现大间隔而源端时间戳连续 —— 指向摄像头到平台之间的传输链路或带宽。"
    };
  }
  if (codes.has("missing_keyframe")) {
    return {
      level: "notice",
      title: "采样窗口内没有关键帧",
      detail: "缺关键帧(IDR)会让播放端黑屏或长时间不出画,查设备编码的 GOP 与关键帧间隔配置。"
    };
  }
  if (rhythm.level === "rough") {
    return {
      level: "notice",
      title: "后端未报警,但到达节奏已不匀",
      detail: "到达抖动已达到一个帧周期,链路处在临界状态,建议结合带宽占用复核。"
    };
  }
  if (status === "ok") {
    return {
      level: "ok",
      title: "编码节奏与到达节奏一致",
      detail: "源端出帧与平台收流的节奏吻合,未发现异常帧间隔。"
    };
  }
  if (status === "warning") {
    return {
      level: "notice",
      title: "检测标记为需关注",
      detail: "后端给出 warning 却没有附带具体问题条目,建议复核该路流的带宽与设备状态。"
    };
  }
  if (status === "error") {
    return {
      level: "suspect",
      title: "检测标记为异常",
      detail: "后端给出 error 却没有附带具体问题条目,建议复核该路流是否可用。"
    };
  }
  return {
    level: "notice",
    title: "诊断信息不完整",
    detail: "这份快照没有带健康状态字段,只能参考下面的逐帧曲线自行判断。"
  };
}

/**
 * 把一份健康快照摊平成可渲染视图模型。
 * `labels` 由注入层从字典取（见 `useProbeDiagnosisDict.ts`），省略时用本模块兜底表。
 */
export function buildProbeDiagnosis(
  snapshot: ProbeSnapshot | null | undefined,
  labels: ProbeDiagnosisLabels = DEFAULT_LABELS
): ProbeDiagnosis | null {
  if (!snapshot) return null;
  const status = normalizeStatus(snapshot.health?.status);
  const rawIssues = snapshot.health?.issues ?? [];
  const rhythm = buildRhythm(snapshot, labels);
  return {
    status,
    statusLabel: pick(labels.status, PROBE_STATUS_LABEL_FALLBACK, status),
    issues: rawIssues.map(issue => toIssueView(issue, labels)),
    rhythm,
    verdict: buildVerdict(status, new Set(rawIssues.map(issue => issue.code)), rhythm)
  };
}
