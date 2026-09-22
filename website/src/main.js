import { createApp } from 'vue'

import '@fontsource/fraunces/400.css'
import '@fontsource/fraunces/500.css'
import '@fontsource/fraunces/600.css'
import '@fontsource/fraunces/700.css'
import '@fontsource/fraunces/800.css'
import '@fontsource/fraunces/900.css'
import '@fontsource/manrope/400.css'
import '@fontsource/manrope/500.css'
import '@fontsource/manrope/600.css'
import '@fontsource/manrope/700.css'
import '@fontsource/manrope/800.css'
import '@fontsource/jetbrains-mono/400.css'
import '@fontsource/jetbrains-mono/500.css'
import '@fontsource/jetbrains-mono/600.css'

import './style.css'
import App from './App.vue'
import router from './router'
import i18n from './i18n'
import { applyDirection } from './i18n/locale.js'

const app = createApp(App)
app.use(router)
app.use(i18n)
applyDirection(i18n.global.locale.value)
app.mount('#app')

