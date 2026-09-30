package alloc

import "testing"

// A countdown loop that accumulates into memory; exercises back-edges and
// liveness merges at the loop header.
const loopProg = `
func main
entry:
	%1 = const 5
	%2 = const 0
	%3 = const 3
	store 0, %2
	store 1, %3
	br head
head:
	%4 = load 0
	%5 = add %4, %1
	store 0, %5
	%6 = load 1
	%7 = sub %6, %1
	store 1, %7
	%8 = cmplt %2, %7
	brcond %8, body, done
body:
	%9 = add %6, %2
	store 2, %9
	br head
done:
	%10 = load 0
	ret %10
`

func TestDiffLoop(t *testing.T) {
	diffCheck(t, loopProg, []int{3, 4, 6})
}

// Multiple calls with values live across several call sites.
const crossCallProg = `
func id
param %1
entry:
	ret %1

func add
param %1
param %2
entry:
	%3 = add %1, %2
	ret %3

func main
entry:
	%10 = const 4
	%11 = const 5
	%12 = call id(%10)
	%13 = add %11, %12
	%14 = call add(%13, %10)
	store 0, %14
	%15 = call id(%11)
	%16 = add %14, %15
	ret %16
`

func TestDiffCrossCall(t *testing.T) {
	diffCheck(t, crossCallProg, []int{3, 4, 6})
}

// Nested control flow where the same value is live into both branches and
// used after the merge.
const diamondProg = `
func main
entry:
	%1 = const 6
	%2 = const 2
	%3 = cmplt %2, %1
	brcond %3, big, small
big:
	%4 = mul %1, %2
	br join
small:
	%5 = sub %1, %2
	br join
join:
	%6 = cmplt %1, %2
	brcond %6, neg, pos
neg:
	store 0, %1
	br out
pos:
	store 0, %2
	br out
out:
	%7 = load 0
	ret %7
`

func TestDiffDiamond(t *testing.T) {
	diffCheck(t, diamondProg, []int{3, 4})
}
