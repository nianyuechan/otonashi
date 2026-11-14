import v8 from "node:v8"
import fs from "node:fs"

const file = "v8-node.bin"
if (process.argv[2] === "write") {
  fs.writeFileSync(file, v8.serialize([Buffer.from("node-v8")]))
  console.log("serialized-with-node")
} else {
  try {
    const value = v8.deserialize(fs.readFileSync(file))
    console.log(JSON.stringify({ ok: true, value: value.map(item => item.toString()) }))
  } catch (error) {
    console.log(JSON.stringify({ ok: false, error: `${error.name}: ${error.message}` }))
    process.exitCode = 1
  }
}
