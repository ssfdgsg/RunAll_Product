package random

import (
	"testing"
)

func TestXorShift64Star_RandNBits(t *testing.T) {
	tests := []struct {
		name     string
		bits     uint8
		maxValue uint64
	}{
		{"3 bits", 3, 7},
		{"8 bits", 8, 255},
		{"16 bits", 16, 65535},
		{"32 bits", 32, 4294967295},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rng := NewXorShift64Star(12345)
			for i := 0; i < 1000; i++ {
				val := rng.RandNBits(tt.bits)
				if val > tt.maxValue {
					t.Errorf("RandNBits(%d) = %d, want <= %d", tt.bits, val, tt.maxValue)
				}
			}
		})
	}
}

func TestXorShift64Star_Rand3Bits(t *testing.T) {
	rng := NewXorShift64Star(12345)
	for i := 0; i < 1000; i++ {
		val := rng.Rand3Bits()
		if val > 7 {
			t.Errorf("Rand3Bits() = %d, want <= 7", val)
		}
	}
}

func TestXorShift64Star_RandRange(t *testing.T) {
	rng := NewXorShift64Star(12345)
	min, max := uint64(10), uint64(20)

	for i := 0; i < 1000; i++ {
		val := rng.RandRange(min, max)
		if val < min || val >= max {
			t.Errorf("RandRange(%d, %d) = %d, want in [%d, %d)", min, max, val, min, max)
		}
	}
}

func TestXorShift64Star_ZeroSeed(t *testing.T) {
	rng := NewXorShift64Star(0)
	val := rng.Rand8Bits()
	if val == 0 && rng.Rand8Bits() == 0 && rng.Rand8Bits() == 0 {
		t.Error("Generator appears stuck at zero")
	}
}

func BenchmarkXorShift64Star_Rand3Bits(b *testing.B) {
	rng := NewXorShift64Star(12345)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rng.Rand3Bits()
	}
}

func BenchmarkXorShift64Star_RandNBits(b *testing.B) {
	rng := NewXorShift64Star(12345)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rng.RandNBits(8)
	}
}

func BenchmarkXorShift64Star_Rand64Bits(b *testing.B) {
	rng := NewXorShift64Star(12345)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rng.Rand64Bits()
	}
}
