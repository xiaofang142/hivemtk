<template>
  <div class="llm-routing-page">
    <el-card class="header-card">
      <div class="header-content">
        <h2>LLM 多模型路由</h2>
        <p class="subtitle">管理多模型接入、场景路由（含灰度）、Fallback 策略与成本统计</p>
      </div>
      <div class="header-actions">
        <el-button type="primary" @click="showModelDialog()">
          <el-icon><Plus /></el-icon>
          新增模型
        </el-button>
        <el-button @click="$router.push('/llmRouting/cost')">成本看板</el-button>
        <el-button @click="refreshAll">
          <el-icon><Refresh /></el-icon>
          刷新
        </el-button>
      </div>
    </el-card>

    <el-tabs v-model="activeTab" class="content-tabs">
      
      <el-tab-pane label="模型列表" name="models">
        <el-table :data="models" v-loading="loading.models" stripe>
          <template #empty>
            <el-empty description="暂无模型数据，请新增模型或检查后端接口" />
          </template>
          <el-table-column prop="name" label="模型名称" min-width="140" />
          <el-table-column prop="vendor" label="厂商" width="110">
            <template #default="{ row }">
              <el-tag size="small" :type="row.vendor === 'local' ? 'success' : 'info'">
                {{ vendorLabel(row.vendor) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="model" label="模型标识" min-width="160" show-overflow-tooltip />
          <el-table-column label="状态" width="90">
            <template #default="{ row }">
              <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
                {{ row.enabled ? '启用' : '禁用' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="max_rpm" label="限流(RPM)" width="110" align="center" />
          <el-table-column prop="cost_per_1k" label="成本(¥/1k)" width="120" align="center">
            <template #default="{ row }">
              <span :class="row.cost_per_1k > 0 ? 'cost-paid' : 'cost-free'">
                {{ row.cost_per_1k > 0 ? row.cost_per_1k.toFixed(4) : '免费' }}
              </span>
            </template>
          </el-table-column>
          <el-table-column prop="base_url" label="接入地址" min-width="200" show-overflow-tooltip />
          <el-table-column label="API Key" width="90">
            <template #default="{ row }">
              <el-tag v-if="row.api_key_set" type="success" size="small">已设置</el-tag>
              <el-tag v-else type="info" size="small">未设置</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="FC 能力" width="80">
            <template #default="{ row }">
              <el-tag v-if="row.no_fc" type="warning" size="small">NoFC</el-tag>
              <el-tag v-else type="success" size="small">支持</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="260" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" @click="testModel(row)">测试</el-button>
              <el-button link type="primary" @click="toggleStatus(row)">
                {{ row.enabled ? '禁用' : '启用' }}
              </el-button>
              <el-button link type="primary" @click="showModelDialog(row)">编辑</el-button>
              <el-button link type="danger" @click="deleteModel(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      
      <el-tab-pane label="场景路由配置" name="routing">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>场景 → 模型路由（含灰度发布与版本号）</span>
              <div>
                <el-button size="small" @click="loadRouting">
                  <el-icon><Refresh /></el-icon> 刷新
                </el-button>
                <el-button type="primary" size="small" @click="addRoutingRow">
                  <el-icon><Plus /></el-icon> 新增映射
                </el-button>
              </div>
            </div>
          </template>
          <el-table :data="sceneRouting" v-loading="loading.routing" stripe>
            <template #empty>
              <el-empty description="暂无路由配置" />
            </template>
            <el-table-column prop="scenario" label="场景" min-width="160" />
            <el-table-column prop="provider" label="首选 Provider" min-width="140" />
            <el-table-column label="Fallback 链" min-width="200">
              <template #default="{ row }">
                <el-tag v-for="fb in (row.fallbacks || [])" :key="fb" size="small" type="info" style="margin-right: 4px">
                  {{ fb }}
                </el-tag>
                <span v-if="!row.fallbacks?.length" class="text-muted">-</span>
              </template>
            </el-table-column>
            <el-table-column prop="version" label="版本" width="80" align="center" />
            <el-table-column prop="weight" label="灰度权重(%)" width="110" align="center">
              <template #default="{ row }">
                <el-progress
                  v-if="row.weight > 0 && row.weight < 100"
                  :percentage="Number(row.weight)"
                  :stroke-width="10"
                  :color="canaryColors"
                />
                <el-tag v-else-if="row.weight === 100" type="warning" size="small">全量灰度</el-tag>
                <span v-else class="text-muted">-</span>
              </template>
            </el-table-column>
            <el-table-column prop="min_quality" label="最低质量" width="100" align="center" />
            <el-table-column prop="max_latency" label="最大时延(ms)" width="120" align="center" />
            <el-table-column label="操作" width="160" fixed="right">
              <template #default="{ row, $index }">
                <el-button link type="primary" @click="editRoutingRow(row, $index)">编辑</el-button>
                <el-button link type="danger" @click="removeRoutingRow($index)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>

      
      <el-tab-pane label="Fallback 策略" name="fallback">
        <el-card v-loading="loading.fallback">
          <template #header><span>当前生效的 Fallback 降级链（按场景）</span></template>
          <el-table :data="fallbackRows" stripe>
            <template #empty>
              <el-empty description="暂无 Fallback 配置，请先配置场景路由" />
            </template>
            <el-table-column prop="scenario" label="场景" min-width="160" />
            <el-table-column label="主 Provider" min-width="140">
              <template #default="{ row }">
                <el-tag type="primary" size="small">{{ row.provider }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="Fallback 链（按顺序）" min-width="300">
              <template #default="{ row }">
                <template v-if="row.fallbacks?.length">
                  <el-tag v-for="(fb, i) in row.fallbacks" :key="fb" size="small" type="info" style="margin-right: 4px">
                    {{ i + 1 }}. {{ fb }}
                  </el-tag>
                </template>
                <span v-else class="text-muted">无降级</span>
              </template>
            </el-table-column>
            <el-table-column prop="version" label="版本" width="80" align="center" />
          </el-table>
        </el-card>
      </el-tab-pane>

      
      <el-tab-pane label="成本统计" name="cost">
        <el-row :gutter="20" class="stat-row">
          <el-col :span="6">
            <el-card class="stat-card">
              <div class="stat-label">总调用次数</div>
              <div class="stat-value">{{ costStats.total_calls || 0 }}</div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stat-card">
              <div class="stat-label">总 Token 数</div>
              <div class="stat-value" style="color: #4F46E5">{{ formatNumber(costStats.total_tokens) }}</div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stat-card">
              <div class="stat-label">总成本(¥)</div>
              <div class="stat-value" style="color: #EF4444">{{ (costStats.monthly_cost || 0).toFixed(4) }}</div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stat-card">
              <div class="stat-label">启用的模型</div>
              <div class="stat-value" style="color: #F59E0B">{{ costStats.enabled_models || 0 }} / {{ costStats.active_models || 0 }}</div>
            </el-card>
          </el-col>
        </el-row>
        <el-card style="margin-top: 20px">
          <template #header>
            <div class="card-header">
              <span>各 Provider 用量明细</span>
              <el-radio-group v-model="costWindow" size="small" @change="loadCost">
                <el-radio-button label="today">今日</el-radio-button>
                <el-radio-button label="week">近 7 天</el-radio-button>
                <el-radio-button label="month">近 30 天</el-radio-button>
                <el-radio-button label="all">全部</el-radio-button>
              </el-radio-group>
            </div>
          </template>
          <el-table :data="costStats.by_provider || []" v-loading="loading.cost" stripe>
            <template #empty><el-empty description="暂无成本数据" /></template>
            <el-table-column prop="provider" label="Provider" min-width="140" />
            <el-table-column prop="call_count" label="调用次数" width="120" align="center" />
            <el-table-column prop="total_tokens" label="Token 用量" width="140" align="center" />
            <el-table-column prop="total_cost" label="成本(¥)" width="120" align="center">
              <template #default="{ row }">{{ (row.total_cost || 0).toFixed(4) }}</template>
            </el-table-column>
            <el-table-column label="占比" width="200" align="center">
              <template #default="{ row }">
                <el-progress
                  :percentage="calcRatio(row.total_cost)"
                  :stroke-width="10"
                />
              </template>
            </el-table-column>
          </el-table>
        </el-card>
        <el-card style="margin-top: 20px">
          <template #header><span>按场景用量</span></template>
          <el-table :data="costStats.by_scenario || []" stripe>
            <template #empty><el-empty description="暂无场景数据" /></template>
            <el-table-column prop="scenario" label="场景" min-width="160" />
            <el-table-column prop="call_count" label="调用次数" width="120" align="center" />
            <el-table-column prop="total_tokens" label="Token 用量" width="140" align="center" />
            <el-table-column prop="total_cost" label="成本(¥)" width="120" align="center">
              <template #default="{ row }">{{ (row.total_cost || 0).toFixed(4) }}</template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>

      
      <el-tab-pane label="路由审计" name="audit">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>路由变更审计日志（最近 50 条）</span>
              <el-button size="small" @click="loadAudit">
                <el-icon><Refresh /></el-icon> 刷新
              </el-button>
            </div>
          </template>
          <el-table :data="auditList" v-loading="loading.audit" stripe>
            <template #empty><el-empty description="暂无审计记录" /></template>
            <el-table-column label="时间" min-width="180">
              <template #default="{ row }">{{ formatTime(row.created_at) }}</template>
            </el-table-column>
            <el-table-column prop="scenario" label="场景" min-width="140" />
            <el-table-column prop="version" label="版本" width="80" align="center" />
            <el-table-column label="变更" min-width="280">
              <template #default="{ row }">
                <span v-if="row.action === 'update_strategy'">
                  <el-tag size="small" type="info">{{ row.prev_provider || '无' }}</el-tag>
                  →
                  <el-tag size="small" type="success">{{ row.new_provider }}</el-tag>
                </span>
                <el-tag v-else-if="row.action === 'create_model'" type="primary" size="small">新增模型</el-tag>
                <el-tag v-else-if="row.action === 'delete_model'" type="danger" size="small">删除模型</el-tag>
                <el-tag v-else-if="row.action === 'update_model'" type="warning" size="small">更新模型</el-tag>
                <el-tag v-else size="small">{{ getRoutingActionLabel(row.action) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="operator" label="操作人" width="120" />
            <el-table-column prop="trace_id" label="Trace ID" min-width="180" show-overflow-tooltip />
          </el-table>
        </el-card>
      </el-tab-pane>

      
      <el-tab-pane label="分类统计与出域审计" name="modeltype">
        <el-row :gutter="20" class="stat-row">
          <el-col :span="12">
            <el-card v-loading="loading.modelType">
              <template #header>
                <div class="card-header">
                  <span>本地 vs 云端 分类统计</span>
                  <el-radio-group v-model="modelTypeWindow" size="small" @change="loadModelType">
                    <el-radio-button label="today">今日</el-radio-button>
                    <el-radio-button label="week">近 7 天</el-radio-button>
                    <el-radio-button label="month">近 30 天</el-radio-button>
                    <el-radio-button label="all">全部</el-radio-button>
                  </el-radio-group>
                </div>
              </template>
              <el-row :gutter="12">
                <el-col :span="8">
                  <div class="metric-box local">
                    <div class="metric-label">本地模型调用</div>
                    <div class="metric-value">{{ getModelTypeCount('local') }}</div>
                    <div class="metric-sub">¥{{ getModelTypeCost('local').toFixed(4) }}</div>
                  </div>
                </el-col>
                <el-col :span="8">
                  <div class="metric-box cloud">
                    <div class="metric-label">云端模型调用</div>
                    <div class="metric-value">{{ getModelTypeCount('cloud') }}</div>
                    <div class="metric-sub">¥{{ getModelTypeCost('cloud').toFixed(4) }}</div>
                  </div>
                </el-col>
                <el-col :span="8">
                  <div class="metric-box">
                    <div class="metric-label">缺失计量（missing）</div>
                    <div class="metric-value">{{ getModelTypeCount('missing') }}</div>
                    <div class="metric-sub">{{ getMissingRatio() }}%</div>
                  </div>
                </el-col>
              </el-row>
              <el-divider />
              <div class="self-sufficiency">
                <div class="metric-label">本地自给率（{{ formatPercent(getSelfSufficiency()) }}%）</div>
                <el-progress
                  :percentage="Number(formatPercent(getSelfSufficiency()))"
                  :stroke-width="14"
                  :color="['#67c23a', '#e6a23c', '#f56c6c']"
                />
              </div>
            </el-card>
          </el-col>
          <el-col :span="12">
            <el-card v-loading="loading.health">
              <template #header>
                <div class="card-header">
                  <span>Provider 健康度（含熔断器）</span>
                  <el-button size="small" @click="loadHealth">
                    <el-icon><Refresh /></el-icon> 刷新
                  </el-button>
                </div>
              </template>
              <el-table :data="llmHealth.providers || []" stripe size="small">
                <template #empty><el-empty description="暂无健康数据" /></template>
                <el-table-column prop="name" label="Provider" min-width="120" />
                <el-table-column prop="vendor" label="厂商" width="100" />
                <el-table-column label="状态" width="100" align="center">
                  <template #default="{ row }">
                    <el-tag v-if="row.healthy" type="success" size="small">健康</el-tag>
                    <el-tag v-else-if="row.circuit_open" type="danger" size="small">熔断</el-tag>
                    <el-tag v-else type="warning" size="small">降级</el-tag>
                  </template>
                </el-table-column>
                <el-table-column label="错误" width="80" align="center">
                  <template #default="{ row }">{{ row.error_count || 0 }}</template>
                </el-table-column>
                <el-table-column prop="avg_latency" label="时延(ms)" width="100" align="center" />
                <el-table-column label="最后错误" min-width="160" show-overflow-tooltip>
                  <template #default="{ row }">{{ row.last_error || '-' }}</template>
                </el-table-column>
              </el-table>
            </el-card>
          </el-col>
        </el-row>
        <el-card style="margin-top: 20px" v-loading="loading.egress">
          <template #header>
            <div class="card-header">
              <span>出域告警（应本地但走云端的异常调用）</span>
              <el-button size="small" @click="loadEgressAlerts">
                <el-icon><Refresh /></el-icon> 刷新
              </el-button>
            </div>
          </template>
          <el-alert
            v-if="egressAlerts.length === 0"
            type="success"
            :closable="false"
            title="未发现出域异常"
            description="所有应当本地推理的请求均未出域"
            show-icon
          />
          <el-table v-else :data="egressAlerts" stripe size="small">
            <el-table-column prop="scenario" label="场景" min-width="120" />
            <el-table-column prop="provider" label="Provider" min-width="120" />
            <el-table-column prop="model" label="Model" min-width="140" />
            <el-table-column prop="base_url" label="出域 URL" min-width="240" show-overflow-tooltip />
            <el-table-column prop="vendor" label="厂商" width="100" />
            <el-table-column label="出域次数" width="100" align="center">
              <template #default="{ row }">{{ row.call_count || 0 }}</template>
            </el-table-column>
            <el-table-column label="首次出现" min-width="180">
              <template #default="{ row }">{{ formatTime(row.first_seen) }}</template>
            </el-table-column>
            <el-table-column label="最近出现" min-width="180">
              <template #default="{ row }">{{ formatTime(row.last_seen) }}</template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>
    </el-tabs>

    
    <el-dialog v-model="modelDialogVisible" :title="modelDialogTitle" width="640px">
      <el-form :model="modelForm" :rules="modelFormRules" ref="modelFormRef" label-width="100px">
        <el-form-item label="Provider 名称" prop="name">
          <el-input v-model="modelForm.name" placeholder="如 deepseek / default" :disabled="!!modelForm._isEdit" />
        </el-form-item>
        <el-form-item label="模型标识" prop="model">
          <el-input v-model="modelForm.model" placeholder="如 deepseek-chat / Qwen2.5-3B-Instruct" />
        </el-form-item>
        <el-form-item label="接入地址" prop="base_url">
          <el-input v-model="modelForm.base_url" placeholder="https://api.deepseek.com/v1 或 http://127.0.0.1:9000/v1" />
        </el-form-item>
        <el-form-item label="API Key" prop="api_key">
          <el-input v-model="modelForm.api_key" type="password" show-password placeholder="留空表示不修改" />
        </el-form-item>
        <el-form-item label="质量分">
          <el-input-number v-model="modelForm.quality_score" :min="0" :max="1" :step="0.01" :precision="2" />
          <span class="form-tip">0-1，必须 ≥ 场景路由的 min_quality 才被选中</span>
        </el-form-item>
        <el-form-item label="限流(RPM)">
          <el-input-number v-model="modelForm.max_rpm" :min="0" :step="10" />
        </el-form-item>
        <el-form-item label="成本(¥/1k)">
          <el-input-number v-model="modelForm.cost_per_1k" :min="0" :precision="4" :step="0.001" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="modelForm.enabled" />
        </el-form-item>
        <el-form-item label="NoFC">
          <el-switch v-model="modelForm.no_fc" />
          <span class="form-tip">本地 Qwen2.5-3B 不支持 FC，启用走 ReAct 适配器</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="modelDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitModel">确定</el-button>
      </template>
    </el-dialog>

    
    <el-dialog v-model="routingDialogVisible" :title="routingDialogTitle" width="600px">
      <el-form :model="routingForm" :rules="routingFormRules" ref="routingFormRef" label-width="120px">
        <el-form-item label="场景" prop="scenario">
          <el-select v-model="routingForm.scenario" placeholder="选择或输入场景" filterable allow-create style="width: 100%">
            <el-option v-for="s in scenarioPresets" :key="s" :label="s" :value="s" />
          </el-select>
        </el-form-item>
        <el-form-item label="首选 Provider" prop="provider">
          <el-select v-model="routingForm.provider" placeholder="选择模型" style="width: 100%">
            <el-option v-for="m in models" :key="m.name" :label="`${m.name} (${m.vendor})`" :value="m.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="Fallback 链">
          <el-select v-model="routingForm.fallbacks" multiple style="width: 100%" placeholder="按顺序选择备选">
            <el-option v-for="m in models" :key="m.name" :label="m.name" :value="m.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="灰度权重(%)">
          <el-input-number v-model="routingForm.weight" :min="0" :max="100" :step="5" />
          <span class="form-tip">0=全量旧路由；100=全量灰度；中间值按权重抽样</span>
        </el-form-item>
        <el-form-item label="最低质量">
          <el-input-number v-model="routingForm.min_quality" :min="0" :max="1" :step="0.01" :precision="2" />
        </el-form-item>
        <el-form-item label="最大时延(ms)">
          <el-input-number v-model="routingForm.max_latency" :min="0" :step="1000" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="routingDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submitRouting">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Refresh } from '@element-plus/icons-vue'
import { LlmRoutingApi } from '@/api/llmRouting.js'

const activeTab = ref('models')
const loading = reactive({ models: false, routing: false, fallback: false, cost: false, audit: false, modelType: false, health: false, egress: false })

const models = ref([]);
const sceneRouting = ref([])
const costStats = ref({})
const auditList = ref([])
const costWindow = ref('month')

const modelTypeWindow = ref('month');
const modelTypeStats = ref({ by_provider_type: [], self_sufficiency: 0, total_calls: 0 })
const llmHealth = ref({ status: 'unknown', providers: [], healthy_providers: 0, total_providers: 0 })
const egressAlerts = ref([])

const fallbackRows = computed(() => sceneRouting.value.map(r => ({
  scenario: r.scenario,
  provider: r.provider,
  fallbacks: r.fallbacks || [],
  version: r.version,
})));

const scenarioPresets = [
  'intent_recognize', 'sop_reply', 'objection', 'friendly_chat',
  'long_summary', 'high_quality', 'low_cost',
]

const canaryColors = [
  { color: '#909399', percentage: 30 },
  { color: '#F59E0B', percentage: 60 },
  { color: '#EF4444', percentage: 100 },
]

const modelDialogVisible = ref(false);
const modelDialogTitle = ref('新增模型')
const modelFormRef = ref()
const emptyModelForm = () => ({
  name: '', model: '', base_url: '', api_key: '',
  quality_score: 0.95, max_rpm: 60, cost_per_1k: 0,
  enabled: true, no_fc: false, _isEdit: false,
})
const modelForm = ref(emptyModelForm())
const modelFormRules = {
  name: [{ required: true, message: '请输入 Provider 名称', trigger: 'blur' }],
  model: [{ required: true, message: '请输入模型标识', trigger: 'blur' }],
  base_url: [{ required: true, message: '请输入接入地址', trigger: 'blur' }],
}

const routingDialogVisible = ref(false);
const routingDialogTitle = ref('新增映射')
const routingFormRef = ref()
const emptyRoutingForm = () => ({
  scenario: '',
  provider: '',
  fallbacks: [],
  weight: 0,
  min_quality: 0.8,
  max_latency: 90000,
})
const routingForm = ref(emptyRoutingForm())
const routingEditIndex = ref(-1)
const routingFormRules = {
  scenario: [{ required: true, message: '请选择场景', trigger: 'change' }],
  provider: [{ required: true, message: '请选择 Provider', trigger: 'change' }],
}

const loadModels = async () => {
  loading.models = true
  try {
    const res = await LlmRoutingApi.getModelList()
    models.value = Array.isArray(res) ? res : (res?.list || res?.items || res?.data || [])
  } catch (e) {
    models.value = []
  } finally {
    loading.models = false
  }
};

const loadRouting = async () => {
  loading.routing = true
  try {
    const res = await LlmRoutingApi.getSceneRouting()
    sceneRouting.value = Array.isArray(res) ? res : (res?.list || res?.items || res?.data || [])
  } catch (e) {
    sceneRouting.value = []
  } finally {
    loading.routing = false
  }
}

const loadFallback = async () => {
  loading.fallback = true
  try {
    if (sceneRouting.value.length === 0) {
      await loadRouting()
    }
  } catch (e) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ } finally {
    loading.fallback = false
  }
}

const loadCost = async () => {
  loading.cost = true
  try {
    const res = await LlmRoutingApi.getCostStats(costWindow.value)
    costStats.value = res || {}
  } catch (e) {
    costStats.value = {}
  } finally {
    loading.cost = false
  }
}

const loadAudit = async () => {
  loading.audit = true
  try {
    const res = await LlmRoutingApi.getAuditHistory(null, 50)
    auditList.value = Array.isArray(res) ? res : (res?.list || res?.items || res?.data || [])
  } catch (e) {
    auditList.value = []
  } finally {
    loading.audit = false
  }
}

const loadModelType = async () => {
  loading.modelType = true
  try {
    const res = await LlmRoutingApi.getModelTypeStats(modelTypeWindow.value)
    modelTypeStats.value = res || { by_provider_type: [], self_sufficiency: 0, total_calls: 0 }
  } catch (e) {
    modelTypeStats.value = { by_provider_type: [], self_sufficiency: 0, total_calls: 0 }
  } finally {
    loading.modelType = false
  }
};

const loadHealth = async () => {
  loading.health = true
  try {
    const res = await LlmRoutingApi.getHealth()
    llmHealth.value = res || { status: 'unknown', providers: [], healthy_providers: 0, total_providers: 0 }
  } catch (e) {
    llmHealth.value = { status: 'unknown', providers: [], healthy_providers: 0, total_providers: 0 }
  } finally {
    loading.health = false
  }
}

const loadEgressAlerts = async () => {
  loading.egress = true
  try {
    const res = await LlmRoutingApi.getEgressAlerts()
    egressAlerts.value = Array.isArray(res) ? res : (res?.list || res?.items || res?.data || [])
  } catch (e) {
    egressAlerts.value = []
  } finally {
    loading.egress = false
  }
}

const findModelTypeRow = (modelType) => {
  const arr = modelTypeStats.value.by_provider_type || []
  return arr.find(r => r.model_type === modelType) || {}
};

const getModelTypeCount = (modelType) => {
  return Number(findModelTypeRow(modelType).call_count || 0)
}

const getModelTypeCost = (modelType) => {
  return Number(findModelTypeRow(modelType).total_cost || 0)
}

const getSelfSufficiency = () => {
  const raw = Number(modelTypeStats.value.self_sufficiency || 0);
  return raw <= 1 ? raw * 100 : raw
}

const getMissingRatio = () => {
  const total = Number(modelTypeStats.value.total_calls || 0)
  if (total === 0) return '0.00'
  const missing = getModelTypeCount('missing')
  return ((missing / total) * 100).toFixed(2)
}

const formatPercent = (v) => {
  const n = Number(v || 0)
  return Math.min(100, Math.max(0, n)).toFixed(2)
}

const refreshAll = () => {
  loadModels()
  loadRouting()
  loadCost()
  loadAudit()
  loadModelType()
  loadHealth()
  loadEgressAlerts()
}

const showModelDialog = (row) => {
  if (row) {
    modelForm.value = {
      ...emptyModelForm(),
      name: row.name,
      model: row.model,
      base_url: row.base_url,
      api_key: '',
      quality_score: row.quality_score,
      max_rpm: row.max_rpm,
      cost_per_1k: row.cost_per_1k,
      enabled: row.enabled,
      no_fc: row.no_fc,
      _isEdit: true,
    }
    modelDialogTitle.value = '编辑模型'
  } else {
    modelForm.value = emptyModelForm()
    modelDialogTitle.value = '新增模型'
  }
  modelDialogVisible.value = true
};

const submitModel = async () => {
  if (!modelFormRef.value) return
  await modelFormRef.value.validate(async (valid) => {
    if (!valid) return
    try {
      const payload = { ...modelForm.value }
      delete payload._isEdit
      if (modelForm.value._isEdit) {
        await LlmRoutingApi.updateModel(modelForm.value.name, payload);
        ElMessage.success('更新成功')
      } else {
        await LlmRoutingApi.createModel(payload)
        ElMessage.success('新增成功')
      }
      modelDialogVisible.value = false
      loadModels()
    } catch (e) {
      ElMessage.error(e?.message || '保存失败')
    }
  })
}

const deleteModel = async (row) => {
  try {
    await ElMessageBox.confirm(`确定删除模型 "${row.name}" 吗？`, '确认', { type: 'warning' })
    await LlmRoutingApi.deleteModel(row.name)
    ElMessage.success('删除成功')
    loadModels()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('删除失败')
  }
}

const toggleStatus = async (row) => {
  const next = !row.enabled
  try {
    await LlmRoutingApi.updateModel(row.name, { enabled: next });
    ElMessage.success('状态已更新')
    loadModels()
  } catch (e) {
    ElMessage.error('状态更新失败：' + (e?.message || ''))
  }
}

const testModel = async (row) => {
  try {
    const result = await LlmRoutingApi.testModel(row.name, { prompt: '你好，请用一句话自我介绍。', timeout_seconds: 30 })
    if (result?.success) {
      ElMessage.success(`模型 "${row.name}" 连通性正常（延迟 ${result.latency_ms}ms）`)
    } else {
      ElMessage.warning(`模型 "${row.name}" 调用失败：${result?.error || '未知错误'}`)
    }
  } catch (e) {
    ElMessage.error('测试失败：' + (e?.message || ''))
  }
}

const addRoutingRow = () => {
  routingForm.value = emptyRoutingForm()
  routingEditIndex.value = -1
  routingDialogTitle.value = '新增映射'
  routingDialogVisible.value = true
};

const editRoutingRow = (row, index) => {
  routingForm.value = {
    scenario: row.scenario,
    provider: row.provider,
    fallbacks: [...(row.fallbacks || [])],
    weight: row.weight || 0,
    min_quality: row.min_quality || 0.8,
    max_latency: row.max_latency || 90000,
  }
  routingEditIndex.value = index
  routingDialogTitle.value = '编辑映射'
  routingDialogVisible.value = true
}

const removeRoutingRow = async (index) => {
  try {
    await ElMessageBox.confirm('确定删除该路由吗？', '确认', { type: 'warning' })
    const removed = sceneRouting.value[index]
    const next = sceneRouting.value.filter((_, i) => i !== index)
    sceneRouting.value = next
    await LlmRoutingApi.saveSceneRouting(next);
    ElMessage.success(`已删除 ${removed?.scenario || ''}`)
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('删除失败')
  }
}

const submitRouting = async () => {
  if (!routingFormRef.value) return
  await routingFormRef.value.validate(async (valid) => {
    if (!valid) return
    const next = [...sceneRouting.value]
    if (routingEditIndex.value >= 0) {
      next[routingEditIndex.value] = { ...routingForm.value }
    } else {
      next.push({ ...routingForm.value })
    }
    try {
      await LlmRoutingApi.saveSceneRouting(next)
      ElMessage.success('保存成功')
      routingDialogVisible.value = false
      loadRouting()
    } catch (e) {
      ElMessage.error('保存失败：' + (e?.message || ''))
    }
  })
}

const vendorLabel = (v) => ({
  local: '本地', deepseek: 'DeepSeek', qwen: '通义千问',
  openai: 'OpenAI', zhipu: '智谱', moonshot: '月之暗面', other: '其它',
}[v] || v || '-');

const formatNumber = (n) => {
  if (!n) return 0
  return Number(n).toLocaleString()
}

const formatTime = (s) => {
  if (!s) return '-'
  return new Date(s).toLocaleString('zh-CN')
}

const calcRatio = (cost) => {
  if (!cost || !costStats.value.monthly_cost) return 0
  const total = costStats.value.monthly_cost;
  if (total <= 0) return 0
  return Math.min(100, Math.round((cost / total) * 100))
}

onMounted(() => {
  refreshAll()
})
</script>

<style scoped lang="scss">
.llm-routing-page { padding: 20px; }
.header-card {
  margin-bottom: 20px;
  :deep(.el-card__body) {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .header-content h2 { margin: 0 0 8px 0; }
  .subtitle { color: #909399; margin: 0; }
  .header-actions { display: flex; gap: 10px; }
}
.content-tabs { background: #fff; padding: 16px; border-radius: 4px; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.stat-row { margin-bottom: 20px; }
.stat-card {
  text-align: center;
  .stat-label { color: #909399; font-size: 14px; margin-bottom: 10px; }
  .stat-value { font-size: 28px; font-weight: bold; }
}
.form-tip { color: #909399; font-size: 12px; margin-left: 12px; }
.text-muted { color: #c0c4cc; }
.cost-free { color: #67C23A; }
.cost-paid { color: #F56C6C; }

// v3.7.0：分类统计 / 自给率 metric-box
.metric-box {
  padding: 16px 8px;
  border-radius: 8px;
  background: #f7f8fa;
  text-align: center;
  margin-bottom: 8px;
  .metric-label { color: #909399; font-size: 13px; }
  .metric-value { font-size: 28px; font-weight: 700; color: #303133; margin: 4px 0; }
  .metric-sub { color: #909399; font-size: 12px; }
  &.local { background: #f0f9eb; }
  &.cloud { background: #fdf6ec; }
}
.self-sufficiency { padding: 0 4px; }
</style>
