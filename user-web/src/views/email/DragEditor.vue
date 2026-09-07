<template>
  <div class="email-editor">
    <el-row :gutter="16">
      <el-col :span="16">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>拖拽式邮件编辑器（USR-RC-02）MJML 编译</span>
              <div>
                <el-button @click="previewDesktop">💻 桌面预览</el-button>
                <el-button @click="previewMobile">📱 移动预览</el-button>
                <el-button type="primary" @click="save">保存</el-button>
              </div>
            </div>
          </template>
          <div class="block-palette">
            <div
              v-for="block in blocks"
              :key="block.type"
              class="block-item"
              draggable="true"
              @dragstart="onDragStart($event, block)"
            >
              <el-icon><component :is="block.icon" /></el-icon>
              <span>{{ block.label }}</span>
            </div>
          </div>
          <div class="canvas" @drop="onDrop" @dragover.prevent>
            <div
              v-for="(block, i) in blocks_state"
              :key="block.id"
              class="canvas-block"
            >
              <component :is="resolveBlock(block.type)" v-bind="block" :editable="true" />
              <el-button-group size="small" class="block-actions">
                <el-button @click="moveUp(i)" :disabled="i === 0">↑</el-button>
                <el-button @click="moveDown(i)" :disabled="i === blocks_state.length - 1">↓</el-button>
                <el-button type="danger" @click="removeBlock(i)">删</el-button>
              </el-button-group>
            </div>
            <div v-if="blocks_state.length === 0" class="empty">从左侧拖入内容块</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="8">
        <el-card>
          <template #header><span>变量 / 主题</span></template>
          <el-form label-width="80px">
            <el-form-item label="主题">
              <el-input v-model="subject" />
            </el-form-item>
            <el-form-item label="发件人">
              <el-input v-model="fromName" />
            </el-form-item>
            <el-form-item label="预发列表">
              <el-tag v-for="(t, i) in testEmails" :key="i" closable @close="testEmails.splice(i, 1)">{{ t }}</el-tag>
              <el-input v-model="newTestEmail" size="small" @keyup.enter="addTestEmail" placeholder="test@x.com" style="width: 200px" />
            </el-form-item>
            <el-form-item>
              <el-button @click="sendTest" :loading="sending">发送测试</el-button>
            </el-form-item>
          </el-form>
          <el-divider />
          <h4>变量列表</h4>
          <el-tag v-for="v in builtinVars" :key="v.key" size="small" style="margin: 2px">
            {{ v.key }}
          </el-tag>
        </el-card>
      </el-col>
    </el-row>

    <el-dialog v-model="previewVisible" title="预览" width="640px">
      <div class="preview-frame" v-html="compiledHTML" />
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Document, Picture, EditPen, PriceTag, Share, ChatLineRound, Loading
} from '@element-plus/icons-vue'
import mjml2html from 'mjml-browser'
import { http } from '@/utils/request'

const BLOCK_TYPES = [
  { type: 'text', label: '文本', icon: 'EditPen', default: { content: '在此输入文本...' } },
  { type: 'image', label: '图片', icon: 'Picture', default: { src: '', alt: '' } },
  { type: 'button', label: '按钮', icon: 'PriceTag', default: { text: '立即查看', url: '#', color: '#4F46E5' } },
  { type: 'divider', label: '分割线', icon: 'Minus' },
  { type: 'social', label: '社交', icon: 'Share', default: { networks: ['wechat', 'weibo'] } },
  { type: 'coupon', label: '优惠券', icon: 'PriceTag', default: { code: 'SAVE10', amount: 10, expires: '30d' } }
]

const blocks = BLOCK_TYPES.map((b) => ({ ...b, icon: resolveIcon(b.icon) }))

const blocks_state = ref([])
const subject = ref('')
const fromName = ref('营销助手')
const testEmails = ref([])
const newTestEmail = ref('')
const sending = ref(false)
const previewVisible = ref(false)
const compiledHTML = ref('')
const saving = ref(false)

const builtinVars = [
  { key: '{{customer.name}}' },
  { key: '{{order.id}}' },
  { key: '{{order.amount}}' },
  { key: '{{product.title}}' },
  { key: '{{agent.name}}' },
  { key: '{{coupon.code}}' }
]

function resolveIcon(name) {
  return { EditPen, Picture, PriceTag, Share, Document, ChatLineRound, Loading }[name] || Document
}

let _blockId = 1
function onDragStart(e, block) {
  e.dataTransfer.setData('blockType', block.type)
  e.dataTransfer.effectAllowed = 'copy'
}
function onDrop(e) {
  const type = e.dataTransfer.getData('blockType')
  if (!type) return
  const tpl = BLOCK_TYPES.find((b) => b.type === type)
  blocks_state.value.push({
    id: _blockId++,
    type,
    ...(tpl?.default || {})
  })
}

function resolveBlock(type) {
  return {
    text: 'TextBlock',
    image: 'ImageBlock',
    button: 'ButtonBlock',
    divider: 'DividerBlock',
    social: 'SocialBlock',
    coupon: 'CouponBlock'
  }[type] || 'TextBlock'
}

function moveUp(i) { if (i > 0) blocks_state.value.splice(i - 1, 0, blocks_state.value.splice(i, 1)[0]) }
function moveDown(i) { if (i < blocks_state.value.length - 1) blocks_state.value.splice(i + 1, 0, blocks_state.value.splice(i, 1)[0]) }
function removeBlock(i) { blocks_state.value.splice(i, 1) }

function addTestEmail() {
  if (newTestEmail.value && /@/.test(newTestEmail.value)) {
    testEmails.value.push(newTestEmail.value)
    newTestEmail.value = ''
  }
}

async function sendTest() {
  if (testEmails.value.length === 0) return ElMessage.warning('请添加测试邮箱')
  sending.value = true
  try {
    await http.post('/api/email/test-send', {
      subject: subject.value,
      html: compiledHTML.value,
      to: testEmails.value
    })
    ElMessage.success('测试邮件已发送')
  } finally {
    sending.value = false
  }
}

async function save() {
  compiledHTML.value = compileToHTML()
  saving.value = true
  try {
    await http.post('/api/email/drafts', {
      subject: subject.value || '未命名拖拽模板',
      content: compiledHTML.value
    })
    ElMessage.success('模板已保存到草稿箱')
  } catch (e) {
    ElMessage.error('保存失败：' + (e?.message || e))
  } finally {
    saving.value = false
  }
}

function previewDesktop() {
  compiledHTML.value = compileToHTML()
  previewVisible.value = true
}
function previewMobile() {
  compiledHTML.value = '<style>body{max-width:375px;margin:0 auto;}</style>' + compileToHTML()
  previewVisible.value = true
}

// 邮件模板可能来自资产市场导入(他人设计),所有插值必须转义,
// URL 属性走协议白名单,否则预览/发送构成存储型 XSS 与属性注入
const esc = (s) => String(s ?? '')
  .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
const escUrl = (u) => {
  const s = esc(u)
  return /^(https?:|mailto:|tel:|#|\/)/i.test(s.trim()) ? s : '#'
}

function compileToHTML() {
  const parts = blocks_state.value.map((b) => {
    if (b.type === 'text') return `<mj-text>${esc(b.content)}</mj-text>`
    if (b.type === 'image') return `<mj-image src="${escUrl(b.src)}" alt="${esc(b.alt)}" />`
    if (b.type === 'button') return `<mj-button href="${escUrl(b.url)}" background-color="${esc(b.color || '#4F46E5')}">${esc(b.text || 'Button')}</mj-button>`
    if (b.type === 'divider') return '<mj-divider />'
    if (b.type === 'social') return `<mj-social><mj-social-element name="${esc(b.networks?.[0] || 'wechat')}" /></mj-social>`
    if (b.type === 'coupon') return `<mj-text>优惠券：${esc(b.code)}（减 ${esc(b.amount)}，${esc(b.expires)} 内有效）</mj-text>`
    return ''
  });
  const mjml = `<mjml><mj-body>${parts.join('\n')}</mj-body></mjml>`
  try {
    return mjml2html(mjml).html
  } catch (e) {
    return `<pre>${esc(e.message)}</pre>`
  }
}
</script>

<style scoped>
.email-editor { padding: 16px; }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.block-palette {
  display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 16px;
  padding: 12px; background: #F8FAFC; border-radius: 6px;
}
.block-item {
  display: flex; align-items: center; gap: 4px;
  padding: 6px 12px; background: #fff;
  border: 1px solid #E2E8F0; border-radius: 4px;
  cursor: grab; font-size: 12px;
}
.canvas {
  min-height: 400px; padding: 16px;
  background: #fff; border: 2px dashed #E2E8F0; border-radius: 6px;
}
.canvas-block { position: relative; margin: 8px 0; padding: 12px; border: 1px solid #E2E8F0; border-radius: 4px; }
.canvas-block:hover { border-color: #6366F1; }
.block-actions { position: absolute; right: 8px; top: 8px; }
.empty { text-align: center; color: #94A3B8; padding: 60px 0; }
.preview-frame { border: 1px solid #E2E8F0; padding: 16px; max-height: 600px; overflow: auto; }
</style>
