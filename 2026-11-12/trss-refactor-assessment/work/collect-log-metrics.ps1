param(
  [Parameter(Mandatory = $true)][string]$LogsRoot,
  [Parameter(Mandatory = $true)][string]$OutputDirectory
)
$ErrorActionPreference = "Stop"
$categories = [ordered]@{
  render_failure = '\[图片生成\].*(?:失败|超时|错误|为空)'
  chromium_restart_or_failure = 'Chromium.*(?:关闭重启|启动失败|断开|错误|崩溃)'
  redis_connection_error = 'Redis.*(?:错误|失败|ECONNREFUSED|Socket)|(?:错误|失败|ECONNREFUSED|Socket).*Redis'
  process_restart = '重启成功|开始重启|TRSS-Yunzai 已停止运行'
  network_timeout_or_reset = '请求超时|ETIMEDOUT|ECONNRESET|socket hang up'
  unhandled_or_memory = 'uncaughtException|unhandledRejection|JavaScript heap|out of memory|ENOMEM|heap limit'
  plugin_load_error = '插件加载错误|插件加载超时'
}
$counts = @{}
foreach ($key in $categories.Keys) { $counts[$key] = 0 }
$renderTimes = [System.Collections.Generic.List[double]]::new()
$renderByName = @{}
$fileRows = @()
$files = @(Get-ChildItem -LiteralPath $LogsRoot -File -Filter '*.log' | Sort-Object Name)
foreach ($file in $files) {
  $fileCounts = @{}
  foreach ($key in $categories.Keys) { $fileCounts[$key] = 0 }
  $lineNumber = 0
  $reader = [System.IO.StreamReader]::new($file.FullName)
  try {
    while ($null -ne ($line = $reader.ReadLine())) {
      $lineNumber++
      foreach ($key in $categories.Keys) {
        if ([regex]::IsMatch($line, $categories[$key], [System.Text.RegularExpressions.RegexOptions]::IgnoreCase)) {
          $counts[$key]++
          $fileCounts[$key]++
        }
      }
      if ($line.Contains('[图片生成]')) {
        foreach ($match in [regex]::Matches($line, '\[图片生成\]\[([^\]]+)\].*?(\d+(?:\.\d+)?)ms')) {
          $milliseconds = [double]::Parse($match.Groups[2].Value, [System.Globalization.CultureInfo]::InvariantCulture)
          $renderTimes.Add($milliseconds)
          $name = $match.Groups[1].Value
          if (-not $renderByName.ContainsKey($name)) { $renderByName[$name] = [System.Collections.Generic.List[double]]::new() }
          $renderByName[$name].Add($milliseconds)
        }
      }
    }
  } finally { $reader.Dispose() }
  $row = [ordered]@{ file = $file.Name; bytes = $file.Length; lines = $lineNumber }
  foreach ($key in $categories.Keys) { $row[$key] = $fileCounts[$key] }
  $fileRows += [pscustomobject]$row
}
function Get-Percentile {
  param([double[]]$Sorted, [double]$Percentile)
  if (-not $Sorted -or $Sorted.Count -eq 0) { return $null }
  $index = [Math]::Ceiling($Sorted.Count * $Percentile) - 1
  $index = [Math]::Max(0, [Math]::Min($Sorted.Count - 1, $index))
  return [Math]::Round($Sorted[$index], 2)
}
function Get-TimeSummary {
  param([System.Collections.Generic.List[double]]$Times)
  $sorted = @($Times | Sort-Object)
  if ($sorted.Count -eq 0) { return $null }
  return [pscustomobject]@{
    count = $sorted.Count
    min_ms = [Math]::Round($sorted[0], 2)
    mean_ms = [Math]::Round(($sorted | Measure-Object -Average).Average, 2)
    p50_ms = Get-Percentile -Sorted $sorted -Percentile 0.50
    p95_ms = Get-Percentile -Sorted $sorted -Percentile 0.95
    p99_ms = Get-Percentile -Sorted $sorted -Percentile 0.99
    max_ms = [Math]::Round($sorted[-1], 2)
  }
}
$summary = [pscustomobject]@{
  logs_root = $LogsRoot
  log_file_count = $files.Count
  oldest_file = @($files | Where-Object { $_.Name -match '^command\.\d{4}-\d{2}-\d{2}\.log$' })[0].Name
  newest_file = @($files | Where-Object { $_.Name -match '^command\.\d{4}-\d{2}-\d{2}\.log$' })[-1].Name
  category_counts = [pscustomobject]$counts
  historical_render_duration = Get-TimeSummary -Times $renderTimes
  note = 'Historical log aggregation only; no process was started and no benchmark was run. Matching is lexical and categories are not necessarily independent.'
}
$summary | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $OutputDirectory 'log-summary.json') -Encoding utf8
$fileRows | Export-Csv -LiteralPath (Join-Path $OutputDirectory 'log-category-counts.csv') -NoTypeInformation -Encoding utf8
$renderRows = foreach ($name in $renderByName.Keys) {
  $stats = Get-TimeSummary -Times $renderByName[$name]
  [pscustomobject]@{ render_name = $name; count = $stats.count; mean_ms = $stats.mean_ms; p50_ms = $stats.p50_ms; p95_ms = $stats.p95_ms; p99_ms = $stats.p99_ms; max_ms = $stats.max_ms }
}
$renderRows | Sort-Object count -Descending | Export-Csv -LiteralPath (Join-Path $OutputDirectory 'historical-render-durations.csv') -NoTypeInformation -Encoding utf8
$summary | Format-List | Out-String | Write-Output
$renderRows | Sort-Object count -Descending | Select-Object -First 15 | Format-Table -AutoSize | Out-String | Write-Output
