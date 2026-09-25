<template>
  <div class="guide-container">
    <el-card>
      <template #header>
        <span>{{ $t('系统使用指南') }}</span>
      </template>

      <el-steps :active="activeStep" finish-status="success" align-center>
        <el-step title="初始化系统" description="完成账户注册与商户初始化" />
        <el-step title="配置存储" description="配置 OBS/MinIO 等对象存储" />
        <el-step title="添加素材" description="上传产品图片、视频等营销素材" />
        <el-step title="创建卡片" description="在抖音/快手/小红书等平台创建营销卡片" />
        <el-step title="开始营销" description="启动自动回复/RAG 智能体/邮件营销" />
      </el-steps>

      <el-divider />

      <el-row :gutter="20">
        <el-col :span="12" v-for="step in steps" :key="step.title">
          <el-card shadow="hover" class="step-card">
            <h3>{{ step.title }}</h3>
            <p>{{ step.description }}</p>
            <ul>
              <li v-for="(item, i) in step.details" :key="i">{{ item }}</li>
            </ul>
            <el-button type="primary" link @click="goTo(step.path)">
              前往操作 <el-icon><ArrowRight /></el-icon>
            </el-button>
          </el-card>
        </el-col>
      </el-row>

      <el-divider />

      <el-alert
        title="提示"
        type="info"
        :closable="false"
        show-icon
      >
        <p>完成上述 5 步后,即可开始正式营销活动。如需技术支持,请联系客服:support@example.com</p>
      </el-alert>
    </el-card>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref } from 'vue'
import { useRouter } from 'vue-router'

const router = useRouter()
const activeStep = ref(0)

const steps = [
  {
    title: i18n.global.t('1. 初始化系统'),
    description: '创建超管账号并完成系统初始化',
    details: [
      '访问 /setup 创建超级管理员账号',
      '初始化完成后系统在本地写入 install.lock',
      '使用创建的账号登录系统'
    ],
    path: '/setup'
  },
  {
    title: i18n.global.t('2. 配置存储'),
    description: '配置对象存储用于素材管理',
    details: [
      '进入系统配置 → 对象存储',
      '填写 OBS/MinIO 连接信息',
      '点击"测试连接"验证配置'
    ],
    path: '/system/obs-config'
  },
  {
    title: i18n.global.t('3. 添加素材'),
    description: '上传营销素材',
    details: [
      '进入素材库',
      '上传产品图片/视频/文案',
      '为素材添加标签便于检索'
    ],
    path: '/system/material-library'
  },
  {
    title: i18n.global.t('4. 创建卡片'),
    description: '在各平台创建营销卡片',
    details: [
      '进入抖音/快手/小红书卡片管理',
      '选择模板并填充产品信息',
      '生成短链并发布'
    ],
    path: '/douyinCard'
  },
  {
    title: i18n.global.t('5. 开始营销'),
    description: '启动自动化营销',
    details: [
      '配置 RAG 智能体(基于知识库)',
      '启动自动回复(抖音/快手/小红书/闲鱼)',
      '配置邮件/短信营销活动'
    ],
    path: '/system/rag-product-config'
  }
]

const goTo = (path) => {
  router.push(path).catch(() => {})
}
</script>

<style scoped>
.guide-container {
  padding: 20px;
}
.step-card {
  margin-bottom: 20px;
  min-height: 200px;
}
.step-card h3 {
  margin: 0 0 10px;
  color: #303133;
}
.step-card p {
  color: #606266;
  margin-bottom: 12px;
}
.step-card ul {
  padding-left: 20px;
  color: #606266;
  margin-bottom: 12px;
}
.step-card li {
  margin-bottom: 6px;
}
</style>
