#!/usr/bin/env bash
# 生产机部署 step3：解包前端产物到 /var/www/hivemtk（保留 hash 旧块）+ Caddy 自证
# 前置：user-web 构建产物已上传到 /root/user-web-dist.tgz
set -e
echo "=== 0) 上传校验 ==="
ls -la /root/user-web-dist.tgz
echo "=== 1) 解包到 /var/www/hivemtk（不删旧 assets，保留 hash 旧块兼容已缓存客户端）==="
mkdir -p /var/www/hivemtk
tar xzf /root/user-web-dist.tgz -C /var/www/hivemtk
rm -f /root/user-web-dist.tgz
echo "=== 2) 产物核对 ==="
ls -la /var/www/hivemtk | head -10
echo "--- version.json ---"
cat /var/www/hivemtk/version.json; echo
echo "--- index.html 入口 ---"
grep -o 'assets/index-[A-Za-z0-9_-]*\.js' /var/www/hivemtk/index.html | head -2
echo "--- assets 数量 ---"
ls /var/www/hivemtk/assets | wc -l
echo "=== 3) 经 Caddy 自证（https 本机）==="
curl -fsS -o /dev/null -w "  /version.json      -> %{http_code} %{content_type}\n" https://api.xjznpt.com/version.json
curl -fsS -o /dev/null -w "  /                  -> %{http_code} %{content_type}\n" https://api.xjznpt.com/
curl -fsS -o /dev/null -w "  /api/health        -> %{http_code} %{content_type}\n" https://api.xjznpt.com/api/health
ENTRY=$(grep -o 'assets/index-[A-Za-z0-9_-]*\.js' /var/www/hivemtk/index.html | head -1)
echo "  entry=$ENTRY"
curl -fsS -o /dev/null -w "  /$ENTRY -> %{http_code} %{content_type}\n" "https://api.xjznpt.com/$ENTRY"
echo "=== 4) 内容自证：新入口里应含本次修复的菜单构建器与九语标记 ==="
curl -fsS "https://api.xjznpt.com/$ENTRY" | grep -c "menuSection" || true
curl -fsS "https://api.xjznpt.com/$ENTRY" | grep -o "menuRegistry\|buildMenuTree" | sort -u || true
