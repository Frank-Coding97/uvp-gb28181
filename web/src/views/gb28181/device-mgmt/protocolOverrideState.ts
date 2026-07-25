import type { ProtocolOverride } from "./api";

export function normalizeProtocolOverride(value?: string | null): ProtocolOverride {
    return value === "2016" || value === "2022" ? value : "auto";
}

export function protocolOverrideAfterSave(
    original: ProtocolOverride,
    selected: ProtocolOverride,
    succeeded: boolean,
): ProtocolOverride {
    return succeeded ? selected : original;
}
