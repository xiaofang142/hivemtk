<template>
  <div class="geo-page">
    <div class="p-4">
      <el-row
        :gutter="16"
        class="mb-4"
      >
        <el-col :span="6">
          <el-card>
            <el-statistic
              title="实体总数"
              :value="entityTotal"
            />
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card>
            <el-statistic
              title="关系总数"
              :value="totalRelations"
            />
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card>
            <el-statistic
              title="关系类型"
              :value="relationTypeCount"
            />
          </el-card>
        </el-col>
        <el-col :span="6">
          <el-card>
            <el-statistic
              title="已生成 Schema"
              :value="schemaGenerated ? 1 : 0"
            />
          </el-card>
        </el-col>
      </el-row>

      <el-row
        :gutter="16"
        style="min-height:500px"
      >
        <el-col :span="8">
          <el-card style="height:100%">
            <template #header>
              <div class="flex items-center gap-2">
                <span class="font-bold">实体列表</span>
                <el-select
                  v-model="typeFilter"
                  placeholder="按类型筛选"
                  size="small"
                  style="width:120px"
                  clearable
                  @change="reloadFromFirstPage"
                  @clear="reloadFromFirstPage"
                >
                  <el-option
                    v-for="t in entityTypes"
                    :key="t"
                    :label="t"
                    :value="t"
                  />
                </el-select>
                <el-input
                  v-model="keyword"
                  placeholder="搜索"
                  size="small"
                  style="width:140px"
                  clearable
                  @clear="reloadFromFirstPage"
                  @keyup.enter="reloadFromFirstPage"
                />
              </div>
            </template>
            <el-table
              ref="entityTable"
              v-loading="loading"
              :data="entities"
              size="small"
              highlight-current-row
              @row-click="onSelectEntity"
            >
              <el-table-column
                prop="name"
                label="名称"
                min-width="140"
                show-overflow-tooltip
              />
              <el-table-column
                prop="type"
                label="类型"
                width="100"
              >
                <template #default="{ row }">
                  <el-tag size="small">
                    {{ row.type }}
                  </el-tag>
                </template>
              </el-table-column>
            </el-table>
            <!-- 原来只展示接口默认的 20 行、没有分页，其余 300+ 实体无法浏览 -->
            <el-pagination
              v-model:current-page="page"
              v-model:page-size="pageSize"
              class="mt-2"
              small
              layout="prev, pager, next, total"
              :total="entityTotal"
              :page-size="pageSize"
              @current-change="loadEntities"
            />
          </el-card>
        </el-col>

      
        <el-col :span="16">
          <el-card class="mb-4">
            <template #header>
              <div class="flex items-center justify-between">
                <span class="font-bold">实体关系图{{ currentEntity ? ` · ${currentEntity.name}` : '' }}</span>
                <el-button
                  size="small"
                  type="primary"
                  :disabled="!currentEntity"
                  :loading="schemaLoading"
                  @click="onGenerateSchema"
                >
                  生成 JSON-LD Schema
                </el-button>
              </div>
            </template>
          
            <div
              ref="graphRef"
              class="graph-container"
            >
              <svg
                :viewBox="`0 0 ${svgSize.w} ${svgSize.h}`"
                preserveAspectRatio="xMidYMid meet"
              >
              
                <g v-if="currentEntity">
                  <line
                    v-for="(rel, i) in relations"
                    :key="'l'+i"
                    :x1="svgSize.w/2"
                    :y1="svgSize.h/2"
                    :x2="rel.node?.x || 0"
                    :y2="rel.node?.y || 0"
                    stroke="#c0c4cc"
                    stroke-width="1.5"
                  />
                  <text
                    v-for="(rel, i) in relations"
                    :key="'t'+i"
                    :x="((svgSize.w/2) + (rel.node?.x || 0)) / 2"
                    :y="((svgSize.h/2) + (rel.node?.y || 0)) / 2 - 4"
                    fill="#909399"
                    font-size="11"
                    text-anchor="middle"
                  >
                    {{ rel.relation_type || rel.relationType || '关联' }}
                  </text>
                
                  <circle
                    :cx="svgSize.w/2"
                    :cy="svgSize.h/2"
                    r="36"
                    fill="#409eff"
                  />
                  <text
                    :x="svgSize.w/2"
                    :y="svgSize.h/2 + 4"
                    text-anchor="middle"
                    fill="#fff"
                    font-size="13"
                    font-weight="600"
                  >
                    {{ currentEntity.name }}
                  </text>
                
                  <g
                    v-for="(rel, i) in relations"
                    :key="'n'+i"
                  >
                    <circle
                      :cx="rel.node?.x || 0"
                      :cy="rel.node?.y || 0"
                      r="24"
                      fill="#10b981"
                    />
                    <text
                      :x="rel.node?.x || 0"
                      :y="(rel.node?.y || 0) + 4"
                      text-anchor="middle"
                      fill="#fff"
                      font-size="11"
                    >
                      {{ (rel.target_name || rel.targetName || rel.target_id || '?').slice(0, 6) }}
                    </text>
                  </g>
                </g>
                <text
                  v-else
                  :x="svgSize.w/2"
                  :y="svgSize.h/2"
                  text-anchor="middle"
                  fill="#c0c4cc"
                  font-size="14"
                >
                  请在左侧选择实体以查看关系图
                </text>
              </svg>
            </div>
          </el-card>

          <el-card>
            <template #header>
              <div class="flex items-center justify-between">
                <span class="font-bold">JSON-LD Schema 预览</span>
                <el-button
                  size="small"
                  :disabled="!schema"
                  @click="onCopySchema"
                >
                  复制
                </el-button>
              </div>
            </template>
            <div
              v-if="!schema"
              class="py-8 text-center text-gray-400"
            >
              暂无 JSON-LD Schema，点击上方「生成 JSON-LD Schema」按钮生成
            </div>
            <pre
              v-else
              class="schema-pre"
            >{{ schema }}</pre>
          </el-card>
        </el-col>
      </el-row>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { copyToClipboard } from '@/utils/clipboard'
import { listEntities, getEntityRelations, extractEntities } from '@/api/geoEntity.js'
import { geoApi } from '@/api/geo.js'

const entities = ref([])
// 接口返回 total（全库 358），列表本身只有一页 20 行，
// KPI 以前用 entities.length 统计，等于把「实体总数」显示成分页大小。
const entityTotal = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const typeFilter = ref('')
const keyword = ref('')
const currentEntity = ref(null)
const relations = ref([])
const schema = ref('')
const schemaLoading = ref(false)
const entityTable = ref(null)

// 类型下拉：既有类型枚举 + 已加载行的类型（后端数据存在大小写变体，如 Concept/concept）
const ENTITY_TYPES = ['Concept', 'Product', 'Person', 'Organization', 'Location']
const entityTypes = computed(() => {
  const set = new Set([...ENTITY_TYPES, ...entities.value.map(e => e.type).filter(Boolean)])
  return [...set]
})
const totalRelations = computed(() => relations.value.length)
const relationTypeCount = computed(() => new Set(relations.value.map(r => r.relation_type || r.relationType)).size)
const schemaGenerated = computed(() => !!schema.value)

const svgSize = { w: 600, h: 300 }

const layoutRelations = (rels) => {
  const n = rels.length
  if (n === 0) return []
  const cx = svgSize.w / 2, cy = svgSize.h / 2
  const R = Math.min(svgSize.w, svgSize.h) / 2 - 60
  return rels.map((r, i) => {
    const angle = (2 * Math.PI * i) / n - Math.PI / 2
    return { ...r, node: { x: cx + R * Math.cos(angle), y: cy + R * Math.sin(angle) } }
  })
}

const loadEntities = async () => {
  loading.value = true
  try {
    const data = await listEntities(typeFilter.value, keyword.value, page.value, pageSize.value)
    entities.value = Array.isArray(data) ? data : (data?.list || data?.items || [])
    entityTotal.value = Array.isArray(data) ? entities.value.length : Number(data?.total || entities.value.length)
  } catch { entities.value = []; entityTotal.value = 0 }
  loading.value = false
}

// 改筛选/搜索条件时回到第 1 页，否则会停在越界页显示空列表
const reloadFromFirstPage = () => {
  page.value = 1
  loadEntities()
}

const onSelectEntity = async (row) => {
  currentEntity.value = row
  schema.value = ''
  try {
    const data = await getEntityRelations(row.entity_id || row.id)
    const list = Array.isArray(data) ? data : (data?.relations || data?.list || [])
    relations.value = layoutRelations(list)
  } catch { relations.value = [] }
}

const onGenerateSchema = async () => {
  if (!currentEntity.value) return
  schemaLoading.value = true
  try {
    let brandName = 'HiveMTK', advantages = '', domain = '';
    try {
      const cfg = await geoApi.getConfig()
      brandName = cfg?.brand_name || brandName
      advantages = cfg?.advantages || ''
      domain = cfg?.domain || ''
    } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
    const data = await geoApi.generateSchema({
      content: currentEntity.value.description || currentEntity.value.name,
      brand_name: brandName,
      advantages,
      keyword: currentEntity.value.name,
      domain
    })
    const result = typeof data === 'string' ? data : (data?.schema_json || data?.schema || JSON.stringify(data, null, 2))
    try {
      const parsed = JSON.parse(typeof result === 'string' ? result : JSON.stringify(result))
      schema.value = JSON.stringify(parsed, null, 2)
    } catch {
      schema.value = typeof result === 'string' ? result : JSON.stringify(result, null, 2)
    }
    ElMessage.success('JSON-LD Schema 生成成功')
  } catch (e) {
    ElMessage.error('生成失败：' + (e?.message || e))
  }
  schemaLoading.value = false
}

const onCopySchema = async () => {
  if (await copyToClipboard(schema.value)) {
    ElMessage.success('已复制到剪贴板')
  } else {
    ElMessage.error('复制失败')
  }
}

onMounted(loadEntities)
</script>

<style lang="scss" scoped>
.graph-container {
  background: var(--el-fill-color-lighter);
  border-radius: 6px;
  min-height: 300px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.schema-pre {
  background: $text-primary;
  color: $border-base;
  padding: $spacing-md;
  border-radius: 6px;
  max-height: 360px;
  overflow: auto;
  font-size: $font-size-extra-small;
  line-height: 1.5;
  margin: 0;
}
</style>
