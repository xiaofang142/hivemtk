import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { readFileSync } from 'node:fs';

const languages = ['ar', 'de', 'en', 'es', 'fr', 'ja', 'pt', 'ru', 'zh'];

beforeEach(() => {
  vi.resetModules();
  localStorage.removeItem('app_locale');
});
afterEach(() => {
  localStorage.removeItem('app_locale');
  document.documentElement.removeAttribute('dir');
  document.documentElement.removeAttribute('lang');
  vi.restoreAllMocks();
});

describe('语言目录兼容契约', () => {
  describe.each(languages)('已保存的%s语言', code => {
    let i18n;
    beforeEach(async () => {
      // 写入存储后才初始化模块，冷加载属于准备阶段；不以切换语言伪造同步启动。
      localStorage.setItem('app_locale', code);
      ({ default: i18n } = await import('../../src/i18n/index.js'));
    });
    it('同步启动且全部语言保持可用', async () => {
      expect(i18n.global.locale.value).toBe(code);
      expect(i18n.global.fallbackLocale.value).toBe('zh');
      expect([...i18n.global.availableLocales].sort()).toEqual(languages);
      expect(document.documentElement.getAttribute('dir')).toBe(code === 'ar' ? 'rtl' : 'ltr');
      expect(document.documentElement.getAttribute('lang')).toBe(code);
      const { loadLocaleMessages } = await import('../../src/i18n/locale.js');
      expect(await loadLocaleMessages(code)).toEqual(i18n.global.getLocaleMessage(code));
      expect(await loadLocaleMessages(code)).toBeNull();
    });
  });

  it('未知语言仍回退中文且方向切换不依赖加载请求', async () => {
    localStorage.setItem('app_locale', 'not-supported');
    vi.spyOn(navigator, 'language', 'get').mockReturnValue('zz-ZZ');
    const { default: i18n } = await import('../../src/i18n/index.js');
    const { setLocale, loadLocaleMessages } = await import('../../src/i18n/locale.js');
    expect(i18n.global.locale.value).toBe('zh');
    expect(await loadLocaleMessages('not-supported')).toBeNull();
    expect(setLocale('ar')).toBe('ar');
    expect(document.documentElement.getAttribute('dir')).toBe('rtl');
    expect(setLocale('not-supported')).toBe('zh');
    expect(document.documentElement.getAttribute('dir')).toBe('ltr');
    expect(localStorage.getItem('app_locale')).toBe('zh');
  });

  it('加载与详情不再对已静态依赖的模块使用动态导入', () => {
    const locale = readFileSync('src/i18n/locale.js', 'utf8');
    const glossary = readFileSync('src/views/glossary/List.vue', 'utf8');
    expect(/import\s*\(\s*[`'"]\.\/locales\//.test(locale)).toBe(false);
    expect(/import\s*\(\s*['"]@\/api\/glossary\.js/.test(glossary)).toBe(false);
  });
});
