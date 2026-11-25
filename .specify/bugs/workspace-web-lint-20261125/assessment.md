# Bug Assessment: 工作区提交前端 lint 失败

- **Slug**: workspace-web-lint-20261125
- **Created**: 2026-11-25
- **Source**: 工作区整体提交前执行 `npm run lint:check`
- **Verdict**: valid
- **Severity**: low

## Symptom and Reproduction

在 `user-web/` 执行 `npm run lint:check`，退出码 1：1 error、990 warnings。
错误为 `customerImportJob/List.vue` 的 `preserve-caught-error`；警告均为 Vue 模板排版规则。
原始日志保存在工作区 `.planning/2026-11-25-workspace-commit/user-web-lint.log`。

## Root Cause

`validateMappingJson` 捕获 JSON 解析异常后重新抛出 Error，却未保留原始 cause。
11 个页面的属性换行、内容换行、缩进和引号不符合当前 eslint 配置。

## Proposed Remediation

重抛异常时补 `{ cause: e }`；对日志列出的页面运行现有 ESLint 自动排版。
不修改 lint 配置，不改变接口或业务流程。

## Files likely to change

均相对于 `user-web/src/views/`：`aiInsightDraft/List.vue`、`businessAccountAuth/List.vue`、
`channels/account-manage.vue`、`clue/List.vue`、`clue/Statistics.vue`、`crm/Dedupe.vue`、
`crossAccountSearch/List.vue`、`customer360/List.vue`、`customerImportJob/List.vue`、
`inbox/List.vue`、`leadMining/Index.vue`。

## Tests to add or update

本次为异常元数据与排版修正，不新增重复实现的测试。重新执行 lint（含 warning 上限）和现有前端全量测试。

## Risks and Considerations

自动修复后检查 script 区块与业务表达式，防止排版修复意外改变逻辑。
