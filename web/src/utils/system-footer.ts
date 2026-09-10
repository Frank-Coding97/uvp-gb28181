export const SYSTEM_COPYRIGHT_DEFAULT = "Copyright © 2025 -2028  云南奇讯科技有限公司版权所有";
export const SYSTEM_RECORD_NO_DEFAULT = "滇ICP备16002997号";

const normalizeText = (value?: string) => value?.trim() || "";

export const isSeedSystemCopyright = (value?: string) => normalizeText(value) === SYSTEM_COPYRIGHT_DEFAULT;
export const isSeedSystemRecordNo = (value?: string) => normalizeText(value) === SYSTEM_RECORD_NO_DEFAULT;

export const getDisplaySystemCopyright = (value?: string) => {
    const text = normalizeText(value);
    return isSeedSystemCopyright(text) ? "" : text;
};

export const getDisplaySystemRecordNo = (value?: string) => {
    const text = normalizeText(value);
    return isSeedSystemRecordNo(text) ? "" : text;
};
