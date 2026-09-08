package handler

import (
	"testing"

	"github-cmdb/internal/model"
)

func TestLoginResponseIncludesAuthenticatedUserIdentity(t *testing.T) {
	user := &model.User{
		ID:          42,
		Username:    "alice",
		DisplayName: "Alice Chen",
		Roles:       model.JSONArray{"asset_mgr"},
	}

	response := loginResponse(user, "token-123")

	if response["token"] != "token-123" {
		t.Fatalf("token = %v, want token-123", response["token"])
	}
	if response["user_id"] != uint64(42) {
		t.Fatalf("user_id = %v, want 42", response["user_id"])
	}
	if response["username"] != "alice" {
		t.Fatalf("username = %v, want alice", response["username"])
	}
	if response["display_name"] != "Alice Chen" {
		t.Fatalf("display_name = %v, want Alice Chen", response["display_name"])
	}
	if roles, ok := response["roles"].(model.JSONArray); !ok || len(roles) != 1 || roles[0] != "asset_mgr" {
		t.Fatalf("roles = %v, want [asset_mgr]", response["roles"])
	}
}
