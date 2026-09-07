<template>
  <div class="segment-builder">
    <el-row :gutter="16">
      <el-col :span="16">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>规则组合（拖拽构建）</span>
              <el-button-group>
                <el-button @click="addNode('and')">+ AND 节点</el-button>
                <el-button @click="addNode('or')">+ OR 节点</el-button>
                <el-button @click="addNode('condition')">+ 条件</el-button>
              </el-button-group>
            </div>
          </template>
          <div class="rule-tree">
            <RuleNode v-for="(node, i) in rules" :key="i" :node="node" :level="0" @update="(n) => rules[i] = n" @remove="removeNode(i)" />
          </div>
          <div class="sql-preview">
            <h4>SQL 预览</h4>
            <pre>{{ sqlPreview }}</pre>
            <el-statistic :value="sqlCount" title="将命中客户数" />
          </div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card>
          <template #header><span>触发器</span></template>
          <el-form :model="trigger" label-width="80px">
            <el-form-item label="触发方式">
              <el-radio-group v-model="trigger.type">
                <el-radio label="manual">手动</el-radio>
                <el-radio label="schedule">定时</el-radio>
                <el-radio label="event">事件触发</el-radio>
              </el-radio-group>
            </el-form-item>
            <el-form-item v-if="trigger.type === 'schedule'" label="Cron">
              <el-input v-model="trigger.cron" placeholder="0 0 9 * * ?" />
            </el-form-item>
            <el-form-item v-if="trigger.type === 'event'" label="事件">
              <el-select v-model="trigger.event">
                <el-option label="新客户进入" value="customer.created" />
                <el-option label="订单完成" value="order.completed" />
                <el-option label="高价值行为" value="behavior.high_value" />
              </el-select>
            </el-form-item>
            <el-form-item label="同步到">
              <el-select v-model="trigger.sink" multiple>
                <el-option label="触达 Pipeline" value="reach" />
                <el-option label="客服队列" value="cs" />
                <el-option label="群发任务" value="bulk" />
              </el-select>
            </el-form-item>
          </el-form>
          <el-button type="primary" style="width: 100%" @click="save">保存分群</el-button>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, reactive, computed, h } from 'vue';
import { ElMessage } from 'element-plus'
import { http } from '@/utils/request'

const rules = ref([
  {
    type: 'and',
    children: [
      { type: 'condition', field: 'last_order_at', op: '>', value: '30d' },
      { type: 'condition', field: 'lifetime_value', op: '>=', value: 1000 }
    ]
  }
])

const trigger = reactive({
  type: 'manual',
  cron: '',
  event: '',
  sink: ['reach']
})

const sqlPreview = computed(() => buildSQL(rules.value))
const sqlCount = ref(0)

function addNode(type) {
  if (type === 'and' || type === 'or') {
    rules.value.push({ type, children: [] })
  } else {
    rules.value.push({ type: 'condition', field: '', op: '=', value: '' })
  }
}

function removeNode(idx) {
  rules.value.splice(idx, 1)
}

function buildSQL(nodes) {
  const parts = nodes.map(compileNode).filter(Boolean);
  return `SELECT id, name FROM customers WHERE ${parts.join(' AND ')}`
}

function compileNode(node) {
  if (node.type === 'and') return `(${node.children?.map(compileNode).join(' AND ') || '1=1'})`
  if (node.type === 'or') return `(${node.children?.map(compileNode).join(' OR ') || '1=1'})`
  if (node.type === 'condition') {
    if (!node.field) return null
    return `${node.field} ${node.op} '${node.value}'`
  }
  return null
}

async function save() {
  // 后端 SegmentSaveRequest.Trigger 是 string；trigger 对象序列化为 JSON 字符串存放
  await http.post('/api/user-segments', {
    name: `分群 ${new Date().toISOString()}`,
    rules: rules.value,
    trigger: JSON.stringify(trigger)
  })
  ElMessage.success('分群已保存')
}
</script>

<style scoped>
.segment-builder { padding: 16px; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.rule-tree { min-height: 240px; }
.sql-preview { margin-top: 16px; padding: 12px; background: #F8FAFC; border-radius: 6px; }
.sql-preview pre { font-size: 12px; color: #334155; white-space: pre-wrap; }
</style>
