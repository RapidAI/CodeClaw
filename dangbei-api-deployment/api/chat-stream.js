'use strict';

const crypto = require('crypto');

const {
  extractToolNames,
  createToolSieveState,
  processToolSieveChunk,
  flushToolSieve,
  parseToolCalls,
  formatOpenAIStreamToolCalls,
} = require('./helpers/stream-tool-sieve');

const DEEPSEEK_COMPLETION_URL = 'https://chat.deepseek.com/api/v0/chat/completion';

const BASE_HEADERS = {
  Host: 'chat.deepseek.com',
  'User-Agent': 'DeepSeek/1.6.11 Android/35',
  Accept: 'application/json',
  'Content-Type': 'application/json',
  'x-client-platform': 'android',
  'x-client-version': '1.6.11',
  'x-client-locale': 'zh_CN',
  'accept-charset': 'UTF-8',
};

const SKIP_PATTERNS = [
  'quasi_status',
  'elapsed_secs',
  'token_usage',
  'pending_fragment',
  'conversation_mode',
  'fragments/-1/status',
  'fragments/-2/status',
  'fragments/-3/status',
];

module.exports = async function handler(req, res) {
  setCorsHeaders(res);
  if (req.method === 'OPTIONS') {
    res.statusCode = 204;
    res.end();
    return;
  }
  if (req.method !== 'POST') {
    writeOpenAIError(res, 405, 'method not allowed');
    return;
  }

  const rawBody = await readRawBody(req);

  // Hard guard: only use Node data path for streaming on Vercel runtime.
  // Any non-Vercel runtime always falls back to Go for full behavior parity.
  if (!isVercelRuntime()) {
    await proxyToGo(req, res, rawBody);
    return;
  }

  let payload;
  try {
    payload = JSON.parse(rawBody.toString('utf8') || '{}');
  } catch (_err) {
    writeOpenAIError(res, 400, 'invalid json');
    return;
  }

  // Keep all non-stream behavior on Go side to avoid compatibility regressions.
  if (!toBool(payload.stream)) {
    await proxyToGo(req, res, rawBody);
    return;
  }

  const prep = await fetchStreamPrepare(req, rawBody);
  if (!prep.ok) {
    relayPreparedFailure(res, prep);
    return;
  }

  const model = asString(prep.body.model) || asString(payload.model);
  const sessionID = asString(prep.body.session_id) || `chatcmpl-${Date.now()}`;
  const leaseID = asString(prep.body.lease_id);
  const deepseekToken = asString(prep.body.deepseek_token);
  const powHeader = asString(prep.body.pow_header);
  const completionPayload = prep.body.payload && typeof prep.body.payload === 'object' ? prep.body.payload : null;
  const finalPrompt = asString(prep.body.final_prompt);
  const thinkingEnabled = toBool(prep.body.thinking_enabled);
  const searchEnabled = toBool(prep.body.search_enabled);
  const toolPolicy = resolveToolcallPolicy(prep.body, payload.tools);
  const toolNames = toolPolicy.toolNames;

  if (!model || !leaseID || !deepseekToken || !powHeader || !completionPayload) {
    writeOpenAIError(res, 500, 'invalid vercel prepare response');
    return;
  }
  const releaseLease = createLeaseReleaser(req, leaseID);
  const upstreamController = new AbortController();
  let clientClosed = false;
  let reader = null;
  const markClientClosed = () => {
    if (clientClosed) {
      return;
    }
    clientClosed = true;
    upstreamController.abort();
    if (reader && typeof reader.cancel === 'function') {
      Promise.resolve(reader.cancel()).catch(() => {});
    }
  };
  const onReqAborted = () => markClientClosed();
  const onResClose = () => {
    if (!res.writableEnded) {
      markClientClosed();
    }
  };
  req.on('aborted', onReqAborted);
  res.on('close', onResClose);
  try {
    let completionRes;
    try {
      completionRes = await fetch(DEEPSEEK_COMPLETION_URL, {
        method: 'POST',
        headers: {
          ...BASE_HEADERS,
          authorization: `Bearer ${deepseekToken}`,
          'x-ds-pow-response': powHeader,
        },
        body: JSON.stringify(completionPayload),
        signal: upstreamController.signal,
      });
    } catch (err) {
      if (clientClosed || isAbortError(err)) {
        return;
      }
      throw err;
    }
    if (clientClosed) {
      return;
    }

    if (!completionRes.ok || !completionRes.body) {
      const detail = await safeReadText(completionRes);
      writeOpenAIError(res, 500, detail ? `Failed to get completion: ${detail}` : 'Failed to get completion.');
      return;
    }

    res.statusCode = 200;
    res.setHeader('Content-Type', 'text/event-stream');
    res.setHeader('Cache-Control', 'no-cache, no-transform');
    res.setHeader('Connection', 'keep-alive');
    res.setHeader('X-Accel-Buffering', 'no');
    if (typeof res.flushHeaders === 'function') {
      res.flushHeaders();
    }

    const created = Math.floor(Date.now() / 1000);
    let firstChunkSent = false;
    let currentType = thinkingEnabled ? 'thinking' : 'text';
    let thinkingText = '';
    let outputText = '';
    const toolSieveEnabled = toolPolicy.toolSieveEnabled;
    const emitEarlyToolDeltas = toolPolicy.emitEarlyToolDeltas;
    const toolSieveState = createToolSieveState();
    let toolCallsEmitted = false;
    const streamToolCallIDs = new Map();
    const decoder = new TextDecoder();
    reader = completionRes.body.getReader();
    let buffered = '';
    let ended = false;

    const sendFrame = (obj) => {
      if (clientClosed || res.writableEnded || res.destroyed) {
        return;
      }
      res.write(`data: ${JSON.stringify(obj)}\n\n`);
      if (typeof res.flush === 'function') {
        res.flush();
      }
    };

    const sendDeltaFrame = (delta) => {
      const payloadDelta = { ...delta };
      if (!firstChunkSent) {
        payloadDelta.role = 'assistant';
        firstChunkSent = true;
      }
      sendFrame({
        id: sessionID,
        object: 'chat.completion.chunk',
        created,
        model,
        choices: [{ delta: payloadDelta, index: 0 }],
      });
    };

    const finish = async (reason) => {
      if (ended) {
        return;
      }
      ended = true;
      if (clientClosed || res.writableEnded || res.destroyed) {
        await releaseLease();
        return;
      }
      const detected = parseToolCalls(outputText, toolNames);
      if (detected.length > 0 && !toolCallsEmitted) {
        toolCallsEmitted = true;
        sendDeltaFrame({ tool_calls: formatOpenAIStreamToolCalls(detected) });
      } else if (toolSieveEnabled) {
        const tailEvents = flushToolSieve(toolSieveState, toolNames);
        for (const evt of tailEvents) {
          if (evt.text) {
            sendDeltaFrame({ content: evt.text });
          }
        }
      }
      if (detected.length > 0 || toolCallsEmitted) {
        reason = 'tool_calls';
      }
      sendFrame({
        id: sessionID,
        object: 'chat.completion.chunk',
        created,
        model,
        choices: [{ delta: {}, index: 0, finish_reason: reason }],
        usage: buildUsage(finalPrompt, thinkingText, outputText),
      });
      if (!res.writableEnded && !res.destroyed) {
        res.write('data: [DONE]\n\n');
      }
      await releaseLease();
      if (!res.writableEnded && !res.destroyed) {
        res.end();
      }
    };

    try {
      // eslint-disable-next-line no-constant-condition
      while (true) {
        if (clientClosed) {
          await finish('stop');
          return;
        }
        const { value, done } = await reader.read();
        if (done) {
          break;
        }
        buffered += decoder.decode(value, { stream: true });
        const lines = buffered.split('\n');
        buffered = lines.pop() || '';

        for (const rawLine of lines) {
          const line = rawLine.trim();
          if (!line.startsWith('data:')) {
            continue;
          }
          const dataStr = line.slice(5).trim();
          if (!dataStr) {
            continue;
          }
          if (dataStr === '[DONE]') {
            await finish('stop');
            return;
          }
          let chunk;
          try {
            chunk = JSON.parse(dataStr);
          } catch (_err) {
            continue;
          }
          if (chunk.error || chunk.code === 'content_filter') {
            await finish('content_filter');
            return;
          }
          const parsed = parseChunkForContent(chunk, thinkingEnabled, currentType);
          currentType = parsed.newType;
          if (parsed.finished) {
            await finish('stop');
            return;
          }

          for (const p of parsed.parts) {
            if (!p.text) {
              continue;
            }
            if (searchEnabled && isCitation(p.text)) {
              continue;
            }
            if (p.type === 'thinking') {
              if (thinkingEnabled) {
                thinkingText += p.text;
                sendDeltaFrame({ reasoning_content: p.text });
              }
            } else {
              outputText += p.text;
              if (!toolSieveEnabled) {
                sendDeltaFrame({ content: p.text });
                continue;
              }
              const events = processToolSieveChunk(toolSieveState, p.text, toolNames);
              for (const evt of events) {
                if (evt.type === 'tool_call_deltas' && Array.isArray(evt.deltas) && evt.deltas.length > 0) {
                  if (!emitEarlyToolDeltas) {
                    continue;
                  }
                  toolCallsEmitted = true;
                  sendDeltaFrame({ tool_calls: formatIncrementalToolCallDeltas(evt.deltas, streamToolCallIDs) });
                  continue;
                }
                if (evt.type === 'tool_calls') {
                  toolCallsEmitted = true;
                  sendDeltaFrame({ tool_calls: formatOpenAIStreamToolCalls(evt.calls) });
                  continue;
                }
                if (evt.text) {
                  sendDeltaFrame({ content: evt.text });
                }
              }
            }
          }
        }
      }
      await finish('stop');
    } catch (err) {
      if (clientClosed || isAbortError(err)) {
        await finish('stop');
        return;
      }
      await finish('stop');
    }
  } finally {
    req.removeListener('aborted', onReqAborted);
    res.removeListener('close', onResClose);
    await releaseLease();
  }
};

function setCorsHeaders(res) {
  res.setHeader('Access-Control-Allow-Origin', '*');
  res.setHeader('Access-Control-Allow-Methods', 'GET, POST, OPTIONS, PUT, DELETE');
  res.setHeader(
    'Access-Control-Allow-Headers',
    'Content-Type, Authorization, X-API-Key, X-Ds2-Target-Account, X-Vercel-Protection-Bypass',
  );
}

function header(req, key) {
  if (!req || !req.headers) {
    return '';
  }
  return asString(req.headers[key.toLowerCase()]);
}

async function readRawBody(req) {
  if (Buffer.isBuffer(req.body)) {
    return req.body;
  }
  if (typeof req.body === 'string') {
    return Buffer.from(req.body);
  }
  if (req.body && typeof req.body === 'object') {
    return Buffer.from(JSON.stringify(req.body));
  }
  const chunks = [];
  for await (const chunk of req) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  }
  return Buffer.concat(chunks);
}

async function fetchStreamPrepare(req, rawBody) {
  const url = buildInternalGoURL(req);
  url.searchParams.set('__stream_prepare', '1');

  const upstream = await fetch(url.toString(), {
    method: 'POST',
    headers: buildInternalGoHeaders(req, { withInternalToken: true, withContentType: true }),
    body: rawBody,
  });

  const text = await upstream.text();
  let body = {};
  try {
    body = JSON.parse(text || '{}');
  } catch (_err) {
    body = {};
  }

  return {
    ok: upstream.ok,
    status: upstream.status,
    contentType: upstream.headers.get('content-type') || 'application/json',
    text,
    body,
  };
}

function relayPreparedFailure(res, prep) {
  if (prep.status === 401 && looksLikeVercelAuthPage(prep.text)) {
    writeOpenAIError(
      res,
      401,
      'Vercel Deployment Protection blocked internal prepare request. Disable protection for this deployment or set VERCEL_AUTOMATION_BYPASS_SECRET.',
    );
    return;
  }
  res.statusCode = prep.status || 500;
  res.setHeader('Content-Type', prep.contentType || 'application/json');
  if (prep.text) {
    res.end(prep.text);
    return;
  }
  writeOpenAIError(res, prep.status || 500, 'vercel prepare failed');
}

function resolveToolcallPolicy(prepBody, payloadTools) {
  const preparedToolNames = normalizePreparedToolNames(prepBody && prepBody.tool_names);
  const toolNames = preparedToolNames.length > 0 ? preparedToolNames : extractToolNames(payloadTools);
  const featureMatchEnabled = boolDefaultTrue(prepBody && prepBody.toolcall_feature_match);
  const emitEarlyToolDeltas = boolDefaultTrue(prepBody && prepBody.toolcall_early_emit_high);
  return {
    toolNames,
    toolSieveEnabled: toolNames.length > 0 && featureMatchEnabled,
    emitEarlyToolDeltas,
  };
}

function normalizePreparedToolNames(v) {
  if (!Array.isArray(v) || v.length === 0) {
    return [];
  }
  const out = [];
  for (const item of v) {
    const name = asString(item);
    if (!name) {
      continue;
    }
    out.push(name);
  }
  return out;
}

function boolDefaultTrue(v) {
  return v !== false;
}

async function safeReadText(resp) {
  if (!resp) {
    return '';
  }
  try {
    const text = await resp.text();
    return text.trim();
  } catch (_err) {
    return '';
  }
}

function internalSecret() {
  return asString(process.env.DS2API_VERCEL_INTERNAL_SECRET) || asString(process.env.DS2API_ADMIN_KEY) || 'admin';
}

function buildInternalGoURL(req) {
  const proto = asString(header(req, 'x-forwarded-proto')) || 'https';
  const host = asString(header(req, 'host'));
  const url = new URL(`${proto}://${host}${req.url || '/v1/chat/completions'}`);
  url.searchParams.set('__go', '1');
  const protectionBypass = resolveProtectionBypass(req);
  if (protectionBypass) {
    url.searchParams.set('x-vercel-protection-bypass', protectionBypass);
  }
  return url;
}

function buildInternalGoHeaders(req, opts = {}) {
  const headers = {
    authorization: asString(header(req, 'authorization')),
    'x-api-key': asString(header(req, 'x-api-key')),
    'x-ds2-target-account': asString(header(req, 'x-ds2-target-account')),
    'x-vercel-protection-bypass': resolveProtectionBypass(req),
  };
  if (opts.withInternalToken) {
    headers['x-ds2-internal-token'] = internalSecret();
  }
  if (opts.withContentType) {
    headers['content-type'] = asString(header(req, 'content-type')) || 'application/json';
  }
  return headers;
}

function createLeaseReleaser(req, leaseID) {
  let released = false;
  return async () => {
    if (released || !leaseID) {
      return;
    }
    released = true;
    try {
      await releaseStreamLease(req, leaseID);
    } catch (_err) {
      // Ignore release errors. Lease TTL cleanup on Go side still prevents permanent leaks.
    }
  };
}

async function releaseStreamLease(req, leaseID) {
  const url = buildInternalGoURL(req);
  url.searchParams.set('__stream_release', '1');
  const body = Buffer.from(JSON.stringify({ lease_id: leaseID }));

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 1500);
  try {
    await fetch(url.toString(), {
      method: 'POST',
      headers: buildInternalGoHeaders(req, { withInternalToken: true, withContentType: true }),
      body,
      signal: controller.signal,
    });
  } finally {
    clearTimeout(timeout);
  }
}

function resolveProtectionBypass(req) {
  const fromHeader = asString(header(req, 'x-vercel-protection-bypass'));
  if (fromHeader) {
    return fromHeader;
  }
  return asString(process.env.VERCEL_AUTOMATION_BYPASS_SECRET) || asString(process.env.DS2API_VERCEL_PROTECTION_BYPASS);
}

function looksLikeVercelAuthPage(text) {
  const body = asString(text).toLowerCase();
  if (!body) {
    return false;
  }
  return body.includes('authentication required') && body.includes('vercel');
}

function parseChunkForContent(chunk, thinkingEnabled, currentType) {
  if (!chunk || typeof chunk !== 'object' || !Object.prototype.hasOwnProperty.call(chunk, 'v')) {
    return { parts: [], finished: false, newType: currentType };
  }
  const pathValue = asString(chunk.p);
  if (shouldSkipPath(pathValue)) {
    return { parts: [], finished: false, newType: currentType };
  }
  if (pathValue === 'response/status' && asString(chunk.v) === 'FINISHED') {
    return { parts: [], finished: true, newType: currentType };
  }

  let newType = currentType;
  const parts = [];

  if (pathValue === 'response/fragments' && asString(chunk.o).toUpperCase() === 'APPEND' && Array.isArray(chunk.v)) {
    for (const frag of chunk.v) {
      if (!frag || typeof frag !== 'object') {
        continue;
      }
      const fragType = asString(frag.type).toUpperCase();
      const content = asString(frag.content);
      if (!content) {
        continue;
      }
      if (fragType === 'THINK' || fragType === 'THINKING') {
        newType = 'thinking';
        parts.push({ text: content, type: 'thinking' });
      } else if (fragType === 'RESPONSE') {
        newType = 'text';
        parts.push({ text: content, type: 'text' });
      } else {
        parts.push({ text: content, type: 'text' });
      }
    }
  }

  if (pathValue === 'response' && Array.isArray(chunk.v)) {
    for (const item of chunk.v) {
      if (!item || typeof item !== 'object') {
        continue;
      }
      if (item.p === 'fragments' && item.o === 'APPEND' && Array.isArray(item.v)) {
        for (const frag of item.v) {
          const fragType = asString(frag && frag.type).toUpperCase();
          if (fragType === 'THINK' || fragType === 'THINKING') {
            newType = 'thinking';
          } else if (fragType === 'RESPONSE') {
            newType = 'text';
          }
        }
      }
    }
  }

  let partType = 'text';
  if (pathValue === 'response/thinking_content') {
    partType = 'thinking';
  } else if (pathValue === 'response/content') {
    partType = 'text';
  } else if (pathValue.includes('response/fragments') && pathValue.includes('/content')) {
    partType = newType;
  } else if (!pathValue && thinkingEnabled) {
    partType = newType;
  }

  const val = chunk.v;
  if (typeof val === 'string') {
    if (val === 'FINISHED' && (!pathValue || pathValue === 'status')) {
      return { parts: [], finished: true, newType };
    }
    if (val) {
      parts.push({ text: val, type: partType });
    }
    return { parts, finished: false, newType };
  }

  if (Array.isArray(val)) {
    const extracted = extractContentRecursive(val, partType);
    if (extracted.finished) {
      return { parts: [], finished: true, newType };
    }
    parts.push(...extracted.parts);
    return { parts, finished: false, newType };
  }

  if (val && typeof val === 'object') {
    const resp = val.response && typeof val.response === 'object' ? val.response : val;
    if (Array.isArray(resp.fragments)) {
      for (const frag of resp.fragments) {
        if (!frag || typeof frag !== 'object') {
          continue;
        }
        const content = asString(frag.content);
        if (!content) {
          continue;
        }
        const t = asString(frag.type).toUpperCase();
        if (t === 'THINK' || t === 'THINKING') {
          newType = 'thinking';
          parts.push({ text: content, type: 'thinking' });
        } else if (t === 'RESPONSE') {
          newType = 'text';
          parts.push({ text: content, type: 'text' });
        } else {
          parts.push({ text: content, type: partType });
        }
      }
    }
  }
  return { parts, finished: false, newType };
}

function extractContentRecursive(items, defaultType) {
  const parts = [];
  for (const it of items) {
    if (!it || typeof it !== 'object') {
      continue;
    }
    if (!Object.prototype.hasOwnProperty.call(it, 'v')) {
      continue;
    }
    const itemPath = asString(it.p);
    const itemV = it.v;
    if (itemPath === 'status' && asString(itemV) === 'FINISHED') {
      return { parts: [], finished: true };
    }
    if (shouldSkipPath(itemPath)) {
      continue;
    }
    const content = asString(it.content);
    if (content) {
      const typeName = asString(it.type).toUpperCase();
      if (typeName === 'THINK' || typeName === 'THINKING') {
        parts.push({ text: content, type: 'thinking' });
      } else if (typeName === 'RESPONSE') {
        parts.push({ text: content, type: 'text' });
      } else {
        parts.push({ text: content, type: defaultType });
      }
      continue;
    }

    let partType = defaultType;
    if (itemPath.includes('thinking')) {
      partType = 'thinking';
    } else if (itemPath.includes('content') || itemPath === 'response' || itemPath === 'fragments') {
      partType = 'text';
    }

    if (typeof itemV === 'string') {
      if (itemV && itemV !== 'FINISHED') {
        parts.push({ text: itemV, type: partType });
      }
      continue;
    }

    if (!Array.isArray(itemV)) {
      continue;
    }
    for (const inner of itemV) {
      if (typeof inner === 'string') {
        if (inner) {
          parts.push({ text: inner, type: partType });
        }
        continue;
      }
      if (!inner || typeof inner !== 'object') {
        continue;
      }
      const ct = asString(inner.content);
      if (!ct) {
        continue;
      }
      const typeName = asString(inner.type).toUpperCase();
      if (typeName === 'THINK' || typeName === 'THINKING') {
        parts.push({ text: ct, type: 'thinking' });
      } else if (typeName === 'RESPONSE') {
        parts.push({ text: ct, type: 'text' });
      } else {
        parts.push({ text: ct, type: partType });
      }
    }
  }
  return { parts, finished: false };
}

function shouldSkipPath(pathValue) {
  if (pathValue === 'response/search_status') {
    return true;
  }
  for (const p of SKIP_PATTERNS) {
    if (pathValue.includes(p)) {
      return true;
    }
  }
  return false;
}

function isCitation(text) {
  return asString(text).trim().startsWith('[citation:');
}

function buildUsage(prompt, thinking, output) {
  const promptTokens = estimateTokens(prompt);
  const reasoningTokens = estimateTokens(thinking);
  const completionTokens = estimateTokens(output);
  return {
    prompt_tokens: promptTokens,
    completion_tokens: reasoningTokens + completionTokens,
    total_tokens: promptTokens + reasoningTokens + completionTokens,
    completion_tokens_details: {
      reasoning_tokens: reasoningTokens,
    },
  };
}

function formatIncrementalToolCallDeltas(deltas, idStore) {
  if (!Array.isArray(deltas) || deltas.length === 0) {
    return [];
  }
  const out = [];
  for (const d of deltas) {
    if (!d || typeof d !== 'object') {
      continue;
    }
    const index = Number.isInteger(d.index) ? d.index : 0;
    const id = ensureStreamToolCallID(idStore, index);
    const item = {
      index,
      id,
      type: 'function',
    };
    const fn = {};
    if (asString(d.name)) {
      fn.name = asString(d.name);
    }
    if (typeof d.arguments === 'string' && d.arguments !== '') {
      fn.arguments = d.arguments;
    }
    if (Object.keys(fn).length > 0) {
      item.function = fn;
    }
    out.push(item);
  }
  return out;
}

function ensureStreamToolCallID(idStore, index) {
  const key = Number.isInteger(index) ? index : 0;
  const existing = idStore.get(key);
  if (existing) {
    return existing;
  }
  const next = `call_${newCallID()}`;
  idStore.set(key, next);
  return next;
}

function newCallID() {
  if (typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID().replace(/-/g, '');
  }
  return `${Date.now()}${Math.floor(Math.random() * 1e9)}`;
}

function estimateTokens(text) {
  const t = asString(text);
  if (!t) {
    return 0;
  }
  const n = Math.floor(Array.from(t).length / 4);
  return n < 1 ? 1 : n;
}

async function proxyToGo(req, res, rawBody) {
  const url = buildInternalGoURL(req);
  const controller = new AbortController();
  let clientClosed = false;
  const markClientClosed = () => {
    if (clientClosed) {
      return;
    }
    clientClosed = true;
    controller.abort();
  };
  const onReqAborted = () => markClientClosed();
  const onResClose = () => {
    if (!res.writableEnded) {
      markClientClosed();
    }
  };
  req.on('aborted', onReqAborted);
  res.on('close', onResClose);

  try {
    let upstream;
    try {
      upstream = await fetch(url.toString(), {
        method: 'POST',
        headers: buildInternalGoHeaders(req, { withContentType: true }),
        body: rawBody,
        signal: controller.signal,
      });
    } catch (err) {
      if (clientClosed || isAbortError(err)) {
        if (!res.writableEnded) {
          res.end();
        }
        return;
      }
      throw err;
    }
    if (clientClosed) {
      if (!res.writableEnded) {
        res.end();
      }
      return;
    }

    res.statusCode = upstream.status;
    upstream.headers.forEach((value, key) => {
      if (key.toLowerCase() === 'content-length') {
        return;
      }
      res.setHeader(key, value);
    });

    if (!upstream.body || typeof upstream.body.getReader !== 'function') {
      const bytes = Buffer.from(await upstream.arrayBuffer());
      res.end(bytes);
      return;
    }

    const reader = upstream.body.getReader();
    try {
      // eslint-disable-next-line no-constant-condition
      while (true) {
        if (clientClosed) {
          break;
        }
        const { value, done } = await reader.read();
        if (done) {
          break;
        }
        if (value && value.length > 0) {
          res.write(Buffer.from(value));
          if (typeof res.flush === 'function') {
            res.flush();
          }
        }
      }
      if (!res.writableEnded) {
        res.end();
      }
    } catch (err) {
      if (!isAbortError(err) && !res.writableEnded) {
        res.end();
      }
    }
  } finally {
    req.removeListener('aborted', onReqAborted);
    res.removeListener('close', onResClose);
    if (!res.writableEnded) {
      res.end();
    }
  }
}

function writeOpenAIError(res, status, message) {
  res.statusCode = status;
  res.setHeader('Content-Type', 'application/json');
  res.end(
    JSON.stringify({
      error: {
        message,
        type: openAIErrorType(status),
      },
    }),
  );
}

function openAIErrorType(status) {
  switch (status) {
    case 400:
      return 'invalid_request_error';
    case 401:
      return 'authentication_error';
    case 403:
      return 'permission_error';
    case 429:
      return 'rate_limit_error';
    case 503:
      return 'service_unavailable_error';
    default:
      return status >= 500 ? 'api_error' : 'invalid_request_error';
  }
}

function toBool(v) {
  return v === true;
}

function isVercelRuntime() {
  return asString(process.env.VERCEL) !== '' || asString(process.env.NOW_REGION) !== '';
}

function asString(v) {
  if (typeof v === 'string') {
    return v.trim();
  }
  if (Array.isArray(v)) {
    return asString(v[0]);
  }
  if (v == null) {
    return '';
  }
  return String(v).trim();
}

function isAbortError(err) {
  if (!err || typeof err !== 'object') {
    return false;
  }
  return err.name === 'AbortError' || err.code === 'ABORT_ERR';
}

module.exports.__test = {
  parseChunkForContent,
  extractContentRecursive,
  shouldSkipPath,
  asString,
  resolveToolcallPolicy,
  normalizePreparedToolNames,
  boolDefaultTrue,
};
