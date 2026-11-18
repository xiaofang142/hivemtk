<template>
  <div class="dictionary-page">
    <el-alert
      type="info"
      show-icon
      :closable="false"
      class="page-intro"
      title="业务字典管的是「有哪几种取值」和「从 A 能不能到 B」，与「动态阈值参数」不是一个东西"
      description="阈值参数回答“这个旋钮拧到几”，业务字典回答“这一列有哪些合法值、它们之间允许怎么流转”。字典描述的是存量行正在使用的值域，所以字典项只增不删：要停用一项请把它关掉，不要删——历史行里还躺着这个值。"
    />

    <el-row :gutter="16">
      <el-col
        :xs="24"
        :md="7"
        :lg="6"
      >
        <el-card
          shadow="hover"
          class="parent-card"
        >
          <template #header>
            <div class="card-header">
              <span>字典域</span>
              <el-button
                text
                size="small"
                :loading="parentsLoading"
                @click="loadParents"
              >
                <el-icon><Refresh /></el-icon>
              </el-button>
            </div>
          </template>
          <div
            v-if="parents.length === 0 && !parentsLoading"
            class="empty-hint"
          >
            后端没有返回任何字典域
          </div>
          <div class="parent-list">
            <div
              v-for="p in parents"
              :key="p.code"
              class="parent-item"
              role="button"
              tabindex="0"
              :class="{ active: currentParent === p.code }"
              @click="selectParent(p.code)"
              @keydown.enter.prevent="selectParent(p.code)"
              @keydown.space.prevent="selectParent(p.code)"
            >
              <div class="parent-item__main">
                <div class="parent-name">
                  {{ p.label }}
                  <el-tag
                    v-if="p.seeded"
                    size="small"
                    effect="plain"
                    type="info"
                  >
                    代码种子
                  </el-tag>
                </div>
                <code class="parent-code">{{ p.code }}</code>
              </div>
              <span class="parent-count">{{ p.item_count }}</span>
            </div>
          </div>
        </el-card>
      </el-col>

      <el-col
        :xs="24"
        :md="17"
        :lg="18"
      >
        <el-card
          shadow="hover"
          class="detail-card"
        >
          <template #header>
            <div class="card-header">
              <div class="header-title">
                <el-tag
                  type="info"
                  effect="plain"
                  class="group-tag"
                >
                  {{ currentParentLabel }}
                </el-tag>
                <span class="header-count">{{ items.length }} 项 · {{ transitions.length }} 条跃迁</span>
              </div>
              <div class="header-actions">
                <el-button
                  :disabled="!currentParent"
                  @click="openItemDialog()"
                >
                  <el-icon><Plus /></el-icon>新增字典项
                </el-button>
                <el-button
                  :disabled="!currentParent"
                  @click="openTransitionDialog"
                >
                  <el-icon><Connection /></el-icon>编辑跃迁
                </el-button>
                <el-button
                  :loading="detailLoading"
                  @click="loadDetail"
                >
                  <el-icon><Refresh /></el-icon>刷新
                </el-button>
              </div>
            </div>
          </template>

          <div
            v-if="currentParentDesc"
            class="parent-desc"
          >
            {{ currentParentDesc }}
          </div>

          <div
            v-if="!currentParent"
            class="empty-block"
          >
            从左侧选一个字典域
          </div>

          <template v-else>
            <div class="section-title">
              字典项
            </div>
            <el-table
              v-loading="detailLoading"
              :data="items"
              stripe
              border
              empty-text="该域还没有字典项"
              style="width: 100%"
              :header-cell-style="{ background: '#fafafa' }"
            >
              <el-table-column
                prop="code"
                label="Code"
                width="160"
              >
                <template #default="{ row }">
                  <code class="item-code">{{ row.code }}</code>
                </template>
              </el-table-column>
              <el-table-column
                prop="name"
                label="名称"
                width="140"
              />
              <el-table-column
                prop="description"
                label="说明"
                min-width="240"
                show-overflow-tooltip
              >
                <template #default="{ row }">
                  {{ row.description || '-' }}
                </template>
              </el-table-column>
              <el-table-column
                prop="sort_order"
                label="排序"
                width="70"
                align="center"
              />
              <el-table-column
                label="状态"
                width="130"
                align="center"
              >
                <template #default="{ row }">
                  <el-tag
                    v-if="row.enabled"
                    type="success"
                    size="small"
                    effect="plain"
                  >
                    启用
                  </el-tag>
                  <el-tag
                    v-else
                    type="info"
                    size="small"
                    effect="plain"
                  >
                    已停用
                  </el-tag>
                  <el-tooltip
                    v-if="row.system_locked"
                    content="由代码种子锁定：代码常量是权威的一侧，改名会让两侧分叉"
                    placement="top"
                  >
                    <el-tag
                      type="warning"
                      size="small"
                      effect="dark"
                      class="locked-tag"
                    >
                      锁定
                    </el-tag>
                  </el-tooltip>
                </template>
              </el-table-column>
              <el-table-column
                label="操作"
                width="120"
                fixed="right"
                align="center"
              >
                <template #default="{ row }">
                  <el-button
                    link
                    type="primary"
                    size="small"
                    @click="openItemDialog(row)"
                  >
                    <el-icon><Edit /></el-icon>编辑
                  </el-button>
                </template>
              </el-table-column>
            </el-table>

            <div class="section-title section-title--trans">
              状态跃迁
            </div>
            <div
              v-if="transitions.length === 0"
              class="empty-block empty-block--sm"
            >
              该域没有定义跃迁（纯枚举域，例如只有取值没有流转概念）
            </div>
            <el-table
              v-else
              v-loading="detailLoading"
              :data="transitionRows"
              stripe
              border
              style="width: 100%"
              :header-cell-style="{ background: '#fafafa' }"
            >
              <el-table-column
                label="从"
                min-width="200"
              >
                <template #default="{ row }">
                  <code class="item-code">{{ row.from }}</code>
                  <span class="trans-name">{{ row.fromName }}</span>
                </template>
              </el-table-column>
              <el-table-column
                label="→"
                width="50"
                align="center"
              >
                <template #default>
                  <el-icon class="trans-arrow">
                    <Right />
                  </el-icon>
                </template>
              </el-table-column>
              <el-table-column
                label="到"
                min-width="200"
              >
                <template #default="{ row }">
                  <code class="item-code">{{ row.to }}</code>
                  <span class="trans-name">{{ row.toName }}</span>
                </template>
              </el-table-column>
              <el-table-column
                prop="description"
                label="触发条件"
                min-width="220"
                show-overflow-tooltip
              >
                <template #default="{ row }">
                  {{ row.description || '-' }}
                </template>
              </el-table-column>
            </el-table>
          </template>
        </el-card>
      </el-col>
    </el-row>

    <el-dialog
      v-model="itemDialogVisible"
      :title="itemForm.code ? `编辑字典项 · ${itemForm.parent_code}.${itemForm.code}` : `新增字典项 · ${currentParent}`"
      width="520px"
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-form
        label-width="88px"
        @submit.prevent
      >
        <el-form-item
          label="Code"
          required
        >
          <el-input
            v-model="itemForm.code"
            placeholder="例如 approved；只能用小写字母、数字、下划线"
            :disabled="!!itemForm.existing && itemForm.existing.system_locked"
          />
          <div class="form-hint">
            这是写进数据库存量的值，一旦有数据就不该再改。
          </div>
        </el-form-item>
        <el-form-item
          label="名称"
          required
        >
          <el-input
            v-model="itemForm.name"
            placeholder="给用户看的中文名"
          />
        </el-form-item>
        <el-form-item label="说明">
          <el-input
            v-model="itemForm.description"
            type="textarea"
            :rows="3"
            placeholder="这一项代表什么、在什么情况下出现"
          />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number
            v-model="itemForm.sort_order"
            :step="1"
            :min="0"
            controls-position="right"
          />
          <div class="form-hint">
            数字小的排前面。
          </div>
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="itemForm.enabled" />
          <div class="form-hint">
            关掉后不参与新写入，但历史行仍可能是它——所以停用而不是删除，
            页面上也仍要能把它译成人话。
          </div>
        </el-form-item>
      </el-form>
      <div
        v-if="itemError"
        class="form-error"
      >
        <el-icon><WarningFilled /></el-icon> {{ itemError }}
      </div>
      <template #footer>
        <el-button @click="itemDialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="savingItem"
          @click="submitItem"
        >
          保存
        </el-button>
      </template>
    </el-dialog>

    <el-dialog
      v-model="transitionDialogVisible"
      title="编辑状态跃迁"
      width="760px"
      :close-on-click-modal="false"
      destroy-on-close
    >
      <el-alert
        type="warning"
        show-icon
        :closable="false"
        title="保存是整体替换"
        description="这里提交的是该域的完整跃迁集合，不是增量。后端 ReplaceTransitions 会先清空再写入，所以列表里删掉的行是真的没了。"
      />
      <el-table
        :data="transitionDraft"
        border
        size="small"
        style="width:100%; margin-top:12px"
      >
        <el-table-column
          label="从"
          width="190"
        >
          <template #default="{ row }">
            <el-select
              v-model="row.from_code"
              filterable
              placeholder="起点"
              style="width:100%"
            >
              <el-option
                v-for="c in items"
                :key="c.code"
                :label="`${c.name} (${c.code})`"
                :value="c.code"
              />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column
          label="→"
          width="40"
          align="center"
        >
          <template #default>
            <el-icon class="trans-arrow">
              <Right />
            </el-icon>
          </template>
        </el-table-column>
        <el-table-column
          label="到"
          width="190"
        >
          <template #default="{ row }">
            <el-select
              v-model="row.to_code"
              filterable
              placeholder="终点"
              style="width:100%"
            >
              <el-option
                v-for="c in items"
                :key="c.code"
                :label="`${c.name} (${c.code})`"
                :value="c.code"
              />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column
          label="触发条件"
          min-width="240"
        >
          <template #default="{ row }">
            <el-input
              v-model="row.description"
              placeholder="什么情况下会发生这次流转"
            />
          </template>
        </el-table-column>
        <el-table-column
          label="操作"
          width="70"
          align="center"
        >
          <template #default="{ $index }">
            <el-button
              link
              type="danger"
              size="small"
              @click="removeTransitionRow($index)"
            >
              <el-icon><Delete /></el-icon>
            </el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="dialog-actions">
        <el-button
          size="small"
          @click="addTransitionRow"
        >
          <el-icon><Plus /></el-icon>加一条跃迁
        </el-button>
      </div>
      <div
        v-if="transitionError"
        class="form-error"
      >
        <el-icon><WarningFilled /></el-icon> {{ transitionError }}
      </div>
      <template #footer>
        <el-button @click="transitionDialogVisible = false">
          取消
        </el-button>
        <el-button
          type="primary"
          :loading="savingTransitions"
          @click="submitTransitions"
        >
          整体替换
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { dictionariesApi } from '@/api/runtimeConfig'

// Code 只允许小写字母、数字、下划线：它要写进数据库、拼进 API 路径、
// 还会出现在前端路由/状态名里，混进大写或空格会让后面每一层都得转义。
const CODE_RE = /^[a-z][a-z0-9_]*$/

const parents = ref([])
const parentsLoading = ref(false)
const currentParent = ref('')

const items = ref([])
const transitions = ref([])
const detailLoading = ref(false)

const parentMap = computed(() => {
  const m = new Map()
  for (const p of parents.value) m.set(p.code, p)
  return m
})

const currentParentLabel = computed(() => {
  const p = parentMap.value.get(currentParent.value)
  return p ? `${p.label} / ${p.code}` : (currentParent.value || '未选择')
})

const currentParentDesc = computed(() => {
  const p = parentMap.value.get(currentParent.value)
  return p ? (p.description || '') : ''
})

// 跃迁表里补上中文名：后端只存 from_code / to_code，
// 运营盯着两个英文 code 看不出这是「待审批 → 已批准」。
const transitionRows = computed(() => {
  const nameByCode = new Map(items.value.map((i) => [i.code, i.name]))
  return transitions.value.map((t) => ({
    from: t.from_code,
    fromName: nameByCode.get(t.from_code) || '（未登记）',
    to: t.to_code,
    toName: nameByCode.get(t.to_code) || '（未登记）',
    description: t.description
  }))
})

function unwrap(res) {
  if (Array.isArray(res)) return res
  if (res && Array.isArray(res.list)) return res.list
  return []
}

async function loadParents() {
  parentsLoading.value = true
  try {
    parents.value = unwrap(await dictionariesApi.listParents())
    if (!parents.value.some((p) => p.code === currentParent.value)) {
      currentParent.value = parents.value.length ? parents.value[0].code : ''
    }
    if (currentParent.value) loadDetail()
  } catch (e) {
    ElMessage.error('加载字典域失败：' + (e.message || '未知错误'))
  } finally {
    parentsLoading.value = false
  }
}

async function loadDetail() {
  if (!currentParent.value) {
    items.value = []
    transitions.value = []
    return
  }
  detailLoading.value = true
  try {
    const res = await dictionariesApi.get(currentParent.value)
    items.value = Array.isArray(res && res.items) ? res.items : []
    transitions.value = Array.isArray(res && res.transitions) ? res.transitions : []
  } catch (e) {
    items.value = []
    transitions.value = []
    ElMessage.error('加载字典详情失败：' + (e.message || '未知错误'))
  } finally {
    detailLoading.value = false
  }
}

function selectParent(code) {
  currentParent.value = code
  loadDetail()
}

onMounted(() => {
  loadParents()
})

/* ── 字典项 CRUD ── */

const itemDialogVisible = ref(false)
const savingItem = ref(false)
const itemError = ref('')
const itemForm = ref(emptyItemForm())

function emptyItemForm() {
  return { parent_code: '', code: '', name: '', description: '', sort_order: 0, enabled: true, existing: null }
}

function openItemDialog(row) {
  if (!currentParent.value) {
    ElMessage.warning('先在左侧选一个字典域')
    return
  }
  if (row) {
    itemForm.value = {
      parent_code: row.parent_code,
      code: row.code,
      name: row.name,
      description: row.description || '',
      sort_order: Number(row.sort_order || 0),
      enabled: row.enabled !== false,
      existing: { ...row }
    }
  } else {
    const maxSort = items.value.reduce((m, i) => Math.max(m, Number(i.sort_order || 0)), 0)
    itemForm.value = { ...emptyItemForm(), parent_code: currentParent.value, sort_order: maxSort + 1 }
  }
  itemError.value = ''
  itemDialogVisible.value = true
}

async function submitItem() {
  const f = itemForm.value
  itemError.value = ''
  if (!CODE_RE.test(String(f.code || ''))) {
    itemError.value = 'Code 只能是小写字母开头的 [a-z0-9_]，例如 pending'
    return
  }
  if (!String(f.name || '').trim()) {
    itemError.value = '名称不能为空'
    return
  }
  savingItem.value = true
  try {
    await dictionariesApi.upsertItem(f.parent_code, {
      code: f.code,
      name: String(f.name).trim(),
      description: f.description,
      sort_order: Number(f.sort_order || 0),
      enabled: !!f.enabled
    })
    itemDialogVisible.value = false
    ElMessage.success('已保存')
    await loadDetail()
    await loadParents()
  } catch (e) {
    itemError.value = e.message || '保存失败'
  } finally {
    savingItem.value = false
  }
}

/* ── 跃迁编辑（整体替换） ── */

const transitionDialogVisible = ref(false)
const savingTransitions = ref(false)
const transitionError = ref('')
const transitionDraft = ref([])

function openTransitionDialog() {
  transitionError.value = ''
  transitionDraft.value = transitions.value.map((t) => ({
    from_code: t.from_code,
    to_code: t.to_code,
    description: t.description || ''
  }))
  transitionDialogVisible.value = true
}

function addTransitionRow() {
  transitionDraft.value.push({ from_code: '', to_code: '', description: '' })
}

function removeTransitionRow(index) {
  transitionDraft.value.splice(index, 1)
}

async function submitTransitions() {
  transitionError.value = ''
  const rows = transitionDraft.value.filter((r) => r.from_code && r.to_code)
  const blank = transitionDraft.value.length - rows.length
  if (blank > 0) {
    transitionError.value = `有 ${blank} 行没选起点或终点，它们会被丢掉。补全或删掉该行再保存。`
    return
  }
  if (rows.some((r) => r.from_code === r.to_code)) {
    transitionError.value = '存在起点与终点相同的跃迁（自己到自己），这在状态机里没有意义'
    return
  }
  try {
    await ElMessageBox.confirm(
      `即将用 ${rows.length} 条跃迁整体替换「${currentParentLabel.value}」现有的跃迁集合，列表里被删掉的行会真的消失。此操作不可撤销。`,
      '确认整体替换',
      { confirmButtonText: '替换', cancelButtonText: '取消', type: 'warning' }
    )
  } catch  {
    return
  }
  savingTransitions.value = true
  try {
    await dictionariesApi.replaceTransitions(currentParent.value, rows)
    transitionDialogVisible.value = false
    ElMessage.success('跃迁已整体替换')
    await loadDetail()
  } catch (e) {
    transitionError.value = e.message || '保存失败'
  } finally {
    savingTransitions.value = false
  }
}
</script>

<style scoped>
.dictionary-page { padding: 16px; }
.page-intro { margin-bottom: 16px; }

.card-header { display: flex; justify-content: space-between; align-items: center; }
.header-title { display: flex; align-items: center; gap: 10px; }
.header-count { font-size: 13px; color: #6b7280; }
.header-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.group-tag { font-weight: 600; }

.parent-card { height: 100%; }
.parent-list { display: flex; flex-direction: column; gap: 4px; max-height: calc(100vh - 340px); overflow-y: auto; }
.empty-hint { font-size: 12px; color: #9ca3af; text-align: center; padding: 12px 0; }
.parent-item {
  display: flex; align-items: center; justify-content: space-between;
  padding: 9px 10px; border-radius: 8px; cursor: pointer; transition: background .15s;
}
.parent-item:hover { background: #f3f4f6; }
.parent-item.active { background: #eef2ff; color: #4338ca; }
.parent-item__main { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.parent-name { font-size: 13px; font-weight: 500; display: flex; align-items: center; gap: 6px; }
.parent-code { font-size: 11px; color: #9ca3af; }
.parent-item.active .parent-code { color: #6366f1; }
.parent-count {
  min-width: 22px; text-align: center; padding: 0 6px;
  background: #f3f4f6; border-radius: 10px; font-size: 11px; color: #6b7280; line-height: 18px;
}
.parent-item.active .parent-count { background: #e0e7ff; color: #4338ca; }

.detail-card .card-header { gap: 12px; flex-wrap: wrap; }
.parent-desc {
  font-size: 13px; color: #4b5563; background: #f9fafb;
  border-left: 3px solid #c7d2fe; padding: 8px 10px; border-radius: 4px; margin-bottom: 14px;
}
.empty-block { text-align: center; color: #9ca3af; padding: 40px 0; font-size: 13px; }
.empty-block--sm { padding: 18px 0; }

.section-title { font-size: 13px; font-weight: 600; color: #374151; margin: 0 0 8px; }
.section-title--trans { margin-top: 20px; }

.item-code {
  background: #f3f4f6; padding: 1px 6px; border-radius: 4px;
  font-size: 12px; color: #4338ca;
}
.trans-name { font-size: 12px; color: #6b7280; margin-left: 6px; }
.trans-arrow { color: #9ca3af; }
.locked-tag { margin-left: 4px; }

.form-hint { font-size: 12px; color: #6b7280; margin-top: 4px; line-height: 1.5; }
.form-error {
  display: flex; align-items: center; gap: 6px;
  color: #dc2626; font-size: 13px; background: #fef2f2;
  border: 1px solid #fecaca; padding: 8px 10px; border-radius: 6px; margin-top: 8px;
}
.dialog-actions { margin-top: 10px; }

@media (max-width: 768px) {
  .dictionary-page { padding: 10px; }
}
</style>
