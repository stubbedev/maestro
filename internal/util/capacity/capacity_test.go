package capacity

import (
	"math"
	"testing"
)

func TestSum(t *testing.T) {
	tests := []struct {
		terms []int
		want  int
	}{
		{nil, 0},
		{[]int{3, 4, 5}, 12},
		{[]int{-1, 7}, 7},
		{[]int{math.MaxInt - 1, 1}, math.MaxInt},
		{[]int{math.MaxInt - 1, 2, 5}, math.MaxInt - 1},
		{[]int{8, math.MaxInt}, math.MaxInt},
		{[]int{math.MaxInt / 2, math.MaxInt / 2, math.MaxInt / 2}, math.MaxInt / 2},
	}

	for _, tt := range tests {
		if got := Sum(tt.terms...); got != tt.want {
			t.Errorf("Sum(%v) = %d, want %d", tt.terms, got, tt.want)
		}
	}
}

func TestProduct(t *testing.T) {
	tests := []struct {
		count, size, want int
	}{
		{0, 5, 0},
		{5, 0, 0},
		{-2, 5, 0},
		{5, -2, 0},
		{6, 7, 42},
		{math.MaxInt, 1, math.MaxInt},
		{math.MaxInt/2 + 1, 2, 0},
		{2, math.MaxInt/2 + 1, 0},
	}

	for _, tt := range tests {
		if got := Product(tt.count, tt.size); got != tt.want {
			t.Errorf("Product(%d, %d) = %d, want %d", tt.count, tt.size, got, tt.want)
		}
	}
}
