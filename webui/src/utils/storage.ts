/**
 * localStorage 封装工具
 * 提供类型安全的 JSON 序列化/反序列化
 */
export const storage = {
  /**
   * 从 localStorage 获取值并反序列化
   * @param key - 存储键名
   * @returns 反序列化后的值，不存在或解析失败返回 null
   */
  get<T>(key: string): T | null {
    const value = localStorage.getItem(key);
    if (value === null) return null;
    try {
      return JSON.parse(value) as T;
    } catch {
      // 如果不是有效 JSON，返回原始字符串
      return value as unknown as T;
    }
  },

  /**
   * 将值序列化后存入 localStorage
   * @param key - 存储键名
   * @param value - 要存储的值
   */
  set<T>(key: string, value: T): void {
    if (typeof value === 'string') {
      localStorage.setItem(key, value);
    } else {
      localStorage.setItem(key, JSON.stringify(value));
    }
  },

  /**
   * 移除指定键
   */
  remove(key: string): void {
    localStorage.removeItem(key);
  },

  /**
   * 清空所有 localStorage 数据
   */
  clear(): void {
    localStorage.clear();
  },
};
