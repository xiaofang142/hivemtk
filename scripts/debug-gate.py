# 本地调试：检查 msg_hourly_summary 系列模型在迁移注册表（scripts/check_model_migration.py）中的状态
import importlib.util, sys, io
sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding='utf-8')
spec = importlib.util.spec_from_file_location('gate', 'scripts/check_model_migration.py')
m = importlib.util.module_from_spec(spec)
try:
    spec.loader.exec_module(m)
except SystemExit:
    pass

models = m.collect_models()
registered = m.collect_registered()

for name in ['MsgHourlySummary', 'AggregationWatermark', 'MsgHourlySummaryRow', 'SessionAISummary']:
    print(f'{name}: 候选={name in models} 已注册={name in registered} 文件={models.get(name)}')

print()
print('--- 所有名字里带 Hourly/Summary 的候选 ---')
for k, v in sorted(models.items()):
    if 'Hourly' in k or 'Summary' in k or 'Aggregation' in k:
        print(f'  {k}  registered={k in registered}  ({v})')