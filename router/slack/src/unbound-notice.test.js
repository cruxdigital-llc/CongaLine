import { describe, it, beforeEach } from 'node:test';
import assert from 'node:assert/strict';

import {
  createUnboundNotifier,
  buildNoticeText,
  RATE_LIMIT_MS,
  MAX_ENTRIES,
} from './unbound-notice.js';

// --- Test helpers ---

// fakeFetch returns a successful postEphemeral response and records calls.
function makeFakeFetch() {
  const calls = [];
  const fetchFn = async (url, opts) => {
    calls.push({ url, opts });
    return {
      ok: true,
      status: 200,
      async json() {
        return { ok: true };
      },
    };
  };
  return { fetchFn, calls };
}

// makeClock returns a controllable clock starting at `start` ms.
function makeClock(start = 1_000_000) {
  let t = start;
  return {
    now: () => t,
    advance(ms) {
      t += ms;
    },
  };
}

// silentLogger keeps warn output out of test reports.
const silentLogger = { warn: () => {} };

// --- buildNoticeText ---

describe('buildNoticeText', () => {
  it('uses the <agent> placeholder (agent-name privacy)', () => {
    const msg = buildNoticeText('C9999999999');
    assert.match(msg, /<agent>/, 'must use literal <agent> placeholder');
    assert.ok(!/contracts|leadership|acme/i.test(msg), 'must not name a real agent');
  });

  it('includes the channel id in the suggested command', () => {
    const msg = buildNoticeText('C9999999999');
    assert.match(msg, /slack:C9999999999/);
  });

  it('includes the exact CLI command', () => {
    const msg = buildNoticeText('C1');
    assert.match(msg, /conga channels bind/);
  });
});

// --- notify: filtering / safety ---

describe('notify: event filtering', () => {
  let fake, clock, notifier;
  beforeEach(() => {
    fake = makeFakeFetch();
    clock = makeClock();
    notifier = createUnboundNotifier({
      now: clock.now,
      fetchFn: fake.fetchFn,
      botToken: 'xoxb-test',
      logger: silentLogger,
    });
  });

  it('suppresses missing channel id', async () => {
    const r = await notifier.notify('', 'U1', {});
    assert.equal(r.sent, false);
    assert.equal(r.suppressed, 'missing-ids');
    assert.equal(fake.calls.length, 0);
  });

  it('suppresses missing user id', async () => {
    const r = await notifier.notify('C1', '', {});
    assert.equal(r.sent, false);
    assert.equal(r.suppressed, 'missing-ids');
  });

  it('suppresses bot users (B-prefix)', async () => {
    const r = await notifier.notify('C1', 'BU1BOT', {});
    assert.equal(r.sent, false);
    assert.equal(r.suppressed, 'bot-user');
    assert.equal(fake.calls.length, 0);
  });

  it('suppresses bot_message subtype', async () => {
    const r = await notifier.notify('C1', 'U1', { event: { subtype: 'bot_message' } });
    assert.equal(r.sent, false);
    assert.equal(r.suppressed, 'bot-message-subtype');
  });

  it('suppresses payloads with event.bot_id', async () => {
    const r = await notifier.notify('C1', 'U1', { event: { bot_id: 'B123' } });
    assert.equal(r.sent, false);
    assert.equal(r.suppressed, 'bot-id');
  });
});

// --- notify: rate limiting ---

describe('notify: rate limit', () => {
  let fake, clock, notifier;
  beforeEach(() => {
    fake = makeFakeFetch();
    clock = makeClock();
    notifier = createUnboundNotifier({
      now: clock.now,
      fetchFn: fake.fetchFn,
      botToken: 'xoxb-test',
      logger: silentLogger,
    });
  });

  it('sends once, suppresses repeats within 24h', async () => {
    const r1 = await notifier.notify('C1', 'U1', { event: {} });
    assert.equal(r1.sent, true);
    assert.equal(fake.calls.length, 1);

    const r2 = await notifier.notify('C1', 'U1', { event: {} });
    assert.equal(r2.sent, false);
    assert.equal(r2.suppressed, 'rate-limited');
    assert.equal(fake.calls.length, 1);
  });

  it('different user in same channel → new send', async () => {
    await notifier.notify('C1', 'U1', { event: {} });
    const r = await notifier.notify('C1', 'U2', { event: {} });
    assert.equal(r.sent, true);
    assert.equal(fake.calls.length, 2);
  });

  it('same user in different channel → new send', async () => {
    await notifier.notify('C1', 'U1', { event: {} });
    const r = await notifier.notify('C2', 'U1', { event: {} });
    assert.equal(r.sent, true);
    assert.equal(fake.calls.length, 2);
  });

  it('after 24h + 1ms, sends again', async () => {
    await notifier.notify('C1', 'U1', { event: {} });
    clock.advance(RATE_LIMIT_MS + 1);
    const r = await notifier.notify('C1', 'U1', { event: {} });
    assert.equal(r.sent, true);
    assert.equal(fake.calls.length, 2);
  });

  it('just before 24h, still suppressed', async () => {
    await notifier.notify('C1', 'U1', { event: {} });
    clock.advance(RATE_LIMIT_MS - 1);
    const r = await notifier.notify('C1', 'U1', { event: {} });
    assert.equal(r.sent, false);
    assert.equal(r.suppressed, 'rate-limited');
  });
});

// --- notify: postEphemeral behavior ---

describe('notify: HTTP behavior', () => {
  it('builds a well-formed POST with bearer token and JSON body', async () => {
    const fake = makeFakeFetch();
    const clock = makeClock();
    const notifier = createUnboundNotifier({
      now: clock.now,
      fetchFn: fake.fetchFn,
      botToken: 'xoxb-super-secret',
      logger: silentLogger,
    });
    await notifier.notify('C9999999999', 'U1', { event: {} });

    assert.equal(fake.calls.length, 1);
    const { url, opts } = fake.calls[0];
    assert.equal(url, 'https://slack.com/api/chat.postEphemeral');
    assert.equal(opts.method, 'POST');
    assert.equal(opts.headers.Authorization, 'Bearer xoxb-super-secret');
    const body = JSON.parse(opts.body);
    assert.equal(body.channel, 'C9999999999');
    assert.equal(body.user, 'U1');
    assert.match(body.text, /slack:C9999999999/);
    assert.match(body.text, /<agent>/);
  });

  it('logs + rate-limits on HTTP non-2xx', async () => {
    const fetchFn = async () => ({
      ok: false,
      status: 500,
      async json() {
        return null;
      },
    });
    const notifier = createUnboundNotifier({
      now: makeClock().now,
      fetchFn,
      botToken: 'xoxb-test',
      logger: silentLogger,
    });
    const r = await notifier.notify('C1', 'U1', { event: {} });
    assert.equal(r.sent, true);
    assert.equal(r.error, 'http-500');
    // Rate-limit still set: a second call within 24h should suppress.
    const r2 = await notifier.notify('C1', 'U1', { event: {} });
    assert.equal(r2.sent, false);
    assert.equal(r2.suppressed, 'rate-limited');
  });

  it('logs + rate-limits on Slack API not_in_channel', async () => {
    const fetchFn = async () => ({
      ok: true,
      status: 200,
      async json() {
        return { ok: false, error: 'not_in_channel' };
      },
    });
    const notifier = createUnboundNotifier({
      now: makeClock().now,
      fetchFn,
      botToken: 'xoxb-test',
      logger: silentLogger,
    });
    const r = await notifier.notify('C1', 'U1', { event: {} });
    assert.equal(r.sent, true);
    assert.equal(r.error, 'api-not_in_channel');
    const r2 = await notifier.notify('C1', 'U1', { event: {} });
    assert.equal(r2.sent, false);
    assert.equal(r2.suppressed, 'rate-limited');
  });

  it('logs + rate-limits on fetch throw', async () => {
    const fetchFn = async () => {
      throw new Error('connection reset');
    };
    const notifier = createUnboundNotifier({
      now: makeClock().now,
      fetchFn,
      botToken: 'xoxb-test',
      logger: silentLogger,
    });
    const r = await notifier.notify('C1', 'U1', { event: {} });
    assert.equal(r.sent, true);
    assert.equal(r.error, 'fetch-error');
  });

  it('skips fetch when bot token is missing (still rate-limits)', async () => {
    const fake = makeFakeFetch();
    const notifier = createUnboundNotifier({
      now: makeClock().now,
      fetchFn: fake.fetchFn,
      botToken: '',
      logger: silentLogger,
    });
    const r = await notifier.notify('C1', 'U1', { event: {} });
    assert.equal(r.sent, true);
    assert.equal(r.error, 'no-token');
    assert.equal(fake.calls.length, 0, 'fetch must not run without a token');
  });
});

// --- notify: map eviction ---

describe('notify: map eviction', () => {
  it('evicts expired entries once the map fills', async () => {
    const fake = makeFakeFetch();
    const clock = makeClock();
    const notifier = createUnboundNotifier({
      now: clock.now,
      fetchFn: fake.fetchFn,
      botToken: 'xoxb-test',
      logger: silentLogger,
    });

    // Fill the map with MAX_ENTRIES expired entries by using old expiries.
    for (let i = 0; i < MAX_ENTRIES; i++) {
      notifier._rateLimit.set(`C${i}:U${i}`, clock.now() - 1); // already expired
    }
    assert.equal(notifier._rateLimit.size, MAX_ENTRIES);

    // Now a fresh send should trigger lazy eviction and accept a new entry.
    const r = await notifier.notify('C-fresh', 'U-fresh', { event: {} });
    assert.equal(r.sent, true);
    // All the expired entries should have been pruned; only the new one remains.
    assert.equal(notifier._rateLimit.size, 1);
    assert.ok(notifier._rateLimit.has('C-fresh:U-fresh'));
  });
});
