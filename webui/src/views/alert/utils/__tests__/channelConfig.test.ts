import { describe, it, expect } from 'vitest';
import { serializeConfig, isConfigEmpty } from '../channelConfig';
import type { ConfigFields } from '../channelConfig';
import { webhookUrlValidator } from '../channelValidation';

// Helper: generate random string
function randomString(maxLen = 50): string {
  const len = Math.floor(Math.random() * maxLen) + 1;
  const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./: ';
  return Array.from({ length: len }, () => chars[Math.floor(Math.random() * chars.length)]).join('');
}

// Helper: generate random whitespace string (may be empty)
function randomWhitespace(): string {
  const spaces = [' ', '\t', '\n', '  ', '\t\t'];
  const len = Math.floor(Math.random() * 3);
  return Array.from({ length: len }, () => spaces[Math.floor(Math.random() * spaces.length)]).join('');
}

const ITERATIONS = 100;

/**
 * Property 4: Config serialization produces valid JSON with correct keys
 * Validates: Requirements 3.1, 3.2, 5.3
 */
describe('serializeConfig - Property 4: produces valid JSON with correct keys', () => {
  it('lark: output is valid JSON with only webhook_url key', () => {
    for (let i = 0; i < ITERATIONS; i++) {
      const fields: ConfigFields = {
        webhook_url: randomString(),
        bot_token: randomString(),
        chat_id: randomString(),
      };
      const result = serializeConfig('lark', fields);
      const parsed = JSON.parse(result);
      expect(Object.keys(parsed)).toEqual(['webhook_url']);
      expect(parsed.webhook_url).toBe(fields.webhook_url);
      // Round-trip: re-serialize should equal original
      expect(JSON.stringify(parsed)).toBe(result);
    }
  });

  it('telegram: output is valid JSON with bot_token and chat_id keys', () => {
    for (let i = 0; i < ITERATIONS; i++) {
      const fields: ConfigFields = {
        webhook_url: randomString(),
        bot_token: randomString(),
        chat_id: randomString(),
      };
      const result = serializeConfig('telegram', fields);
      const parsed = JSON.parse(result);
      expect(Object.keys(parsed).sort()).toEqual(['bot_token', 'chat_id']);
      expect(parsed.bot_token).toBe(fields.bot_token);
      expect(parsed.chat_id).toBe(fields.chat_id);
      // Round-trip
      expect(JSON.stringify(parsed)).toBe(result);
    }
  });
});

/**
 * Property 5: isConfigEmpty returns true IFF the relevant fields are empty/whitespace
 * Validates: Requirements 4.3
 */
describe('isConfigEmpty - Property 5: correct emptiness detection', () => {
  it('lark: returns true iff webhook_url is empty/whitespace', () => {
    for (let i = 0; i < ITERATIONS; i++) {
      const webhookUrl = Math.random() > 0.5 ? randomWhitespace() : randomString();
      const fields: ConfigFields = {
        webhook_url: webhookUrl,
        bot_token: randomString(),
        chat_id: randomString(),
      };
      const result = isConfigEmpty('lark', fields);
      const expected = webhookUrl.trim() === '';
      expect(result).toBe(expected);
    }
  });

  it('telegram: returns true iff both bot_token and chat_id are empty/whitespace', () => {
    for (let i = 0; i < ITERATIONS; i++) {
      const botToken = Math.random() > 0.5 ? randomWhitespace() : randomString();
      const chatId = Math.random() > 0.5 ? randomWhitespace() : randomString();
      const fields: ConfigFields = {
        webhook_url: randomString(),
        bot_token: botToken,
        chat_id: chatId,
      };
      const result = isConfigEmpty('telegram', fields);
      const expected = botToken.trim() === '' && chatId.trim() === '';
      expect(result).toBe(expected);
    }
  });
});

/**
 * Property 2: Webhook URL validation rejects non-https strings
 * Validates: Requirements 2.2
 */
describe('webhookUrlValidator - Property 2: rejects non-https strings', () => {
  it('any string not starting with https:// is rejected', () => {
    const nonHttpsPrefixes = ['http://', 'ftp://', 'ws://', '', 'HTTPS://', 'hTTps://', 'file://', 'abc'];
    for (let i = 0; i < ITERATIONS; i++) {
      const prefix = nonHttpsPrefixes[Math.floor(Math.random() * nonHttpsPrefixes.length)];
      const value = prefix + randomString();
      // Ensure it doesn't start with https://
      if (value.startsWith('https://')) continue;
      const result = webhookUrlValidator(value);
      expect(result).toBeInstanceOf(Error);
    }
  });

  it('strings starting with https:// are accepted', () => {
    for (let i = 0; i < ITERATIONS; i++) {
      const value = 'https://' + randomString();
      const result = webhookUrlValidator(value);
      expect(result).toBe(true);
    }
  });

  it('empty/whitespace strings are rejected', () => {
    expect(webhookUrlValidator('')).toBeInstanceOf(Error);
    expect(webhookUrlValidator('   ')).toBeInstanceOf(Error);
    expect(webhookUrlValidator('\t')).toBeInstanceOf(Error);
  });
});
