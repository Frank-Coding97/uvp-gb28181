import { describe, expect, it } from "vitest";
import {
    buildTimelineSegments,
    buildTimelineTicks,
    centerTimelineViewport,
    createTimelineViewport,
    panTimelineViewport,
    positionToTime,
    timeToPercent,
    zoomTimelineViewport
} from "./timeline";

describe("record playback timeline", () => {
    const range = {
        startTime: "2026-08-02T08:00:00+08:00",
        endTime: "2026-08-02T12:00:00+08:00"
    };

    it("clips segments to the query range and preserves true boundaries", () => {
        const segments = buildTimelineSegments(range, [{
            recordKey: "a",
            startTime: "2026-08-02T07:30:00+08:00",
            endTime: "2026-08-02T09:00:00+08:00",
            type: "time"
        }]);
        expect(segments).toHaveLength(1);
        expect(segments[0]).toMatchObject({ key: "a", left: 0, width: 25, lane: 0 });
        expect(segments[0].trueStartTime).toContain("07:30:00");
    });

    it("uses stable lanes for overlaps and drops invalid segments", () => {
        const segments = buildTimelineSegments(range, [
            { recordKey: "a", startTime: "2026-08-02T08:10:00+08:00", endTime: "2026-08-02T09:10:00+08:00", type: "time" },
            { recordKey: "b", startTime: "2026-08-02T08:30:00+08:00", endTime: "2026-08-02T08:40:00+08:00", type: "alarm" },
            { recordKey: "bad", startTime: "2026-08-02T10:00:00+08:00", endTime: "2026-08-02T09:00:00+08:00", type: "manual" }
        ]);
        expect(segments.map(item => item.lane)).toEqual([0, 1]);
    });

    it("maps pointer and keyboard positions to bounded platform time", () => {
        expect(positionToTime(range, -10, 200)).toContain("08:00:00");
        expect(positionToTime(range, 100, 200)).toContain("10:00:00");
        expect(timeToPercent(range, "2026-08-02T13:00:00+08:00")).toBe(100);
    });

    it("zooms around the mouse anchor without moving its screen position", () => {
        const viewport = createTimelineViewport(range);
        const zoomed = zoomTimelineViewport(range, viewport, 4, "2026-08-02T10:00:00+08:00");
        expect(zoomed).toMatchObject({ zoom: 4 });
        expect(zoomed.startTime).toContain("09:30:00");
        expect(zoomed.endTime).toContain("10:30:00");
        expect(timeToPercent(zoomed, "2026-08-02T10:00:00+08:00")).toBe(50);
    });

    it("keeps both query endpoints reachable under a fixed center playhead", () => {
        const viewport = zoomTimelineViewport(range, createTimelineViewport(range), 4, "2026-08-02T10:00:00+08:00");
        const right = panTimelineViewport(range, viewport, 10 * 60 * 60 * 1000);
        const left = panTimelineViewport(range, viewport, -10 * 60 * 60 * 1000);
        expect(right.startTime).toContain("11:30:00");
        expect(right.endTime).toContain("12:30:00");
        expect(positionToTime(right, 0.5, 1)).toContain("12:00:00");
        expect(left.startTime).toContain("07:30:00");
        expect(left.endTime).toContain("08:30:00");
        expect(positionToTime(left, 0.5, 1)).toContain("08:00:00");
    });

    it("centers a requested time while preserving the selected zoom level", () => {
        const viewport = centerTimelineViewport(range, 4, "2026-08-02T08:00:00+08:00");
        expect(viewport).toMatchObject({ zoom: 4 });
        expect(viewport.startTime).toContain("07:30:00");
        expect(viewport.endTime).toContain("08:30:00");
        expect(positionToTime(viewport, 0.5, 1)).toContain("08:00:00");
    });

    it("builds readable major labels and denser minor ticks after zooming", () => {
        const fullTicks = buildTimelineTicks(createTimelineViewport(range));
        const zoomedTicks = buildTimelineTicks(zoomTimelineViewport(range, createTimelineViewport(range), 4, "2026-08-02T10:00:00+08:00"));
        expect(fullTicks.filter(tick => tick.major).map(tick => tick.label)).toEqual(["08:00", "09:00", "10:00", "11:00", "12:00"]);
        expect(zoomedTicks.filter(tick => tick.major).map(tick => tick.label)).toEqual(["09:30", "09:45", "10:00", "10:15", "10:30"]);
        expect(zoomedTicks.length).toBeGreaterThan(zoomedTicks.filter(tick => tick.major).length);
    });

    it("uses five-minute labels and one-minute minor ticks for a high-zoom window", () => {
        const viewport = {
            startTime: "2026-08-02T07:57:54+08:00",
            endTime: "2026-08-02T08:34:47+08:00"
        };
        const ticks = buildTimelineTicks(viewport);
        expect(ticks.filter(tick => tick.major).map(tick => tick.label)).toEqual([
            "08:00", "08:05", "08:10", "08:15", "08:20", "08:25", "08:30"
        ]);
        expect(Date.parse(ticks[1].time) - Date.parse(ticks[0].time)).toBe(60_000);
    });

    it("keeps local wall-clock labels when the query range has no timezone suffix", () => {
        const localRange = {
            startTime: "2026-08-02T00:00:00",
            endTime: "2026-08-02T04:00:00"
        };
        const labels = buildTimelineTicks(localRange).filter(tick => tick.major).map(tick => tick.label);
        expect(labels).toEqual(["00:00", "01:00", "02:00", "03:00", "04:00"]);
        expect(positionToTime(localRange, 100, 200)).toContain("02:00:00");
    });
});
