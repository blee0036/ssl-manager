/**
 * 组件级集成测试 (不挂载 DOM，直接测试 ChannelDialog 的数据流逻辑)
 *
 * 由于项目未安装 @vue/test-utils 且 vitest 环境为 node，
 * 这里通过模拟 ChannelDialog 的 reactive 状态和提交逻辑来验证关键场景：
 * 1. 创建 Lark — 合法 webhook_url 能生成正确 payload
 * 2. 创建 Telegram — 合法 bot_token + chat_id 能生成正确 payload
 * 3. 切换类型清空字段
 * 4. 编辑全空不发 config_json
 * 5. 编辑非空发完整 config_json
 *
 * Validates: Requirements 1.3, 2.1-2.5, 3.1-3.2, 4.3-4.4
 */
import { describe, it, expect } from 'vitest';
import { serializeConfig, isConfigEmpty } from '../utils/channelConfig';
import type { ConfigFields } from '../utils/channelConfig';
import { getConfigRules } from '../utils/channelValidation';
import type { CreateAlertChannelRequest, UpdateAlertChannelRequest } from '@/service/api/alert';

/**
 * 模拟 ChannelDialog 的 formModel 结构（和组件中一致）
 */
interface FormModel {
  name: string;
  type: 'lark' | 'telegram';
  enabled: boolean;
  webhook_url: string;
  bot_token: string;
  chat_id: string;
}

/** 模拟 configFields computed 的值 */
function getConfigFields(fm: FormModel): ConfigFields {
  return {
    webhook_url: fm.webhook_url,
    bot_token: fm.bot_token,
    chat_id: fm.chat_id,
  };
}

/** 模拟类型切换行为：清空所有配置字段 */
function simulateTypeSwitch(fm: FormModel, newType: 'lark' | 'telegram'): void {
  fm.type = newType;
  fm.webhook_url = '';
  fm.bot_token = '';
  fm.chat_id = '';
}

/** 模拟创建模式的 onSubmit payload 构建 */
function buildCreatePayload(fm: FormModel): CreateAlertChannelRequest {
  const cf = getConfigFields(fm);
  return {
    name: fm.name,
    type: fm.type,
    config_json: serializeConfig(fm.type, cf),
    enabled: fm.enabled,
  };
}

/** 模拟编辑模式的 onSubmit payload 构建 */
function buildUpdatePayload(fm: FormModel): UpdateAlertChannelRequest {
  const cf = getConfigFields(fm);
  const payload: UpdateAlertChannelRequest = {
    name: fm.name,
    enabled: fm.enabled,
  };
  if (!isConfigEmpty(fm.type, cf)) {
    payload.config_json = serializeConfig(fm.type, cf);
  }
  return payload;
}

/** 模拟表单校验：调用 getConfigRules 检查必填项 */
function validateConfigFields(isEdit: boolean, fm: FormModel): string[] {
  const errors: string[] = [];
  const rules = getConfigRules(isEdit, fm.type);

  if (fm.type === 'lark' && rules.webhook_url) {
    const ruleArray = rules.webhook_url as Array<any>;
    for (const rule of ruleArray) {
      if (rule.required && (!fm.webhook_url || fm.webhook_url.trim() === '')) {
        errors.push(rule.message || '必填');
      }
      if (rule.validator) {
        const result = rule.validator(rule, fm.webhook_url);
        if (result instanceof Error) {
          errors.push(result.message);
        }
      }
    }
  }

  if (fm.type === 'telegram' && rules.bot_token) {
    const botRules = rules.bot_token as Array<any>;
    for (const rule of botRules) {
      if (rule.required && (!fm.bot_token || fm.bot_token.trim() === '')) {
        errors.push(rule.message || '必填');
      }
    }
  }

  if (fm.type === 'telegram' && rules.chat_id) {
    const chatRules = rules.chat_id as Array<any>;
    for (const rule of chatRules) {
      if (rule.required && (!fm.chat_id || fm.chat_id.trim() === '')) {
        errors.push(rule.message || '必填');
      }
    }
  }

  return errors;
}

describe('ChannelDialog 集成测试: 创建 Lark 渠道', () => {
  it('合法 webhook_url 通过校验并生成正确 payload', () => {
    const fm: FormModel = {
      name: '测试 Lark',
      type: 'lark',
      enabled: true,
      webhook_url: 'https://open.feishu.cn/open-apis/bot/v2/hook/abc123',
      bot_token: '',
      chat_id: '',
    };

    // 校验应通过
    const errors = validateConfigFields(false, fm);
    expect(errors).toEqual([]);

    // payload 正确
    const payload = buildCreatePayload(fm);
    expect(payload.name).toBe('测试 Lark');
    expect(payload.type).toBe('lark');
    expect(payload.enabled).toBe(true);
    const parsed = JSON.parse(payload.config_json);
    expect(parsed).toEqual({ webhook_url: 'https://open.feishu.cn/open-apis/bot/v2/hook/abc123' });
  });

  it('空 webhook_url 在创建模式下校验失败', () => {
    const fm: FormModel = {
      name: '空配置',
      type: 'lark',
      enabled: true,
      webhook_url: '',
      bot_token: '',
      chat_id: '',
    };

    const errors = validateConfigFields(false, fm);
    expect(errors.length).toBeGreaterThan(0);
    expect(errors.some(e => e.includes('Webhook URL'))).toBe(true);
  });

  it('非 https:// 的 webhook_url 校验失败', () => {
    const fm: FormModel = {
      name: '非安全',
      type: 'lark',
      enabled: true,
      webhook_url: 'http://insecure.example.com/hook',
      bot_token: '',
      chat_id: '',
    };

    const errors = validateConfigFields(false, fm);
    expect(errors.length).toBeGreaterThan(0);
    expect(errors.some(e => e.includes('https://'))).toBe(true);
  });
});

describe('ChannelDialog 集成测试: 创建 Telegram 渠道', () => {
  it('合法 bot_token + chat_id 通过校验并生成正确 payload', () => {
    const fm: FormModel = {
      name: '测试 Telegram',
      type: 'telegram',
      enabled: true,
      webhook_url: '',
      bot_token: '123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11',
      chat_id: '-1001234567890',
    };

    const errors = validateConfigFields(false, fm);
    expect(errors).toEqual([]);

    const payload = buildCreatePayload(fm);
    expect(payload.type).toBe('telegram');
    const parsed = JSON.parse(payload.config_json);
    expect(parsed).toEqual({
      bot_token: '123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11',
      chat_id: '-1001234567890',
    });
  });

  it('空 bot_token 在创建模式下校验失败', () => {
    const fm: FormModel = {
      name: '缺 token',
      type: 'telegram',
      enabled: true,
      webhook_url: '',
      bot_token: '',
      chat_id: '-100123',
    };

    const errors = validateConfigFields(false, fm);
    expect(errors.length).toBeGreaterThan(0);
    expect(errors.some(e => e.includes('Bot Token'))).toBe(true);
  });

  it('空 chat_id 在创建模式下校验失败', () => {
    const fm: FormModel = {
      name: '缺 chat_id',
      type: 'telegram',
      enabled: true,
      webhook_url: '',
      bot_token: '123456:validtoken',
      chat_id: '',
    };

    const errors = validateConfigFields(false, fm);
    expect(errors.length).toBeGreaterThan(0);
    expect(errors.some(e => e.includes('Chat ID'))).toBe(true);
  });
});

describe('ChannelDialog 集成测试: 切换类型清空字段', () => {
  it('从 lark 切到 telegram 清空所有配置', () => {
    const fm: FormModel = {
      name: '切换测试',
      type: 'lark',
      enabled: true,
      webhook_url: 'https://filled.example.com',
      bot_token: '',
      chat_id: '',
    };

    simulateTypeSwitch(fm, 'telegram');

    expect(fm.type).toBe('telegram');
    expect(fm.webhook_url).toBe('');
    expect(fm.bot_token).toBe('');
    expect(fm.chat_id).toBe('');
  });

  it('从 telegram 切到 lark 清空所有配置', () => {
    const fm: FormModel = {
      name: '切换测试',
      type: 'telegram',
      enabled: true,
      webhook_url: '',
      bot_token: 'filled-token',
      chat_id: '-100filled',
    };

    simulateTypeSwitch(fm, 'lark');

    expect(fm.type).toBe('lark');
    expect(fm.webhook_url).toBe('');
    expect(fm.bot_token).toBe('');
    expect(fm.chat_id).toBe('');
  });
});

describe('ChannelDialog 集成测试: 编辑模式 payload', () => {
  it('编辑全空不发 config_json', () => {
    const fm: FormModel = {
      name: '已有渠道',
      type: 'lark',
      enabled: true,
      webhook_url: '',
      bot_token: '',
      chat_id: '',
    };

    // 编辑模式校验应通过（无必填限制）
    const errors = validateConfigFields(true, fm);
    expect(errors).toEqual([]);

    // payload 不包含 config_json
    const payload = buildUpdatePayload(fm);
    expect(payload.name).toBe('已有渠道');
    expect(payload.enabled).toBe(true);
    expect(payload.config_json).toBeUndefined();
  });

  it('编辑非空发完整 config_json (lark)', () => {
    const fm: FormModel = {
      name: '更新渠道',
      type: 'lark',
      enabled: false,
      webhook_url: 'https://new-webhook.example.com',
      bot_token: '',
      chat_id: '',
    };

    const payload = buildUpdatePayload(fm);
    expect(payload.name).toBe('更新渠道');
    expect(payload.enabled).toBe(false);
    expect(payload.config_json).toBeDefined();
    const parsed = JSON.parse(payload.config_json!);
    expect(parsed).toEqual({ webhook_url: 'https://new-webhook.example.com' });
  });

  it('编辑非空发完整 config_json (telegram)', () => {
    const fm: FormModel = {
      name: '更新 TG',
      type: 'telegram',
      enabled: true,
      webhook_url: '',
      bot_token: 'new-token',
      chat_id: '-100new',
    };

    const payload = buildUpdatePayload(fm);
    expect(payload.config_json).toBeDefined();
    const parsed = JSON.parse(payload.config_json!);
    expect(parsed).toEqual({ bot_token: 'new-token', chat_id: '-100new' });
  });

  it('编辑 telegram 仅一个字段非空也发 config_json', () => {
    const fm: FormModel = {
      name: '部分更新',
      type: 'telegram',
      enabled: true,
      webhook_url: '',
      bot_token: 'only-token',
      chat_id: '',
    };

    const payload = buildUpdatePayload(fm);
    expect(payload.config_json).toBeDefined();
    const parsed = JSON.parse(payload.config_json!);
    // 序列化包含所有 telegram 字段（包括空的 chat_id）
    expect(parsed).toEqual({ bot_token: 'only-token', chat_id: '' });
  });
});
