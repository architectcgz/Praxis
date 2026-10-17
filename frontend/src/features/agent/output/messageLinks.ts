export type MessageLink = { kind: 'file'; path: string } | { kind: 'external'; url: string } | { kind: 'blocked' }

/** 解析 Markdown 链接：文件只交给预览，网页只交给外部浏览器；不允许脚本和自定义协议。 */
export function parseMessageLink(href: string, basePath = ''): MessageLink {
    const value = href.trim()
    if (!value || value.startsWith('#') || /[\u0000-\u001f\u007f]/.test(value)) return { kind: 'blocked' }
    try {
        if (/^https?:\/\//i.test(value) || value.startsWith('//')) {
            const url = new URL(value.startsWith('//') ? `https:${value}` : value)
            return { kind: 'external', url: url.href }
        }
        let path: string
        if (/^file:/i.test(value)) {
            const url = new URL(value)
            if (url.hostname && url.hostname !== 'localhost') return { kind: 'blocked' }
            path = decodeURIComponent(url.pathname).replace(/^\/([a-z]:\/)/i, '$1')
        } else {
            path = decodeURIComponent(value.split(/[?#]/, 1)[0])
            if (/^[a-z][a-z\d+.-]*:/i.test(path) && !/^[a-z]:[\\/]/i.test(path)) return { kind: 'blocked' }
        }
        if (!path || /[\u0000-\u001f\u007f]/.test(path) || /^[\\/]{2}/.test(path)) return { kind: 'blocked' }
        path = path.replace(/\\/g, '/')
        if (basePath && !path.startsWith('/') && !/^[a-z]:\//i.test(path)) {
            path = basePath.slice(0, basePath.lastIndexOf('/') + 1) + path
        }
        return { kind: 'file', path }
    } catch {
        return { kind: 'blocked' }
    }
}
