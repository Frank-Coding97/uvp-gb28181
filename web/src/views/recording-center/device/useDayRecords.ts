import { ref, type ComputedRef, type Ref } from "vue";
import {
  queryDeviceRecords,
  type RecordQueryItem,
  type RecordQueryOptions,
  type RecordQueryResult
} from "../../gb28181/device-mgmt/api";
import {
  mapRecordQueryError,
  recordQueryTimeoutMs,
  sortRecordQueryItems,
  type RecordQueryUiState
} from "../../gb28181/device-mgmt/recordQueryState";
import { dayRange, isSameDay } from "./urlState";

export interface DayEntry {
  records: RecordQueryItem[];
  state: RecordQueryUiState;
  message: string;
  result: RecordQueryResult | null;
}

export interface UseDayRecordsOptions {
  channelId: Ref<number>;
  canQuery: ComputedRef<boolean>;
}

/**
 * 按「一天」查询设备录像，并在页面内存里缓存结果。
 *
 * 和旧回放页的区别：旧页是「用户选一个时间区间 → 查一次」，每翻一天都要重开查询；
 * 这里把范围固定成整天，翻回去的时候直接命中缓存 —— 设备侧查询走 SIP，一次要
 * 十几秒，重复查同一天完全是浪费。
 *
 * 两种失败的处理刻意不同：
 * - **查询失败/超时/离线**：保留上一次的结果并标记 `stale`，时间轴灰化但不消失。
 *   直接清空会让用户以为「这天本来就没录像」，而实际上只是设备没答。
 * - **确认没有录像**（`empty`）：清空并明确展示空态，这是真实结论。
 */
export function useDayRecords(options: UseDayRecordsOptions) {
  const records = ref<RecordQueryItem[]>([]);
  const state = ref<RecordQueryUiState>("idle");
  const message = ref("");
  const result = ref<RecordQueryResult | null>(null);
  /** 当前展示的是「上一天/上一次」的旧结果，仅用于展示，不可作为当天结论。 */
  const stale = ref(false);

  const cache = new Map<string, DayEntry>();
  let token = 0;
  let abort: AbortController | null = null;

  function cacheKey(date: string) {
    return `${options.channelId.value}|${date}`;
  }

  function applyEntry(entry: DayEntry) {
    records.value = entry.records;
    state.value = entry.state;
    message.value = entry.message;
    result.value = entry.result;
    stale.value = false;
  }

  function clear() {
    ++token;
    abort?.abort();
    abort = null;
    records.value = [];
    state.value = "idle";
    message.value = "";
    result.value = null;
    stale.value = false;
    cache.clear();
  }

  async function load(date: string, queryOptions: RecordQueryOptions, opts: { force?: boolean } = {}) {
    if (!options.canQuery.value) return;
    if (!options.channelId.value) return;

    const cached = cache.get(cacheKey(date));
    if (cached && !opts.force) {
      applyEntry(cached);
      return;
    }

    abort?.abort();
    const current = ++token;
    abort = new AbortController();
    state.value = "querying";
    message.value = "";
    stale.value = false;

    const serverNow = queryOptions.serverNow || null;
    const range = dayRange(date, isSameDay(date, serverNow) ? serverNow : null);

    try {
      const response = await queryDeviceRecords(
        options.channelId.value,
        { ...range, type: "all", secrecy: 0, recorderId: "" },
        abort.signal,
        recordQueryTimeoutMs(queryOptions)
      );
      if (current !== token) return;
      const data = response.data;
      const list = sortRecordQueryItems(data.list || []);
      const entry: DayEntry = {
        records: list,
        state: data.status,
        message: "",
        result: data
      };
      cache.set(cacheKey(date), entry);
      applyEntry(entry);
    } catch (error) {
      if (current !== token) return;
      if ((error as Error)?.name === "AbortError") return;
      const mapped = mapRecordQueryError(error);
      state.value = mapped.state;
      message.value = mapped.message;
      result.value = null;
      // ⛔ 不清空 records：保留上一次的结果并标 stale，让用户还看得到「之前查到过什么」。
      //    只有真正拿到 empty 才是「这天没有录像」。
      stale.value = records.value.length > 0;
    }
  }

  function invalidate(date: string) {
    cache.delete(cacheKey(date));
  }

  return { records, state, message, result, stale, load, clear, invalidate };
}
