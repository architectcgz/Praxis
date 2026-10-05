export function PatchDiff({ content, addedFile }: { content: string; addedFile: boolean }) {
    let oldLine: number | undefined
    let newLine: number | undefined = addedFile ? 1 : undefined
    return <div className="output-diff" tabIndex={0} aria-label="代码差异，支持横向滚动">
        {content.split('\n').map((line, index) => {
            if (line.startsWith('@@')) {
                const match = line.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)/)
                // 没有行号的补丁只展示上下文，避免把推测位置显示为真实行号。
                oldLine = match ? Number(match[1]) : undefined
                newLine = match ? Number(match[2]) : undefined
                return <div className="output-diff-hunk" key={index}>{line}</div>
            }
            const kind = line.startsWith('+') ? 'add' : line.startsWith('-') ? 'del' : 'context'
            const before = kind === 'add' ? '' : oldLine
            const after = kind === 'del' ? '' : newLine
            if (kind !== 'add' && oldLine !== undefined) oldLine++
            if (kind !== 'del' && newLine !== undefined) newLine++
            return <div className="output-diff-row" data-kind={kind} key={index}>
                <span className="output-diff-number" aria-hidden="true">{before}</span><span className="output-diff-number" aria-hidden="true">{after}</span>
                <span className="output-diff-sign" aria-hidden="true">{kind === 'context' ? ' ' : line[0]}</span><code>{line.slice(1) || ' '}</code>
            </div>
        })}
    </div>
}

export function PatchCommand({ content }: { content: string }) {
    return content.split('\n').map((line, index) => {
        const kind = line.startsWith('+') && !line.startsWith('+++') ? 'add'
            : line.startsWith('-') && !line.startsWith('---') ? 'del'
                : line.startsWith('@@') ? 'hunk' : 'context'
        return <span className="output-terminal-command-line" data-kind={kind} key={`${index}-${line}`}>{line || ' '}</span>
    })
}
