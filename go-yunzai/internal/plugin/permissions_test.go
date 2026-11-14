package plugin

import (
	"context"
	"testing"

	"github.com/trss-yunzai/go-yunzai/internal/event"
	"github.com/trss-yunzai/go-yunzai/internal/message"
	"github.com/trss-yunzai/go-yunzai/pkg/plugin/sdk"
)

func TestRulePermissionsFollowLegacyRolesAndStopOnDenial(t *testing.T) {
	for _, test := range []struct {
		name          string
		permission    sdk.Permission
		kind, role    string
		account, user int64
		denial        string
	}{
		{name: "all", permission: sdk.PermissionAll, kind: "group"},
		{name: "master denied", permission: sdk.PermissionMaster, kind: "private", denial: "暂无权限，只有主人才能操作"},
		{name: "master allowed", permission: sdk.PermissionMaster, kind: "group", account: 10, user: 20},
		{name: "master account isolation", permission: sdk.PermissionMaster, kind: "private", account: 11, user: 20, denial: "暂无权限，只有主人才能操作"},
		{name: "owner allowed", permission: sdk.PermissionOwner, kind: "group", role: "owner"},
		{name: "admin is not owner", permission: sdk.PermissionOwner, kind: "group", role: "admin", denial: "暂无权限，只有群主才能操作"},
		{name: "admin allowed", permission: sdk.PermissionAdmin, kind: "group", role: "admin"},
		{name: "owner is admin", permission: sdk.PermissionAdmin, kind: "group", role: "owner"},
		{name: "member denied", permission: sdk.PermissionAdmin, kind: "group", role: "member", denial: "暂无权限，只有管理员才能操作"},
		{name: "unknown role denied", permission: sdk.PermissionAdmin, kind: "group", role: "superuser", denial: "暂无权限，只有管理员才能操作"},
		{name: "master bypasses owner", permission: sdk.PermissionOwner, kind: "group", account: 10, user: 20},
		{name: "legacy private owner", permission: sdk.PermissionOwner, kind: "private"},
		{name: "legacy private admin", permission: sdk.PermissionAdmin, kind: "private"},
	} {
		t.Run(test.name, func(t *testing.T) {
			sender := &fakeSender{}
			manager := NewWithOptions(sender, nil, Options{Policy: sdk.Policy{Masters: map[int64][]int64{10: {20}}}})
			var handled, later bool
			for _, p := range []*fakePlugin{
				{descriptor: sdk.Descriptor{ID: "protected", Version: "1"}, rules: func(r sdk.Router) error {
					return r.Rule(sdk.Rule{ID: "restricted", Event: "message", Permission: test.permission}, func(_ context.Context, c *sdk.Context) (sdk.Result, error) {
						handled = true
						if c.Permissions.IsMaster != (test.account == 10 && test.user == 20) {
							t.Error("incorrect master context")
						}
						return sdk.Stop, nil
					})
				}},
				{descriptor: sdk.Descriptor{ID: "later", Version: "1"}, rules: func(r sdk.Router) error {
					return r.Rule(sdk.Rule{ID: "fallback", Event: "message"}, func(context.Context, *sdk.Context) (sdk.Result, error) { later = true; return sdk.Stop, nil })
				}},
			} {
				if err := manager.Register(p); err != nil {
					t.Fatal(err)
				}
			}
			startManager(t, manager)
			user := test.user
			if user == 0 {
				user = 30
			}
			evt := &event.Event{PostType: "message", MessageType: test.kind, SelfID: test.account, UserID: user, Sender: event.Sender{Role: test.role}}
			if test.kind == "group" {
				evt.GroupID = 40
			}
			if err := manager.HandleEvent(context.Background(), evt); err != nil {
				t.Fatal(err)
			}
			if test.denial == "" {
				if !handled || !later || len(sender.outgoing.Message) != 0 {
					t.Fatalf("allowed rule did not run cleanly: handled=%v later=%v", handled, later)
				}
			} else {
				if handled || later {
					t.Fatal("permission rejection did not terminate the event")
				}
				if len(sender.outgoing.Message) != 1 || sender.outgoing.Message[0].Data["text"] != test.denial {
					t.Fatalf("denial reply = %+v", sender.outgoing)
				}
				if (test.kind == "group" && sender.outgoing.GroupID != 40) || (test.kind == "private" && sender.outgoing.UserID != user) {
					t.Fatal("denial routed to wrong conversation")
				}
			}
		})
	}
}

func TestRoleContextDoesNotTrustPrivateOrMismatchedSender(t *testing.T) {
	policy := newPolicyState(sdk.Policy{})
	for _, evt := range []*event.Event{
		{MessageType: "private", GroupID: 0, UserID: 20, Sender: event.Sender{UserID: 20, Role: "owner"}},
		{MessageType: "group", GroupID: 30, UserID: 20, Sender: event.Sender{UserID: 21, Role: "owner"}},
	} {
		if got := policy.permissions(evt); got.IsAdmin || got.IsOwner || got.IsMaster {
			t.Fatalf("untrusted role granted permissions: %+v", got)
		}
	}
}

func TestInvalidPermissionRejectedAndPolicyCannotBeMutatedAfterCreation(t *testing.T) {
	masters := map[int64][]int64{10: {20}}
	manager := NewWithOptions(nil, nil, Options{Policy: sdk.Policy{Masters: masters}})
	masters[10][0] = 30
	if !manager.policy.permissions(&event.Event{SelfID: 10, UserID: 20}).IsMaster {
		t.Fatal("policy retained caller's mutable master slice")
	}
	p := &fakePlugin{descriptor: sdk.Descriptor{ID: "invalid", Version: "1"}, rules: func(r sdk.Router) error {
		return r.Rule(sdk.Rule{ID: "invalid", Event: "message", Permission: "typo"}, func(context.Context, *sdk.Context) (sdk.Result, error) { return sdk.Stop, nil })
	}}
	if err := manager.Register(p); err == nil {
		t.Fatal("invalid permission was silently accepted")
	}
}

func TestPermissionChecksCannotBeChangedByEarlierPluginContext(t *testing.T) {
	sender := &fakeSender{}
	manager := New(sender, nil)
	for _, p := range []*fakePlugin{
		{descriptor: sdk.Descriptor{ID: "mutate", Version: "1"}, rules: func(r sdk.Router) error {
			return r.Accept(func(_ context.Context, c *sdk.Context) (sdk.AcceptDecision, error) {
				c.Permissions.IsMaster = true
				c.Event.UserID = 999
				return sdk.AcceptNextPlugin, nil
			})
		}},
		{descriptor: sdk.Descriptor{ID: "protected", Version: "1"}, rules: func(r sdk.Router) error {
			return r.Rule(sdk.Rule{ID: "master", Event: "message", Permission: sdk.PermissionMaster}, func(context.Context, *sdk.Context) (sdk.Result, error) {
				t.Error("master permission bypassed")
				return sdk.Stop, nil
			})
		}},
	} {
		if err := manager.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	startManager(t, manager)
	if err := manager.HandleEvent(context.Background(), &event.Event{PostType: "message", MessageType: "private", UserID: 20, Message: []message.Segment{message.Text("hello")}}); err != nil {
		t.Fatal(err)
	}
	if sender.outgoing.UserID != 20 || len(sender.outgoing.Message) != 1 {
		t.Fatalf("denial reply lost original identity: %+v", sender.outgoing)
	}
}
