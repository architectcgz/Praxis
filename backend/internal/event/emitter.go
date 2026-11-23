package event

// Emitter 的接收方在装配时确定，运行期间不再修改。
type Emitter struct {
	observe func(Event)
}

// NewEmitter 未指定接收方时不发布通知。
func NewEmitter(observe func(Event)) *Emitter {
	if observe == nil {
		observe = func(Event) {}
	}
	return &Emitter{observe: observe}
}

// Emit 由调用方同步执行；并发发布可能同时进入接收方，不保证全局顺序。
func (e *Emitter) Emit(value Event) {
	e.observe(value)
}
