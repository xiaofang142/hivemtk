-- ============================================================
-- 用户端 PostgreSQL 初始化脚本（独立数据库/独立容器）
-- ============================================================

-- 启用 pgvector 扩展
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ============================================================
-- 用户端数据库表结构 (user_db)
-- ============================================================

-- 知识库向量表
-- 私域基线：默认 1024 维（本地 Xinference + bge-m3）
CREATE TABLE IF NOT EXISTS knowledge_embeddings (
    id SERIAL PRIMARY KEY,
    product_id VARCHAR(64) NOT NULL,
    chunk_id VARCHAR(64) NOT NULL,
    content TEXT,
    embedding vector(1024),
    metadata JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(product_id, chunk_id)
);

-- 创建向量索引
CREATE INDEX IF NOT EXISTS idx_knowledge_embeddings_product_id ON knowledge_embeddings(product_id);
CREATE INDEX IF NOT EXISTS idx_knowledge_embeddings_chunk_id ON knowledge_embeddings(chunk_id);

-- RAG 产品配置表
-- 与 Go 模型 RagProduct 对齐（id 为 VARCHAR(64) UUID，
-- 含 is_active / 嵌入式 llm_/emb_/rerank_ 配置列）。
CREATE TABLE IF NOT EXISTS rag_products (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    category VARCHAR(100),
    vector_table VARCHAR(128) UNIQUE,
    embedding_model VARCHAR(64) DEFAULT 'bge-m3',
    embedding_dim INTEGER DEFAULT 1024,
    llm_model VARCHAR(100) DEFAULT 'Qwen2.5-3B-Instruct',
    -- LLMProviderConfig (embeddedPrefix:llm_)
    llm_api_key VARCHAR(255),
    llm_base_url VARCHAR(255),
    llm_api_type VARCHAR(32),
    llm_model_detail VARCHAR(100),
    llm_max_retries INTEGER DEFAULT 3,
    llm_request_timeout INTEGER DEFAULT 60,
    -- EmbeddingProviderConfig (embeddedPrefix:emb_)
    emb_api_key VARCHAR(255),
    emb_base_url VARCHAR(255),
    emb_api_type VARCHAR(32),
    emb_model VARCHAR(100),
    emb_dimension INTEGER DEFAULT 1024,
    emb_enabled BOOLEAN DEFAULT TRUE,
    -- RerankProviderConfig (embeddedPrefix:rerank_)
    rerank_api_key VARCHAR(255),
    rerank_base_url VARCHAR(255),
    rerank_api_type VARCHAR(32),
    rerank_model VARCHAR(100),
    rerank_enabled BOOLEAN DEFAULT TRUE,
    temperature DOUBLE PRECISION DEFAULT 0.7,
    max_tokens INTEGER DEFAULT 1000,
    top_p DOUBLE PRECISION DEFAULT 0.9,
    frequency_penalty DOUBLE PRECISION DEFAULT 0.5,
    presence_penalty DOUBLE PRECISION DEFAULT 0.5,
    response_format VARCHAR(50) DEFAULT 'json_object',
    system_prompt TEXT,
    top_k INTEGER DEFAULT 5,
    chunk_size INTEGER DEFAULT 800,
    chunk_overlap INTEGER DEFAULT 100,
    similarity_threshold DOUBLE PRECISION DEFAULT 0.6,
    is_active BOOLEAN DEFAULT TRUE,
    status INTEGER DEFAULT 1,
    doc_count INTEGER DEFAULT 0,
    chunk_count BIGINT DEFAULT 0,
    last_import_at TIMESTAMP,
    last_search_at TIMESTAMP,
    search_count BIGINT DEFAULT 0,
    intent_classification TEXT,
    enabled_intents TEXT,
    intent_sop_map TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- RAG 产品配置索引
CREATE INDEX IF NOT EXISTS idx_rag_products_vector_table ON rag_products(vector_table);

-- 知识库文档表
-- rag_products.id 为 VARCHAR(64) 字符串产品ID；knowledge_documents.product_id
-- 自 2026-08 起与之一致为 VARCHAR(64)（KnowledgeDocument.ProductID 为 string）。
-- GORM 模型未声明 foreignKey，故此处不建 FK。
CREATE TABLE IF NOT EXISTS knowledge_documents (
    id SERIAL PRIMARY KEY,
    product_id VARCHAR(64),
    filename VARCHAR(256) NOT NULL,
    file_type VARCHAR(32),
    file_size INTEGER,
    content TEXT,
    status BIGINT DEFAULT 1,
    error_message TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 知识库文档索引
CREATE INDEX IF NOT EXISTS idx_knowledge_documents_product ON knowledge_documents(product_id);

-- 脚本模板表
CREATE TABLE IF NOT EXISTS script_templates (
    id SERIAL PRIMARY KEY,
    name VARCHAR(128) NOT NULL,
    category_id INTEGER,
    content TEXT,
    status INTEGER DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 脚本分类表
CREATE TABLE IF NOT EXISTS script_categories (
    id SERIAL PRIMARY KEY,
    name VARCHAR(64) NOT NULL,
    sort_order INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 脚本推荐表
CREATE TABLE IF NOT EXISTS script_recommends (
    id SERIAL PRIMARY KEY,
    script_id INTEGER REFERENCES script_templates(id),
    sort_order INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 邮件 SMTP 配置表
CREATE TABLE IF NOT EXISTS email_smtps (
    id SERIAL PRIMARY KEY,
    name VARCHAR(64),
    host VARCHAR(128),
    port INTEGER,
    username VARCHAR(128),
    password VARCHAR(256),
    from_address VARCHAR(128),
    from_name VARCHAR(64),
    status INTEGER DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 邮件列表表
CREATE TABLE IF NOT EXISTS email_lists (
    id SERIAL PRIMARY KEY,
    name VARCHAR(128),
    description TEXT,
    status INTEGER DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 客户标签表
CREATE TABLE IF NOT EXISTS customer_tags (
    id SERIAL PRIMARY KEY,
    name VARCHAR(64) NOT NULL,
    color VARCHAR(16),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- RAG 会话表
-- 与 Go 模型 RagSession 对齐（id 为 VARCHAR(64) UUID，
-- 字段 user_id/platform/kb_id/status/config）。
CREATE TABLE IF NOT EXISTS rag_sessions (
    id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(64),
    platform VARCHAR(20),
    kb_id VARCHAR(64),
    status VARCHAR(20) DEFAULT 'active',
    config TEXT DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_rag_sessions_user_id ON rag_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_rag_sessions_platform ON rag_sessions(platform);
CREATE INDEX IF NOT EXISTS idx_rag_sessions_status ON rag_sessions(status);

-- RAG 消息表
-- 与 Go 模型 RagMessage 对齐（message_id / timestamp 字段）。
CREATE TABLE IF NOT EXISTS rag_messages (
    id SERIAL PRIMARY KEY,
    session_id VARCHAR(64) NOT NULL,
    message_id VARCHAR(64),
    role VARCHAR(20) NOT NULL,
    content TEXT,
    timestamp TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_rag_messages_session_id ON rag_messages(session_id);
CREATE INDEX IF NOT EXISTS idx_rag_messages_timestamp ON rag_messages(timestamp);

-- OBS 配置表
CREATE TABLE IF NOT EXISTS obs_configs (
    id SERIAL PRIMARY KEY,
    provider VARCHAR(32) NOT NULL,
    name VARCHAR(64),
    endpoint VARCHAR(256),
    bucket VARCHAR(128),
    access_key VARCHAR(128),
    secret_key VARCHAR(256),
    status INTEGER DEFAULT 1,
    is_default INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 短链接表
CREATE TABLE IF NOT EXISTS short_links (
    id SERIAL PRIMARY KEY,
    original_url TEXT NOT NULL,
    short_code VARCHAR(16) NOT NULL UNIQUE,
    click_count INTEGER DEFAULT 0,
    status INTEGER DEFAULT 1,
    expire_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 短链接访问记录表
CREATE TABLE IF NOT EXISTS short_link_accesses (
    id SERIAL PRIMARY KEY,
    short_link_id INTEGER REFERENCES short_links(id),
    ip_address VARCHAR(64),
    user_agent VARCHAR(256),
    referer VARCHAR(512),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 抖音卡片活动表
CREATE TABLE IF NOT EXISTS douyin_card_activities (
    id SERIAL PRIMARY KEY,
    name VARCHAR(128),
    card_type VARCHAR(32),
    config JSONB,
    status INTEGER DEFAULT 1,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
