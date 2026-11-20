/* eslint-disable vue/one-component-per-file -- Synthetic list and editor share one router fixture. */
import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { defineComponent, h, nextTick, onMounted, ref } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import ElementPlus from 'element-plus'
import i18n from '@/i18n'
import ListEditorOutlet from '@/layout/ListEditorOutlet.vue'
import { useListEditor, useListEditorRefresh } from '@/composables/useListEditor'
import { usePermissionStore } from '@/stores/permission'
import { settle } from './settle-helper'

vi.mock('@/utils/request', () => ({ updateRequestConfig: vi.fn(), http: {}, default: {} }))

const owner = '/records'
const action = '/records/42/edit'

async function setup(grantedPaths, initialPath, history = createMemoryHistory()) {
  const pinia = createPinia()
  const permission = usePermissionStore(pinia)
  permission.menusLoaded = true
  permission.menus = [{ key: 'workspace', children: grantedPaths.map(path => ({ key: path, path })) }]
  const mounted = vi.fn()
  const refreshed = vi.fn()
  const List = defineComponent({
    setup() {
      const search = ref('')
      onMounted(mounted)
      useListEditorRefresh(owner, refreshed)
      return () => h('input', {
        'aria-label': 'filter',
        value: search.value,
        onInput: (event) => { search.value = event.target.value }
      })
    }
  })
  const Editor = defineComponent({
    setup() {
      const { close, markChanged } = useListEditor(owner)
      return () => h('section', { 'data-test': 'editor' }, [
        h('button', { onClick: close }, 'cancel'),
        h('button', { onClick: () => { markChanged(); close() } }, 'save'),
        h('button', { onClick: markChanged }, 'result')
      ])
    }
  })
  const router = createRouter({ history, routes: [
    { path: owner, component: List },
    { path: '/records/:id/edit', components: { default: Editor, list: List }, meta: { listEditor: owner, title: 'Edit record' } },
    { path: '/profile', component: { render: () => null } }
  ] })
  await router.push(initialPath)
  const wrapper = mount(ListEditorOutlet, { attachTo: document.body, global: { plugins: [router, pinia, ElementPlus, i18n] } })
  await settle(wrapper)
  return { wrapper, router, mounted, refreshed, permission }
}

describe('列表内编辑', () => {
  it('保留同一个列表和筛选值；取消不刷新或写入，并恢复原列表 URL', async () => {
    const listUrl = `${owner}?page=3#results`
    const { wrapper, router, mounted, refreshed } = await setup([owner, action], listUrl)
    try {
      const filter = wrapper.get('input[aria-label="filter"]')
      await filter.setValue('existing filter')
      await router.push(action)
      await settle(wrapper)
      expect(document.body.querySelector('[role="dialog"] [data-test="editor"]')).not.toBeNull()
      expect(wrapper.get('input').element).toBe(filter.element)
      document.body.querySelector('[data-test="editor"] button').click()
      await settle(wrapper)
      expect(router.currentRoute.value.fullPath).toBe(listUrl)
      expect(wrapper.get('input').element.value).toBe('existing filter')
      expect(mounted).toHaveBeenCalledTimes(1)
      expect(refreshed).not.toHaveBeenCalled()
    } finally { wrapper.unmount() }
  })

  it('直接打开旧地址时，有列表授权就显示抽屉；保存通知列表刷新后关闭', async () => {
    const { wrapper, router, mounted, refreshed } = await setup([owner, action], action)
    try {
      expect(mounted).toHaveBeenCalledTimes(1)
      document.body.querySelector('[role="dialog"] [data-test="editor"] button:nth-child(2)').click()
      await settle(wrapper)
      expect(refreshed).toHaveBeenCalledTimes(1)
      expect(mounted).toHaveBeenCalledTimes(1)
      expect(router.currentRoute.value.path).toBe(owner)
    } finally { wrapper.unmount() }
  })

  it('只有操作权限时，旧地址可使用且不会挂载或刷新未授权的列表', async () => {
    const { wrapper, router, mounted, refreshed } = await setup([action], action)
    try {
      expect(document.body.querySelector('[role="dialog"] [data-test="editor"]')).toBeNull()
      await wrapper.get('[data-test="editor"] button:nth-child(2)').trigger('click')
      await settle(wrapper)
      expect(router.currentRoute.value.path).toBe('/profile')
      expect(mounted).not.toHaveBeenCalled()
      expect(refreshed).not.toHaveBeenCalled()
    } finally { wrapper.unmount() }
  })

  it('浏览器后退关抽屉并保留列表，打开异步结果时不强制关抽屉', async () => {
    const { wrapper, router, mounted, refreshed } = await setup([owner, action], owner)
    try {
      await router.push(action)
      await settle(wrapper)
      document.body.querySelector('[role="dialog"] [data-test="editor"] button:nth-child(3)').click()
      await nextTick()
      expect(refreshed).toHaveBeenCalledTimes(1)
      expect(router.currentRoute.value.path).toBe(action)
      router.back()
      await settle(wrapper)
      expect(router.currentRoute.value.path).toBe(owner)
      expect(mounted).toHaveBeenCalledTimes(1)
    } finally { wrapper.unmount() }
  })
})
