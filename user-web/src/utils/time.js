import { formatDate, getLocale } from './format'

/**
 * 统一的时间展示工具。
 *
 * 后端接口多数直接返回 Go 的 RFC3339 字符串（如 `2026-10-22T17:07:04+08:00`），
 * 未预测/未活跃的行还会返回 Go 零值时间 `0001-01-01T00:00:00Z`（序列化后可能带 +08:05 这种怪时区）。
 * 直接把原串丢到表格里会出现「2026-10-22T17:07:04+08:00」「0001-01-01T08:05:43+08:05」这类给用户看的垃圾，
 * 所以统一走这里：合法时间按当前语言格式化，零值/非法值显示占位符。
 */
export const fmtTime = (value, opts = { dateStyle: 'medium', timeStyle: 'short' }, empty = '-') => {
  if (value === null || value === undefined || value === '') return empty
  // 后端的零值时间有两种形态：RFC3339 的 `0001-01-01T00:00:00Z`，以及数据库直出的
  // `0001-01-01 08:05:43`。后一种在部分浏览器里会被宽松解析成 2001 年，只靠
  // `getFullYear() <= 1` 拦不住，会显示成「2001年1月1日 08:05」这种假日期。
  // 所以前缀直接判定为零值。
  if (typeof value === 'string' && /^0001-01-01/.test(value.trim())) return empty
  const date = new Date(value)
  if (Number.isNaN(date.getTime()) || date.getFullYear() <= 1) return empty
  return formatDate(value, getLocale(), opts) || String(value)
}

/** 只要日期（不显示时分），用于生日/统计日期等场景 */
export const fmtDate = (value, empty = '-') => fmtTime(value, { dateStyle: 'medium' }, empty)

export default fmtTime
