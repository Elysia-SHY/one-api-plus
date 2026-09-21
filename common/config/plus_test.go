package config

import "testing"

func TestSetFallbackModels(t *testing.T) {
	if err := SetFallbackModels(`{"gpt-4o":["claude-3-5-sonnet-20241022","deepseek-chat"]}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := GetFallbackModels("gpt-4o")
	if len(got) != 2 || got[0] != "claude-3-5-sonnet-20241022" {
		t.Fatalf("unexpected fallback list: %+v", got)
	}
	if GetFallbackModels("unknown") != nil {
		t.Fatal("unknown model should have no fallback")
	}
	if err := SetFallbackModels(""); err != nil {
		t.Fatalf("empty payload should be accepted: %v", err)
	}
	if len(AllFallbackModels()) != 0 {
		t.Fatal("fallback map should be empty after reset")
	}
	if err := SetFallbackModels("{not json"); err == nil {
		t.Fatal("invalid json should return an error")
	}
}
