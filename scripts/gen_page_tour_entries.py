#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""由「页面 + 选择器」清单生成页面导览注册表片段（TS）与批次 JSON。

只做**机械拼装**：文案是人写的，这里负责把
  「路由 → pageKey → 两个锚点选择器」
拼成
  1. src/config/onboarding/pageTours.ts 里 createPageTours() 的条目
  2. 批次 JSON 里该页的 summary / step2

为什么要脚本：注册表条目和 9 个语言包的词条是**一一对应**的，
手抄 25 页 × 2 步的键名必然抄错，而抄错一个键的后果是该语言的整条导览
退化成「显示原始 key」——这类错误肉眼很难发现。
"""
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from path_guard import safe_resolve  # noqa: E402  路径闸：src/dst 不得越出仓库

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# route -> (pageKey, step1 选择器, step2 选择器)
GEO = {
    '/geo-tools/brand-config':        ('geoBrandConfig',      '.geo-brand-config .card-header',        '.geo-brand-config .code-block'),
    '/geo-tools/seo-infra':           ('geoSeoInfra',         '.seo-infra .el-tabs__header',          '.seo-infra .code'),
    '/geo-tools/pusher-config':       ('geoPusherConfig',     '.geo-pusher-config .card-header',      '.geo-pusher-config .el-table'),
    '/geo-tools/site-config':         ('geoSiteConfig',       '.geo-site-config .card-header',        '.geo-site-config .el-table'),
    '/geo-tools/schema-templates':    ('geoSchemaTemplates',  '.geo-schema-templates .card-header',   '.geo-schema-templates .preview'),
    '/geo-tools/competitors':         ('geoCompetitorManage', '.el-table',                            '.el-dialog'),
    '/geo-tools/config':              ('geoConfigOptimizer',  '.geo-page .form-card',                 '.geo-page .el-form'),
    '/geo-tools/keyword-mining':      ('geoKeywordMining',    '.geo-page .search-card',               '.geo-page .el-table'),
    '/geo-tools/content-creation':    ('geoContentCreation',  '.geo-page .form-card',                 '.geo-page .compare-card'),
    '/geo-tools/content-optimize':    ('geoContentOptimize',  '.geo-page .input-card',                '.geo-page .compare-card'),
    '/geo-tools/site-publish':        ('geoSitePublish',      '.site-publish .card-header',           '.site-publish .el-table'),
    '/geo-tools/push-center':         ('geoPushCenter',       '.push-center .quota-card',             '.push-center .el-table'),
    '/geo-tools/platform-publish':    ('geoPlatformPublish',  '.geo-page .el-card',                   '.geo-page .el-table'),
    '/geo-tools/workflow':            ('geoWorkflowEditor',   '.geo-page .el-card',                   '.geo-page .el-form'),
    '/geo-tools/knowledge-base':      ('geoKnowledgeBase',    '.geo-page .el-card',                   '.geo-page .el-table'),
    '/geo-tools/entity-graph':        ('geoEntityGraph',      '.geo-page .el-card',                   '.geo-page .el-table'),
    '/geo-tools/funnel-dashboard':    ('geoFunnelDashboard',  '.funnel-dashboard .card-header',        '.funnel-dashboard .funnel-row'),
    '/geo-tools/index-tracking':      ('geoIndexTracking',    '.index-tracking .card-header',         '.index-tracking .el-table'),
    '/geo-tools/visibility':          ('geoVisibilityBoard',  '.stat-block',                          '.el-table'),
    '/geo-tools/sov-board':           ('geoSovBoard',         '.geo-page .el-card',                   '.geo-page .el-table'),
    '/geo-tools/crawler-stats':       ('geoCrawlerStats',     '.coverage-card',                       '.el-table'),
    '/geo-tools/verification':        ('geoVerification',     '.geo-page .form-card',                 '.geo-page .result-card'),
    '/geo/decision-report':           ('geoDecisionReport',   '.geo-page .el-card',                   '.geo-page .el-table'),
    '/geo-tools/reports':             ('geoReports',          '.geo-page .filter-card',               '.geo-page .el-table'),
    '/geo-tools/alerts':              ('geoAlertCenter',      '.geo-page .el-card',                   '.geo-page .el-table'),
}


def build_registry_entries():
    out = []
    for route, (page_key, sel1, sel2) in GEO.items():
        out.append(f"""    '{route}': {{
      route: '{route}',
      pageKey: '{page_key}',
      steps: [
        {{
          element: '{sel1}',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.{page_key}.summary'),
          side: 'bottom',
          align: 'center'
        }},
        {{
          element: '{sel2}',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.{page_key}.step2.description'),
          side: 'top',
          align: 'center'
        }}
      ]
    }},""")
    return '\n'.join(out)


def main():
    mode = sys.argv[1]
    if mode == 'registry':
        print(build_registry_entries())
        return
    if mode == 'strip-titles':
        # 把批次 JSON 里的 step2.title 去掉（第 2 步标题改用共享的 common.action）
        # src/dst 来自命令行：realpath+commonpath 必须留在仓库根内。
        src = safe_resolve(sys.argv[2], ROOT)
        dst = safe_resolve(sys.argv[3], ROOT)
        d = json.load(open(src, encoding='utf-8'))
        for lang, pages in d.items():
            for pk, entry in pages.items():
                if 'step2' in entry:
                    entry['step2'].pop('title', None)
        json.dump(d, open(dst, 'w', encoding='utf-8'), ensure_ascii=False, indent=2)
        print(f'{dst}: 已去掉 step2.title（改用共享键 common.action）')
        return
    raise SystemExit(__doc__)


if __name__ == '__main__':
    main()
