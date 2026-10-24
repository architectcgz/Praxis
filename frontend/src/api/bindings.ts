import { System } from '@wailsio/runtime'
import {
    AgentBindings,
    CommandBindings,
    ModelBindings,
    ProjectBindings,
    SessionBindings,
} from '../../bindings/praxis/wails/bindings'

export class BindingUnavailableError extends Error {
    constructor() {
        super('binding unavailable')
        this.name = 'BindingUnavailableError'
    }
}

export function isBindingAvailable() {
    // 运行时模块也能被浏览器加载，不能仅凭全局命名空间存在就认定桌面桥可用。
    return System.IsDesktop()
}

function requireBinding<T>(binding: T): T {
    if (!isBindingAvailable()) {
        throw new BindingUnavailableError()
    }
    return binding
}

export function getProjectBinding() {
    return requireBinding(ProjectBindings)
}

export function getSessionBinding() {
    return requireBinding(SessionBindings)
}

export function getAgentBinding() {
    return requireBinding(AgentBindings)
}

export function getCommandBinding() {
    return requireBinding(CommandBindings)
}

export function getModelBinding() {
    return requireBinding(ModelBindings)
}
