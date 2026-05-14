package vector

import (
	"testing"
)

func TestSerializeVectorToBlob(t *testing.T) {
	v := []float32{0.1, 0.2, -0.5, 1.0, -0.001}
	blob, err := serializeVectorToBlob(v)
	if err != nil {
		t.Fatalf("serializeVectorToBlob failed: %v", err)
	}
	if len(blob) == 0 {
		t.Fatal("expected non-empty blob")
	}

	restored, err := deserializeBlobToVector(blob)
	if err != nil {
		t.Fatalf("deserializeBlobToVector failed: %v", err)
	}
	if len(restored) != len(v) {
		t.Fatalf("expected %d elements, got %d", len(v), len(restored))
	}
	for i := range v {
		if restored[i] != v[i] {
			t.Errorf("mismatch at index %d: expected %v, got %v", i, v[i], restored[i])
		}
	}
}

func TestDeserializeEmptyBlob(t *testing.T) {
	result, err := deserializeBlobToVector([]byte{})
	if err != nil {
		t.Fatalf("deserializeBlobToVector on empty slice failed: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
}

func TestSerializeVector(t *testing.T) {
	v := []float32{0.1, 0.2, -0.5}
	result, err := serializeVector(v)
	if err != nil {
		t.Fatalf("serializeVector failed: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(result))
	}
	for i, f := range result {
		if float64(v[i]) != f {
			t.Errorf("mismatch at index %d: expected %v, got %v", i, v[i], f)
		}
	}
}

func TestSerializeVectorRaw(t *testing.T) {
	v := []float32{0.1, 0.2, -0.5}
	result := serializeVectorRaw(v)
	if len(result) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(result))
	}
	for i, f := range result {
		if f != v[i] {
			t.Errorf("mismatch at index %d: expected %v, got %v", i, v[i], f)
		}
	}
}

func TestSearchInString(t *testing.T) {
	tests := []struct {
		s, substr string
		want      bool
	}{
		{"hello world", "world", true},
		{"hello world", "xyz", false},
		{"hello", "hello", true},
		{"hello", "", true},
		{"", "test", false},
	}
	for _, tt := range tests {
		got := searchInString(tt.s, tt.substr)
		if got != tt.want {
			t.Errorf("searchInString(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.want)
		}
	}
}

func TestContainsString(t *testing.T) {
	tests := []struct {
		s, substr string
		want      bool
	}{
		{"hello world", "world", true},
		{"hello world", "xyz", false},
		{"error 404 not found", "404", true},
	}
	for _, tt := range tests {
		got := containsString(tt.s, tt.substr)
		if got != tt.want {
			t.Errorf("containsString(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.want)
		}
	}
}

func TestUrlPathEscape(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"observations", "observations"},
		{"my-collection", "my-collection"},
		{"MyCollection", "MyCollection"},
		{"test_collection", "test_collection"},
	}
	for _, tt := range tests {
		got := urlPathEscape(tt.input)
		if got != tt.want {
			t.Errorf("urlPathEscape(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func BenchmarkSerializeVectorToBlob(b *testing.B) {
	v := make([]float32, 768)
	for i := range v {
		v[i] = float32(i) / 768.0
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = serializeVectorToBlob(v)
	}
}

func BenchmarkDeserializeBlobToVector(b *testing.B) {
	v := make([]float32, 768)
	for i := range v {
		v[i] = float32(i) / 768.0
	}
	blob, _ := serializeVectorToBlob(v)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = deserializeBlobToVector(blob)
	}
}
