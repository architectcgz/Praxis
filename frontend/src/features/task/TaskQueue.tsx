import { memo } from 'react'
import { ChevronDown, ListOrdered } from 'lucide-react'
import type { AgentHistoryItem, QueuedTaskSnapshot } from '../../api'

/** 队列状态以服务端快照为准，正文复用已加载的用户消息，不另存一份任务内容。 */
export const TaskQueue = memo(function TaskQueue({ tasks, history }: {
    tasks: QueuedTaskSnapshot[]
    history: AgentHistoryItem[]
}) {
    const pending = tasks.filter((task) => task.status === 'pending').sort((left, right) => left.sequence - right.sequence)
    if (pending.length === 0) return null

    const prompts = new Map<string, string>()
    for (const { message } of history) {
        if (message?.role === 'user' && message.sourceTaskId) prompts.set(message.sourceTaskId, message.content)
    }

    return (
        <section className="task-queue" aria-label="待执行任务">
            <div className="task-queue-head">
                <ListOrdered size={15} strokeWidth={1.8} aria-hidden="true" />
                <strong role="status">待执行 {pending.length}</strong>
                <span>按发送顺序执行</span>
            </div>
            <ol className="task-queue-list" role="list">
                {pending.map((task, index) => {
                    const content = prompts.get(task.id) || '任务内容尚未加载，可加载更早消息查看。'
                    return (
                        <li key={task.id}>
                            <details>
                                <summary className="task-queue-summary">
                                    <span className="task-queue-position">{index + 1}</span>
                                    <span className="task-queue-preview">{content}</span>
                                    {index === 0 && <span className="task-queue-next">下一项</span>}
                                    <ChevronDown className="task-queue-chevron" size={14} aria-hidden="true" />
                                </summary>
                                <p className="task-queue-content">{content}</p>
                            </details>
                        </li>
                    )
                })}
            </ol>
        </section>
    )
})
