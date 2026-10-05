export type ToolStatus = 'queued' | 'running' | 'completed' | 'failed' | 'timeout' | 'cancelled' | 'retrying' | 'blocked' | 'info' | 'unknown'

export type ToolPresentation = {
    name: string
    label: string
    summary: string
    input: Record<string, unknown>
    result: Record<string, unknown>
    content: string
    status: ToolStatus
    notice: string
    truncated: boolean
    exitCode?: number
    nextOffset?: number
}

const toolLabels: Record<string, string> = {
    bash: '运行',
    run_command: '运行',
    read_file: '读取',
    search_text: '搜索',
    write_file: '写入',
    apply_patch: '补丁',
}

/** 读取工具的 JSON 对象；普通文本、数组和无效 JSON 保留给正文展示。 */
export function toolRecord(value: unknown): Record<string, unknown> {
    if (typeof value === 'string') {
        try {
            value = JSON.parse(value)
        } catch {
            return {}
        }
    }
    return typeof value === 'object' && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

/** 格式化原始工具参数，无法序列化时保留可读文本。 */
export function formatToolInput(input: unknown): string {
    if (typeof input === 'string') return input
    try {
        return JSON.stringify(input ?? {}, null, 2) || '{}'
    } catch {
        return String(input)
    }
}

/** 按真实结果生成状态和必要提示；错误详情保留在正文，没有结果时使用调用方状态。 */
export function presentTool(name: string, inputValue: unknown, resultText?: string, isError = false, pendingStatus: ToolStatus = 'running'): ToolPresentation {
    const input = toolRecord(inputValue)
    const result = toolRecord(resultText)
    const label = toolLabels[name] || name || '工具'
    const summary = stringValue(input.command) || stringValue(input.path) || stringValue(input.pattern) || name || '工具'
    const content = typeof result.output === 'string' ? result.output
        : typeof result.content === 'string' ? result.content
        : typeof result.message === 'string' ? result.message
        : Object.keys(result).length > 0 ? JSON.stringify(result, null, 2) : resultText || ''
    const exitCode = typeof result.exitCode === 'number' ? result.exitCode : undefined
    const nextOffset = typeof result.nextOffset === 'number' ? result.nextOffset : undefined
    const truncated = result.truncated === true || nextOffset !== undefined
    let status: ToolStatus = resultText === undefined ? pendingStatus : isError || result.error ? 'failed' : 'completed'
    let notice = ''
    const errorText = `${stringValue(result.error)} ${content}`
    if (result.timedOut === true || (status === 'failed' && /ETIMEDOUT|deadline exceeded|timed?\s*out|超时/i.test(errorText))) {
        status = 'timeout'
        notice = '执行超过等待上限，已返回的输出保留在下方。'
    } else if (status === 'failed' && /cancelled|canceled|context canceled|已取消/i.test(errorText)) {
        status = 'cancelled'
        notice = '执行已取消，已返回的输出保留在下方。'
    } else if (exitCode !== undefined && exitCode !== 0) {
        if (exitCode === 1 && !content.trim() && /^\s*(?:rg|grep)\b/.test(stringValue(input.command)) && !isError && !result.error) {
            status = 'info'
            notice = '搜索无结果 · 没有找到匹配项。'
        } else {
            status = 'failed'
            notice = '命令执行失败 · 详情见下方输出。'
        }
    } else if (status === 'completed' && !content.trim()) {
        notice = name === 'read_file' ? '文件为空 · 已读取的范围没有文本内容。' : ''
    } else if (status === 'completed' && Array.isArray(result.entries) && result.entries.length === 0) {
        notice = '目录为空 · 当前范围没有条目。'
    }
    return { name, label, summary, input, result, content, status, notice, truncated, exitCode, nextOffset }
}

export type PatchFile = {
    path: string
    from?: string
    action: string
    diff: string
    added: number
    removed: number
}

/** 从 apply_patch 请求提取文件和差异；不把请求内容当作已成功写入的结果。 */
export function parsePatchFiles(patch: string): PatchFile[] {
    const files: PatchFile[] = []
    let current: PatchFile | undefined
    for (const line of patch.replace(/\r\n/g, '\n').split('\n')) {
        const header = line.match(/^\*\*\* (Add|Update|Delete) File: (.+)$/)
        if (header) {
            current = { path: header[2], action: { Add: 'added', Update: 'updated', Delete: 'deleted' }[header[1]] || 'updated', diff: '', added: 0, removed: 0 }
            files.push(current)
        } else if (current && line.startsWith('*** Move to: ')) {
            current.from = current.path
            current.path = line.slice('*** Move to: '.length)
            current.action = 'moved'
        } else if (current && !line.startsWith('*** ')) {
            current.diff += `${current.diff ? '\n' : ''}${line}`
            if (line.startsWith('+')) current.added++
            if (line.startsWith('-')) current.removed++
        }
    }
    return files
}

/** 合并补丁请求与已返回的文件清单，保留结果中的实际操作和路径。 */
export function patchFiles(input: Record<string, unknown>, result: Record<string, unknown>): PatchFile[] {
    const requested = parsePatchFiles(stringValue(input.patch))
    if (!Array.isArray(result.files)) return requested
    return result.files.flatMap((value) => {
        const file = toolRecord(value)
        if (typeof file.path !== 'string') return []
        const request = requested.find((item) => item.path === file.path || item.path === file.from)
        return [{
            path: file.path,
            from: stringValue(file.from) || request?.from,
            action: stringValue(file.action) || request?.action || 'updated',
            diff: request?.diff || '',
            added: request?.added || 0,
            removed: request?.removed || 0,
        }]
    })
}

function stringValue(value: unknown): string {
    return typeof value === 'string' ? value : ''
}
