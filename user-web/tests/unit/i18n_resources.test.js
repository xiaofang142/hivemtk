import { describe, expect, it } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'
import resources from '@/i18n/resources.json'
import { assembleResources } from '@/i18n/assembleResources'
import { messages } from '@/i18n/catalog'
import i18n from '@/i18n'

const directory = resolve(process.cwd(), 'src/i18n/locales')
const raw = assembleResources(resources, file => JSON.parse(readFileSync(resolve(directory, file), 'utf8')))

describe('i18n 资源清单', () => {
  it('所有资源均登记且原始 JSON 与运行时预编译消息提供相同的键', () => {
    const registered = resources.locales.map(locale => `${locale}.json`)
    for (const bundle of resources.bundles) {
      for (const locale of bundle.locales) registered.push(`${bundle.namespace}.${locale}.json`)
    }
    expect(registered.sort()).toEqual(readdirSync(directory).filter(file => file.endsWith('.json')).sort())
    const keys = object => Object.entries(object).flatMap(([key, value]) => value && typeof value === 'object'
      ? keys(value).map(child => `${key}.${child}`) : [key]).sort()
    for (const locale of resources.locales) expect(keys(messages[locale])).toEqual(keys(raw[locale]))
  })

  it('分包以业务命名空间接入，尚未翻译的语种使用中文兜底', () => {
    for (const locale of resources.locales) {
      const source = locale === 'en' ? raw.en : raw.zh
      expect(i18n.global.t('workflowNavigation.dedupe', {}, { locale })).toBe(source.workflowNavigation.dedupe)
      expect(i18n.global.t('navigationContent.geoDetails.leadsCaptured', {}, { locale }))
        .toBe(source.navigationContent.geoDetails.leadsCaptured)
    }
  })
})
