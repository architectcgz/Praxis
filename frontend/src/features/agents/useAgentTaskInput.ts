import { useCallback, useEffect, useState } from 'react'
import type { AgentSnapshot, ModelOption } from '../../api'

/**
 * 管理 Agent 任务草稿与执行选项；没有模型时清空选项，非法选择不改变状态。
 * Agent 或模型目录变化时应用默认配置；草稿由调用方在切换视图、清理或发送成功时重置。
 */
export function useAgentTaskInput(agent: AgentSnapshot | null, models: ModelOption[]) {
    const [input, setInput] = useState('')
    const [selectedProviderID, setSelectedProviderID] = useState('')
    const [selectedModelID, setSelectedModelID] = useState('')
    const [reasoning, setReasoning] = useState('')

    // 查询可能返回内容相同的新数组，只有实际选项变化才重置用户的模型选择。
    const modelCatalogKey = models.map((model) => [
        model.providerId,
        model.modelId,
        model.reasoningLevels.join(','),
        model.defaultReasoningLevel,
        model.defaultProviderId,
        model.defaultModelId,
        model.assignedAgentDefinitions.join(','),
    ].join(':')).join('|')

    useEffect(() => {
        const defaultModel = models.find((model) => model.assignedAgentDefinitions.includes(agent?.definitionId || '')) ||
            models.find((model) => model.providerId === model.defaultProviderId && model.modelId === model.defaultModelId) ||
            models.find((model) => model.providerId === model.defaultProviderId) ||
            models[0]
        if (!defaultModel) {
            setSelectedProviderID('')
            setSelectedModelID('')
            setReasoning('')
            return
        }
        setSelectedProviderID(defaultModel.providerId)
        setSelectedModelID(defaultModel.modelId)
        setReasoning(defaultModel.reasoningLevels.length > 0 ? defaultModel.defaultReasoningLevel : '')
    }, [agent?.id, modelCatalogKey])

    const selectModel = useCallback((providerId: string, modelId: string) => {
        const model = models.find((item) => item.providerId === providerId && item.modelId === modelId)
        if (!model) {
            return
        }
        setSelectedProviderID(model.providerId)
        setSelectedModelID(model.modelId)
        setReasoning(model.reasoningLevels.length > 0 ? model.defaultReasoningLevel : '')
    }, [models])

    const selectReasoning = useCallback((level: string) => {
        const model = models.find((item) => item.providerId === selectedProviderID && item.modelId === selectedModelID)
        if (!model?.reasoningLevels.includes(level)) {
            return
        }
        setReasoning(level)
    }, [models, selectedModelID, selectedProviderID])

    return { input, setInput, selectedProviderID, selectedModelID, reasoning, selectModel, selectReasoning }
}
