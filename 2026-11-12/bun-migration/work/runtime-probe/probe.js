import { createRequire } from "node:module"

const require = createRequire(import.meta.url)
const relative1 = await import("./counter.js?case=1")
const relative2 = await import("./counter.js?case=2")
const fileUrl1 = await import(new URL("./counter.js?case=3", import.meta.url).href)
const fileUrl2 = await import(new URL("./counter.js?case=4", import.meta.url).href)

let reportCall = "not-called"
if (typeof process.report?.getReport === "function") {
  try {
    reportCall = process.report.getReport()?.header ? "header-present" : "no-header"
  } catch (error) {
    reportCall = `threw:${error.name}`
  }
}

console.log(JSON.stringify({
  runtime: globalThis.Bun ? "bun" : "node",
  runtimeVersion: globalThis.Bun?.version ?? process.version,
  processVersion: process.version,
  argv1IsProbe: process.argv[1]?.endsWith("probe.js") ?? null,
  dirnameType: typeof __dirname,
  filenameType: typeof __filename,
  importMetaUrl: import.meta.url.startsWith("file:"),
  createRequireResolve: require.resolve("./alias.js").endsWith("alias.js"),
  subpathImport: (await import("#probe")).default,
  relativeQuery: { sameModule: relative1 === relative2, values: [relative1.n, relative2.n] },
  fileUrlQuery: { sameModule: fileUrl1 === fileUrl2, values: [fileUrl1.n, fileUrl2.n] },
  processReportType: typeof process.report?.getReport,
  processReportCall: reportCall,
  processExecveType: typeof process.execve,
  promiseWithResolvers: typeof Promise.withResolvers,
}, null, 2))
