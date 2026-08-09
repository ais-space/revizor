package trace

import (
	"testing"
)

func TestValidatePath_ValidSimple(t *testing.T) {
	if !ValidatePath("auth.callback.enter") {
		t.Error("auth.callback.enter should be valid")
	}
}

func TestValidatePath_ValidWithUnderscores(t *testing.T) {
	if !ValidatePath("auth.callback.oauth_start") {
		t.Error("path with underscores should be valid")
	}
}

func TestValidatePath_ValidNumbers(t *testing.T) {
	if !ValidatePath("module.v1.method") {
		t.Error("path with numbers should be valid")
	}
}

func TestValidatePath_ValidGlob(t *testing.T) {
	if !ValidatePath("auth.**") {
		t.Error("path with ** should be valid")
	}
	if !ValidatePath("auth.*.start") {
		t.Error("path with * should be valid")
	}
}

func TestValidatePath_EmptyString(t *testing.T) {
	if ValidatePath("") {
		t.Error("empty path should be invalid")
	}
}

func TestValidatePath_UppercaseRejected(t *testing.T) {
	if ValidatePath("Auth.Callback.Enter") {
		t.Error("uppercase path should be rejected")
	}
}

func TestValidatePath_CyrillicRejected(t *testing.T) {
	if ValidatePath("модуль.компонент.событие") {
		t.Error("cyrillic path should be rejected")
	}
}

func TestValidatePath_SpacesRejected(t *testing.T) {
	if ValidatePath("auth. callback.enter") {
		t.Error("path with spaces should be rejected")
	}
}

func TestValidatePath_HyphensRejected(t *testing.T) {
	if ValidatePath("auth.callback-enter") {
		t.Error("path with hyphens should be rejected")
	}
}

func TestValidatePath_MaxLengthAllowed(t *testing.T) {
	path := ""
	for i := 0; i < MaxPathLength; i++ {
		path += "a"
	}
	if !ValidatePath(path) {
		t.Error("path at max length should be valid")
	}
}

func TestValidatePath_TooLongRejected(t *testing.T) {
	path := ""
	for i := 0; i < MaxPathLength+1; i++ {
		path += "a"
	}
	if ValidatePath(path) {
		t.Error("path exceeding max length should be rejected")
	}
}

func TestGlobMatch_ExactMatch(t *testing.T) {
	if !GlobMatch("auth.callback.enter", "auth.callback.enter") {
		t.Error("exact match should succeed")
	}
}

func TestGlobMatch_ExactNoMatch(t *testing.T) {
	if GlobMatch("auth.callback.enter", "auth.callback.exit") {
		t.Error("different path should not match")
	}
}
func TestGlobMatch_DoubleStarMatchesAll(t *testing.T) {
	if !GlobMatch("auth.callback.enter", "auth.**") {
		t.Error("** should match anything under auth")
	}
	if !GlobMatch("auth.x.y.z", "auth.**") {
		t.Error("** should match deep path")
	}
}

func TestGlobMatch_DoubleStarNoMatchDifferentPrefix(t *testing.T) {
	if GlobMatch("other.callback.enter", "auth.**") {
		t.Error("** should not match different prefix")
	}
}

func TestGlobMatch_SingleStarMatchesOneSegment(t *testing.T) {
	if !GlobMatch("auth.callback.enter", "auth.*.enter") {
		t.Error("* should match one segment")
	}
	if !GlobMatch("auth.oauth.enter", "auth.*.enter") {
		t.Error("* should match any one segment")
	}
}

func TestGlobMatch_SingleStarNoMatchMultipleSegments(t *testing.T) {
	if GlobMatch("auth.callback.deep.enter", "auth.*.enter") {
		t.Error("* should NOT match multiple segments")
	}
}

func TestGlobMatch_SingleStarNoMatchShortPath(t *testing.T) {
	if GlobMatch("auth.enter", "auth.*.enter") {
		t.Error("* should NOT match zero segments")
	}
}

func TestGlobMatch_TrailingDoubleStar(t *testing.T) {
	if !GlobMatch("elevation.start.go", "elevation.**") {
		t.Error("trailing ** should match everything")
	}
}

func TestGlobMatch_LeadingDoubleStar(t *testing.T) {
	if !GlobMatch("something.elevation.check", "**.elevation.check") {
		t.Error("leading ** should match any prefix")
	}
}

func TestGlobMatch_MultipleStars(t *testing.T) {
	if !GlobMatch("auth.oauth.callback.enter", "auth.*.*.enter") {
		t.Error("multiple * should match multiple segments")
	}
}

func TestGlobMatch_MultipleStarsNoMatch(t *testing.T) {
	if GlobMatch("auth.oauth.enter", "auth.*.*.enter") {
		t.Error("multiple * should not match fewer segments")
	}
}

func TestShouldTrace_ExactEnabled(t *testing.T) {
	ResetCache()
	SetCacheForTest("test-sid", map[string]bool{"auth.callback.enter": true}, nil)

	if !ShouldTrace("auth.callback.enter", "test-sid") {
		t.Error("exact path should be traced when enabled")
	}
}

func TestShouldTrace_ExactDisabled(t *testing.T) {
	ResetCache()
	SetCacheForTest("test-sid", map[string]bool{"auth.callback.enter": false}, nil)

	if ShouldTrace("auth.callback.enter", "test-sid") {
		t.Error("exact path should NOT be traced when disabled")
	}
}

func TestShouldTrace_GlobDoubleStar(t *testing.T) {
	ResetCache()
	SetCacheForTest("test-sid", map[string]bool{"auth.**": true}, nil)

	if !ShouldTrace("auth.oauth.callback.enter", "test-sid") {
		t.Error("** glob should match")
	}
}

func TestShouldTrace_GlobNotMatching(t *testing.T) {
	ResetCache()
	SetCacheForTest("test-sid", map[string]bool{"auth.**": true}, nil)

	if ShouldTrace("other.module.enter", "test-sid") {
		t.Error("non-matching glob should return false")
	}
}

func TestShouldTrace_NotInConfigReturnsFalse(t *testing.T) {
	ResetCache()
	SetCacheForTest("test-sid", map[string]bool{}, nil)

	if ShouldTrace("any.path.here", "test-sid") {
		t.Error("should return false when path not in config")
	}
}

func TestShouldTrace_ExcludeOverridesInclude(t *testing.T) {
	ResetCache()
	SetCacheForTest("test-sid",
		map[string]bool{"auth.**": true},
		map[string]bool{"auth.callback.normal": true},
	)

	if ShouldTrace("auth.callback.normal", "test-sid") {
		t.Error("exclude should override include")
	}
}

func TestShouldTrace_ExcludeDoesNotBlockOthers(t *testing.T) {
	ResetCache()
	SetCacheForTest("test-sid",
		map[string]bool{"auth.**": true},
		map[string]bool{"auth.callback.normal": true},
	)

	if !ShouldTrace("auth.callback.special", "test-sid") {
		t.Error("exclude of one path should not block other paths")
	}
}

func TestShouldTrace_ExcludeGlobPattern(t *testing.T) {
	ResetCache()
	SetCacheForTest("test-sid",
		map[string]bool{"auth.**": true},
		map[string]bool{"auth.callback.*": true},
	)

	if ShouldTrace("auth.callback.anything", "test-sid") {
		t.Error("exclude glob should block all matching paths")
	}
}

func TestShouldTrace_DifferentSession(t *testing.T) {
	ResetCache()
	SetCacheForTest("sid-A", map[string]bool{"auth.**": true}, nil)
	SetCacheForTest("sid-B", map[string]bool{}, nil)

	if ShouldTrace("auth.module.enter", "sid-B") {
		t.Error("session B should not see session A config")
	}
}

func TestInvalidateCache_InvalidateAll(t *testing.T) {
	ResetCache()
	SetCacheForTest("sid-A", map[string]bool{"test": true}, nil)
	SetCacheForTest("sid-B", map[string]bool{"test": true}, nil)

	InvalidateCache("")

	if ShouldTrace("test", "sid-A") || ShouldTrace("test", "sid-B") {
		t.Error("InvalidateCache() should clear all")
	}
}

func TestInvalidateCache_InvalidateSpecificSession(t *testing.T) {
	ResetCache()
	SetCacheForTest("sid-A", map[string]bool{"test": true}, nil)
	SetCacheForTest("sid-B", map[string]bool{"test": true}, nil)

	InvalidateCache("sid-A")

	if ShouldTrace("test", "sid-A") {
		t.Error("sid-A should be invalidated")
	}
	if !ShouldTrace("test", "sid-B") {
		t.Error("sid-B should NOT be affected")
	}
}
