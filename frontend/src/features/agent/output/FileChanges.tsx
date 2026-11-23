import type { ReactNode } from 'react'
import { ChevronDown, FileDiff } from 'lucide-react'
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

/** 补丁调用：方案 A 账本行直出文件路径与增删行数微指标，单文件展开直接呈现精简 Diff 视图。 */
export function FileChanges({ tool, duration }: { tool: ToolPresentation; duration?: ReactNode }) {
    const files = patchFiles(tool.input, tool.result)
    const changes = files.reduce(
        (total, file) => ({ added: total.added + file.added, removed: total.removed + file.removed }),
        { added: 0, removed: 0 },
    )
    const argument = files.length === 1
        ? (files[0].from ? `${files[0].from} -> ${files[0].path}` : files[0].path)
        : files.length > 1
            ? `${files[0].path} 等 ${files.length} 个文件`
            : tool.summary
    const awaitingApproval = tool.status === 'awaiting_approval'
    const hasResultFiles = Array.isArray(tool.result.files)
    const showOutputSection = !awaitingApproval && (
        tool.status === 'failed'
        || typeof tool.result.error === 'string'
        || typeof tool.result.message === 'string'
        || (!hasResultFiles && Boolean(tool.content))
    )

    const metrics = (changes.added > 0 || changes.removed > 0 || files.length > 1) ? (
        <>
            {files.length > 1 && <span className="act-metric-pill">{files.length} 文件</span>}
            {changes.added > 0 && <span className="act-metric-pill is-add">+{changes.added}</span>}
            {changes.removed > 0 && <span className="act-metric-pill is-del">-{changes.removed}</span>}
        </>
    ) : undefined

    const singleFile = files.length === 1 ? files[0] : null

    return (
        <ActivityItem
            tone={activityTone(tool.status)}
            icon={<FileDiff size={13} strokeWidth={1.85} aria-hidden="true" />}
            name={tool.label || '补丁'}
            rawName={tool.name || 'apply_patch'}
            argument={argument}
            metrics={metrics}
            status={tool.status}
            duration={duration}
            open={isOpenByDefault(tool.status)}
            foot={files.length > 0 ? (
                <>
                    <span>
                        {awaitingApproval
                            ? `待批准 ${files.length} 个文件变更`
                            : hasResultFiles
                                ? `已应用 ${files.length} 个文件变更`
                                : `${files.length} 个目标文件`}
                    </span>
                    {(changes.added > 0 || changes.removed > 0) && <span>+{changes.added} / -{changes.removed}</span>}
                </>
            ) : undefined}
        >
            {singleFile ? (
                <section className="output-section output-patch-single">
                    <div className="output-patch-file-bar">
                        <span className="output-file-action" data-action={singleFile.action}>
                            {actionLabels[singleFile.action] || singleFile.action}
                        </span>
                        <code className="output-file-path">
                            {singleFile.from ? `${singleFile.from} -> ${singleFile.path}` : singleFile.path}
                        </code>
                        {(singleFile.added > 0 || singleFile.removed > 0) && (
                            <span className="output-file-diffstat">
                                {singleFile.added > 0 && <span className="is-add">+{singleFile.added}</span>}
                                {singleFile.removed > 0 && <span className="is-del">-{singleFile.removed}</span>}
                            </span>
                        )}
                    </div>
                    {singleFile.diff ? (
                        <PatchDiff content={singleFile.diff} addedFile={singleFile.action === 'added'} />
                    ) : (
                        <p className="output-muted output-patch-empty">请求未提供差异内容</p>
                    )}
                </section>
            ) : (
                <section className="output-section output-input">
                    {files.length > 0 ? files.map((file) => (
                        <details className="output-fold" key={file.path}>
                            <summary className="output-fold-button">
                                <span className="output-file-action" data-action={file.action}>
                                    {actionLabels[file.action] || file.action}
                                </span>
                                <code className="output-file-path">
                                    {file.from ? `${file.from} -> ${file.path}` : file.path}
                                </code>
                                {(file.added > 0 || file.removed > 0) && (
                                    <span className="output-file-diffstat">
                                        {file.added > 0 && <span className="is-add">+{file.added}</span>}
                                        {file.removed > 0 && <span className="is-del">-{file.removed}</span>}
                                    </span>
                                )}
                                <ChevronDown className="output-chevron" size={14} aria-hidden="true" />
                            </summary>
                            {file.diff ? (
                                <PatchDiff content={file.diff} addedFile={file.action === 'added'} />
                            ) : (
                                <p className="output-muted">请求未提供差异内容</p>
                            )}
                        </details>
                    )) : <pre className="output-text">{formatToolInput(tool.input)}</pre>}
                </section>
            )}
            {showOutputSection && (
                <section className="output-section output-result">
                    <div className="output-section-label">输出</div>
                    {tool.content ? <ToolResultContent content={tool.content} /> : <p className="output-muted">暂无输出</p>}
                </section>
            )}
        </ActivityItem>
    )
}
