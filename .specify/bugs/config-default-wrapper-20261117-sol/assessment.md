# 私有默认值与Raw外层回落识别

Verdict: valid / medium。

## 证据及决策

最新audit把四个webhook Raw读取的0误当有效默认值。实际同文件外层明确以正数判断回落30秒、60秒，或positiveIntOr回落100、180。保留Raw0及运行时默认契约，不改种子、不降低覆盖率基线、不加键名豁免。静态门只新增可证明的同文件直接返回positiveIntOr/positiveDurationOr和正数if-return回落形态；多处不一致或形态无法证明仍拒绝推断。

## Files likely to change

- `scripts/check-config-param-defaults.py`
- 新` scripts/test_config_param_defaults.py`（路径无前导空格）
- 本目录fix/test及主进度

## 验证

先用Python合成Go源码验证四种真实包装及不一致/未受保护的反向例，原逻辑必须失败；再运行完整默认值门。没有生产配置或测试库写入。
