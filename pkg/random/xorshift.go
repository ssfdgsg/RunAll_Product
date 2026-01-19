package random

// XorShift64Star 是一个快速的伪随机数生成器
// 注意：不适用于加密场景，仅用于高性能随机数生成
type XorShift64Star struct {
	s uint64
}

// NewXorShift64Star 创建一个新的随机数生成器
// seed: 种子值，如果为 0 则使用默认种子
func NewXorShift64Star(seed uint64) *XorShift64Star {
	if seed == 0 {
		seed = 0x9e3779b97f4a7c15 // 避免零状态
	}
	return &XorShift64Star{s: seed}
}

// next64 生成下一个 64 位随机数
func (r *XorShift64Star) next64() uint64 {
	x := r.s
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	r.s = x
	return x * 2685821657736338717
}

// RandNBits 生成指定位数的随机数
// bits: 位数 (1-64)，返回值范围为 [0, 2^bits-1]
// 例如: bits=3 返回 0..7, bits=8 返回 0..255
func (r *XorShift64Star) RandNBits(bits uint8) uint64 {
	if bits == 0 {
		return 0
	}
	if bits >= 64 {
		return r.next64()
	}
	mask := (uint64(1) << bits) - 1
	return r.next64() & mask
}

// Rand3Bits 生成 3 位随机数 (0..7)
// 这是一个快捷方法，等价于 RandNBits(3)
func (r *XorShift64Star) Rand3Bits() uint8 {
	return uint8(r.next64() & 7)
}

// Rand8Bits 生成 8 位随机数 (0..255)
func (r *XorShift64Star) Rand8Bits() uint8 {
	return uint8(r.next64() & 0xFF)
}

// Rand16Bits 生成 16 位随机数 (0..65535)
func (r *XorShift64Star) Rand16Bits() uint16 {
	return uint16(r.next64() & 0xFFFF)
}

// Rand32Bits 生成 32 位随机数
func (r *XorShift64Star) Rand32Bits() uint32 {
	return uint32(r.next64() & 0xFFFFFFFF)
}

// Rand64Bits 生成 64 位随机数
func (r *XorShift64Star) Rand64Bits() uint64 {
	return r.next64()
}

// RandRange 生成指定范围内的随机数 [min, max)
// 注意：max 不包含在内
func (r *XorShift64Star) RandRange(min, max uint64) uint64 {
	if min >= max {
		return min
	}
	return min + r.next64()%(max-min)
}
