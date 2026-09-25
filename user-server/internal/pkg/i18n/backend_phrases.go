package i18n

// BackendPhrases 以「中文原文」为键的业务提示四语词典。
// 仅收录真正返回给前端的响应 message（Success/Error 等传入的中文文案），
// 不含开发日志、测试断言、含 %v 的调试串。
// response 层在输出前用 Localize 按请求语言翻译；未收录的中文回退为原文（源语言）。
type Phrase struct {
	ZH string
	EN string
	JA string
	AR string
}

var BackendPhrases = map[string]Phrase{
	"ID 错误":                {ZH: "ID 错误", EN: "ID error", JA: "IDエラー", AR: "خطأ في المعرّف"},
	"SalesEngine 未注入":      {ZH: "SalesEngine 未注入", EN: "SalesEngine not injected", JA: "SalesEngine が注入されていません", AR: "لم يتم حقن SalesEngine"},
	"X-Chat-Visitor-Id 必填": {ZH: "X-Chat-Visitor-Id 必填", EN: "X-Chat-Visitor-Id is required", JA: "X-Chat-Visitor-Id は必須です", AR: "X-Chat-Visitor-Id مطلوب"},
	"app_key 不能为空":         {ZH: "app_key 不能为空", EN: "app_key cannot be empty", JA: "app_key は必須です", AR: "app_key لا يمكن أن يكون فارغًا"},
	"session_id 必填":        {ZH: "session_id 必填", EN: "session_id is required", JA: "session_id は必須です", AR: "session_id مطلوب"},
	"webhook_url 未配置":      {ZH: "webhook_url 未配置", EN: "webhook_url not configured", JA: "webhook_url が未設定です", AR: "webhook_url غير مُهيأ"},
	"上报成功":                 {ZH: "上报成功", EN: "Reported successfully", JA: "報告しました", AR: "تم الإبلاغ بنجاح"},
	"会话已关闭":                {ZH: "会话已关闭", EN: "Conversation closed", JA: "会話が終了しました", AR: "تم إغلاق المحادثة"},
	"会话已打开":                {ZH: "会话已打开", EN: "Conversation opened", JA: "会話が開始されました", AR: "تم فتح المحادثة"},
	"保存状态失败":               {ZH: "保存状态失败", EN: "Failed to save status", JA: "ステータス保存失敗", AR: "فشل حفظ الحالة"},
	"保存规则失败":               {ZH: "保存规则失败", EN: "Failed to save rule", JA: "ルール保存失敗", AR: "فشل حفظ القاعدة"},
	"保存规则成功":               {ZH: "保存规则成功", EN: "Rule saved successfully", JA: "ルールを保存しました", AR: "تم حفظ القاعدة بنجاح"},
	"保存账号失败":               {ZH: "保存账号失败", EN: "Failed to save account", JA: "アカウント保存失敗", AR: "فشل حفظ الحساب"},
	"停止失败":                 {ZH: "停止失败", EN: "Failed to stop", JA: "停止失敗", AR: "فشل التوقف"},
	"创建失败":                 {ZH: "创建失败", EN: "Creation failed", JA: "作成失敗", AR: "فشل الإنشاء"},
	"创建失败:":                {ZH: "创建失败:", EN: "Creation failed: ", JA: "作成失敗: ", AR: "فشل الإنشاء: "},
	"创建成功":                 {ZH: "创建成功", EN: "Created successfully", JA: "作成しました", AR: "تم الإنشاء بنجاح"},
	"创建模板成功":               {ZH: "创建模板成功", EN: "Template created successfully", JA: "テンプレートを作成しました", AR: "تم إنشاء القالب بنجاح"},
	"删除失败":                 {ZH: "删除失败", EN: "Deletion failed", JA: "削除失敗", AR: "فشل الحذف"},
	"删除成功":                 {ZH: "删除成功", EN: "Deleted successfully", JA: "削除しました", AR: "تم الحذف بنجاح"},
	"删除模板成功":               {ZH: "删除模板成功", EN: "Template deleted successfully", JA: "テンプレートを削除しました", AR: "تم حذف القالب بنجاح"},
	"删除账号失败":               {ZH: "删除账号失败", EN: "Failed to delete account", JA: "アカウント削除失敗", AR: "فشل حذف الحساب"},
	"刷新失败":                 {ZH: "刷新失败", EN: "Refresh failed", JA: "更新失敗", AR: "فشل التحديث"},
	"加载失败":                 {ZH: "加载失败", EN: "Failed to load", JA: "読み込み失敗", AR: "فشل التحميل"},
	"加载成功":                 {ZH: "加载成功", EN: "Loaded successfully", JA: "読み込みました", AR: "تم التحميل بنجاح"},
	"卡片不存在:":               {ZH: "卡片不存在:", EN: "Card does not exist: ", JA: "カードが存在しません: ", AR: "البطاقة غير موجودة: "},
	"参数不完整":                {ZH: "参数不完整", EN: "Incomplete parameters", JA: "パラメータが不完全です", AR: "معلمات غير مكتملة"},
	"参数缺失":                 {ZH: "参数缺失", EN: "Missing parameters", JA: "パラメータが不足しています", AR: "معلمات مفقودة"},
	"参数错误":                 {ZH: "参数错误", EN: "Invalid parameters", JA: "パラメータエラー", AR: "معلمات غير صحيحة"},
	"参数错误:":                {ZH: "参数错误:", EN: "Invalid parameters: ", JA: "パラメータエラー: ", AR: "معلمات غير صحيحة: "},
	"发送失败":                 {ZH: "发送失败", EN: "Failed to send", JA: "送信失敗", AR: "فشل الإرسال"},
	"发送成功":                 {ZH: "发送成功", EN: "Sent successfully", JA: "送信しました", AR: "تم الإرسال بنجاح"},
	"启动失败":                 {ZH: "启动失败", EN: "Failed to start", JA: "起動失敗", AR: "فشل التشغيل"},
	"商户ID不能为空":             {ZH: "商户ID不能为空", EN: "Merchant ID cannot be empty", JA: "加盟店 ID は必須です", AR: "معرف التاجر لا يمكن أن يكون فارغًا"},
	"安装态检查器未初始化":           {ZH: "安装态检查器未初始化", EN: "Installation status checker not initialized", JA: "インストール状態チェッカーが初期化されていません", AR: "مدقق حالة التثبيت غير مُهيأ"},
	"对象存储未配置":              {ZH: "对象存储未配置", EN: "Object storage not configured", JA: "オブジェクトストレージが未設定です", AR: "تخزين الكائنات غير مُهيأ"},
	"已为您转接人工客服":            {ZH: "已为您转接人工客服", EN: "Transferred you to a human agent", JA: "オペレーターに転送しました", AR: "تم تحويلك إلى موظف خدمة العملاء"},
	"已全部标记为已读":             {ZH: "已全部标记为已读", EN: "All marked as read", JA: "すべて既読にしました", AR: "تم تحديد الكل كمقروء"},
	"已更新":                  {ZH: "已更新", EN: "Updated", JA: "更新しました", AR: "تم التحديث"},
	"已标记为已读":               {ZH: "已标记为已读", EN: "Marked as read", JA: "既読にしました", AR: "تم تحديده كمقروء"},
	"已禁用":                  {ZH: "已禁用", EN: "Disabled", JA: "無効化しました", AR: "تم التعطيل"},
	"平台客户端未初始化,请检查 config/platform.yaml 配置": {ZH: "平台客户端未初始化,请检查 config/platform.yaml 配置", EN: "Platform client not initialized, please check config/platform.yaml", JA: "プラットフォームクライアントが初期化されていません。config/platform.yaml を確認してください", AR: "عميل المنصة غير مُهيأ، يرجى التحقق من config/platform.yaml"},
	"心跳上报失败:":        {ZH: "心跳上报失败:", EN: "Heartbeat report failed: ", JA: "ハートビート報告失敗: ", AR: "فشل إبلاغ نبض القلب: "},
	"心跳上报成功":         {ZH: "心跳上报成功", EN: "Heartbeat reported successfully", JA: "ハートビートを報告しました", AR: "تم الإبلاغ عن نبض القلب بنجاح"},
	"感谢您的评价":         {ZH: "感谢您的评价", EN: "Thank you for your feedback", JA: "ご評価ありがとうございます", AR: "شكرًا على تقييمك"},
	"挂载不存在":          {ZH: "挂载不存在", EN: "Mount does not exist", JA: "マウントが存在しません", AR: "نقطة التحميل غير موجودة"},
	"操作失败":           {ZH: "操作失败", EN: "Operation failed", JA: "操作に失敗しました", AR: "فشلت العملية"},
	"文件大小超出 20MB 限制": {ZH: "文件大小超出 20MB 限制", EN: "File size exceeds the 20MB limit", JA: "ファイルサイズが 20MB の制限を超えています", AR: "حجم الملف يتجاوز حد 20 ميجابايت"},
	"文件类型不被允许:":      {ZH: "文件类型不被允许:", EN: "File type not allowed: ", JA: "ファイル形式が許可されていません: ", AR: "نوع الملف غير مسموح: "},
	"无效的 id":         {ZH: "无效的 id", EN: "Invalid id", JA: "無効な id", AR: "معرف غير صحيح"},
	"无效的ID":          {ZH: "无效的ID", EN: "Invalid ID", JA: "無効な ID", AR: "معرف غير صحيح"},
	"无效的卡片ID":        {ZH: "无效的卡片ID", EN: "Invalid card ID", JA: "無効なカード ID", AR: "معرف البطاقة غير صحيح"},
	"无效的座席ID":        {ZH: "无效的座席ID", EN: "Invalid agent seat ID", JA: "無効な席 ID", AR: "معرف المقعد غير صحيح"},
	"无效的智能体ID":       {ZH: "无效的智能体ID", EN: "Invalid agent ID", JA: "無効なエージェント ID", AR: "معرف الوكيل الذكي غير صحيح"},
	"无效的用户ID":        {ZH: "无效的用户ID", EN: "Invalid user ID", JA: "無効なユーザー ID", AR: "معرف المستخدم غير صحيح"},
	"无效的账号ID":        {ZH: "无效的账号ID", EN: "Invalid account ID", JA: "無効なアカウント ID", AR: "معرف الحساب غير صحيح"},
	"无活跃会话":          {ZH: "无活跃会话", EN: "No active conversation", JA: "アクティブな会話がありません", AR: "لا توجد محادثة نشطة"},
	"智能体不存在":         {ZH: "智能体不存在", EN: "Agent does not exist", JA: "エージェントが存在しません", AR: "الوكيل الذكي غير موجود"},
	"智能体不存在或已禁用":     {ZH: "智能体不存在或已禁用", EN: "Agent does not exist or is disabled", JA: "エージェントが存在しないか無効です", AR: "الوكيل الذكي غير موجود أو معطّل"},
	"暂无消息":           {ZH: "暂无消息", EN: "No messages yet", JA: "メッセージはまだありません", AR: "لا توجد رسائل بعد"},
	"更新失败":           {ZH: "更新失败", EN: "Update failed", JA: "更新失敗", AR: "فشل التحديث"},
	"更新成功":           {ZH: "更新成功", EN: "Updated successfully", JA: "更新しました", AR: "تم التحديث بنجاح"},
	"更新模板成功":         {ZH: "更新模板成功", EN: "Template updated successfully", JA: "テンプレートを更新しました", AR: "تم تحديث القالب بنجاح"},
	"查询失败":           {ZH: "查询失败", EN: "Query failed", JA: "照会失敗", AR: "فشل الاستعلام"},
	"查询成功":           {ZH: "查询成功", EN: "Query successful", JA: "照会しました", AR: "تم الاستعلام بنجاح"},
	"标记成功":           {ZH: "标记成功", EN: "Marked successfully", JA: "マークしました", AR: "تم التحديد بنجاح"},
	"模板ID不能为空":       {ZH: "模板ID不能为空", EN: "Template ID cannot be empty", JA: "テンプレート ID は必須です", AR: "معرف القالب لا يمكن أن يكون فارغًا"},
	"模板未激活":          {ZH: "模板未激活", EN: "Template not activated", JA: "テンプレートが有効化されていません", AR: "القالب غير مُفعّل"},
	"注册 Webhook 失败":  {ZH: "注册 Webhook 失败", EN: "Failed to register Webhook", JA: "Webhook 登録失敗", AR: "فشل تسجيل Webhook"},
	"测试失败":           {ZH: "测试失败", EN: "Test failed", JA: "テスト失敗", AR: "فشل الاختبار"},
	"测试成功":           {ZH: "测试成功", EN: "Test successful", JA: "テストしました", AR: "نجح الاختبار"},
	"消息ID不能为空":       {ZH: "消息ID不能为空", EN: "Message ID cannot be empty", JA: "メッセージ ID は必須です", AR: "معرف الرسالة لا يمكن أن يكون فارغًا"},
	"消息已发送":          {ZH: "消息已发送", EN: "Message sent", JA: "メッセージを送信しました", AR: "تم إرسال الرسالة"},
	"添加到队列失败":        {ZH: "添加到队列失败", EN: "Failed to add to queue", JA: "キューへの追加失敗", AR: "فشلت إضافة إلى الطابور"},
	"渠道":             {ZH: "渠道", EN: "Channel", JA: "チャネル", AR: "القناة"},
	"渠道已创建":          {ZH: "渠道已创建", EN: "Channel created", JA: "チャネルを作成しました", AR: "تم إنشاء القناة"},
	"版本ID不能为空":       {ZH: "版本ID不能为空", EN: "Version ID cannot be empty", JA: "バージョン ID は必須です", AR: "معرف الإصدار لا يمكن أن يكون فارغًا"},
	"状态更新失败":         {ZH: "状态更新失败", EN: "Status update failed", JA: "ステータス更新失敗", AR: "فشل تحديث الحالة"},
	"状态更新成功":         {ZH: "状态更新成功", EN: "Status updated successfully", JA: "ステータスを更新しました", AR: "تم تحديث الحالة بنجاح"},
	"用户ID不能为空":       {ZH: "用户ID不能为空", EN: "User ID cannot be empty", JA: "ユーザー ID は必須です", AR: "معرف المستخدم لا يمكن أن يكون فارغًا"},
	"用户未认证":          {ZH: "用户未认证", EN: "User not authenticated", JA: "ユーザーが認証されていません", AR: "المستخدم غير مصادق"},
	"绑定不存在":          {ZH: "绑定不存在", EN: "Binding does not exist", JA: "バインディングが存在しません", AR: "الارتباط غير موجود"},
	"缺少 ext 参数":      {ZH: "缺少 ext 参数", EN: "Missing ext parameter", JA: "ext パラメータが不足しています", AR: "معلمة ext مفقودة"},
	"获取列表失败":         {ZH: "获取列表失败", EN: "Failed to fetch list", JA: "一覧取得失敗", AR: "فشل جلب القائمة"},
	"获取列表失败:":        {ZH: "获取列表失败:", EN: "Failed to fetch list: ", JA: "一覧取得失敗: ", AR: "فشل جلب القائمة: "},
	"获取成功":           {ZH: "获取成功", EN: "Fetched successfully", JA: "取得しました", AR: "تم الجلب بنجاح"},
	"获取日志失败":         {ZH: "获取日志失败", EN: "Failed to fetch logs", JA: "ログ取得失敗", AR: "فشل جلب السجلات"},
	"获取最新消息失败":       {ZH: "获取最新消息失败", EN: "Failed to fetch latest messages", JA: "最新メッセージ取得失敗", AR: "فشل جلب أحدث الرسائل"},
	"获取模板失败":         {ZH: "获取模板失败", EN: "Failed to fetch template", JA: "テンプレート取得失敗", AR: "فشل جلب القالب"},
	"获取模板成功":         {ZH: "获取模板成功", EN: "Template fetched successfully", JA: "テンプレートを取得しました", AR: "تم جلب القالب بنجاح"},
	"获取线索失败":         {ZH: "获取线索失败", EN: "Failed to fetch lead", JA: "リード取得失敗", AR: "فشل جلب العميل المحتمل"},
	"获取统计失败:":        {ZH: "获取统计失败:", EN: "Failed to fetch statistics: ", JA: "統計取得失敗: ", AR: "فشل جلب الإحصائيات: "},
	"获取规则失败":         {ZH: "获取规则失败", EN: "Failed to fetch rule", JA: "ルール取得失敗", AR: "فشل جلب القاعدة"},
	"获取账号列表失败":       {ZH: "获取账号列表失败", EN: "Failed to fetch account list", JA: "アカウント一覧取得失敗", AR: "فشل جلب قائمة الحسابات"},
	"获取账号列表成功":       {ZH: "获取账号列表成功", EN: "Account list fetched successfully", JA: "アカウント一覧を取得しました", AR: "تم جلب قائمة الحسابات بنجاح"},
	"解绑失败:":          {ZH: "解绑失败:", EN: "Unbind failed: ", JA: "解除失敗: ", AR: "فشل الفك: "},
	"评分必须在 1-5 之间":   {ZH: "评分必须在 1-5 之间", EN: "Rating must be between 1 and 5", JA: "評価は 1〜5 の間でなければなりません", AR: "يجب أن يكون التقييم بين 1 و 5"},
	"请求参数错误":         {ZH: "请求参数错误", EN: "Invalid request parameters", JA: "リクエストパラメータエラー", AR: "معلمات الطلب غير صحيحة"},
	"账号不存在":          {ZH: "账号不存在", EN: "Account does not exist", JA: "アカウントが存在しません", AR: "الحساب غير موجود"},
	"账号保存成功":         {ZH: "账号保存成功", EN: "Account saved successfully", JA: "アカウントを保存しました", AR: "تم حفظ الحساب بنجاح"},
}

// Localize 按请求语言翻译中文业务提示；未收录时回退为原文（中文）。
func Localize(loc Locale, zh string) string {
	if p, ok := BackendPhrases[zh]; ok {
		switch loc {
		case EN:
			return p.EN
		case JA:
			return p.JA
		case AR:
			return p.AR
		default:
			return p.ZH
		}
	}
	return zh
}
