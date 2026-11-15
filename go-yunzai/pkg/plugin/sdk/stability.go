package sdk

// Stability tells a plugin author how much they may rely on an exported SDK
// symbol. It is a declaration, not a version number: this package is released
// together with the core, so there is nothing to negotiate at runtime.
//
// Three levels are used. There is deliberately no "internal" level: anything
// exported from this package is reachable by plugin authors, so calling it
// internal would be a promise the compiler cannot keep. Implementation details
// live in packages under internal/ instead.
type Stability uint8

const (
	// StabilityStable means the symbol is a contract. Removing it, renaming it,
	// changing its type, or changing its documented semantics breaks plugins and
	// requires an ADR plus a migration note (see ADR 0004).
	StabilityStable Stability = iota
	// StabilityExperimental means the symbol may change or disappear in any
	// release. A plugin may use it, but must not treat its shape as settled.
	StabilityExperimental
	// StabilityReserved means the symbol exists as a placeholder and must not be
	// depended on. It carries no behavioural guarantee yet.
	StabilityReserved
)

// String returns the lowercase level name used in documentation and errors.
func (s Stability) String() string {
	switch s {
	case StabilityStable:
		return "stable"
	case StabilityExperimental:
		return "experimental"
	case StabilityReserved:
		return "reserved"
	default:
		return "unknown"
	}
}

// stabilityIndex is the single source of truth for what a plugin may rely on.
// Every exported top-level symbol in this package must appear here; the
// contract test in stability_test.go reads the package AST and fails otherwise.
var stabilityIndex = map[string]Stability{
	// Plugin identity and lifecycle.
	"Plugin":     StabilityStable,
	"Descriptor": StabilityStable,

	// Registration surface.
	"Router":      StabilityStable,
	"Rule":        StabilityStable,
	"Result":      StabilityStable,
	"HandlerFunc": StabilityStable,
	"RegexEngine": StabilityStable,
	"RegexRE2":    StabilityStable,
	"Stop":        StabilityStable,
	"Next":        StabilityStable,

	// Accept phase.
	"AcceptFunc":       StabilityStable,
	"AcceptDecision":   StabilityStable,
	"AcceptNextPlugin": StabilityStable,
	"AcceptBreak":      StabilityStable,
	"AcceptReturn":     StabilityStable,

	// Context and event model.
	"Context":        StabilityStable,
	"Event":          StabilityStable,
	"Segment":        StabilityStable,
	"SenderInfo":     StabilityStable,
	"RuntimeContext": StabilityStable,

	// Sending.
	"Sender":     StabilityStable,
	"Outgoing":   StabilityStable,
	"SendResult": StabilityStable,

	// Sessions.
	"Session": StabilityStable,

	// Tasks.
	"Task":        StabilityStable,
	"TaskFunc":    StabilityStable,
	"TaskContext": StabilityStable,

	// Policy and permissions.
	"Policy":             StabilityStable,
	"Capability":         StabilityStable,
	"CapabilityMessages": StabilityStable,
	"CapabilityRender":   StabilityStable,
	"CapabilityStorage":  StabilityStable,
	"Permission":         StabilityStable,
	"PermissionAll":      StabilityStable,
	"PermissionMaster":   StabilityStable,
	"PermissionOwner":    StabilityStable,
	"PermissionAdmin":    StabilityStable,
	"Permissions":        StabilityStable,

	// Storage.
	"KVStore":             StabilityStable,
	"ConfigView":          StabilityStable,
	"ErrKeyNotFound":      StabilityStable,
	"ErrStoreUnavailable": StabilityStable,
	"ErrStoreCommand":     StabilityStable,
	"ErrStoreClosed":      StabilityStable,
	"ErrInvalidKey":       StabilityStable,
	"ErrInvalidTTL":       StabilityStable,

	// Runtime errors.
	"ErrNotRunning": StabilityStable,

	// Reserved: exists, but must not be depended on yet.
	"Clock": StabilityReserved,

	// This classification surface.
	"Stability":             StabilityStable,
	"StabilityStable":       StabilityStable,
	"StabilityExperimental": StabilityStable,
	"StabilityReserved":     StabilityStable,
	"StabilityOf":           StabilityStable,
}

// StabilityOf reports the declared level of an exported SDK symbol. The second
// result is false when the name is not part of the SDK contract table.
func StabilityOf(name string) (Stability, bool) {
	level, ok := stabilityIndex[name]
	return level, ok
}
