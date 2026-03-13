package main

// ProviderAdapter describes how a managed CLI provider is launched and controlled.
type ProviderAdapter interface {
	ProviderName() string
	BuildCommand(spec LaunchSpec) (CommandSpec, error)
}
