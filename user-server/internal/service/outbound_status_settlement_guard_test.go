package service

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

// 出站行的状态由「谁投的、投没投成」这一侧决定，而不是靠事后补写：
//   - 直接投递渠道（telegram / feishu / whatsapp / qq / email）：调用平台发送 API 成功之后
//     才写 message_hub，所以落库那一刻就是终态 delivered；留在 pending 会让任何按
//     status='delivered' 统计的口径（如 monitor 的出站送达计数）永远看不见这批真发出去的消息。
//   - 桥接渠道（douyin 等五家）：pending 是「等扩展来认领」的工作队列状态，行由 outbox ack 收口，
//     写入时绝不能预置终态。
//   - 先落库后发送的渠道：必须留 pending，否则发送被跳过/失败时行已被谎称送达。
//
// 本守卫按「函数」而不是按「行号」认站点，改行号不会让它误判；把某个站点的 Status 摘掉、
// 或给先落库后发送的渠道预置终态，都会当场红。

type pushSite struct {
	file     string
	recvType string
	fn       string
}

// settledSites 发送已确认成功后才落库的出站站点，PushMessageRequest 必须声明 delivered。
var settledSites = []pushSite{
	{"feishu.go", "FeishuIntegrationService", "sendMessageTyped"},
	{"feishu.go", "TelegramIntegrationService", "SendMessageWithReceipt"},
	{"feishu.go", "TelegramIntegrationService", "SendCard"},
	{"feishu.go", "WhatsAppCloudIntegrationService", "SendMessageWithTemplate"},
	{"qq_account.go", "QQIntegrationService", "SendMessage"},
	{"email.go", "EmailService", "Send"},
}

// pendingSites 先落库后发送（且可能被整段跳过）的出站站点，必须不声明状态。
var pendingSites = []pushSite{
	{"wecom_integration.go", "WeComIntegrationService", "SendMessage"},
}

// hubPushLiterals 取出某个函数体里所有 PushMessageRequest 字面量的键值形状。
//
// 只认键名与字面量值，不读整棵语法树之外的上下文——站点身份由「函数名 + 方向」确定。
func hubPushLiterals(decl *ast.FuncDecl) []map[string]string {
	var out []map[string]string
	ast.Inspect(decl, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if id, ok := lit.Type.(*ast.Ident); !ok || id.Name != "PushMessageRequest" {
			return true
		}
		fields := map[string]string{}
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			fields[key.Name] = exprString(kv.Value)
		}
		out = append(out, fields)
		return true
	})
	return out
}

func exprString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			if s, err := strconv.Unquote(v.Value); err == nil {
				return s
			}
		}
		return v.Value
	case *ast.SelectorExpr:
		return exprString(v.X) + "." + v.Sel.Name
	case *ast.Ident:
		return v.Name
	case *ast.CallExpr:
		return exprString(v.Fun) + "()"
	default:
		return fmt.Sprintf("<%T>", e)
	}
}

func TestOutboundStatusSettledAtSendSuccessSites(t *testing.T) {
	fset := token.NewFileSet()
	parsed := map[string]*ast.File{}
	parse := func(file string) (*ast.File, error) {
		if f, ok := parsed[file]; ok {
			return f, nil
		}
		path := filepath.Join(".", file)
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("解析 %s: %w", path, err)
		}
		parsed[file] = f
		return f, nil
	}

	check := func(site pushSite, wantStatus string) error {
		f, err := parse(site.file)
		if err != nil {
			return err
		}
		var matched []*ast.FuncDecl
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != site.fn || fn.Body == nil {
				continue
			}
			if recvTypeName(fn) != site.recvType {
				continue
			}
			matched = append(matched, fn)
		}
		if len(matched) != 1 {
			return fmt.Errorf("%s 里 %s.%s 应恰好命中 1 个函数，实得 %d（守卫的站点身份已失效，须同步本表）",
				site.file, site.recvType, site.fn, len(matched))
		}
		lits := hubPushLiterals(matched[0])
		if len(lits) != 1 {
			return fmt.Errorf("%s.%s 里应恰好有 1 个 PushMessageRequest 字面量，实得 %d",
				site.file, site.fn, len(lits))
		}
		lit := lits[0]
		if lit["Direction"] != "outbound" {
			return fmt.Errorf("%s.%s 的 Direction 期望 outbound，实得 %q（站点认错了）",
				site.file, site.fn, lit["Direction"])
		}
		if lit["Status"] != wantStatus {
			return fmt.Errorf("%s.%s 的 Status 期望 %q，实得 %q", site.file, site.fn, wantStatus, lit["Status"])
		}
		return nil
	}

	for _, site := range settledSites {
		if err := check(site, "model.BridgeAckStatusDelivered"); err != nil {
			t.Error(err)
		}
	}
	for _, site := range pendingSites {
		// 未声明：字面量里没有 Status 键，exprString 的缺键读回空串。
		if err := check(site, ""); err != nil {
			t.Error(err)
		}
	}
}

func recvTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	switch rt := fn.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if id, ok := rt.X.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.Ident:
		return rt.Name
	}
	return ""
}
