// Package osfeat holds the features of offstage that depend on the operating
// system. Each system supplies its backends in its own file. Shared code calls
// the interfaces and does not check the system. A nil backend means that the
// feature is not supported here.
package osfeat

// Window is a window of the human's desktop, as a backend needs it.
type Window struct {
	Class string // app id
	Title string
}

// Prompts recognizes the windows that ask the human for a secret: polkit,
// keyring and pinentry prompts. No input tool may act on them.
type Prompts interface {
	IsPrompt(w Window) bool
}

// Set holds the backends of this system.
type Set struct {
	Prompts Prompts
}
