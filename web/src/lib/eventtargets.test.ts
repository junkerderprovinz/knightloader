import { describe, expect, it } from 'vitest';

import { discordWebhook } from './eventtargets';

describe('a Discord webhook address', () => {
  // The same cases as notify's TestADiscordAddressIsRecognisedByItsHostAndPath,
  // so the note on the row and the message the server sends agree.
  it.each([
    ['https://discord.com/api/webhooks/123/abc-DEF_1', true],
    ['https://discordapp.com/api/webhooks/123/abc', true],
    ['https://ptb.discord.com/api/webhooks/123/abc', true],
    ['https://Discord.com/api/v10/webhooks/123/abc?thread_id=9', true],
    ['https://discord.com/api/webhooks/123/abc/', true],
    ['https://discord.com/api/webhooks/123/abc/slack', false],
    ['https://discord.com/api/webhooks/123', false],
    ['https://discord.com.example/api/webhooks/123/abc', false],
    ['https://ntfy.example/api/webhooks/123/abc', false],
    ['not an address', false],
  ])('%s is %s', (addr, want) => {
    expect(discordWebhook(addr)).toBe(want);
  });
});
