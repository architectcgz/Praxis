import type { ReactNode } from 'react'
import { ChevronDown } from 'lucide-react'
import { PatchDiff } from './PatchViews'
import { ToolResultContent } from './ToolResultContent'
import { ActivityItem, activityTone, isOpenByDefault } from './ActivityLedger'
import { formatToolInput, patchFiles, type ToolPresentation } from './toolPresentation'

/** 补丁调用：展示请求中的差异和工具实际返回结果；请求差异不代表写入成功。 */
export function FileChanges({ tool, duration }: { tool: ToolPresentation; duration?: ReactNode }) {
    const files = patchFiles(tool.input, tool.result)
    const changes = files.reduce((total, file) => ({ added: total.added + file.added, removed: total.removed + file.removed }), { added: 0, removed: 0 })
    const summary = files.length
        ? `${files.length} 个文件${changes.added || changes.removed ? ` · +${changes.added} −${changes.removed}` : ''}`
        : tool.summary
    const awaitingApproval = tool.status === 'awaiting_approval'
    return <ActivityItem
        tone={activityTone(tool.status)}
        name={tool.name}
        argument={summary}
        status={tool.status}
        duration={duration}
        open={isOpenByDefault(tool.status)}
    >
        <section className="output-section output-input">
            <div className="output-section-label">参数</div>
            {files.length ? files.map((file) => <details className="output-fold" key={file.path}>
                <summary className="output-fold-button"><code className="output-file-path">{file.from ? `${file.from} → ${file.path}` : file.path}</code><ChevronDown className="output-chevron" size={14} aria-hidden="true" /></summary>
                {file.diff ? <PatchDiff content={file.diff} addedFile={file.action === 'added'} /> : <p className="output-muted">请求未提供差异内容</p>}
            </details>) : <pre className="output-text">{formatToolInput(tool.input)}</pre>}
        </section>
        {!awaitingApproval && <section className="output-section output-result">
            <div className="output-section-label">输出</div>
            {tool.content ? <ToolResultContent content={tool.content} /> : <p className="output-muted">暂无输出</p>}
        </section>}
    </ActivityItem>
}
