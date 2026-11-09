<template>
  <div class="rag-overview-container">
    <el-card class="rag-overview-card">
      <template #header>
        <div class="card-header">
          <span>RAG智能体概览</span>
        </div>
      </template>
      
      <div class="overview-content">
        <el-row :gutter="20">
          <el-col :span="8">
            <el-card class="stat-card">
              <div class="stat-item">
                <div class="stat-number">
                  {{ stats.totalProducts }}
                </div>
                <div class="stat-label">
                  RAG产品数量
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="8">
            <el-card class="stat-card">
              <div class="stat-item">
                <div class="stat-number">
                  {{ stats.activeProducts }}
                </div>
                <div class="stat-label">
                  活跃产品
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="8">
            <el-card class="stat-card">
              <div class="stat-item">
                <div class="stat-number">
                  {{ stats.activatedAccounts }}
                </div>
                <div class="stat-label">
                  已启用账号
                </div>
              </div>
            </el-card>
          </el-col>
        </el-row>

        <el-divider />

        <div class="quick-actions">
          <h3>快速操作</h3>
          <el-row :gutter="20">
            <el-col :span="6">
              <el-card
                class="action-card"
                @click="goToConfig"
              >
                <div class="action-item">
                  <el-icon class="action-icon">
                    <Setting />
                  </el-icon>
                  <div class="action-label">
                    配置RAG产品
                  </div>
                </div>
              </el-card>
            </el-col>
            <el-col :span="6">
              <el-card
                class="action-card"
                @click="goToAccountConfig"
              >
                <div class="action-item">
                  <el-icon class="action-icon">
                    <User />
                  </el-icon>
                  <div class="action-label">
                    账号配置
                  </div>
                </div>
              </el-card>
            </el-col>
            <el-col :span="6">
              <el-card
                class="action-card"
                @click="goToAccountConfig"
              >
                <div class="action-item">
                  <el-icon class="action-icon">
                    <ChatDotRound />
                  </el-icon>
                  <div class="action-label">
                    自动回复设置
                  </div>
                </div>
              </el-card>
            </el-col>
            <el-col :span="6">
              <el-card
                class="action-card"
                @click="goToDocs"
              >
                <div class="action-item">
                  <el-icon class="action-icon">
                    <Document />
                  </el-icon>
                  <div class="action-label">
                    帮助文档
                  </div>
                </div>
              </el-card>
            </el-col>
          </el-row>
        </div>

        <el-divider />

        <div class="recent-activity">
          <h3>最近活动</h3>
          <el-table
            :data="activities"
            style="width: 100%"
          >
            <el-table-column
              prop="time"
              label="时间"
              width="180"
            />
            <el-table-column
              prop="action"
              label="操作"
            />
            <el-table-column
              prop="user"
              label="用户"
              width="120"
            />
            <el-table-column
              prop="status"
              label="状态"
              width="100"
            >
              <template #default="{ row }">
                <el-tag :type="getStatusTagType(row.status)">
                  {{ getStatusLabel(row.status) }}
                </el-tag>
              </template>
            </el-table-column>
          </el-table>
        </div>
      </div>
    </el-card>

    
    <el-dialog
      v-model="helpDialogVisible"
      title="RAG 帮助文档"
      width="780px"
      :close-on-click-modal="false"
    >
      <div class="help-doc-content">
        <el-collapse v-model="activeHelpSections">
          <el-collapse-item
            title="什么是 RAG？"
            name="1"
          >
            <div class="help-section">
              <p>RAG（Retrieval-Augmented Generation，检索增强生成）是一种结合信息检索与文本生成的 AI 技术。通过将大语言模型（LLM）与外部知识库连接，RAG 能够在生成回复时动态检索相关文档，从而提供更准确、更及时的回答。</p>
              <p><strong>核心优势：</strong></p>
              <ul>
                <li>实时性：基于最新知识库内容生成回答，无需重新训练模型</li>
                <li>准确性：回复可溯源至具体文档，降低 AI 幻觉</li>
                <li>可控性：管理员可精确控制知识范围，确保回复符合业务规范</li>
                <li>灵活性：支持多种平台接入，如网站客服、社交媒体自动回复等</li>
              </ul>
            </div>
          </el-collapse-item>

          <el-collapse-item
            title="如何配置 RAG 产品？"
            name="2"
          >
            <div class="help-section">
              <p><strong>步骤一：</strong>进入「系统设置」→「RAG 配置」页面，或点击概览页的「配置RAG产品」卡片。</p>
              <p><strong>步骤二：</strong>点击「新增产品」，填写以下信息：</p>
              <ul>
                <li><strong>产品名称：</strong>为 RAG 应用起一个有辨识度的名称</li>
                <li><strong>产品描述：</strong>简要描述该 RAG 产品的用途和场景</li>
                <li><strong>模型选择：</strong>选择底层大语言模型（如 DeepSeek、GPT 等）</li>
                <li><strong>温度参数：</strong>控制生成回复的随机性（0-2，建议 0.7）</li>
                <li><strong>最大 Token：</strong>限制单次回复的最大长度</li>
              </ul>
              <p><strong>步骤三：</strong>保存配置后，产品将出现在列表中，可随时启用或禁用。</p>
            </div>
          </el-collapse-item>

          <el-collapse-item
            title="如何搭建知识库？"
            name="3"
          >
            <div class="help-section">
              <p><strong>步骤一：</strong>进入「知识库管理」页面。</p>
              <p><strong>步骤二：</strong>上传文档或直接输入文本内容。支持的格式包括：</p>
              <ul>
                <li>PDF 文档</li>
                <li>Word 文档（.docx）</li>
                <li>Markdown 文件</li>
                <li>纯文本（TXT）</li>
                <li>网页链接（URL）</li>
              </ul>
              <p><strong>步骤三：</strong>为文档设置分类标签和关联 RAG 产品，便于管理。</p>
              <p><strong>步骤四：</strong>系统将自动对文档进行向量化处理，处理完成后即可用于 RAG 检索。</p>
              <p><strong>最佳实践：</strong></p>
              <ul>
                <li>保持文档内容简洁、结构化</li>
                <li>定期更新知识库以确保信息时效性</li>
                <li>按主题分类文档，提高检索精度</li>
                <li>避免上传重复内容</li>
              </ul>
            </div>
          </el-collapse-item>

          <el-collapse-item
            title="如何接入平台账号？"
            name="4"
          >
            <div class="help-section">
              <p><strong>步骤一：</strong>进入「账号配置」页面。</p>
              <p><strong>步骤二：</strong>点击「新增账号」，选择平台类型（如抖音、微信、网站等）。</p>
              <p><strong>步骤三：</strong>填写平台账号信息：</p>
              <ul>
                <li><strong>平台类型：</strong>选择要接入的社交媒体或客服平台</li>
                <li><strong>账号凭证：</strong>根据平台要求填写 API Key、Token 等认证信息</li>
                <li><strong>关联 RAG 产品：</strong>选择该账号使用的 RAG 产品</li>
                <li><strong>自动回复规则：</strong>配置触发条件和回复策略</li>
              </ul>
              <p><strong>步骤四：</strong>保存后，可在「自动回复设置」中进一步配置回复规则。</p>
            </div>
          </el-collapse-item>

          <el-collapse-item
            title="常见问题排查"
            name="5"
          >
            <div class="help-section">
              <p><strong>1. RAG 回复不准确怎么办？</strong></p>
              <p>检查知识库文档是否覆盖了相关问题；优化文档内容结构和质量；调整模型温度参数降低随机性。</p>

              <p><strong>2. 知识库文档上传失败？</strong></p>
              <p>确认文件格式是否支持；检查文件大小是否超过限制（单文件最大 50MB）；确认网络连接正常。</p>

              <p><strong>3. 平台账号连接失败？</strong></p>
              <p>检查 API 凭证是否正确且未过期；确认平台 API 配额是否充足；查看系统日志获取详细错误信息。</p>

              <p><strong>4. 向量化处理卡住？</strong></p>
              <p>检查文档内容是否为空或损坏；尝试重新上传文档；联系管理员检查 Embedding 服务状态。</p>

              <p><strong>5. 如何查看 RAG 使用统计？</strong></p>
              <p>在 RAG 概览页面可查看产品数量、活跃状态及最近活动记录。详细统计数据可在各子页面查看。</p>
            </div>
          </el-collapse-item>
        </el-collapse>
      </div>
      <template #footer>
        <el-button
          type="primary"
          @click="helpDialogVisible = false"
        >
          关闭
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ragProductConfigAPI } from '@/api/ragProductConfig'
import { knowledgeBaseAPI } from '@/api/knowledgeBase'

const router = useRouter()

const stats = ref({
  totalProducts: 0,
  activeProducts: 0,
  activatedAccounts: 0,
  totalDocuments: 0
});

const activities = ref([]);

const STATUS_ZH_TO_VALUE = { '成功': 'success', '失败': 'failed', '进行中': 'running' };
const getStatusTagType = (status) => {
  const v = STATUS_ZH_TO_VALUE[status] || status
  switch (v) {
    case 'success': return 'success'
    case 'failed':  return 'danger'
    case 'running': return 'warning'
    default:        return 'info'
  }
}
const STATUS_ZH_LABEL = { '成功': '成功', '失败': '失败', '进行中': '进行中' }
const getStatusLabel = (status) => STATUS_ZH_LABEL[status] || (status === 'success' ? '成功' : status === 'failed' ? '失败' : status === 'running' ? '进行中' : status)

const goToConfig = () => {
  // 2026-11-10：RAG 主配置包装页下线后，两个按钮分别直达各自的独立页
  //（此前都指向同一个 tabs 包装页，按钮语义早已名存实亡）。
  router.push('/system/rag-product')
};

const goToAccountConfig = () => {
  router.push('/system/rag-account')
};

const helpDialogVisible = ref(false);
const activeHelpSections = ref(['1'])

const goToDocs = () => {
  helpDialogVisible.value = true
};

const loadStats = async () => {
  try {
    const productsRes = await ragProductConfigAPI.getRagProducts({});
    const products = productsRes?.items || productsRes?.list || []
    if (Array.isArray(products)) {
      stats.value.totalProducts = products.length
      stats.value.activeProducts = products.filter((p) => p.is_active !== false).length
    }

    try {
      const kbsRes = await knowledgeBaseAPI.listKBs({ page: 1, page_size: 100 })
      const kbs = kbsRes?.list || kbsRes?.items || (Array.isArray(kbsRes) ? kbsRes : [])
      stats.value.totalDocuments = kbs.reduce((sum, k) => sum + Number(k?.doc_count || k?.document_count || 0), 0)
    } catch (e) {
      console.warn('加载知识库文档失败:', e);
    }

    try {
      const accountsRes = await ragProductConfigAPI.getAccountConfig({})
      const accounts = accountsRes?.list || accountsRes || []
      if (Array.isArray(accounts)) {
        stats.value.activatedAccounts = accounts.filter((a) => a.is_active !== false).length
      }
    } catch (e) {
      console.warn('加载账号配置失败:', e);
    }

    if (Array.isArray(products) && products.length > 0) {
      activities.value = products.slice(0, 5).map((p) => ({
        time: p.updated_at || p.created_at || '未知时间',
        action: `RAG产品: ${p.name || '未命名'}`,
        user: p.creator || 'system',
        status: p.is_active ? '成功' : '失败'
      }))
    } else {
      activities.value = []
    }
  } catch (error) {
    console.error('加载 RAG 概览数据失败:', error)
    ElMessage.warning(i18n.global.t('RAG 概览数据加载失败,请稍后重试'))
  }
};

onMounted(() => {
  loadStats()
})
</script>

<style scoped>
.rag-overview-container {
  padding: 20px;
}

.rag-overview-card {
  min-height: 600px;
}

.card-header {
  font-size: 18px;
  font-weight: bold;
}

.stat-item {
  text-align: center;
}

.stat-number {
  font-size: 28px;
  font-weight: bold;
  color: #4F46E5;
  margin-bottom: 8px;
}

.stat-label {
  color: #909399;
  font-size: 14px;
}

.stat-card {
  text-align: center;
  cursor: pointer;
  transition: all 0.3s;
}

.stat-card:hover {
  transform: translateY(-5px);
  box-shadow: 0 8px 16px rgba(0,0,0,0.1);
}

.quick-actions h3 {
  margin-bottom: 20px;
}

.action-card {
  text-align: center;
  cursor: pointer;
  transition: all 0.3s;
  height: 120px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.action-card:hover {
  transform: translateY(-5px);
  box-shadow: 0 8px 16px rgba(0,0,0,0.1);
}

.action-item {
  display: flex;
  flex-direction: column;
  align-items: center;
}

.action-icon {
  font-size: 32px;
  color: #4F46E5;
  margin-bottom: 8px;
}

.action-label {
  font-size: 14px;
  color: #606266;
}

.recent-activity h3 {
  margin-bottom: 20px;
}

.help-doc-content {
  max-height: 60vh;
  overflow-y: auto;
}

.help-section {
  padding: 10px 0;
  line-height: 1.8;
  color: #303133;
}

.help-section p {
  margin-bottom: 10px;
}

.help-section ul {
  margin-left: 20px;
  margin-bottom: 10px;
}

.help-section ul li {
  margin-bottom: 4px;
}
</style>