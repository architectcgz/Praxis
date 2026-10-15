import { useCallback, useState } from 'react'
import type { AgentSnapshot, ModelOption } from '../../api'

type ModelSelection = { agentId: string; catalogKey: string; providerId: string; modelId: string; reasoning: string }

/** 草稿由操作入口管理；模型选择仅在所属 Agent 或模型能力变化时使用默认值。 */
export function useAgentTaskInput(agent: AgentSnapshot | null, models: ModelOption[]) {
    const [input, setInput] = useState('')
    const [selection, setSelection] = useState<ModelSelection | null>(null)
    const agentId = agent?.id || ''
    const catalogKey = JSON.stringify(models.map((model) => [
        model.providerId, model.modelId, model.reasoningLevels, model.defaultReasoningLevel,
        model.defaultProviderId, model.defaultModelId, model.assignedAgentDefinitions,
    ]))
    const defaultModel = models.find((model) => model.assignedAgentDefinitions.includes(agent?.definitionId || '')) ||
        models.find((model) => model.providerId === model.defaultProviderId && model.modelId === model.defaultModelId) ||
        models.find((model) => model.providerId === model.defaultProviderId) || models[0]
    const current = selection?.agentId === agentId && selection.catalogKey === catalogKey ? selection : {
        providerId: defaultModel?.providerId || '',
        modelId: defaultModel?.modelId || '',
        reasoning: defaultModel?.reasoningLevels.length ? defaultModel.defaultReasoningLevel : '',
    }
    const selectedProviderID = current.providerId
    const selectedModelID = current.modelId
    const reasoning = current.reasoning

    const selectModel = useCallback((providerId: string, modelId: string) => {
        const model = models.find((item) => item.providerId === providerId && item.modelId === modelId)
        if (!model) return
        setSelection({ agentId, catalogKey, providerId, modelId, reasoning: model.reasoningLevels.length ? model.defaultReasoningLevel : '' })
    }, [agentId, catalogKey, models])

    const selectReasoning = useCallback((level: string) => {
        const model = models.find((item) => item.providerId === selectedProviderID && item.modelId === selectedModelID)
        if (!model?.reasoningLevels.includes(level)) return
        setSelection({ agentId, catalogKey, providerId: selectedProviderID, modelId: selectedModelID, reasoning: level })
    }, [agentId, catalogKey, models, selectedModelID, selectedProviderID])

    return { input, setInput, selectedProviderID, selectedModelID, reasoning, selectModel, selectReasoning }
}
