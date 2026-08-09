package trace

import (
	"encoding/json"
	"testing"
)

func TestDeepSanitize_TokenKeyMasked(t *testing.T) {
	input := map[string]any{"token": "abc123"}
	result := DeepSanitize(input).(map[string]any)
	if result["token"] != Mask {
		t.Errorf("token key not masked: got %v", result["token"])
	}
}

func TestDeepSanitize_PasswordKeyMasked(t *testing.T) {
	input := map[string]any{"password": "secret123"}
	result := DeepSanitize(input).(map[string]any)
	if result["password"] != Mask {
		t.Errorf("password key not masked")
	}
}

func TestDeepSanitize_SecretKeyMasked(t *testing.T) {
	input := map[string]any{"secret": "s3cret"}
	result := DeepSanitize(input).(map[string]any)
	if result["secret"] != Mask {
		t.Errorf("secret key not masked")
	}
}

func TestDeepSanitize_APIKeyMasked(t *testing.T) {
	input := map[string]any{"api_key": "sk-abc123"}
	result := DeepSanitize(input).(map[string]any)
	if result["api_key"] != Mask {
		t.Errorf("api_key not masked")
	}
}

func TestDeepSanitize_AccessTokenMasked(t *testing.T) {
	input := map[string]any{"access_token": "at-secret"}
	result := DeepSanitize(input).(map[string]any)
	if result["access_token"] != Mask {
		t.Errorf("access_token not masked")
	}
}

func TestDeepSanitize_RefreshTokenMasked(t *testing.T) {
	input := map[string]any{"refresh_token": "rt-secret"}
	result := DeepSanitize(input).(map[string]any)
	if result["refresh_token"] != Mask {
		t.Errorf("refresh_token not masked")
	}
}

func TestDeepSanitize_AuthorizationKeyMasked(t *testing.T) {
	input := map[string]any{"authorization": "Bearer xyz"}
	result := DeepSanitize(input).(map[string]any)
	if result["authorization"] != Mask {
		t.Errorf("authorization not masked")
	}
}

func TestDeepSanitize_CookieKeyMasked(t *testing.T) {
	input := map[string]any{"cookie": "session=xyz"}
	result := DeepSanitize(input).(map[string]any)
	if result["cookie"] != Mask {
		t.Errorf("cookie not masked")
	}
}

func TestDeepSanitize_CredentialKeyMasked(t *testing.T) {
	input := map[string]any{"credential": "creds"}
	result := DeepSanitize(input).(map[string]any)
	if result["credential"] != Mask {
		t.Errorf("credential not masked")
	}
}

func TestDeepSanitize_PrivateKeyMasked(t *testing.T) {
	input := map[string]any{"private_key": "pk-data"}
	result := DeepSanitize(input).(map[string]any)
	if result["private_key"] != Mask {
		t.Errorf("private_key not masked")
	}
}

func TestDeepSanitize_CaseInsensitive(t *testing.T) {
	input := map[string]any{"Token": "abc"}
	result := DeepSanitize(input).(map[string]any)
	if result["Token"] != Mask {
		t.Errorf("Token (capitalized) not masked")
	}
}

func TestDeepSanitize_SimcaSessionKeyMasked(t *testing.T) {
	input := map[string]any{"simca_session": "sess-data"}
	result := DeepSanitize(input).(map[string]any)
	if result["simca_session"] != Mask {
		t.Errorf("simca_session not masked")
	}
}

func TestDeepSanitize_PiiPrefixMasked(t *testing.T) {
	input := map[string]any{"pii_email": "test@example.com"}
	result := DeepSanitize(input).(map[string]any)
	if result["pii_email"] != Mask {
		t.Errorf("pii_email not masked")
	}
}

func TestDeepSanitize_ViolatorPrefixMasked(t *testing.T) {
	input := map[string]any{"violator_name": "Test User"}
	result := DeepSanitize(input).(map[string]any)
	if result["violator_name"] != Mask {
		t.Errorf("violator_name not masked")
	}
}

func TestDeepSanitize_NormalKeyNotMasked(t *testing.T) {
	input := map[string]any{"user_name": "John", "action": "login"}
	result := DeepSanitize(input).(map[string]any)
	if result["user_name"] != "John" {
		t.Errorf("normal key should not be masked")
	}
	if result["action"] != "login" {
		t.Errorf("normal key should not be masked")
	}
}

func TestDeepSanitize_JwtMasked(t *testing.T) {
	input := map[string]any{"data": "Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"}
	result := DeepSanitize(input).(map[string]any)
	if result["data"] == input["data"] {
		t.Errorf("JWT should be masked in value")
	}
}

func TestDeepSanitize_SimcaSessionInValueMasked(t *testing.T) {
	input := map[string]any{"data": "simca_session_abc123def456"}
	result := DeepSanitize(input).(map[string]any)
	if result["data"] == input["data"] {
		t.Errorf("simca_session should be masked in value")
	}
}

func TestDeepSanitize_EmailMasked(t *testing.T) {
	input := map[string]any{"data": "user@example.com"}
	result := DeepSanitize(input).(map[string]any)
	// Email should be masked by the email regex
	if result["data"] == "user@example.com" {
		t.Errorf("email should be masked in value")
	}
}

func TestDeepSanitize_NilValue(t *testing.T) {
	result := DeepSanitize(nil)
	if result != nil {
		t.Errorf("nil should stay nil")
	}
}

func TestDeepSanitize_IntValue(t *testing.T) {
	result := DeepSanitize(42)
	if result != 42 {
		t.Errorf("int should stay unchanged")
	}
}

func TestDeepSanitize_FloatValue(t *testing.T) {
	result := DeepSanitize(3.14)
	if result != 3.14 {
		t.Errorf("float should stay unchanged")
	}
}

func TestDeepSanitize_BoolValue(t *testing.T) {
	result := DeepSanitize(true)
	if result != true {
		t.Errorf("bool should stay unchanged")
	}
}

func TestDeepSanitize_EmptyDict(t *testing.T) {
	result := DeepSanitize(map[string]any{}).(map[string]any)
	if len(result) != 0 {
		t.Errorf("empty dict should stay empty")
	}
}

func TestDeepSanitize_ListValue(t *testing.T) {
	input := []any{"token", 42, "normal"}
	result := DeepSanitize(input).([]any)
	if len(result) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(result))
	}
	if result[1] != 42 {
		t.Errorf("int in list should stay unchanged")
	}
	if result[2] != "normal" {
		t.Errorf("normal string in list should stay unchanged")
	}
}

func TestDeepSanitize_NestedSensitive(t *testing.T) {
	input := map[string]any{
		"user": map[string]any{
			"password": "secret",
			"name":     "John",
		},
	}
	result := DeepSanitize(input).(map[string]any)
	inner := result["user"].(map[string]any)
	if inner["password"] != Mask {
		t.Errorf("nested password not masked")
	}
	if inner["name"] != "John" {
		t.Errorf("nested normal key masked incorrectly")
	}
}

func TestDeepSanitize_DeepNestingTruncated(t *testing.T) {
	// Создаём вложенность глубиной 6
	input := map[string]any{"a": map[string]any{"b": map[string]any{"c": map[string]any{"d": map[string]any{"e": map[string]any{"f": "too_deep"}}}}}}
	result := DeepSanitize(input)
	resultJSON, _ := json.Marshal(result)
	if string(resultJSON) == "" {
		t.Error("truncated result should not be empty")
	}
}

func TestDeepSanitize_ListWithSensitiveKeys(t *testing.T) {
	input := []any{
		map[string]any{"token": "secret1"},
		map[string]any{"token": "secret2"},
	}
	result := DeepSanitize(input).([]any)
	for i, item := range result {
		m := item.(map[string]any)
		if m["token"] != Mask {
			t.Errorf("list[%d]: token not masked", i)
		}
	}
}

func TestIsSensitiveKey_Token(t *testing.T) {
	if !isSensitiveKey("token") {
		t.Error("token should be sensitive")
	}
}

func TestIsSensitiveKey_Password(t *testing.T) {
	if !isSensitiveKey("password") {
		t.Error("password should be sensitive")
	}
}

func TestIsSensitiveKey_CaseInsensitive(t *testing.T) {
	if !isSensitiveKey("Token") {
		t.Error("Token should be sensitive (case-insensitive)")
	}
}

func TestIsSensitiveKey_PiiPrefix(t *testing.T) {
	if !isSensitiveKey("pii_anything") {
		t.Error("pii_ prefix should be sensitive")
	}
}

func TestIsSensitiveKey_ViolatorPrefix(t *testing.T) {
	if !isSensitiveKey("violator_something") {
		t.Error("violator_ prefix should be sensitive")
	}
}

func TestIsSensitiveKey_NormalKey(t *testing.T) {
	if isSensitiveKey("username") {
		t.Error("username should NOT be sensitive")
	}
}

func TestSanitizeString_NormalStringUnchanged(t *testing.T) {
	result := sanitizeString("hello world")
	if result != "hello world" {
		t.Errorf("normal string should not change: got %q", result)
	}
}

func TestSanitizeString_EmailRemoved(t *testing.T) {
	result := sanitizeString("Contact: user@example.com please")
	if result == "Contact: user@example.com please" {
		t.Errorf("email should be masked: got %q", result)
	}
}
