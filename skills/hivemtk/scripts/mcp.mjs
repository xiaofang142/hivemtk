#!/usr/bin/env node
// HiveMTK MCP 调用器（零依赖，Node 18+）
//
// 用法：
//   node mcp.mjs tools-list
//   node mcp.mjs tools-call <toolName> '<jsonArgs>'
//
// 凭证（三选一，优先级从高到低）：
//   1. 命令行:   --client-id mcp-xxxx --api-key wgk_xxxx
//   2. 环境变量: HIVEMTK_MCP_CLIENT_ID / HIVEMTK_MCP_API_KEY
//   3. 配置文件: ~/.hivemtk/skill.json {"client_id":"...","api_key":"...","server":"..."}
//
// 服务器默认 https://api.xjznpt.com，可用 --server 或配置文件覆盖。

import { readFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

function parseArgs(argv) {
  const opts = { server: 'https://api.xjznpt.com' };
  const rest = [];
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === '--client-id') opts.clientId = argv[++i];
    else if (a === '--api-key') opts.apiKey = argv[++i];
    else if (a === '--server') opts.server = argv[++i];
    else rest.push(a);
  }
  if (!opts.clientId || !opts.apiKey) {
    try {
      const cfg = JSON.parse(readFileSync(join(homedir(), '.hivemtk', 'skill.json'), 'utf8'));
      opts.clientId ||= cfg.client_id;
      opts.apiKey ||= cfg.api_key;
      opts.server = opts.server || cfg.server;
    } catch { /* 无配置文件则靠环境变量/参数 */ }
  }
  if (process.env.HIVEMTK_MCP_CLIENT_ID) opts.clientId ||= process.env.HIVEMTK_MCP_CLIENT_ID;
  if (process.env.HIVEMTK_MCP_API_KEY) opts.apiKey ||= process.env.HIVEMTK_MCP_API_KEY;
  return { opts, rest };
}

async function rpc(opts, method, params) {
  const res = await fetch(`${opts.server.replace(/\/$/, '')}/api/mcp`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Client-Id': opts.clientId,
      'X-API-Key': opts.apiKey,
    },
    body: JSON.stringify({ jsonrpc: '2.0', id: Date.now(), method, params: params || {} }),
  });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    const msg = body?.message || body?.error || `HTTP ${res.status}`;
    throw new Error(`server: ${msg}`);
  }
  if (body.error) throw new Error(`rpc ${body.error.code}: ${body.error.message}`);
  return body.result;
}

const { opts, rest } = parseArgs(process.argv.slice(2));
if (!opts.clientId || !opts.apiKey) {
  console.error('缺少凭证：--client-id/--api-key、环境变量或 ~/.hivemtk/skill.json');
  process.exit(2);
}
const [cmd, tool, argsJson] = rest;
if (cmd === 'tools-list') {
  const r = await rpc(opts, 'tools/list', {});
  for (const t of r.tools || []) console.log(`${t.name}\t${t.description || ''}`);
} else if (cmd === 'tools-call') {
  if (!tool) { console.error('用法: tools-call <toolName> <jsonArgs>'); process.exit(2); }
  let args = {};
  if (argsJson) { try { args = JSON.parse(argsJson); } catch { console.error('args 不是合法 JSON'); process.exit(2); } }
  const r = await rpc(opts, 'tools/call', { name: tool, arguments: args });
  console.log(JSON.stringify(r, null, 2));
} else {
  console.error('用法: mcp.mjs [--server URL] [--client-id ID --api-key KEY] <tools-list|tools-call> [tool] [jsonArgs]');
  process.exit(2);
}
