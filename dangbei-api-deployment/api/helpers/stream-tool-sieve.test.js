'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const {
  extractToolNames,
  createToolSieveState,
  processToolSieveChunk,
  flushToolSieve,
  parseToolCalls,
  parseStandaloneToolCalls,
} = require('./stream-tool-sieve');

function runSieve(chunks, toolNames) {
  const state = createToolSieveState();
  const events = [];
  for (const chunk of chunks) {
    events.push(...processToolSieveChunk(state, chunk, toolNames));
  }
  events.push(...flushToolSieve(state, toolNames));
  return events;
}

function collectText(events) {
  return events
    .filter((evt) => evt.type === 'text' && evt.text)
    .map((evt) => evt.text)
    .join('');
}

test('extractToolNames keeps tool mode enabled with unknown fallback', () => {
  const names = extractToolNames([
    { function: { description: 'no name tool' } },
    { function: { name: ' read_file ' } },
    {},
  ]);
  assert.deepEqual(names, ['unknown', 'read_file', 'unknown']);
});

test('parseToolCalls keeps non-object argument strings as _raw (Go parity)', () => {
  const payload = JSON.stringify({
    tool_calls: [
      { name: 'read_file', input: '123' },
      { name: 'list_dir', input: '[1,2,3]' },
    ],
  });
  const calls = parseToolCalls(payload, ['read_file', 'list_dir']);
  assert.deepEqual(calls, [
    { name: 'read_file', input: { _raw: '123' } },
    { name: 'list_dir', input: { _raw: '[1,2,3]' } },
  ]);
});

test('parseToolCalls still intercepts unknown schema names to avoid leaks', () => {
  const payload = JSON.stringify({
    tool_calls: [{ name: 'not_in_schema', input: { q: 'go' } }],
  });
  const calls = parseToolCalls(payload, ['search']);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].name, 'not_in_schema');
});

test('parseToolCalls supports fenced json and function.arguments string payload', () => {
  const text = [
    'I will call a tool now.',
    '```json',
    '{"tool_calls":[{"function":{"name":"read_file","arguments":"{\\"path\\":\\"README.md\\"}"}}]}',
    '```',
  ].join('\n');
  const calls = parseToolCalls(text, ['read_file']);
  assert.equal(calls.length, 0);
});

test('parseStandaloneToolCalls only matches standalone payload and ignores mixed prose', () => {
  const mixed = '这里是示例：{"tool_calls":[{"name":"read_file","input":{"path":"README.MD"}}]}，请勿执行。';
  const standalone = '{"tool_calls":[{"name":"read_file","input":{"path":"README.MD"}}]}';
  const mixedCalls = parseStandaloneToolCalls(mixed, ['read_file']);
  const standaloneCalls = parseStandaloneToolCalls(standalone, ['read_file']);
  assert.equal(mixedCalls.length, 0);
  assert.equal(standaloneCalls.length, 1);
});

test('parseStandaloneToolCalls ignores fenced code block tool_call examples', () => {
  const fenced = ['```json', '{"tool_calls":[{"name":"read_file","input":{"path":"README.MD"}}]}', '```'].join('\n');
  const calls = parseStandaloneToolCalls(fenced, ['read_file']);
  assert.equal(calls.length, 0);
});

test('sieve emits tool_calls and does not leak suspicious prefix on late key convergence', () => {
  const events = runSieve(
    [
      '{"',
      'tool_calls":[{"name":"read_file","input":{"path":"README.MD"}}]}',
      '后置正文C。',
    ],
    ['read_file'],
  );
  const leakedText = collectText(events);
  const hasToolCall = events.some((evt) => evt.type === 'tool_calls' && Array.isArray(evt.calls) && evt.calls.length > 0);
  const hasToolDelta = events.some((evt) => evt.type === 'tool_call_deltas' && Array.isArray(evt.deltas) && evt.deltas.length > 0);
  assert.equal(hasToolCall || hasToolDelta, true);
  assert.equal(leakedText.includes('{'), false);
  assert.equal(leakedText.toLowerCase().includes('tool_calls'), false);
  assert.equal(leakedText.includes('后置正文C。'), true);
});

test('sieve keeps embedded invalid tool-like json as normal text to avoid stream stalls', () => {
  const events = runSieve(
    [
      '前置正文D。',
      "{'tool_calls':[{'name':'read_file','input':{'path':'README.MD'}}]}",
      '后置正文E。',
    ],
    ['read_file'],
  );
  const leakedText = collectText(events);
  const hasToolCall = events.some((evt) => evt.type === 'tool_calls');
  assert.equal(hasToolCall, false);
  assert.equal(leakedText.includes('前置正文D。'), true);
  assert.equal(leakedText.includes('后置正文E。'), true);
  assert.equal(leakedText.toLowerCase().includes('tool_calls'), true);
});

test('sieve flushes incomplete captured tool json as text on stream finalize', () => {
  const events = runSieve(
    ['前置正文F。', '{"tool_calls":[{"name":"read_file"'],
    ['read_file'],
  );
  const leakedText = collectText(events);
  assert.equal(leakedText.includes('前置正文F。'), true);
  assert.equal(leakedText.toLowerCase().includes('tool_calls'), true);
  assert.equal(leakedText.includes('{'), true);
});

test('sieve keeps plain text intact in tool mode when no tool call appears', () => {
  const events = runSieve(
    ['你好，', '这是普通文本回复。', '请继续。'],
    ['read_file'],
  );
  const leakedText = collectText(events);
  const hasToolCall = events.some((evt) => evt.type === 'tool_calls');
  assert.equal(hasToolCall, false);
  assert.equal(leakedText, '你好，这是普通文本回复。请继续。');
});

test('sieve emits incremental tool_call_deltas for split arguments payload', () => {
  const state = createToolSieveState();
  const first = processToolSieveChunk(
    state,
    '{"tool_calls":[{"name":"read_file","input":{"path":"READ',
    ['read_file'],
  );
  const second = processToolSieveChunk(
    state,
    'ME.MD","mode":"head"}}]}',
    ['read_file'],
  );
  const tail = flushToolSieve(state, ['read_file']);
  const events = [...first, ...second, ...tail];
  const deltaEvents = events.filter((evt) => evt.type === 'tool_call_deltas');
  assert.equal(deltaEvents.length > 0, true);
  const merged = deltaEvents.flatMap((evt) => evt.deltas || []);
  const hasName = merged.some((d) => d.name === 'read_file');
  const argsJoined = merged
    .map((d) => d.arguments || '')
    .join('');
  assert.equal(hasName, true);
  assert.equal(argsJoined.includes('"path":"README.MD"'), true);
  assert.equal(argsJoined.includes('"mode":"head"'), true);
});

test('sieve still intercepts tool call after leading plain text without suffix', () => {
  const events = runSieve(
    ['我将调用工具。', '{"tool_calls":[{"name":"read_file","input":{"path":"README.MD"}}]}'],
    ['read_file'],
  );
  const hasTool = events.some((evt) => (evt.type === 'tool_calls' && evt.calls?.length > 0) || (evt.type === 'tool_call_deltas' && evt.deltas?.length > 0));
  const leakedText = collectText(events);
  assert.equal(hasTool, true);
  assert.equal(leakedText.includes('我将调用工具。'), true);
  assert.equal(leakedText.toLowerCase().includes('tool_calls'), false);
});

test('sieve intercepts tool call and preserves trailing same-chunk text', () => {
  const events = runSieve(
    ['{"tool_calls":[{"name":"read_file","input":{"path":"README.MD"}}]}然后继续解释。'],
    ['read_file'],
  );
  const hasTool = events.some((evt) => (evt.type === 'tool_calls' && evt.calls?.length > 0) || (evt.type === 'tool_call_deltas' && evt.deltas?.length > 0));
  const leakedText = collectText(events);
  assert.equal(hasTool, true);
  assert.equal(leakedText.includes('然后继续解释。'), true);
  assert.equal(leakedText.toLowerCase().includes('tool_calls'), false);
});
