/**
 * 通知渠道配置序列化与判空工具函数。
 * 纯函数，无外部依赖。
 */

export type ChannelType = 'lark' | 'telegram';

export interface ConfigFields {
  // Lark
  webhook_url: string;
  // Telegram
  bot_token: string;
  chat_id: string;
}

/**
 * 将结构化配置字段序列化为 JSON 字符串。
 * 仅包含当前 type 对应的字段。
 */
export function serializeConfig(type: ChannelType, fields: ConfigFields): string {
  if (type === 'lark') {
    return JSON.stringify({ webhook_url: fields.webhook_url });
  }
  return JSON.stringify({ bot_token: fields.bot_token, chat_id: fields.chat_id });
}

/**
 * 判断当前 type 对应的配置字段是否全部为空（trim 后）。
 */
export function isConfigEmpty(type: ChannelType, fields: ConfigFields): boolean {
  if (type === 'lark') {
    return fields.webhook_url.trim() === '';
  }
  return fields.bot_token.trim() === '' && fields.chat_id.trim() === '';
}
