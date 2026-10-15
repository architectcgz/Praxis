import { act, renderHook } from '@testing-library/react'
import { expect, it } from 'vitest'
import type { ModelOption } from '../../api'
import { agent } from '../../test/fixtures'
import { useAgentTaskInput } from './useAgentTaskInput'

const models: ModelOption[] = ['first', 'second'].map((modelId) => ({ providerId: 'provider', modelId, label: modelId, providerName: 'Provider', defaultProviderId: 'provider', defaultModelId: 'first', reasoningLevels: ['low', 'high'], defaultReasoningLevel: 'low', assignedAgentDefinitions: modelId === 'first' ? ['primary'] : [] }))

it('内容相同的目录刷新保留模型和 reasoning，切换 Agent 时使用默认值', () => {
    const { result, rerender } = renderHook(({ id, catalog }) => useAgentTaskInput(agent(id), catalog), { initialProps: { id: 'a1', catalog: models } })
    act(() => result.current.selectModel('provider', 'second'))
    act(() => result.current.selectReasoning('high'))
    rerender({ id: 'a1', catalog: structuredClone(models) })
    expect(result.current.selectedModelID).toBe('second')
    expect(result.current.reasoning).toBe('high')
    rerender({ id: 'a2', catalog: models })
    expect(result.current.selectedModelID).toBe('first')
    expect(result.current.reasoning).toBe('low')
    rerender({ id: 'a2', catalog: [] })
    expect(result.current.selectedModelID).toBe('')
    expect(result.current.selectedProviderID).toBe('')
    expect(result.current.reasoning).toBe('')
})
