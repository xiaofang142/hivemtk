<template>
  <div class="rule-node" :style="{ marginLeft: level ? '16px' : '0' }">
    <!-- and / or 分组节点：可继续嵌套子节点 -->
    <div v-if="node.type === 'and' || node.type === 'or'" class="rule-group">
      <el-tag :type="node.type === 'and' ? 'primary' : 'warning'" size="small">
        {{ node.type.toUpperCase() }}
      </el-tag>
      <el-button size="small" @click="addChild('and')">+ AND</el-button>
      <el-button size="small" @click="addChild('or')">+ OR</el-button>
      <el-button size="small" @click="addChild('condition')">+ 条件</el-button>
      <el-button size="small" type="danger" @click="$emit('remove')">删除</el-button>
    </div>

    <!-- 叶子条件节点：field op value 三元组，缺 field 时 compileNode 会返回 null -->
    <div v-else class="rule-condition">
      <el-input
        :model-value="node.field"
        placeholder="字段，如 lifetime_value"
        style="width: 200px"
        size="small"
        @update:model-value="patch('field', $event)"
      />
      <el-select :model-value="node.op" style="width: 90px" size="small" @update:model-value="patch('op', $event)">
        <el-option v-for="o in OPS" :key="o" :label="o" :value="o" />
      </el-select>
      <el-input
        :model-value="node.value"
        placeholder="值，如 1000"
        style="width: 180px"
        size="small"
        @update:model-value="patch('value', $event)"
      />
      <el-button size="small" type="danger" @click="$emit('remove')">删除</el-button>
    </div>

    <div v-if="node.type === 'and' || node.type === 'or'" class="rule-children">
      <!-- 递归渲染子节点：node 数据结构由 Builder.vue 的 addNode/compileNode 定义 -->
      <RuleNode
        v-for="(child, i) in children"
        :key="i"
        :node="child"
        :level="level + 1"
        @update="patchChild(i, $event)"
        @remove="removeChild(i)"
      />
      <el-empty v-if="!children.length" description="暂无子条件" :image-size="40" />
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({
  node: { type: Object, required: true },
  level: { type: Number, default: 0 }
})
const emit = defineEmits(['update', 'remove'])

// 操作符集合与 Builder.vue compileNode 产出的 SQL 语义对齐（field op 'value'）
const OPS = ['=', '!=', '>', '>=', '<', '<=', 'like', 'in']

const children = computed(() => (Array.isArray(props.node.children) ? props.node.children : []))

// 浅拷贝后回传：不直接改 prop 对象，符合单向数据流
const patch = (key, value) => emit('update', { ...props.node, [key]: value })
const addChild = (type) => {
  const child = type === 'condition' ? { type: 'condition', field: '', op: '=', value: '' } : { type, children: [] }
  emit('update', { ...props.node, children: [...children.value, child] })
}
const patchChild = (idx, next) => {
  const list = [...children.value]
  list[idx] = next
  emit('update', { ...props.node, children: list })
}
const removeChild = (idx) => {
  const list = [...children.value]
  list.splice(idx, 1)
  emit('update', { ...props.node, children: list })
}
</script>

<style scoped>
.rule-group,
.rule-condition {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 0;
}

.rule-children {
  border-left: 1px dashed var(--el-border-color);
  padding-left: 8px;
}
</style>
