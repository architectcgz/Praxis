import type * as GeneratedAgentBinding from '../../wailsjs/go/bindings/AgentBindings'
import type * as GeneratedCommandBinding from '../../wailsjs/go/bindings/CommandBindings'
import type * as GeneratedModelBinding from '../../wailsjs/go/bindings/ModelBindings'
import type * as GeneratedProjectBinding from '../../wailsjs/go/bindings/ProjectBindings'
import type * as GeneratedSessionBinding from '../../wailsjs/go/bindings/SessionBindings'

type ProjectBinding = typeof GeneratedProjectBinding
type SessionBinding = typeof GeneratedSessionBinding
type AgentBinding = typeof GeneratedAgentBinding
type CommandBinding = typeof GeneratedCommandBinding
type ModelBinding = typeof GeneratedModelBinding

export class BindingUnavailableError extends Error {
    constructor() {
        super('binding unavailable')
        this.name = 'BindingUnavailableError'
    }
}

type WailsBindings = {
    ProjectBindings?: ProjectBinding
    SessionBindings?: SessionBinding
    AgentBindings?: AgentBinding
    CommandBindings?: CommandBinding
    ModelBindings?: ModelBinding
}

type WailsWindow = Window & {
    go?: {
        bindings?: WailsBindings
    }
}

function bindingNamespace(): WailsBindings {
    const bindings = (window as WailsWindow).go?.bindings
    if (!bindings) {
        throw new BindingUnavailableError()
    }
    return bindings
}

function requireBinding<T>(binding: T | undefined): T {
    if (!binding) {
        throw new BindingUnavailableError()
    }
    return binding
}

export function isBindingAvailable() {
    const bindings = (window as WailsWindow).go?.bindings
    return Boolean(
        bindings?.ProjectBindings &&
        bindings.SessionBindings &&
        bindings.AgentBindings &&
        bindings.CommandBindings &&
        bindings.ModelBindings,
    )
}

export function getProjectBinding() {
    return requireBinding(bindingNamespace().ProjectBindings)
}

export function getSessionBinding() {
    return requireBinding(bindingNamespace().SessionBindings)
}

export function getAgentBinding() {
    return requireBinding(bindingNamespace().AgentBindings)
}

export function getCommandBinding() {
    return requireBinding(bindingNamespace().CommandBindings)
}

export function getModelBinding() {
    return requireBinding(bindingNamespace().ModelBindings)
}
