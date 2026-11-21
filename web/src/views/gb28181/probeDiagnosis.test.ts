import { describe, expect, it } from "vitest";
import type { ProbeSnapshot } from "@/api/gb28181";
import {
  buildProbeDiagnosis,
  PROBE_ARRIVAL_LABEL_FALLBACK,
  PROBE_ISSUE_TITLE_FALLBACK,
  PROBE_STATUS_LABEL_FALLBACK
} from "./probeDiagnosis";

type ProbeHealth = ProbeSnapshot["health"];
type ProbeIssue = ProbeHealth["issues"][number];

function rhythm(videoDtsIntervalMeanMs: number | null, arrivalJitterMs: number | null) {
  return { videoDtsIntervalMeanMs, arrivalJitterMs, ptsDtsMaxMs: 0, avArrivalSkewMaxMs: 0 };
}

function snapshot(overrides: Partial<ProbeSnapshot> = {}, health: Partial<ProbeHealth> = {}): ProbeSnapshot {
  return {
    nodeId: 1,
    nodeName: "ZLM",
    completedAt: "2026-09-16T10:00:03Z",
    summary: { sampleDurationMs: 60000, frameCount: 4109, totalBytes: 877539, averageBitrateKbps: 117 },
    video: null,
    audio: null,
    timestamps: rhythm(40, 6),
    timeline: [],
    health: {
      status: "ok",
      issues: [],
      thresholds: { largeArrivalGapMs: 500, keyFrameWindowMs: 3000 },
      ...health
    },
    ...overrides
  };
}

function issue(code: string, extra: Partial<ProbeIssue> = {}): ProbeIssue {
  return { code, message: `后端原始 message: ${code}`, ...extra };
}

describe("buildProbeDiagnosis", () => {
  it("没有采样结果时返回 null", () => {
    expect(buildProbeDiagnosis(null)).toBeNull();
    expect(buildProbeDiagnosis(undefined)).toBeNull();
  });

  it("全部平稳时不列问题,定责结论是编码与到达节奏一致", () => {
    const diagnosis = buildProbeDiagnosis(snapshot());

    expect(diagnosis?.status).toBe("ok");
    expect(diagnosis?.statusLabel).toBe("平稳");
    expect(diagnosis?.issues).toEqual([]);
    expect(diagnosis?.verdict.level).toBe("ok");
    expect(diagnosis?.verdict.title).toContain("节奏一致");
  });

  it("多条问题全部保留 —— 侧栏当年只读 issues[0],后面的会被静默吞掉", () => {
    const diagnosis = buildProbeDiagnosis(
      snapshot({}, { status: "warning", issues: [issue("large_arrival_gap"), issue("missing_keyframe")] })
    );

    expect(diagnosis?.issues.map(item => item.code)).toEqual(["large_arrival_gap", "missing_keyframe"]);
    expect(diagnosis?.issues[1].title).toBe("采样窗口内没有关键帧");
  });

  it("把后端算好的实测与阈值对照带出来", () => {
    const diagnosis = buildProbeDiagnosis(
      snapshot({}, { status: "warning", issues: [issue("large_arrival_gap", { thresholdMs: 500, observedMs: 1160 })] })
    );

    expect(diagnosis?.issues[0].evidence).toBe("实测 1160 ms ／ 阈值 500 ms");
  });

  it("只下发了一项时按有的那项拼证据,两项都没有则为空", () => {
    const onlyObserved = buildProbeDiagnosis(
      snapshot({}, { status: "warning", issues: [issue("large_arrival_gap", { observedMs: 1160 })] })
    );
    const onlyThreshold = buildProbeDiagnosis(
      snapshot({}, { status: "warning", issues: [issue("large_arrival_gap", { thresholdMs: 500 })] })
    );
    const neither = buildProbeDiagnosis(snapshot({}, { status: "warning", issues: [issue("missing_keyframe")] }));

    expect(onlyObserved?.issues[0].evidence).toBe("实测 1160 ms");
    expect(onlyThreshold?.issues[0].evidence).toBe("阈值 500 ms");
    expect(neither?.issues[0].evidence).toBeNull();
  });

  it("时间戳倒退是设备侧硬证据,优先于链路判读", () => {
    const diagnosis = buildProbeDiagnosis(
      snapshot(
        {},
        {
          status: "warning",
          issues: [issue("large_arrival_gap"), issue("timestamp_regression")]
        }
      )
    );

    expect(diagnosis?.verdict.level).toBe("suspect");
    expect(diagnosis?.verdict.title).toContain("设备侧");
    expect(diagnosis?.issues[1].focus).toContain("设备编码器");
  });

  it("到达大间隔而源端时间戳连续,结论指向传输链路", () => {
    const diagnosis = buildProbeDiagnosis(snapshot({}, { status: "warning", issues: [issue("large_arrival_gap")] }));

    expect(diagnosis?.verdict.level).toBe("suspect");
    expect(diagnosis?.verdict.title).toContain("到达节奏异常");
    expect(diagnosis?.verdict.detail).toContain("传输链路");
    expect(diagnosis?.issues[0].focus).toContain("传输链路");
  });

  it("缺关键帧指向设备编码配置", () => {
    const diagnosis = buildProbeDiagnosis(snapshot({}, { status: "warning", issues: [issue("missing_keyframe")] }));

    expect(diagnosis?.verdict.title).toContain("关键帧");
    expect(diagnosis?.issues[0].focus).toContain("GOP");
  });

  it("一帧未到是 error 级,并压过其它判读", () => {
    const diagnosis = buildProbeDiagnosis(
      snapshot({}, { status: "error", issues: [issue("no_frames"), issue("large_arrival_gap")] })
    );

    expect(diagnosis?.statusLabel).toBe("异常");
    expect(diagnosis?.issues[0].severity).toBe("error");
    expect(diagnosis?.issues[1].severity).toBe("warning");
    expect(diagnosis?.verdict.title).toContain("一帧未到");
  });

  it("到达抖动按编码间隔无量纲分级", () => {
    const at = (jitterMs: number) => buildProbeDiagnosis(snapshot({ timestamps: rhythm(40, jitterMs) }))?.rhythm;

    // 40ms 编码间隔下的 10 / 24 / 50 ms 抖动 → 比值 0.25 / 0.6 / 1.25。
    expect(at(10)?.level).toBe("steady");
    expect(at(10)?.label).toBe("平稳");
    expect(at(24)?.level).toBe("slight");
    expect(at(24)?.label).toBe("轻微波动");
    expect(at(50)?.level).toBe("rough");
    expect(at(50)?.label).toBe("明显不匀");
  });

  it("缺任一侧指标时到达节奏降级为无法判断", () => {
    const noJitter = buildProbeDiagnosis(snapshot({ timestamps: rhythm(40, null) }))?.rhythm;
    const noEncode = buildProbeDiagnosis(snapshot({ timestamps: rhythm(null, 6) }))?.rhythm;

    expect(noJitter?.level).toBe("unknown");
    expect(noJitter?.ratio).toBeNull();
    expect(noJitter?.label).toBe("无法判断");
    expect(noEncode?.level).toBe("unknown");
  });

  it("后端没报警但抖动已超一个帧周期,仍给出提示而不是当成健康", () => {
    const diagnosis = buildProbeDiagnosis(snapshot({ timestamps: rhythm(40, 50) }));

    // 后端只报「单个间隔 > 500ms」,整体性劣化它报不出来,这里补上。
    expect(diagnosis?.status).toBe("ok");
    expect(diagnosis?.issues).toEqual([]);
    expect(diagnosis?.verdict.level).toBe("notice");
    expect(diagnosis?.verdict.title).toContain("未报警");
  });

  it("状态是风险却没带问题条目时,给出不指错方向的泛化结论", () => {
    const diagnosis = buildProbeDiagnosis(snapshot({}, { status: "warning", issues: [] }));

    expect(diagnosis?.verdict.level).toBe("notice");
    expect(diagnosis?.verdict.title).toContain("需关注");
    expect(diagnosis?.verdict.detail).toContain("没有附带具体问题条目");
  });

  it("快照缺健康字段时标记为未知,不假装健康", () => {
    const diagnosis = buildProbeDiagnosis({ health: undefined } as unknown as ProbeSnapshot);

    expect(diagnosis?.status).toBe("unknown");
    expect(diagnosis?.statusLabel).toBe("未知");
    expect(diagnosis?.verdict.level).toBe("notice");
    expect(diagnosis?.verdict.title).toContain("信息不完整");
  });
});

/** 全量 issue 码值 —— 两张表（标题 / 排查方向）必须都覆盖到。 */
const ALL_ISSUE_CODES = ["no_frames", "large_arrival_gap", "missing_keyframe", "timestamp_regression"];

/** 代码侧 `ISSUE_FOCUS` 未命中时给的那句通用兜底（见 `probeDiagnosis.ts`）。 */
const GENERIC_FOCUS = "建议结合逐帧曲线复核";

describe("buildProbeDiagnosis · 字典化（三张纯展示表）", () => {
  it("兜底表按后端值域写死 —— 状态四态 / 到达节奏四档 / 问题标题四个码", () => {
    expect(Object.keys(PROBE_STATUS_LABEL_FALLBACK).sort()).toEqual(["error", "ok", "unknown", "warning"]);
    expect(Object.keys(PROBE_ARRIVAL_LABEL_FALLBACK).sort()).toEqual(["rough", "slight", "steady", "unknown"]);
    expect(Object.keys(PROBE_ISSUE_TITLE_FALLBACK).sort()).toEqual([...ALL_ISSUE_CODES].sort());
  });

  it("⛔ 状态表与到达节奏表都有一档叫 unknown，但说法不同 —— 不许并成一张表", () => {
    expect(PROBE_STATUS_LABEL_FALLBACK.unknown).toBe("未知");
    expect(PROBE_ARRIVAL_LABEL_FALLBACK.unknown).toBe("无法判断");
  });

  it("字典给的名字覆盖兜底，且三张表各自独立生效", () => {
    const diagnosis = buildProbeDiagnosis(snapshot({}, { status: "error", issues: [issue("no_frames")] }), {
      status: { ...PROBE_STATUS_LABEL_FALLBACK, error: "严重异常" },
      arrival: { ...PROBE_ARRIVAL_LABEL_FALLBACK, steady: "很匀" },
      issueTitle: { ...PROBE_ISSUE_TITLE_FALLBACK, no_frames: "没有收到任何帧" }
    });

    expect(diagnosis?.statusLabel).toBe("严重异常");
    expect(diagnosis?.rhythm.label).toBe("很匀");
    expect(diagnosis?.issues[0].title).toBe("没有收到任何帧");
  });

  it("注入进来的表缺项时回落本模块兜底，而不是把英文码值甩到界面上", () => {
    const diagnosis = buildProbeDiagnosis(snapshot(), { status: {}, arrival: {}, issueTitle: {} });

    expect(diagnosis?.statusLabel).toBe("平稳");
    expect(diagnosis?.rhythm.label).toBe("平稳");
  });

  it("后端新增了不认识的码值时，标题回显后端原话、方向回落通用兜底", () => {
    const diagnosis = buildProbeDiagnosis(snapshot({}, { status: "warning", issues: [issue("brand_new_code")] }));

    expect(diagnosis?.issues[0].title).toBe("后端原始 message: brand_new_code");
    expect(diagnosis?.issues[0].focus).toBe(GENERIC_FOCUS);
  });

  it("⭐ 标题与排查方向是**两张表按同一组 code 拆的** —— 防止拆开后单边漂移", () => {
    const issues = ALL_ISSUE_CODES.map(code => issue(code));
    const diagnosis = buildProbeDiagnosis(snapshot({}, { status: "warning", issues }));

    expect(diagnosis?.issues).toHaveLength(ALL_ISSUE_CODES.length);
    for (const item of diagnosis?.issues ?? []) {
      // 标题取到了兜底表里的专属文案（不是后端 message）
      expect(item.title).not.toContain("后端原始 message");
      // 方向取到了代码侧表里的专属文案（不是那句通用兜底）
      expect(item.focus).not.toBe(GENERIC_FOCUS);
    }
  });
});
