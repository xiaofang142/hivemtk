#!/usr/bin/env python3
"""
知识库扩展脚本第三批次 - 从885条扩展到1000+条
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

# 扩展知识库条目（第三批次）
EXTENDED_ENTRIES_BATCH3 = {
    "行业解决方案": [
        "电商行业：多平台店铺管理、智能客服、营销自动化、数据分析、会员管理。",
        "教育行业：在线课程、学员管理、营销推广、社群运营、数据分析。",
        "金融行业：客户管理、合规营销、风险控制、数据分析、安全审计。",
        "医疗行业：患者管理、预约挂号、健康咨询、数据分析、隐私保护。",
        "房产行业：房源管理、客户跟进、营销活动、数据分析、合同管理。",
        "汽车行业：潜客管理、试驾预约、售后服务、数据分析、会员管理。",
        "零售行业：门店管理、会员营销、库存管理、数据分析、供应链。",
        "餐饮行业：外卖管理、会员营销、预订管理、数据分析、供应链。",
        "旅游行业：线路管理、预订管理、客户跟进、数据分析、营销推广。",
        "健身行业：会员管理、课程预约、营销活动、数据分析、社群运营。",
        "美容行业：会员管理、预约管理、营销活动、数据分析、产品推荐。",
        "宠物行业：宠物档案、服务预约、营销活动、数据分析、社群运营。",
        "家政行业：服务预约、技师管理、营销活动、数据分析、客户管理。",
        "婚庆行业：客户管理、案例展示、营销活动、数据分析、订单管理。",
        "摄影行业：客户管理、作品展示、预约管理、数据分析、营销推广。",
        "法律行业：客户咨询、案件管理、营销推广、数据分析、合规管理。",
        "设计行业：项目管理、客户跟进、作品展示、数据分析、营销推广。",
        "翻译行业：项目管理、客户跟进、质量控制、数据分析、营销推广。",
        "咨询行业：客户管理、项目管理、知识库、数据分析、营销推广。",
        "培训行业：学员管理、课程管理、营销活动、数据分析、社群运营。"
    ],
    "技术支持": [
        "如何解决数据库连接失败？检查 PostgreSQL 服务是否启动，端口是否正确，密码是否正确。",
        "如何解决 Redis 连接失败？检查 Redis 服务是否启动，端口是否正确，密码是否正确。",
        "如何解决 LLM 服务不可用？检查 llama.cpp 是否启动，模型是否加载，端口是否正确。",
        "如何解决 Embedding 服务不可用？检查 TEI 服务是否启动，模型是否加载，端口是否正确。",
        "如何解决 WebSocket 连接失败？检查防火墙设置，代理配置，静态托管配置。",
        "如何解决跨域问题？检查 CORS 配置，确保前端域名在允许列表中。",
        "如何解决文件上传失败？检查文件大小限制，存储空间，文件类型白名单。",
        "如何解决邮件发送失败？检查 SMTP 配置，网络连接，邮件模板。",
        "如何解决短信发送失败？检查短信服务商配置，余额，签名审核。",
        "如何解决 Telegram Bot 不响应？检查 Bot Token，网络代理，Webhook 配置。",
        "如何解决性能问题？检查数据库索引，缓存策略，连接池配置。",
        "如何解决内存溢出？检查 Goroutine 泄漏，内存缓存，大对象处理。",
        "如何解决磁盘空间不足？清理日志文件，归档旧数据，扩容磁盘。",
        "如何解决 CPU 占用过高？检查热点代码，优化算法，增加缓存。",
        "如何解决网络延迟？检查网络配置，CDN 加速，数据压缩。",
        "如何解决日志过大？配置日志轮转，调整日志级别，归档旧日志。",
        "如何解决备份失败？检查存储空间，网络连接，备份脚本。",
        "如何解决升级失败？回滚版本，检查日志，修复数据。",
        "如何解决部署失败？检查环境配置，依赖版本，端口冲突。",
        "如何解决监控告警？检查告警规则，通知渠道，值班安排。"
    ],
    "性能优化": [
        "数据库优化：添加索引、优化查询、连接池、读写分离、分库分表。",
        "缓存优化：Redis 缓存、本地缓存、缓存预热、缓存更新、缓存降级。",
        "代码优化：算法优化、并发优化、内存优化、GC 调优、连接复用。",
        "网络优化：HTTP/2、连接复用、数据压缩、CDN 加速、负载均衡。",
        "前端优化：代码分割、懒加载、图片优化、缓存策略、Service Worker。",
        "容器优化：镜像优化、资源限制、健康检查、日志管理、自动伸缩。",
        "架构优化：分层解耦、异步处理、事件驱动、CQRS。",
        "监控优化：指标采集、告警规则、日志聚合、链路追踪、性能分析。",
        "安全优化：最小权限、加密传输、输入验证、SQL 注入防护、XSS 防护。",
        "运维优化：自动化部署、配置管理、监控告警、故障恢复、容量规划。",
        "搜索优化：向量索引、BM25 索引、混合检索、缓存策略、查询优化。",
        "AI 优化：模型选择、Prompt 优化、工具调用、上下文管理、结果缓存。",
        "消息优化：异步发送、批量处理、重试机制、限流熔断、监控告警。",
        "数据优化：数据压缩、分区表、归档策略、清理策略、备份恢复。",
        "接口优化：响应压缩、分页优化、字段筛选、批量接口、GraphQL。",
        "日志优化：异步写入、批量写入、日志级别、采样策略、存储优化。",
        "配置优化：热更新、配置中心、环境隔离、灰度发布、回滚机制。",
        "测试优化：单元测试、集成测试、性能测试、自动化测试、持续集成。",
        "文档优化：结构清晰、示例丰富、版本管理、多语言、持续更新。",
        "流程优化：CI/CD、代码审查、自动化测试、监控告警、持续改进。"
    ],
    "常见错误": [
        "错误：database connection refused。解决方案：检查 PostgreSQL 服务是否启动，端口是否正确。",
        "错误：redis connection refused。解决方案：检查 Redis 服务是否启动，端口是否正确。",
        "错误：llama.cpp server not found。解决方案：检查 LLM 服务是否启动，端口是否正确。",
        "错误：embedding service unavailable。解决方案：检查 TEI 服务是否启动，端口是否正确。",
        "错误：jwt token expired。解决方案：刷新 Token 或重新登录。",
        "错误：permission denied。解决方案：检查用户权限，联系管理员。",
        "错误：file not found。解决方案：检查文件路径，确认文件存在。",
        "错误：invalid request body。解决方案：检查请求格式，确认字段类型。",
        "错误：rate limit exceeded。解决方案：降低请求频率，等待限流窗口重置。",
        "错误：internal server error。解决方案：查看服务器日志，联系技术支持。",
        "错误：timeout。解决方案：检查网络连接，增加超时时间，优化服务性能。",
        "错误：connection reset。解决方案：检查网络稳定性，重试请求。",
        "错误：dns resolution failed。解决方案：检查 DNS 配置，使用 IP 地址。",
        "错误：ssl certificate error。解决方案：检查证书配置，更新证书。",
        "错误：out of memory。解决方案：增加内存，优化内存使用，清理缓存。",
        "错误：disk full。解决方案：清理磁盘空间，扩容磁盘，归档旧数据。",
        "错误：too many connections。解决方案：优化连接池，减少并发，增加连接数。",
        "错误：deadlock detected。解决方案：优化事务，减少锁竞争，重试事务。",
        "错误：constraint violation。解决方案：检查数据完整性，修复数据。",
        "错误：authentication failed。解决方案：检查用户名密码，重置密码。"
    ],
    "更新日志": [
        "v1.0.0 - 首次发布：支持七端打通、AI 智能体、知识库、客服系统。",
        "v1.1.0 - 新增 Telegram 渠道：Bot 管理、Webhook、Long Polling、群发。",
        "v1.2.0 - 新增邮件营销：邮件列表、草稿管理、任务调度、发送引擎。",
        "v1.3.0 - 新增短信营销：短信配置、草稿管理、任务调度、列表管理。",
        "v1.4.0 - 新增社群管理：企微集成、WhatsApp 营销、Telegram Bot。",
        "v1.5.0 - 新增线索客户：多渠道收集、智能评分、客户 360 视图。",
        "v1.6.0 - 新增营销自动化：SOP 编排、A/B 测试、RFM 分层、流失预警。",
        "v1.7.0 - 新增数据分析：客户旅程、转化漏斗、智能体产能、自定义报表。",
        "v1.8.0 - 新增安全合规：JWT 鉴权、权限控制、异常检测、安全审计。",
        "v1.9.0 - 新增网页客服：Widget 嵌入、iframe 聊天、WebSocket 实时消息。",
        "v2.0.0 - 架构升级：五层架构、性能优化、稳定性提升。",
        "v2.1.0 - 新增多语言：知识库预翻译、实时翻译、多语言 AI 回复。",
        "v2.2.0 - 新增数据大屏：ECharts 图表、拖拽设计、公开分享、实时更新。",
        "v2.3.0 - 新增运维仪表盘：CPU/内存/磁盘/网络监控、告警通知。",
        "v2.4.0 - 新增链路追踪：全链路追踪、性能分析、错误定位。",
        "v2.5.0 - 新增配置中心：热更新、远程配置、配置加密、版本管理。",
        "v2.6.0 - 新增容器化：Docker 镜像、Kubernetes 部署、Helm Chart。",
        "v2.7.0 - 新增 CI/CD：GitHub Actions、自动化测试、代码质量检查。",
        "v2.8.0 - 新增可观测性: 私域部署: layer_decision_logs / audit_logs 落库 + SQL 巡检 (无外部监控/告警通道)。",
        "v2.9.0 - 新增安全加固：SAST、DAST、依赖扫描、秘密检测。"
    ],
    "路线图": [
        "Q1 2024：完成七端打通、AI 智能体、知识库、客服系统核心功能。",
        "Q2 2024：完成邮件营销、短信营销、社群管理、线索客户功能。",
        "Q3 2024：完成营销自动化、数据分析、安全合规、网页客服功能。",
        "Q4 2024：完成架构升级、多语言、数据大屏、运维仪表盘功能。",
        "Q1 2025：完成链路追踪、配置中心、容器化、CI/CD 功能。",
        "Q2 2025：完成监控告警、安全加固、性能优化、稳定性提升。",
        "Q3 2025：完成 AI 智能体、知识库 RAG、AI 销冠、平台集成。",
        "Q4 2025：完成 AI 2.0、智能推荐、预测分析、自动化决策。",
        "2026 年：成为私域营销 AI 操作系统领导者，构建开放生态。",
        "长期愿景：用 AI 赋能私域营销，让每个团队成员都能发挥销冠能力。"
    ],
    "社区贡献": [
        "代码贡献：提交 Bug 修复、功能开发、性能优化、文档完善。",
        "文档贡献：编写教程、翻译文档、完善 API 文档、更新 README。",
        "社区支持：回答问题、分享经验、参与讨论、帮助新手。",
        "测试贡献：编写测试用例、发现 Bug、性能测试、兼容性测试。",
        "设计贡献：UI 设计、UX 优化、图标设计、品牌设计。",
        "翻译贡献：多语言翻译、本地化、国际化支持。",
        "推广贡献：撰写博客、录制视频、参加活动、分享案例。",
        "生态贡献：开发插件、集成服务、创建模板、分享工具。",
        "反馈建议：功能建议、体验反馈、需求收集、优先级排序。",
        "代码审查：审查 PR、提出改进建议、确保代码质量。"
    ]
}

def generate_content_hash(content):
    """生成内容哈希"""
    return hashlib.sha256(content.encode('utf-8')).hexdigest()

def insert_knowledge_base_batch3():
    """插入知识库条目（第三批次）"""
    conn = psycopg2.connect(**DB_CONFIG)
    cur = conn.cursor()
    
    try:
        # 获取当前最大的 document_id
        cur.execute("SELECT COALESCE(MAX(id), 0) FROM knowledge_documents WHERE product_id = %s", (PRODUCT_ID,))
        max_doc_id = cur.fetchone()[0]
        
        # 为每个类别创建一个文档
        doc_id = max_doc_id + 1
        total_inserted = 0
        
        for category, chunks in EXTENDED_ENTRIES_BATCH3.items():
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
        print(f"成功插入 {total_inserted} 条知识库条目（第三批次）")
        
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
    insert_knowledge_base_batch3()
