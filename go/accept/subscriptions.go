package accept

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"slices"
)

// An authorKey is a public key in comparable form, so that it can key a map and
// be compared without allocating.
type authorKey [ed25519.PublicKeySize]byte

func (k authorKey) public() ed25519.PublicKey { return slices.Clone(k[:]) }

// keyOf converts a public key to comparable form. ok is false for a key that is
// not 32 bytes long, which no block can carry: block.Content.Validate refuses
// one, so such a key can never match an authorship tag.
func keyOf(pub ed25519.PublicKey) (k authorKey, ok bool) {
	if len(pub) != ed25519.PublicKeySize {
		return authorKey{}, false
	}
	copy(k[:], pub)
	return k, true
}

// compareKeys orders public keys bytewise.
func compareKeys(a, b ed25519.PublicKey) int { return bytes.Compare(a, b) }

// Subscriptions is a user's list of subscribed authors — the input, beside the
// graph, that decides what L3 contains (spec/05-processing-model.md, "Author
// subscriptions"):
//
//	A user maintains a list of subscribed authors. This list is:
//	  - Local configuration. Subscriptions are stored locally on the user's node.
//	  - Private. Subscriptions are never published on-chain.
//
// It is therefore not a protocol structure: nothing here is encoded, signed or
// published, and a Subscriptions value never leaves the node it was built on.
//
// # Own keys are subscriptions
//
// SubscribeOwn marks a key as one the user holds — their own chains. It
// subscribes the key and records why; no query consults the flag when deciding
// what is in a view. "A user is always considered subscribed to the chains
// signed by a key they hold, which is what makes their own private chains
// unconditional: that is an instance of the filtering rule, not a mechanism
// beside it" (spec/05-processing-model.md, "Private chains"). The flag is there
// for an application that wants to tell the two apart in its interface, and for
// a node that wants to refuse to unsubscribe from itself.
//
// Holding a chain's content key is not a subscription either. A reader for whom
// an author wrapped a private chain's key can decrypt that chain at L2 and will
// not see its entities at L3 until they subscribe to its author: "a content key
// is a capability to read, not a declaration to accept" (same section).
//
// A Subscriptions is not safe for concurrent modification, and Build copies
// what it needs, so a view is unaffected by later changes to the set it was
// built from.
type Subscriptions struct {
	own   map[authorKey]bool // subscribed key -> is it one the user holds
	order []authorKey        // ascending
}

// NewSubscriptions returns the subscription set holding pubs.
func NewSubscriptions(pubs ...ed25519.PublicKey) *Subscriptions {
	s := &Subscriptions{own: make(map[authorKey]bool)}
	return s.Subscribe(pubs...)
}

// Subscribe adds authors to the set and returns the receiver, so that calls
// chain. Subscribing to a key that is already subscribed changes nothing, and
// does not clear the own flag of a key added with SubscribeOwn.
//
// A key that is not 32 bytes long is ignored: no block can carry one, so it
// could never match an authorship tag.
func (s *Subscriptions) Subscribe(pubs ...ed25519.PublicKey) *Subscriptions {
	return s.add(false, pubs)
}

// SubscribeOwn adds authors the user holds the signing key of — their own
// chains — and returns the receiver. They are ordinary subscriptions; see the
// type documentation for what the flag does and does not mean.
func (s *Subscriptions) SubscribeOwn(pubs ...ed25519.PublicKey) *Subscriptions {
	return s.add(true, pubs)
}

func (s *Subscriptions) add(own bool, pubs []ed25519.PublicKey) *Subscriptions {
	if s.own == nil {
		s.own = make(map[authorKey]bool)
	}
	for _, pub := range pubs {
		k, ok := keyOf(pub)
		if !ok {
			continue
		}
		if was, held := s.own[k]; !held {
			s.own[k] = own
			i, _ := slices.BinarySearchFunc(s.order, k, compareAuthorKeys)
			s.order = slices.Insert(s.order, i, k)
		} else if own && !was {
			s.own[k] = true
		}
	}
	return s
}

// Contains reports whether pub is subscribed. It is the whole of the L3
// filtering test (spec/05-processing-model.md, "Filtering rules"): an entity is
// in a view when one of its authorship tags names a key this answers true for.
func (s *Subscriptions) Contains(pub ed25519.PublicKey) bool {
	if s == nil {
		return false
	}
	k, ok := keyOf(pub)
	if !ok {
		return false
	}
	_, held := s.own[k]
	return held
}

// IsOwn reports whether pub was subscribed as a key the user holds.
func (s *Subscriptions) IsOwn(pub ed25519.PublicKey) bool {
	if s == nil {
		return false
	}
	k, ok := keyOf(pub)
	if !ok {
		return false
	}
	return s.own[k]
}

// Keys returns every subscribed author, ascending by key.
func (s *Subscriptions) Keys() []ed25519.PublicKey {
	if s == nil {
		return nil
	}
	out := make([]ed25519.PublicKey, len(s.order))
	for i, k := range s.order {
		out[i] = k.public()
	}
	return out
}

// Len returns the number of subscribed authors.
func (s *Subscriptions) Len() int {
	if s == nil {
		return 0
	}
	return len(s.order)
}

func (s *Subscriptions) String() string {
	return fmt.Sprintf("subscriptions(%d author(s))", s.Len())
}

// snapshot copies the set, so that a View is unaffected by later changes to the
// Subscriptions it was built from.
func (s *Subscriptions) snapshot() *Subscriptions {
	out := &Subscriptions{own: make(map[authorKey]bool, s.Len())}
	if s == nil {
		return out
	}
	out.order = slices.Clone(s.order)
	for _, k := range s.order {
		out.own[k] = s.own[k]
	}
	return out
}

// compareAuthorKeys orders comparable public keys bytewise.
func compareAuthorKeys(a, b authorKey) int { return bytes.Compare(a[:], b[:]) }
