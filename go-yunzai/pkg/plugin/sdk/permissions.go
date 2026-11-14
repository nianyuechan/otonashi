package sdk

import "context"

// Permission restricts a rule. Empty and PermissionAll allow all subjects.
type Permission string

const (
	PermissionAll    Permission = "all"
	PermissionMaster Permission = "master"
	PermissionOwner  Permission = "owner"
	PermissionAdmin  Permission = "admin"
)

// Permissions describes trusted account ownership and group roles. A group
// owner is also an admin. Message text is never a source of these flags.
type Permissions struct {
	IsMaster bool
	IsOwner  bool
	IsAdmin  bool
}

// AcceptDecision controls the accept phase, before any ordinary rules run.
type AcceptDecision uint8

const (
	AcceptNextPlugin AcceptDecision = iota
	AcceptBreak                     // Skip remaining accepts and proceed to ordinary rules.
	AcceptReturn                    // End the entire event, without ordinary rule dispatch.
)

// AcceptFunc runs after pending sessions and before ordinary rules. Each
// plugin may register one accept hook; errors are isolated from other plugins.
type AcceptFunc func(context.Context, *Context) (AcceptDecision, error)
