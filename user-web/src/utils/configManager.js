import axios from 'axios';

const _CONFIG_FILE_PATH = '/src/config/http.js';

const DEFAULT_CONFIG_TEMPLATE = `// API配置文件
// 此文件由系统自动生成，请勿手动修改
const API_BASE_URL = '{{API_BASE_URL}}'

export { API_BASE_URL }
`;

export const saveApiConfig = async (config) => {
  try {
    localStorage.setItem('apiConfig', JSON.stringify(config));
    
    const _configContent = DEFAULT_CONFIG_TEMPLATE
      .replace('{{API_BASE_URL}}', config.baseUrl);
    
    return { success: true };
  } catch (error) {
    console.error('保存API配置失败:', error)
    return { success: false, error: error.message }
  }
};

export const getApiConfig = () => {
  try {
    const configStr = localStorage.getItem('apiConfig');
    if (!configStr) {
      return {
        baseUrl: ''
      }
    }

    const config = JSON.parse(configStr)
    return {
      baseUrl: config.baseUrl || ''
    }
  } catch (error) {
    console.error('获取API配置失败:', error)
    return {
      baseUrl: ''
    }
  }
};

export const testApiConnection = async (config) => {
  try {
    const fullUrl = `${config.baseUrl}/api/health`
    const response = await axios.get(fullUrl, { timeout: 10000 })
    if (response?.data?.code === 200 || response?.data?.status === 'ok') {
      return { success: true, message: '连接成功' }
    }
    return { success: false, message: response?.data?.msg || response?.data?.message || '连接失败' }
  } catch (error) {
    return {
      success: false,
      message: error.response?.data?.message || error.message || '连接失败'
    }
  }
};

