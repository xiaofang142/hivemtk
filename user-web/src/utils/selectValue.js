// 外键下拉的选中值归一化（T06 线上复核发现）。
//
// 后端 bigint 列在 JSON 里是字符串（如 "10"），而选项 API 的 id 是数字 10；
// Element Plus 的 el-select 按严格相等匹配，直接把字符串传给数字选项会显示裸 id。
// 这里按当前选项集的类型把字符串数字转成数字；0 / '0' 表示「未关联」，显示占位符。
// 只影响展示用的 model-value，提交仍由组件 onChange 抛出用户实际选中的原值。

export function normalizeSelectValue(value, options = []) {
  if (value == null || value === '' || value === 0 || value === '0') return null
  const sample = options.find((o) => o && o.value != null)?.value
  const one = (v) => {
    if (typeof sample === 'number' && v !== '' && v != null && !Number.isNaN(Number(v))) return Number(v)
    return v
  }
  return Array.isArray(value) ? value.map(one) : one(value)
}
