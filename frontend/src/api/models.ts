import { getModelBinding } from './bindings'
import { dto } from '../../wailsjs/go/models'

export type ModelOption = {
    providerId: string
    modelId: string
    defaultProviderId: string
    defaultModelId: string
    label: string
    providerName: string
    reasoningLevels: string[]
    defaultReasoningLevel: string
    assignedAgentDefinitions: string[]
}

export type ProviderConfigOption = {
    id: string
    providerName: string
    baseURL: string
    proxyURL: string
    defaultModelId: string
    hasAPIKey: boolean
}

export type GroupConfigOption = {
    id: string
    displayName: string
}

export type ModelAPIFormat = 'anthropic_messages' | 'openai_responses' | 'openai_chat_completions'

export type ModelConfigOption = {
    providerId: string
    modelId: string
    label: string
    groupId: string
    apiFormat: ModelAPIFormat
    contextWindow: number
    maxOutputTokens: number
    reasoningLevels: string[]
    defaultReasoningLevel: string
}

export type ModelConfigDocument = {
    groups: GroupConfigOption[]
    defaultProviderId: string
    providers: ProviderConfigOption[]
    models: ModelConfigOption[]
}

export function listModels() {
    return getModelBinding().ListModels().then(normalizeModelCatalog)
}

export function reloadConfig() {
    return getModelBinding().ReloadConfig()
}

export function getModelConfig() {
    return getModelBinding().GetModelConfig().then(normalizeModelConfig)
}

export function saveModelConfig(config: ModelConfigDocument) {
    return getModelBinding().SaveModelConfig(new dto.SaveModelConfigRequest({
        groups: config.groups,
        defaultProviderId: config.defaultProviderId,
        providers: config.providers,
        models: config.models,
    }))
}

export function listProviderModels(providerID: string) {
    return getModelBinding().ListProviderModels(providerID).then((models) => {
        if (!Array.isArray(models) || !models.every((model) => typeof model === 'string')) {
            throw new Error('Provider Model 列表响应无效。')
        }
        return models
    })
}

export function setProviderKey(providerID: string, value: string) {
    return getModelBinding().SetProviderKey(providerID, value)
}

function normalizeModelCatalog(value: unknown): ModelOption[] {
    if (!Array.isArray(value)) {
        throw new Error('Model 列表响应无效。')
    }
    return value.map((model) => {
        if (!isRecord(model)) {
            throw new Error('Model 列表响应无效。')
        }
        return {
            providerId: stringValue(model.providerId),
            modelId: stringValue(model.modelId),
            defaultProviderId: stringValue(model.defaultProviderId),
            defaultModelId: model.defaultModelId == null ? '' : stringValue(model.defaultModelId),
            label: stringValue(model.label),
            providerName: stringValue(model.providerName),
            reasoningLevels: model.reasoningLevels == null ? [] : stringArray(model.reasoningLevels),
            defaultReasoningLevel: model.defaultReasoningLevel == null ? '' : stringValue(model.defaultReasoningLevel),
            assignedAgentDefinitions: stringArray(model.assignedAgentDefinitions),
        }
    })
}

function normalizeModelConfig(value: unknown): ModelConfigDocument {
    if (!isRecord(value) || !Array.isArray(value.providers) || !Array.isArray(value.models)) {
        throw new Error('Model 配置响应无效。')
    }
    return {
        groups: !Array.isArray(value.groups) ? [] : value.groups.map((group: unknown) => {
            if (!isRecord(group)) throw new Error('Model 配置响应无效。')
            return { id: stringValue(group.id), displayName: stringValue(group.displayName) }
        }),
        defaultProviderId: stringValue(value.defaultProviderId),
        providers: value.providers.map((provider) => {
            if (!isRecord(provider)) throw new Error('Model 配置响应无效。')
            return {
                id: stringValue(provider.id),
                providerName: stringValue(provider.providerName),
                baseURL: stringValue(provider.baseURL),
                proxyURL: stringValue(provider.proxyURL),
                defaultModelId: provider.defaultModelId == null ? '' : stringValue(provider.defaultModelId),
                hasAPIKey: provider.hasAPIKey === true,
            }
        }),
        models: value.models.map((model) => {
            if (!isRecord(model) ||
                typeof model.contextWindow !== 'number' || typeof model.maxOutputTokens !== 'number') {
                throw new Error('Model 配置响应无效。')
            }
            const apiFormat = stringValue(model.apiFormat)
            if (apiFormat !== 'anthropic_messages' && apiFormat !== 'openai_responses' &&
                apiFormat !== 'openai_chat_completions') {
                throw new Error('Model 配置响应无效。')
            }
            return {
                providerId: stringValue(model.providerId),
                modelId: stringValue(model.modelId),
                label: stringValue(model.label),
                groupId: stringValue(model.groupId),
                apiFormat,
                contextWindow: model.contextWindow,
                maxOutputTokens: model.maxOutputTokens,
                reasoningLevels: model.reasoningLevels == null ? [] : stringArray(model.reasoningLevels),
                defaultReasoningLevel: model.defaultReasoningLevel == null ? '' : stringValue(model.defaultReasoningLevel),
            }
        }),
    }
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null
}

function stringValue(value: unknown): string {
    if (typeof value !== 'string') {
        throw new Error('Model 列表响应无效。')
    }
    return value
}

function stringArray(value: unknown): string[] {
    if (!Array.isArray(value) || !value.every((item) => typeof item === 'string')) {
        throw new Error('Model 列表响应无效。')
    }
    return value
}
