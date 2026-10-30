import {
  saveApiConfig,
  testApiConnection as testConnection,
  getApiConfig as getConfig,
} from './configManager';

export const INIT_STATUS_KEY = 'system_initialized';

export const testApiConnection = async (config) => {
  return await testConnection(config)
};

export const saveApiConfigToFile = async (config) => {
  try {
    await saveApiConfig(config)
    return { success: true }
  } catch (error) {
    throw new Error(error.response?.data?.message || '保存配置失败', { cause: error })
  }
};

// markInitializationComplete 翻的是**后端**安装锁（POST /api/system/init-complete，
// 写 install.lock），不是只打本地标记 —— 后端 init-status 的 initialized 就读这把锁。
// 只写 localStorage 的旧实现会让每个会话的 App.vue 启动都看到 initialized=false，
// 把所有人反复推进 /setup 向导。403 = 已初始化（SetupOnlyGuard），视为成功；
// 网络失败同样吞掉 —— 本地标记先行，不能因一次可失败的请求把调用方挡住。
export const markLocalInitialized = () => {
  localStorage.setItem(INIT_STATUS_KEY, 'true')
}

export const markInitializationComplete = async () => {
  markLocalInitialized()
  try {
    const { http } = await import('@/utils/request');
    await http.post('/api/system/init-complete', {}, { _silent: true });
  } catch { /* 已初始化(403)或网络失败：本地标记已落，不阻断调用方 */ }
};

export const isInitialized = () => {
  return localStorage.getItem(INIT_STATUS_KEY) === 'true'
};

export const getApiConfig = () => {
  try {
    return getConfig()
  } catch (error) {
    throw new Error(error.response?.data?.message || '获取配置失败', { cause: error })
  }
};
