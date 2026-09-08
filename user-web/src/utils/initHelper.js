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

export const markInitializationComplete = async () => {
  localStorage.setItem(INIT_STATUS_KEY, 'true')
};

export const createDefaultAdmin = async () => {
  try {
    const { http } = await import('@/utils/request');
    
    const response = await http.post('/api/system/create-default-admin');
    return response
  } catch (error) {
    console.error('创建默认管理员账户失败:', error)
    
    if (error.response?.data?.msg === '管理员已存在') {
      return { success: true, message: '管理员已存在' }
    }
    if (error.response?.status === 404) {
      throw new Error('创建默认管理员账户失败: 请求地址不存在', { cause: error })
    }
    if (!error.response) {
      throw new Error('创建默认管理员账户失败: 网络连接失败', { cause: error })
    }
    throw new Error('创建默认管理员账户失败: ' + (error.response?.data?.message || error.message), { cause: error })
  }
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
