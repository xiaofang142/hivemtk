const fs = require('fs');
const path = require('path');

const LOCALES_DIR = path.resolve(__dirname, '../src/i18n/locales');

const COMMON = {
  zh: {
    orderDraft: '订单草稿',
    salesWorkbench: '销售工作台',
    followups: '今日跟进',
    qq: 'QQ 机器人',
  },
  en: {
    orderDraft: 'Order Draft',
    salesWorkbench: 'Sales Workbench',
    followups: "Today's Follow-ups",
    qq: 'QQ Bot',
  },
  es: {
    orderDraft: 'Borrador de pedido',
    salesWorkbench: 'Banco de trabajo de ventas',
    followups: 'Seguimientos de hoy',
    qq: 'Bot de QQ',
  },
  fr: {
    orderDraft: 'Brouillon de commande',
    salesWorkbench: 'Poste de travail commercial',
    followups: 'Suivis du jour',
    qq: 'Robot QQ',
  },
  de: {
    orderDraft: 'Bestellentwurf',
    salesWorkbench: 'Vertriebsarbeitsplatz',
    followups: 'Heutige Nachfassungen',
    qq: 'QQ-Bot',
  },
  ja: {
    orderDraft: '注文ドラフト',
    salesWorkbench: '営業ワークベンチ',
    followups: '今日のフォローアップ',
    qq: 'QQ ボット',
  },
  ru: {
    orderDraft: 'Черновик заказа',
    salesWorkbench: 'Рабочий стол продаж',
    followups: 'Сегодняшние повторные обращения',
    qq: 'QQ-бот',
  },
  pt: {
    orderDraft: 'Rascunho de pedido',
    salesWorkbench: 'Banco de trabalho de vendas',
    followups: 'Acompanhamentos de hoje',
    qq: 'Bot do QQ',
  },
  ar: {
    orderDraft: 'مسودة طلب',
    salesWorkbench: 'مكتب المبيعات',
    followups: 'متابعات اليوم',
    qq: 'روبوت QQ',
  },
};

const GEO = {
  en: {
    geoConfigGroup: 'Configuration',
    geoBrandConfig: 'Brand & Domains',
    geoSeoInfra: 'SEO Infrastructure',
    geoPusherConfig: 'Crawler Push Config',
    geoSiteConfig: 'Static Site Deploy',
    geoSchemaTemplates: 'Schema Templates',
    geoSitePublish: 'Publish to Site',
    geoPushCenter: 'Crawler Push',
    geoFunnelDashboard: 'Funnel Overview',
    geoIndexTracking: 'Index & Citations',
  },
  es: {
    geoConfigGroup: 'Configuración',
    geoBrandConfig: 'Marca y dominios',
    geoSeoInfra: 'Infraestructura SEO',
    geoPusherConfig: 'Config. de push de rastreador',
    geoSiteConfig: 'Despliegue de sitio estático',
    geoSchemaTemplates: 'Plantillas Schema',
    geoSitePublish: 'Publicar en el sitio',
    geoPushCenter: 'Push de rastreador',
    geoFunnelDashboard: 'Vista de embudo',
    geoIndexTracking: 'Indexación y citas',
  },
  fr: {
    geoConfigGroup: 'Configuration',
    geoBrandConfig: 'Marque et domaines',
    geoSeoInfra: 'Infrastructure SEO',
    geoPusherConfig: "Config d'envoi des crawlers",
    geoSiteConfig: 'Déploiement de site statique',
    geoSchemaTemplates: 'Modèles Schema',
    geoSitePublish: 'Publier sur le site',
    geoPushCenter: "Envoi aux crawlers",
    geoFunnelDashboard: "Vue d'ensemble de l'entonnoir",
    geoIndexTracking: 'Indexation et citations',
  },
  de: {
    geoConfigGroup: 'Konfiguration',
    geoBrandConfig: 'Marke und Domains',
    geoSeoInfra: 'SEO-Infrastruktur',
    geoPusherConfig: 'Crawler-Push-Konfiguration',
    geoSiteConfig: 'Statische Website bereitstellen',
    geoSchemaTemplates: 'Schema-Vorlagen',
    geoSitePublish: 'Auf der Website veröffentlichen',
    geoPushCenter: 'Crawler-Push',
    geoFunnelDashboard: 'Trichterübersicht',
    geoIndexTracking: 'Indexierung und Zitate',
  },
  ja: {
    geoConfigGroup: '設定',
    geoBrandConfig: 'ブランドとドメイン',
    geoSeoInfra: 'SEOインフラ',
    geoPusherConfig: 'クローラープッシュ設定',
    geoSiteConfig: '静的サイトデプロイ',
    geoSchemaTemplates: 'Schemaテンプレート',
    geoSitePublish: 'サイトに公開',
    geoPushCenter: 'クローラープッシュ',
    geoFunnelDashboard: 'ファネル概要',
    geoIndexTracking: 'インデックスと引用',
  },
  ru: {
    geoConfigGroup: 'Конфигурация',
    geoBrandConfig: 'Бренд и домены',
    geoSeoInfra: 'SEO-инфраструктура',
    geoPusherConfig: 'Настройка отправки паукам',
    geoSiteConfig: 'Развертывание статического сайта',
    geoSchemaTemplates: 'Шаблоны Schema',
    geoSitePublish: 'Публикация на сайт',
    geoPushCenter: 'Отправка паукам',
    geoFunnelDashboard: 'Обзор воронки',
    geoIndexTracking: 'Индексация и цитирование',
  },
  pt: {
    geoConfigGroup: 'Configuração',
    geoBrandConfig: 'Marca e domínios',
    geoSeoInfra: 'Infraestrutura de SEO',
    geoPusherConfig: 'Config. de envio do rastreador',
    geoSiteConfig: 'Implantação de site estático',
    geoSchemaTemplates: 'Modelos de Schema',
    geoSitePublish: 'Publicar no site',
    geoPushCenter: 'Envio para rastreadores',
    geoFunnelDashboard: 'Visão geral do funil',
    geoIndexTracking: 'Indexação e citações',
  },
  ar: {
    geoConfigGroup: 'التكوين',
    geoBrandConfig: 'العلامة والنطاقات',
    geoSeoInfra: 'بنية SEO الأساسية',
    geoPusherConfig: 'إعداد دفع الزاحف',
    geoSiteConfig: 'نشر الموقع الثابت',
    geoSchemaTemplates: 'قوالب Schema',
    geoSitePublish: 'نشر على الموقع',
    geoPushCenter: 'دفع الزاحف',
    geoFunnelDashboard: 'نظرة عامة على القمع',
    geoIndexTracking: 'الفهرس والاقتباسات',
  },
};

function findMenuEnd(raw, menuStartIdx) {
  let i = raw.indexOf('{', menuStartIdx);
  let depth = 0;
  for (; i < raw.length; i++) {
    if (raw[i] === '{') depth++;
    else if (raw[i] === '}') {
      depth--;
      if (depth === 0) return i;
    }
  }
  throw new Error('menu block not closed');
}

function insertKeys(raw, keys) {
  const menuIdx = raw.indexOf('"menu": {');
  if (menuIdx === -1) throw new Error('menu key not found');
  const endIdx = findMenuEnd(raw, menuIdx);
  const before = raw.slice(0, endIdx).replace(/\s+$/, '');
  const after = raw.slice(endIdx);
  const indent = '    ';
  const lines = Object.entries(keys).map(([k, v]) => `${indent}"${k}": "${v}"`);
  const insert = lines.join(',\n') + '\n';
  return before + ',\n' + insert + '  ' + after;
}

for (const lang of Object.keys(COMMON)) {
  const file = path.join(LOCALES_DIR, lang + '.json');
  let raw = fs.readFileSync(file, 'utf8');
  const menu = JSON.parse(raw).menu;
  const toAdd = {};
  for (const k of Object.keys(COMMON[lang])) {
    if (!(k in menu)) toAdd[k] = COMMON[lang][k];
  }
  if (lang !== 'zh' && GEO[lang]) {
    for (const k of Object.keys(GEO[lang])) {
      if (!(k in menu)) toAdd[k] = GEO[lang][k];
    }
  }
  if (Object.keys(toAdd).length === 0) {
    console.log(lang + ': nothing to add');
    continue;
  }
  raw = insertKeys(raw, toAdd);
  fs.writeFileSync(file, raw);
  // verify valid JSON
  const parsed = JSON.parse(raw);
  const missing = Object.keys(toAdd).filter((k) => !(k in parsed.menu));
  console.log(lang + ': added ' + Object.keys(toAdd).length + ' keys' + (missing.length ? ', MISSING: ' + missing : ', JSON valid, all present'));
}
