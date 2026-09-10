# -*- coding: utf-8 -*-
"""api-contract 审计脚本：前端 api/*.js 调用 vs 后端 gin 路由注册 双向对照。

用法：python scripts/audit_api_contract.py
输出：UNMATCHED（前端调了后端不存在的路由）、BACKEND_NOT_CALLED（后端注册但前端未调，候选死接口/为外部客户端保留）。
"""
import re, os, glob

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
RDIRS = ['user-server/internal/router', 'user-server/internal/controller']
WEB = os.path.join(ROOT, 'user-web/src/api')

route_re = re.compile(r'(\w+)\.(GET|POST|PUT|DELETE|PATCH)\(\s*"([^"]*)"', re.ASCII)
group_re = re.compile(r'(\w+)\s*:?=\s*(\w+)\.Group\(\s*"([^"]*)"')
func_re = re.compile(r'func\s+(?:\((\w+)\s+\*?(\w+)\)\s+)?(\w+)\s*\(([^)]*)\)\s*\{')
rgtype = re.compile(r'\*gin\.RouterGroup\b')
m_re = re.compile(r"http\.(get|post|put|delete|patch)\(\s*([`'\"])([^`'\"]+)\2")
tpl = re.compile(r"\$\{[^}]+\}")


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
                    continue
                key = cls + '.' + callee
                if key not in func_file:
                    continue
                cf, rgn = func_file[key]
            else:
                cf, rgn = func_file.get(callee, (None, []))
            for idx, pname in rgn:
                if idx < 0 or idx >= len(args):
                    continue
                a = args[idx].split(',')[0].strip()
                val = resolve(fkey, a)
                kk = (cf, key if obj else callee, pname)
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

unres = sorted(k for k in backend if 'UNRESOLVED' in k[1])
print("backend routes: %d, unresolved: %d" % (len(backend), len(unres)))
for k in unres:
    print("   UNRESOLVED:", k)

frontend = {}
for fn2 in sorted(os.listdir(WEB)):
    if not fn2.endswith('.js'):
        continue
    src = open(os.path.join(WEB, fn2), encoding='utf-8').read()
    for m in m_re.finditer(src):
        p = tpl.sub(':param', m.group(3))
        p = p.split('?')[0]
        frontend.setdefault((m.group(1).upper(), p), []).append(fn2)


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
    if not any(match(fp, bpn) for (m2, fp) in fe_keys):
        dead.append((bm, bp))
print("\n=== BACKEND NOT CALLED BY FRONTEND (candidates): %d ===" % len(dead))
for bm, bp in dead:
    print("%-7s %s" % (bm, bp))
