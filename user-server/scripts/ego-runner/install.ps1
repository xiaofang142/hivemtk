# ego-runner 安装器（specs/015 C7，Windows；设计文档 R2）
#
# 用法（PowerShell）：
#   .\install.ps1 [-ServerUrl ws://127.0.0.1:8204] [-Token eg_...] [-EgoServerName <name>]
#              [-TrustUrl https://<实例>/api/egolite/assets/trust.pem]
#   .\install.ps1 -Check          # 只核对环境与配置，不写不装
#   .\uninstall.ps1               # 卸载（同目录）
#
# 与 macOS install.sh 逐项对齐（同一份 conf 形状、同一组参数、同一种"不覆盖已有 conf"）：
#   1. 编译 runner（go build，需 Go 1.25+；或 -Bin 指定已编译 exe）
#   2. 落盘 %USERPROFILE%\.hivemtk\bin\ego-runner.exe + conf 模板（已存在的 conf 不覆盖）
#   3. 注册登录自启（计划任务 cn.hivemtk.ego-runner；无 ScheduledTasks 模块时回落 HKCU Run）
#   4. -Check 模式核对 server_url / ego-browser / conf / runner / 自启项
#
# 形态说明：server_url 指向回环 ⇒ conf 写 loopback_only=true（仅提示，判定在服务端）。

[CmdletBinding()]
param(
    [string]$ServerUrl = "ws://127.0.0.1:8204",
    [string]$Token = "",
    [string]$EgoServerName = "",
    [string]$Bin = "",
    [string]$TrustUrl = "",
    [switch]$Check
)

$ErrorActionPreference = "Stop"
$Prefix   = Join-Path $env:USERPROFILE ".hivemtk"
$BinDir   = Join-Path $Prefix "bin"
$Conf     = Join-Path $Prefix "ego_runner.conf"
$TaskName = "cn.hivemtk.ego-runner"
$ExePath  = Join-Path $BinDir "ego-runner.exe"
$LogPath  = Join-Path $Prefix "ego-runner.log"
$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..")

function Say($msg) { Write-Host "[ego-runner] $msg" -ForegroundColor Cyan }
function Die($msg) { Write-Host "[ego-runner] X $msg" -ForegroundColor Red; exit 1 }

function Get-Conf([string]$path) {
    $map = @{}
    if (-not (Test-Path $path)) { return $map }
    foreach ($line in Get-Content -LiteralPath $path) {
        $t = $line.Trim()
        if ($t -eq "" -or $t.StartsWith("#")) { continue }
        $i = $t.IndexOf("=")
        if ($i -lt 0) { continue }
        $map[$t.Substring(0, $i).Trim()] = $t.Substring($i + 1).Trim()
    }
    return $map
}

if ($Check) {
    Say "server_url = $ServerUrl"
    $ego = Get-Command ego-browser -ErrorAction SilentlyContinue
    if ($ego) { Say "ego-browser = $($ego.Source)" } else { Say "! ego-browser 不在 PATH——请先安装并启动 ego lite" }
    if (Test-Path $Conf) {
        Say "conf = $Conf（已存在，不覆盖）"
        $c = Get-Conf $Conf
        foreach ($k in @("server_url", "workspace_dir", "ego_server_name", "loopback_only")) {
            if ($c.ContainsKey($k)) { Say "    $k=$($c[$k])" }
        }
        if ($c["token"] -like "eg_*") { Say "token = 已配置" } else { Say "! conf 缺 token= 行" }
    } else { Say "! conf 不存在：$Conf" }
    if (Test-Path $ExePath) { Say "runner 二进制 = $ExePath" } else { Say "! runner 未安装：$ExePath" }
    if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
        Say "计划任务 = 已注册"
    } elseif (Get-ItemProperty "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run" -Name $TaskName -ErrorAction SilentlyContinue) {
        Say "自启项 = 已注册（HKCU Run 回落模式）"
    } else { Say "自启 = 未注册" }
    exit 0
}

# ---------- 1. 二进制 ----------
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
if ($Bin -ne "") {
    if (-not (Test-Path $Bin)) { Die "-Bin 指定的文件不存在: $Bin" }
    Copy-Item -Force $Bin $ExePath
    Say "使用指定二进制：$Bin"
} else {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        Die "未找到 go，请先安装 Go 1.25+ 或用 -Bin 指定已编译二进制"
    }
    Say "编译 runner（go build ./cmd/ego-runner）…"
    Push-Location $RepoRoot
    try { go build -o $ExePath ./cmd/ego-runner } finally { Pop-Location }
}
Say "已安装：$ExePath"

# ---------- 2. conf（不覆盖已有） ----------
if (Test-Path $Conf) {
    Say "conf 已存在，保留：$Conf"
} else {
    if ($Token -eq "") { Die "conf 不存在且未提供 -Token（在 user-web「浏览器自动化 → ego 资产中心 → Runner」页签发）" }
    $loop = if ($ServerUrl -match '^ws://(127\.0\.0\.1|localhost|\[::1\])') { "true" } else { "false" }
    New-Item -ItemType Directory -Force -Path $Prefix | Out-Null
    $lines = @(
        "# HiveMtk ego-runner 配置（key=value；详见 scripts/ego-runner/README.md）",
        "server_url=$ServerUrl",
        "token=$Token",
        "device_id=$([guid]::NewGuid().ToString())",
        "loopback_only=$loop"
    )
    if ($EgoServerName -ne "") { $lines += "ego_server_name=$EgoServerName" }
    Set-Content -LiteralPath $Conf -Value $lines -Encoding UTF8
    # Windows 无 POSIX 权限位：用 ACL 只保留当前用户读写
    icacls $Conf /inheritance:r /grant:r "$($env:USERNAME):(R,W)" | Out-Null
    Say "已生成 conf：$Conf（loopback_only=$loop）"
}

# ---------- 2.5 trust.pem（可选；S-6 分发通道） ----------
if ($TrustUrl -ne "") {
    $TrustPath = Join-Path $Prefix "ego_trust.pem"
    if (Test-Path $TrustPath) {
        Say "trust.pem 已存在，保留：$TrustPath"
    } else {
        Say "拉取信任公钥：$TrustUrl"
        try {
            Invoke-WebRequest -Uri $TrustUrl -OutFile $TrustPath -UseBasicParsing
            Say "已落盘：$TrustPath"
        } catch {
            Die "trust.pem 拉取失败（检查实例是否配置 EGO_ASSET_TRUST_PUBLIC_KEY）: $($_.Exception.Message)"
        }
    }
}

# ---------- 3. 登录自启 ----------
$task = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($task) { Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false }
if (Get-Command Register-ScheduledTask -ErrorAction SilentlyContinue) {
    $action    = New-ScheduledTaskAction -Execute $ExePath
    $trigger   = New-ScheduledTaskTrigger -AtLogOn
    $settings  = New-ScheduledTaskSettingsSet -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
    Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Settings $settings -Description "HiveMtk ego runner" | Out-Null
    Start-ScheduledTask -TaskName $TaskName
    Say "已注册计划任务：$TaskName（日志 $LogPath）"
} else {
    $runKey = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
    New-Item -Path $runKey -Force | Out-Null
    Set-ItemProperty -Path $runKey -Name $TaskName -Value "`"$ExePath`""
    Start-Process -FilePath $ExePath -WindowStyle Hidden -RedirectStandardError $LogPath -RedirectStandardOutput $LogPath
    Say "已注册 HKCU Run 自启项：$TaskName（日志 $LogPath）"
}

Say "OK 安装完成。核对：.\install.ps1 -Check"
Say "   服务端确认：user-web → 浏览器自动化 → ego Runner 页应显示在线状态。"
