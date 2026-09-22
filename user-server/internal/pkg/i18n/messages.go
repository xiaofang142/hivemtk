package i18n

import "strconv"

// ErrorMessages 错误码 -> 四语消息。键使用 error_code.go 中的 ErrorCode 字符串值。
// 后端绝大部分 API 响应都走错误码注册表，本地化此处即可覆盖绝大多数业务提示。
var ErrorMessages = map[string]map[Locale]string{
	"SUCCESS": {
		ZH: "成功", EN: "Success", JA: "成功しました", AR: "نجاح",
	},

	"UNKNOWN_1000": {
		ZH: "未知错误", EN: "Unknown error", JA: "不明なエラー", AR: "خطأ غير معروف",
	},
	"INVALID_PARAM_1001": {
		ZH: "参数无效", EN: "Invalid parameters", JA: "パラメータが無効です", AR: "المعلمات غير صالحة",
	},
	"NOT_FOUND_1002": {
		ZH: "资源不存在", EN: "Resource not found", JA: "リソースが見つかりません", AR: "المورد غير موجود",
	},
	"ALREADY_EXISTS_1003": {
		ZH: "资源已存在", EN: "Resource already exists", JA: "リソースは既に存在します", AR: "المورد موجود بالفعل",
	},
	"TIMEOUT_1004": {
		ZH: "请求超时", EN: "Request timeout", JA: "リクエストがタイムアウトしました", AR: "انتهت مهلة الطلب",
	},

	"UNAUTHORIZED_2001": {
		ZH: "未授权访问", EN: "Unauthorized", JA: "認証が必要です", AR: "غير مصرح",
	},
	"FORBIDDEN_2002": {
		ZH: "拒绝访问", EN: "Access denied", JA: "アクセスが拒否されました", AR: "تم رفض الوصول",
	},
	"TOKEN_EXPIRED_2003": {
		ZH: "令牌已过期", EN: "Token expired", JA: "トークンの有効期限が切れています", AR: "انتهت صلاحية الرمز",
	},
	"TOKEN_INVALID_2004": {
		ZH: "令牌无效", EN: "Invalid token", JA: "トークンが無効です", AR: "الرمز غير صالح",
	},
	"API_KEY_INVALID_2005": {
		ZH: "API Key 无效", EN: "Invalid API key", JA: "APIキーが無効です", AR: "مفتاح API غير صالح",
	},
	"PERMISSION_DENIED_2007": {
		ZH: "权限不足", EN: "Insufficient permissions", JA: "権限が不足しています", AR: "صلاحيات غير كافية",
	},

	"DB_ERROR_3001": {
		ZH: "数据库错误", EN: "Database error", JA: "データベースエラー", AR: "خطأ في قاعدة البيانات",
	},
	"RECORD_NOT_FOUND_3002": {
		ZH: "记录不存在", EN: "Record not found", JA: "レコードが見つかりません", AR: "السجل غير موجود",
	},
	"DUPLICATE_ENTRY_3003": {
		ZH: "重复的记录", EN: "Duplicate record", JA: "重複するレコードです", AR: "سجل مكرر",
	},
	"FK_VIOLATION_3004": {
		ZH: "外键约束冲突", EN: "Foreign key constraint violation", JA: "外部キー制約違反", AR: "انتهاك قيد المفتاح الخارجي",
	},

	"VALIDATION_4001": {
		ZH: "验证失败", EN: "Validation failed", JA: "検証に失敗しました", AR: "فشل التحقق",
	},
	"REQUIRED_FIELD_4002": {
		ZH: "必填字段缺失", EN: "Required field missing", JA: "必須項目が不足しています", AR: "حقل مطلوب مفقود",
	},
	"INVALID_FORMAT_4003": {
		ZH: "格式无效", EN: "Invalid format", JA: "フォーマットが無効です", AR: "التنسيق غير صالح",
	},
	"INVALID_RANGE_4004": {
		ZH: "数值范围无效", EN: "Invalid value range", JA: "値の範囲が無効です", AR: "النطاق غير صالح",
	},

	"BUSINESS_5001": {
		ZH: "业务错误", EN: "Business error", JA: "ビジネスエラー", AR: "خطأ في العمل",
	},
	"RESOURCE_LOCKED_5002": {
		ZH: "资源已锁定", EN: "Resource locked", JA: "リソースはロックされています", AR: "المورد مقفل",
	},
	"INSUFFICIENT_QUOTA_5003": {
		ZH: "配额不足", EN: "Insufficient quota", JA: "割り当てが不足しています", AR: "الحصة غير كافية",
	},
	"OPERATION_FAILED_5004": {
		ZH: "操作失败", EN: "Operation failed", JA: "操作に失敗しました", AR: "فشلت العملية",
	},

	"SYSTEM_6001": {
		ZH: "系统错误", EN: "System error", JA: "システムエラー", AR: "خطأ في النظام",
	},
	"INTERNAL_ERROR_6002": {
		ZH: "内部错误", EN: "Internal error", JA: "内部エラー", AR: "خطأ داخلي",
	},
	"SERVICE_UNAVAILABLE_6003": {
		ZH: "服务不可用", EN: "Service unavailable", JA: "サービス利用不可", AR: "الخدمة غير متوفرة",
	},

	"FILE_TOO_LARGE_7001": {
		ZH: "文件过大", EN: "File too large", JA: "ファイルが大きすぎます", AR: "الملف كبير جدًا",
	},
	"INVALID_FILE_TYPE_7002": {
		ZH: "文件类型不支持", EN: "Unsupported file type", JA: "サポートされていないファイル形式", AR: "نوع الملف غير مدعوم",
	},
	"UPLOAD_FAILED_7003": {
		ZH: "上传失败", EN: "Upload failed", JA: "アップロードに失敗しました", AR: "فشل الرفع",
	},
}

// Messages 业务提示字典（错误码之外的通用文案，使用语义化 key）。
var Messages = map[string]map[Locale]string{
	"success": {
		ZH: "成功", EN: "Success", JA: "成功しました", AR: "نجاح",
	},
	"operation_success": {
		ZH: "操作成功", EN: "Operation successful", JA: "操作に成功しました", AR: "تمت العملية بنجاح",
	},
	"invalid_params": {
		ZH: "无效的请求参数", EN: "Invalid request parameters", JA: "無効なリクエストパラメータ", AR: "معلمات طلب غير صالحة",
	},
	"invalid_id_format": {
		ZH: "无效的 ID 格式", EN: "Invalid ID format", JA: "無効なID形式", AR: "تنسيق المعرّف غير صالح",
	},
	"id_mismatch": {
		ZH: "ID 不一致", EN: "ID mismatch", JA: "IDが一致しません", AR: "المعرّف غير متطابق",
	},
	"resource_not_exist": {
		ZH: "{0}不存在", EN: "{0} not found", JA: "{0}が見つかりません", AR: "{0} غير موجود",
	},
	"operation_failed": {
		ZH: "{0}失败", EN: "{0} failed", JA: "{0}に失敗しました", AR: "فشل {0}",
	},
	"record_not_found": {
		ZH: "记录不存在", EN: "Record not found", JA: "レコードが見つかりません", AR: "السجل غير موجود",
	},
	"db_operation_failed": {
		ZH: "数据库操作失败", EN: "Database operation failed", JA: "データベース操作に失敗しました", AR: "فشلت عملية قاعدة البيانات",
	},
}

// T 解析业务提示字典中的文案，支持 {0},{1}... 位置参数替换。
// 找不到 key 时返回 key 本身（便于发现遗漏）；找不到语言时回退中文。
func T(loc Locale, key string, args ...string) string {
	m, ok := Messages[key]
	if !ok {
		return key
	}
	s, ok := m[loc]
	if !ok || s == "" {
		s = m[ZH]
	}
	if len(args) > 0 {
		for i, a := range args {
			s = replaceAll(s, "{"+strconv.Itoa(i)+"}", a)
		}
	}
	return s
}

// ErrorMessage 返回错误码的四语消息（回退中文）。
func ErrorMessage(code string, loc Locale) string {
	if m, ok := ErrorMessages[code]; ok {
		if s, ok := m[loc]; ok && s != "" {
			return s
		}
		return m[ZH]
	}
	return code
}

func replaceAll(s, old, new string) string {
	if old == "" {
		return s
	}
	out := ""
	for {
		i := indexOf(s, old)
		if i < 0 {
			return out + s
		}
		out += s[:i] + new
		s = s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	n := len(sub)
	if n == 0 {
		return 0
	}
	for i := 0; i+n <= len(s); i++ {
		if s[i:i+n] == sub {
			return i
		}
	}
	return -1
}
