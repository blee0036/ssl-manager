/**
 * Base64 编码工具
 * 用于证书上传时读取 cert/key 文件内容（需求 9.2）
 */

/**
 * 将字符串编码为 base64
 * 支持 Unicode 字符
 */
export function encodeBase64(str: string): string {
  return btoa(unescape(encodeURIComponent(str)));
}

/**
 * 将 base64 解码为字符串
 * 支持 Unicode 字符
 */
export function decodeBase64(base64: string): string {
  return decodeURIComponent(escape(atob(base64)));
}

/**
 * 将 File 对象内容读取为 base64 字符串
 * 用于证书上传场景：读取 cert/key 文件后 base64 编码提交
 * @param file - File 对象
 * @returns base64 编码的文件内容（不含 data URI 前缀）
 */
export function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = reader.result as string;
      // 移除 data:xxx;base64, 前缀，只保留纯 base64 内容
      const base64 = result.split(',')[1] || result;
      resolve(base64);
    };
    reader.onerror = () => {
      reject(new Error('Failed to read file'));
    };
    reader.readAsDataURL(file);
  });
}
