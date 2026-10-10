import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

export const useUserStore = defineStore('user', () => {
  const userInfo = ref({
    id: '',
    username: '',
    email: '',
    role: '',
    teamId: 0,
    dataScope: ''
  })
  
  const token = ref('')
  
  const isLoggedIn = computed(() => !!token.value)
  
  const username = computed(() => userInfo.value.username)
  const role = computed(() => userInfo.value.role || '')
  const isAdmin = computed(() => role.value === 'admin');
  const isTeamAdmin = computed(() => role.value === 'team_admin' || isAdmin.value);
  
  const setUserInfo = (info) => {
    userInfo.value = {
      id: info.id || '',
      username: info.username || '',
      email: info.email || '',
      role: info.role || '',
      teamId: info.team_id || info.teamId || 0,
      dataScope: info.data_scope || info.dataScope || ''
    }
    localStorage.setItem('user_info', JSON.stringify(userInfo.value));
  }
  
  const setToken = (newToken) => {
    token.value = newToken
    if (newToken) {
      localStorage.setItem('token', newToken)
    } else {
      localStorage.removeItem('token')
    }
  }
  
  const login = (info, userToken) => {
    setUserInfo(info)
    setToken(userToken)
  }
  
  const logout = () => {
    userInfo.value = {
      id: '',
      username: '',
      email: '',
      role: '',
      teamId: 0,
      dataScope: ''
    }
    token.value = ''
    localStorage.removeItem('token')
    localStorage.removeItem('user_info')
  }
  
  const initAuth = () => {
    const savedToken = localStorage.getItem('token')
    if (savedToken) {
      token.value = savedToken
    }
    const savedUserInfo = localStorage.getItem('user_info')
    if (savedUserInfo) {
      try {
        const parsed = JSON.parse(savedUserInfo)
        userInfo.value = {
          id: parsed.id || '',
          username: parsed.username || '',
          email: parsed.email || '',
          role: parsed.role || '',
          teamId: parsed.teamId || 0,
          dataScope: parsed.dataScope || ''
        }
      } catch (e) {
        localStorage.removeItem('user_info')
      }
    }
  };
  
  initAuth();
  
  return {
    userInfo,
    token,
    isLoggedIn,
    username,
    role,
    isAdmin,
    isTeamAdmin,
    setUserInfo,
    setToken,
    login,
    logout
  }
})