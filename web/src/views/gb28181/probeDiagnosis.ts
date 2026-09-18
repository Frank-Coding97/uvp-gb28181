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
 * 后端 issue.code → 人话标题与该问题的排查方向。
 *
 * 用 Map 而不是 Record<string, …>:后者是索引签名,开了 noUncheckedIndexedAccess 时
 * 取值类型会带 undefined,Map.get 则天然是 `| undefined`,两种情况都不用额外断言。
 */
const ISSUE_META = new Map<string, { title: string; focus: string }>([
    ["no_frames", { title: "采样期间未收到媒体帧", focus: "先确认设备在推流、平台已收到该路流" }],
    ["large_arrival_gap", { title: "帧到达出现连续大间隔", focus: "指向传输链路:带宽不足或链路抖动" }],
    ["missing_keyframe", { title: "采样窗口内没有关键帧", focus: "指向设备编码配置:GOP 过长或未发 IDR" }],
    ["timestamp_regression", { title: "媒体时间戳倒退", focus: "指向设备编码器:出帧时间戳错乱" }],
]);

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

const STATUS_LABEL: Record<ProbeDiagnosisStatus, string> = {
    ok: "平稳",
    warning: "需关注",
    error: "异常",
    unknown: "未知",
};

const ARRIVAL_LABEL: Record<ProbeArrivalLevel, string> = {
    steady: "平稳",
    slight: "轻微波动",
    rough: "明显不匀",
    unknown: "无法判断",
};

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

function toIssueView(issue: {
    code: string;
    message: string;
    thresholdMs?: number;
    observedMs?: number;
}): ProbeIssueView {
    const meta = ISSUE_META.get(issue.code);
    return {
        code: issue.code,
        title: meta?.title ?? issue.message,
        message: issue.message,
        evidence: issueEvidence(issue.thresholdMs, issue.observedMs),
        focus: meta?.focus ?? "建议结合逐帧曲线复核",
        severity: issue.code === "no_frames" ? "error" : "warning",
    };
}

function buildRhythm(snapshot: ProbeSnapshot): ProbeRhythmView {
    const encodeMs = snapshot.timestamps?.videoDtsIntervalMeanMs ?? null;
    const arrivalMs = snapshot.timestamps?.arrivalJitterMs ?? null;
    const ratio = encodeMs != null && encodeMs > 0 && arrivalMs != null ? arrivalMs / encodeMs : null;
    const level: ProbeArrivalLevel =
        ratio == null ? "unknown" : ratio >= ROUGH_RATIO ? "rough" : ratio >= STEADY_RATIO ? "slight" : "steady";
    return { encodeMs, arrivalMs, ratio, level, label: ARRIVAL_LABEL[level] };
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
            detail: "先确认设备是否在推流、平台是否已收到该路流,此时还谈不上链路质量。",
        };
    }
    if (codes.has("timestamp_regression")) {
        return {
            level: "suspect",
            title: "源端时间戳倒退,指向设备侧",
            detail: "编码节奏本身已经错乱(媒体 DTS 倒退),传输链路不会造成这一点,应查摄像头编码器。",
        };
    }
    if (codes.has("large_arrival_gap")) {
        return {
            level: "suspect",
            title: "编码节奏正常、到达节奏异常",
            detail: "帧到达出现大间隔而源端时间戳连续 —— 指向摄像头到平台之间的传输链路或带宽。",
        };
    }
    if (codes.has("missing_keyframe")) {
        return {
            level: "notice",
            title: "采样窗口内没有关键帧",
            detail: "缺关键帧(IDR)会让播放端黑屏或长时间不出画,查设备编码的 GOP 与关键帧间隔配置。",
        };
    }
    if (rhythm.level === "rough") {
        return {
            level: "notice",
            title: "后端未报警,但到达节奏已不匀",
            detail: "到达抖动已达到一个帧周期,链路处在临界状态,建议结合带宽占用复核。",
        };
    }
    if (status === "ok") {
        return {
            level: "ok",
            title: "编码节奏与到达节奏一致",
            detail: "源端出帧与平台收流的节奏吻合,未发现异常帧间隔。",
        };
    }
    if (status === "warning") {
        return {
            level: "notice",
            title: "检测标记为需关注",
            detail: "后端给出 warning 却没有附带具体问题条目,建议复核该路流的带宽与设备状态。",
        };
    }
    if (status === "error") {
        return {
            level: "suspect",
            title: "检测标记为异常",
            detail: "后端给出 error 却没有附带具体问题条目,建议复核该路流是否可用。",
        };
    }
    return {
        level: "notice",
        title: "诊断信息不完整",
        detail: "这份快照没有带健康状态字段,只能参考下面的逐帧曲线自行判断。",
    };
}

export function buildProbeDiagnosis(snapshot: ProbeSnapshot | null | undefined): ProbeDiagnosis | null {
    if (!snapshot) return null;
    const status = normalizeStatus(snapshot.health?.status);
    const rawIssues = snapshot.health?.issues ?? [];
    const rhythm = buildRhythm(snapshot);
    return {
        status,
        statusLabel: STATUS_LABEL[status],
        issues: rawIssues.map(toIssueView),
        rhythm,
        verdict: buildVerdict(status, new Set(rawIssues.map(issue => issue.code)), rhythm),
    };
}
