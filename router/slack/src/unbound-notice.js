// Rate-limited "I'm not configured for this channel" ephemeral notice.
//
// Fires when a Slack message lands in a channel the bot was invited to but
// that is not present in routing.json. The message names the `conga channels
// bind` command the admin needs to run. Rate-limited per (channel, user) per
// 24 hours so busy channels don't spam users.
//
// Agent privacy: the command in the message uses the literal placeholder
// `<agent>` rather than naming the target agent. Admins know which agent
// should own which channel; users posting in the channel don't need that
// mapping and shouldn't learn internal agent names from an ephemeral.

export const RATE_LIMIT_MS = 24 * 60 * 60 * 1000; // 24 hours
export const MAX_ENTRIES = 5000;

// Build the ephemeral message text. Exported so tests can assert against the
// exact string the router will send.
export function buildNoticeText(channelId) {
  return (
    `I'm not configured for this channel yet. ` +
    `Ask your Conga admin to run: \`conga channels bind <agent> slack:${channelId}\``
  );
}

// createUnboundNotifier returns a scoped notifier with its own rate-limit
// map. The factory shape keeps tests hermetic (each test gets its own state)
// and accepts optional dependencies for time and fetch so they can be faked.
export function createUnboundNotifier({
  now = Date.now,
  fetchFn = fetch,
  botToken = process.env.SLACK_BOT_TOKEN,
  logger = console,
} = {}) {
  const rateLimit = new Map(); // key: `${channelId}:${userId}` → expiry ms

  async function notify(channelId, userId, payload) {
    // --- Filter events we should never send ephemerals for ---
    if (!channelId || !userId) {
      return { sent: false, suppressed: 'missing-ids' };
    }
    // Bot users have IDs starting with 'B'; messages from our own bot (or
    // other bots) should never trigger a "not configured" nudge.
    if (userId.startsWith('B')) {
      return { sent: false, suppressed: 'bot-user' };
    }
    if (payload?.event?.subtype === 'bot_message') {
      return { sent: false, suppressed: 'bot-message-subtype' };
    }
    if (payload?.event?.bot_id) {
      return { sent: false, suppressed: 'bot-id' };
    }

    // --- Rate limit check ---
    const key = `${channelId}:${userId}`;
    const t = now();
    const expiry = rateLimit.get(key);
    if (expiry !== undefined && expiry > t) {
      return { sent: false, suppressed: 'rate-limited' };
    }

    // Lazy eviction when the map fills. Prune expired entries in-line so we
    // don't need a separate sweeper goroutine.
    if (rateLimit.size >= MAX_ENTRIES) {
      for (const [k, exp] of rateLimit) {
        if (exp <= t) rateLimit.delete(k);
      }
    }
    // Set the entry BEFORE the HTTP call so even if postEphemeral fails with
    // a non-retriable Slack error (e.g. not_in_channel), we don't retry on
    // every subsequent message. Users report fatigue more often than they
    // report missed ephemerals.
    rateLimit.set(key, t + RATE_LIMIT_MS);

    // --- Send the ephemeral ---
    if (!botToken) {
      logger.warn?.('[router] SLACK_BOT_TOKEN not set; skipping ephemeral notice');
      return { sent: true, error: 'no-token' };
    }

    const text = buildNoticeText(channelId);
    try {
      const res = await fetchFn('https://slack.com/api/chat.postEphemeral', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json; charset=utf-8',
          Authorization: `Bearer ${botToken}`,
        },
        body: JSON.stringify({ channel: channelId, user: userId, text }),
      });
      if (!res.ok) {
        logger.warn?.(`[router] postEphemeral HTTP ${res.status}`);
        return { sent: true, error: `http-${res.status}` };
      }
      const body = await res.json().catch(() => null);
      if (body && body.ok === false) {
        // Common non-fatal failures: not_in_channel, user_not_in_channel,
        // channel_not_found, missing_scope. Log once; rate limit already set.
        logger.warn?.(`[router] postEphemeral not ok: ${body.error}`);
        return { sent: true, error: `api-${body.error}` };
      }
      return { sent: true };
    } catch (err) {
      logger.warn?.(`[router] postEphemeral error: ${err?.message || err}`);
      return { sent: true, error: 'fetch-error' };
    }
  }

  return {
    notify,
    // Exposed for tests and diagnostics only — do not mutate from the router.
    _rateLimit: rateLimit,
  };
}
