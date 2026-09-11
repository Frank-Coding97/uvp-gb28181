export function formatBytes(value: number | null | undefined): string {
    const bytes = Number(value || 0);
    if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
    const units = ["B", "KB", "MB", "GB", "TB"];
    const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
    const scaled = bytes / 1024 ** index;
    return `${scaled >= 100 || index === 0 ? scaled.toFixed(0) : scaled.toFixed(1)} ${units[index]}`;
}

export function latestRequestGuard() {
    let version = 0;
    return {
        next() {
            version += 1;
            return version;
        },
        current(value: number) {
            return value === version;
        },
        cancel() {
            version += 1;
        }
    };
}

export function coverageLabel(value?: string): string {
    if (value === "partial") return "存在采集缺口";
    if (value === "not_started") return "尚未开始统计";
    return "数据完整";
}
