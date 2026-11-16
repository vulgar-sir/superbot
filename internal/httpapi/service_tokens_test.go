package httpapi

import "testing"

func TestHashServiceToken(t *testing.T) {
	first := hashServiceToken("sbx_abc")
	second := hashServiceToken("sbx_abc")
	if first != second {
		t.Fatalf("hash is not deterministic: %s vs %s", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("expected sha256 hex length 64, got %d", len(first))
	}
	if other := hashServiceToken("sbx_abd"); other == first {
		t.Fatal("different tokens must not collide")
	}
}
