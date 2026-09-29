import { createPinia } from 'pinia'
import { useUserStore } from './user'
import { useAppStore } from './app'
import { useGeoStore } from './geo'

const pinia = createPinia()

export default pinia
export { useUserStore, useAppStore, useGeoStore }