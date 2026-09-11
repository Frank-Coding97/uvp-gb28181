export const WORKBENCH_PAGE_HARD_LIMIT = 100;

export function boundedPageSize(value: unknown, fallback = 20, hardLimit = WORKBENCH_PAGE_HARD_LIMIT): number {
  const safeHardLimit = Number.isSafeInteger(hardLimit) && hardLimit > 0 ? hardLimit : WORKBENCH_PAGE_HARD_LIMIT;
  const safeFallback = Number.isSafeInteger(fallback) && fallback > 0 ? Math.min(fallback, safeHardLimit) : Math.min(20, safeHardLimit);
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0
    ? Math.min(value, safeHardLimit)
    : safeFallback;
}

export function boundedPageRows<T>(
  rows: readonly T[] | null | undefined,
  pageSize: unknown,
  hardLimit = WORKBENCH_PAGE_HARD_LIMIT
): T[] {
  if (!Array.isArray(rows) || rows.length === 0) return [];
  return rows.slice(0, boundedPageSize(pageSize, 20, hardLimit));
}
