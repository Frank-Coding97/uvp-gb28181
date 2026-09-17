import { describe, expect, it } from "vitest";
import type { ProbeSnapshot } from "@/api/gb28181";
import { PROBE_OVERVIEW_BUCKETS, buildProbeOverview } from "./probeOverview";

type TimelineFrame = ProbeSnapshot["timeline"][number];

function frame(relativeTimeMs: number, trackType = "video"): TimelineFrame {
    return {
        sequence: 0,
        trackType,
        codec: "H264",
        keyFrame: false,
        configFrame: false,
        relativeTimeMs,
        frameSize: 1024,
    };
}

function snapshot(overrides: Partial<ProbeSnapshot> = {}): ProbeSnapshot {
    return {
        nodeId: 1,
        nodeName: "ZLM",
        completedAt: "2026-07-22T10:00:03Z",
        summary: { sampleDurationMs: 3000, frameCount: 0, totalBytes: 0, averageBitrateKbps: 0 },
        video: null,
        audio: null,
        timestamps: {
            videoDtsIntervalMeanMs: null,
            arrivalJitterMs: null,
            ptsDtsMaxMs: null,
            avArrivalSkewMaxMs: null,
        },
        timeline: [],
        health: { status: "ok", issues: [], thresholds: { largeArrivalGapMs: 500, keyFrameWindowMs: 3000 } },
        ...overrides,
    };
}

function summaryWith(sampleDurationMs: number, frameCount: number) {
    return { sampleDurationMs, frameCount, totalBytes: 0, averageBitrateKbps: 0 };
}

describe("buildProbeOverview", () => {
    it("没有采样结果时返回 null", () => {
        expect(buildProbeOverview(null)).toBeNull();
        expect(buildProbeOverview(undefined)).toBeNull();
        expect(buildProbeOverview(snapshot())).toBeNull();
    });

    it("按时间分桶,桶数与帧数解耦", () => {
        // 四千帧是 60 秒采样的量级,桶数必须仍然是固定值,否则侧栏会被 DOM 撑爆。
        const timeline = Array.from({ length: 4000 }, (_, i) => frame(i * 0.75));
        const overview = buildProbeOverview(snapshot({ timeline, summary: summaryWith(3000, 4000) }));

        expect(overview).not.toBeNull();
        expect(overview!.buckets).toHaveLength(PROBE_OVERVIEW_BUCKETS);
        expect(overview!.sampledFrames).toBe(4000);
        expect(overview!.buckets.reduce((total, bucket) => total + bucket.count, 0)).toBe(4000);
    });

    it("把大到达间隔标到它跨越的全部桶上", () => {
        const timeline = [frame(0), frame(40), frame(1200), frame(1240)];
        const overview = buildProbeOverview(snapshot({ timeline, summary: summaryWith(3000, 4) }));

        expect(overview!.stallCount).toBe(1);
        expect(overview!.maxGapMs).toBe(1160);
        // 40ms 与 1200ms 之间断了 1160ms,要从第 0 桶一路标到第 22 桶,不能只标端点。
        const stalled = overview!.buckets.filter(bucket => bucket.stalled);
        expect(stalled.length).toBeGreaterThan(1);
        expect(overview!.buckets[0].stalled).toBe(true);
    });

    it("断档阈值取后端 health.thresholds,前端不另定口径", () => {
        const timeline = [frame(0), frame(600)];

        expect(buildProbeOverview(snapshot({ timeline, summary: summaryWith(3000, 2) }))!.stallCount).toBe(1);

        const relaxed = snapshot({
            timeline,
            summary: summaryWith(3000, 2),
            health: { status: "ok", issues: [], thresholds: { largeArrivalGapMs: 1000, keyFrameWindowMs: 3000 } },
        });
        expect(buildProbeOverview(relaxed)!.stallCount).toBe(0);
    });

    it("透传后端裁剪标记与总帧数", () => {
        const timeline = [frame(0), frame(40)];
        const overview = buildProbeOverview(
            snapshot({ timeline, timelineTruncated: true, summary: summaryWith(60000, 4500) })
        );

        expect(overview!.truncated).toBe(true);
        expect(overview!.totalFrames).toBe(4500);
        expect(overview!.sampledFrames).toBe(2);
    });

    it("柱高按最密桶归一化", () => {
        const timeline = [frame(0), frame(10), frame(20), frame(3000)];
        const overview = buildProbeOverview(snapshot({ timeline, summary: summaryWith(3000, 4) }));

        expect(Math.max(...overview!.buckets.map(bucket => bucket.count))).toBe(3);
        expect(Math.max(...overview!.buckets.map(bucket => bucket.ratio))).toBe(1);
    });

    it("采样窗口为 0 时不除零", () => {
        const overview = buildProbeOverview(snapshot({ timeline: [frame(0)], summary: summaryWith(0, 1) }));

        expect(overview!.buckets).toHaveLength(PROBE_OVERVIEW_BUCKETS);
        expect(overview!.buckets.reduce((total, bucket) => total + bucket.count, 0)).toBe(1);
    });

    it("缺 timelineTruncated 字段时按未裁剪处理", () => {
        const overview = buildProbeOverview(snapshot({ timeline: [frame(0)], summary: summaryWith(3000, 1) }));

        expect(overview!.truncated).toBe(false);
    });
});
