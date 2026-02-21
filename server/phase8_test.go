package main

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// newAdminClient creates a client that has the ADMIN role in the room.
func newAdminClient(t *testing.T, username string, room *Room) (*Client, *bytes.Buffer) {
	t.Helper()
	c, buf := newCtrlClient(username)
	room.AddClient(c)
	room.SetClientRole(c.ID, RoleAdmin)
	return c, buf
}

// newOwnerClient creates a client and makes them the room owner.
func newOwnerClient(t *testing.T, username string, room *Room) (*Client, *bytes.Buffer) {
	t.Helper()
	c, buf := newCtrlClient(username)
	room.AddClient(c)
	room.ClaimOwnership(c.ID)
	room.SetClientRole(c.ID, RoleOwner)
	return c, buf
}

// ---------------------------------------------------------------------------
// ban
// ---------------------------------------------------------------------------

func TestProcessControlBanByAdmin(t *testing.T) {
	room := NewRoom()
	var banRecorded bool
	room.SetOnBan(func(pubkey, ip, reason, bannedBy string, durationS int) { banRecorded = true })
	room.SetOnAuditLog(func(int, string, string, string, string) {})

	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	admin, _ := newAdminClient(t, "admin", room)
	target, targetBuf, _, targetCloser := newKickableClient("bob")
	room.AddClient(target)

	processControl(ControlMsg{Type: "ban", ID: target.ID, Reason: "spam"}, admin, room)

	got := decodeControl(t, targetBuf)
	if got.Type != "banned" {
		t.Errorf("target should receive banned, got %q", got.Type)
	}
	if got.Reason != "spam" {
		t.Errorf("reason: got %q, want %q", got.Reason, "spam")
	}
	if !targetCloser.closed {
		t.Error("target connection should be closed after ban")
	}
	if !banRecorded {
		t.Error("ban callback should have been called")
	}
}

func TestProcessControlBanByNonAdmin(t *testing.T) {
	room := NewRoom()
	room.SetOnBan(func(string, string, string, string, int) {})
	room.SetOnAuditLog(func(int, string, string, string, string) {})

	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	user, _ := newCtrlClient("user")
	room.AddClient(user)
	target, targetBuf, _, targetCloser := newKickableClient("bob")
	room.AddClient(target)

	processControl(ControlMsg{Type: "ban", ID: target.ID}, user, room)

	if targetBuf.Len() != 0 {
		t.Error("USER should not be able to ban")
	}
	if targetCloser.closed {
		t.Error("target connection should not be closed")
	}
}

func TestProcessControlBanSelf(t *testing.T) {
	room := NewRoom()
	admin, adminBuf := newAdminClient(t, "admin", room)
	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner

	processControl(ControlMsg{Type: "ban", ID: admin.ID}, admin, room)

	if adminBuf.Len() != 0 {
		t.Error("banning self should be rejected")
	}
}

func TestProcessControlBanOwner(t *testing.T) {
	room := NewRoom()
	room.SetOnBan(func(string, string, string, string, int) {})
	room.SetOnAuditLog(func(int, string, string, string, string) {})

	owner, ownerBuf, _, ownerCloser := newKickableClient("owner")
	room.AddClient(owner)
	room.ClaimOwnership(owner.ID)

	admin, _ := newCtrlClient("admin")
	room.AddClient(admin)
	room.SetClientRole(admin.ID, RoleAdmin)

	processControl(ControlMsg{Type: "ban", ID: owner.ID}, admin, room)

	if ownerBuf.Len() != 0 {
		t.Error("banning the owner should be rejected")
	}
	if ownerCloser.closed {
		t.Error("owner connection should not be closed")
	}
}

func TestProcessControlBanNonExistentTarget(t *testing.T) {
	room := NewRoom()
	admin, adminBuf := newAdminClient(t, "admin", room)
	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner

	processControl(ControlMsg{Type: "ban", ID: 9999}, admin, room)

	if adminBuf.Len() != 0 {
		t.Error("banning non-existent target should be silently rejected")
	}
}

func TestProcessControlBanWithDefaultReason(t *testing.T) {
	room := NewRoom()
	var bannedReason string
	room.SetOnBan(func(_, _, reason, _ string, _ int) { bannedReason = reason })
	room.SetOnAuditLog(func(int, string, string, string, string) {})

	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	admin, _ := newAdminClient(t, "admin", room)
	target, _, _, _ := newKickableClient("bob")
	room.AddClient(target)

	// Empty reason should be replaced with default.
	processControl(ControlMsg{Type: "ban", ID: target.ID, Reason: ""}, admin, room)

	if bannedReason != "No reason provided" {
		t.Errorf("default reason: got %q, want %q", bannedReason, "No reason provided")
	}
}

func TestProcessControlBanWithIPBan(t *testing.T) {
	room := NewRoom()
	var bannedIP string
	room.SetOnBan(func(_, ip, _, _ string, _ int) { bannedIP = ip })
	room.SetOnAuditLog(func(int, string, string, string, string) {})

	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	admin, _ := newAdminClient(t, "admin", room)
	target, _, _, _ := newKickableClient("bob")
	target.remoteIP = "192.168.1.50"
	room.AddClient(target)

	processControl(ControlMsg{Type: "ban", ID: target.ID, BanIP: true}, admin, room)

	if bannedIP != "192.168.1.50" {
		t.Errorf("IP ban: got %q, want %q", bannedIP, "192.168.1.50")
	}
}

func TestProcessControlBanWithTempDuration(t *testing.T) {
	room := NewRoom()
	var bannedDuration int
	room.SetOnBan(func(_, _, _, _ string, dur int) { bannedDuration = dur })
	room.SetOnAuditLog(func(int, string, string, string, string) {})

	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	admin, _ := newAdminClient(t, "admin", room)
	target, _, _, _ := newKickableClient("bob")
	room.AddClient(target)

	processControl(ControlMsg{Type: "ban", ID: target.ID, Duration: 3600}, admin, room)

	if bannedDuration != 3600 {
		t.Errorf("duration: got %d, want 3600", bannedDuration)
	}
}

func TestProcessControlBanZeroID(t *testing.T) {
	room := NewRoom()
	admin, adminBuf := newAdminClient(t, "admin", room)
	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner

	processControl(ControlMsg{Type: "ban", ID: 0}, admin, room)

	if adminBuf.Len() != 0 {
		t.Error("ban with ID=0 should be rejected")
	}
}

// ---------------------------------------------------------------------------
// unban
// ---------------------------------------------------------------------------

func TestProcessControlUnbanByAdmin(t *testing.T) {
	room := NewRoom()
	var unbannedID int64
	room.SetOnUnban(func(id int64) { unbannedID = id })

	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	admin, _ := newAdminClient(t, "admin", room)

	processControl(ControlMsg{Type: "unban", BanID: 42}, admin, room)

	if unbannedID != 42 {
		t.Errorf("unban ID: got %d, want 42", unbannedID)
	}
}

func TestProcessControlUnbanByNonAdmin(t *testing.T) {
	room := NewRoom()
	var called bool
	room.SetOnUnban(func(int64) { called = true })

	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	user, _ := newCtrlClient("user")
	room.AddClient(user)

	processControl(ControlMsg{Type: "unban", BanID: 42}, user, room)

	if called {
		t.Error("USER should not be able to unban")
	}
}

func TestProcessControlUnbanZeroBanID(t *testing.T) {
	room := NewRoom()
	var called bool
	room.SetOnUnban(func(int64) { called = true })

	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	admin, _ := newAdminClient(t, "admin", room)

	processControl(ControlMsg{Type: "unban", BanID: 0}, admin, room)

	if called {
		t.Error("unban with BanID=0 should be rejected")
	}
}

// ---------------------------------------------------------------------------
// set_role
// ---------------------------------------------------------------------------

func TestProcessControlSetRoleByOwner(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	target, targetBuf := newCtrlClient("bob")
	room.AddClient(target)

	processControl(ControlMsg{Type: "set_role", ID: target.ID, Role: RoleAdmin}, owner, room)

	got := decodeControl(t, targetBuf)
	if got.Type != "role_changed" {
		t.Errorf("type: got %q, want %q", got.Type, "role_changed")
	}
	if got.ID != target.ID {
		t.Errorf("ID: got %d, want %d", got.ID, target.ID)
	}
	if got.Role != RoleAdmin {
		t.Errorf("role: got %q, want %q", got.Role, RoleAdmin)
	}

	if room.GetClientRole(target.ID) != RoleAdmin {
		t.Errorf("room role: got %q, want %q", room.GetClientRole(target.ID), RoleAdmin)
	}
}

func TestProcessControlSetRoleByNonOwner(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	admin, _ := newAdminClient(t, "admin", room)
	target, targetBuf := newCtrlClient("bob")
	room.AddClient(target)

	processControl(ControlMsg{Type: "set_role", ID: target.ID, Role: RoleModerator}, admin, room)

	if targetBuf.Len() != 0 {
		t.Error("non-owner (even admin) should not be able to set roles")
	}
}

func TestProcessControlSetRoleSelf(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newOwnerClient(t, "owner", room)

	processControl(ControlMsg{Type: "set_role", ID: owner.ID, Role: RoleAdmin}, owner, room)

	if ownerBuf.Len() != 0 {
		t.Error("owner should not be able to set their own role")
	}
}

func TestProcessControlSetRoleInvalidRole(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	target, targetBuf := newCtrlClient("bob")
	room.AddClient(target)

	processControl(ControlMsg{Type: "set_role", ID: target.ID, Role: "SUPERADMIN"}, owner, room)

	if targetBuf.Len() != 0 {
		t.Error("unknown role should be rejected")
	}
}

func TestProcessControlSetRoleZeroID(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newOwnerClient(t, "owner", room)

	processControl(ControlMsg{Type: "set_role", ID: 0, Role: RoleAdmin}, owner, room)

	if ownerBuf.Len() != 0 {
		t.Error("set_role with ID=0 should be rejected")
	}
}

func TestProcessControlSetRoleNonExistentTarget(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newOwnerClient(t, "owner", room)

	processControl(ControlMsg{Type: "set_role", ID: 9999, Role: RoleAdmin}, owner, room)

	if ownerBuf.Len() != 0 {
		t.Error("set_role for non-existent target should be silently rejected")
	}
}

func TestProcessControlSetRoleCannotSetOwner(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	target, targetBuf := newCtrlClient("bob")
	room.AddClient(target)

	// OWNER is not a valid target role for set_role.
	processControl(ControlMsg{Type: "set_role", ID: target.ID, Role: RoleOwner}, owner, room)

	if targetBuf.Len() != 0 {
		t.Error("setting role to OWNER should be rejected")
	}
}

func TestProcessControlSetRoleDemoteAdmin(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	target, targetBuf := newCtrlClient("bob")
	room.AddClient(target)
	room.SetClientRole(target.ID, RoleAdmin)

	processControl(ControlMsg{Type: "set_role", ID: target.ID, Role: RoleUser}, owner, room)

	got := decodeControl(t, targetBuf)
	if got.Type != "role_changed" {
		t.Fatalf("type: got %q, want %q", got.Type, "role_changed")
	}
	if got.Role != RoleUser {
		t.Errorf("role: got %q, want %q", got.Role, RoleUser)
	}
	if room.GetClientRole(target.ID) != RoleUser {
		t.Errorf("room role: got %q, want %q", room.GetClientRole(target.ID), RoleUser)
	}
}

func TestProcessControlSetRoleAllValidRoles(t *testing.T) {
	for _, role := range []string{RoleAdmin, RoleModerator, RoleUser} {
		t.Run(role, func(t *testing.T) {
			room := NewRoom()
			owner, _ := newOwnerClient(t, "owner", room)
			target, targetBuf := newCtrlClient("bob")
			room.AddClient(target)

			processControl(ControlMsg{Type: "set_role", ID: target.ID, Role: role}, owner, room)

			got := decodeControl(t, targetBuf)
			if got.Type != "role_changed" {
				t.Errorf("type: got %q, want %q", got.Type, "role_changed")
			}
			if got.Role != role {
				t.Errorf("role: got %q, want %q", got.Role, role)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// announce
// ---------------------------------------------------------------------------

func TestProcessControlAnnounceByOwner(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	receiver, receiverBuf := newCtrlClient("bob")
	room.AddClient(receiver)

	processControl(ControlMsg{Type: "announce", Announcement: "Server maintenance tonight"}, owner, room)

	got := decodeControl(t, receiverBuf)
	if got.Type != "announcement" {
		t.Errorf("type: got %q, want %q", got.Type, "announcement")
	}
	if got.Announcement != "Server maintenance tonight" {
		t.Errorf("announcement: got %q", got.Announcement)
	}
	if got.Username != "owner" {
		t.Errorf("username: got %q, want %q", got.Username, "owner")
	}

	content, user := room.GetAnnouncement()
	if content != "Server maintenance tonight" {
		t.Errorf("room announcement: got %q", content)
	}
	if user != "owner" {
		t.Errorf("room announcement user: got %q", user)
	}
}

func TestProcessControlAnnounceByNonOwner(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	admin, _ := newAdminClient(t, "admin", room)
	receiver, receiverBuf := newCtrlClient("bob")
	room.AddClient(receiver)

	processControl(ControlMsg{Type: "announce", Announcement: "hacked"}, admin, room)

	if receiverBuf.Len() != 0 {
		t.Error("non-owner (even admin) should not be able to announce")
	}
}

func TestProcessControlAnnounceEmpty(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newOwnerClient(t, "owner", room)

	processControl(ControlMsg{Type: "announce", Announcement: ""}, owner, room)

	if ownerBuf.Len() != 0 {
		t.Error("empty announcement should be rejected")
	}
}

func TestProcessControlAnnounceTooLong(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newOwnerClient(t, "owner", room)

	long := make([]byte, MaxChatLength+1)
	for i := range long {
		long[i] = 'x'
	}

	processControl(ControlMsg{Type: "announce", Announcement: string(long)}, owner, room)

	if ownerBuf.Len() != 0 {
		t.Error("announcement exceeding MaxChatLength should be rejected")
	}
}

func TestProcessControlAnnounceOverwritesPrevious(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newOwnerClient(t, "owner", room)

	processControl(ControlMsg{Type: "announce", Announcement: "first"}, owner, room)
	_ = decodeControl(t, ownerBuf)

	processControl(ControlMsg{Type: "announce", Announcement: "second"}, owner, room)
	_ = decodeControl(t, ownerBuf)

	content, _ := room.GetAnnouncement()
	if content != "second" {
		t.Errorf("announcement should be overwritten: got %q, want %q", content, "second")
	}
}

// ---------------------------------------------------------------------------
// set_slow_mode
// ---------------------------------------------------------------------------

func TestProcessControlSetSlowModeByOwner(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	receiver, receiverBuf := newCtrlClient("bob")
	room.AddClient(receiver)

	processControl(ControlMsg{Type: "set_slow_mode", ChannelID: 1, SlowMode: 5}, owner, room)

	got := decodeControl(t, receiverBuf)
	if got.Type != "slow_mode_set" {
		t.Errorf("type: got %q, want %q", got.Type, "slow_mode_set")
	}
	if got.ChannelID != 1 {
		t.Errorf("channel_id: got %d, want 1", got.ChannelID)
	}
	if got.SlowMode != 5 {
		t.Errorf("slow_mode: got %d, want 5", got.SlowMode)
	}

	if room.GetSlowMode(1) != 5 {
		t.Errorf("room slow mode: got %d, want 5", room.GetSlowMode(1))
	}
}

func TestProcessControlSetSlowModeByNonOwner(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	admin, _ := newAdminClient(t, "admin", room)
	receiver, receiverBuf := newCtrlClient("bob")
	room.AddClient(receiver)

	processControl(ControlMsg{Type: "set_slow_mode", ChannelID: 1, SlowMode: 5}, admin, room)

	if receiverBuf.Len() != 0 {
		t.Error("non-owner should not be able to set slow mode")
	}
}

func TestProcessControlSetSlowModeZeroChannel(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newOwnerClient(t, "owner", room)

	processControl(ControlMsg{Type: "set_slow_mode", ChannelID: 0, SlowMode: 5}, owner, room)

	if ownerBuf.Len() != 0 {
		t.Error("set_slow_mode with channel_id=0 should be rejected")
	}
}

func TestProcessControlSetSlowModeNegativeClamped(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	receiver, receiverBuf := newCtrlClient("bob")
	room.AddClient(receiver)

	processControl(ControlMsg{Type: "set_slow_mode", ChannelID: 1, SlowMode: -5}, owner, room)

	got := decodeControl(t, receiverBuf)
	if got.SlowMode != 0 {
		t.Errorf("negative slow_mode should be clamped to 0, got %d", got.SlowMode)
	}
}

func TestProcessControlSetSlowModeOverMaxClamped(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	receiver, receiverBuf := newCtrlClient("bob")
	room.AddClient(receiver)

	processControl(ControlMsg{Type: "set_slow_mode", ChannelID: 1, SlowMode: 9999}, owner, room)

	got := decodeControl(t, receiverBuf)
	if got.SlowMode != 3600 {
		t.Errorf("slow_mode should be clamped to 3600, got %d", got.SlowMode)
	}
}

func TestProcessControlSetSlowModeRemove(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newOwnerClient(t, "owner", room)

	// Set slow mode first.
	processControl(ControlMsg{Type: "set_slow_mode", ChannelID: 1, SlowMode: 10}, owner, room)
	_ = decodeControl(t, ownerBuf)

	// Remove slow mode (set to 0).
	processControl(ControlMsg{Type: "set_slow_mode", ChannelID: 1, SlowMode: 0}, owner, room)
	got := decodeControl(t, ownerBuf)

	if got.SlowMode != 0 {
		t.Errorf("slow_mode should be 0 after removal, got %d", got.SlowMode)
	}
	if room.GetSlowMode(1) != 0 {
		t.Errorf("room slow mode should be 0 after removal, got %d", room.GetSlowMode(1))
	}
}

// ---------------------------------------------------------------------------
// mute_user
// ---------------------------------------------------------------------------

func TestProcessControlMuteByAdmin(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	admin, _ := newAdminClient(t, "admin", room)
	target, targetBuf := newCtrlClient("bob")
	room.AddClient(target)

	processControl(ControlMsg{Type: "mute_user", ID: target.ID}, admin, room)

	got := decodeControl(t, targetBuf)
	if got.Type != "user_muted" {
		t.Errorf("type: got %q, want %q", got.Type, "user_muted")
	}
	if got.ID != target.ID {
		t.Errorf("ID: got %d, want %d", got.ID, target.ID)
	}
	if !got.Muted {
		t.Error("muted should be true")
	}

	if !room.IsClientMuted(target.ID) {
		t.Error("target should be muted in room")
	}
}

func TestProcessControlMuteByNonAdmin(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	user, _ := newCtrlClient("user")
	room.AddClient(user)
	target, targetBuf := newCtrlClient("bob")
	room.AddClient(target)

	processControl(ControlMsg{Type: "mute_user", ID: target.ID}, user, room)

	if targetBuf.Len() != 0 {
		t.Error("USER should not be able to mute")
	}
}

func TestProcessControlMuteSelf(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	admin, adminBuf := newAdminClient(t, "admin", room)

	processControl(ControlMsg{Type: "mute_user", ID: admin.ID}, admin, room)

	if adminBuf.Len() != 0 {
		t.Error("muting self should be rejected")
	}
}

func TestProcessControlMuteOwner(t *testing.T) {
	room := NewRoom()
	owner, ownerBuf := newOwnerClient(t, "owner", room)
	admin, _ := newAdminClient(t, "admin", room)

	processControl(ControlMsg{Type: "mute_user", ID: owner.ID}, admin, room)

	if ownerBuf.Len() != 0 {
		t.Error("muting the owner should be rejected")
	}
	if room.IsClientMuted(owner.ID) {
		t.Error("owner should not be mutable")
	}
}

func TestProcessControlMuteNonExistentTarget(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	admin, adminBuf := newAdminClient(t, "admin", room)

	processControl(ControlMsg{Type: "mute_user", ID: 9999}, admin, room)

	if adminBuf.Len() != 0 {
		t.Error("muting non-existent target should be silently rejected")
	}
}

func TestProcessControlMuteWithDuration(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	admin, _ := newAdminClient(t, "admin", room)
	target, targetBuf := newCtrlClient("bob")
	room.AddClient(target)

	before := time.Now()
	processControl(ControlMsg{Type: "mute_user", ID: target.ID, Duration: 60}, admin, room)
	after := time.Now()

	got := decodeControl(t, targetBuf)
	if !got.Muted {
		t.Error("muted should be true")
	}
	// Expiry should be roughly 60 seconds from now.
	expectedMin := before.Add(60 * time.Second).UnixMilli()
	expectedMax := after.Add(60 * time.Second).UnixMilli()
	if got.MuteExpiry < expectedMin || got.MuteExpiry > expectedMax {
		t.Errorf("mute_expiry %d not in expected range [%d, %d]", got.MuteExpiry, expectedMin, expectedMax)
	}
}

func TestProcessControlMuteZeroID(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	admin, adminBuf := newAdminClient(t, "admin", room)

	processControl(ControlMsg{Type: "mute_user", ID: 0}, admin, room)

	if adminBuf.Len() != 0 {
		t.Error("mute_user with ID=0 should be rejected")
	}
}

func TestProcessControlMuteAlreadyMutedUserOverwrites(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	admin, _ := newAdminClient(t, "admin", room)
	target, targetBuf := newCtrlClient("bob")
	room.AddClient(target)

	// Mute with no duration (permanent).
	processControl(ControlMsg{Type: "mute_user", ID: target.ID, Duration: 0}, admin, room)
	_ = decodeControl(t, targetBuf) // drain

	// Mute again with a duration — should overwrite.
	processControl(ControlMsg{Type: "mute_user", ID: target.ID, Duration: 60}, admin, room)
	got := decodeControl(t, targetBuf)

	if !got.Muted {
		t.Error("muted should be true after re-mute")
	}
	if got.MuteExpiry == 0 {
		t.Error("mute_expiry should be set after re-mute with duration")
	}
}

// ---------------------------------------------------------------------------
// unmute_user
// ---------------------------------------------------------------------------

func TestProcessControlUnmuteByAdmin(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	admin, _ := newAdminClient(t, "admin", room)
	target, targetBuf := newCtrlClient("bob")
	room.AddClient(target)
	room.SetClientMute(target.ID, true, 0)

	processControl(ControlMsg{Type: "unmute_user", ID: target.ID}, admin, room)

	got := decodeControl(t, targetBuf)
	if got.Type != "user_muted" {
		t.Errorf("type: got %q, want %q", got.Type, "user_muted")
	}
	if got.Muted {
		t.Error("muted should be false after unmute")
	}
	if room.IsClientMuted(target.ID) {
		t.Error("target should be unmuted in room")
	}
}

func TestProcessControlUnmuteByNonAdmin(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	user, _ := newCtrlClient("user")
	room.AddClient(user)
	target, targetBuf := newCtrlClient("bob")
	room.AddClient(target)
	room.SetClientMute(target.ID, true, 0)

	processControl(ControlMsg{Type: "unmute_user", ID: target.ID}, user, room)

	if targetBuf.Len() != 0 {
		t.Error("USER should not be able to unmute")
	}
}

func TestProcessControlUnmuteZeroID(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner
	admin, adminBuf := newAdminClient(t, "admin", room)

	processControl(ControlMsg{Type: "unmute_user", ID: 0}, admin, room)

	if adminBuf.Len() != 0 {
		t.Error("unmute_user with ID=0 should be rejected")
	}
}

// ---------------------------------------------------------------------------
// replay
// ---------------------------------------------------------------------------

func TestProcessControlReplay(t *testing.T) {
	room := NewRoom()
	// Buffer some messages.
	room.BufferMessage(1, ControlMsg{Type: "chat", Message: "msg1"})
	room.BufferMessage(1, ControlMsg{Type: "chat", Message: "msg2"})
	room.BufferMessage(1, ControlMsg{Type: "chat", Message: "msg3"})

	client, clientBuf := newCtrlClient("alice")
	room.AddClient(client)

	// Replay all messages (since seq 0).
	processControl(ControlMsg{Type: "replay", ChannelID: 1, LastSeq: 0}, client, room)

	msgs := drainAllControl(t, clientBuf)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 replayed messages, got %d", len(msgs))
	}
	if msgs[0].Message != "msg1" {
		t.Errorf("first replayed: got %q, want %q", msgs[0].Message, "msg1")
	}
	if msgs[2].Message != "msg3" {
		t.Errorf("third replayed: got %q, want %q", msgs[2].Message, "msg3")
	}
}

func TestProcessControlReplaySinceSeq(t *testing.T) {
	room := NewRoom()
	room.BufferMessage(1, ControlMsg{Type: "chat", Message: "msg1"})
	room.BufferMessage(1, ControlMsg{Type: "chat", Message: "msg2"})
	room.BufferMessage(1, ControlMsg{Type: "chat", Message: "msg3"})

	client, clientBuf := newCtrlClient("alice")
	room.AddClient(client)

	// Replay only messages since seq 2.
	processControl(ControlMsg{Type: "replay", ChannelID: 1, LastSeq: 2}, client, room)

	msgs := drainAllControl(t, clientBuf)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 replayed message, got %d", len(msgs))
	}
	if msgs[0].Message != "msg3" {
		t.Errorf("replayed: got %q, want %q", msgs[0].Message, "msg3")
	}
}

func TestProcessControlReplayZeroChannel(t *testing.T) {
	room := NewRoom()
	client, clientBuf := newCtrlClient("alice")
	room.AddClient(client)

	processControl(ControlMsg{Type: "replay", ChannelID: 0, LastSeq: 0}, client, room)

	if clientBuf.Len() != 0 {
		t.Error("replay with channel_id=0 should be rejected")
	}
}

func TestProcessControlReplayNoMessages(t *testing.T) {
	room := NewRoom()
	client, clientBuf := newCtrlClient("alice")
	room.AddClient(client)

	processControl(ControlMsg{Type: "replay", ChannelID: 99, LastSeq: 0}, client, room)

	if clientBuf.Len() != 0 {
		t.Error("replay with no messages should produce no output")
	}
}

func TestProcessControlReplayHighLastSeq(t *testing.T) {
	room := NewRoom()
	room.BufferMessage(1, ControlMsg{Type: "chat", Message: "msg1"})

	client, clientBuf := newCtrlClient("alice")
	room.AddClient(client)

	// LastSeq is beyond the current seq — no messages should be returned.
	processControl(ControlMsg{Type: "replay", ChannelID: 1, LastSeq: 999}, client, room)

	if clientBuf.Len() != 0 {
		t.Error("replay with LastSeq beyond current should produce no output")
	}
}

func TestProcessControlReplayDifferentChannels(t *testing.T) {
	room := NewRoom()
	room.BufferMessage(1, ControlMsg{Type: "chat", Message: "ch1-msg"})
	room.BufferMessage(2, ControlMsg{Type: "chat", Message: "ch2-msg"})

	client, clientBuf := newCtrlClient("alice")
	room.AddClient(client)

	// Replay only channel 1.
	processControl(ControlMsg{Type: "replay", ChannelID: 1, LastSeq: 0}, client, room)

	msgs := drainAllControl(t, clientBuf)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message from channel 1, got %d", len(msgs))
	}
	if msgs[0].Message != "ch1-msg" {
		t.Errorf("replayed: got %q, want %q", msgs[0].Message, "ch1-msg")
	}
}

// ---------------------------------------------------------------------------
// kick is owner-only (moderator and admin cannot kick)
// ---------------------------------------------------------------------------

func TestProcessControlKickByModeratorRejected(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner

	mod, _ := newCtrlClient("mod")
	room.AddClient(mod)
	room.SetClientRole(mod.ID, RoleModerator)

	target, targetBuf, _, targetCloser := newKickableClient("bob")
	room.AddClient(target)

	processControl(ControlMsg{Type: "kick", ID: target.ID}, mod, room)

	if targetBuf.Len() != 0 {
		t.Error("moderator should not be able to kick (owner-only)")
	}
	if targetCloser.closed {
		t.Error("target connection should not be closed by moderator kick")
	}
}

func TestProcessControlKickByAdminRejected(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)
	_ = owner

	admin, _ := newAdminClient(t, "admin", room)
	target, targetBuf, _, targetCloser := newKickableClient("bob")
	room.AddClient(target)

	processControl(ControlMsg{Type: "kick", ID: target.ID}, admin, room)

	if targetBuf.Len() != 0 {
		t.Error("admin should not be able to kick (owner-only)")
	}
	if targetCloser.closed {
		t.Error("target connection should not be closed by admin kick")
	}
}

// ---------------------------------------------------------------------------
// Integration: muted user voice is blocked but chat is NOT
// ---------------------------------------------------------------------------

func TestMutedUserChatStillAllowed(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)
	room.SetClientMute(sender.ID, true, 0)

	processControl(ControlMsg{Type: "chat", Message: "hello"}, sender, room)

	// Muted users are blocked from voice (Broadcast), NOT from chat.
	if senderBuf.Len() == 0 {
		t.Error("muted user should still be able to send chat messages")
	}
	got := decodeControl(t, senderBuf)
	if got.Type != "chat" {
		t.Errorf("type: got %q, want %q", got.Type, "chat")
	}
}

// ---------------------------------------------------------------------------
// Kick with cancel context
// ---------------------------------------------------------------------------

func TestProcessControlKickCancelsContext(t *testing.T) {
	room := NewRoom()
	owner, _ := newOwnerClient(t, "owner", room)

	ctx, cancel := context.WithCancel(context.Background())
	mc := &mockCloser{}
	target := &Client{
		Username: "bob",
		session:  &mockSender{},
		ctrl:     &bytes.Buffer{},
		cancel:   cancel,
		closer:   mc,
	}
	room.AddClient(target)

	processControl(ControlMsg{Type: "kick", ID: target.ID}, owner, room)

	// Context should be canceled.
	select {
	case <-ctx.Done():
		// ok
	default:
		t.Error("kick should cancel the target's context")
	}
}
