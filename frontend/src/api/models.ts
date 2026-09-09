import { getModelBinding } from './bindings'

export type ModelOption = {
    providerId: string
    modelId: string
    label: string
    providerName: string
    reasoningLevels: string[]
    defaultReasoningLevel: string
    assignedAgents: string[]
}

export type ProviderConfigOption = {
    id: string
    providerName: string
    baseURL: string
    proxyURL: string
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
    providers: ProviderConfigOption[]
    models: ModelConfigOption[]
}

export type SaveModelConfigResponse = {
    saved: boolean
    validationError?: string
}

export function listModels() {
    return getModelBinding().ListModels().then(normalizeModelCatalog)
}

export function getModelConfig() {
    return getModelBinding().GetModelConfig().then(normalizeModelConfig)
}

export function saveModelConfig(config: ModelConfigDocument) {
    return getModelBinding().SaveModelConfig({
        groups: config.groups,
        providers: config.providers,
        models: config.models,
    })
}

export function listProviderModels(providerID: string) {
    return getModelBinding().ListProviderModels(providerID).then((models) => {
        if (!Array.isArray(models) || !models.every((model) => typeof model === 'string')) {
            throw new Error('The provider model catalog response is invalid.')
        }
        return models
    })
}

export function setProviderKey(providerID: string, value: string) {
    return getModelBinding().SetProviderKey(providerID, value)
}

export function clearProviderKey(providerID: string) {
    return getModelBinding().ClearProviderKey(providerID)
}

function normalizeModelCatalog(value: unknown): ModelOption[] {
    if (!Array.isArray(value)) {
        throw new Error('The model catalog response is invalid.')
    }
    return value.map((model) => {
        if (!isRecord(model)) {
            throw new Error('The model catalog response is invalid.')
        }
        return {
            providerId: stringValue(model.providerId),
            modelId: stringValue(model.modelId),
            label: stringValue(model.label),
            providerName: stringValue(model.providerName),
            reasoningLevels: model.reasoningLevels == null ? [] : stringArray(model.reasoningLevels),
            defaultReasoningLevel: model.defaultReasoningLevel == null ? '' : stringValue(model.defaultReasoningLevel),
            assignedAgents: stringArray(model.assignedAgents),
        }
    })
}

function normalizeModelConfig(value: unknown): ModelConfigDocument {
    if (!isRecord(value) || !Array.isArray(value.providers) || !Array.isArray(value.models)) {
        throw new Error('The model configuration response is invalid.')
    }
    return {
        groups: !Array.isArray(value.groups) ? [] : value.groups.map((group: unknown) => {
            if (!isRecord(group)) throw new Error('The model configuration response is invalid.')
            return { id: stringValue(group.id), displayName: stringValue(group.displayName) }
        }),
        providers: value.providers.map((provider) => {
            if (!isRecord(provider)) throw new Error('The model configuration response is invalid.')
            return {
                id: stringValue(provider.id),
                providerName: stringValue(provider.providerName),
                baseURL: stringValue(provider.baseURL),
                proxyURL: stringValue(provider.proxyURL),
                hasAPIKey: provider.hasAPIKey === true,
            }
        }),
        models: value.models.map((model) => {
            if (!isRecord(model) ||
                typeof model.contextWindow !== 'number' || typeof model.maxOutputTokens !== 'number') {
                throw new Error('The model configuration response is invalid.')
            }
            const apiFormat = stringValue(model.apiFormat)
            if (apiFormat !== 'anthropic_messages' && apiFormat !== 'openai_responses' &&
                apiFormat !== 'openai_chat_completions') {
                throw new Error('The model configuration response is invalid.')
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
        throw new Error('The model catalog response is invalid.')
    }
    return value
}

function stringArray(value: unknown): string[] {
    if (!Array.isArray(value) || !value.every((item) => typeof item === 'string')) {
        throw new Error('The model catalog response is invalid.')
    }
    return value
}

function apiFormatValue(value: unknown): ModelAPIFormat {
    const format = stringValue(value)
    if (format !== 'anthropic_messages' && format !== 'openai_responses' && format !== 'openai_chat_completions') {
        throw new Error('The model configuration response is invalid.')
    }
    return format
}
