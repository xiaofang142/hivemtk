<template>
  <div class="app-container">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>RBAC 权限矩阵</span>
        </div>
      </template>

      <el-alert
        type="info"
        :closable="false"
        show-icon
        title="只读矩阵：单一事实源是 docs/registry/registry.yaml（生成器 scripts/gen-registry.py）。改权限先改源，运行时由 RequireRBACPermission 强制执法——本页只做可视化对账。"
        style="margin-bottom: 12px"
      />

      <template v-if="data">
        <el-descriptions :column="4" border size="small" style="margin-bottom: 16px" title="角色">
          <el-descriptions-item v-for="r in data.roles" :key="r.id" :label="r.id">
            <el-tag size="small" :type="r.id === 'super_admin' ? 'danger' : r.id === 'team_admin' ? 'warning' : 'info'">
              {{ r.id }}
            </el-tag>
          </el-descriptions-item>
        </el-descriptions>

        <el-table :data="data.permissions" border size="small">
          <el-table-column prop="id" label="权限点（资源.动作）" min-width="200">
            <template #default="{ row }">
              <code class="mono">{{ row.id }}</code>
            </template>
          </el-table-column>
          <el-table-column
            v-for="r in data.roles"
            :key="r.id"
            :label="r.id"
            :width="roleColWidth"
            align="center"
          >
            <template #default="{ row }">
              <span v-if="row.roles.includes(r.id)">✅</span>
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
        </el-table>

        <el-descriptions :column="4" border size="small" style="margin-top: 16px" title="API Key 状态机">
          <el-descriptions-item v-for="st in data.api_key_states" :key="st.id" :label="st.id">
            <code class="mono">{{ st.id }}</code>
          </el-descriptions-item>
        </el-descriptions>
      </template>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { rbacApi } from '@/api/teamAdmin'

const data = ref(null)

const roleColWidth = computed(() => (data.value ? Math.floor(600 / (data.value.roles.length || 1)) : 120))

const load = async () => {
  const res = await rbacApi.matrix()
  data.value = res?.data ?? res ?? null
}

onMounted(load)
</script>

<style scoped>
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.mono {
  font-family: monospace;
  font-size: 12px;
}
.muted {
  color: #ccc;
}
</style>
