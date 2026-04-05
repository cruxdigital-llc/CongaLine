import { readFileSync, watch } from 'fs';
import { createHmac } from 'crypto';
import { createServer } from 'http';

// Load routing config
const CONFIG_PATH = process.env.ROUTER_CONFIG || '/opt/conga/config/telegram-routing.json';
let config;

function loadConfig() {
  const raw = JSON.parse(readFileSync(CONFIG_PATH, 'utf-8'));
  console.log(`[telegram-router] Loaded config: ${Object.keys(raw.channels || {}).length} channels, ${Object.keys(raw.members || {}).length} members`);
  return raw;
}

try {
  config = loadConfig();
} catch (err) {
  console.error(`[telegram-router] Failed to load config from ${CONFIG_PATH}:`, err.message);
  process.exit(1);
}

// Watch for config changes and hot-reload
let reloadTimer;
watch(CONFIG_PATH, () => {
  clearTimeout(reloadTimer);
  reloadTimer = setTimeout(() => {
    try {
      config = loadConfig();
    } catch (err) {
      console.error(`[telegram-router] Config reload failed, keeping previous config:`, err.message);
    }
  }, 500);
});

const botToken = process.env.TELEGRAM_BOT_TOKEN;
const webhookSecret = process.env.TELEGRAM_WEBHOOK_SECRET || '';
const webhookPort = parseInt(process.env.TELEGRAM_ROUTER_PORT || '8443', 10);
const signingSecret = process.env.SLACK_SIGNING_SECRET || webhookSecret; // reuse for HMAC to containers

if (!botToken) { console.error('[telegram-router] TELEGRAM_BOT_TOKEN required'); process.exit(1); }

// Deduplication by update_id
const recentUpdates = new Map();
const DEDUP_TTL_MS = 30_000;

function isDuplicate(updateId) {
  if (!updateId) return false;
  if (recentUpdates.has(updateId)) return true;
  recentUpdates.set(updateId, Date.now());
  if (recentUpdates.size > 500) {
    const cutoff = Date.now() - DEDUP_TTL_MS;
    for (const [k, v] of recentUpdates) {
      if (v < cutoff) recentUpdates.delete(k);
    }
  }
  return false;
}

// Extract the user ID from a Telegram update
function extractUserId(update) {
  return update?.message?.from?.id?.toString()
    || update?.callback_query?.from?.id?.toString()
    || update?.inline_query?.from?.id?.toString()
    || null;
}

// Extract the chat ID from a Telegram update
function extractChatId(update) {
  return update?.message?.chat?.id?.toString()
    || update?.callback_query?.message?.chat?.id?.toString()
    || null;
}

// Resolve routing target for a Telegram update
function resolveTarget(update) {
  const chatId = extractChatId(update);
  const userId = extractUserId(update);

  // Group chats: negative chat IDs
  if (chatId && chatId.startsWith('-') && config.channels?.[chatId]) {
    return { target: config.channels[chatId], reason: `group:${chatId}` };
  }

  // DMs: route by user ID
  if (userId && config.members?.[userId]) {
    return { target: config.members[userId], reason: `user:${userId}` };
  }

  return null;
}

// Forward a Telegram update to the target container via HTTP POST
async function forwardUpdate(target, update) {
  const body = JSON.stringify(update);
  const timestamp = Math.floor(Date.now() / 1000).toString();

  const headers = { 'Content-Type': 'application/json' };

  const url = new URL(target);
  if (url.pathname.startsWith('/webhooks/')) {
    // Hermes webhook adapter: HMAC of body
    if (signingSecret) {
      const hmac = createHmac('sha256', signingSecret).update(body).digest('hex');
      headers['X-Webhook-Signature'] = hmac;
    }
    headers['X-Webhook-Timestamp'] = timestamp;
    headers['X-Webhook-Source'] = 'telegram';
  } else {
    // OpenClaw or generic: include Telegram secret token header
    if (webhookSecret) {
      headers['X-Telegram-Bot-Api-Secret-Token'] = webhookSecret;
    }
  }

  try {
    const res = await fetch(target, { method: 'POST', headers, body });
    if (!res.ok) {
      const text = await res.text().catch(() => '');
      console.error(`[telegram-router] Forward failed: ${res.status} → ${target} ${text}`);
    }
  } catch (err) {
    console.error(`[telegram-router] Forward error to ${target}:`, err.message);
  }
}

// Register webhook with Telegram API
async function registerWebhook() {
  // In Docker, the webhook URL is typically set externally (e.g., via ngrok
  // or a reverse proxy). The router just needs to listen for incoming POSTs.
  // If TELEGRAM_WEBHOOK_URL is set, register it with Telegram.
  const webhookUrl = process.env.TELEGRAM_WEBHOOK_URL;
  if (!webhookUrl) {
    console.log('[telegram-router] No TELEGRAM_WEBHOOK_URL set — listening for updates without registering webhook.');
    console.log('[telegram-router] Set TELEGRAM_WEBHOOK_URL to register with Telegram API.');
    return;
  }

  const params = new URLSearchParams({
    url: webhookUrl,
    allowed_updates: JSON.stringify(['message', 'callback_query', 'inline_query']),
  });
  if (webhookSecret) {
    params.set('secret_token', webhookSecret);
  }

  try {
    const res = await fetch(`https://api.telegram.org/bot${botToken}/setWebhook?${params}`);
    const data = await res.json();
    if (data.ok) {
      console.log(`[telegram-router] Webhook registered: ${webhookUrl}`);
    } else {
      console.error(`[telegram-router] Failed to register webhook:`, data.description);
    }
  } catch (err) {
    console.error(`[telegram-router] Webhook registration error:`, err.message);
  }
}

// HTTP server to receive Telegram webhook updates
const server = createServer(async (req, res) => {
  // Health check
  if (req.method === 'GET' && req.url === '/health') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ status: 'ok', platform: 'telegram-router' }));
    return;
  }

  if (req.method !== 'POST') {
    res.writeHead(405);
    res.end('Method Not Allowed');
    return;
  }

  // Verify secret token if configured
  if (webhookSecret) {
    const headerSecret = req.headers['x-telegram-bot-api-secret-token'];
    if (headerSecret !== webhookSecret) {
      res.writeHead(403);
      res.end('Forbidden');
      return;
    }
  }

  // Read body
  const chunks = [];
  for await (const chunk of req) chunks.push(chunk);
  const body = Buffer.concat(chunks).toString();

  let update;
  try {
    update = JSON.parse(body);
  } catch {
    res.writeHead(400);
    res.end('Bad Request');
    return;
  }

  // Respond immediately (Telegram expects 200 within seconds)
  res.writeHead(200);
  res.end('ok');

  // Deduplicate
  if (isDuplicate(update.update_id)) return;

  const userId = extractUserId(update);
  const chatId = extractChatId(update);
  const route = resolveTarget(update);

  if (route) {
    console.log(`[telegram-router] update_id=${update.update_id} → ${route.reason}`);
    forwardUpdate(route.target, update).catch(err =>
      console.error(`[telegram-router] Async forward error:`, err.message)
    );
  } else {
    console.log(`[telegram-router] No route: user=${userId} chat=${chatId} — dropped`);
  }
});

// Start
console.log(`[telegram-router] Starting Conga Line Telegram event router on port ${webhookPort}...`);
registerWebhook();
server.listen(webhookPort, () => {
  console.log(`[telegram-router] Listening on port ${webhookPort}`);
});
