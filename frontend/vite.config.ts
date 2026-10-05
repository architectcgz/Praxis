import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
    plugins: [react()],
    build: {
        rollupOptions: {
            output: {
                // 稳定依赖独立缓存，业务变更不使运行时和 Markdown 解析链同时失效。
                manualChunks: {
                    'react-vendor': ['react', 'react-dom', 'react-dom/client'],
                    'markdown-vendor': ['react-markdown', 'remark-gfm'],
                },
            },
        },
    },
})
