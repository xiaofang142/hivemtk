# -*- coding: utf-8 -*-
"""api-contract 审计脚本：前端 api/*.js 调用 vs 后端 gin 路由注册 双向对照。

用法：python scripts/audit_api_contract.py
输出：UNMATCHED（前端调了后端不存在的路由）、BACKEND_NOT_CALLED（后端注册但前端未调，候选死接口/为外部客户端保留）。
"""
import re, os, glob, sys

# --strict：供 CI 使用。UNMATCHED（前端调了后端不存在的路由）与通配 key（解析失败）
# 都是**确定缺陷**，此时退出码 1；BACKEND_NOT_CALLED 只是候选清单（大量为
# 外部客户端/管理端保留接口），不参与判定。
STRICT = ('--strict' in sys.argv) or (os.environ.get('AUDIT_STRICT') == '1')

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
RDIRS = ['user-server/internal/router', 'user-server/internal/controller']

# ⚠️ 2026-09-16 审计（TOOL-06）：原为 `user-web/src/api` 且**只扫顶层 *.js**。
# 实测 src/ 下还有 **81 处 `http.get/post/...` 写在 .vue 组件里**（如
# `knowledge.vue` 直接 `http.put(\`/api/knowledge/connectors/${source}\`)`），
# 这些调用**从未进入过契约审计** —— 于是"断链 0"对它们毫无意义。
# 这正是 docs/architecture/BACKLOG_TODOLIST.md「断链 API ~200 个（前端已开发
# 后端未实现）」可能的来源：两边说的根本不是同一批调用。
#
# 现改为递归扫描整个 `user-web/src`，同时覆盖 .js 与 .vue。
WEB_ROOTS = [os.path.join(ROOT, 'user-web/src')]
WEB_EXT = ('.js', '.vue')
WEB_SKIP_DIRS = {'node_modules', 'dist', 'build', '.git'}


def iter_web_files():
    for base in WEB_ROOTS:
        for dirpath, dirnames, filenames in os.walk(base):
            dirnames[:] = [d for d in dirnames if d not in WEB_SKIP_DIRS]
            for fn2 in sorted(filenames):
                if fn2.endswith(WEB_EXT):
                    yield os.path.join(dirpath, fn2)

route_re = re.compile(r'(\w+)\.(GET|POST|PUT|DELETE|PATCH)\(\s*"([^"]*)"', re.ASCII)
group_re = re.compile(r'(\w+)\s*:?=\s*(\w+)\.Group\(\s*"([^"]*)"')
func_re = re.compile(r'func\s+(?:\((\w+)\s+\*?(\w+)\)\s+)?(\w+)\s*\(([^)]*)\)\s*\{')
rgtype = re.compile(r'\*gin\.RouterGroup\b')
m_re = re.compile(r"http\.(get|post|put|delete|patch)\(\s*([`'\"])([^`'\"]+)\2")
tpl = re.compile(r"\$\{[^}]+\}")
# 模块级字符串常量：const BASE = '/api/dingtalk-app/accounts'
# ⚠️ 2026-09-16 审计（TOOL-05）：原实现不解析这类常量，于是 `${BASE}/${id}` 经 tpl
# 替换后变成 `:param/:param` —— 一个**匹配任意同长度路由**的通配 key：
#   GET  :param/:param 命中 79 条后端路由、PUT 命中 8 条、DELETE 命中 3 条……
# 即这些前端调用**从未被真正校验过**（后端把该路由删掉它也照样"匹配"），
# 与此同时真实后端路由被反向误报进 BACKEND_NOT_CALLED 死接口清单。
const_re = re.compile(r"\b(?:const|let|var)\s+(\w+)\s*=\s*(['\"])([^'\"]+)\2")
# 裸常量实参：http.get(BASE, params) —— 实参不是字符串字面量，m_re 完全匹配不到
ident_re = re.compile(r"http\.(get|post|put|delete|patch)\(\s*([A-Za-z_$][\w$]*)\s*[,)]")
# ${NAME} 形式引用已知常量
tpl_name = re.compile(r"\$\{(\w+)\}")


def braces(src, start):
    depth = 0
    j = start
    while j < len(src):
        if src[j] == '{':
            depth += 1
        elif src[j] == '}':
            depth -= 1
            if depth == 0:
                break
        j += 1
    return start, j + 1


def analyze(dirs):
    sc_info = {}
    func_file = {}
    raw = {}
    for root in dirs:
        for fp in glob.glob(os.path.join(ROOT, root) + '/*.go'):
            if fp.endswith('_test.go'):
                continue
            src = open(fp, encoding='utf-8').read()
            fname = os.path.relpath(fp, os.path.join(ROOT, 'user-server')).replace('\\', '/')
            raw[fname] = src
            scopes = [(None, '<file>', src)]
            for m in func_re.finditer(src):
                recv = m.group(1)
                recvtype = m.group(2)
                name = m.group(3)
                params = [p.strip() for p in m.group(4).split(',') if p.strip()]
                rgn = []
                for i, p in enumerate(params):
                    if rgtype.search(p):
                        rgn.append((i, p.split()[0]))
                s, e = braces(src, m.end() - 1)
                qual = (recvtype + '.' + name) if recvtype else name
                scopes.append((recvtype, qual, src[s:e]))
                if rgn:
                    func_file.setdefault(qual, (fname, rgn))
            for recvtype, qual, body in scopes:
                calls = []
                for mm in re.finditer(r'\b(?:\w+\.)?((?:setup|Setup|Register|Mount)\w*)\s*\(', body):
                    calls.append((mm.group(1), get_args(body, mm.end()), None))
                for mm in re.finditer(r'\b(\w+)\.((?:Register|Mount)\w*)\s*\(', body):
                    calls.append((mm.group(2), get_args(body, mm.end()), mm.group(1)))
                # doReg/doRegAdmin 闭包直注册：doReg("GET","/path",...)
                extra_routes = []
                for mm in re.finditer(r'\bdoReg\w*\(\s*"(GET|POST|PUT|DELETE|PATCH)"\s*,\s*"([^"]*)"', body):
                    extra_routes.append(('auth', mm.group(1), mm.group(2)))
                # for-range 多组注册：for _, g := range []*gin.RouterGroup{A, B} { ... }
                # 块内 g.GET(...) 路由与 ctrl.Register(g) 调用按每组展开
                expanded_routes = []
                expanded_calls = []

                def _expand_range_loops(text, slice_defs):
                    out_routes = []
                    out_calls = []
                    for fm in re.finditer(
                            r'for\s+_,\s*(\w+)\s*:?=\s*range\s+(?:\[\]\*gin\.RouterGroup\{([^}]*)\}|(\w+))\s*\{', text):
                        lv, items, ranged_var = fm.group(1), fm.group(2), fm.group(3)
                        if items is None:
                            items = slice_defs.get(ranged_var, '')
                        gexprs = [x.strip() for x in items.split(',') if x.strip()]
                        # 找循环体范围
                        bstart = fm.end() - 1
                        bend = braces(text, bstart)[1]
                        inner = text[bstart + 1:bend - 1]
                        for gi, gexpr in enumerate(gexprs):
                            pseudo = '%s#%d' % (lv, gi)
                            for rr in route_re.finditer(inner):
                                out_routes.append((pseudo, rr.group(2), rr.group(3)))
                            for cm in re.finditer(r'\b(\w+)\.((?:Register|Mount)\w*)\s*\(', inner):
                                out_calls.append((cm.group(2), [pseudo], cm.group(1)))
                    # 去掉被展开的 range 块内原始路由：简单起见，丢弃所有位于 range 块内的原捕获
                    drop = []
                    for fm in re.finditer(
                            r'for\s+_,\s*(\w+)\s*:?=\s*range\s+(?:\[\]\*gin\.RouterGroup\{([^}]*)\}|(\w+))\s*\{', text):
                        bstart = fm.end() - 1
                        bend = braces(text, bstart)[1]
                        drop.append((bstart, bend))
                    return out_routes, out_calls, drop

                # for-range 多组注册：for _, g := range []*gin.RouterGroup{A, B} { ... }
                # 块内 g.GET(...) 路由与 ctrl.Register(g) 调用按每组展开
                slice_defs = {}
                for sm in re.finditer(r'(\w+)\s*:?=\s*\[\]\*gin\.RouterGroup\{([^}]*)\}', body):
                    slice_defs[sm.group(1)] = sm.group(2)
                out_r, out_c, drop_spans = _expand_range_loops(body, slice_defs)
                kept_routes = []
                route_spans = [(mm.start(), mm.end(), mm.groups()) for mm in route_re.finditer(body)]
                for s0, e0, grp in route_spans:
                    if any(s0 >= ds and e0 <= de for ds, de in drop_spans):
                        continue
                    kept_routes.append(grp)
                pseudo_groups = {}
                for fm in re.finditer(
                        r'for\s+_,\s*(\w+)\s*:?=\s*range\s+(?:\[\]\*gin\.RouterGroup\{([^}]*)\}|(\w+))\s*\{', body):
                    lv, items, ranged_var = fm.group(1), fm.group(2), fm.group(3)
                    if items is None:
                        items = slice_defs.get(ranged_var, '')
                    for gi, gexpr in enumerate(x.strip() for x in items.split(',') if x.strip()):
                        pseudo_groups['%s#%d' % (lv, gi)] = ('__expr__', gexpr)
                sc_info[(fname, qual)] = dict(
                    groups={mm.group(1): (mm.group(2), mm.group(3)) for mm in group_re.finditer(body)},
                    routes=kept_routes + out_r + extra_routes,
                    calls=calls + out_c,
                    _body=body,
                    _pseudo=pseudo_groups,
                )
    return sc_info, func_file, raw


def get_args(src, pos):
    i = pos - 1
    depth = 0
    j = i
    while j < len(src):
        if src[j] == '(':
            depth += 1
        elif src[j] == ')':
            depth -= 1
            if depth == 0:
                break
        j += 1
    return [a.strip() for a in src[i + 1:j].split(',') if a.strip()]


sc_info, func_file, raw = analyze(RDIRS + ['user-server/internal/monitor',
                                           'user-server/internal/aiagent/knowledge/controller',
                                           'user-server/internal/ops/controller'])

bindings = {}


def resolve(fkey, var, depth=0):
    if depth > 10 or not var:
        return None
    d = sc_info.get(fkey)
    if d and var in d['groups']:
        parent, prefix = d['groups'][var]
        pp = resolve(fkey, parent, depth + 1)
        return (pp or '') + prefix
    # for-range 伪组 'lv#i'：解析循环头中的组表达式（如 auth.Group("/v1") 或 auth）
    if d and '#' in var and var in d.get('_pseudo', {}):
        expr = d['_pseudo'][var][1]
        gm = re.match(r'(\w+)\.Group\(\s*"([^"]*)"\s*\)$', expr)
        if gm:
            base = resolve(fkey, gm.group(1), depth + 1) or ''
            return base + gm.group(2)
        return resolve(fkey, expr, depth + 1)
    # 本 scope 自身的 RouterGroup 参数（自由函数签名参数）：查绑定表
    ff = func_file.get(fkey[1])
    if ff and fkey[0] == ff[0]:
        for _, pname in ff[1]:
            if pname == var:
                return bindings.get((fkey[0], fkey[1], var))
    if var in func_file:
        cf, rgn = func_file[var]
        for idx, pname in rgn:
            if idx >= 0:
                return bindings.get((cf, var, pname))
    if var in ('auth', 'public', 'router'):
        return '/api'
    if var == 'r':
        return ''
    return None


# 1) 自由函数调用绑定 setupX(auth) 与方法调用 x.RegisterRoutes(y)
ctrl_types = set()
for fname, src in raw.items():
    for m in re.finditer(r'func\s+\(\w+\s+\*(\w+)\)', src):
        ctrl_types.add(m.group(1))

def infer_type_of(body):
    """scope 内 var -> 控制器类型（变量名可能跨文件重名，必须按 scope 推断）"""
    t = {}
    for m in re.finditer(r'(\w+)\s*:?=\s*(?:controller|opsctrl|knowledgectrl)\.New(\w+)\(', body):
        cname = m.group(2)
        best = None
        for ct in ctrl_types:
            if cname.startswith(ct) and (best is None or len(ct) > len(best)):
                best = ct
        t[m.group(1)] = best or cname
    for m in re.finditer(r'(\w+)\s*:?=\s*New(\w+)\(', body):
        t.setdefault(m.group(1), m.group(2))
    return t

for _ in range(8):
    changed = False
    for fkey, d in sc_info.items():
        for callee, args, obj in d['calls']:
            if obj:
                cls = infer_type_of(d.get('_body', '')).get(obj)
                if not cls:
                    # 包限定自由函数调用，如 `monitor.RegisterRoutes(auth)`：
                    # obj 是 import 的包别名，不是本地控制器变量，infer_type_of 必然返回
                    # None。原实现直接 `continue`，导致这类注册的 RouterGroup 参数永远
                    # 绑定不上 → 路由前缀解析为 <UNRESOLVED:xxx> → 前端调用被误判为断链。
                    # 这里按「包别名 == 被调函数所在目录名」匹配自由函数表。
                    # （2026-09-16 修复：此前 /api/monitor/* 7 条被误报为 UNMATCHED）
                    pkg_hit = func_file.get(callee)
                    if not pkg_hit or os.path.basename(os.path.dirname(pkg_hit[0])) != obj:
                        continue
                    cf, rgn = pkg_hit
                    key = callee
                else:
                    key = cls + '.' + callee
                    if key not in func_file:
                        continue
                    cf, rgn = func_file[key]
            else:
                key = callee
                cf, rgn = func_file.get(callee, (None, []))
            for idx, pname in rgn:
                if idx < 0 or idx >= len(args):
                    continue
                a = args[idx].split(',')[0].strip()
                val = resolve(fkey, a)
                kk = (cf, key, pname)
                if val is not None and bindings.get(kk) != val:
                    bindings[kk] = val
                    changed = True
    if not changed:
        break

# 2) 方法调用 x.RegisterRoutes(y) 已并入上方 fixpoint（obj 分支）

backend = {}
for fkey, d in sc_info.items():
    for var, meth, path in d['routes']:
        if meth == 'Any':
            meth = 'GET'
        full = resolve(fkey, var)
        if full is None:
            full = '<UNRESOLVED:%s>' % var
        backend[(meth, full + path)] = fkey

# 未解析项去重（2026-09-16）：同一路由会在两个 scope 中被捕获 ——
#   ① 函数级 scope（如 RegisterRoutes(rg *gin.RouterGroup)）—— 能解析出前缀；
#   ② 文件级伪 scope（qual == '<file>'）—— 函数形参不在作用域内，必然解析失败。
# 于是「已解析成功」的路由仍会以 <UNRESOLVED:xxx> 形式重复出现，把 unresolved 计数
# 抬高成假象（实测 /api/monitor/* 7 条即为此类）。此处剔除「同方法下已存在同后缀
# 可解析路由」的未解析项，只保留真正没解析出来的。
_resolved = [(m, p) for (m, p) in backend if 'UNRESOLVED' not in p]


def _shadowed(meth, path):
    suffix = path.split('>', 1)[1] if '>' in path else path
    return any(rm == meth and rp.endswith(suffix) for (rm, rp) in _resolved)


unres = sorted(k for k in backend if 'UNRESOLVED' in k[1] and not _shadowed(*k))
print("backend routes: %d, unresolved: %d" % (len(backend), len(unres)))
for k in unres:
    print("   UNRESOLVED:", k)

frontend = {}
for _path in iter_web_files():
    fn2 = os.path.relpath(_path, os.path.join(ROOT, 'user-web/src')).replace('\\', '/')
    src = open(_path, encoding='utf-8').read()
    consts = {m.group(1): m.group(3) for m in const_re.finditer(src)}

    def _expand(u):
        """把 ${KNOWN_CONST} 展开成其字面量，未知模板留给 tpl 兜底成 :param。

        2026-09-16 审计（TOOL-05）：不展开已知常量时，整段前缀会退化成通配 `:param`。
        """
        return tpl_name.sub(lambda mm: consts.get(mm.group(1), mm.group(0)), u)

    # ① 字面量 / 模板字面量实参
    for m in m_re.finditer(src):
        p = tpl.sub(':param', _expand(m.group(3))).split('?')[0]
        frontend.setdefault((m.group(1).upper(), p), []).append(fn2)
    # ② 裸常量标识符实参（http.get(BASE, params)）—— 此前完全不可见
    for m in ident_re.finditer(src):
        name = m.group(2)
        if name in consts:
            p = consts[name].split('?')[0]
            frontend.setdefault((m.group(1).upper(), p), []).append(fn2)


# 自检（TOOL-05）：首段为 `:param` 的 key 是**通配**路径，会匹配任意同长度同方法的路由
# —— 等于该前端调用根本没被校验（后端删掉该路由它也照样"匹配"）。
# 正常业务路径不可能以参数开头，故一旦出现即说明前缀解析失败，必须显式报警而非静默计入"已匹配"。
_degenerate = sorted(k for k in frontend if [s for s in k[1].split('/') if s][:1] == [':param'])
if _degenerate:
    print("⚠️  以下 %d 个前端调用被解析成通配路径（首段为 :param），其「已匹配」结论不可信：" % len(_degenerate))
    for _m, _p in _degenerate:
        print("      %-6s %-28s <- %s" % (_m, _p, ','.join(sorted(set(frontend[(_m, _p)])))))
    print()


def norm(p):
    return [s for s in p.split('/') if s]


def match(fe, be):
    a, b = norm(fe), norm(be)
    if len(a) != len(b):
        return False
    return all(x == y or x.startswith(':') or y.startswith(':') for x, y in zip(a, b))


unmatched = []
for (m, p), fs in sorted(frontend.items()):
    if not any(match(p, bp) for (bm, bp) in backend if bm == m):
        unmatched.append((m, p, fs))
print("\n=== UNMATCHED (frontend -> no backend): %d / %d ===" % (len(unmatched), len(frontend)))
for m, p, fs in unmatched:
    print("%-7s %-65s <- %s" % (m, p, ','.join(fs)))

fe_keys = list(frontend.keys())
dead = []
for (bm, bp), f in sorted(backend.items()):
    if not bp.startswith('/api/'):
        continue
    if any(x in bp for x in ('/webhook', '/bridge', '/ws', '/mcp', '/health', '/init',
                             '/public', '/s/', '/l/', '/livecode', '/callback', '/platform')):
        continue
    bpn = re.sub(r'/:[^/]+', '/:param', bp)
    # ⚠️ 2026-09-16 审计（TOOL-05）：原为 `for (m2, fp) in fe_keys`，**丢弃了 m2**
    # —— 于是后端 `POST /api/x` 只要前端存在任意方法的 `/api/x` 就算"被调用"。
    # 与上方 UNMATCHED 方向（方法敏感）口径不一致，属单向宽松 → 低估死接口。
    if not any(m2 == bm and match(fp, bpn) for (m2, fp) in fe_keys):
        dead.append((bm, bp))
print("\n=== BACKEND NOT CALLED BY FRONTEND (candidates): %d ===" % len(dead))
for bm, bp in dead:
    print("%-7s %s" % (bm, bp))

# 退出码（2026-09-16 审计 TOOL-05）：把"能发现断链"变成"能拦住断链"。
# 本脚本此前**未接入任何 workflow**，即 功能连贯性 维度**没有任何自动化门禁** ——
# 脚本写得很认真，但只在人工手动跑时才起作用。
if STRICT:
    bad = len(unmatched) + len(_degenerate)
    if bad:
        print("\n❌ strict: UNMATCHED=%d, 通配key=%d —— 存在确定的前后端契约缺陷"
              % (len(unmatched), len(_degenerate)))
        sys.exit(1)
    print("\n✅ strict: UNMATCHED=0 且无通配 key（%d 个前端调用全部可解析并与后端匹配）"
          % len(frontend))
