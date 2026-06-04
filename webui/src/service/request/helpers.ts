/** 适配后的列表响应 */
export interface ListResponse<T> {
  items: T[];
  total: number;
}

/** 适配器错误 */
export class AdapterError extends Error {
  public payload: unknown;

  constructor(message: string, payload: unknown) {
    super(`API adapter error: ${message}`);
    this.payload = payload;
    this.name = 'AdapterError';
  }
}

/**
 * 解包标准响应结构
 *
 * 如果 payload 是 { code, message, data } 结构，unwrap 取 data；
 * 否则视为直接数据（兼容非标准响应）。
 */
export function unwrapPayload<T>(payload: any): T {
  if (payload && typeof payload === 'object' && 'code' in payload && 'message' in payload) {
    return payload.data as T;
  }
  return payload as T;
}

/**
 * 适配单个对象响应
 *
 * unwrap 后返回对象/值；null/undefined 时抛出 AdapterError。
 */
export function adaptResponse<T>(payload: any): T {
  const data = unwrapPayload<T>(payload);
  if (data === null || data === undefined) {
    throw new AdapterError('response data is null or undefined', payload);
  }
  return data;
}

/**
 * 适配列表响应
 *
 * unwrap 后判断：
 * a. 如果是数组 → { items: array, total: array.length }
 * b. 如果是 { items: array, total? } → { items, total: total ?? items.length }
 * c. 其他情况 → 抛出 AdapterError（不静默返回空列表）
 */
export function adaptListResponse<T>(payload: any): ListResponse<T> {
  const data = unwrapPayload<any>(payload);

  // Case a: 纯数组
  if (Array.isArray(data)) {
    return { items: data, total: data.length };
  }

  // Case b: { items: array, total? }
  if (data && typeof data === 'object' && Array.isArray(data.items)) {
    return { items: data.items, total: data.total ?? data.items.length };
  }

  // Case c: 结构不匹配
  throw new AdapterError('cannot adapt response to list format', payload);
}
