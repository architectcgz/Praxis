import { render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { agent, message } from '../../test/fixtures'
import { AgentConversation } from './AgentConversation'
import { mergeStreamingEvent } from './streaming'

vi.mock('./AgentTaskInput', () => ({ AgentTaskInput: () => null }))
vi.mock('./ToolApprovals', () => ({ ToolApprovals: () => null }))

it('运行中刷新后正文只出现一次，尚未落盘的工具结果仍展示在原调用卡片', () => {
    const history = [message(1, { content: 'hello', blocks: [{ kind: 'text', text: 'hello' }, { kind: 'tool_call', callId: 'call-1', name: 'read_file', input: { path: 'README.md' } }] })]
    let outputs = mergeStreamingEvent({}, { kind: 'text_delta', agentId: 'a1', taskId: 't1', turnId: 'turn-1', text: 'hello' })
    outputs = mergeStreamingEvent(outputs, { kind: 'tool_result', agentId: 'a1', taskId: 't1', turnId: 'turn-1', callId: 'call-1', name: 'read_file', result: 'fresh tool output' })
    render(<AgentConversation agent={agent('a1', 's1', { state: 'executing', currentTaskId: 't1' })} history={history}
        streamingOutput={outputs.a1} pendingUserMessages={[]} awaitingOutput={false} input="" setInput={() => {}} models={[]}
        historyCursor="" historyLoadingOlder={false} historyError="" onLoadOlderHistory={() => {}}
        selectedProviderID="" selectedModelID="" reasoning="" busy={false} onSend={() => {}} onControl={() => {}}
        onModelChange={() => {}} onReasoningChange={() => {}} commands={[]} onCommand={async () => true} />)
    expect(screen.getAllByText('hello')).toHaveLength(1)
    expect(screen.getAllByText('fresh tool output')).toHaveLength(1)
    expect(screen.getAllByText('read_file')).toHaveLength(1)
})
