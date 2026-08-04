export interface TimelineRange {
    startTime: string;
    endTime: string;
}

export interface TimelineViewport extends TimelineRange {
    zoom: number;
}

export interface TimelineTick {
    time: string;
    percent: number;
    major: boolean;
    label: string;
}

export const timelineZoomLevels = [1, 2, 4, 8, 16, 32] as const;

export interface TimelineRecord {
    recordKey: string;
    startTime: string | null;
    endTime: string | null;
    type: string | null;
}

export interface TimelineSegment {
    key: string;
    left: number;
    width: number;
    lane: number;
    type: string;
    trueStartTime: string;
    trueEndTime: string;
}

function rangeMs(range: TimelineRange) {
    return { start: Date.parse(range.startTime), end: Date.parse(range.endTime) };
}

function offsetParts(reference: string) {
    const match = reference.match(/([+-])(\d{2}):(\d{2})$/);
    if (!match) return null;
    const minutes = (Number(match[2]) * 60 + Number(match[3])) * (match[1] === "+" ? 1 : -1);
    return { sign: match[1], hours: match[2], minutesText: match[3], minutes };
}

function offsetMinutes(reference: string): number {
    const explicit = offsetParts(reference);
    if (explicit) return explicit.minutes;
    if (/Z$/i.test(reference)) return 0;
    const value = new Date(reference);
    return Number.isFinite(value.getTime()) ? -value.getTimezoneOffset() : 0;
}

function formatAtOffset(value: number, reference: string): string {
    const offset = offsetParts(reference);
    if (!offset) {
        if (/Z$/i.test(reference)) return new Date(value).toISOString();
        const localOffset = -new Date(value).getTimezoneOffset();
        return new Date(value + localOffset * 60_000).toISOString().replace("Z", "");
    }
    const local = new Date(value + offset.minutes * 60_000).toISOString().replace("Z", "");
    return `${local}${offset.sign}${offset.hours}:${offset.minutesText}`;
}

function clampZoom(zoom: number) {
    return Math.min(timelineZoomLevels[timelineZoomLevels.length - 1], Math.max(1, zoom));
}

export function createTimelineViewport(range: TimelineRange): TimelineViewport {
    return { ...range, zoom: 1 };
}

export function centerTimelineViewport(
    range: TimelineRange,
    requestedZoom: number,
    centerTime: string
): TimelineViewport {
    const full = rangeMs(range);
    if (!Number.isFinite(full.start) || !Number.isFinite(full.end) || full.end <= full.start) return createTimelineViewport(range);
    const zoom = clampZoom(requestedZoom);
    const duration = (full.end - full.start) / zoom;
    const requestedCenter = Date.parse(centerTime);
    const center = Math.min(full.end, Math.max(full.start, Number.isFinite(requestedCenter) ? requestedCenter : (full.start + full.end) / 2));
    return {
        startTime: formatAtOffset(Math.round(center - duration / 2), range.startTime),
        endTime: formatAtOffset(Math.round(center + duration / 2), range.startTime),
        zoom
    };
}

export function zoomTimelineViewport(
    range: TimelineRange,
    _viewport: TimelineViewport,
    requestedZoom: number,
    anchorTime: string
): TimelineViewport {
    return centerTimelineViewport(range, requestedZoom, anchorTime);
}

export function panTimelineViewport(range: TimelineRange, viewport: TimelineViewport, deltaMs: number): TimelineViewport {
    const full = rangeMs(range);
    const visible = rangeMs(viewport);
    if (full.end <= full.start || visible.end <= visible.start) return viewport;
    const duration = visible.end - visible.start;
    const center = visible.start + duration / 2 + deltaMs;
    return centerTimelineViewport(range, viewport.zoom, formatAtOffset(center, range.startTime));
}

function tickSteps(durationMs: number) {
    const minute = 60_000;
    const hour = 60 * minute;
    if (durationMs > 8 * hour) return { major: 2 * hour, minor: 30 * minute };
    if (durationMs > 2 * hour) return { major: hour, minor: 15 * minute };
    if (durationMs > hour) return { major: 30 * minute, minor: 10 * minute };
    if (durationMs > 45 * minute) return { major: 15 * minute, minor: 5 * minute };
    if (durationMs > 15 * minute) return { major: 5 * minute, minor: minute };
    return { major: 2 * minute, minor: 30_000 };
}

export function buildTimelineTicks(viewport: TimelineRange): TimelineTick[] {
    const visible = rangeMs(viewport);
    if (visible.end <= visible.start) return [];
    const steps = tickSteps(visible.end - visible.start);
    const offset = offsetMinutes(viewport.startTime);
    const offsetMs = offset * 60_000;
    const first = Math.ceil((visible.start + offsetMs) / steps.minor) * steps.minor - offsetMs;
    const ticks: TimelineTick[] = [];
    for (let time = first; time <= visible.end; time += steps.minor) {
        const major = Math.abs((time + offsetMs) % steps.major) < 1;
        const formatted = formatAtOffset(time, viewport.startTime);
        ticks.push({
            time: formatted,
            percent: ((time - visible.start) / (visible.end - visible.start)) * 100,
            major,
            label: major ? formatted.slice(11, 16) : ""
        });
    }
    return ticks;
}

export function buildTimelineSegments(range: TimelineRange, records: TimelineRecord[]): TimelineSegment[] {
    const query = rangeMs(range);
    if (!Number.isFinite(query.start) || !Number.isFinite(query.end) || query.end <= query.start) return [];
    const lanes: number[] = [];
    return records
        .map(record => ({ record, start: Date.parse(record.startTime || ""), end: Date.parse(record.endTime || "") }))
        .filter(item => Number.isFinite(item.start) && Number.isFinite(item.end) && item.end > item.start && item.end > query.start && item.start < query.end)
        .sort((a, b) => a.start - b.start || a.end - b.end || a.record.recordKey.localeCompare(b.record.recordKey))
        .map(item => {
            const clippedStart = Math.max(query.start, item.start);
            const clippedEnd = Math.min(query.end, item.end);
            let lane = lanes.findIndex(laneEnd => laneEnd <= clippedStart);
            if (lane < 0) lane = lanes.length;
            lanes[lane] = clippedEnd;
            return {
                key: item.record.recordKey,
                left: ((clippedStart - query.start) / (query.end - query.start)) * 100,
                width: ((clippedEnd - clippedStart) / (query.end - query.start)) * 100,
                lane,
                type: ["time", "alarm", "manual"].includes(item.record.type || "") ? item.record.type! : "unknown",
                trueStartTime: item.record.startTime!,
                trueEndTime: item.record.endTime!
            };
        });
}

export function positionToTime(range: TimelineRange, position: number, width: number): string {
    const query = rangeMs(range);
    const ratio = Math.min(1, Math.max(0, width > 0 ? position / width : 0));
    const value = query.start + (query.end - query.start) * ratio;
    return formatAtOffset(value, range.startTime);
}

export function timeToPercent(range: TimelineRange, time: string): number {
    const query = rangeMs(range);
    const value = Date.parse(time);
    if (!Number.isFinite(value) || query.end <= query.start) return 0;
    return Math.min(100, Math.max(0, ((value - query.start) / (query.end - query.start)) * 100));
}
