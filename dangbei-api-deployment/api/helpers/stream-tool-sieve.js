'use strict';

const crypto = require('crypto');
const TOOL_CALL_PATTERN = /\{\s*["']tool_calls["']\s*:\s*\[(.*?)\]\s*\}/s;
const TOOL_SIEVE_CAPTURE_LIMIT = 8 * 1024;
const TOOL_SIEVE_CONTEXT_TAIL_LIMIT = 256;

function extractToolNames(tools) {
  if (!Array.isArray(tools) || tools.length === 0) {
    return [];
  }
  const out = [];
  for (const t of tools) {
    if (!t || typeof t !== 'object') {
      continue;
    }
    const fn = t.function && typeof t.function === 'object' ? t.function : t;
    const name = toStringSafe(fn.name);
    // Keep parity with Go injectToolPrompt: object tools without name still
    // enter tool mode via fallback name "unknown".
    out.push(name || 'unknown');
  }
  return out;
}

function createToolSieveState() {
  return {
    pending: '',
    capture: '',
    capturing: false,
    recentTextTail: '',
    toolNameSent: false,
    toolName: '',
    toolArgsStart: -1,
    toolArgsSent: -1,
    toolArgsString: false,
    toolArgsDone: false,
  };
}

function resetIncrementalToolState(state) {
  state.toolNameSent = false;
  state.toolName = '';
  state.toolArgsStart = -1;
  state.toolArgsSent = -1;
  state.toolArgsString = false;
  state.toolArgsDone = false;
}

function processToolSieveChunk(state, chunk, toolNames) {
  if (!state) {
    return [];
  }
  if (chunk) {
    state.pending += chunk;
  }
  const events = [];
  // eslint-disable-next-line no-constant-condition
  while (true) {
    if (state.capturing) {
      if (state.pending) {
        state.capture += state.pending;
        state.pending = '';
      }
      const deltas = buildIncrementalToolDeltas(state);
      if (deltas.length > 0) {
        events.push({ type: 'tool_call_deltas', deltas });
      }
      const consumed = consumeToolCapture(state, toolNames);
      if (!consumed.ready) {
        if (state.capture.length > TOOL_SIEVE_CAPTURE_LIMIT) {
          noteText(state, state.capture);
          events.push({ type: 'text', text: state.capture });
          state.capture = '';
          state.capturing = false;
          resetIncrementalToolState(state);
          continue;
        }
        break;
      }
      state.capture = '';
      state.capturing = false;
      resetIncrementalToolState(state);
      if (consumed.prefix) {
        noteText(state, consumed.prefix);
        events.push({ type: 'text', text: consumed.prefix });
      }
      if (Array.isArray(consumed.calls) && consumed.calls.length > 0) {
        events.push({ type: 'tool_calls', calls: consumed.calls });
      }
      if (consumed.suffix) {
        state.pending += consumed.suffix;
      }
      continue;
    }

    if (!state.pending) {
      break;
    }

    const start = findToolSegmentStart(state.pending);
    if (start >= 0) {
      const prefix = state.pending.slice(0, start);
      if (prefix) {
        noteText(state, prefix);
        events.push({ type: 'text', text: prefix });
      }
      state.capture = state.pending.slice(start);
      state.pending = '';
      state.capturing = true;
      resetIncrementalToolState(state);
      continue;
    }

    const [safe, hold] = splitSafeContentForToolDetection(state.pending);
    if (!safe) {
      break;
    }
    state.pending = hold;
    noteText(state, safe);
    events.push({ type: 'text', text: safe });
  }
  return events;
}

function flushToolSieve(state, toolNames) {
  if (!state) {
    return [];
  }
  const events = processToolSieveChunk(state, '', toolNames);
  if (state.capturing) {
    const consumed = consumeToolCapture(state, toolNames);
    if (consumed.ready) {
      if (consumed.prefix) {
        noteText(state, consumed.prefix);
        events.push({ type: 'text', text: consumed.prefix });
      }
      if (Array.isArray(consumed.calls) && consumed.calls.length > 0) {
        events.push({ type: 'tool_calls', calls: consumed.calls });
      }
      if (consumed.suffix) {
        noteText(state, consumed.suffix);
        events.push({ type: 'text', text: consumed.suffix });
      }
    } else if (state.capture) {
      noteText(state, state.capture);
      events.push({ type: 'text', text: state.capture });
    }
    state.capture = '';
    state.capturing = false;
    resetIncrementalToolState(state);
  }
  if (state.pending) {
    noteText(state, state.pending);
    events.push({ type: 'text', text: state.pending });
    state.pending = '';
  }
  return events;
}

function splitSafeContentForToolDetection(s) {
  const text = s || '';
  if (!text) {
    return ['', ''];
  }
  const suspiciousStart = findSuspiciousPrefixStart(text);
  if (suspiciousStart < 0) {
    return [text, ''];
  }
  if (suspiciousStart > 0) {
    return [text.slice(0, suspiciousStart), text.slice(suspiciousStart)];
  }
  // If suspicious content starts at the beginning, keep holding until we can
  // either parse a full tool JSON block or reach stream flush.
  return ['', text];
}

function findSuspiciousPrefixStart(s) {
  let start = -1;
  for (const needle of ['{', '[', '```']) {
    const idx = s.lastIndexOf(needle);
    if (idx > start) {
      start = idx;
    }
  }
  return start;
}

function findToolSegmentStart(s) {
  if (!s) {
    return -1;
  }
  const lower = s.toLowerCase();
  let offset = 0;
  // eslint-disable-next-line no-constant-condition
  while (true) {
    const keyRel = lower.indexOf('tool_calls', offset);
    if (keyRel < 0) {
      return -1;
    }
    const keyIdx = keyRel;
    const start = s.slice(0, keyIdx).lastIndexOf('{');
    const candidateStart = start >= 0 ? start : keyIdx;
    if (!insideCodeFence(s.slice(0, candidateStart))) {
      return candidateStart;
    }
    offset = keyIdx + 'tool_calls'.length;
  }
}

function consumeToolCapture(state, toolNames) {
  const captured = state.capture;
  if (!captured) {
    return { ready: false, prefix: '', calls: [], suffix: '' };
  }
  const lower = captured.toLowerCase();
  const keyIdx = lower.indexOf('tool_calls');
  if (keyIdx < 0) {
    return { ready: false, prefix: '', calls: [], suffix: '' };
  }
  const start = captured.slice(0, keyIdx).lastIndexOf('{');
  if (start < 0) {
    return { ready: false, prefix: '', calls: [], suffix: '' };
  }
  const obj = extractJSONObjectFrom(captured, start);
  if (!obj.ok) {
    return { ready: false, prefix: '', calls: [], suffix: '' };
  }
  const prefixPart = captured.slice(0, start);
  const suffixPart = captured.slice(obj.end);
  if (insideCodeFence((state.recentTextTail || '') + prefixPart)) {
    return {
      ready: true,
      prefix: captured,
      calls: [],
      suffix: '',
    };
  }
  const parsed = parseStandaloneToolCalls(captured.slice(start, obj.end), toolNames);
  if (parsed.length === 0) {
    if (state.toolNameSent) {
      return {
        ready: true,
        prefix: prefixPart,
        calls: [],
        suffix: suffixPart,
      };
    }
    return {
      ready: true,
      prefix: captured,
      calls: [],
      suffix: '',
    };
  }
  if (state.toolNameSent) {
    if (parsed.length > 1) {
      return {
        ready: true,
        prefix: prefixPart,
        calls: parsed.slice(1),
        suffix: suffixPart,
      };
    }
    return {
      ready: true,
      prefix: prefixPart,
      calls: [],
      suffix: suffixPart,
    };
  }
  return {
    ready: true,
    prefix: prefixPart,
    calls: parsed,
    suffix: suffixPart,
  };
}

function buildIncrementalToolDeltas(state) {
  const captured = state.capture || '';
  if (!captured) {
    return [];
  }
  if (looksLikeToolExampleContext(state.recentTextTail)) {
    return [];
  }
  const lower = captured.toLowerCase();
  const keyIdx = lower.indexOf('tool_calls');
  if (keyIdx < 0) {
    return [];
  }
  const start = captured.slice(0, keyIdx).lastIndexOf('{');
  if (start < 0) {
    return [];
  }
  if (insideCodeFence((state.recentTextTail || '') + captured.slice(0, start))) {
    return [];
  }
  const callStart = findFirstToolCallObjectStart(captured, keyIdx);
  if (callStart < 0) {
    return [];
  }

  const deltas = [];
  if (!state.toolName) {
    const name = extractToolCallName(captured, callStart);
    if (!name) {
      return [];
    }
    state.toolName = name;
  }

  if (state.toolArgsStart < 0) {
    const args = findToolCallArgsStart(captured, callStart);
    if (args) {
      state.toolArgsString = Boolean(args.stringMode);
      state.toolArgsStart = state.toolArgsString ? args.start + 1 : args.start;
      state.toolArgsSent = state.toolArgsStart;
    }
  }
  if (!state.toolNameSent) {
    if (state.toolArgsStart < 0) {
      return [];
    }
    state.toolNameSent = true;
    deltas.push({ index: 0, name: state.toolName });
  }
  if (state.toolArgsStart < 0 || state.toolArgsDone) {
    return deltas;
  }
  const progress = scanToolCallArgsProgress(captured, state.toolArgsStart, state.toolArgsString);
  if (!progress) {
    return deltas;
  }
  if (progress.end > state.toolArgsSent) {
    deltas.push({
      index: 0,
      arguments: captured.slice(state.toolArgsSent, progress.end),
    });
    state.toolArgsSent = progress.end;
  }
  if (progress.complete) {
    state.toolArgsDone = true;
  }
  return deltas;
}

function findFirstToolCallObjectStart(text, keyIdx) {
  const arrStart = findToolCallsArrayStart(text, keyIdx);
  if (arrStart < 0) {
    return -1;
  }
  const i = skipSpaces(text, arrStart + 1);
  if (i >= text.length || text[i] !== '{') {
    return -1;
  }
  return i;
}

function findToolCallsArrayStart(text, keyIdx) {
  let i = keyIdx + 'tool_calls'.length;
  while (i < text.length && text[i] !== ':') {
    i += 1;
  }
  if (i >= text.length) {
    return -1;
  }
  i = skipSpaces(text, i + 1);
  if (i >= text.length || text[i] !== '[') {
    return -1;
  }
  return i;
}

function extractToolCallName(text, callStart) {
  let valueStart = findObjectFieldValueStart(text, callStart, ['name']);
  if (valueStart < 0 || text[valueStart] !== '"') {
    const fnStart = findFunctionObjectStart(text, callStart);
    if (fnStart < 0) {
      return '';
    }
    valueStart = findObjectFieldValueStart(text, fnStart, ['name']);
    if (valueStart < 0 || text[valueStart] !== '"') {
      return '';
    }
  }
  const parsed = parseJSONStringLiteral(text, valueStart);
  if (!parsed) {
    return '';
  }
  return parsed.value;
}

function findToolCallArgsStart(text, callStart) {
  const keys = ['input', 'arguments', 'args', 'parameters', 'params'];
  let valueStart = findObjectFieldValueStart(text, callStart, keys);
  if (valueStart < 0) {
    const fnStart = findFunctionObjectStart(text, callStart);
    if (fnStart < 0) {
      return null;
    }
    valueStart = findObjectFieldValueStart(text, fnStart, keys);
    if (valueStart < 0) {
      return null;
    }
  }
  if (valueStart >= text.length) {
    return null;
  }
  const ch = text[valueStart];
  if (ch === '{' || ch === '[') {
    return { start: valueStart, stringMode: false };
  }
  if (ch === '"') {
    return { start: valueStart, stringMode: true };
  }
  return null;
}

function scanToolCallArgsProgress(text, start, stringMode) {
  if (start < 0 || start > text.length) {
    return null;
  }
  if (stringMode) {
    let escaped = false;
    for (let i = start; i < text.length; i += 1) {
      const ch = text[i];
      if (escaped) {
        escaped = false;
        continue;
      }
      if (ch === '\\') {
        escaped = true;
        continue;
      }
      if (ch === '"') {
        return { end: i, complete: true };
      }
    }
    return { end: text.length, complete: false };
  }
  if (start >= text.length || (text[start] !== '{' && text[start] !== '[')) {
    return null;
  }
  let depth = 0;
  let quote = '';
  let escaped = false;
  for (let i = start; i < text.length; i += 1) {
    const ch = text[i];
    if (quote) {
      if (escaped) {
        escaped = false;
        continue;
      }
      if (ch === '\\') {
        escaped = true;
        continue;
      }
      if (ch === quote) {
        quote = '';
      }
      continue;
    }
    if (ch === '"' || ch === "'") {
      quote = ch;
      continue;
    }
    if (ch === '{' || ch === '[') {
      depth += 1;
      continue;
    }
    if (ch === '}' || ch === ']') {
      depth -= 1;
      if (depth === 0) {
        return { end: i + 1, complete: true };
      }
    }
  }
  return { end: text.length, complete: false };
}

function findObjectFieldValueStart(text, objStart, keys) {
  if (!text || objStart < 0 || objStart >= text.length || text[objStart] !== '{') {
    return -1;
  }
  let depth = 0;
  let quote = '';
  let escaped = false;
  for (let i = objStart; i < text.length; i += 1) {
    const ch = text[i];
    if (quote) {
      if (escaped) {
        escaped = false;
        continue;
      }
      if (ch === '\\') {
        escaped = true;
        continue;
      }
      if (ch === quote) {
        quote = '';
      }
      continue;
    }
    if (ch === '"' || ch === "'") {
      if (depth === 1) {
        const parsed = parseJSONStringLiteral(text, i);
        if (!parsed) {
          return -1;
        }
        let j = skipSpaces(text, parsed.end);
        if (j >= text.length || text[j] !== ':') {
          i = parsed.end - 1;
          continue;
        }
        j = skipSpaces(text, j + 1);
        if (j >= text.length) {
          return -1;
        }
        if (keys.includes(parsed.value)) {
          return j;
        }
        i = j - 1;
        continue;
      }
      quote = ch;
      continue;
    }
    if (ch === '{') {
      depth += 1;
      continue;
    }
    if (ch === '}') {
      depth -= 1;
      if (depth === 0) {
        break;
      }
    }
  }
  return -1;
}

function findFunctionObjectStart(text, callStart) {
  const valueStart = findObjectFieldValueStart(text, callStart, ['function']);
  if (valueStart < 0 || valueStart >= text.length || text[valueStart] !== '{') {
    return -1;
  }
  return valueStart;
}

function parseJSONStringLiteral(text, start) {
  if (!text || start < 0 || start >= text.length || text[start] !== '"') {
    return null;
  }
  let out = '';
  let escaped = false;
  for (let i = start + 1; i < text.length; i += 1) {
    const ch = text[i];
    if (escaped) {
      out += ch;
      escaped = false;
      continue;
    }
    if (ch === '\\') {
      escaped = true;
      continue;
    }
    if (ch === '"') {
      return { value: out, end: i + 1 };
    }
    out += ch;
  }
  return null;
}

function skipSpaces(text, i) {
  let idx = i;
  while (idx < text.length) {
    const ch = text[idx];
    if (ch === ' ' || ch === '\t' || ch === '\n' || ch === '\r') {
      idx += 1;
      continue;
    }
    break;
  }
  return idx;
}

function extractJSONObjectFrom(text, start) {
  if (!text || start < 0 || start >= text.length || text[start] !== '{') {
    return { ok: false, end: 0 };
  }
  let depth = 0;
  let quote = '';
  let escaped = false;
  for (let i = start; i < text.length; i += 1) {
    const ch = text[i];
    if (quote) {
      if (escaped) {
        escaped = false;
        continue;
      }
      if (ch === '\\') {
        escaped = true;
        continue;
      }
      if (ch === quote) {
        quote = '';
      }
      continue;
    }
    if (ch === '"' || ch === "'") {
      quote = ch;
      continue;
    }
    if (ch === '{') {
      depth += 1;
      continue;
    }
    if (ch === '}') {
      depth -= 1;
      if (depth === 0) {
        return { ok: true, end: i + 1 };
      }
    }
  }
  return { ok: false, end: 0 };
}

function parseToolCalls(text, toolNames) {
  if (!toStringSafe(text)) {
    return [];
  }
  const sanitized = stripFencedCodeBlocks(text);
  if (!toStringSafe(sanitized)) {
    return [];
  }
  const candidates = buildToolCallCandidates(sanitized);
  let parsed = [];
  for (const c of candidates) {
    parsed = parseToolCallsPayload(c);
    if (parsed.length > 0) {
      break;
    }
  }
  if (parsed.length === 0) {
    return [];
  }
  return filterToolCalls(parsed, toolNames);
}

function stripFencedCodeBlocks(text) {
  const t = typeof text === 'string' ? text : '';
  if (!t) {
    return '';
  }
  return t.replace(/```[\s\S]*?```/g, ' ');
}

function parseStandaloneToolCalls(text, toolNames) {
  const trimmed = toStringSafe(text);
  if (!trimmed) {
    return [];
  }
  if ((trimmed.startsWith('```') && trimmed.endsWith('```')) || trimmed.includes('```')) {
    return [];
  }
  if (looksLikeToolExampleContext(trimmed)) {
    return [];
  }
  const candidates = [trimmed];
  if (trimmed.startsWith('```') && trimmed.endsWith('```')) {
    const m = trimmed.match(/```(?:json)?\s*([\s\S]*?)\s*```/i);
    if (m && m[1]) {
      candidates.push(toStringSafe(m[1]));
    }
  }
  for (const candidate of candidates) {
    const c = toStringSafe(candidate);
    if (!c) {
      continue;
    }
    if (!c.startsWith('{') && !c.startsWith('[')) {
      continue;
    }
    const parsed = parseToolCallsPayload(c);
    if (parsed.length > 0) {
      return filterToolCalls(parsed, toolNames);
    }
  }
  return [];
}

function buildToolCallCandidates(text) {
  const trimmed = toStringSafe(text);
  const candidates = [trimmed];
  const fenced = trimmed.match(/```(?:json)?\s*([\s\S]*?)\s*```/gi) || [];
  for (const block of fenced) {
    const m = block.match(/```(?:json)?\s*([\s\S]*?)\s*```/i);
    if (m && m[1]) {
      candidates.push(toStringSafe(m[1]));
    }
  }
  for (const candidate of extractToolCallObjects(trimmed)) {
    candidates.push(toStringSafe(candidate));
  }
  const first = trimmed.indexOf('{');
  const last = trimmed.lastIndexOf('}');
  if (first >= 0 && last > first) {
    candidates.push(toStringSafe(trimmed.slice(first, last + 1)));
  }
  const m = trimmed.match(TOOL_CALL_PATTERN);
  if (m && m[1]) {
    candidates.push(`{"tool_calls":[${m[1]}]}`);
  }
  return [...new Set(candidates.filter(Boolean))];
}

function extractToolCallObjects(text) {
  const raw = toStringSafe(text);
  if (!raw) {
    return [];
  }
  const lower = raw.toLowerCase();
  const out = [];
  let offset = 0;
  // eslint-disable-next-line no-constant-condition
  while (true) {
    let idx = lower.indexOf('tool_calls', offset);
    if (idx < 0) {
      break;
    }
    let start = raw.slice(0, idx).lastIndexOf('{');
    while (start >= 0) {
      const obj = extractJSONObjectFrom(raw, start);
      if (obj.ok) {
        out.push(raw.slice(start, obj.end).trim());
        offset = obj.end;
        idx = -1;
        break;
      }
      start = raw.slice(0, start).lastIndexOf('{');
    }
    if (idx >= 0) {
      offset = idx + 'tool_calls'.length;
    }
  }
  return out;
}

function parseToolCallsPayload(payload) {
  let decoded;
  try {
    decoded = JSON.parse(payload);
  } catch (_err) {
    return [];
  }
  if (Array.isArray(decoded)) {
    return parseToolCallList(decoded);
  }
  if (!decoded || typeof decoded !== 'object') {
    return [];
  }
  if (decoded.tool_calls) {
    return parseToolCallList(decoded.tool_calls);
  }
  const one = parseToolCallItem(decoded);
  return one ? [one] : [];
}

function parseToolCallList(v) {
  if (!Array.isArray(v)) {
    return [];
  }
  const out = [];
  for (const item of v) {
    if (!item || typeof item !== 'object') {
      continue;
    }
    const one = parseToolCallItem(item);
    if (one) {
      out.push(one);
    }
  }
  return out;
}

function parseToolCallItem(m) {
  let name = toStringSafe(m.name);
  let inputRaw = m.input;
  let hasInput = Object.prototype.hasOwnProperty.call(m, 'input');
  const fn = m.function && typeof m.function === 'object' ? m.function : null;
  if (fn) {
    if (!name) {
      name = toStringSafe(fn.name);
    }
    if (!hasInput && Object.prototype.hasOwnProperty.call(fn, 'arguments')) {
      inputRaw = fn.arguments;
      hasInput = true;
    }
  }
  if (!hasInput) {
    for (const k of ['arguments', 'args', 'parameters', 'params']) {
      if (Object.prototype.hasOwnProperty.call(m, k)) {
        inputRaw = m[k];
        hasInput = true;
        break;
      }
    }
  }
  if (!name) {
    return null;
  }
  return {
    name,
    input: parseToolCallInput(inputRaw),
  };
}

function parseToolCallInput(v) {
  if (v == null) {
    return {};
  }
  if (typeof v === 'string') {
    const raw = toStringSafe(v);
    if (!raw) {
      return {};
    }
    try {
      const parsed = JSON.parse(raw);
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        return parsed;
      }
      return { _raw: raw };
    } catch (_err) {
      return { _raw: raw };
    }
  }
  if (typeof v === 'object' && !Array.isArray(v)) {
    return v;
  }
  try {
    const parsed = JSON.parse(JSON.stringify(v));
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed;
    }
  } catch (_err) {
    return {};
  }
  return {};
}

function filterToolCalls(parsed, toolNames) {
  const allowed = new Set((toolNames || []).filter(Boolean));
  const out = [];
  for (const tc of parsed) {
    if (!tc || !tc.name) {
      continue;
    }
    if (allowed.size > 0 && !allowed.has(tc.name)) {
      continue;
    }
    out.push({ name: tc.name, input: tc.input || {} });
  }
  if (out.length === 0 && parsed.length > 0) {
    for (const tc of parsed) {
      if (!tc || !tc.name) {
        continue;
      }
      out.push({ name: tc.name, input: tc.input || {} });
    }
  }
  return out;
}

function noteText(state, text) {
  if (!state || !hasMeaningfulText(text)) {
    return;
  }
  state.recentTextTail = appendTail(state.recentTextTail, text, TOOL_SIEVE_CONTEXT_TAIL_LIMIT);
}

function appendTail(prev, next, max) {
  const left = typeof prev === 'string' ? prev : '';
  const right = typeof next === 'string' ? next : '';
  if (!Number.isFinite(max) || max <= 0) {
    return '';
  }
  const combined = left + right;
  if (combined.length <= max) {
    return combined;
  }
  return combined.slice(combined.length - max);
}

function looksLikeToolExampleContext(text) {
  return insideCodeFence(text);
}

function insideCodeFence(text) {
  const t = typeof text === 'string' ? text : '';
  if (!t) {
    return false;
  }
  const ticks = (t.match(/```/g) || []).length;
  return ticks % 2 === 1;
}

function hasMeaningfulText(text) {
  return toStringSafe(text) !== '';
}

function formatOpenAIStreamToolCalls(calls) {
  if (!Array.isArray(calls) || calls.length === 0) {
    return [];
  }
  return calls.map((c, idx) => ({
    index: idx,
    id: `call_${newCallID()}`,
    type: 'function',
    function: {
      name: c.name,
      arguments: JSON.stringify(c.input || {}),
    },
  }));
}

function newCallID() {
  if (typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID().replace(/-/g, '');
  }
  return `${Date.now()}${Math.floor(Math.random() * 1e9)}`;
}

function toStringSafe(v) {
  if (typeof v === 'string') {
    return v.trim();
  }
  if (Array.isArray(v)) {
    return toStringSafe(v[0]);
  }
  if (v == null) {
    return '';
  }
  return String(v).trim();
}

module.exports = {
  extractToolNames,
  createToolSieveState,
  processToolSieveChunk,
  flushToolSieve,
  parseToolCalls,
  parseStandaloneToolCalls,
  formatOpenAIStreamToolCalls,
};
