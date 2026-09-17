import type { ProbeSnapshot } from "@/api/gb28181";

/**
 * 概览条按时间切成的桶数。
 *
 * 侧栏那一栏只有约 250px 可用宽,而一次 60 秒采样能到四千多帧 —— 逐帧渲染既放不下
 * (每根柱子不足 0.1px)也会把 DOM 撑爆。所以概览层按时间分桶聚合,DOM 节点数固定
 * 等于桶数,与采样帧数解耦;要看逐帧明细时再进详情弹窗。
 */
export const PROBE_OVERVIEW_BUCKETS = 56;

export type ProbeOverviewBucket = {
    /** 该桶内落到的帧数。 */
    count: number;
    /** 占最密桶的比例,0~1,用于决定柱高。 */
    ratio: number;
    /** 该桶是否落在大到达间隔区间内。 */
    stalled: boolean;
    /** 落在断档区间时的最大间隔毫秒数。 */
    maxGapMs: number;
};

export type ProbeOverview = {
    buckets: ProbeOverviewBucket[];
    /** 参与聚合的帧条数,等于 timeline 长度。 */
    sampledFrames: number;
    /** 后端采到的帧总数,可能大于 sampledFrames(见 truncated)。 */
    totalFrames: number;
    /** timeline 是否被后端裁剪过。 */
    truncated: boolean;
    /** 采样窗口时长,毫秒。 */
    durationMs: number;
    /** 大到达间隔出现的次数。 */
    stallCount: number;
    /** 最长的到达间隔,毫秒。 */
    maxGapMs: number;
};

/**
 * 把探针快照里的逐帧明细聚合成概览桶。
 *
 * 断档判定复用后端 health.thresholds.largeArrivalGapMs,前端不再自己定阈值 ——
 * 否则同一段流在图上标红、后端却没报警,或者反过来。
 * 采样不足(无帧)时返回 null,由调用方决定空态文案。
 */
export function buildProbeOverview(snapshot: ProbeSnapshot | null | undefined): ProbeOverview | null {
    const frames = snapshot?.timeline ?? [];
    if (!frames.length) return null;

    const durationMs = Math.max(0, snapshot?.summary?.sampleDurationMs ?? 0);
    // 只有一帧时采样窗口为 0,退化成单桶,避免除零。
    const bucketMs = (durationMs > 0 ? durationMs : 1) / PROBE_OVERVIEW_BUCKETS;
    const gapThreshold = snapshot?.health?.thresholds?.largeArrivalGapMs ?? 500;

    const buckets: ProbeOverviewBucket[] = Array.from({ length: PROBE_OVERVIEW_BUCKETS }, () => ({
        count: 0,
        ratio: 0,
        stalled: false,
        maxGapMs: 0,
    }));

    const bucketIndex = (relativeMs: number) =>
        Math.min(PROBE_OVERVIEW_BUCKETS - 1, Math.max(0, Math.floor(relativeMs / bucketMs)));

    for (const frame of frames) {
        buckets[bucketIndex(frame.relativeTimeMs)].count += 1;
    }

    let stallCount = 0;
    let maxGapMs = 0;
    for (let i = 1; i < frames.length; i++) {
        const gap = frames[i].relativeTimeMs - frames[i - 1].relativeTimeMs;
        if (gap <= gapThreshold) continue;
        stallCount += 1;
        if (gap > maxGapMs) maxGapMs = gap;
        // 断档跨越的所有桶都要标脏:60 秒采样下一个桶就有一秒,只标端点会让断档看起来很短。
        const from = bucketIndex(frames[i - 1].relativeTimeMs);
        const to = bucketIndex(frames[i].relativeTimeMs);
        for (let k = from; k <= to; k++) {
            buckets[k].stalled = true;
            if (gap > buckets[k].maxGapMs) buckets[k].maxGapMs = gap;
        }
    }

    const peak = buckets.reduce((max, bucket) => (bucket.count > max ? bucket.count : max), 0);
    for (const bucket of buckets) {
        bucket.ratio = peak > 0 ? bucket.count / peak : 0;
    }

    return {
        buckets,
        sampledFrames: frames.length,
        totalFrames: snapshot?.summary?.frameCount ?? frames.length,
        truncated: snapshot?.timelineTruncated === true,
        durationMs,
        stallCount,
        maxGapMs,
    };
}

/**
 * 概览条里单个桶的柱高百分比。
 * 无帧的桶只留一条底线 —— 空白本身就是「这段时间没有帧到达」的表达。
 */
export function probeBucketHeight(bucket: ProbeOverviewBucket): number {
    return bucket.count === 0 ? 4 : Math.max(10, Math.round(bucket.ratio * 100));
}
