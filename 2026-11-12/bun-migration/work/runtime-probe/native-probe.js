import { createRequire } from "node:module"
import path from "node:path"
import { fileURLToPath } from "node:url"

const require = createRequire(import.meta.url)
const root = "d:/Toshikoshi_chan/Agent/音无/mysy2/mysy2/home/SERVER2/TRSS_AllBot/TRSS-Yunzai"
const work = path.dirname(fileURLToPath(import.meta.url))
const target = process.argv[2]
const direct = target.endsWith("-pnpm")
const kind = direct ? target.slice(0, -5) : target
const result = { target, runtime: globalThis.Bun?.version ?? process.version }

try {
  if (kind === "level") {
    const { Level } = require(direct ? `${root}/node_modules/.pnpm/level@10.0.0/node_modules/level` : `${root}/node_modules/level`)
    const db = new Level(path.join(work, "leveldb"), { valueEncoding: "json" })
    await db.open()
    await db.put("probe", { value: 42 })
    result.value = await db.get("probe")
    result.iterated = []
    for await (const [key, value] of db.iterator()) result.iterated.push([key, value])
    await db.close()
  } else if (kind === "sqlite") {
    const sqlite3Module = require(direct ? `${root}/node_modules/.pnpm/@karinjs+sqlite3@0.4.3/node_modules/@karinjs/sqlite3` : `${root}/node_modules/sqlite3`)
    const sqlite3 = sqlite3Module.default ?? sqlite3Module
    const sqlite = sqlite3.verbose?.() ?? sqlite3
    const db = await new Promise((resolve, reject) => {
      const handle = new sqlite.Database(":memory:", error => error ? reject(error) : resolve(handle))
    })
    result.row = await new Promise((resolve, reject) => {
      db.get("SELECT 42 AS value", (error, row) => error ? reject(error) : resolve(row))
    })
    await new Promise((resolve, reject) => db.close(error => error ? reject(error) : resolve()))
  } else if (kind === "sharp34" || kind === "sharp33") {
    const plugin = kind === "sharp34" ? "pixiv-plugin" : "Speech-statistics-plugin"
    const version = kind === "sharp34" ? "0.34.5" : "0.33.5"
    const sharp = require(direct ? `${root}/node_modules/.pnpm/sharp@${version}/node_modules/sharp` : `${root}/plugins/${plugin}/node_modules/sharp`)
    const input = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", "base64")
    const output = path.join(work, `${target}-output.png`)
    await sharp(input).png().toFile(output)
    const metadata = await sharp(output).metadata()
    result.metadata = { format: metadata.format, width: metadata.width, height: metadata.height }
  } else if (kind === "hummus") {
    const hummus = require(`${root}/node_modules/.pnpm/hummus@1.0.118/node_modules/hummus`)
    result.pdfWriterType = typeof hummus.PDFWriter
  } else if (kind === "bufferutil") {
    const bufferutil = require(`${root}/node_modules/.pnpm/bufferutil@4.0.9/node_modules/bufferutil`)
    const source = Buffer.from([1, 2, 3, 4])
    const mask = Buffer.from([5, 6, 7, 8])
    const output = Buffer.alloc(4)
    bufferutil.mask(source, mask, output, 0, output.length)
    bufferutil.unmask(output, mask)
    result.values = [...output]
  } else {
    throw new Error("unknown probe target")
  }
  console.log(JSON.stringify({ ok: true, ...result }))
} catch (error) {
  console.log(JSON.stringify({ ok: false, ...result, error: String(error), stack: error.stack }))
  process.exitCode = 1
}
