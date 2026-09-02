import { getModelBinding } from './bindings'

export type ReasoningOption = {
    supported: boolean
    levels: string[]
    default: string
}

export type ModelOption = {
    providerId: string
    modelId: string
    label: string
    providerName: string
    reasoning: ReasoningOption
    defaultProfiles: string[]
}

export type ProviderConfigOption = {
    id: string
    providerName: string
    baseURL: string
    proxyURL: string
    hasAPIKey: boolean
}

export type ModelAPIFormat = 'anthropic_messages' | 'openai_responses' | 'openai_chat_completions'

export type ModelConfigOption = {
    providerId: string
    modelId: string
    label: string
    apiFormat: ModelAPIFormat
    contextWindow: number
    maxOutputTokens: number
    reasoning: ReasoningOption
}

export type ModelConfigDocument = {
    providers: ProviderConfigOption[]
    models: ModelConfigOption[]
    profiles: Record<string, ModelReference>
    profileNames: string[]
}

export type ModelReference = {
    providerId: string
    modelId: string
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
        providers: config.providers,
        models: config.models,
        profiles: config.profiles,
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
        if (!isRecord(model) || !isRecord(model.reasoning)) {
            throw new Error('The model catalog response is invalid.')
        }
        const reasoning = model.reasoning
        if (typeof reasoning.supported !== 'boolean') {
            throw new Error('The model catalog response is invalid.')
        }
        const levels = reasoning.levels == null && !reasoning.supported
            ? []
            : stringArray(reasoning.levels)
        return {
            providerId: stringValue(model.providerId),
            modelId: stringValue(model.modelId),
            label: stringValue(model.label),
            providerName: stringValue(model.providerName),
            reasoning: {
                supported: reasoning.supported,
                levels,
                default: stringValue(reasoning.default),
            },
            defaultProfiles: stringArray(model.defaultProfiles),
        }
    })
}

function normalizeModelConfig(value: unknown): ModelConfigDocument {
    if (!isRecord(value) || !Array.isArray(value.providers) || !Array.isArray(value.models) ||
        !isRecord(value.profiles)) {
        throw new Error('The model configuration response is invalid.')
    }
    return {
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
            if (!isRecord(model) || !isRecord(model.reasoning) ||
                typeof model.contextWindow !== 'number' || typeof model.maxOutputTokens !== 'number' ||
                typeof model.reasoning.supported !== 'boolean') {
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
                apiFormat,
                contextWindow: model.contextWindow,
                maxOutputTokens: model.maxOutputTokens,
                reasoning: {
                    supported: model.reasoning.supported,
                    levels: stringArray(model.reasoning.levels),
                    default: stringValue(model.reasoning.default),
                },
            }
        }),
        profiles: Object.fromEntries(Object.entries(value.profiles).map(([profile, reference]) => {
            if (!isRecord(reference)) throw new Error('The model configuration response is invalid.')
            return [profile, {
                providerId: stringValue(reference.providerId),
                modelId: stringValue(reference.modelId),
            }]
        })),
        profileNames: value.profileNames == null ? [] : stringArray(value.profileNames),
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
