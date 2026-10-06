package codegen

// Regression tests for branch forms: a value used by a conditional branch
// and still live in successor blocks must reach every successor with its
// current value, at any physical register count. Every test executes all
// reachable paths (via several argument sets) and requires the target
// machine's final state — return value and full memory, which carries the
// exit-live values the programs store — to match the IR interpreter exactly.

import (
	"fmt"
	"strings"
	"testing"
)

// runPaths checks interpreter/target equivalence for every argument set at
// every register count in ks.
func runPaths(t *testing.T, src string, ks []int, argSets ...[]int64) {
	t.Helper()
	for _, k := range ks {
		for _, args := range argSets {
			runEquivalent(t, src, "main", k, args)
		}
	}
}

var allK = []int{2, 3, 4, 5, 8}

// The branch condition is defined in the branching block and read again in
// both successors. The successors must not reload a stale slot copy.
func TestBranchConditionLiveIntoSuccessors(t *testing.T) {
	src := `
func main(v0):
  entry:
    v1 = 5
    v2 = cmplt v0, v1
    br v2, yes, no
  yes:
    v3 = add v2, v2
    ret v3
  no:
    ret v2
`
	runPaths(t, src, allK, []int64{3}, []int64{9}, []int64{5})
}

// Branch-dense chain: the same condition value steers several consecutive
// branches and is read in ordinary instructions in later blocks.
func TestBranchConditionChain(t *testing.T) {
	src := `
func main(v0):
  entry:
    v1 = 8
    v2 = cmplt v0, v1
    br v2, l1, l2
  l1:
    v3 = add v2, v0
    br v2, l2, l3
  l2:
    v4 = add v2, v2
    br v2, l3, l4
  l3:
    v5 = add v2, v1
    ret v5
  l4:
    v6 = sub v1, v2
    ret v6
`
	runPaths(t, src, allK, []int64{3}, []int64{12})
}

// Nested branches: conditions of outer and inner branches all stay live and
// are read again inside the innermost successors.
func TestNestedBranches(t *testing.T) {
	src := `
func main(v0):
  entry:
    v1 = 10
    v2 = cmplt v0, v1
    br v2, small, big
  small:
    v3 = 0
    v4 = cmplt v3, v0
    br v4, pos, neg
  pos:
    v5 = mul v2, v4
    v6 = add v5, v0
    ret v6
  neg:
    v7 = sub v2, v4
    ret v7
  big:
    v8 = 100
    v9 = cmplt v8, v0
    br v9, huge, mid
  huge:
    v10 = add v2, v9
    ret v10
  mid:
    v11 = add v2, v0
    ret v11
`
	runPaths(t, src, allK, []int64{5}, []int64{-3}, []int64{50}, []int64{200})
}

// Multi-predecessor merge: a value defined along both arms becomes a branch
// condition after the merge and is read again in a successor.
func TestMergeValueBecomesCondition(t *testing.T) {
	src := `
func main(v0):
  entry:
    v1 = 5
    v2 = cmplt v0, v1
    br v2, small, big
  small:
    v3 = 1
    jmp merge
  big:
    v3 = 0
    jmp merge
  merge:
    br v3, one, zero
  one:
    v4 = add v3, v0
    ret v4
  zero:
    ret v3
`
	runPaths(t, src, allK, []int64{3}, []int64{9})
}

// The condition value stays live across a function call inside a successor;
// the caller-saved convention must preserve it.
func TestBranchConditionLiveAcrossCall(t *testing.T) {
	src := `
func callee(v0):
  entry:
    v1 = 2
    v2 = mul v0, v1
    ret v2

func main(v0):
  entry:
    v1 = 5
    v2 = cmplt v0, v1
    br v2, yes, no
  yes:
    v3 = callee(v0)
    v4 = add v3, v2
    ret v4
  no:
    v5 = callee(v2)
    ret v5
`
	runPaths(t, src, allK, []int64{3}, []int64{9})
}

// A loop condition is read inside the loop body and again after the loop
// exit, across every back-edge.
func TestLoopConditionLiveInBodyAndAfterExit(t *testing.T) {
	src := `
func main(v0):
  entry:
    v1 = 0
    v2 = 5
    jmp head
  head:
    v3 = cmplt v1, v2
    br v3, body, exit
  body:
    v4 = add v3, v1
    v5 = add v0, v4
    v0 = v5
    v6 = 1
    v1 = add v1, v6
    jmp head
  exit:
    v7 = add v3, v0
    ret v7
`
	runPaths(t, src, allK, []int64{10}, []int64{0})
}

// A back-edge to the entry block must not re-home the parameters: the
// parameter-homing prologue runs exactly once per call.
func TestEntryBackEdge(t *testing.T) {
	src := `
func main(v0):
  entry:
    v1 = 10
    v2 = cmplt v0, v1
    br v2, again, done
  again:
    v3 = 1
    v0 = add v0, v3
    jmp entry
  done:
    ret v0
`
	runPaths(t, src, allK, []int64{3}, []int64{10}, []int64{20})
}

// Two parameters, a back-edge to the entry block, and a branch condition
// that survives the loop: both the prologue and the edge flushes must be
// correct at once.
func TestEntryBackEdgeTwoParams(t *testing.T) {
	src := `
func main(v0, v1):
  entry:
    v2 = cmplt v0, v1
    br v2, again, done
  again:
    v3 = add v0, v2
    v0 = v3
    jmp entry
  done:
    v4 = add v2, v0
    ret v4
`
	runPaths(t, src, allK, []int64{0, 4}, []int64{5, 2})
}

// Branching under extreme register pressure: many values stay live across
// every edge, the branch condition is reused in both arms, and exit-live
// values are stored to memory so the final memory comparison observes them.
func TestBranchesUnderPressure(t *testing.T) {
	var b strings.Builder
	b.WriteString("func main(v0):\n  entry:\n")
	const n = 24
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "    v%d = %d\n", i+1, i*3-7)
	}
	// Condition depends on a live value and the parameter.
	fmt.Fprintf(&b, "    v100 = add v1, v0\n")
	fmt.Fprintf(&b, "    v101 = cmplt v100, v%d\n", n)
	b.WriteString("    br v101, yes, no\n")
	// Both arms read the condition and a rotating selection of live values.
	b.WriteString("  yes:\n")
	fmt.Fprintf(&b, "    v102 = add v101, v2\n")
	fmt.Fprintf(&b, "    v103 = add v102, v%d\n", n/2)
	b.WriteString("    jmp merge\n")
	b.WriteString("  no:\n")
	fmt.Fprintf(&b, "    v104 = mul v101, v3\n")
	fmt.Fprintf(&b, "    v105 = sub v104, v%d\n", n/3)
	b.WriteString("    jmp merge\n")
	// Merge: store every still-live entry value plus the condition to memory.
	b.WriteString("  merge:\n")
	fmt.Fprintf(&b, "    v200 = 0\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "    store v%d, [v200 + %d]\n", i+1, i)
	}
	fmt.Fprintf(&b, "    store v101, [v200 + %d]\n", n)
	// Return the condition plus the parameter so the exit state is checked.
	fmt.Fprintf(&b, "    v201 = add v101, v0\n")
	b.WriteString("    ret v201\n")
	runPaths(t, b.String(), allK, []int64{1}, []int64{100})
}

// Both branch targets naming the same block is legal; the merged value must
// still be current.
func TestBranchSameTarget(t *testing.T) {
	src := `
func main(v0):
  entry:
    v1 = 3
    v2 = cmplt v0, v1
    br v2, join, join
  join:
    v3 = add v2, v0
    ret v3
`
	runPaths(t, src, allK, []int64{1}, []int64{7})
}
