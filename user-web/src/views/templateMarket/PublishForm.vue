<template>
  <el-form
    ref="formRef"
    :model="form"
    :rules="rules"
    label-width="110px"
  >
    <el-form-item
      :label="$t('templateMarketPage.fieldKind')"
      prop="kind"
    >
      <el-radio-group v-model="form.kind">
        <el-radio-button
          v-for="(label, k) in ENTRY_KIND_LABEL"
          :key="k"
          :value="k"
        >
          {{ label }}
        </el-radio-button>
      </el-radio-group>
    </el-form-item>
    <el-form-item
      :label="$t('templateMarketPage.fieldName')"
      prop="name"
    >
      <el-input
        v-model="form.name"
        maxlength="128"
        show-word-limit
      />
    </el-form-item>
    <el-form-item
      :label="$t('templateMarketPage.fieldCategory')"
      prop="category"
    >
      <el-select
        v-model="form.category"
        filterable
        allow-create
        default-first-option
        style="width: 100%"
      >
        <el-option
          v-for="c in categories"
          :key="c"
          :label="c"
          :value="c"
        />
      </el-select>
    </el-form-item>
    <el-form-item :label="$t('templateMarketPage.fieldSummary')">
      <el-input
        v-model="form.summary"
        type="textarea"
        :rows="2"
        maxlength="500"
      />
    </el-form-item>
    <el-form-item
      :label="$t('templateMarketPage.fieldSource')"
      prop="source"
    >
      <el-input
        v-model="form.source"
        :placeholder="$t('templateMarketPage.sourceHint')"
      />
    </el-form-item>
    <el-form-item
      :label="$t('templateMarketPage.fieldAvailability')"
      prop="availability"
    >
      <el-radio-group v-model="form.availability">
        <el-radio-button
          v-for="(label, k) in AVAILABILITY_LABEL"
          :key="k"
          :value="k"
        >
          {{ label }}
        </el-radio-button>
      </el-radio-group>
    </el-form-item>
    <el-form-item :label="$t('templateMarketPage.fieldTags')">
      <el-input
        v-model="tagsText"
        :placeholder="$t('templateMarketPage.tagsHint')"
      />
    </el-form-item>
    <el-form-item :label="$t('templateMarketPage.fieldPrice')">
      <el-input-number
        v-model="priceYuan"
        :min="0"
        :precision="2"
        :step="1"
        style="width: 180px"
      />
    </el-form-item>
  </el-form>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import {
  submitMarketEntry, marketCategories,
  ENTRY_KIND_LABEL, AVAILABILITY_LABEL
} from '@/api/templateMarket'

// 发布入口表单（S-22）：提审落审核台（pending），审核通过自动上公开橱窗。
// 本组件只做表单与提交，弹窗壳由使用方（Market.vue）控制；本组件只在
// 发布弹窗打开时实例化（v-if 门），公开橱窗的匿名访客不会触达这里。
const { t } = useI18n()

const emit = defineEmits(['submitted'])

const formRef = ref()
const categories = ref([])
const tagsText = ref('')
const priceYuan = ref(0)

const form = ref({
  kind: 'template',
  name: '',
  category: '',
  summary: '',
  source: '',
  availability: 'builtin'
})

const rules = {
  name: [{ required: true, message: () => t('templateMarketPage.nameRequired'), trigger: 'blur' }],
  category: [{ required: true, message: () => t('templateMarketPage.categoryRequired'), trigger: 'change' }],
  source: [{ required: true, message: () => t('templateMarketPage.sourceRequired'), trigger: 'blur' }]
}

const payload = computed(() => ({
  kind: form.value.kind,
  name: form.value.name,
  category: form.value.category,
  summary: form.value.summary,
  source: form.value.source,
  availability: form.value.availability,
  tags: tagsText.value.split(/[,，]/).map((x) => x.trim()).filter(Boolean),
  price_cents: Math.round((priceYuan.value || 0) * 100)
}))

async function submit () {
  await formRef.value.validate()
  await submitMarketEntry(payload.value)
  ElMessage.success(t('templateMarketPage.submitOk'))
  emit('submitted')
}

onMounted(async () => {
  // 分类候选与橱窗同源：拉不到就空着（allow-create 兜底，手填也能过）。
  try {
    categories.value = await marketCategories({ _silent: true }) || []
  } catch {
    categories.value = []
  }
})

defineExpose({ submit })
</script>
