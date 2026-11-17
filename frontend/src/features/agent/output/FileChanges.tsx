import type { ReactNode } from 'react'
import { ChevronDown } from 'lucide-react'
import { PatchDiff } from './PatchViews'
import { ToolResultContent } from './ToolResultContent'
import { ActivityItem, activityTone, isOpenByDefault } from './ActivityLedger'
import { formatToolInput, patchFiles, type ToolPresentation } from './toolPresentation'

const actionLabels: Record<string, string> = {
    added: '新增',
    updated: '修改',
    deleted: '删除',
    moved: '移动',
}

/** 补丁调用：展示请求中的差异和工具实际返回结果；请求差异不代表写入成功。 */
export function FileChanges({ tool, duration }: { tool: ToolPresentation; duration?: ReactNode }) {
    const files = patchFiles(tool.input, tool.result)
    const changes = files.reduce((total, file) => ({ added: total.added + file.added, removed: total.removed + file.removed }), { added: 0, removed: 0 })
    const summary = files.length
        ? `${files.length} 个文件${changes.added || changes.removed ? ` · +${changes.added} −${changes.removed}` : ''}`
        : tool.summary
    const awaitingApproval = tool.status === 'awaiting_approval'
    const hasResultFiles = Array.isArray(tool.result.files)
    const showOutputSection = !awaitingApproval && (
        tool.status === 'failed'
        || typeof tool.result.error === 'string'
        || typeof tool.result.message === 'string'
        || (!hasResultFiles && Boolean(tool.content))
    )
    return <ActivityItem
        tone={activityTone(tool.status)}
        name={tool.name}
        argument={summary}
        status={tool.status}
        duration={duration}
        open={isOpenByDefault(tool.status)}
        foot={files.length > 0 ? <>
            <span>{awaitingApproval ? `待批准 ${files.length} 个文件变更` : hasResultFiles ? `已应用 ${files.length} 个文件变更` : `${files.length} 个目标文件`}</span>
            {(changes.added > 0 || changes.removed > 0) && <span>+{changes.added} −{changes.removed}</span>}
        </> : undefined}
    >
        <section className="output-section output-input">
            <div className="output-section-label">补丁变更</div>
            {files.length ? files.map((file) => <details className="output-fold" key={file.path} open={files.length === 1 || undefined}>
                <summary className="output-fold-button">
                    <span className="output-file-action" data-action={file.action}>{actionLabels[file.action] || file.action}</span>
                    <code className="output-file-path">{file.from ? `${file.from} → ${file.path}` : file.path}</code>
                    {(file.added > 0 || file.removed > 0) && (
                        <span className="output-file-diffstat">
                            {file.added > 0 && <span className="is-add">+{file.added}</span>}
                            {file.removed > 0 && <span className="is-del">-{file.removed}</span>}
                        </span>
                    )}
                    <ChevronDown className="output-chevron" size={14} aria-hidden="true" />
                </summary>
                {file.diff ? <PatchDiff content={file.diff} addedFile={file.action === 'added'} /> : <p className="output-muted">请求未提供差异内容</p>}
            </details>) : <pre className="output-text">{formatToolInput(tool.input)}</pre>}
        </section>
        {showOutputSection && <section className="output-section output-result">
            <div className="output-section-label">输出</div>
            {tool.content ? <ToolResultContent content={tool.content} /> : <p className="output-muted">暂无输出</p>}
        </section>}
    </ActivityItem>
}
