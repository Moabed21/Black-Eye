package tabs

// JumpToTabMsg signals the root app model to switch active tab and deliver a payload.
type JumpToTabMsg struct {
	TabIndex int
	Payload  interface{}
}

// PayloadReceiver is an interface for tab models that can receive cross-tab payloads.
type PayloadReceiver interface {
	ReceivePayload(payload interface{})
}

// InputCapturer is an interface for tab models that capture text/numeric input or modal dialogs.
type InputCapturer interface {
	IsInputActive() bool
}
