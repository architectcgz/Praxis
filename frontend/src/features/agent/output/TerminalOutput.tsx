import { useState } from 'react'
import type { ReactNode } from 'react'
import { isDiffContent } from './MarkdownViews'
import { PatchCommand } from './PatchViews'
import { ActivityItem, activityTone, isOpenByDefault } from './ActivityLedger'
import type { ToolPresentation } from './toolPresentation'

/** 单行且不超过这个长度的命令在账本行里已完整可见，展开区不再重复展示。 */
const inlineCommandLimit = 64

/** Bash 调用：折叠时是活动账本的一行，展开后是仪器域的完整命令与输出。 */
export function TerminalOutput({ tool, duration }: { tool: ToolPresentation; duration?: ReactNode }) {
    const [expanded, setExpanded] = useState(false)
    const lines = tool.content.trim() ? tool.content.replace(/\r\n/g, '\n').replace(/\n$/, '').split('\n') : []
    const shown = expanded ? lines : lines.slice(0, 6)
    const running = tool.status === 'running'
    const commandPreview = tool.summary.split(/\r?\n/)[0]
    const awaitingApproval = tool.status === 'awaiting_approval'
    const singleLine = !/[\r\n]/.test(tool.summary)
    const showCommand = awaitingApproval || !singleLine || tool.summary.length > inlineCommandLimit
    const workdir = typeof tool.input.path === 'string' ? tool.input.path : ''
    return <ActivityItem
        tone={activityTone(tool.status)}
        name={tool.name || 'bash'}
        argument={commandPreview}
        status={tool.status}
        statusLabel={tool.notice.startsWith('搜索无结果') ? '无匹配' : undefined}
        duration={duration}
        open={isOpenByDefault(tool.status)}
        foot={<>
            {lines.length > 0 && <span>{lines.length} 行输出</span>}
            {tool.exitCode !== undefined && <span>退出码 {tool.exitCode}</span>}
            {workdir && <span>工作目录 {workdir}</span>}
        </>}
    >
        {showCommand && <section className="output-section">
            {/* 命令由 $ 提示符自证身份，不需要文字标签；
                只有待批准这种「还没执行」的状态才需要写明。 */}
            {awaitingApproval && <div className="output-section-label">待执行命令</div>}
            <pre className="output-command" tabIndex={0} aria-label={awaitingApproval ? '待执行命令' : '完整命令'}>
                <span className="output-command-prompt" aria-hidden="true">$</span>
                {isDiffContent(tool.summary) ? <PatchCommand content={tool.summary} /> : tool.summary}
            </pre>
        </section>}
        {!awaitingApproval && <section className="output-section">
            <div className="output-section-label">输出</div>
            <div className="output-terminal-scroll" tabIndex={0} aria-label="命令输出">
                {shown.length > 0 ? shown.map((line, index) => <div className="output-terminal-line" key={index}>{line || ' '}</div>)
                    : running ? <p className="output-muted">等待工具返回输出…</p> : <p className="output-muted">暂无输出</p>}
            </div>
            {lines.length > shown.length && <div className="output-muted">显示 {shown.length} / {lines.length} 行 · 更多输出已折叠</div>}
            {lines.length > 6 && <div className="output-section-actions">
                <button type="button" className="output-button" aria-expanded={expanded} onClick={() => setExpanded((value) => !value)}>{expanded ? '收起日志' : '展开全部'}</button>
            </div>}
        </section>}
    </ActivityItem>
}
