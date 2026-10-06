# register-allocator-go

A code-generation backend for a virtual-register, basic-block IR. The input
program may use arbitrarily many virtual registers; the backend lowers it to
code for a target machine with a fixed, much smaller physical register file,
reusing registers and inserting spill/reload instructions under pressure while
preserving exact program semantics.

## Components

- `internal/ir` — IR text parser, located errors, semantic validation and a
  canonical printer.
- `internal/interp` — a direct interpreter for the IR (reference semantics).
- `internal/target` — the target machine model, its instruction set, a
  canonical printer and an executor.
- `internal/alloc` — liveness analysis (per-instruction live sets) and
  register-pressure measurement.
- `internal/codegen` — deterministic instruction selection plus the register
  allocator/spiller.
- `internal/bench` — benchmark-suite equivalence tests with enforced spill
  budgets.
- `cmd/ra` — CLI to print generated code and to run either the IR reference
  interpreter or the generated target code.

## IR language

Functions are declared with virtual-register parameters and contain named
basic blocks; every block ends with exactly one terminator (`jmp`,
`br v, then, else`, or `ret [v]`). Instructions:

```
nop
vD = 123                      # integer constant
vD = add|sub|mul|div|mod vA, vB
vD = cmplt|cmple|cmpeq|cmpne vA, vB
vD = vA                       # move
vD = [vBase + off]            # load
store vData, [vBase + off]    # store
vD = fname(vA, vB, ...)       # call
```

Values are signed 64-bit integers. Memory is a flat shared word array
addressed by `base + offset`. Comparisons produce 0/1. Branches test non-zero.
Comments start with `#`, `;` or `//`.

## Validation and located errors

Before any target code is emitted the program is parsed and validated.
`ir.Error` carries file, line and column. The backend rejects:

- malformed instruction syntax or unknown opcodes;
- duplicate functions, blocks or parameters;
- blocks missing a terminator, or a terminator that is not the last
  instruction;
- jumps/branches to undefined blocks;
- calls to undefined functions or with the wrong argument count;
- uses of a value that is not defined on every path reaching the use
  (a must-be-defined data-flow analysis over the CFG);
- more parameters than the target can pass in registers.

Invalid input never produces target code silently: `Generate` returns the
first located error.

## Target machine model

The target has `k` general-purpose 64-bit registers `r0..r(k-1)` and shares
the same flat memory as the IR interpreter. The instruction set mirrors the IR
with physical registers and two extra opcodes:

- `spill rN, slotS` — store a register into the current frame's spill slot S.
- `rD = reload slotS` — load spill slot S into a register.

Each function owns a private frame of spill slots; slot numbers are fixed per
virtual register for the duration of a function and are reused across calls
(callees have their own frames), so spilled values are never confused across
the call boundary.

### Calling convention

- Arguments arrive in `r0..r(n-1)`.
- The integer return value is delivered in `r0`.
- **Every** register is caller-saved. Values that must survive a call are
  written to spill slots before the call and reloaded afterwards; the code
  generator guarantees this, so cross-call-live values remain intact.

The target executor (`target.Machine.Run`) models exactly this machine:
recursive calls use separate frames with independent registers and slots.

## Register allocation and spilling

Allocation is performed per function and is entirely static and
deterministic (no randomness, no map-iteration dependence, no environment
input).

1. **Liveness.** A backwards data-flow pass computes live-in/live-out sets at
   every instruction, treating calls as clobbering all registers and
   considering argument and return registers explicitly.
2. **Fixed homes.** Every virtual register gets one stable spill slot in its
   function frame, allocated on first need.
3. **Block-scoped register cache.** Within a block a value may stay resident
   in a physical register across instructions; the cache is invalidated on a
   new definition.
4. **Pressure-driven eviction.** When an instruction needs a register and none
   is free, a victim is chosen among cached values by a furthest-next-use
   heuristic (a value never read again in the block is preferred). A victim
   whose slot copy is stale is emitted as a `spill` first.
5. **Control-flow and call boundaries.** Before every terminator and every
   call, every live register-resident value with a stale slot is written to
   its home slot; successor blocks therefore find live values in slots. This
   includes the value of a branch condition: when it stays live into a
   successor block it is flushed to its slot before the `br`, so the
   successor can never reload a stale copy while the branch itself still
   tests the (correct) register.
6. **Entry-block back-edges.** If some terminator jumps back to the entry
   block, re-executing the parameter-homing prologue would clobber the
   parameters' home slots with whatever the ABI registers hold at the jump.
   For such functions the homing runs exactly once per call in a synthetic
   `.prologue` block that falls through to the real entry block via a `jmp`.
   Functions without entry back-edges are unaffected and keep their previous
   output shape.
7. **Two-address operations.** Binary operations need two distinct source
   registers and a destination. With `k >= 3` the sources are materialized in
   `r0/r1` and the result in another register; with `k == 2` the sources are
   routed through their slots into `r0/r1` and the result is produced in `r0`,
   which is always possible without losing any live value.

It is an invariant that at no point do two simultaneously-live virtual
registers occupy the same physical register, and no active value is
overwritten without first being stored when it is still needed.

## Correctness testing

For every benchmark and several hand-written programs, tests execute the
input both with the direct IR interpreter and with the generated target code
across multiple register counts (`k = 2, 3, 4, 8, ...`) and require identical
return values **and** memory contents. Programs store values that are still
live at their exit points into memory before returning, so the full-memory
comparison also covers exit-live values. Coverage includes:

- loops and reducible control flow;
- recursive calls and cross-call-live values;
- calls that occupy every physical register with arguments;
- extreme register pressure (200 live values with 2–4 registers);
- branch forms (`internal/codegen/branch_test.go`): a branch condition that
  stays live into one or both successor blocks, branch-dense chains, nested
  branches, values merged from multiple predecessors that become branch
  conditions, loop conditions read in the body and after the exit, and
  back-edges to the entry block — each exercised over every reachable path
  (via several argument sets) at several register counts;
- a deterministic randomized probe
  (`internal/codegen/branch_fuzz_test.go`) that generates branch-heavy
  programs with fixed seeds and requires interpreter/target equivalence for
  every generated program; a failure reproduces exactly from its seed.

The required invariant is: for every legal input program and every physical
register count `k >= 2`, the target machine's final state (return value and
full memory) equals the IR interpreter's, on every reachable path.

## Spill budgets

`internal/bench` loads the programs in `testdata/bench` and, in addition to
semantic equivalence, enforces an explicit upper bound on generated spill and
reload counts for each benchmark at several values of `k` (see the `budgets`
table in `internal/bench/bench_test.go`). A change that increases stack
traffic beyond the ceiling fails the tests, guarding against allocation
quality regressions. The current allocator comfortably fits the ceilings.

`branchy.ir` is the branch-form regression benchmark: its condition values
stay live into successor blocks, so its budget also guards the edge-flush
correctness fix from degenerating (a missing flush is a wrong-code bug the
equivalence tests catch; a reappearing *excess* of spills is caught here).

Maintaining the ceilings: when the allocator changes, regenerate the observed
counts with `go test ./internal/bench -run TestSpillBudgets -v`, then either
tighten the code until the old ceilings hold again, or raise a ceiling only
after consciously accepting the extra stack traffic. New benchmarks must be
added to both the `budgets` table (with ceilings close to the observed
counts) and the `entryArgs` map so equivalence is checked too.

## Determinism

`Generate` is a pure function of the parsed program and `k`. A test generates
the same program repeatedly and asserts byte-identical canonical target
assembly every time.

## CLI

```
go run ./cmd/ra compile [-k 4] file.ir     # print target code
go run ./cmd/ra run-ir [-args 1,2] file.ir # run IR reference interpreter
go run ./cmd/ra run [-k 4] [-args 1,2] file.ir
```

## Tests

```
go test ./...
```
