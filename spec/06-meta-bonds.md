# Meta-Bonds

**Version:** <<VERSION>> | **Status:** Draft

## Abstract

This document defines the Dialog v1 standard meta-bond library — a set of bonds whose molecules carry special semantics during Layer 2 to Layer 3 processing. It specifies the five standard meta-bonds, their semantics, and the process for extending the library.

## Terminology

The key words "MUST", "MUST NOT", "SHOULD", "SHOULD NOT", and "MAY" are to be interpreted as described in [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119).

- **Meta-bond:** A bond whose molecules represent states of data rather than data itself.
- **Meta-molecule:** A molecule created from a meta-bond. Regular at L1/L2, interpreted at L3.

## Overview

Most molecules in Dialog represent data or relationships between data (e.g., "[Paris] is the capital of [France]"). Meta-molecules represent assertions *about* data — truth claims, equivalences, contradictions, and corrections.

Meta-molecules are created with the same `create_molecule` operation as any other molecule. They are stored as regular molecules in L1 and L2. Their special semantics are only applied during the L2→L3 transformation, and the specific processing behavior is implementation-scoped.

## Specification

### Standard meta-bond library

Implementations MUST support the following five meta-bonds. These bonds are content-addressed like any other bond — their CIDs are computed from their template strings.

#### 1. Equivalence

```
Template: "_A_ is the same as _B_"
Fillers:  A = atom (type 0) / bond (type 1) / molecule (type 2),
          B = atom (type 0) / bond (type 1) / molecule (type 2)
```

Declares transitive equivalence between two entities of the same type. Both fillers MUST be the same type (both atoms, both bonds, or both molecules). If A is the same as B, and B is the same as C, then A, B, and C are all equivalent.

**L3 semantics:** Implementations SHOULD treat equivalent entities as interchangeable when querying L3. The specific deduplication strategy (merge, prefer one, show both) is implementation-scoped.

#### 2. Truth assertion

```
Template: "_A_ is true"
Fillers:  A = molecule (type 2)
```

Asserts that a molecule is true according to the publishing author.

**L3 semantics:** A molecule asserted as true by a subscribed author SHOULD be treated as factual in L3.

#### 3. Truth retraction

```
Template: "_A_ is untrue"
Fillers:  A = molecule (type 2)
```

Asserts that a molecule is not true according to the publishing author.

**L3 semantics:** A molecule asserted as untrue by a subscribed author SHOULD be excluded or flagged in L3. If the same author previously asserted the molecule as true, the later assertion (by block order) takes precedence.

#### 4. Contradiction

```
Template: "_A_ contradicts _B_"
Fillers:  A = molecule (type 2), B = molecule (type 2)
```

Declares that two molecules are contradictory — they cannot both be true.

**L3 semantics:** If both molecules are present in L3 (asserted by subscribed authors), the implementation MUST surface the contradiction to the application layer. Resolution strategy is implementation-scoped.

#### 5. Supersession

```
Template: "_A_ supersedes _B_"
Fillers:  A = molecule (type 2), B = molecule (type 2)
```

Declares that molecule A replaces molecule B. Used for corrections and versioning.

**L3 semantics:** If both A and B are in L3, implementations SHOULD present A and hide or deprecate B.

### Conflict handling

When subscribed authors disagree through meta-molecules, the protocol requires:

1. Implementations MUST detect conflicts (e.g., author X says "M is true" and author Y says "M is untrue")
2. Implementations MUST surface detected conflicts to the application layer
3. Implementations MUST NOT silently discard conflicting assertions

The resolution strategy is implementation-scoped. See [05-processing-model.md](05-processing-model.md).

### Meta-molecules are regular molecules

Meta-molecules are published using `create_molecule` like any other molecule. They follow the same content-addressing rules, appear in L1 and L2 as regular molecules, and are only distinguished by their bond template matching a known meta-bond.

The standard meta-bonds are identified by their content hashes (CIDs of their templates). An implementation recognizes a meta-molecule by checking whether its bond reference matches the CID of any known meta-bond template.

### Extension process

The v1 standard library is intentionally minimal. New meta-bonds will be adopted through an RFC-like process:

1. An implementor defines a new meta-bond and uses it in their implementation
2. If the bond proves useful across multiple implementations, it is proposed for standardization
3. Community review and consensus
4. Addition to the standard library in a future protocol version

Implementations MAY define and use custom meta-bonds beyond the standard library. Custom meta-bonds are processed identically at L1/L2 (as regular molecules) and have implementation-specific L3 semantics.

## Security Considerations

- **Key rotation abuse:** An attacker who compromises an author's private key can publish a fraudulent rotation block via the L1 `rotate_key` operation (see [02-block-format.md](02-block-format.md)). v1 does not include pre-rotation or social recovery mechanisms.
- **Equivalence attacks:** A malicious author could assert "A is the same as B" where A and B are unrelated entities, potentially confusing applications. L3 filtering mitigates this — the assertion only affects users who subscribe to that author.
- **Truth assertion spam:** An author could assert large numbers of molecules as true or untrue. This is mitigated by L3 subscription filtering and is comparable to any other spam in the system.

## Examples

### Declaring atom equivalence

Two authors independently created atoms for Paris:

```
Author A: create_atom("Paris, the capital of France")
  → CID: 017112206545050a...

Author B: create_atom("Paris, France")
  → CID: 01711220<different hash>...
```

Author C publishes an equivalence:

```
create_molecule(
  bond: <CID of "_A_ is the same as _B_">,
  fillers: [
    {type: 0, value: <hash of "Paris, the capital of France">},
    {type: 0, value: <hash of "Paris, France">}
  ]
)
```

Users who subscribe to Author C will see both atoms as equivalent in L3.

### Declaring bond equivalence

Two authors independently created bonds that express the same relationship:

```
Author A: create_bond("_A_ is the capital of _B_")
  → CID: 01711220f295b892...

Author B: create_bond("_A_ is the capital city of _B_")
  → CID: 01711220<different hash>...
```

Author C declares the two bonds equivalent:

```
create_molecule(
  bond: <CID of "_A_ is the same as _B_">,
  fillers: [
    {type: 1, value: <hash of "_A_ is the capital of _B_">},
    {type: 1, value: <hash of "_A_ is the capital city of _B_">}
  ]
)
```

In L3, molecules using either bond template are treated as expressing the same relationship.

### Declaring molecule equivalence

Two authors independently published molecules that state the same fact using different atoms and bonds:

```
Author A: create_molecule(
  bond: <CID of "_A_ is the capital of _B_">,
  fillers: [{type: 0, value: <hash of "Paris, the capital of France">},
            {type: 0, value: <hash of "France">}]
)
  → CID: 01711220f9f124b0...

Author B: create_molecule(
  bond: <CID of "_A_ is the capital city of _B_">,
  fillers: [{type: 0, value: <hash of "Paris, France">},
            {type: 0, value: <hash of "The French Republic">}]
)
  → CID: 01711220<different hash>...
```

Author C declares the two molecules equivalent:

```
create_molecule(
  bond: <CID of "_A_ is the same as _B_">,
  fillers: [
    {type: 2, value: <hash of Author A's molecule>},
    {type: 2, value: <hash of Author B's molecule>}
  ]
)
```

In L3, both molecules are treated as the same assertion. This is useful when atom-level or bond-level equivalence alone is insufficient because the molecules combine different atoms and different bond templates.

## References

### Normative
- [01-data-model.md](01-data-model.md) — Molecule and filler type definitions
- [02-block-format.md](02-block-format.md) — Operation types
- [03-encoding.md](03-encoding.md) — CBOR encoding and CID computation
- [04-cryptography.md](04-cryptography.md) — Key encoding and signing
- [05-processing-model.md](05-processing-model.md) — L2→L3 processing and conflict handling
