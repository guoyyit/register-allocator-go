package alloc

import "testing"

const manyParams = `
func sum6
param %1
param %2
param %3
param %4
param %5
param %6
entry:
	%7 = add %1, %2
	%8 = add %7, %3
	%9 = add %8, %4
	%10 = add %9, %5
	%11 = add %10, %6
	ret %11

func main
entry:
	%20 = call sum6(1, 2, 3, 4, 5, 6)
	ret %20
`

func TestManyParams(t *testing.T) {
	diffCheck(t, manyParams, []int{6, 8})
}

const immEdges = `
func main
entry:
	%1 = const -7
	%2 = add %1, -3
	%3 = mul %2, 2
	store 0, %3
	ret %3
`

func TestImmediateEdges(t *testing.T) {
	diffCheck(t, immEdges, []int{3, 4})
}

const immRet = `
func main
entry:
	%1 = const 5
	ret 42
`

func TestImmediateReturn(t *testing.T) {
	diffCheck(t, immRet, []int{3, 4})
}
