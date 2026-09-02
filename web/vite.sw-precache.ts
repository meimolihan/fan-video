import { promises as fs } from 'fs'
import path from 'path'
import type { Plugin } from 'vite'

// 构建后把首屏核心资源清单写入 dist/assets/sw-precache.json，
// Service Worker install 阶段读取并预热，使二次访问可离线秒开。
// 只收录非内容哈希入口无法预见的稳定资源清单；其余 hash 资源由
// fetch 阶段的运行时缓存兜底，无需在 manifest 里重复列出。
export function swPrecacheManifest(): Plugin {
  let outDir = 'dist'
  return {
    name: 'sw-precache-manifest',
    configResolved(config) {
      outDir = config.build.outDir ?? outDir
    },
    async writeBundle(_options, bundle) {
      const assets: string[] = ['/assets/favicon.svg']
      for (const key of Object.keys(bundle)) {
        const asset = bundle[key]
        if (asset.type !== 'chunk' && asset.type !== 'asset') continue
        const fileName = key.replace(/^assets\//, '')
        if (!fileName.startsWith('index-')) continue
        assets.push(`/assets/${fileName}`)
      }
      const target = path.resolve(outDir, 'assets/sw-precache.json')
      await fs.mkdir(path.dirname(target), { recursive: true })
      await fs.writeFile(target, JSON.stringify(assets, null, 2) + '\n')
    },
  }
}