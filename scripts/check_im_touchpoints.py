#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""check_im_touchpoints.py — 接入触点自动化校验（specs/024-im-seam-alignment T1201）。

把「接一个平台要改哪几处」变成可执行检查：漏一处即失败，并列出具体渠道 + 缺失项。

触点矩阵（channel × 六触点，全部从源码符号提取，不在本脚本维护第二份渠道清单）：
  1. 渠道常量      service 包 WebhookChannel 常量 / model 包渠道常量
  2. channelcaps   internal/channelcaps/manifests.go 的 Manifest 登记
  3. 账号表行      service/webhook.go webhookAccountTableByChannel（直发渠道必须有本地账号表）
  4. 入站分支      service/webhook.go webhookInboundCapable（或文档内白名单例外：专用控制器）
  5. sender 注册   outbound_sender_*.go 的 RegisterChannelSender（或 bridge_outbox 能力位）
  6. client 包     internal/channelbot/<id>/ 目录（有才要求，无不罚）

豁免集（每条都有注释出处，新增豁免必须给理由）：
  - douyin/tiktok：入站 capable 但无专用账号表——退 integration_accounts 按平台取密钥
    （webhook.go webhookAccountTableByChannel 头注释明文）。
  - dingtalk/wechat：有直发 sender 但不在 webhookInboundCapable——入站走专用控制器
    （钉钉机器人回调 / 公众号消息回调），不经通用 webhook 路由。
  - web/widget、sms、email、custom：非 IM 平台接入语义（站内控件 / 触达 / 自定义），
    不要求账号表行与 sender。
  - channelbot/core：共享内核包，不是渠道。

用法：python3 scripts/check_im_touchpoints.py   （仓根执行；CI 每次 push 全量跑）
退出码：0 = 全通过；1 = 有缺失（打印渠道 + 缺失项清单）。
"""

import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SVC = os.path.join(ROOT, "user-server", "internal", "service")
MODEL = os.path.join(ROOT, "user-server", "internal", "model")
CAPS = os.path.join(ROOT, "user-server", "internal", "channelcaps")
BOT = os.path.join(ROOT, "user-server", "internal", "channelbot")

# 文档内白名单例外（每条必须能在源码注释里找到出处，见文件头）：
NO_ACCOUNT_TABLE_OK = {"douyin", "tiktok"}            # 退 integration_accounts 按平台取密钥
DEDICATED_INBOUND_OK = {"dingtalk", "wechat"}         # 专用控制器入站，不经 webhookInboundCapable
NON_IM_MANIFEST_OK = {"web", "sms", "email", "custom"}  # 站内控件 / 触达 / 自定义接入
SHARED_CLIENT_PACKAGES = {"core"}                     # channelbot 共享内核
# manifest 已登记但不走 service WebhookChannel 常量族的渠道（各自路径自证）：
#   web=embed-sdk 站内控件、email=SMTP 触达、sms=阿里云触达、custom=字面量接入、
#   pdd=channelgw registry 名字串（model.ChannelPDD，桥接族不逐渠道立 service 常量）。
NOT_WEBHOOK_CHANNEL_OK = {"web", "email", "sms", "custom", "pdd"}


def read(path):
    with open(path, "r", encoding="utf-8") as f:
        return f.read()


def strip_line_comments(src):
    """去掉 // 行注释后再做符号匹配——被注释掉的注册/分支不算数（负例验收的要求）。"""
    return re.sub(r"^\s*//.*$", "", src, flags=re.M)


def service_channel_constants():
    """service 包 WebhookChannel 常量：名字 → 渠道 id。"""
    consts = {}
    for fn in os.listdir(SVC):
        if not fn.endswith(".go"):
            continue
        src = read(os.path.join(SVC, fn))
        for m in re.finditer(r"Channel\w+\s+WebhookChannel\s*=\s*\"([a-z_]+)\"", src):
            consts["Channel" + _const_name(m.group(0))] = m.group(1)
    return consts


def _const_name(decl):
    m = re.match(r"(Channel\w+)\s+WebhookChannel", decl)
    return m.group(1)[len("Channel"):] if m else ""


def model_channel_constants():
    """model 包渠道常量：名字 → 渠道 id（manifests.go 引用 model.ChannelX）。"""
    consts = {}
    for fn in os.listdir(MODEL):
        if not fn.endswith(".go"):
            continue
        src = read(os.path.join(MODEL, fn))
        for m in re.finditer(r"(Channel\w+)\s*=\s*\"([a-z_]+)\"", src):
            consts[m.group(1)] = m.group(2)
    return consts


def manifest_channels():
    """channelcaps/manifests.go 登记的渠道 id 集合（含 bridge(...) 便捷调用）。"""
    src = strip_line_comments(read(os.path.join(CAPS, "manifests.go")))
    model_consts = model_channel_constants()
    ids = set()
    for m in re.finditer(r"Channel:\s*(model\.Channel\w+|\"([a-z_]+)\")", src):
        tok = m.group(1)
        if tok.startswith("model."):
            name = tok[len("model."):]
            if name in model_consts:
                ids.add(model_consts[name])
            else:
                sys.exit(f"[FATAL] manifests.go 引用了未知 model 常量 {tok}（model 渠道常量漂移）")
        else:
            ids.add(m.group(2))
    for m in re.finditer(r"bridge\(\s*(model\.Channel\w+)", src):
        name = m.group(1)[len("model."):]
        if name in model_consts:
            ids.add(model_consts[name])
    return ids


def resolve_service_const(tok, consts):
    """把 RegisterChannelSender(ChannelX, ...) / map 键 ChannelX: 解析成渠道 id。"""
    name = tok.strip()
    if not name.startswith("Channel"):
        return None
    if name in consts:
        return consts[name]
    return None


def sender_registrations():
    """outbound_sender_*.go 里 RegisterChannelSender 的渠道 id 集合。"""
    consts = service_channel_constants()
    ids = set()
    for fn in os.listdir(SVC):
        if not (fn.startswith("outbound_sender_") and fn.endswith(".go")):
            continue
        src = strip_line_comments(read(os.path.join(SVC, fn)))
        for m in re.finditer(r"RegisterChannelSender\(\s*(Channel\w+)", src):
            cid = resolve_service_const(m.group(1), consts)
            if cid is None:
                sys.exit(f"[FATAL] {fn} RegisterChannelSender 引用未知常量 {m.group(1)}")
            ids.add(cid)
    return ids


def inbound_capable_channels():
    """webhookInboundCapable switch 命中的渠道 id 集合。"""
    src = strip_line_comments(read(os.path.join(SVC, "webhook.go")))
    consts = service_channel_constants()
    m = re.search(r"func webhookInboundCapable\(channel WebhookChannel\) bool \{\s*switch channel \{(.*?)\}", src, re.S)
    if not m:
        sys.exit("[FATAL] webhook.go 里找不到 webhookInboundCapable 的 switch 结构（函数被改名？）")
    ids = set()
    for cm in re.finditer(r"(Channel\w+)", m.group(1)):
        cid = resolve_service_const(cm.group(1), consts)
        if cid:
            ids.add(cid)
    return ids


def account_table_channels():
    """webhookAccountTableByChannel 的渠道 id 集合。"""
    src = strip_line_comments(read(os.path.join(SVC, "webhook.go")))
    consts = service_channel_constants()
    m = re.search(r"var webhookAccountTableByChannel = map\[WebhookChannel\]string\{(.*?)\}", src, re.S)
    if not m:
        sys.exit("[FATAL] webhook.go 里找不到 webhookAccountTableByChannel（账号表被改名？）")
    ids = set()
    for cm in re.finditer(r"(Channel\w+)\s*:", m.group(1)):
        cid = resolve_service_const(cm.group(1), consts)
        if cid:
            ids.add(cid)
    return ids


def client_packages():
    """internal/channelbot/<id>/ 目录名集合。"""
    if not os.path.isdir(BOT):
        return set()
    return {d for d in os.listdir(BOT) if os.path.isdir(os.path.join(BOT, d))}


def main():
    declared = set()
    for fn in os.listdir(SVC):
        if fn.endswith(".go"):
            src = strip_line_comments(read(os.path.join(SVC, fn)))
            for m in re.finditer(r"Channel\w+\s+WebhookChannel\s*=\s*\"([a-z_]+)\"", src):
                declared.add(m.group(1))
    manifests = manifest_channels()
    senders = sender_registrations()
    inbound = inbound_capable_channels()
    accounts = account_table_channels()
    clients = client_packages()

    problems = []

    # 1) channelcaps 登记的每个渠道必须是已声明常量（manifest 引用漂浮 = 立刻红）。
    for ch in sorted(manifests - declared - NOT_WEBHOOK_CHANNEL_OK):
        problems.append(f"渠道 {ch}: channelcaps 已登记但 service 无 WebhookChannel 常量（触点1缺失）")

    # 2) 直发 sender 必须有 channelcaps manifest（渲染层按 manifest 定格式）。
    for ch in sorted(senders - manifests):
        problems.append(f"渠道 {ch}: 已注册出站 sender 但 channelcaps 未登记（触点2缺失，渲染会拒绝）")

    # 3) 直发 sender ↔ 账号表行严格相等（有 sender 无表=租户反查断链；有表无 sender=死表）。
    for ch in sorted(senders - accounts):
        problems.append(f"渠道 {ch}: 已注册出站 sender 但 webhookAccountTableByChannel 无账号表行（触点3缺失，租户反查断链）")
    for ch in sorted(accounts - senders):
        problems.append(f"渠道 {ch}: webhookAccountTableByChannel 有账号表行但没有出站 sender（触点5缺失，死表漂移）")

    # 4) 入站 capable 的渠道必须可出站：直发 sender 或（豁免集外）明确说明。
    for ch in sorted(inbound):
        if ch in senders:
            continue
        if ch in NO_ACCOUNT_TABLE_OK:
            continue  # 桥接族：bridge_outbox 能力位承接（由 T111 parity 测试钉住），此处只查账号表豁免
        problems.append(f"渠道 {ch}: webhookInboundCapable 可入站但既无 sender 也不在豁免集（触点5缺失，能收不能发）")

    # 5) 直发 sender 必须可入站：webhookInboundCapable 或专用控制器豁免集。
    for ch in sorted(senders - inbound - DEDICATED_INBOUND_OK):
        problems.append(f"渠道 {ch}: 已注册出站 sender 但不可入站且不在专用控制器豁免集（触点4缺失）")

    # 6) channelbot client 包必须对应已登记 manifest 渠道（core 为共享内核豁免）。
    for pkg in sorted(clients - manifests - SHARED_CLIENT_PACKAGES):
        problems.append(f"渠道 {pkg}: internal/channelbot/{pkg}/ 存在但 channelcaps 未登记（触点2缺失）")

    if problems:
        print("IM 接入触点校验失败（specs/024 T1201）：漏改的触点清单——")
        for p in problems:
            print(f"  - {p}")
        print("接一个平台的完整触点见 docs/channel-onboarding.md；新增豁免必须在本脚本头注释给出来源。")
        sys.exit(1)

    print(f"IM 接入触点校验通过：常量 {len(declared)}、manifest {len(manifests)}、"
          f"sender {len(senders)}、入站 {len(inbound)}、账号表 {len(accounts)}、client 包 {len(clients)}。")
    sys.exit(0)


if __name__ == "__main__":
    main()
