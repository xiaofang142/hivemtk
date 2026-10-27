<template>
  <div class="guide">
    <p class="lead">
      ego-runner 是跑在<strong>你自己电脑</strong>上的小进程：它把本机 ego-browser 接到 HiveMTK，
      于是「流程/资产」可以在你的浏览器里执行（用的是你已登录的网站与个人上下文）。
      四步装好：
    </p>
    <ol class="steps">
      <li>
        装 ego lite 客户端（自带 ego-browser CLI），确认能用：
        <code>ego-browser --version</code>
        <div class="note">
          learnings 目录固定在 <code>~/.local/share/ego/ego-skills</code>（ego 自己的资产目录）；
          runner 会把流程/资产写进它的 <code>learnings/</code> 下
        </div>
      </li>
      <li>
        编译 runner：<code>cd user-server &amp;&amp; go build -o ~/.hivemtk/ego-runner ./cmd/ego-runner</code>
      </li>
      <li>
        写配置 <code>~/.hivemtk/ego_runner.conf</code>（key=value，同 nm_host.conf 形状；也可用
        <code>user-server/scripts/ego-runner/install.sh</code> 一键安装）：
        <pre class="conf">server_url=ws://127.0.0.1:8204
token=eg_&lt;你的用户ID&gt;_&lt;随机串&gt;
workspace_dir=~/.local/share/ego/ego-skills
ego_bin=ego-browser
# 可选：本机有多个 ego lite 实例时按名连接（官方 --ego-server-name）
# ego_server_name=work-a</pre>
        <div class="note">
          token 在本页「我的 runner 令牌」里签发（明文只显示一次）；撤销即立刻失效。
          ego lite 升级由官方负责：管理端可向在线 runner 下发 <code>ego-browser upgrade</code> 指令
        </div>
      </li>
      <li>
        启动：<code>~/.hivemtk/ego-runner</code>
        <div class="note">
          常驻建议交给系统服务（macOS launchd / Linux systemd），进程退出后可重连；
          日志里出现 <code>register ok</code> 即连上
        </div>
      </li>
    </ol>
    <p class="lead">
      装好回本页点「刷新」，状态应显示在线；之后在「我的流程」试跑或「资产中心」运行资产即可下发。
    </p>
  </div>
</template>

<style scoped>
.guide { line-height: 1.8; }
.lead { margin: 0 0 6px; }
.steps { margin: 0 0 6px 18px; padding: 0; }
.steps li { margin-bottom: 8px; }
.note { color: #909399; font-size: 12px; line-height: 1.6; }
.conf {
  margin: 6px 0;
  padding: 8px 10px;
  background: #f5f7fa;
  border-radius: 4px;
  font-size: 12px;
  line-height: 1.6;
}
</style>
