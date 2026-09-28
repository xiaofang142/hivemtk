<template>
  <div class="guide">
    <p class="lead">执行浏览器任务需要本机 Chrome 同时运行 HiveMTK 扩展和 NM Host，按顺序四步：</p>
    <ol class="steps">
      <li>构建扩展：<code>cd user-web/browser_automation && npm install && npm run build</code>
        <div class="note">扩展目录里只有源码，<code>dist</code> 是构建产物、不在版本控制里 —— 不先构建，下一步就选不到目录</div></li>
      <li>chrome://extensions → 打开「开发者模式」→「加载已解压的扩展程序」→ 选择
        <code>user-web/browser_automation/dist</code></li>
      <li>注册 Native Messaging：运行 <code>user-server/cmd/nm-host/install.sh</code>
        <div class="note">扩展 ID 由 manifest 里写死的 <code>key</code> 推导、固定不变，脚本自己算，无需传参数</div></li>
      <li>把 admin 生成的 token 填进 <code>~/.hivemtk/nm_host.conf</code> 的 <code>token=</code> 行
        <div class="note">改过 token 后要重启 Host 进程：扩展断开后不会自动重连</div></li>
    </ol>
    <p class="lead">完成后刷新扩展，回本页点「刷新」，状态应显示「可服务」。</p>
  </div>
</template>

<style scoped>
.guide { line-height: 1.8; }
.lead { margin: 0 0 6px; }
.steps { margin: 0 0 6px 18px; padding: 0; }
.steps li { margin-bottom: 6px; }
.note { color: #909399; font-size: 12px; line-height: 1.6; }
</style>
