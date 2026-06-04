/**
 * 统一日期格式化工具
 * 所有日期时间值使用 YYYY-MM-DD HH:mm:ss 格式（需求 20.5）
 */

/**
 * 将日期值格式化为 YYYY-MM-DD HH:mm:ss
 * @param value - 日期字符串、Date 对象或时间戳
 * @returns 格式化后的日期字符串，无效输入返回空字符串
 */
export function formatDateTime(value: string | Date | number): string {
  const date = new Date(value);
  if (isNaN(date.getTime())) return '';

  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  const hours = String(date.getHours()).padStart(2, '0');
  const minutes = String(date.getMinutes()).padStart(2, '0');
  const seconds = String(date.getSeconds()).padStart(2, '0');

  return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;
}

/**
 * 仅格式化日期部分 YYYY-MM-DD
 */
export function formatDate(value: string | Date | number): string {
  const date = new Date(value);
  if (isNaN(date.getTime())) return '';

  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');

  return `${year}-${month}-${day}`;
}

/**
 * 计算从现在到目标日期的剩余天数
 * 正数表示未来，负数表示已过期
 */
export function daysUntil(dateStr: string): number {
  const target = new Date(dateStr);
  if (isNaN(target.getTime())) return 0;

  const now = new Date();
  const diff = target.getTime() - now.getTime();
  return Math.ceil(diff / (1000 * 60 * 60 * 24));
}

/**
 * 判断日期是否在指定天数内过期
 */
export function isExpiringSoon(dateStr: string, days: number = 15): boolean {
  const remaining = daysUntil(dateStr);
  return remaining > 0 && remaining <= days;
}

/**
 * 判断日期是否已过期
 */
export function isExpired(dateStr: string): boolean {
  return daysUntil(dateStr) <= 0;
}
