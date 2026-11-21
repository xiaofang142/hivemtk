# Bug Fix: 转化事件列表分页元数据归一

- Slug: api-ab-events-pagination-20261120
- Fixed: 2026-11-20
- Status: applied（验证进行中）
- Assessment: ./assessment.md

## Changes

GetConversionEvents 采用已有 utils.ParsePagination，默认1/10，上限200，超限钳制。归一参数同时用于查询和响应。新增真实测试库回归，覆盖负数、非数字、超限、合法第二页。

## Verification

- 双模式修复前curl：page=-1&page_size=-2→HTTP200/code0，响应仍为-1/-2；证据 `010-off-invalid-page.json` 和 `010-on-invalid-page.json`。
- 最初命名为red的测试命令因同机编译争用，源码编译发生在补丁应用后；结果PASS 18.401s，仅算修复后测试，**不算修复前RED**。
- 全控制器测试 PASS（91.280s），build与vet命令exit0。新进程HTTP回归正在进行。

## Deviations

修复前失败证据使用真实双模式curl；不虚报单测RED。
