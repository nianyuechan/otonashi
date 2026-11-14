param(
  [Parameter(Mandatory = $true)][string]$PluginsRoot,
  [Parameter(Mandatory = $true)][string]$OutputDirectory
)
$ErrorActionPreference = "Stop"
$extensions = @(".js", ".cjs", ".mjs", ".jsx", ".ts", ".tsx", ".vue")
$patterns = [ordered]@{
  plugin_base = '(?<![\w$])(?:extends\s+plugin\b|new\s+plugin\s*\()'
  Bot_member = '(?<![\w$])Bot\.[A-Za-z_$][\w$]*'
  segment = '(?<![\w$])segment\.'
  logger = '(?<![\w$])logger\.'
  redis = '(?<![\w$])redis\.'
  cfg_lexical = '(?<![\w$])cfg\.'
  common_lexical = '(?<![\w$])common\.'
  puppeteer_lexical = '(?<![\w$])puppeteer\.'
  Renderer = '(?<![\w$])Renderer\b'
  Handler = '(?<![\w$])Handler\.'
  runtime = '(?<![\w$])(?:e|this\.e)\.runtime\b'
  e_reply = '(?<![\w$])(?:e|this\.e)\.reply\b'
  e_recall = '(?<![\w$])(?:e|this\.e)\.recall\b'
  e_group = '(?<![\w$])(?:e|this\.e)\.group\b'
  e_friend = '(?<![\w$])(?:e|this\.e)\.friend\b'
  e_member = '(?<![\w$])(?:e|this\.e)\.member\b'
  e_bot = '(?<![\w$])(?:e|this\.e)\.bot\b'
  miao_alias = '#miao(?:\.models|\.path)?'
  core_relative_import = '(?:\.\./)+lib/(?:config|plugins|renderer|puppeteer|common|util|bot)'
  node_builtin = '(?:node:|[\"\x27](?:fs|path|os|url|util|events|stream|crypto|buffer|http|https|zlib|worker_threads|child_process)[\"\x27])'
  child_process = '(?:node:child_process|[\"\x27]child_process[\"\x27])'
  lifecycle_init = '(?<![\w$])init\s*\([^)]*\)\s*\{'
  lifecycle_accept = '(?<![\w$])accept\s*\([^)]*\)\s*\{'
  lifecycle_rule = '(?<![\w$])rule\s*(?::|=)'
  lifecycle_task = '(?<![\w$])task\s*(?::|=)'
  lifecycle_handler = '(?<![\w$])handler\s*(?::|=)'
  cross_plugin_path = '(?:plugins/[A-Za-z0-9_.-]+/|(?:\.\./)+(?:[A-Za-z0-9_.-]+-plugin|miao-plugin|genshin|liangshi-calc|Atlas)/)'
  context = '(?<![\w$])(?:setContext|getContext|awaitContext|finish)\s*\('
  node_schedule = '(?:node-schedule|scheduleJob\s*\()'
  setInterval = '(?<![\w$])setInterval\s*\('
  setTimeout = '(?<![\w$])setTimeout\s*\('
}
$stats = @{}
foreach ($key in $patterns.Keys) {
  $stats[$key] = @{ files = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::OrdinalIgnoreCase); plugins = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::OrdinalIgnoreCase); references = 0; examples = [System.Collections.Generic.List[string]]::new() }
}
$botStats = @{}
$packageStats = @{}
$runtimeFileCount = 0
$staticAssetFileCount = 0
$files = [System.Collections.Generic.List[object]]::new()
$stack = [System.Collections.Generic.Stack[System.IO.DirectoryInfo]]::new([System.IO.DirectoryInfo[]]@([System.IO.DirectoryInfo]::new($PluginsRoot)))
while ($stack.Count -gt 0) {
  $dir = $stack.Pop()
  foreach ($child in $dir.GetDirectories()) {
    if ($child.Name -in @(".git", "node_modules")) { continue }
    $stack.Push($child)
  }
  foreach ($file in $dir.GetFiles()) {
    if ($file.Extension.ToLowerInvariant() -notin $extensions) { continue }
    $relative = $file.FullName.Substring($PluginsRoot.Length).TrimStart('\', '/')
    $plugin = ($relative -split '[\\/]')[0]
    $files.Add([pscustomobject]@{ File = $file; Relative = $relative; Plugin = $plugin })
  }
}
function Add-MatchStat {
  param($Table, $Key, $Plugin, $Relative, $Count)
  if (-not $Table.ContainsKey($Key)) {
    $Table[$Key] = @{ files = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::OrdinalIgnoreCase); plugins = [System.Collections.Generic.HashSet[string]]::new([System.StringComparer]::OrdinalIgnoreCase); references = 0; examples = [System.Collections.Generic.List[string]]::new() }
  }
  $entry = $Table[$Key]
  [void]$entry.files.Add($Relative)
  [void]$entry.plugins.Add($Plugin)
  $entry.references += $Count
  if ($entry.examples.Count -lt 6 -and -not $entry.examples.Contains($Relative)) { $entry.examples.Add($Relative) }
}
foreach ($item in $files) {
  $normalized = $item.Relative.Replace('\', '/')
  if ($normalized -match '(^|/)(resources|temp|static|assets|server/public)(/|$)' -or $normalized -match '\.min\.(?:js|cjs|mjs)$') {
    $staticAssetFileCount++
    continue
  }
  $runtimeFileCount++
  $text = [System.IO.File]::ReadAllText($item.File.FullName)
  foreach ($key in $patterns.Keys) {
    $matches = [regex]::Matches($text, $patterns[$key])
    if ($matches.Count) { Add-MatchStat -Table $stats -Key $key -Plugin $item.Plugin -Relative $item.Relative -Count $matches.Count }
  }
  foreach ($match in [regex]::Matches($text, '(?<![\w$])Bot\.([A-Za-z_$][\w$]*)')) {
    Add-MatchStat -Table $botStats -Key $match.Groups[1].Value -Plugin $item.Plugin -Relative $item.Relative -Count 1
  }
  $importPattern = '(?m)(?:\bfrom\s*|\bimport\s*\(\s*|\brequire\s*\(\s*|\bimport\s+)["\x27]([^"\x27]+)["\x27]'
  foreach ($match in [regex]::Matches($text, $importPattern)) {
    $specifier = $match.Groups[1].Value
    if ($specifier.StartsWith('.') -or $specifier.StartsWith('/') -or $specifier.StartsWith('file:')) { continue }
    $parts = $specifier -split '/'
    if ($specifier.StartsWith('@') -and $parts.Count -ge 2) { $package = "$($parts[0])/$($parts[1])" }
    elseif ($specifier.StartsWith('node:')) { $package = $parts[0] }
    else { $package = $parts[0] }
    Add-MatchStat -Table $packageStats -Key $package -Plugin $item.Plugin -Relative $item.Relative -Count 1
  }
}
function Convert-Stats {
  param($Table, $NameColumn)
  $rows = foreach ($key in $Table.Keys) {
    $entry = $Table[$key]
    [pscustomobject]@{
      $NameColumn = $key
      plugin_count = $entry.plugins.Count
      file_count = $entry.files.Count
      references = $entry.references
      plugins = (($entry.plugins | Sort-Object) -join ';')
      example_files = ($entry.examples -join ';')
    }
  }
  return @($rows | Sort-Object plugin_count, file_count, references -Descending)
}
$apiRows = Convert-Stats -Table $stats -NameColumn 'api'
$botRows = Convert-Stats -Table $botStats -NameColumn 'bot_member'
$packageRows = Convert-Stats -Table $packageStats -NameColumn 'package'
$apiRows | Export-Csv -LiteralPath (Join-Path $OutputDirectory 'framework-api.csv') -NoTypeInformation -Encoding utf8
$botRows | Export-Csv -LiteralPath (Join-Path $OutputDirectory 'bot-api.csv') -NoTypeInformation -Encoding utf8
$packageRows | Export-Csv -LiteralPath (Join-Path $OutputDirectory 'package-imports.csv') -NoTypeInformation -Encoding utf8
$upstream = foreach ($pluginDir in Get-ChildItem -LiteralPath $PluginsRoot -Directory -Force | Sort-Object Name) {
  $gitConfig = Join-Path $pluginDir.FullName '.git\config'
  $urls = @()
  if (Test-Path -LiteralPath $gitConfig -PathType Leaf) {
    $gitText = [System.IO.File]::ReadAllText($gitConfig)
    $urls = @([regex]::Matches($gitText, '(?m)^\s*url\s*=\s*(.+?)\s*$') | ForEach-Object { $_.Groups[1].Value -replace '//[^/@]+@', '//***@' })
  }
  $packageName = ''
  $packageVersion = ''
  $repository = ''
  $packagePath = Join-Path $pluginDir.FullName 'package.json'
  if (Test-Path -LiteralPath $packagePath -PathType Leaf) {
    try {
      $package = [System.IO.File]::ReadAllText($packagePath) | ConvertFrom-Json
      $packageName = $package.name
      $packageVersion = $package.version
      if ($package.repository -is [string]) { $repository = $package.repository }
      elseif ($package.repository.url) { $repository = $package.repository.url }
    } catch {}
  }
  [pscustomobject]@{
    plugin = $pluginDir.Name
    has_git_config = Test-Path -LiteralPath $gitConfig -PathType Leaf
    remote_urls = (($urls | Sort-Object -Unique) -join ';')
    package_name = $packageName
    package_version = $packageVersion
    repository = ($repository -replace '//[^/@]+@', '//***@')
  }
}
$upstream | Export-Csv -LiteralPath (Join-Path $OutputDirectory 'plugin-upstream.csv') -NoTypeInformation -Encoding utf8
"source_files=$($files.Count) runtime_scanned_files=$runtimeFileCount excluded_static_asset_files=$staticAssetFileCount" | Write-Output
$apiRows | Select-Object api, plugin_count, file_count, references | Format-Table -AutoSize | Out-String | Write-Output
$botRows | Select-Object -First 30 bot_member, plugin_count, file_count, references | Format-Table -AutoSize | Out-String | Write-Output
$packageRows | Select-Object -First 30 package, plugin_count, file_count, references | Format-Table -AutoSize | Out-String | Write-Output
