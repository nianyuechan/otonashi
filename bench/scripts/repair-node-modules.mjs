// 修复旧版 TRSS-Yunzai 基准副本的 node_modules 解析。
//
// 背景：原安装从 D: 盘移动到 G: 盘时，pnpm 的 junction 被复制工具解引用成
// 真实目录，根 node_modules 里每个包自己的传递依赖仍留在 .pnpm 存储深层，
// Node 的模块解析因此断裂（例如 chokidar 找不到 readdirp）。
//
// 关键约束：Node 会把 junction 解析成真实路径再做依赖查找，因此任何指向原
// 安装的 junction 都继承原安装（不完整）的解析上下文。基准副本的 node_modules
// 必须是完全自包含的真实文件。
//
// 策略：把原安装根 node_modules 的每个包复制进副本（真实目录），随后 BFS
// 补齐每个已复制包缺失的传递依赖：优先从原安装 .pnpm 存储复制，缺失时按
// 需要嵌套 node_modules 解决版本冲突。原安装不被写入。
//
// 用法：node repair-node-modules.mjs <原安装目录> <基准副本目录>
import { existsSync, lstatSync, mkdirSync, readdirSync, readFileSync, readlinkSync, rmSync, symlinkSync } from "node:fs"
import path from "node:path"

const [, , originArg, benchArg] = process.argv
// 用法：node repair-node-modules.mjs <原安装> <副本> [--resume]
// resume：不重建 node_modules，跳过已复制内容，只补缺失项与 link: 修复。
const resume = process.argv.includes("--resume")
if (!originArg || !benchArg) {
  console.error("用法: node repair-node-modules.mjs <原安装目录> <基准副本目录> [--resume]")
  process.exit(1)
}
const originNM = path.join(originArg, "node_modules")
const benchNM = path.join(benchArg, "node_modules")
if (!existsSync(originNM)) {
  console.error(`原安装 node_modules 不存在: ${originNM}`)
  process.exit(1)
}

function readPackage(dir, verbose = false) {
  try {
    return JSON.parse(readFileSync(path.join(dir, "package.json"), "utf8"))
  } catch (err) {
    if (verbose) console.warn(`读取 ${path.join(dir, "package.json")} 失败: ${err.message}`)
    return null
  }
}

/** 判断目录是否为链接（junction/symlink），并解析其真实目标。 */
function resolveDir(dir) {
  const stat = lstatSync(dir)
  if (!stat.isSymbolicLink()) return { link: false, target: dir }
  try {
    const target = readlinkSync(dir)
    const resolved = path.isAbsolute(target) ? target : path.resolve(path.dirname(dir), target)
    return { link: true, target: resolved }
  } catch {
    return { link: false, target: dir }
  }
}

/** 原安装 .pnpm 存储：包名 → [{ version, dir }]。dir 取真实路径。 */
function loadPnpmStore() {
  const store = new Map()
  const pnpmDir = path.join(originNM, ".pnpm")
  if (!existsSync(pnpmDir)) return store
  for (const entry of readdirSync(pnpmDir)) {
    const inner = path.join(pnpmDir, entry, "node_modules")
    if (!existsSync(inner)) continue
    for (const name of readdirSync(inner)) {
      const raw = path.join(inner, name)
      // scope 目录（如 @borewit）需要下钻一层才是真正的包目录。
      if (name.startsWith("@")) {
        const scope = path.join(inner, name)
        for (const scopedName of readdirSync(scope)) {
          recordStorePackage(store, path.join(scope, scopedName))
        }
        continue
      }
      recordStorePackage(store, raw)
    }
  }
  return store
}

function recordStorePackage(store, raw) {
  const { link, target } = resolveDir(raw)
  const real = link ? target : raw
  if (!existsSync(real)) return
  const pkg = readPackage(real)
  if (!pkg || !pkg.name || !pkg.version) return
  if (!store.has(pkg.name)) store.set(pkg.name, [])
  store.get(pkg.name).push({ version: pkg.version, dir: real })
}

function semverSatisfies(version, range) {
  if (range.startsWith("^")) return version.startsWith(`${range.slice(1).split(".")[0]}.`)
  if (range.startsWith("~")) {
    const [major, minor] = range.slice(1).split(".")
    return version.startsWith(`${major}.${minor}.`)
  }
  return version === range.replace(/^[=v]/, "")
}

function pickVersion(store, name, range) {
  const candidates = store.get(name) ?? []
  if (candidates.length === 0) return null
  const match = candidates.find(item => semverSatisfies(item.version, range ?? "*"))
  if (match) return match
  return [...candidates].sort((a, b) => b.version.localeCompare(a.version))[0]
}

const store = loadPnpmStore()
console.log(`pnpm 存储包含 ${store.size} 个包名`)

// 第一步：重建副本 node_modules 为真实目录（resume 模式跳过）。
const { execSync } = await import("node:child_process")
if (existsSync(benchNM) && !resume) {
  const stat = lstatSync(benchNM)
  const reparse = (stat.attributes ?? "").includes("ReparsePoint")
  const isEmpty = stat.isDirectory() && readdirSync(benchNM).length === 0
  if (!stat.isSymbolicLink() && !reparse && !isEmpty) {
    throw new Error(`${benchNM} 是非空真实目录而非链接，请先人工确认后删除`)
  }
  if (stat.isSymbolicLink() || reparse) {
    // rmdir 只移除链接本身，绝不会删除目标内容。
    execSync(`cmd /c rmdir "${benchNM}"`)
  } else {
    rmSync(benchNM, { recursive: true })
  }
}
mkdirSync(benchNM, { recursive: true })

// copyInto 手写递归复制。不使用 fs.cpSync：本机 node v24.13.0 的 cpSync 在
// 含非 ASCII 的路径上会硬崩溃（RC=127、无错误输出），而 readdirSync/
// copyFileSync 等 API 处理中文路径正常。内部链接按链接复制，复制失败仅警告。
import { copyFileSync, mkdirSync as mkDirSync } from "node:fs"

function copyInto(src, dst) {
  if (existsSync(dst)) return false
  try {
    copyTree(src, dst)
    return true
  } catch (err) {
    console.warn(`复制失败 ${src}: ${err.message}`)
    rmSync(dst, { recursive: true, force: true })
    return false
  }
}

function copyTree(src, dst) {
  const stat = lstatSync(src)
  if (stat.isSymbolicLink()) {
    const target = readlinkSync(src)
    try {
      symlinkSync(target, dst, stat.isDirectory() ? "junction" : "file")
    } catch (err) {
      console.warn(`跳过链接 ${src}: ${err.message}`)
    }
    return
  }
  if (stat.isFile()) {
    copyFileSync(src, dst)
    return
  }
  if (!stat.isDirectory()) return
  mkDirSync(dst, { recursive: true })
  for (const entry of readdirSync(src)) {
    copyTree(path.join(src, entry), path.join(dst, entry))
  }
}

// BFS 工作队列：已复制且需要检查依赖的 { dir } 列表。
const queue = []
const notFound = []

// 第二步：复制原安装根 node_modules 的每个包。
const originEntries = readdirSync(originNM).filter(name => {
  if (name === ".pnpm" || name === ".bin") return false
  const dir = path.join(originNM, name)
  return lstatSync(dir).isDirectory()
})
for (const name of originEntries) {
  const { target } = resolveDir(path.join(originNM, name))
  if (copyInto(target, path.join(benchNM, name))) {
    queue.push(path.join(benchNM, name))
  } else {
    console.warn(`跳过已存在的 ${name}`)
  }
}
console.log(`已复制根依赖 ${originEntries.length} 个`)

// resume 模式：把副本中已存在的包全部重新入队，补齐它们的缺失依赖。
function collectPackageDirs(base, out) {
  for (const entry of readdirSync(base)) {
    if (entry === ".bin") continue
    const dir = path.join(base, entry)
    if (!lstatSync(dir).isDirectory()) continue
    if (entry.startsWith("@")) {
      for (const scoped of readdirSync(dir)) {
        const scopedDir = path.join(dir, scoped)
        if (lstatSync(scopedDir).isDirectory()) {
          if (readPackage(scopedDir)) out.push(scopedDir)
          if (existsSync(path.join(scopedDir, "node_modules"))) {
            collectPackageDirs(path.join(scopedDir, "node_modules"), out)
          }
        }
      }
      continue
    }
    if (readPackage(dir)) out.push(dir)
    if (existsSync(path.join(dir, "node_modules"))) {
      collectPackageDirs(path.join(dir, "node_modules"), out)
    }
  }
}
if (resume) {
  collectPackageDirs(benchNM, queue)
  console.log(`resume: 重新入队 ${queue.length} 个已存在包`)
}

// 第三步（前置）：package.json 里声明为 link: 的依赖（如 lodash ->
// lib/modules/lodash）是工作区链接，其内部带硬编码的相对导入路径，必须在
// 真实声明位置上才能解析。删除解引用副本，改为 junction 指向副本自己的目标。
const originPkg = readPackage(originArg, true)
console.log(`link: 检查 package.json: ${originPkg ? "读取成功" : "读取失败"}`)
if (originPkg) {
  for (const [name, target] of Object.entries(originPkg.dependencies ?? {})) {
    if (typeof target !== "string" || !target.startsWith("link:")) continue
    console.log(`发现 link: 依赖 ${name} -> ${target}`)
    const copied = path.join(benchNM, name)
    const linkTarget = path.resolve(benchArg, target.slice("link:".length))
    if (!existsSync(linkTarget)) {
      console.warn(`link: 依赖目标缺失: ${name} -> ${linkTarget}`)
      continue
    }
    // 本机 node 的 rmSync 在 G 盘中文路径上可能静默不删除（与 cpSync 同源
    // 的 fs 问题），统一改用 cmd rmdir；Windows rmdir 不跟随 junction。
    for (let attempt = 0; attempt < 5; attempt++) {
      if (existsSync(copied)) execSync(`cmd /c rmdir /s /q "${copied}"`)
      let gone = false
      for (let wait = 0; wait < 20 && !gone; wait++) {
        gone = !existsSync(copied)
        if (!gone) {
          await new Promise(resolve => setTimeout(resolve, 50))
        }
      }
      if (!gone) throw new Error(`无法删除 ${copied}`)
      try {
        symlinkSync(linkTarget, copied, "junction")
        console.log(`link: 依赖已按声明位置链接: ${name}`)
        break
      } catch (err) {
        if (attempt === 4) throw err
      }
    }
  }
}

// 第三步：BFS 补齐传递依赖。
// 依赖查找语义与 Node 一致：从包目录向上逐层找 node_modules/<name>。
function resolvableFrom(fromDir, name) {
  let current = fromDir
  while (true) {
    const candidate = path.join(current, name)
    if (existsSync(candidate)) {
      const pkg = readPackage(candidate)
      if (pkg) return { found: true, version: pkg.version }
    }
    const parent = path.dirname(current)
    if (parent === current || !parent.startsWith(benchNM)) return { found: false }
    current = parent
  }
}

let copiedTransitive = 0
while (queue.length > 0) {
  const dir = queue.shift()
  const pkg = readPackage(dir)
  if (!pkg) continue
  const deps = { ...pkg.dependencies, ...pkg.optionalDependencies, ...pkg.peerDependencies }
  for (const [name, range] of Object.entries(deps ?? {})) {
    // Node 只在 dependencies 声明的包上做解析；peer 缺失通常可容忍，
    // 但既然存储里有就直接补齐，代价只是磁盘。
    const resolved = resolvableFrom(dir, name)
    if (resolved.found) {
      const satisfied = resolved.version && (!range || range === "*" || range.startsWith("link:") || semverSatisfies(resolved.version, range))
      if (!satisfied) {
        // 版本不满足：嵌套到该包自己的 node_modules。
        const candidate = pickVersion(store, name, range)
        if (!candidate) {
          notFound.push(`${pkg.name} -> ${name}@${range}`)
          continue
        }
        const nested = path.join(dir, "node_modules")
        mkdirSync(nested, { recursive: true })
        if (copyInto(candidate.dir, path.join(nested, name))) {
          queue.push(path.join(nested, name))
          copiedTransitive++
        }
      }
      continue
    }
    const candidate = pickVersion(store, name, range ?? "*")
    if (!candidate) {
      notFound.push(`${pkg.name} -> ${name}@${range}`)
      continue
    }
    if (copyInto(candidate.dir, path.join(dir, "node_modules", name))) {
      queue.push(path.join(dir, "node_modules", name))
      copiedTransitive++
    }
  }
}
console.log(`已补齐传递依赖 ${copiedTransitive} 个（含嵌套）`)

if (notFound.length > 0) {
  const seen = new Set()
  console.warn("以下依赖在 .pnpm 存储中未找到，启动时可能报错：")
  for (const line of notFound) {
    if (seen.has(line)) continue
    seen.add(line)
    console.warn(`  ${line}`)
  }
}

// 冒烟验证：chokidar 必须能解析 readdirp。
if (existsSync(path.join(benchNM, "chokidar")) && !existsSync(path.join(benchNM, "readdirp"))) {
  console.warn("警告: 根目录仍未复制 readdirp，chokidar 将无法解析")
}
console.log("修复完成")
