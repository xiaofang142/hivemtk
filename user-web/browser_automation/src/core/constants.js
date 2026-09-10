// constants.js — 服务端默认地址与存储键（对齐 bridge/src/core/constants.js 模式）
// 文档源：user-server/internal/config/ports.go DefaultListenPort = "8204"

export const DEFAULT_USER_SERVER = {
  host: 'localhost',
  port: 8204,
  baseUrl: 'http://localhost:8204',
  healthPaths: ['/health', '/healthz', '/readyz'],
};

export const STORAGE_KEY = 'browserAutomationConfig';

export const NM_HOST_NAME = 'com.hivemtk.browser';

// 登录态（chrome.storage.local）：扩展自带账号，独立于 NM Host token
export const AUTH_KEY = 'browserAutomationAuth';

// 原语下拉（与 user-server dto.StepItem oneof 保持一致）
export const STEP_ACTIONS = [
  { value: 'open_tab', label: '打开标签页' },
  { value: 'click', label: '点击' },
  { value: 'type', label: '输入' },
  { value: 'snapshot', label: '页面快照' },
  { value: 'markdown', label: '页面 Markdown' },
  { value: 'screenshot', label: '截图' },
  { value: 'wait', label: '等待' },
  { value: 'wait_for_selector', label: '等待元素' },
  { value: 'scroll', label: '滚动' },
  { value: 'extract', label: '提取数据' },
  { value: 'close_tab', label: '关闭标签页' },
];

// URL 规范化：补协议、去尾斜杠（参照 bridge normalizeServerUrl）
export function normalizeServerUrl(raw) {
  if (!raw) return '';
  let s = String(raw).trim();
  if (!s) return '';
  if (!/^https?:\/\//i.test(s)) s = 'http://' + s;
  s = s.replace(/\/+$/, '');
  return s;
}
