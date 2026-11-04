# Contributing

Thanks for wanting to help.

This project is a fork of [Crush](https://github.com/charmbracelet/crush) by
Charmbracelet, Inc. Its primary licence is the GNO Network General Public
License, version 6, with Additional Terms (Strong Attribution), in
[LICENSE.md](./LICENSE.md); each contribution is under the licence of the
part it changes. The files that came from Crush remain Charmbracelet's, under
FSL-1.1-MIT, reproduced at the end of that file; `internal/gnovm/`, the GnoVM
machine subsystem, carries its own copy of the same License in
[internal/gnovm/LICENSE.md](./internal/gnovm/LICENSE.md).

## Before you open a pull request

- Build and test: `go build ./...` and `go test ./...`.
- Format with `gofumpt` (`gofmt` is not enough; the linter checks it).
- Write a test that fails before your change and passes after it.
- Keep the commit message about what changes and why, in plain sentences.

See [AGENTS.md](./AGENTS.md) for the conventions in more detail.

## Contributor Licence Agreement

By submitting a contribution to this repository — opening a pull request,
adding to one, sending a patch, or any other form of submission — you agree
to the terms below for that contribution, as this file reads on the `main`
branch of <https://github.com/gnosh-ai/gnosh> (or at another location the
Maintainer names in the copy there) when you submit it, unless you say
otherwise in writing at the time you submit it. For that contribution, they
replace the terms of any earlier version of this file. Work you have already
submitted to this repository keeps the terms of the version of this file it
was first submitted under, even if you submit it again (by rebasing, for
example), unless you agree otherwise in writing, other than by submitting
new work.

**Maintainer** means Jae Kwon, the maintainer of this fork, and their
successors and assigns. The Maintainer's own work, listed below, is not a
Contribution under this agreement, whoever submits it, and the work in the
first item never was one under any earlier version of this file either:

- work Jae Kwon makes, by himself or with any tools and AI agents, and his
  part of any work he makes with others;
- work that, when it is submitted, by law or by agreement, belongs to Jae
  Kwon, to NewTendermint, LLC, or to another company he controls by owning
  more than half of it or by having the power to direct its management, or
  that an agreement says is to be assigned to any of them, such as work an
  employee makes for them in their job, or a contractor makes for them under
  a contract that says it is to be assigned, including that person's part of
  work made with others; and
- work that belongs to a successor or assign and that they make while they
  are the Maintainer.

Whoever submits work in the second item, other than work they did not make
that they mark "Submitted on behalf of a third party" as section 4 asks,
grants the Maintainer, and not Charm, the licences in sections 1 and 2 for
it, and makes the promises in section 4 about it, as if it were a
Contribution. Work not listed above does not become the Maintainer's own
work because the Maintainer asked for it, directed it, ran the tools or AI
agents it was made with, merged it, rebased it or submitted it again. The
Maintainer grants nothing under this agreement by submitting work.

**Charm** means Charmbracelet, Inc., the author of the upstream Crush
project this fork is derived from.

**Contribution** means any work you intentionally submit to this repository,
however it was made, by hand or with any tools and AI agents, including
code, documentation and any modification of or addition to existing work,
except anything you clearly mark at the time of submission as "Not a
Contribution" or "Submitted on behalf of a third party".

### 1. Copyright licence

You grant the Maintainer, **and separately grant Charm**, each a
perpetual, worldwide, non-exclusive, no-charge, royalty-free, irrevocable
copyright licence to reproduce, prepare derivative works of, publicly
display, publicly perform, sublicense and distribute your Contribution and
such derivative works.

Both grants are of the same scope, and each stands on its own: Charm's
grant is made directly to Charm, which is an intended beneficiary of this
agreement and may rely on it, whether or not the Maintainer ever sends
your Contribution upstream. The purpose of granting Charm the same rights
is so that work you do here can flow back to Crush without having to come
back to you for permission.

**Exception for `internal/gnovm/`.** For the parts of a Contribution under
`internal/gnovm/` submitted on or after 29 October 2026, you make only the
grants to the Maintainer, in this section and in section 2; the separate
grants to Charm do not extend to them, whatever an earlier agreement under
this file said. The rest of a Contribution that also changes other files is
under the usual terms. That subsystem is not part of Crush and is licensed
under different terms.

### 2. Patent licence

You grant the Maintainer, and separately grant Charm, each a perpetual,
worldwide, non-exclusive, no-charge, royalty-free, irrevocable (except as
stated in this section) patent licence to make, have made, use, offer to
sell, sell, import and otherwise transfer the work, where that licence
applies only to those patent claims you can license that are necessarily
infringed by your Contribution alone, or by your Contribution combined
with the work it was submitted to.

If any person or entity starts patent litigation against you or anyone
else (including a cross-claim or counterclaim in a lawsuit) alleging that
your Contribution, or the work it was submitted to, directly or
contributorily infringes a patent, the patent licences you granted under
this agreement to whoever started that litigation, for that Contribution or
that work, end on the day it is filed.

### 3. What you keep

These licences are non-exclusive. You keep all right, title and interest
in your Contribution, and you may use it for anything else you like.
Nothing here assigns your copyright to anyone.

### 4. What you are promising

- You are legally entitled to grant these licences.
- If your employer has rights to work you create, you have their
  permission to contribute, or they have waived those rights, or they
  have entered into a separate agreement covering your contributions. If
  you are contributing on behalf of a company, you are authorised to do
  so on its behalf.
- To the best of your knowledge, your Contribution is your own creation,
  made alone or with others who let you submit it, by hand or with any
  tools and AI agents, and does not infringe or misappropriate anyone
  else's rights.
- If you want to submit work you did not make, submit it separately, say
  where it came from, and name any licence, patent or trademark
  restriction you are aware of, marking it "Submitted on behalf of a third
  party: [name]".
- If you later learn that something above is not accurate, you will say
  so.

### 5. No warranty, no obligation to support

You provide your Contribution as is, without warranties or conditions of
any kind, express or implied, except for what you promise in section 4.
You are not expected to support your Contribution, though you are welcome
to.

## What this agreement does not do

- It does not change the licence on the code. This project's primary licence
  is the GNO Network General Public License, version 6, with Additional Terms
  (Strong Attribution); the files that came from Crush stay under
  FSL-1.1-MIT; and your Contribution is distributed under the licence of the
  part it changes.
- It gives neither the Maintainer nor you any rights to Crush beyond what
  FSL-1.1-MIT already grants, and no rights at all to Charm's trademarks,
  trade names or product names.
- Charm runs its own contributor process for the upstream project. This
  agreement means Charm already holds a licence to each Contribution, except
  its parts under `internal/gnovm/` (see section 1); it gives Charm no
  rights in the Maintainer's own work, does not oblige Charm to accept
  anything, and does not replace any signature Charm asks for in its own
  repository. Charm's own agreement is at [CLA.md](./CLA.md).
