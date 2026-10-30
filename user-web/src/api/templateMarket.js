import { http } from '@/utils/request';

// ============================================================
// 模板/技能市场（F2 + S-22 闭环）
//
// 只读浏览 + 元数据 + 分类（公开橱窗）；
// S-22 接入发布-审核-支付闭环：提审/我的/下单（登录档）、审核台/收款确认（admin 档）。
// 后端目录 = 进程内种子 + DB 已上架条目合并（审核通过即上橱窗）。
// ============================================================

// ── 市场浏览（public 档）──────────────────────────────────────────

// 第二参 config 透传（如 {_silent:true}），静默失败不弹 toast。
export const listMarketEntries = (params, config) =>
  http.get('/api/template-market/list', params, config);

export const marketCategories = (config) =>
  http.get('/api/template-market/categories', undefined, config);

// 详情目前用列表页内的抽屉展示；此函数为后续独立详情页预留。
export const marketEntryDetail = (id) =>
  http.get(`/api/template-market/detail/${encodeURIComponent(id)}`);

// ── 发布-审核-支付闭环（S-22）────────────────────────────────────

// 发布入口：提审目录条目（进审核台；ID 缺省由后端生成）。
export const submitMarketEntry = (data) =>
  http.post('/api/template-market/entries', data);

// 我的发布（提审记录与审核状态）。
export const myMarketSubmissions = (config) =>
  http.get('/api/template-market/my/submissions', undefined, config);

// 我的订单（待支付/已支付/已取消）。
export const myMarketOrders = (config) =>
  http.get('/api/template-market/my/orders', undefined, config);

// 我的已获得（订单已支付的条目，带获得时点）。
export const myMarketOwned = (config) =>
  http.get('/api/template-market/my/owned', undefined, config);

// 下单：免费条目 0 元直通；付费条目需留联系电话（线下收款确认）。
export const orderMarketEntry = (entryId, data) =>
  http.post(`/api/template-market/entries/${encodeURIComponent(entryId)}/order`, data || {});

// 买家撤单（自己的待支付单）。
export const cancelMarketOrder = (orderNo) =>
  http.post(`/api/template-market/orders/${encodeURIComponent(orderNo)}/cancel`);

// ── 审核台（admin 档）────────────────────────────────────────────

export const marketReviewList = (params, config) =>
  http.get('/api/template-market/admin/entries', params, config);

// action: approve | reject | disable | enable；reject 必须带 reason。
export const marketReviewAction = (entryId, data) =>
  http.post(`/api/template-market/admin/entries/${encodeURIComponent(entryId)}/review`, data);

export const marketAdminOrders = (params, config) =>
  http.get('/api/template-market/admin/orders', params, config);

// 线下收款确认（待支付单 → 已支付）。
export const marketMarkPaid = (orderNo) =>
  http.post(`/api/template-market/admin/orders/${encodeURIComponent(orderNo)}/mark-paid`);

// ── 展示辅助 ─────────────────────────────────────────────────────

export const ENTRY_KIND_LABEL = { template: '模板', skill: '技能' };

export const AVAILABILITY_LABEL = {
  builtin: '内置可用',
  needs_module: '需开通模块',
  coming_soon: '即将上线'
};

export const AVAILABILITY_TAG_TYPE = {
  builtin: 'success',
  needs_module: 'warning',
  coming_soon: 'info'
};

// ── 写侧状态展示（S-22）────────────────────────────────────────

// 目录条目审核状态（发布/审核台共用）。
export const MARKET_ENTRY_STATUS_LABEL = {
  pending: '待审核',
  approved: '已上架',
  rejected: '已驳回',
  disabled: '已下架'
};

export const MARKET_ENTRY_STATUS_TAG_TYPE = {
  pending: 'warning',
  approved: 'success',
  rejected: 'danger',
  disabled: 'info'
};

// 订单状态。
export const MARKET_ORDER_STATUS_LABEL = {
  pending_payment: '待支付',
  paid: '已支付',
  cancelled: '已取消'
};

export const MARKET_ORDER_STATUS_TAG_TYPE = {
  pending_payment: 'warning',
  paid: 'success',
  cancelled: 'info'
};

// 金额展示：分 → 元（免费显示「免费」）。
export function marketPriceText (cents) {
  const n = Number(cents || 0)
  return n > 0 ? `¥${(n / 100).toFixed(2)}` : '免费'
}
