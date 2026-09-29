import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

export const useGeoStore = defineStore('geo', () => {
  const config = ref({
    brand_name: '',
    brand_description: '',
    advantages: '',
    competitors: '',
    domain: '',
    language: 'zh',
    default_model: '',
    verify_models: '',
    negative_keywords: ''
  })

  const keywords = ref([])
  const articles = ref([])
  const alerts = ref([])
  const sovData = ref([])
  const probeRuns = ref([])
  const crawlerStats = ref(null)
  const reportData = ref(null)
  const decisionReport = ref(null)
  const indexFunnel = ref(null)
  const visibilityTrend = ref([])
  const engineCompare = ref([])

  const unreadAlertCount = computed(() => alerts.value.filter(a => !a.notified).length)
  const hiveMTKSov = computed(() => {
    const own = sovData.value.find(s => s.is_own)
    return own ? own.sov_percent : 0
  })
  const hiveMTKSovTrend = computed(() => {
    const own = sovData.value.find(s => s.is_own)
    return own ? own.trend_change : null
  })
  const totalMentions = computed(() => sovData.value.reduce((sum, s) => sum + s.mentions, 0))
  const hiveMTKMentions = computed(() => {
    const own = sovData.value.find(s => s.is_own)
    return own ? own.mentions : 0
  })
  const competitorCount = computed(() => config.value.competitors ? config.value.competitors.split('、').filter(Boolean).length : 0)

  const setConfig = (newConfig) => {
    config.value = { ...config.value, ...newConfig }
  }

  const setKeywords = (list) => {
    keywords.value = list
  }

  const addKeyword = (keyword) => {
    keywords.value.push(keyword)
  }

  const removeKeyword = (id) => {
    keywords.value = keywords.value.filter(k => k.id !== id)
  }

  const setArticles = (list) => {
    articles.value = list
  }

  const addArticle = (article) => {
    articles.value.unshift(article)
  }

  const updateArticle = (id, updates) => {
    const idx = articles.value.findIndex(a => a.id === id)
    if (idx > -1) {
      articles.value[idx] = { ...articles.value[idx], ...updates }
    }
  }

  const removeArticle = (id) => {
    articles.value = articles.value.filter(a => a.id !== id)
  }

  const setAlerts = (list) => {
    alerts.value = list
  }

  const markAlertNotified = (id) => {
    const alert = alerts.value.find(a => a.id === id)
    if (alert) {
      alert.notified = true
    }
  }

  const removeAlert = (id) => {
    alerts.value = alerts.value.filter(a => a.id !== id)
  }

  const setSovData = (list) => {
    sovData.value = list
  }

  const setProbeRuns = (list) => {
    probeRuns.value = list
  }

  const addProbeRun = (run) => {
    probeRuns.value.unshift(run)
  }

  const setCrawlerStats = (stats) => {
    crawlerStats.value = stats
  }

  const setReportData = (data) => {
    reportData.value = data
  }

  const setDecisionReport = (data) => {
    decisionReport.value = data
  }

  const setIndexFunnel = (data) => {
    indexFunnel.value = data
  }

  const setVisibilityTrend = (list) => {
    visibilityTrend.value = list
  }

  const setEngineCompare = (list) => {
    engineCompare.value = list
  }

  const reset = () => {
    config.value = {
      brand_name: '',
      brand_description: '',
      advantages: '',
      competitors: '',
      domain: '',
      language: 'zh',
      default_model: '',
      verify_models: '',
      negative_keywords: ''
    }
    keywords.value = []
    articles.value = []
    alerts.value = []
    sovData.value = []
    probeRuns.value = []
    crawlerStats.value = null
    reportData.value = null
    decisionReport.value = null
    indexFunnel.value = null
    visibilityTrend.value = []
    engineCompare.value = []
  }

  return {
    config,
    keywords,
    articles,
    alerts,
    sovData,
    probeRuns,
    crawlerStats,
    reportData,
    decisionReport,
    indexFunnel,
    visibilityTrend,
    engineCompare,
    unreadAlertCount,
    hiveMTKSov,
    hiveMTKSovTrend,
    totalMentions,
    hiveMTKMentions,
    competitorCount,
    setConfig,
    setKeywords,
    addKeyword,
    removeKeyword,
    setArticles,
    addArticle,
    updateArticle,
    removeArticle,
    setAlerts,
    markAlertNotified,
    removeAlert,
    setSovData,
    setProbeRuns,
    addProbeRun,
    setCrawlerStats,
    setReportData,
    setDecisionReport,
    setIndexFunnel,
    setVisibilityTrend,
    setEngineCompare,
    reset
  }
})
