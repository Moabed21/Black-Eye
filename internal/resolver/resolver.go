// Package resolver provides human-readable naming for raw system identifiers.
// All resolvers are initialized once at startup and are stateless after that —
// safe for concurrent reads from multiple service goroutines without locking.
//
// Naming rule: if a raw name is ambiguous to a non-expert, display it as:
//   real-name (purpose)
// If the name is self-describing, show it as-is.
package resolver
