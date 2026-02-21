package main

import (
	"testing"
)

// ---------------------------------------------------------------------------
// validateName
// ---------------------------------------------------------------------------

func TestValidateNameValid(t *testing.T) {
	name, err := validateName("alice", MaxNameLength)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "alice" {
		t.Errorf("got %q, want %q", name, "alice")
	}
}

func TestValidateNameTrimWhitespace(t *testing.T) {
	name, err := validateName("  alice  ", MaxNameLength)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "alice" {
		t.Errorf("got %q, want %q", name, "alice")
	}
}

func TestValidateNameEmpty(t *testing.T) {
	_, err := validateName("", MaxNameLength)
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestValidateNameWhitespaceOnly(t *testing.T) {
	_, err := validateName("   ", MaxNameLength)
	if err == nil {
		t.Error("expected error for whitespace-only name")
	}
}

func TestValidateNameTabsOnly(t *testing.T) {
	_, err := validateName("\t\t", MaxNameLength)
	if err == nil {
		t.Error("expected error for tabs-only name")
	}
}

func TestValidateNameExactMaxLen(t *testing.T) {
	name := "12345678901234567890123456789012345678901234567890" // 50 chars
	got, err := validateName(name, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != name {
		t.Errorf("got %q, want %q", got, name)
	}
}

func TestValidateNameExceedsMaxLen(t *testing.T) {
	name := "123456789012345678901234567890123456789012345678901" // 51 chars
	_, err := validateName(name, 50)
	if err == nil {
		t.Error("expected error for name exceeding max length")
	}
}

func TestValidateNameTrimmedExceedsMaxLen(t *testing.T) {
	// After trimming, this is still 51 chars.
	name := "  123456789012345678901234567890123456789012345678901  "
	_, err := validateName(name, 50)
	if err == nil {
		t.Error("expected error for trimmed name exceeding max length")
	}
}

func TestValidateNameTrimmedFitsMaxLen(t *testing.T) {
	// After trimming, this is exactly 50 chars.
	name := "  12345678901234567890123456789012345678901234567890  "
	got, err := validateName(name, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "12345678901234567890123456789012345678901234567890" {
		t.Errorf("got %q, want trimmed name", got)
	}
}

func TestValidateNameCustomMaxLen(t *testing.T) {
	_, err := validateName("abcdef", 5)
	if err == nil {
		t.Error("expected error for name exceeding custom max length of 5")
	}

	got, err := validateName("abcde", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "abcde" {
		t.Errorf("got %q, want %q", got, "abcde")
	}
}

func TestValidateNameUTF8(t *testing.T) {
	// UTF-8 multi-byte characters — validateName checks len in bytes.
	name := "日本語" // 9 bytes in UTF-8
	got, err := validateName(name, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != name {
		t.Errorf("got %q, want %q", got, name)
	}
}

func TestValidateNameNewlines(t *testing.T) {
	// Newlines are not trimmed by TrimSpace, but leading/trailing are.
	name := " hello\nworld "
	got, err := validateName(name, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello\nworld" {
		t.Errorf("got %q, want %q", got, "hello\nworld")
	}
}

// ---------------------------------------------------------------------------
// DefaultVideoLayers
// ---------------------------------------------------------------------------

func TestDefaultVideoLayersCount(t *testing.T) {
	layers := DefaultVideoLayers()
	if len(layers) != 3 {
		t.Fatalf("expected 3 layers, got %d", len(layers))
	}
}

func TestDefaultVideoLayersQualities(t *testing.T) {
	layers := DefaultVideoLayers()
	expected := []string{"high", "medium", "low"}
	for i, l := range layers {
		if l.Quality != expected[i] {
			t.Errorf("layer %d: quality %q, want %q", i, l.Quality, expected[i])
		}
	}
}

func TestDefaultVideoLayersDescendingResolution(t *testing.T) {
	layers := DefaultVideoLayers()
	for i := 1; i < len(layers); i++ {
		if layers[i].Width >= layers[i-1].Width {
			t.Errorf("width should decrease: layer %d (%d) >= layer %d (%d)",
				i, layers[i].Width, i-1, layers[i-1].Width)
		}
		if layers[i].Height >= layers[i-1].Height {
			t.Errorf("height should decrease: layer %d (%d) >= layer %d (%d)",
				i, layers[i].Height, i-1, layers[i-1].Height)
		}
		if layers[i].Bitrate >= layers[i-1].Bitrate {
			t.Errorf("bitrate should decrease: layer %d (%d) >= layer %d (%d)",
				i, layers[i].Bitrate, i-1, layers[i-1].Bitrate)
		}
	}
}

func TestDefaultVideoLayersNonZeroValues(t *testing.T) {
	for _, l := range DefaultVideoLayers() {
		if l.Width <= 0 {
			t.Errorf("layer %q: width should be > 0, got %d", l.Quality, l.Width)
		}
		if l.Height <= 0 {
			t.Errorf("layer %q: height should be > 0, got %d", l.Quality, l.Height)
		}
		if l.Bitrate <= 0 {
			t.Errorf("layer %q: bitrate should be > 0, got %d", l.Quality, l.Bitrate)
		}
	}
}

func TestDefaultVideoLayersHighIs720p(t *testing.T) {
	layers := DefaultVideoLayers()
	if layers[0].Width != 1280 || layers[0].Height != 720 {
		t.Errorf("high layer: got %dx%d, want 1280x720", layers[0].Width, layers[0].Height)
	}
}

// ---------------------------------------------------------------------------
// roleLevel
// ---------------------------------------------------------------------------

func TestRoleLevelOwner(t *testing.T) {
	if roleLevel(RoleOwner) != 4 {
		t.Errorf("OWNER level: got %d, want 4", roleLevel(RoleOwner))
	}
}

func TestRoleLevelAdmin(t *testing.T) {
	if roleLevel(RoleAdmin) != 3 {
		t.Errorf("ADMIN level: got %d, want 3", roleLevel(RoleAdmin))
	}
}

func TestRoleLevelModerator(t *testing.T) {
	if roleLevel(RoleModerator) != 2 {
		t.Errorf("MODERATOR level: got %d, want 2", roleLevel(RoleModerator))
	}
}

func TestRoleLevelUser(t *testing.T) {
	if roleLevel(RoleUser) != 1 {
		t.Errorf("USER level: got %d, want 1", roleLevel(RoleUser))
	}
}

func TestRoleLevelUnknown(t *testing.T) {
	if roleLevel("UNKNOWN") != 0 {
		t.Errorf("unknown role level: got %d, want 0", roleLevel("UNKNOWN"))
	}
}

func TestRoleLevelEmptyString(t *testing.T) {
	if roleLevel("") != 0 {
		t.Errorf("empty role level: got %d, want 0", roleLevel(""))
	}
}

func TestRoleLevelOrdering(t *testing.T) {
	if !(roleLevel(RoleOwner) > roleLevel(RoleAdmin)) {
		t.Error("OWNER should be higher than ADMIN")
	}
	if !(roleLevel(RoleAdmin) > roleLevel(RoleModerator)) {
		t.Error("ADMIN should be higher than MODERATOR")
	}
	if !(roleLevel(RoleModerator) > roleLevel(RoleUser)) {
		t.Error("MODERATOR should be higher than USER")
	}
	if !(roleLevel(RoleUser) > roleLevel("")) {
		t.Error("USER should be higher than unknown")
	}
}

// ---------------------------------------------------------------------------
// HasPermission — comprehensive role × action matrix
// ---------------------------------------------------------------------------

func TestHasPermissionKick(t *testing.T) {
	tests := []struct {
		role string
		want bool
	}{
		{RoleOwner, true},
		{RoleAdmin, true},
		{RoleModerator, true},
		{RoleUser, false},
		{"", false},
	}
	for _, tt := range tests {
		got := HasPermission(tt.role, "kick")
		if got != tt.want {
			t.Errorf("HasPermission(%q, %q) = %v, want %v", tt.role, "kick", got, tt.want)
		}
	}
}

func TestHasPermissionBan(t *testing.T) {
	tests := []struct {
		role string
		want bool
	}{
		{RoleOwner, true},
		{RoleAdmin, true},
		{RoleModerator, false},
		{RoleUser, false},
		{"", false},
	}
	for _, tt := range tests {
		got := HasPermission(tt.role, "ban")
		if got != tt.want {
			t.Errorf("HasPermission(%q, %q) = %v, want %v", tt.role, "ban", got, tt.want)
		}
	}
}

func TestHasPermissionUnban(t *testing.T) {
	tests := []struct {
		role string
		want bool
	}{
		{RoleOwner, true},
		{RoleAdmin, true},
		{RoleModerator, false},
		{RoleUser, false},
	}
	for _, tt := range tests {
		got := HasPermission(tt.role, "unban")
		if got != tt.want {
			t.Errorf("HasPermission(%q, %q) = %v, want %v", tt.role, "unban", got, tt.want)
		}
	}
}

func TestHasPermissionMuteUnmute(t *testing.T) {
	for _, action := range []string{"mute", "unmute"} {
		tests := []struct {
			role string
			want bool
		}{
			{RoleOwner, true},
			{RoleAdmin, true},
			{RoleModerator, false},
			{RoleUser, false},
		}
		for _, tt := range tests {
			got := HasPermission(tt.role, action)
			if got != tt.want {
				t.Errorf("HasPermission(%q, %q) = %v, want %v", tt.role, action, got, tt.want)
			}
		}
	}
}

func TestHasPermissionDeleteAnyMessage(t *testing.T) {
	tests := []struct {
		role string
		want bool
	}{
		{RoleOwner, true},
		{RoleAdmin, true},
		{RoleModerator, true},
		{RoleUser, false},
	}
	for _, tt := range tests {
		got := HasPermission(tt.role, "delete_any_message")
		if got != tt.want {
			t.Errorf("HasPermission(%q, %q) = %v, want %v", tt.role, "delete_any_message", got, tt.want)
		}
	}
}

func TestHasPermissionPinMessage(t *testing.T) {
	tests := []struct {
		role string
		want bool
	}{
		{RoleOwner, true},
		{RoleAdmin, true},
		{RoleModerator, true},
		{RoleUser, false},
	}
	for _, tt := range tests {
		got := HasPermission(tt.role, "pin_message")
		if got != tt.want {
			t.Errorf("HasPermission(%q, %q) = %v, want %v", tt.role, "pin_message", got, tt.want)
		}
	}
}

func TestHasPermissionManageChannels(t *testing.T) {
	tests := []struct {
		role string
		want bool
	}{
		{RoleOwner, true},
		{RoleAdmin, true},
		{RoleModerator, false},
		{RoleUser, false},
	}
	for _, tt := range tests {
		got := HasPermission(tt.role, "manage_channels")
		if got != tt.want {
			t.Errorf("HasPermission(%q, %q) = %v, want %v", tt.role, "manage_channels", got, tt.want)
		}
	}
}

func TestHasPermissionOwnerOnlyActions(t *testing.T) {
	ownerOnly := []string{"set_role", "server_settings", "announce", "set_slow_mode"}
	for _, action := range ownerOnly {
		if !HasPermission(RoleOwner, action) {
			t.Errorf("OWNER should have permission for %q", action)
		}
		if HasPermission(RoleAdmin, action) {
			t.Errorf("ADMIN should NOT have permission for %q", action)
		}
		if HasPermission(RoleModerator, action) {
			t.Errorf("MODERATOR should NOT have permission for %q", action)
		}
		if HasPermission(RoleUser, action) {
			t.Errorf("USER should NOT have permission for %q", action)
		}
	}
}

func TestHasPermissionUnknownAction(t *testing.T) {
	// Unknown actions require at least USER level.
	if !HasPermission(RoleUser, "some_unknown_action") {
		t.Error("USER should have permission for unknown action (default case)")
	}
	if !HasPermission(RoleOwner, "some_unknown_action") {
		t.Error("OWNER should have permission for unknown action")
	}
	if HasPermission("", "some_unknown_action") {
		t.Error("empty role should NOT have permission for unknown action")
	}
}

func TestHasPermissionUnknownRole(t *testing.T) {
	if HasPermission("SUPERUSER", "kick") {
		t.Error("unknown role should not have permission for kick")
	}
	// But unknown roles have level 0, which is below USER (1), so even default actions fail.
	if HasPermission("SUPERUSER", "chat") {
		t.Error("unknown role should not have permission for default actions")
	}
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

func TestConstants(t *testing.T) {
	if MaxNameLength != 50 {
		t.Errorf("MaxNameLength: got %d, want 50", MaxNameLength)
	}
	if MaxChatLength != 500 {
		t.Errorf("MaxChatLength: got %d, want 500", MaxChatLength)
	}
	if MaxFileSize != 10*1024*1024 {
		t.Errorf("MaxFileSize: got %d, want %d", MaxFileSize, 10*1024*1024)
	}
}

func TestRoleConstants(t *testing.T) {
	if RoleOwner != "OWNER" {
		t.Errorf("RoleOwner: got %q, want %q", RoleOwner, "OWNER")
	}
	if RoleAdmin != "ADMIN" {
		t.Errorf("RoleAdmin: got %q, want %q", RoleAdmin, "ADMIN")
	}
	if RoleModerator != "MODERATOR" {
		t.Errorf("RoleModerator: got %q, want %q", RoleModerator, "MODERATOR")
	}
	if RoleUser != "USER" {
		t.Errorf("RoleUser: got %q, want %q", RoleUser, "USER")
	}
}
