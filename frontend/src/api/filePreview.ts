import { getSessionBinding } from './bindings'

export type FilePreview = { path: string; content: string }

/** 读取会话工作区中的文本文件；路径授权、文件类型和大小限制均由后端执行。 */
export async function previewFile(sessionId: string, path: string): Promise<FilePreview> {
    const value: unknown = await getSessionBinding().PreviewFile(sessionId, path)
    if (!value || typeof value !== 'object' || !('path' in value) || typeof value.path !== 'string' ||
        !('content' in value) || typeof value.content !== 'string') throw new Error('文件预览数据格式无效。')
    return { path: value.path, content: value.content }
}

/** 网页链接交给系统浏览器；非桌面环境使用新标签页，始终不替换当前会话页面。 */
export function openExternalURL(url: string): void {
    if (!/^https?:\/\//i.test(url)) return
    const runtime = (window as Window & { runtime?: { BrowserOpenURL?: (url: string) => void } }).runtime
    if (runtime?.BrowserOpenURL) runtime.BrowserOpenURL(url)
    else window.open(url, '_blank', 'noopener,noreferrer')
}
