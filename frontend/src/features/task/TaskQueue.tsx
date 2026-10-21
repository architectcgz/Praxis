import { memo, useState } from 'react'
import { ChevronDown, ListOrdered } from 'lucide-react'
import type { QueuedTask } from '../../api'

/** 待办正文来自内存队列快照，不依赖尚未写入消息流的历史。 */
export const TaskQueue = memo(function TaskQueue({ tasks, busy, onCancel }: {
    tasks: QueuedTask[]
    busy: boolean
    onCancel: (taskID: string) => Promise<void>
}) {
    const [confirmingID, setConfirmingID] = useState('')
    const pending = [...tasks].sort((left, right) => left.sequence - right.sequence)
    if (pending.length === 0) return null

    return (
        <section className="task-queue" aria-label="待执行任务">
            <div className="task-queue-head">
                <ListOrdered size={15} strokeWidth={1.8} aria-hidden="true" />
                <strong role="status">待执行 {pending.length}</strong>
                <span>按发送顺序执行 · 重启后清空</span>
            </div>
            <ol className="task-queue-list" role="list">
                {pending.map((task, index) => (
                    <li key={task.id}>
                        <details>
                            <summary className="task-queue-summary">
                                <span className="task-queue-position">{index + 1}</span>
                                <span className="task-queue-preview">{task.prompt}</span>
                                {index === 0 && <span className="task-queue-next">下一项</span>}
                                <ChevronDown className="task-queue-chevron" size={14} aria-hidden="true" />
                            </summary>
                            <p className="task-queue-content">{task.prompt}</p>
                            <div className="task-queue-actions">
                                {confirmingID === task.id ? <>
                                    <span>移出队列？</span>
                                    <button type="button" className="task-queue-cancel" disabled={busy} onClick={() => {
                                        void onCancel(task.id).then(() => setConfirmingID(''))
                                    }}>确认取消</button>
                                    <button type="button" disabled={busy} onClick={() => setConfirmingID('')}>保留</button>
                                </> : <button type="button" className="task-queue-cancel" disabled={busy} onClick={() => setConfirmingID(task.id)}>取消任务</button>}
                            </div>
                        </details>
                    </li>
                ))}
            </ol>
        </section>
    )
})
