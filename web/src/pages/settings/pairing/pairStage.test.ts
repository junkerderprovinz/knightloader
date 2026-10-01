import { describe, expect, it } from 'vitest';
import { ALONE_AFTER_S, STAGE_BADGE, clock, pairStage } from './pairStage';

const member = { id: 'b', name: 'office', direct: true, relay: false };

describe('pairStage', () => {
  it('is unpaired outside a group, whatever else is known', () => {
    expect(pairStage({ active: false, members: [], apps: [], memberSeen: false }, 0, false)).toBe('unpaired');
    expect(pairStage({ active: false, members: [member], apps: [], memberSeen: true }, 500, true)).toBe('unpaired');
  });

  it('says New group right after this page generated the phrase', () => {
    expect(pairStage({ active: true, members: [], apps: [], memberSeen: false }, 0, true)).toBe('new');
  });

  it('says Searching in the first minute after entering a phrase', () => {
    expect(pairStage({ active: true, members: [], apps: [], memberSeen: false }, ALONE_AFTER_S - 1, false)).toBe('searching');
  });

  it('says Still alone once a minute has passed with nobody there', () => {
    expect(pairStage({ active: true, members: [], apps: [], memberSeen: false }, ALONE_AFTER_S, false)).toBe('alone');
    expect(pairStage({ active: true, members: [], apps: [], memberSeen: false }, ALONE_AFTER_S, true)).toBe('alone');
  });

  it('says Paired only while another instance is there', () => {
    expect(pairStage({ active: true, members: [member], apps: [], memberSeen: true }, 5, true)).toBe('paired');
    expect(pairStage({ active: true, members: [member], apps: [], memberSeen: true }, 3600, false)).toBe('paired');
  });

  it('counts a connected phone as somebody there', () => {
    const phone = { id: 'p', name: 'Pixel 8', deployment: 'mobile', connected: true, lastSeen: 1 };
    expect(pairStage({ active: true, members: [], apps: [phone], memberSeen: true }, 500, false)).toBe('paired');
    expect(pairStage({ active: true, members: [], apps: [{ ...phone, connected: false }], memberSeen: true }, 500, false)).toBe('gone');
  });

  it('goes back to searching, not to alone, when members that came are gone', () => {
    expect(pairStage({ active: true, members: [], apps: [], memberSeen: true }, 3600, false)).toBe('gone');
    expect(STAGE_BADGE.gone.key).toBe('pairing.stateSearching');
  });
});

describe('STAGE_BADGE', () => {
  it('gives every stage in a group its word and tone', () => {
    expect(STAGE_BADGE.new).toEqual({ key: 'pairing.stateNew', tone: 'hue' });
    expect(STAGE_BADGE.searching).toEqual({ key: 'pairing.stateSearching', tone: 'neutral' });
    expect(STAGE_BADGE.alone).toEqual({ key: 'pairing.stateAlone', tone: 'warn' });
    expect(STAGE_BADGE.paired).toEqual({ key: 'pairing.paired', tone: 'ok' });
  });
});

describe('clock', () => {
  it('writes minutes and two-digit seconds', () => {
    expect(clock(0)).toBe('0:00');
    expect(clock(9)).toBe('0:09');
    expect(clock(75)).toBe('1:15');
  });
});
