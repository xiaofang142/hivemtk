<template>
  <div class="params-form">
    <!-- 有契约就渲染表单；没契约（未声明 params_schema）就退回原来的 JSON 文本域，
         老资产/老流程的行为不能因为这次改版而变 -->
    <template v-if="fields.length">
      <el-form-item
        v-for="f in fields"
        :key="f.name"
        :label="f.title || f.name"
        :required="f.required"
      >
        <el-select
          v-if="f.enum"
          v-model="model[f.name]"
          clearable
          :placeholder="f.placeholder || `选择 ${f.name}`"
          style="width: 100%"
          @change="emitChange"
        >
          <el-option
            v-for="opt in f.enum"
            :key="String(opt)"
            :label="String(opt)"
            :value="opt"
          />
        </el-select>

        <el-switch
          v-else-if="f.type === 'boolean'"
          v-model="model[f.name]"
          @change="emitChange"
        />

        <el-input-number
          v-else-if="f.type === 'number' || f.type === 'integer'"
          v-model="model[f.name]"
          :step="f.type === 'integer' ? 1 : 0.1"
          :min="f.minimum"
          :max="f.maximum"
          :precision="f.type === 'integer' ? 0 : undefined"
          controls-position="right"
          style="width: 100%"
          @change="emitChange"
        />

        <el-input
          v-else-if="f.type === 'array'"
          v-model="arrayText[f.name]"
          :placeholder="f.arrayHint"
          @change="onArrayText(f)"
        />

        <el-input
          v-else
          v-model="model[f.name]"
          :type="f.multiline ? 'textarea' : 'text'"
          :rows="f.multiline ? 3 : undefined"
          :maxlength="f.maxLength"
          :placeholder="f.placeholder || `填写 ${f.name}`"
          @change="emitChange"
        />
        <div
          v-if="f.description"
          class="field-hint"
        >
          {{ f.description }}
        </div>
      </el-form-item>

      <el-alert
        v-if="missing.length"
        type="warning"
        :closable="false"
        show-icon
        :title="`必填未填：${missing.join('、')}`"
      />
    </template>

    <el-form-item
      v-else
      label="参数 JSON"
    >
      <el-input
        v-model="rawText"
        type="textarea"
        :rows="5"
        placeholder="可选；将作为 params 透传给脚本"
        @change="onRawChange"
      />
      <div class="field-hint">
        该{{ isFlow ? '流程' : '资产' }}未登记参数契约（params_schema），按原样透传 JSON。
      </div>
    </el-form-item>

    <div
      v-if="fields.length"
      class="advanced"
    >
      <el-button
        link
        type="primary"
        @click="advanced = !advanced"
      >
        {{ advanced ? '收起 JSON' : '高级：直接编辑 JSON' }}
      </el-button>
      <el-input
        v-if="advanced"
        v-model="rawText"
        type="textarea"
        :rows="5"
        style="margin-top: 6px"
        @change="onRawChange"
      />
    </div>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'

// ParamsForm —— S-11（U1）：按 params_schema 渲染执行参数表单。
//
// 定位：把「让人手写 JSON」换成「按契约填表」。契约的最终判据仍在服务端
// （egolite.ValidateEgoParams），这里只做即时提示；表单不给安全感，服务端才给。
const props = defineProps({
  modelValue: { type: Object, default: () => ({}) },
  schema: { type: [Object, String], default: null },
  isFlow: { type: Boolean, default: false }
})
const emit = defineEmits(['update:modelValue'])

const model = reactive({})
const arrayText = reactive({})
const rawText = ref('')
const advanced = ref(false)

const schemaObj = computed(() => {
  let s = props.schema
  if (!s) return {}
  if (typeof s === 'string') {
    try {
      s = JSON.parse(s)
    } catch {
      return {}
    }
  }
  return s && typeof s === 'object' ? s : {}
})

const fields = computed(() => {
  const props_ = schemaObj.value.properties
  if (!props_ || typeof props_ !== 'object') return []
  const required = Array.isArray(schemaObj.value.required) ? schemaObj.value.required : []
  return Object.keys(props_)
    .map((name) => {
      const p = props_[name] || {}
      const type = typeof p.type === 'string' ? p.type : 'string'
      const maxLength = typeof p.maxLength === 'number' ? p.maxLength : undefined
      const enumList = Array.isArray(p.enum) ? p.enum : null
      const itemType = p.items?.type
      return {
        name,
        type,
        title: p.title,
        description: p.description,
        placeholder: p.placeholder,
        required: required.includes(name),
        enum: enumList,
        minimum: typeof p.minimum === 'number' ? p.minimum : undefined,
        maximum: typeof p.maximum === 'number' ? p.maximum : undefined,
        maxLength,
        multiline: type === 'string' && (maxLength === undefined || maxLength > 60),
        itemType,
        arrayHint:
          itemType === 'number' || itemType === 'integer'
            ? '多个数字用英文逗号分隔'
            : '多个值用英文逗号分隔'
      }
    })
    .filter(Boolean)
})

const missing = computed(() =>
  fields.value
    .filter((f) => f.required)
    .filter((f) => {
      const v = model[f.name]
      return v === undefined || v === null || String(v).trim() === ''
    })
    .map((f) => f.title || f.name)
)

const syncRaw = () => {
  rawText.value = Object.keys(model).length ? JSON.stringify(model, null, 2) : ''
}

const emitChange = () => {
  syncRaw()
  emit('update:modelValue', { ...model })
}

const onArrayText = (f) => {
  const txt = String(arrayText[f.name] ?? '').trim()
  if (!txt) {
    delete model[f.name]
    emitChange()
    return
  }
  const parts = txt.split(',').map((s) => s.trim()).filter((s) => s !== '')
  const numeric = f.itemType === 'number' || f.itemType === 'integer'
  model[f.name] = numeric ? parts.map((s) => Number(s)).filter((n) => !Number.isNaN(n)) : parts
  emitChange()
}

const onRawChange = () => {
  const txt = String(rawText.value || '').trim()
  if (!txt) {
    Object.keys(model).forEach((k) => delete model[k])
    emit('update:modelValue', {})
    return
  }
  try {
    const parsed = JSON.parse(txt)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      Object.keys(model).forEach((k) => delete model[k])
      Object.assign(model, parsed)
      emit('update:modelValue', { ...model })
    }
  } catch {
    // 非法 JSON 不写回：留原样让人改，服务端会给出明确报错
  }
}

// 外部重置（重新打开运行弹窗）时同步进来；默认值来自 schema.default
watch(
  () => props.modelValue,
  (v) => {
    const next = v && typeof v === 'object' ? v : {}
    Object.keys(model).forEach((k) => delete model[k])
    Object.assign(model, next)
    fields.value.forEach((f) => {
      if (model[f.name] === undefined) {
        const def = schemaObj.value.properties?.[f.name]?.default
        if (def !== undefined) model[f.name] = def
      }
    })
    syncRaw()
  },
  { immediate: true, deep: true }
)

watch(
  () => props.schema,
  () => {
    syncRaw()
  }
)

defineExpose({ missing })
</script>

<style scoped>
.field-hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.5;
  margin-top: 2px;
}
.advanced {
  margin: 0 0 6px;
}
</style>
