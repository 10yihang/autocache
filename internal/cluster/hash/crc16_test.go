package hash

import "testing"

func TestCRC16(t *testing.T) {
	tests := []struct {
		input string
		want  uint16
	}{
		{"", 0},
		{"123456789", 0x31C3},
	}

	for _, tt := range tests {
		got := CRC16([]byte(tt.input))
		if got != tt.want {
			t.Errorf("CRC16(%q) = %#x, want %#x", tt.input, got, tt.want)
		}
	}
}

func TestKeySlot(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want uint16
	}{
		{"simple_foo", "foo", 12182},
		{"simple_bar", "bar", 5061},
		{"simple_hello", "hello", 866},
		// Edge cases for hash-tag parsing (Redis spec compliance)
		{"empty_hashtag", "{}", 0},             // Bug fix: empty {} should hash entire key
		{"empty_hashtag_prefix", "{}foo", 0},   // Empty {} before suffix should hash entire key
		{"normal_hashtag", "{user}:123", 5474}, // Normal case: hash only "user"
		{"nested_braces", "{{foo}}", 13308},    // First { to first } → hash "{foo"
		{"multiple_hashtags", "{a}{b}", 15495}, // Only use first pair → hash "a"
		{"unclosed_brace", "{foo", 13308},      // No closing }, hash entire key "{foo"
		{"reversed_braces", "}foo{bar", 7622},  // } before {, hash entire key "}foo{bar"
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := KeySlot(tt.key)
			if got != tt.want {
				t.Errorf("KeySlot(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestKeySlotHashTag(t *testing.T) {
	slot1 := KeySlot("{user:1000}.name")
	slot2 := KeySlot("{user:1000}.email")
	slot3 := KeySlot("{user:1000}.profile")

	if slot1 != slot2 || slot2 != slot3 {
		t.Errorf("hash tags should map to same slot: %d, %d, %d", slot1, slot2, slot3)
	}

	slotDiff := KeySlot("{user:2000}.name")
	if slotDiff == slot1 {
		t.Errorf("different hash tags should likely map to different slots")
	}
}

func TestKeySlotEmptyHashTag(t *testing.T) {
	slot1 := KeySlot("{}.foo")
	slot2 := KeySlot("{}.foo")

	if slot1 != slot2 {
		t.Errorf("empty hash tags should be consistent: %d != %d", slot1, slot2)
	}
}

func TestKeySlotNoHashTag(t *testing.T) {
	slot := KeySlot("normalkey")
	if slot >= SlotCount {
		t.Errorf("slot should be < %d, got %d", SlotCount, slot)
	}
}

func BenchmarkKeySlot(b *testing.B) {
	for i := 0; i < b.N; i++ {
		KeySlot("user:12345:profile")
	}
}

func BenchmarkKeySlotWithHashTag(b *testing.B) {
	for i := 0; i < b.N; i++ {
		KeySlot("{user:12345}.profile")
	}
}
