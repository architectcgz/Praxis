import { type KeyboardEvent } from 'react'
import { Plus, Send } from 'lucide-react'

type PrimaryAgentTaskInputProps = {
    input: string
    setInput: (value: string) => void
    busy: boolean
    onSend: () => void
}

export function PrimaryAgentTaskInput({ input, setInput, busy, onSend }: PrimaryAgentTaskInputProps) {
    const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
        if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
            event.preventDefault()
            if (input.trim() && !busy) {
                onSend()
            }
        }
    }

    return (
        <div className="agent-conversation">
            <div className="message-thread empty" aria-hidden="true" />
            <div className="command-band">
                <div className="composer-shell">
                    <button className="composer-icon-button" type="button" title="聚焦执行输入" aria-label="聚焦执行输入" onClick={() => document.getElementById('agent-input')?.focus()} disabled={busy}>
                        <Plus size={18} strokeWidth={1.9} aria-hidden="true" />
                    </button>
                    <textarea className="composer-textarea" id="agent-input" aria-label="执行输入" value={input} onChange={(event) => setInput(event.target.value)} onKeyDown={handleKeyDown} placeholder="为主代理编写新任务" disabled={busy} rows={1} />
                    <div className="composer-tools">
                        <button className="composer-submit" type="button" title="发送输入" aria-label="发送输入" onClick={onSend} disabled={busy || !input.trim()}>
                            <Send size={16} strokeWidth={2.1} />
                        </button>
                    </div>
                </div>
            </div>
        </div>
    )
}
