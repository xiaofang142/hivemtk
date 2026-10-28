<template>
  <div class="script-ab-page">
    <el-card class="header-card">
      <div>
        <h2>{{ $t('话术 AB 测试') }}</h2>
        <p class="subtitle">
          {{ $t('版本管理、曝光转化统计与分流配置') }}
        </p>
      </div>
      <el-select
        v-model="scriptId"
        filterable
        :placeholder="$t('选择话术')"
        style="width: 260px"
        @change="loadAll"
      >
        <el-option
          v-for="s in scripts"
          :key="s.id"
          :label="s.name || s.title"
          :value="s.id"
        />
      </el-select>
    </el-card>

    <template v-if="scriptId">
      <el-row :gutter="16">
        <el-col :span="14">
          <el-card>
            <template #header>
              <span>{{ $t('版本历史') }}</span>
            </template>
            <el-table
              v-loading="loadingVersions"
              :data="versions"
              stripe
              size="small"
            >
              <el-table-column
                prop="version"
                label="版本"
                width="70"
              />
              <el-table-column
                prop="note"
                label="说明"
                min-width="160"
                show-overflow-tooltip
              />
              <el-table-column
                label="激活"
                width="80"
              >
                <template #default="{ row }">
                  <el-tag
                    v-if="row.active"
                    type="success"
                    size="small"
                  >
                    {{ $t('当前') }}
                  </el-tag>
                  <el-button
                    v-else
                    link
                    type="primary"
                    size="small"
                    @click="activate(row)"
                  >
                    {{ $t('激活') }}
                  </el-button>
                </template>
              </el-table-column>
              <el-table-column
                prop="created_at"
                label="创建时间"
                width="160"
              />
            </el-table>
          </el-card>
        </el-col>
        <el-col :span="10">
          <el-card>
            <template #header>
              <span>{{ $t('AB 配置') }}</span>
            </template>
            <el-form label-width="110px">
              <el-form-item :label="$t('启用 AB')">
                <el-switch v-model="abForm.enabled" />
              </el-form-item>
              <el-form-item :label="$t('A 组流量 %')">
                <el-input-number
                  v-model="abForm.split_a"
                  :min="0"
                  :max="100"
                />
              </el-form-item>
              <el-form-item :label="$t('归因窗口(小时)')">
                <el-input-number
                  v-model="abForm.attribution_h"
                  :min="1"
                  :max="720"
                />
              </el-form-item>
              <el-form-item>
                <el-button
                  type="primary"
                  @click="saveConfig"
                >
                  {{ $t('保存配置') }}
                </el-button>
              </el-form-item>
            </el-form>
          </el-card>
        </el-col>
      </el-row>

      <el-card style="margin-top: 16px">
        <template #header>
          <span>{{ $t('曝光-转化统计（按版本×分桶）') }}</span>
        </template>
        <el-table
          v-loading="loadingStats"
          :data="statsVersions"
          stripe
        >
          <el-table-column
            prop="version"
            label="版本"
            width="90"
          />
          <el-table-column
            prop="exposures"
            label="曝光数"
            width="120"
          />
          <el-table-column
            prop="conversions"
            label="转化数"
            width="120"
          />
          <el-table-column
            :label="$t('转化率')"
            min-width="200"
          >
            <template #default="{ row }">
              <el-progress
                :percentage="Math.round((row.conversion_rate || 0) * 100)"
                :stroke-width="14"
                :text-inside="true"
              />
            </template>
          </el-table-column>
        </el-table>
        <el-empty
          v-if="!loadingStats && !statsVersions.length"
          :description="$t('暂无曝光数据，等待话术被使用后自动记录')"
        />
      </el-card>
    </template>
    <el-empty
      v-else
      :description="$t('请先选择一个话术')"
    />
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue';
import { ElMessage } from 'element-plus';
import { getScriptTemplateList } from '@/api/scriptTemplate';
import { scriptAbApi } from '@/api/scriptAb';

const scripts = ref([]);
const scriptId = ref(null);
const versions = ref([]);
const stats = ref({});
const abForm = ref({ enabled: true, split_a: 50, attribution_h: 48 });
const loadingVersions = ref(false);
const loadingStats = ref(false);

const statsVersions = computed(() => stats.value.versions || []);

const loadScripts = async () => {
  try {
    const res = await getScriptTemplateList({ page: 1, pageSize: 100 });
    const list = res?.list || res?.data?.list || res || [];
    scripts.value = list.map((s) => ({ id: s.id, name: s.name || s.title }));
  } catch (e) {
    scripts.value = [];
  }
};

const loadVersions = async () => {
  loadingVersions.value = true;
  try {
    const res = await scriptAbApi.listVersions(scriptId.value);
    versions.value = res?.list || res?.data?.list || [];
  } catch (e) {
    versions.value = [];
  } finally {
    loadingVersions.value = false;
  }
};

const loadStats = async () => {
  loadingStats.value = true;
  try {
    const res = await scriptAbApi.getAbStats(scriptId.value);
    stats.value = res || {};
    if (stats.value.config) {
      abForm.value = {
        enabled: !!stats.value.config.enabled,
        split_a: stats.value.config.split_a ?? 50,
        attribution_h: stats.value.config.attribution_h ?? 48,
      };
    }
  } catch (e) {
    stats.value = {};
  } finally {
    loadingStats.value = false;
  }
};

const loadAll = () => {
  if (!scriptId.value) return;
  loadVersions();
  loadStats();
};

const activate = async (row) => {
  try {
    await scriptAbApi.activateVersion(scriptId.value, row.version);
    ElMessage.success('版本已激活');
    loadVersions();
  } catch (e) {
    ElMessage.error('激活失败');
  }
};

const saveConfig = async () => {
  try {
    await scriptAbApi.updateAbConfig(scriptId.value, abForm.value);
    ElMessage.success('配置已保存');
    loadStats();
  } catch (e) {
    ElMessage.error('保存失败');
  }
};

onMounted(loadScripts);
</script>

<style scoped>
.script-ab-page { padding: 0; }
.header-card { margin-bottom: 16px; display: flex; }
.header-card > div { flex: 1; }
.subtitle { color: #909399; font-size: 13px; margin-top: 4px; }
</style>
