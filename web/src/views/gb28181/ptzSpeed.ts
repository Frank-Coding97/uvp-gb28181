export const DEFAULT_PTZ_SPEED_LEVEL = 6;

export function normalizePtzSpeedLevel(level: number): number {
    if (!Number.isFinite(level)) return DEFAULT_PTZ_SPEED_LEVEL;
    return Math.max(1, Math.min(10, Math.round(level)));
}

export function levelToProtocolSpeed(level: number): number {
    return Math.round(normalizePtzSpeedLevel(level) * 255 / 10);
}
