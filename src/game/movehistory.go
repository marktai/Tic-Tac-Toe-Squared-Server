package game

import ()

// TODO: move to queue
type MoveHistory [18]uint

func (m *MoveHistory) Decompress(a, b uint64) {
	compressed := [2]uint64{a, b}

	for i := 0; i < 2; i++ {
		for moveIndex := 0; moveIndex < 9; moveIndex++ {
			move := compressed[i] & 0x7F
			m[moveIndex+9*i] = uint(move)
			compressed[i] >>= 7
		}
	}
}

func (m *MoveHistory) Compress() (uint64, uint64) {
	var compressed [2]uint64
	j := 1
	for i := 17; i >= 0; i-- {
		if i == 8 {
			j -= 1
		}
		compressed[j] *= 128
		compressed[j] += uint64(m[i])
	}
	return compressed[0], compressed[1]
}

func (m *MoveHistory) AddMove(move uint) {
	for i := 17; i >= 1; i-- {
		m[i] = m[i-1]
	}
	m[0] = move
}
