/**
 * 通知渠道动态表单校验规则。
 * 根据 isEdit 和 channelType 生成不同的 FormRules。
 */
import type { FormRules } from 'naive-ui';
import type { ChannelType } from './channelConfig';

/**
 * 生成配置字段的动态校验规则。
 * - 创建模式 lark: webhook_url 必填 + https:// 前缀
 * - 创建模式 telegram: bot_token 和 chat_id 必填
 * - 编辑模式: 无必填限制（允许留空保持原值）
 */
export function getConfigRules(isEdit: boolean, type: ChannelType): FormRules {
  if (isEdit) {
    // 编辑模式：配置字段无必填限制
    return {};
  }
  if (type === 'lark') {
    return {
      webhook_url: [
        { required: true, message: '请输入 Webhook URL', trigger: 'blur' },
        {
          validator(_rule: unknown, value: string) {
            if (value && !value.startsWith('https://')) {
              return new Error('Webhook URL 必须以 https:// 开头');
            }
            return true;
          },
          trigger: 'blur',
        },
      ],
    };
  }
  // telegram
  return {
    bot_token: [
      { required: true, message: '请输入 Bot Token', trigger: 'blur' },
    ],
    chat_id: [
      { required: true, message: '请输入 Chat ID', trigger: 'blur' },
    ],
  };
}

/**
 * 校验 webhook URL 是否合法（必须以 https:// 开头）。
 * 返回 true 表示合法，返回 Error 表示不合法。
 */
export function webhookUrlValidator(value: string): true | Error {
  if (!value || value.trim() === '') {
    return new Error('请输入 Webhook URL');
  }
  if (!value.startsWith('https://')) {
    return new Error('Webhook URL 必须以 https:// 开头');
  }
  return true;
}
