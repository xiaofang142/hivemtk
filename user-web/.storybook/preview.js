/** @type { import('@storybook/vue3').Preview } */
// =============================================================================
// Storybook 全局预览配置 ()
// 控制所有 story 的包裹层、全局装饰器、参数
// =============================================================================

import { setup } from '@storybook/vue3';

// 1) 全局 i18n 注入 — 让组件的 $t() 调用不报错
setup((app) => {
  // 这里仅做骨架占位; 真实环境应 import i18n 实例
});

// 2) 全局装饰器: 给所有 story 套一层容器, 统一 padding/背景
export const decorators = [
  (story) => ({
    components: { story },
    template: `
      <div style="padding: 24px; background: #f7f8fa; min-height: 100vh; box-sizing: border-box;">
        <story />
      </div>
    `,
  }),
];

// 3) 全局参数: 控制面板/工具栏
export const parameters = {
  // 操作日志 addon
  actions: { argTypesRegex: '^on[A-Z].*' },
  // 控件 addon
  controls: {
    matchers: {
      color: /(background|color)$/i,
      date: /Date$/,
    },
  },
  // 背景色
  backgrounds: {
    default: 'light',
    values: [
      { name: 'light', value: '#ffffff' },
      { name: 'gray', value: '#f5f5f5' },
      { name: 'dark', value: '#1f2937' },
    ],
  },
  // 视口
  viewport: {
    defaultViewport: 'responsive',
  },
};
