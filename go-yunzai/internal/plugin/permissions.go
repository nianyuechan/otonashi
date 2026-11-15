package plugin

import (
	"github.com/nianyuechan/otonashi/go-yunzai/internal/event"
	"github.com/nianyuechan/otonashi/go-yunzai/pkg/plugin/sdk"
)

func (p policyState) permissions(evt *event.Event) sdk.Permissions {
	var result sdk.Permissions
	if evt.SelfID > 0 && evt.UserID > 0 {
		_, result.IsMaster = p.masters[evt.SelfID][evt.UserID]
	}
	if evt.MessageType == "group" && evt.GroupID > 0 && (evt.Sender.UserID == 0 || evt.Sender.UserID == evt.UserID) {
		result.IsOwner = evt.Sender.Role == "owner"
		result.IsAdmin = result.IsOwner || evt.Sender.Role == "admin"
	}
	return result
}

// Preserve loader.js filtPermission: account masters bypass rule permissions;
// owner/admin restrict group events, and remain unrestricted in private chat.
func permissionDenial(permission sdk.Permission, evt *event.Event, permissions sdk.Permissions) string {
	if permissions.IsMaster {
		return ""
	}
	if permission == sdk.PermissionMaster {
		return "暂无权限，只有主人才能操作"
	}
	if evt.MessageType != "group" && evt.GroupID == 0 {
		return ""
	}
	if permission == sdk.PermissionOwner && !permissions.IsOwner {
		return "暂无权限，只有群主才能操作"
	}
	if permission == sdk.PermissionAdmin && !permissions.IsAdmin {
		return "暂无权限，只有管理员才能操作"
	}
	return ""
}
