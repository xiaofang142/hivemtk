# ego-runner 卸载器（Windows；与 uninstall.sh 同语义：只摘安装物，保留 conf）
$ErrorActionPreference = "Stop"
$Prefix   = Join-Path $env:USERPROFILE ".hivemtk"
$BinDir   = Join-Path $Prefix "bin"
$TaskName = "cn.hivemtk.ego-runner"

if (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue) {
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
    Write-Host "[ego-runner] 已注销计划任务：$TaskName"
}
Remove-ItemProperty "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run" -Name $TaskName -ErrorAction SilentlyContinue
Remove-Item -Force (Join-Path $BinDir "ego-runner.exe") -ErrorAction SilentlyContinue
Remove-Item -Force (Join-Path $Prefix "ego-runner.log") -ErrorAction SilentlyContinue
Write-Host "[ego-runner] 已卸载（conf 保留：$Prefix\ego_runner.conf）"
