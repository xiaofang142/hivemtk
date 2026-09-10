// 客户会话 · 操作与辅助逻辑:坐席状态切换 / 接管与释放 / 结束 / 拉黑 / 黑名单 / SOP / 快捷话术
// 由 views/customerSession/List.vue 原样迁出(零行为变更拆分)
// 依赖通过参数注入,保持与原实现相同的语义与调用顺序
import { ref, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { http } from '@/utils/request'
import i18n from '@/i18n'
import {
  createSession,
  closeSession as closeSess,
  switchSessionHandler,
  blacklistSession,
  unblacklistUser,
  listBlacklist
} from '@/api/customerSession.js'
import { goOnline, goOffline, updateAgentStatus } from '@/api/customerService.js'

export function useSessionActions({ currentSession, currentHandler, inputMsg, myAgentId }) {
  // —— 坐席状态 ——
  const myStatus = ref('offline');

  // —— SOP 阶段 ——
  const sopStage = ref('')

  // —— 黑名单 ——
  const blacklistDialogVisible = ref(false);
  const blacklistItems = ref([])
  const blacklistLoading = ref(false)
  const blacklistedSessionIds = ref([])

  // —— 接管/释放 loading(与 useSessionList 中的 handlerSwitching 同源,由容器传入) ——
  const handlerSwitching = currentHandler ? ref(false) : ref(false)

  // —— AI 摘要生成 loading(独立于 handlerSwitching,仅控制摘要按钮状态) ——
  const summaryLoading = ref(false)

  const currentHandlerComputed = currentHandler ||
    computed(() => (currentSession.value?.handlerType === 'human' ? 'human' : 'ai'))

  // 由容器注入:结束后/拉黑后刷新会话列表(语义与原 loadSessions 一致)
  let reloadSessions = () => {}
  const setReload = (fn) => { if (fn) reloadSessions = fn }

  const aiSummaryCurrent = async () => {
    if (!currentSession.value) return ElMessage.warning('请先选择会话')
    const sid = currentSession.value.sessionId || currentSession.value.session_id
    try {
      summaryLoading.value = true
      const res = await http.post(`/api/customer-sessions/${sid}/ai-summary`)
      const d = res?.data || {}
      ElMessageBox.alert(d.summary || '摘要生成完成', 'AI 会话摘要（' + (d.sentiment || 'neutral') + '）', { confirmButtonText: '知道了' })
    } catch (e) {
      ElMessage.error(e?.message || '摘要生成失败')
    } finally { summaryLoading.value = false }
  };

  const exportTranscriptCurrent = () => {
    if (!currentSession.value) return ElMessage.warning('请先选择会话')
    const sid = currentSession.value.sessionId || currentSession.value.session_id
    window.open(`/api/customer-sessions/${sid}/transcript?format=csv`, '_blank')
  }

  const snoozeCurrent = async () => {
    if (!currentSession.value) return ElMessage.warning('请先选择会话')
    const sid = currentSession.value.sessionId || currentSession.value.session_id
    try {
      await http.post(`/api/customer-sessions/${sid}/snooze`, { hours: 2 })
      ElMessage.success('会话已暂缓 2 小时')
    } catch (e) { ElMessage.error(e?.message || '暂缓失败') }
  }

  const setPriorityCurrent = async () => {
    if (!currentSession.value) return ElMessage.warning('请先选择会话')
    const sid = currentSession.value.sessionId || currentSession.value.session_id
    try {
      const { value } = await ElMessageBox.prompt('输入优先级 0 普通 / 1 低 / 2 高 / 3 紧急', '设置优先级', { inputValue: String(currentSession.value.priority ?? 0) })
      await http.put(`/api/customer-sessions/${sid}/priority`, { level: Number(value) })
      currentSession.value.priority = Number(value)
      ElMessage.success('优先级已更新')
    } catch (e) {
      if (e !== 'cancel' && e !== 'close') ElMessage.error(e?.message || '设置失败')
    }
  }

  const showCreateSession = async () => {
    try {
      const { value } = await ElMessageBox.prompt('请输入客户ID', '新建会话')
      if (value) {
        await createSession({
          platform: 'web',
          account_id: 'default',
          user_id: value,
          user_name: value
        })
        ElMessage.success(i18n.global.t('会话已创建'))
        reloadSessions()
      }
    } catch (e) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
  }

  const closeSession = async () => {
    try {
      await ElMessageBox.confirm('确定结束该会话？', '确认', { type: 'warning' })
      await closeSess(currentSession.value.id)
      ElMessage.success(i18n.global.t('会话已结束'))
      currentSession.value = null
      reloadSessions()
    } catch (e) {
      if (e !== 'cancel') ElMessage.error(i18n.global.t('操作失败'))
    }
  }

  const handleTakeover = async () => {
    if (!currentSession.value) return
    if (currentHandlerComputed.value === 'human') {
      ElMessage.warning('该会话已由人工接管')
      return
    }
    try {
      handlerSwitching.value = true
      await switchSessionHandler(currentSession.value.id, 'human', '坐席主动接管')
      currentSession.value.handlerType = 'human';
      if (currentSession.value.status !== 'human_handling') {
        currentSession.value.status = 'human_handling'
      }
      ElMessage.success('已接管会话，现在可以与客户对话')
    } catch (e) {
      ElMessage.error('接管失败：' + (e?.message || ''))
    } finally {
      handlerSwitching.value = false
    }
  };

  const handleRelease = async () => {
    if (!currentSession.value) return
    if (currentHandlerComputed.value === 'ai') {
      ElMessage.warning('该会话已由 AI 托管')
      return
    }
    try {
      await ElMessageBox.confirm('释放后会话将交回 AI 托管，确定吗？', '确认释放', { type: 'warning' })
      handlerSwitching.value = true
      await switchSessionHandler(currentSession.value.id, 'ai', '')
      currentSession.value.handlerType = 'ai'
      currentSession.value.status = 'waiting'
      ElMessage.success('已释放回 AI 托管')
    } catch (e) {
      if (e !== 'cancel') ElMessage.error('释放失败：' + (e?.message || ''))
    } finally {
      handlerSwitching.value = false
    }
  };

  const saveSopStage = async () => {
    if (!currentSession.value || !sopStage.value) return
    ElMessage.success(`已标记 SOP 阶段：${sopStage.value}`);
  };

  const sendCoupon = () => {
    if (!currentSession.value) return
    inputMsg.value = '【优惠券】新人专享 8 折，输入 COUPON8 立减';
    ElMessage.success('已准备优惠券话术，可直接发送')
  };
  const sendProductCard = () => {
    if (!currentSession.value) return
    inputMsg.value = '【商品卡片】热卖推荐：XXX 蓝莓味爆款 ¥99'
    ElMessage.success('已准备商品卡话术，可直接发送')
  }
  const blacklist = async () => {
    if (!currentSession.value) return
    let reason;
    try {
      const promptRes = await ElMessageBox.prompt('请输入拉黑原因（选填）', '拉黑访客', {
        type: 'warning',
        confirmButtonText: '确认拉黑',
        cancelButtonText: '取消',
        inputPlaceholder: '例如：恶意刷屏 / 辱骂客服 / 欺诈风险'
      });
      reason = promptRes?.value || ''
    } catch (e) {
      return;
    }
    try {
      await blacklistSession(currentSession.value.id, reason, 0);
      if (!blacklistedSessionIds.value.includes(currentSession.value.id)) {
        blacklistedSessionIds.value = [...blacklistedSessionIds.value, currentSession.value.id]
      }
      currentSession.value.status = 'closed'
      ElMessage.success('已加入黑名单，该会话已关闭')
      reloadSessions()
    } catch (e) {
      ElMessage.error('拉黑失败：' + (e?.message || ''))
    }
  }

  const openBlacklistDialog = async () => {
    blacklistDialogVisible.value = true
    await loadBlacklist()
  };

  const loadBlacklist = async () => {
    blacklistLoading.value = true
    try {
      const res = await listBlacklist({ page: 1, page_size: 50 })
      const list = Array.isArray(res) ? res : (res?.list || res?.data || []);
      blacklistItems.value = list
    } catch (e) {
      blacklistItems.value = []
      ElMessage.error('加载黑名单失败：' + (e?.message || ''))
    } finally {
      blacklistLoading.value = false
    }
  }

  const handleUnblacklist = async (item) => {
    const userId = item.user_id ?? item.UserID ?? item.userId
    const platform = item.platform ?? item.Platform ?? 'web'
    if (!userId) {
      ElMessage.warning('缺少 user_id，无法解除')
      return
    }
    try {
      await ElMessageBox.confirm(`确定解除 ${userId} 的黑名单？`, '解除拉黑', { type: 'warning' })
      await unblacklistUser(userId, platform)
      ElMessage.success('已解除拉黑')
      await loadBlacklist()
    } catch (e) {
      if (e !== 'cancel') ElMessage.error('解除失败：' + (e?.message || ''))
    }
  };

  const handleStatusChange = async (newStatus) => {
    try {
      if (myAgentId.value) {
        if (newStatus === 'online') {
          await goOnline(myAgentId.value)
        } else if (newStatus === 'offline') {
          await goOffline(myAgentId.value)
        } else {
          await updateAgentStatus(myAgentId.value, { status: newStatus })
        }
        ElMessage.success(`已切换至${newStatus === 'online' ? '在线' : newStatus === 'busy' ? '忙碌' : '离线'}`)
      } else {
        ElMessage.info(i18n.global.t('未检测到坐席身份，状态仅本地保存'))
      }
    } catch (e) {
      ElMessage.error('状态切换失败：' + (e?.message || ''))
      myStatus.value = 'offline';
    }
  };

  return {
    myStatus,
    sopStage,
    handlerSwitching,
    blacklistedSessionIds,
    blacklistDialogVisible,
    blacklistItems,
    blacklistLoading,
    setReload,
    aiSummaryCurrent,
    exportTranscriptCurrent,
    snoozeCurrent,
    setPriorityCurrent,
    showCreateSession,
    closeSession,
    handleTakeover,
    handleRelease,
    saveSopStage,
    sendCoupon,
    sendProductCard,
    blacklist,
    openBlacklistDialog,
    loadBlacklist,
    handleUnblacklist,
    handleStatusChange
  }
}
