/**
 * 列表页模糊搜索工具
 *
 * 语义与后端 SQLite `LIKE '%keyword%'` 保持一致：去除首尾空白后做不区分大小写的子串匹配。
 */

/** 可参与匹配的字段：字符串或字符串数组（如证书 domains、DNS main_domains） */
export type SearchField = string | null | undefined | ReadonlyArray<string | null | undefined>;

/** 规范化关键字：去首尾空白并转小写 */
export function normalizeKeyword(keyword: string | null | undefined): string {
  return (keyword ?? '').trim().toLowerCase();
}

/**
 * 判断记录是否命中关键字。
 * - 关键字为空时视为命中（不过滤）
 * - 任一字段（或数组字段中的任一元素）包含关键字即命中
 */
export function matchesKeyword(keyword: string | null | undefined, fields: SearchField[]): boolean {
  const kw = normalizeKeyword(keyword);
  if (!kw) return true;
  return fields.some((field) => {
    if (field == null) return false;
    if (typeof field === 'string') return field.toLowerCase().includes(kw);
    return field.some((item) => item != null && item.toLowerCase().includes(kw));
  });
}

export interface DebouncedFn<A extends unknown[]> {
  (...args: A): void;
  /** 取消尚未执行的调用 */
  cancel: () => void;
}

/** 防抖：最后一次调用 wait 毫秒后执行 */
export function debounce<A extends unknown[]>(fn: (...args: A) => void, wait: number): DebouncedFn<A> {
  let timer: ReturnType<typeof setTimeout> | null = null;
  const debounced = ((...args: A) => {
    if (timer !== null) clearTimeout(timer);
    timer = setTimeout(() => {
      timer = null;
      fn(...args);
    }, wait);
  }) as DebouncedFn<A>;
  debounced.cancel = () => {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
  };
  return debounced;
}
