/**
 * 统一字典消费层（`sys_dict` / `sys_dict_item` 驱动）
 *
 * 范式对齐 `views/gb28181/playbackProtocol.ts`：
 *   字典可用 → 用字典；字典为空 / 未登录 / 接口失败 → **回落代码内常量**，界面不塌。
 *
 * 职责边界（⛔ 别扩）：
 * - 本层只做「值 → 展示名」的翻译，以及「生成下拉选项」。
 * - 值域**校验白名单**（`isValidResolutionCode` 之类）不在这里 —— 那校验的是"值合不合法"，
 *   不是"值叫什么"；走字典会让非法值被"翻译"成合法文案。
 * - 协议原始码（GB28181 报文里的 PTZType / RoomType 等）不在这里。
 *
 * ⛔ 值域外要**原样回显**（`未知(7)` 这类），不能吞成空 —— 排障时要看得见设备报了什么。
 *    `dictLabelsFromItems` 因此只在字典命中时覆盖，未命中时由 `useDictLabel` 兜底回显原值。
 */
import { computed, type ComputedRef } from "vue";
import { storeToRefs } from "pinia";
import { useSystemStore } from "@/store/modules/system";

/** 字典项的最小结构（兼容 store 里 `SystemDictItem` 与接口返回的松散对象） */
export interface DictItemLike {
  name?: unknown;
  value?: unknown;
  status?: unknown;
}

/** 归一化后的下拉选项 */
export interface DictOption {
  label: string;
  value: string;
}

/** 展示层「值 → 展示名」兜底表；键为字典项的 `value`（字符串化） */
export type DictLabelFallback = Readonly<Record<string, string>>;

/**
 * 字典项是否启用。
 * `status` 缺省视为启用，仅**显式** 0 / false 判为停用 —— 与 `playbackLogState` 的
 * `item.status !== 0` 口径保持"停用项不展示"一致。
 */
export function isDictItemEnabled(item: DictItemLike): boolean {
  return item.status !== 0 && item.status !== false;
}

function normalizeValue(value: unknown): string {
  return typeof value === "string" ? value.trim() : String(value ?? "").trim();
}

function normalizeLabel(name: unknown): string {
  return typeof name === "string" ? name.trim() : "";
}

/**
 * 字典项 → 下拉选项。
 * 滤掉停用项、空名、空值；按 `value` 去重（先到先得，字典顺序即展示顺序）。
 * 结果为空时回落到 `fallback`（拷贝一份，防止调用方改到常量）。
 */
export function dictOptionsFromItems(
  items: readonly DictItemLike[] | null | undefined,
  fallback: readonly DictOption[] = []
): DictOption[] {
  const seen = new Set<string>();
  const options: DictOption[] = [];
  for (const item of items || []) {
    if (!isDictItemEnabled(item)) continue;
    const value = normalizeValue(item.value);
    const label = normalizeLabel(item.name);
    if (!value || !label || seen.has(value)) continue;
    seen.add(value);
    options.push({ label, value });
  }
  return options.length > 0 ? options : fallback.map(option => ({ ...option }));
}

/**
 * 字典项 → `{value: label}` 查表。
 * `fallback` 先铺底，命中的字典项覆盖之（**字典优先**）；
 * 未在字典里的 value 不进表，由 `useDictLabel` 回显原值。
 */
export function dictLabelsFromItems(
  items: readonly DictItemLike[] | null | undefined,
  fallback: DictLabelFallback = {}
): Record<string, string> {
  const labels: Record<string, string> = { ...fallback };
  for (const item of items || []) {
    if (!isDictItemEnabled(item)) continue;
    const value = normalizeValue(item.value);
    const label = normalizeLabel(item.name);
    if (!value || !label) continue;
    labels[value] = label;
  }
  return labels;
}

/** 取某个 code 的原始字典项（响应式）。找不到返回空数组，不抛错。 */ export function useDictItems(
  code: string
): ComputedRef<DictItemLike[]> {
  const { dict } = storeToRefs(useSystemStore());
  return computed<DictItemLike[]>(() => {
    const found = (dict.value as Array<{ code?: unknown; list?: unknown }> | null | undefined)?.find?.(
      entry => entry?.code === code
    );
    return Array.isArray(found?.list) ? (found!.list as DictItemLike[]) : [];
  });
}

/** 取某个 code 的下拉选项（响应式），字典缺失时回落 `fallback`。 */
export function useDictOptions(code: string, fallback: readonly DictOption[] = []): ComputedRef<DictOption[]> {
  const items = useDictItems(code);
  return computed(() => dictOptionsFromItems(items.value, fallback));
}

/** 取某个 code 的 `{value: label}` 查表（响应式），字典缺失时回落 `fallback`。 */
export function useDictLabelMap(code: string, fallback: DictLabelFallback = {}): ComputedRef<Record<string, string>> {
  const items = useDictItems(code);
  return computed(() => dictLabelsFromItems(items.value, fallback));
}

/**
 * 取某个 code 的翻译函数：`statusLabel(record.status)`。
 * - `null` / `undefined` / `""` → 空串（不渲染占位）
 * - 字典命中 → 字典名
 * - 字典未命中 → **原值回显**（`未知(7)` 这类排障信息要看得见）
 */
export function useDictLabel(code: string, fallback: DictLabelFallback = {}): (value: unknown) => string {
  const labels = useDictLabelMap(code, fallback);
  return (value: unknown): string => {
    if (value === null || value === undefined || value === "") return "";
    const key = normalizeValue(value);
    return labels.value[key] ?? key;
  };
}

/* -------------------------------------------------------------------------- *
 * 通用「启用 / 禁用」字典
 * -------------------------------------------------------------------------- */

/** `sys_dict.code`：种子内置（id=2），`0` = 禁用 / `1` = 启用 */
export const DICT_CODE_STATUS = "status";

/**
 * 字典未加载时的兜底，与种子 `sys_dict_item`（id=21 禁用 / id=22 启用）逐字对齐。
 * ⛔ 兜底只在字典缺失时生效 —— 现场改了字典名，界面必须跟着变。
 */
export const STATUS_LABEL_FALLBACK: DictLabelFallback = { "0": "禁用", "1": "启用" };

/** `status` 字段的翻译函数（页面里取一次即可）：`statusLabel(record.status)`。 */
export function useStatusLabel(): (value: unknown) => string {
  return useDictLabel(DICT_CODE_STATUS, STATUS_LABEL_FALLBACK);
}
