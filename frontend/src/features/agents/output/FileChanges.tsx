import type { ReactNode } from 'react'
import { ChevronDown } from 'lucide-react'
import { OutputStatus } from './OutputStatus'
import { PatchDiff } from './PatchViews'
import { ToolResultContent } from './ToolResultContent'
import { formatToolInput, patchFiles, type ToolPresentation } from './toolPresentation'

/** 展示请求中的补丁和工具实际返回结果；请求差异不代表写入成功。 */
export function FileChanges({ tool, duration }: { tool: ToolPresentation; duration?: ReactNode }) {
    const files = patchFiles(tool.input, tool.result)
    return <details className="output-fold output-files" data-status={tool.status} open={tool.status === 'running' || tool.status === 'failed' || tool.status === 'timeout' ? true : undefined}>
        <summary className="output-fold-button output-tool-header">
            <span className="output-tool-summary"><strong>{tool.name}</strong><code>{files.length ? `${files.length} 个文件` : tool.summary}</code></span>
            <OutputStatus status={tool.status} />
            {duration}
            <ChevronDown className="output-chevron" size={14} aria-hidden="true" />
        </summary>
        <div className="output-tool-body">
            <section className="output-section output-input">
                <div className="output-section-label">调用参数</div>
                {files.length ? files.map((file) => <details className="output-fold" key={file.path}>
                    <summary className="output-fold-button"><code className="output-file-path">{file.from ? `${file.from} → ${file.path}` : file.path}</code><ChevronDown className="output-chevron" size={14} aria-hidden="true" /></summary>
                    {file.diff ? <PatchDiff content={file.diff} addedFile={file.action === 'added'} /> : <p className="output-muted">请求未提供差异内容</p>}
                </details>) : <pre className="output-text">{formatToolInput(tool.input)}</pre>}
            </section>
            <section className="output-section output-result">
                <div className="output-section-label">执行输出</div>
                {tool.content ? <ToolResultContent content={tool.content} /> : <p className="output-muted">暂无输出</p>}
            </section>
        </div>
    </details>
}
