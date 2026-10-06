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
   its home slot; successor blocks therefore find live values in slots. The
   condition value of a conditional branch is no exception: when it stays
   live into a successor block its home slot must be current before the
   `br` executes, otherwise the successor would reload a stale value.
6. **Prologue block.** Functions with parameters start with a synthetic
   prologue block that homes the ABI argument registers into the parameter
   slots and then jumps to the IR entry block. The homing therefore runs
   exactly once per call: the IR entry block may itself be the target of a
   back-edge, and re-executing the spills there would clobber the parameter
   slots with whatever the argument registers happen to hold at jump time.
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
across multiple register counts (`k = 2, 3, 4, 8, ...`) and multiple argument
sets, and require identical final states: the return value, the **full**
memory image, and the values still live at the program exit. Exit-live values
are made observable by a test harness
(`instrumentExitLive` in `internal/codegen/branch_test.go`) that rewrites the
entry function to dump every definitely-defined virtual register to a
dedicated memory region just before each exit `ret`; the full-memory
comparison then covers them. Any divergence between the target machine and
the interpreter — a changed branch decision, an overwritten value, or a
successor block reading stale slot contents — shows up as a mismatch.

Coverage includes:

- loops and reducible control flow, including back-edges to the function
  entry block;
- conditional branches whose condition value is read again in successor
  blocks, in nested branches, branch chains, multi-predecessor joins and
  loop headers, also under extreme register pressure and across calls;
- recursive calls and cross-call-live values;
- calls that occupy every physical register with arguments;
- extreme register pressure (200 live values with 2–4 registers);
- deterministic pseudo-random branchy programs (seeded, therefore exactly
  reproducible) checked on every reachable path
  (`TestRandomizedBranchEquivalence`).

The branch-shape regressions live in `internal/codegen/branch_test.go` and
`internal/codegen/differential_test.go`; each of them fails on the historical
allocator that skipped flushing the branch condition at control-flow edges.

## Spill budgets

`internal/bench` loads the programs in `testdata/bench` and, in addition to
semantic equivalence, enforces an explicit upper bound on generated spill and
reload counts for each benchmark at several values of `k` (see the `budgets`
table in `internal/bench/bench_test.go`). A change that increases stack
traffic beyond the ceiling fails the tests, guarding against allocation
quality regressions. The current allocator comfortably fits the ceilings.

The suite includes `branch_maze.ir`, a branch-dense benchmark whose condition
values stay live into successor blocks; its ceilings (spills ≤ 30, reloads ≤
40 at `k = 2, 4, 8`) were set by measuring the fixed allocator and adding a
small headroom. To raise a ceiling deliberately, measure with a temporary
test that logs `res.Stats` for the benchmark, then update the `budgets`
table with the observed numbers plus headroom and say why in the commit
message.

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
