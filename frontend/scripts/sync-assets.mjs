import { cp, mkdir, readdir, rm } from 'node:fs/promises'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

const source = fileURLToPath(new URL('../dist/', import.meta.url))
const destination = fileURLToPath(new URL('../../backend/frontend/dist/', import.meta.url))

await mkdir(destination, { recursive: true })
// 清除过期哈希资源，但保留仓库中的嵌入目录占位文件。
for (const name of await readdir(destination)) {
    if (name !== '.gitkeep') {
        await rm(join(destination, name), { recursive: true, force: true })
    }
}
await cp(source, destination, { recursive: true })
