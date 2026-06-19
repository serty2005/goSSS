import { describe, expect, it } from 'vitest';
import { isAgentDataStale, resolveAgentDataTimestamp } from './agentUpdates';

describe('isAgentDataStale', () => {
  const now = new Date('2026-06-19T12:00:00Z');

  it('помечает данные старше 25 дней', () => {
    expect(isAgentDataStale('2026-05-24T11:59:59Z', now)).toBe(true);
    expect(isAgentDataStale('2026-05-25T12:00:00Z', now)).toBe(false);
    expect(isAgentDataStale(undefined, now)).toBe(false);
  });

  it('использует current_time только при отсутствии v_time', () => {
    expect(resolveAgentDataTimestamp({ v_time: 'v', current_time: 'current' })).toBe('v');
    expect(resolveAgentDataTimestamp({ current_time: 'current' })).toBe('current');
  });
});
