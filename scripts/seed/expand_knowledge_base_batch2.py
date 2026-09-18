#!/usr/bin/env python3
"""
知识库扩展脚本第二批次 - 从770条扩展到1000+条
"""

import os
import sys
import json
import hashlib
import psycopg2

# 口令仅从环境注入（本文件不落任何明文字面量）：本机 `set -a && . .env && set +a`
_DB_PW = (os.environ.get("POSTGRES_PASSWORD")
          or os.environ.get("HIVEMTK_DB_PASSWORD")
          or os.environ.get("PGPASSWORD"))
if not _DB_PW:
    sys.exit("❌ 缺少数据库口令：请导出 POSTGRES_PASSWORD（或 HIVEMTK_DB_PASSWORD / PGPASSWORD）")

# 数据库配置
DB_CONFIG = {
    "host": "127.0.0.1",
    "port": 8232,
    "database": "user_db",
    "user": "admin",
    "password": _DB_PW
}

# 产品ID
PRODUCT_ID = 'hivemtk-platform-cs'

# 扩展知识库条目（第二批次）
EXTENDED_ENTRIES_BATCH2 = {
    "技术架构": [
        "HiveMtk 采用五层架构：Router→Handler→Service→Repository→Model，依赖方向强制，禁止跨层调用。",
        "HiveMtk 的 Router 层负责 URL→Handler 映射，禁止写业务逻辑和内联 handler。",
        "HiveMtk 的 Handler 层负责参数绑定+调Service+返回响应，禁止写SQL和业务判断。",
        "HiveMtk 的 Service 层负责业务逻辑，禁止直接操作DB。",
        "HiveMtk 的 Repository 层负责 GORM 数据访问，禁止包含业务判断。",
        "HiveMtk 的 Model 层负责纯数据结构，禁止外部依赖。",
        "HiveMtk 使用 Go 1.25 + Gin 作为 Web 框架，高性能、轻量级。",
        "HiveMtk 使用 GORM 作为 ORM，支持多种数据库，简化数据库操作。",
        "HiveMtk 使用 pgvector 扩展进行向量存储和检索，支持 1024 维向量。",
        "HiveMtk 使用 Redis 作为缓存，支持会话管理、Token 存储、数据缓存。",
        "HiveMtk 使用 WebSocket 实现实时消息推送，支持双向通信。",
        "HiveMtk 使用 SSE（Server-Sent Events）实现服务器推送，支持实时监控。",
        "HiveMtk 使用 JWT 进行身份认证，支持 Token 刷新、过期管理。",
        "HiveMtk 使用 AES-256-GCM 进行数据加密，确保敏感数据安全。",
        "HiveMtk 使用 HNSW 索引加速向量检索，提高检索效率。",
        "HiveMtk 使用混合检索：向量 + BM25 + RRF 融合，提高检索准确性。",
        "HiveMtk 使用三级 RAG 检索：粗排→精排→LLM 改写，确保回答质量。",
        "HiveMtk 使用 ReAct 自主智能体，支持感知→规划→调工具→反思循环。",
        "HiveMtk 使用 Docker Compose 进行容器化部署，一键启动所有服务。",
        "HiveMtk 使用 PostgreSQL 15 作为主数据库，支持 JSONB、全文检索等高级功能。"
    ],
    "API接口": [
        "HiveMtk API 采用 RESTful 设计，统一响应格式：{\"code\": 0, \"data\": {...}, \"message\": \"ok\"}。",
        "HiveMtk API 错误码：0=成功 400=参数错误 401=认证失败 403=权限不足 404=不存在 500=服务端错误。",
        "HiveMtk API 路径规范：/api/{domain}/{resource} 用户端，/api/manage/{resource} 管理端。",
        "HiveMtk API 支持分页查询：?page=1&page_size=20，返回 {\"list\": [], \"total\": N}。",
        "HiveMtk API 支持排序：?sort=created_at&order=desc，支持多字段排序。",
        "HiveMtk API 支持过滤：?status=active&category=product，支持多条件过滤。",
        "HiveMtk API 支持搜索：?keyword=关键词，支持全文检索。",
        "HiveMtk API 支持批量操作：POST /api/{domain}/{resource}/batch，支持批量创建、更新、删除。",
        "HiveMtk API 支持文件上传：POST /api/upload，支持分片上传、断点续传。",
        "HiveMtk API 支持 WebSocket 连接：ws://localhost:8204/ws，支持实时消息。",
        "HiveMtk API 支持 SSE 连接：GET /api/events，支持服务器推送。",
        "HiveMtk API 支持跨域：CORS 配置，支持前端跨域访问。",
        "HiveMtk API 支持限流：令牌桶算法，防止恶意请求。",
        "HiveMtk API 支持缓存：Redis 缓存，提高接口响应速度。",
        "HiveMtk API 支持日志：结构化日志，支持链路追踪。",
        "HiveMtk API 支持监控：私域: 应用层日志 + audit_logs 落库, 无外部监控通道。",
        "HiveMtk API 支持版本管理：/api/v1/{domain}/{resource}，支持多版本并行。",
        "HiveMtk API 支持认证：JWT Token，支持刷新、过期管理。",
        "HiveMtk API 支持权限：RBAC 角色权限，支持细粒度控制。",
        "HiveMtk API 支持审计：操作日志，支持审计追踪。"
    ],
    "数据模型": [
        "HiveMtk 使用 GORM 模型定义，支持自动创建、更新时间戳。",
        "HiveMtk 的 Customer 模型：ID、Name、Phone、Email、OpenID、Avatar、Tags、Metadata 等字段。",
        "HiveMtk 的 Conversation 模型：ID、CustomerID、Channel、Status、LastMessageAt 等字段。",
        "HiveMtk 的 Message 模型：ID、ConversationID、Content、Type、Direction、Status 等字段。",
        "HiveMtk 的 KnowledgeChunk 模型：ID、DocumentID、ProductID、Content、Embedding 等字段。",
        "HiveMtk 的 Agent 模型：ID、Name、Type、Config、Status 等字段。",
        "HiveMtk 的 Task 模型：ID、Type、Status、Payload、Result 等字段。",
        "HiveMtk 的 User 模型：ID、Username、Password、Role、MerchantID 等字段。",
        "HiveMtk 的 Merchant 模型：ID、Name、Config、Status、APIKey 等字段。",
        "HiveMtk 的 Product 模型：ID、Name、Description、Price、Status 等字段。",
        "HiveMtk 的 Order 模型：ID、CustomerID、ProductID、Amount、Status 等字段。",
        "HiveMtk 的 Campaign 模型：ID、Name、Type、Config、Status 等字段。",
        "HiveMtk 的 Template 模型：ID、Name、Content、Type、Status 等字段。",
        "HiveMtk 的 Tag 模型：ID、Name、Color、Category 等字段。",
        "HiveMtk 的 Label 模型：ID、Name、Type、Value 等字段。",
        "HiveMtk 的 Event 模型：ID、Type、Payload、Source、Timestamp 等字段。",
        "HiveMtk 的 Log 模型：ID、Level、Message、Source、Timestamp 等字段。",
        "HiveMtk 的 Config 模型：ID、Key、Value、Type、Status 等字段。",
        "HiveMtk 的 Notification 模型：ID、Type、Content、Read、Timestamp 等字段。",
        "HiveMtk 的 File 模型：ID、Name、Path、Size、Type、Status 等字段。"
    ],
    "业务流程": [
        "HiveMtk 客服流程：客户咨询 → 意图识别 → RAG 检索 → AI 生成回复 → 人工审核 → 发送回复。",
        "HiveMtk 营销流程：线索收集 → 智能评分 → 自动培育 → 转化成交 → 复购推荐。",
        "HiveMtk 智能体流程：感知环境 → 规划行动 → 调用工具 → 反思结果 → 决策下一步。",
        "HiveMtk 知识库流程：文档上传 → 文本分块 → 向量化 → 索引存储 → 检索匹配。",
        "HiveMtk 消息流程：消息接收 → 渠道路由 → 会话匹配 → AI 处理 → 消息发送。",
        "HiveMtk 会话分配流程：新会话 → 技能匹配 → 负载均衡 → 分配客服 → 监控质量。",
        "HiveMtk 转人工流程：AI 识别 → 信心评估 → 转接人工 → 会话交接 → 满意度回访。",
        "HiveMtk 数据同步流程：数据采集 → 数据清洗 → 数据转换 → 数据存储 → 数据分析。",
        "HiveMtk 安全流程：身份认证 → 权限校验 → 操作审计 → 异常检测 → 安全响应。",
        "HiveMtk 部署流程：代码拉取 → 环境配置 → 服务启动 → 健康检查 → 流量接入。",
        "HiveMtk 监控流程：指标采集 → 数据存储 → 告警规则 → 异常通知 → 故障恢复。",
        "HiveMtk 备份流程：全量备份 → 增量备份 → 备份验证 → 异地存储 → 恢复测试。",
        "HiveMtk 升级流程：版本检查 → 备份数据 → 执行迁移 → 服务重启 → 功能验证。",
        "HiveMtk 扩容流程：容量评估 → 资源申请 → 服务部署 → 负载均衡 → 性能测试。",
        "HiveMtk 故障处理：故障发现 → 影响评估 → 应急处理 → 根因分析 → 改进措施。"
    ],
    "竞品对比": [
        "HiveMtk vs Dify：HiveMtk 专注私域营销，Dify 专注通用 AI 应用开发。HiveMtk 七端打通，Dify 无社媒渠道。",
        "HiveMtk vs Chatwoot：HiveMtk 内置 AI 智能体，Chatwoot 需要集成外部 AI。HiveMtk 数据零出域，Chatwoot 需要云端。",
        "HiveMtk vs Rasa：HiveMtk 开箱即用，Rasa 需要大量定制开发。HiveMtk 支持七端，Rasa 主要支持文本。",
        "HiveMtk vs HubSpot：HiveMtk 开源免费，HubSpot 商业收费。HiveMtk 数据私域，HubSpot 数据云端。",
        "HiveMtk vs Salesforce：HiveMtk 轻量级，Salesforce 企业级。HiveMtk 专注营销，Salesforce 全面 CRM。",
        "HiveMtk vs 企业微信 SCRM：HiveMtk 多端统一，企业微信 SCRM 单端。HiveMtk AI 智能体，企业微信 SCRM 规则引擎。",
        "HiveMtk vs 尘锋 SCRM：HiveMtk 开源免费可私域部署，尘锋商业 SaaS。",
        "HiveMtk vs 探马 SCRM：HiveMtk 七端打通，探马 主要企微。HiveMtk AI 智能体，探马 营销自动化。",
        "HiveMtk vs 小能客服：HiveMtk 内置营销，小能客服 专注客服。HiveMtk AI 智能体，小能客服 传统客服。",
        "HiveMtk vs 网易七鱼：HiveMtk 开源免费，网易七鱼 商业收费。HiveMtk 数据私域，网易七鱼 数据云端。",
        "HiveMtk vs 智齿客服：HiveMtk 七端打通，智齿客服 主要文本。HiveMtk AI 智能体，智齿客服 传统客服。",
        "HiveMtk vs 容联七陌：HiveMtk 轻量级，容联七陌 企业级。HiveMtk 专注营销，容联七陌 全面客服。",
        "HiveMtk vs Udesk：HiveMtk 开源免费，Udesk 商业收费。HiveMtk 数据私域，Udesk 数据云端。",
        "HiveMtk vs 环信：HiveMtk 内置 AI，环信 需要集成。HiveMtk 七端，环信 主要 IM。",
        "HiveMtk vs 融云：HiveMtk 营销导向，融云 通信导向。HiveMtk AI 智能体，融云 IM 能力。",
        "HiveMtk vs 容联云：HiveMtk 轻量级，容联云 企业级。HiveMtk 专注营销，容联云 全面通信。",
        "HiveMtk vs 天润融通：HiveMtk 开源免费，天润融通 商业收费。HiveMtk 数据私域，天润融通 数据云端。",
        "HiveMtk vs 智云众包：HiveMtk 自动化，智云众包 人工众包。HiveMtk AI 智能体，智云众包 人工客服。",
        "HiveMtk vs 京东客服：HiveMtk 通用型，京东客服 电商专用。HiveMtk 七端，京东客服 主要电商。",
        "HiveMtk vs 阿里小蜜：HiveMtk 开源，阿里小蜜 闭源。HiveMtk 私域，阿里小蜜 公域。"
    ],
    "最佳实践": [
        "知识库最佳实践：文档结构化、分块合理、标签分类、定期更新、质量监控。",
        "智能体最佳实践：意图清晰、工具匹配、反馈循环、持续优化、人工兜底。",
        "客服最佳实践：响应及时、回答准确、态度友好、转接顺畅、满意度回访。",
        "营销最佳实践：精准定位、个性化内容、A/B 测试、数据分析、持续优化。",
        "数据安全最佳实践：最小权限、加密存储、访问控制、审计日志、定期备份。",
        "性能优化最佳实践：缓存策略、索引优化、查询优化、连接池、异步处理。",
        "监控告警最佳实践：关键指标、合理阈值、分级告警、快速响应、持续改进。",
        "代码质量最佳实践：代码规范、单元测试、代码审查、静态分析、持续集成。",
        "文档最佳实践：结构清晰、示例丰富、版本管理、多语言、持续更新。",
        "用户体验最佳实践：界面简洁、操作便捷、响应迅速、错误友好、引导清晰。",
        "API 设计最佳实践：RESTful 规范、统一格式、版本管理、文档完整、向后兼容。",
        "数据库设计最佳实践：范式设计、索引优化、分库分表、读写分离、备份恢复。",
        "安全防护最佳实践：输入验证、SQL 注入防护、XSS 防护、CSRF 防护、权限控制。",
        "容器化最佳实践：镜像优化、多阶段构建、健康检查、资源限制、日志管理。",
        "架构设计最佳实践：五层架构、依赖注入、接口隔离、单一职责、依赖倒置。",
        "DevOps 最佳实践：CI/CD、基础设施即代码、自动化测试、监控告警、持续交付。",
        "敏捷开发最佳实践：迭代开发、持续反馈、快速响应、团队协作、价值交付。",
        "产品设计最佳实践：用户研究、需求分析、原型设计、用户测试、迭代优化。",
        "增长黑客最佳实践：数据驱动、快速实验、用户反馈、病毒传播、持续优化。",
        "开源社区最佳实践：贡献指南、代码规范、Issue 模板、PR 审查、社区治理。"
    ]
}

def generate_content_hash(content):
    """生成内容哈希"""
    return hashlib.sha256(content.encode('utf-8')).hexdigest()

def insert_knowledge_base_batch2():
    """插入知识库条目（第二批次）"""
    conn = psycopg2.connect(**DB_CONFIG)
    cur = conn.cursor()
    
    try:
        # 获取当前最大的 document_id
        cur.execute("SELECT COALESCE(MAX(id), 0) FROM knowledge_documents WHERE product_id = %s", (PRODUCT_ID,))
        max_doc_id = cur.fetchone()[0]
        
        # 为每个类别创建一个文档
        doc_id = max_doc_id + 1
        total_inserted = 0
        
        for category, chunks in EXTENDED_ENTRIES_BATCH2.items():
            # 插入文档
            cur.execute("""
                INSERT INTO knowledge_documents (id, product_id, filename, title, source_type, status, created_at, updated_at)
                VALUES (%s, %s, %s, %s, 'batch', 1, NOW(), NOW())
            """, (doc_id, PRODUCT_ID, f"{category}.txt", f"HiveMtk {category}知识库"))
            
            # 插入分段
            for i, chunk_content in enumerate(chunks):
                content_hash = generate_content_hash(chunk_content)
                cur.execute("""
                    INSERT INTO knowledge_chunks (document_id, product_id, chunk_index, content, content_hash, 
                                               token_count, char_count, embed_status, created_at)
                    VALUES (%s, %s, %s, %s, %s, %s, %s, 'indexed', NOW())
                """, (doc_id, PRODUCT_ID, i, chunk_content, content_hash, 
                      len(chunk_content.split()), len(chunk_content)))
                total_inserted += 1
            
            doc_id += 1
        
        conn.commit()
        print(f"成功插入 {total_inserted} 条知识库条目（第二批次）")
        
        # 验证总数
        cur.execute("SELECT COUNT(*) FROM knowledge_chunks WHERE product_id = %s", (PRODUCT_ID,))
        total_count = cur.fetchone()[0]
        print(f"当前知识库总数: {total_count}")
        
    except Exception as e:
        conn.rollback()
        print(f"插入失败: {e}")
        raise
    finally:
        cur.close()
        conn.close()

if __name__ == "__main__":
    insert_knowledge_base_batch2()
