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

Implementations MUST support the following five meta-bonds. These bonds are content-addressed like any other bond — their identifiers are computed from their template strings (see [03-encoding.md](03-encoding.md)).

#### 1. Equivalence

```
Template: "_A_ is the same as _B_"
Fillers:  A = atom (type 0) / bond (type 1) / molecule (type 2),
          B = atom (type 0) / bond (type 1) / molecule (type 2)
```

Declares transitive equivalence between two entities of the same type. Both fillers MUST be the same type (both atoms, both bonds, or both molecules). If A is the same as B, and B is the same as C, then A, B, and C are all equivalent.

That MUST binds the author publishing the equivalence, and its consequence is the loss of meta semantics, not invalidity: a molecule whose two fillers are of different types is a valid molecule declaring no equivalence, and implementations MUST NOT unify the entities it names. See "Meta-molecules are regular molecules" below, which states this for all five meta-bonds.

**L3 semantics:** Implementations SHOULD treat equivalent entities as interchangeable when querying L3. The specific deduplication strategy (merge, prefer one, show both) is implementation-scoped.

*Informative.* Equivalence relates the entities a meta-molecule names, and nothing else. Under the reference reading it is the transitive closure of the pairs subscribed authors have *declared*, and it is not otherwise closed: no equivalence between two molecules is derived from an equivalence between their bonds, or between the entities filling them. Two molecules whose bonds are declared equivalent, and whose fillers are declared equivalent position by position, are therefore two classes and not one — each carries its own truth state, and an assertion, retraction, contradiction or supersession naming one says nothing about the other. An author who wants two molecules treated as the same statement declares that with a molecule-level equivalence, which is what "Declaring molecule equivalence" below is for. Two things argue for the narrow reading. Deriving the wider one is a fixpoint over the whole view — a derived molecule equivalence can make two further molecules equivalent in turn — and every implementation would have to compute it identically, down to what "equivalent" means for a scalar or IPFS filler that no equivalence can name, for two nodes to agree about what a class contains; and a class is what carries a truth state, so disagreeing about its membership is disagreeing about what is true. It also keeps the blast radius small: under the wider reading a single bond equivalence silently unifies every molecule built on either template, which is the attack "Security Considerations", "Equivalence attacks", already warns about, multiplied by the size of the graph. Whether equivalence should compose through a molecule's parts is deferred (see [00-overview.md](00-overview.md), "Open questions (v1)").

*Informative.* The reference implementation reads "interchangeable" at its word, and applies it to the other four meta-bonds as well: a truth assertion, a truth retraction, a contradiction or a supersession naming any member of an equivalence class is read as a statement about the whole class. The class, rather than the individual molecule, is what carries a truth state, what a supersession marks as replaced, and what a contradiction is surfaced between. The reasoning is that entities declared the same say the same thing, so a statement about one of them is a statement about all of them; the cost is that a subscribed author's equivalence redirects other authors' assertions onto molecules those authors never named, which is what "Security Considerations", "Equivalence attacks", is about and what subscription filtering bounds. Other strategies remain conformant — in particular, keeping every assertion attached to the entity actually named and exposing the class so that an application can widen the query itself. Whichever is chosen, a class whose members are asserted true by one subscribed author and untrue by another is a disagreement to surface under "Conflict handling" below, exactly as a single molecule in that position would be.

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

**L3 semantics:** A molecule asserted as untrue by a subscribed author SHOULD be excluded or flagged in L3. If the same author previously asserted the molecule as true, the later assertion (by block order) takes precedence. **Block order** is the position of the publishing block in its author chain — the `prev` sequence, continuing across a key rotation into the successor chain — and never the block's self-reported `ts`; it is defined in [05-processing-model.md](05-processing-model.md), "Assertion order", which also states that the assertions of two *different* authors are not ordered against each other but conflict.

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

The standard meta-bonds are identified by the SHA-256 digest of their dCBOR-encoded template. An implementation recognizes a meta-molecule by checking whether the molecule's `bond` field matches the digest of any known meta-bond template. The `bond` field holds a 32-byte digest, not a full CID — see [03-encoding.md](03-encoding.md), "Internal references".

The `Fillers:` line printed with each meta-bond above names the filler types that meta-bond's semantics are defined for. It is a **recognition criterion applied during L2→L3 processing**, not a rule of block validity. A molecule built from a meta-bond is validated exactly as any other molecule ([02-block-format.md](02-block-format.md), "Validation"), against a data model that has no notion of a meta-bond: rule 5 checks the number of fillers against the bond's variable count and each filler against its own type tag ([01-data-model.md](01-data-model.md)), and never against the filler types a particular bond expects. Consequently:

- Implementations MUST NOT reject a block, or refuse an entity at L1 or L2, because a molecule's `bond` field matches a standard meta-bond while its fillers do not match that meta-bond's declared shape. Such a molecule is a valid, plain molecule; it has a digest, it propagates, and it enters L2 like any other.
- Implementations MUST NOT apply a meta-bond's L3 semantics to a molecule whose fillers do not match its declared shape. It asserts nothing: `"_A_ is true"` filled with an atom is not a truth assertion, and an equivalence between two entities of different types unifies nothing.
- Implementations SHOULD surface such molecules to the application layer rather than discard them silently. They remain molecules of L3 like any other, and an application may want to know that a subscribed author published something that reads as nothing.

This keeps validation schema-driven and meta-bond-agnostic, which is what "Meta-molecules are regular molecules" asserts and what makes the extension process below work: whether a block validates cannot depend on which meta-bonds the validating implementation happens to know.

*Informative.* The reference implementation does exactly this. A molecule whose bond is a standard meta-bond but whose fillers do not fit its template stays in the L3 view as an ordinary molecule, is read as no assertion at all, and is listed separately from the meta-molecules that were applied, so an application can see it without guessing what its author meant.

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
  → CID: 01711220<different digest>...
```

The CIDs above are the external 36-byte form. Author C publishes an equivalence, in which the same entities are referenced by their raw 32-byte digests:

```
create_molecule(
  bond: <digest of "_A_ is the same as _B_">,
  fillers: [
    {type: 0, value: <digest of "Paris, the capital of France">},
    {type: 0, value: <digest of "Paris, France">}
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
  → CID: 01711220<different digest>...
```

Author C declares the two bonds equivalent:

```
create_molecule(
  bond: <digest of "_A_ is the same as _B_">,
  fillers: [
    {type: 1, value: <digest of "_A_ is the capital of _B_">},
    {type: 1, value: <digest of "_A_ is the capital city of _B_">}
  ]
)
```

In L3 the two bonds are members of one equivalence class, so an application that has found a molecule using either template can find the molecules using the other by walking that class. The declaration is about the bonds: it does not by itself make two molecules built from them equivalent to each other. See "Declaring molecule equivalence" below, and the informative paragraph on what equivalence does and does not close over in "Equivalence" above.

### Declaring molecule equivalence

Two authors independently published molecules that state the same fact using different atoms and bonds:

```
Author A: create_molecule(
  bond: <digest of "_A_ is the capital of _B_">,
  fillers: [{type: 0, value: <digest of "Paris, the capital of France">},
            {type: 0, value: <digest of "France">}]
)
  → CID: 01711220f9f124b0...

Author B: create_molecule(
  bond: <digest of "_A_ is the capital city of _B_">,
  fillers: [{type: 0, value: <digest of "Paris, France">},
            {type: 0, value: <digest of "The French Republic">}]
)
  → CID: 01711220<different digest>...
```

Author C declares the two molecules equivalent:

```
create_molecule(
  bond: <digest of "_A_ is the same as _B_">,
  fillers: [
    {type: 2, value: <digest of Author A's molecule>},
    {type: 2, value: <digest of Author B's molecule>}
  ]
)
```

In L3, both molecules are treated as the same assertion. A molecule-level equivalence is what says that two molecules are the same statement, and the only thing that says it: the equivalences Author C could publish between the two bonds, or between the atoms filling them, relate those entities and are not composed into an equivalence between the molecules built from them (see "Equivalence" above).

## References

### Normative
- [01-data-model.md](01-data-model.md) — Molecule and filler type definitions
- [02-block-format.md](02-block-format.md) — Operation types
- [03-encoding.md](03-encoding.md) — CBOR encoding and CID computation
- [04-cryptography.md](04-cryptography.md) — Key encoding and signing
- [05-processing-model.md](05-processing-model.md) — L2→L3 processing and conflict handling
