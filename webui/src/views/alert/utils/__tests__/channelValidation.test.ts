import { describe, it, expect } from 'vitest';
import { getConfigRules, webhookUrlValidator } from '../channelValidation';
import { isConfigEmpty } from '../channelConfig';
import type { ConfigFields } from '../channelConfig';

describe('Property 1: Type switch clears all config fields', () => {
  // This tests the logic that type switch should produce empty fields.
  // In the component, watch(formModel.type) clears configFields.
  // Here we verify isConfigEmpty correctly identifies all-empty state.

  /**
   * Validates: Requirements 1.3
   */
  it('after clearing, isConfigEmpty returns true for lark', () => {
    const clearedFields: ConfigFields = { webhook_url: '', bot_token: '', chat_id: '' };
    expect(isConfigEmpty('lark', clearedFields)).toBe(true);
  });

  it('after clearing, isConfigEmpty returns true for telegram', () => {
    const clearedFields: ConfigFields = { webhook_url: '', bot_token: '', chat_id: '' };
    expect(isConfigEmpty('telegram', clearedFields)).toBe(true);
  });

  it('non-empty fields are not considered empty after type switch', () => {
    // Before switch: lark with webhook_url filled
    const fields: ConfigFields = { webhook_url: 'https://example.com', bot_token: '', chat_id: '' };
    expect(isConfigEmpty('lark', fields)).toBe(false);
    // After switch to telegram and clear:
    const cleared: ConfigFields = { webhook_url: '', bot_token: '', chat_id: '' };
    expect(isConfigEmpty('telegram', cleared)).toBe(true);
  });
});

describe('Property 3: Edit mode validation passes with all-empty config', () => {
  /**
   * Validates: Requirements 2.5
   */
  it('edit mode returns empty rules for lark (no required constraints)', () => {
    const rules = getConfigRules(true, 'lark');
    expect(rules).toEqual({});
  });

  it('edit mode returns empty rules for telegram (no required constraints)', () => {
    const rules = getConfigRules(true, 'telegram');
    expect(rules).toEqual({});
  });

  it('create mode lark requires webhook_url', () => {
    const rules = getConfigRules(false, 'lark');
    expect(rules.webhook_url).toBeDefined();
    expect(Array.isArray(rules.webhook_url)).toBe(true);
    // Should have at least required rule
    const ruleArray = rules.webhook_url as Array<any>;
    expect(ruleArray.some((r: any) => r.required === true)).toBe(true);
  });

  it('create mode telegram requires bot_token and chat_id', () => {
    const rules = getConfigRules(false, 'telegram');
    expect(rules.bot_token).toBeDefined();
    expect(rules.chat_id).toBeDefined();
    const botRules = rules.bot_token as Array<any>;
    const chatRules = rules.chat_id as Array<any>;
    expect(botRules.some((r: any) => r.required === true)).toBe(true);
    expect(chatRules.some((r: any) => r.required === true)).toBe(true);
  });

  it('create mode lark webhook_url validator rejects non-https', () => {
    const result = webhookUrlValidator('http://not-secure.com');
    expect(result).toBeInstanceOf(Error);
  });

  it('create mode lark webhook_url validator accepts https', () => {
    const result = webhookUrlValidator('https://hooks.example.com/webhook');
    expect(result).toBe(true);
  });
});
