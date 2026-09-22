function build(K){
  const z={},e={},j={},a={}
  for(const k in K){z[k]=k;e[k]=K[k][0];j[k]=K[k][1];a[k]=K[k][2]}
  return {zh:z,en:e,ja:j,ar:a}
}

export default build({
  '首页':['Home','ホーム','الرئيسية'],
  '高客单服务':['High-ticket service','高客単価サービス','خدمة باهظة الثمن'],
  '抖音/快手/小红书/闲鱼/TikTok/企微/邮件/Telegram/WhatsApp/短信':['Douyin/Kuaishou/Xiaohongshu/Xianyu/TikTok/WeCom/Email/Telegram/WhatsApp/SMS','Douyin/Kuaishou/Xiaohongshu/Xianyu/TikTok/WeCom/Email/Telegram/WhatsApp/SMS','Douyin/Kuaishou/Xiaohongshu/Xianyu/TikTok/WeCom/Email/Telegram/WhatsApp/SMS'],
  'Telegram 核心能力':['Telegram core capabilities','Telegram コア機能','قدرات Telegram الأساسية'],
  'Bot 收发 · 群线索挖掘 · 主动 DM 触达 · 群消息分析 · 入群管控':['Bot send/receive · group lead mining · proactive DM outreach · group message analysis · join control','Bot 送受信・グループリード抽出・DM 能動コンタクト・グループメッセージ分析・参加制御','Bot إرسال/استقبال · استخراج عملاء من المجموعات · تواصل DM استباقي · تحليل رسائل المجموعات · تحكم بالانضمام'],
  'Telegram 群增长引擎':['Telegram group growth engine','Telegram グループ成長エンジン','محرك نمو مجموعات Telegram'],
  '外贸获客':['Cross-border customer acquisition','越境顧客獲得','اكتساب عملاء عبر الحدود'],
  '加密/Web3':['Crypto/Web3','暗号資産/Web3','Crypto/Web3'],
  '社群运营':['Community operations','コミュニティ運営','إدارة المجتمعات'],
  '多少钱':['How much','いくらですか','بكم'],
  '怎么买':['How to buy','どうやって買いますか','كيف أشتري'],
  '求购':['Want to buy','購入希望','أريد الشراء'],
  '合作':['Cooperation','提携','تعاون'],
  '内置群增长完整闭环：① Bot 协议直连（Webhook/Polling 双模式兜底）→ ② 群发言静默打分（中英双语 + 联系方式信号，0-100）→ ③ 高意向（score≥60）自动触发个性化 DM 触达 → ④ 群消息全量入库 + AI 智能回复 → ⑤ 入群管控强制用户先 /start 打通 DM 前置。':['Built-in closed-loop group growth: ① Bot protocol direct (Webhook/Polling dual-mode fallback) → ② Silent scoring of group posts (bilingual + contact signals, 0-100) → ③ Auto-trigger personalized DM outreach for high intent (score≥60) → ④ All group messages stored + AI smart replies → ⑤ Join control forces users to /start first to unlock DM.','グループ成長の完全クローズドループ：① Bot プロトコル直結（Webhook/Polling デュアルモードフォールバック）→ ② グループ投稿のサイレントスコアリング（日英バイリンガル + 連絡先シグナル、0-100）→ ③ 高意向（score≥60）で個別化 DM 能動コンタクトを自動トリガー → ④ グループメッセージ全量保存 + AI スマート返信 → ⑤ 参加制御でユーザーに /start を強制し DM の前段階を開放。','حلقة نمو مجموعات مغلقة مضمنة: ① اتصال مباشر عبر بروتوكول Bot (Webhook/Polling مع وضع احتياطي ثنائي) → ② تقييم صامت لمنشورات المجموعة (ثنائي اللغة + إشارات جهة اتصال، 0-100) → ③ إطلاق تلقائي لتواصل DM مخصص للنوايا العالية (score≥60) → ④ تخزين كامل لرسائل المجموعة + ردود ذكية بالذكاء الاصطناعي → ⑤ تحكم بالانضمام يفرض على المستخدمين /start أولاً لفتح DM.'],
  '推荐方案 B（反向代理 终止 TLS + frpc=http），与已有宝塔/反向代理 环境兼容性最好。':['Recommended Plan B (reverse proxy terminates TLS + frpc=http) — best compatibility with existing BaoTa/reverse proxy environments.','推奨ソリューション B（リバースプロキシが TLS 終端 + frpc=http）。既存の宝塔/リバースプロキシ環境との互換性が最良。','الخطة B الموصى بها (الوكيل العكسي ينهي TLS + frpc=http) - أفضل توافق مع بيئات BaoTa/الوكيل العكسي الحالية.'],
  '；反向代理 端':['; reverse proxy side','；リバースプロキシ側','؛ جانب الوكيل العكسي'],
  '2. 云端反代 TLS 终止（方案 B）':['2. Cloud reverse proxy TLS termination (Plan B)','2. クラウドリバースプロキシ TLS 終端（方案 B）','2. إنهاء TLS بالوكيل العكسي السحابي (الخطة B)'],
  '10+ 渠道打透 · AI 真自主 · 数据封死在域内':['10+ channels covered · genuinely autonomous AI · data sealed in-domain','10 以上のチャネル貫通・AI 真の自律・データはドメイン内封印','أكثر من 10 قنوات مغطاة · ذكاء اصطناعي ذاتي حقًا · بيانات محبوسة داخل النطاق'],
  'HiveMTK 10+ 渠道打透 + AI 真自主 + 数据封死在域内，落地私域最常被问到的问题一次说清。':['HiveMTK covers 10+ channels end-to-end, genuinely autonomous AI, data sealed inside your domain — the most-asked questions about private-domain rollout, answered once and clearly.','HiveMTK 10 以上のチャネル貫通、真に自律する AI、データはドメイン内完結——私域運用でよく聞かれる疑問に一度で明快にお答えします。','يغطي HiveMTK أكثر من 10 قنوات كاملة، وذكاء اصطناعي ذاتي حقًا، وبيانات محبوسة داخل نطاقك — أكثر أسئلة القطاع الخاص شيوعًا نجيب عنها دفعة واحدة بوضوح.'],
  '抖音/快手/小红书/闲鱼/TikTok/企微/邮件/Telegram/WhatsApp/短信 等 10+ 社媒渠道':['10+ social channels — Douyin/Kuaishou/Xiaohongshu/Xianyu/TikTok/WeCom/Email/Telegram/WhatsApp/SMS','10 以上のソーシャルチャネル：Douyin/Kuaishou/Xiaohongshu/Xianyu/TikTok/WeCom/Email/Telegram/WhatsApp/SMS','أكثر من 10 قنوات اجتماعية — Douyin/Kuaishou/Xiaohongshu/Xianyu/TikTok/WeCom/Email/Telegram/WhatsApp/SMS'],
})
