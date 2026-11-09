<template>
  <div class="dashboard">
    <div class="dashboard-content">
      <div class="recent-tasks">
        <el-table
          :data="jobsList"
          style="width: 100%"
        >
          <el-table-column
            prop="subject"
            :label="$t('主题')"
            width="200"
          />
          <el-table-column
            prop="email_total"
            :label="$t('计划条数')"
            width="120"
          />
          <el-table-column
            prop="send_total"
            :label="$t('已发送条数')"
            width="120"
          />
          <el-table-column
            prop="success_total"
            :label="$t('成功条数')"
            width="120"
          />
          <el-table-column
            prop="fail_total"
            :label="$t('失败条数')"
            width="120"
          />
          <el-table-column
            prop="read_total"
            :label="$t('已读条数')"
            width="120"
          />
          <el-table-column
            :label="$t('未读条数')"
            width="120"
            flex="1"
          >
            <template #default="scope">
              {{ Math.max(0, (scope.row.success_total || 0) - (scope.row.read_total || 0)) }}
            </template>
          </el-table-column>
          <el-table-column
            prop="created_at"
            label="创建时间"
            width="300"
            flex="1"
          >
            <template #default="{ row }">
              {{ fmtTime(row.created_at) }}
            </template>
          </el-table-column>
        </el-table>
        <div style="width: 100%;text-align: center; display: flex;">
          <el-pagination
            style="margin: 0 auto;"
            :page-size="jobsLimit"
            layout="prev, pager, next"
            :total="jobsTotal"
            @change="handlePageChange"
          />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import i18n from '@/i18n'

import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { emailApi } from '@/api/email'
import { fmtTime } from '@/utils/time'

const jobsList = ref([])
const jobsTotal = ref(0)
const jobsPage = ref(1)
const jobsLimit = ref(10)
const loading = ref(false);

const handlePageChange = (number) => {
  if (jobsPage.value === number)
    return;
  jobsPage.value = number
  fetchJobsList()
}

const fetchJobsList = async () => {
  if (loading.value)
    return;
  loading.value = true
  try {
    const res = await emailApi.getJobsList(jobsPage.value, jobsLimit.value)
    jobsList.value = res.list || [];
    jobsTotal.value = res.total || 0
  } catch (error) {
    console.error('获取任务列表失败:', error) 
    ElMessage.error(i18n.global.t('获取任务列表失败'))
  } finally {
    loading.value = false
  }
}
onMounted(() => {
  fetchJobsList()
});

</script>

<style lang="scss" scoped>
// 变量由 vite additionalData 全局注入(@import)，此处不得再 @use（dart-sass 要求 @use 先于一切规则）

.dashboard {
  .page-header {
    margin-bottom: 20px;

    h1 {
      font-size: 24px;
      color: $text-primary;
      margin: 0;
    }
  }

  .dashboard-content {
    .stat-card {
      height: 140px;
      margin-bottom: 20px;

      .stat-card-content {
        display: flex;
        align-items: center;
        margin-bottom: 15px;

        .stat-icon {
          width: 60px;
          height: 60px;
          border-radius: 50%;
          background-color: rgba($primary-color, 0.1);
          display: flex;
          justify-content: center;
          align-items: center;
          margin-right: 15px;

          .el-icon {
            font-size: 30px;
            color: $primary-color;
          }

          &.platform-icon {
            background-color: rgba($success-color, 0.1);

            .el-icon {
              color: $success-color;
            }
          }

          &.task-icon {
            background-color: rgba($warning-color, 0.1);

            .el-icon {
              color: $warning-color;
            }
          }

          &.content-icon {
            background-color: rgba($info-color, 0.1);

            .el-icon {
              color: $info-color;
            }
          }
        }

        .stat-info {
          .stat-value {
            font-size: 24px;
            font-weight: bold;
            color: $text-primary;
            line-height: 1.2;
          }

          .stat-label {
            font-size: 14px;
            color: $text-secondary;
          }
        }
      }

      .stat-footer {
        border-top: 1px solid $border-lighter;
        padding-top: 10px;

        .stat-detail {
          display: flex;
          justify-content: space-between;
          color: $text-secondary;
          font-size: 13px;

          .el-tag {
            margin-right: 5px;
          }
        }
      }
    }

    .quick-actions {
      margin: 20px 0 30px;

      h2 {
        font-size: 18px;
        margin-bottom: 15px;
        color: $text-primary;
      }

      .action-card {
        height: 160px;
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        cursor: pointer;
        transition: all 0.3s;

        &:hover {
          transform: translateY(-5px);
          box-shadow: 0 10px 20px rgba(0, 0, 0, 0.1);
        }

        .action-icon {
          width: 50px;
          height: 50px;
          border-radius: 50%;
          background-color: rgba($primary-color, 0.1);
          display: flex;
          justify-content: center;
          align-items: center;
          margin-bottom: 15px;

          .el-icon {
            font-size: 24px;
            color: $primary-color;
          }
        }

        .action-title {
          font-size: 16px;
          font-weight: bold;
          color: $text-primary;
          margin-bottom: 5px;
        }

        .action-desc {
          font-size: 13px;
          color: $text-secondary;
          text-align: center;
        }
      }
    }

    .recent-tasks {

      .section-header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        margin-bottom: 15px;

        h2 {
          font-size: 18px;
          color: $text-primary;
          margin: 0;
        }
      }
    }
  }
}
</style>