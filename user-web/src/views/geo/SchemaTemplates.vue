<template>
  <div>
    <el-card shadow="never">
      <template #header><span>{{ pageLabel }}</span></template>
      <el-empty description="待实现（GEO v2 配置页面骨架）">
        <el-button type="primary">去 GEO 后端配置</el-button>
      </el-empty>
    </el-card>
  </div>
</template>

<script setup>
const pageLabel = 'SchemaTemplates'
</script>
