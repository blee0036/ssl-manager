/**
 * JWT payload 解析工具
 *
 * 仅解码 payload 部分（base64url），不做签名验证。
 * 签名验证由后端负责，前端只需要读取 claims。
 */

export interface JWTPayload {
  user_id: string;
  username: string;
  role: string;
  exp?: number;
  iat?: number;
}

/**
 * 解析 JWT token 的 payload 部分
 *
 * @param token - 完整的 JWT token 字符串
 * @returns 解析后的 payload 对象，解析失败返回 null
 */
export function parseJWT(token: string): JWTPayload | null {
  try {
    const parts = token.split('.');
    if (parts.length !== 3) return null;

    // base64url → base64
    const base64 = parts[1].replace(/-/g, '+').replace(/_/g, '/');
    const jsonStr = atob(base64);
    const payload = JSON.parse(jsonStr);

    // 基本结构校验
    if (typeof payload !== 'object' || payload === null) return null;
    if (typeof payload.username !== 'string' || typeof payload.role !== 'string') return null;

    return payload as JWTPayload;
  } catch {
    return null;
  }
}
