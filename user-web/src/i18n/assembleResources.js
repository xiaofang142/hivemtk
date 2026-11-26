// 同一份资源清单用于运行时消息函数和构建前的原始 JSON 校验。
export function assembleResources(resources, load) {
  return Object.fromEntries(resources.locales.map(locale => {
    const messages = { ...load(`${locale}.json`) }
    for (const bundle of resources.bundles) {
      // 新业务文案先提供中英文，其余语种沿用应用的中文兜底策略。
      const bundleLocale = bundle.locales.includes(locale) ? locale : resources.bundleFallback
      const content = load(`${bundle.namespace}.${bundleLocale}.json`)
      if (bundle.root) Object.assign(messages, content)
      else messages[bundle.namespace] = content
    }
    return [locale, messages]
  }))
}
