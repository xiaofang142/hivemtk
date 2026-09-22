# i18n 翻译协作指南 (TRANSLATOR_GUIDE)

> **任务编号**: OPT-MISC-03
> **创建日期**: 2026-08-16
> **适用版本**: user-web v0.3+
> **维护者**: 走本仓 issue（GitHub `xiaofang142/hivemtk` / Gitee `xhpmayun/hivemtk`）。

## 一、目的

为出海客服场景, 在现有 4 语言 (`zh` / `en` / `ja` / `ar`) 基础上, 新增 5 种欧洲/南美语种:

| 语种 | 代码 | 适用市场 | 优先级 |
|---|---|---|---|
| 西班牙语 | `es` | 拉美 (墨西哥/阿根廷/智利) | P0 |
| 法语 | `fr` | 欧洲/非洲法语区 | P0 |
| 德语 | `de` | DACH 区域 | P1 |
| 俄语 | `ru` | 俄语区 (RU/CIS) | P1 |
| 葡萄牙语 | `pt` | 巴西/葡萄牙 | P0 |

## 二、文件结构

```
src/i18n/
├── index.js           # i18n 实例(无需修改)
├── locale.js          # 语言列表(需添加 5 个 code)
└── locales/
    ├── zh.json        # 基准 (勿改)
    ├── en.json        # 基准 (勿改)
    ├── ja.json
    ├── ar.json
    ├── es.json        # 新增 (placeholder = en.json 复制)
    ├── fr.json        # 新增
    ├── de.json        # 新增
    ├── ru.json        # 新增
    └── pt.json        # 新增
```

## 三、翻译步骤 (给译者的 checklist)

1. **复制** `en.json` 到目标语言文件, 文件头加注释 (模板见下文)
2. **逐项翻译** value 部分, **不要修改 key**
3. **保留 ICU 占位符**: `{name}` / `{0}` / `Plural(count)` 等
4. **保留 HTML 标签**: `<br>` / `<span class="x">` / 等
5. **保持术语一致**: 同一概念在所有 key 中用同一译法
6. **运行 CI 校验**:
   ```bash
   node scripts/i18n-extract.cjs --base zh
   ```
   退出码 0 = 通过。

## 四、文件头注释模板 (每个 locale 文件第一行)

```json
{
  "_comment": "TRANSLATION REQUIRED — 请由 [translator-name] 翻译。原语言: en.json。严禁修改 key。完成日期: ____",
  ...
}
```

## 五、locale.js 需补充项 (前端维护者)

```js
export const SUPPORTED_LOCALES = [
  { code: 'zh', label: '简体中文', el: 'zh-cn', rtl: false },
  { code: 'en', label: 'English',   el: 'en',    rtl: false },
  { code: 'ja', label: '日本語',     el: 'ja',    rtl: false },
  { code: 'ar', label: 'العربية',   el: 'en',    rtl: true  },
  // --- OPT-MISC-03 新增 ---
  { code: 'es', label: 'Español',  el: 'es',    rtl: false },
  { code: 'fr', label: 'Français', el: 'fr',    rtl: false },
  { code: 'de', label: 'Deutsch',  el: 'de',    rtl: false },
  { code: 'ru', label: 'Русский',  el: 'ru',    rtl: false },
  { code: 'pt', label: 'Português',el: 'pt',    rtl: false },
]
```

并补充 `getStoredLocale()` 中 5 个 `if (base === 'xx') return 'xx'`

## 六、术语表 (Terminology)

| EN | ES | FR | DE | RU | PT |
|---|---|---|---|---|---|
| Customer | Cliente | Client | Kunde | Клиент | Cliente |
| Merchant | Comercio | Commerçant | Händler | Продавец | Comerciante |
| Conversation | Conversación | Conversation | Gespräch | Диалог | Conversa |
| AI Agent | Agente IA | Agent IA | KI-Agent | ИИ-агент | Agente IA |
| Knowledge Base | Base de conocimientos | Base de connaissances | Wissensdatenbank | База знаний | Base de conhecimento |
| SOP | SOP | SOP | SOP | SOP | SOP (不译) |
| Short Link | Enlace corto | Lien court | Kurzlink | Короткая ссылка | Link curto |
| Funnel | Embudo | Entonnoir | Trichter | Воронка | Funil |

## 七、参考

- 现有 `ar.json` (1800+ keys) 作为最复杂的多语言参考
- Element Plus 语言包路径: `node_modules/element-plus/es/locale/lang/`
- ICU 语法: https://formatjs.io/docs/intl-messageformat/

## 八、变更记录

| 日期 | 版本 | 变更 |
|---|---|---|
| 2026-08-16 | v0.1 | 初始创建 (OPT-MISC-03) |
