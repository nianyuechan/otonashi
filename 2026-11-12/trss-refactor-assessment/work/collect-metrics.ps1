param(
  [Parameter(Mandatory = $true)][string]$Root,
  [Parameter(Mandatory = $true)][string]$OutputDirectory
)

$ErrorActionPreference = "Stop"
$codeExtensions = @(".js", ".cjs", ".mjs", ".jsx", ".ts", ".tsx", ".vue")

function Get-SelfFiles {
  param([Parameter(Mandatory = $true)][string]$Path)
  if (-not (Test-Path -LiteralPath $Path -PathType Container)) { return @() }
  $result = [System.Collections.Generic.List[System.IO.FileInfo]]::new()
  $stack = [System.Collections.Generic.Stack[System.IO.DirectoryInfo]]::new()
  $stack.Push([System.IO.DirectoryInfo]::new($Path))
  while ($stack.Count -gt 0) {
    $dir = $stack.Pop()
    foreach ($file in $dir.GetFiles()) { $result.Add($file) }
    foreach ($child in $dir.GetDirectories()) {
      if ($child.Name -in @(".git", "node_modules")) { continue }
      $stack.Push($child)
    }
  }
  return $result
}

function Get-LineCount {
  param([Parameter(Mandatory = $true)][string]$Path)
  $count = 0
  $reader = [System.IO.StreamReader]::new($Path)
  try { while ($null -ne $reader.ReadLine()) { $count++ } }
  finally { $reader.Dispose() }
  return $count
}

function Test-StaticAsset {
  param([Parameter(Mandatory = $true)][string]$Path)
  $normalized = $Path.Replace('\', '/')
  return $normalized -match '(^|/)(resources|temp|static|assets|server/public)(/|$)' -or $normalized -match '\.min\.(?:js|cjs|mjs)$'
}

function Measure-Tree {
  param(
    [Parameter(Mandatory = $true)][string]$Name,
    [Parameter(Mandatory = $true)][string]$Path,
    [string]$ExcludePrefix
  )
  $files = @(Get-SelfFiles -Path $Path)
  if ($ExcludePrefix) {
    $files = @($files | Where-Object { -not $_.FullName.StartsWith($ExcludePrefix, [System.StringComparison]::OrdinalIgnoreCase) })
  }
  $codeFiles = @($files | Where-Object { $_.Extension.ToLowerInvariant() -in $codeExtensions })
  $lines = 0
  foreach ($file in $codeFiles) { $lines += Get-LineCount -Path $file.FullName }
  $runtimeCodeFiles = @($codeFiles | Where-Object { -not (Test-StaticAsset -Path $_.FullName.Substring($Path.Length)) })
  $runtimeLines = 0
  foreach ($file in $runtimeCodeFiles) { $runtimeLines += Get-LineCount -Path $file.FullName }
  $entryModules = 0
  if (Test-Path -LiteralPath (Join-Path $Path "index.js") -PathType Leaf) {
    $entryModules = 1
  } else {
    $entryModules = @(Get-ChildItem -LiteralPath $Path -File -Filter "*.js" -ErrorAction SilentlyContinue).Count
  }
  [pscustomobject]@{
    name = $Name
    path = $Path
    all_files = $files.Count
    all_bytes = [int64](($files | Measure-Object -Property Length -Sum).Sum ?? 0)
    code_files = $codeFiles.Count
    code_lines = $lines
    code_bytes = [int64](($codeFiles | Measure-Object -Property Length -Sum).Sum ?? 0)
    runtime_code_files = $runtimeCodeFiles.Count
    runtime_code_lines = $runtimeLines
    static_asset_code_files = $codeFiles.Count - $runtimeCodeFiles.Count
    static_asset_code_lines = $lines - $runtimeLines
    entry_modules = $entryModules
    has_package_json = Test-Path -LiteralPath (Join-Path $Path "package.json") -PathType Leaf
    has_git = Test-Path -LiteralPath (Join-Path $Path ".git")
  }
}

function Measure-SingleFile {
  param([Parameter(Mandatory = $true)][string]$Path)
  $file = [System.IO.FileInfo]::new($Path)
  [pscustomobject]@{
    name = $file.Name
    path = $file.FullName
    all_files = 1
    all_bytes = $file.Length
    code_files = 1
    code_lines = Get-LineCount -Path $file.FullName
    code_bytes = $file.Length
    runtime_code_files = 1
    runtime_code_lines = Get-LineCount -Path $file.FullName
    static_asset_code_files = 0
    static_asset_code_lines = 0
    entry_modules = 0
    has_package_json = $false
    has_git = $false
  }
}

$pluginsPath = Join-Path $Root "plugins"
$core = @(
  Measure-SingleFile -Path (Join-Path $Root "app.js")
  Measure-Tree -Name "lib" -Path (Join-Path $Root "lib")
  Measure-Tree -Name "lib-excluding-vendored-modules" -Path (Join-Path $Root "lib") -ExcludePrefix (Join-Path $Root "lib\modules")
  Measure-Tree -Name "lib-modules-vendored" -Path (Join-Path $Root "lib\modules")
  Measure-Tree -Name "renderers" -Path (Join-Path $Root "renderers")
)
$plugins = @(
  Get-ChildItem -LiteralPath $pluginsPath -Directory -Force |
    Sort-Object Name |
    ForEach-Object { Measure-Tree -Name $_.Name -Path $_.FullName }
)
$looseFiles = @(Get-ChildItem -LiteralPath $pluginsPath -File -Force)
$summary = [pscustomobject]@{
  collected_at = (Get-Date).ToString("o")
  root = $Root
  exclusions = @(".git", "node_modules")
  code_extensions = $codeExtensions
  line_definition = "physical lines, including blank lines and comments"
  plugin_directory_count = $plugins.Count
  loadable_plugin_directory_count = @($plugins | Where-Object { $_.entry_modules -gt 0 }).Count
  package_manifest_count = @($plugins | Where-Object { $_.has_package_json }).Count
  git_metadata_plugin_count = @($plugins | Where-Object { $_.has_git }).Count
  plugin_all_files = [int64](($plugins | Measure-Object -Property all_files -Sum).Sum ?? 0)
  plugin_all_bytes = [int64](($plugins | Measure-Object -Property all_bytes -Sum).Sum ?? 0)
  plugin_code_files = [int64](($plugins | Measure-Object -Property code_files -Sum).Sum ?? 0)
  plugin_code_lines = [int64](($plugins | Measure-Object -Property code_lines -Sum).Sum ?? 0)
  plugin_code_bytes = [int64](($plugins | Measure-Object -Property code_bytes -Sum).Sum ?? 0)
  plugin_runtime_code_files = [int64](($plugins | Measure-Object -Property runtime_code_files -Sum).Sum ?? 0)
  plugin_runtime_code_lines = [int64](($plugins | Measure-Object -Property runtime_code_lines -Sum).Sum ?? 0)
  plugin_static_asset_code_files = [int64](($plugins | Measure-Object -Property static_asset_code_files -Sum).Sum ?? 0)
  plugin_static_asset_code_lines = [int64](($plugins | Measure-Object -Property static_asset_code_lines -Sum).Sum ?? 0)
  static_asset_path_heuristic = "resources,temp,static,assets,server/public path segments and .min.js/.min.cjs/.min.mjs"
  loose_plugin_files = $looseFiles.Count
  loose_plugin_bytes = [int64](($looseFiles | Measure-Object -Property Length -Sum).Sum ?? 0)
}
$result = [pscustomobject]@{ summary = $summary; core = $core; plugins = $plugins }
$result | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $OutputDirectory "metrics.json") -Encoding utf8
$plugins | Export-Csv -LiteralPath (Join-Path $OutputDirectory "plugin-metrics.csv") -NoTypeInformation -Encoding utf8
$core | Export-Csv -LiteralPath (Join-Path $OutputDirectory "core-metrics.csv") -NoTypeInformation -Encoding utf8
$summary | Format-List | Out-String | Write-Output
$core | Select-Object name, all_files, all_bytes, code_files, code_lines | Format-Table -AutoSize | Out-String | Write-Output
$plugins | Sort-Object all_bytes -Descending | Select-Object -First 15 name, all_files, all_bytes, code_files, code_lines, entry_modules | Format-Table -AutoSize | Out-String | Write-Output
