// Package vectorfile declares the on-disk shape of Dialog's conformance test
// vectors — the JSON files under vectors/ at the root of this repository —
// and reads them back.
//
// It is a leaf: it imports nothing of the protocol. That is deliberate. The
// generator (internal/vectors) builds these types out of the dcbor, cid,
// entity, block and privacy packages, and the tests of those same packages
// read the committed files through this one. A package that both produced and
// verified the vectors would prove nothing; keeping the schema here lets the
// verification side depend on the files alone.
//
// The format is documented for other languages in vectors/README.md. This
// package is its Go binding, not its definition.
package vectorfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// Format is the value of every file's "vectors" field. It names the shape of
// the JSON — the envelope, the value model, the field names — and not the
// protocol version, which is a block's v field.
const Format = "dialog-conformance/1"

// A Document is one vectors file: everything pinned about one area of the
// protocol.
type Document struct {
	// Vectors is Format.
	Vectors string `json:"vectors"`
	// Area names the file: dcbor, entities, blocks or privacy.
	Area string `json:"area"`
	// Description says what the file pins, in prose.
	Description string `json:"description"`
	// Spec lists the specification documents the area is defined by.
	Spec []string `json:"spec"`
	// Inputs holds the fixed constants the cases are derived from, when the
	// area has any (seeds, keys, nonces).
	Inputs any `json:"inputs,omitempty"`
	// Sections group the cases.
	Sections []Section `json:"sections"`
}

// A Section is a group of cases with a common shape: every case in one
// section is the same JSON object type.
type Section struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Cases holds a slice of one of the case types below. It is typed as any
	// because the type differs per section; DecodeCases converts it.
	Cases any `json:"cases"`
}

// Section returns the named section.
func (d Document) Section(name string) (Section, bool) {
	for _, s := range d.Sections {
		if s.Name == name {
			return s, true
		}
	}
	return Section{}, false
}

// A File is a Document together with the name it is written under.
type File struct {
	Name string
	Doc  Document
}

// JSON renders the file exactly as it is committed: indented with two spaces
// and terminated by a newline. The rendering is deterministic — every field
// order is a struct's — so a regenerated file is byte-identical unless the
// values changed.
func (f File) JSON() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// Descriptions are prose and may hold any UTF-8; HTML escaping would
	// mangle it into < sequences no other language's reader expects.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(f.Doc); err != nil {
		return nil, fmt.Errorf("vectorfile: encoding %s: %w", f.Name, err)
	}
	return buf.Bytes(), nil
}

// Read parses a committed vectors file.
func Read(path string) (Document, error) {
	// The callers are the vector generator and the conformance test, both of
	// which name files inside this repository. The package is internal, so
	// there is no path here that an untrusted caller can choose.
	//nolint:gosec // G304: an internal package reading this repository's own files.
	b, err := os.ReadFile(path)
	if err != nil {
		return Document{}, err
	}
	var doc Document
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return Document{}, fmt.Errorf("vectorfile: %s: %w", path, err)
	}
	if doc.Vectors != Format {
		return Document{}, fmt.Errorf("vectorfile: %s declares format %q, want %q", path, doc.Vectors, Format)
	}
	return doc, nil
}

// DecodeInputs converts a document's inputs into the type the area declares,
// on a document that was built or one that was parsed.
func DecodeInputs[T any](d Document) (T, error) {
	var out T
	raw, err := json.Marshal(d.Inputs)
	if err != nil {
		return out, fmt.Errorf("vectorfile: %s inputs: %w", d.Area, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, fmt.Errorf("vectorfile: %s inputs: %w", d.Area, err)
	}
	return out, nil
}

// DecodeCases converts a section's cases into the typed slice the section
// holds. It works both on a document that was just built, whose Cases is
// already a []T, and on one that was parsed from JSON, whose Cases is a slice
// of generic maps.
func DecodeCases[T any](s Section) ([]T, error) {
	raw, err := json.Marshal(s.Cases)
	if err != nil {
		return nil, fmt.Errorf("vectorfile: section %q: %w", s.Name, err)
	}
	var out []T
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("vectorfile: section %q: %w", s.Name, err)
	}
	return out, nil
}

// A Value is the JSON model of a dCBOR value: enough for a reader in any
// language to rebuild the value and encode it, without parsing CBOR first.
// Integers are decimal strings because CBOR's integer range does not survive
// a JSON number in every language.
//
//	{"type": "uint",    "number": "1"}
//	{"type": "neg",     "number": "-1"}
//	{"type": "text",    "text": "France"}
//	{"type": "bytes",   "bytes": "<hex>"}
//	{"type": "array",   "items": [...]}          ; absent items = empty array
//	{"type": "map",     "entries": [{"key": "...", "value": {...}}, ...]}
//	{"type": "decimal", "exponent": "-2", "mantissa": "314"}
//	{"type": "null"}
//
// Map entries are listed in the canonical encoding order — the bytewise
// lexicographic order of the encoded keys — so a consumer that preserves the
// order it reads produces canonical bytes without sorting.
type Value struct {
	Type     string  `json:"type"`
	Number   string  `json:"number,omitempty"`
	Text     string  `json:"text,omitempty"`
	Bytes    string  `json:"bytes,omitempty"`
	Exponent string  `json:"exponent,omitempty"`
	Mantissa string  `json:"mantissa,omitempty"`
	Items    []Value `json:"items,omitempty"`
	Entries  []Entry `json:"entries,omitempty"`
}

// An Entry is one key/value pair of a map Value. Dialog map keys are always
// text strings (spec/03-encoding.md), so the key is a plain JSON string.
type Entry struct {
	Key   string `json:"key"`
	Value Value  `json:"value"`
}

// A DCBORCase is one value and the one byte string that encodes it.
type DCBORCase struct {
	Name  string `json:"name"`
	Note  string `json:"note,omitempty"`
	Value Value  `json:"value"`
	DCBOR string `json:"dcbor"`
}

// An InvalidCase is a byte string an implementation MUST reject, together
// with the rule it violates. It is used for dCBOR input, for entities and
// for whole blocks.
//
// Kind is set only in the entities file, where it names the decoder the bytes
// are handed to — atom, bond, molecule or filler — because the entity layer
// has one decoder per kind and a case is a rejection by *its* decoder. The
// dCBOR and block files have a single decoder each and leave it empty.
type InvalidCase struct {
	Name   string `json:"name"`
	Kind   string `json:"kind,omitempty"`
	Rule   string `json:"rule"`
	Reason string `json:"reason"`
	Bytes  string `json:"bytes"`
}

// An InvalidInChainCase is a block that is well-formed on its own and MUST be
// rejected once a node holds the blocks around it: the half of
// spec/02-block-format.md, "Validation", that is a relation between a block and
// a store — rules 3, 4, 5, 6, the own-chain half of rule 10 and the scan limit
// of spec/05-processing-model.md.
//
// A consumer replays Setup into a fresh store, in order, accepting every block,
// and then offers Bytes, which MUST be rejected under Rule. Setup may be empty
// for a block that is wrong about itself and needs no store at all.
type InvalidInChainCase struct {
	Name   string `json:"name"`
	Rule   string `json:"rule"`
	Reason string `json:"reason"`
	// Setup is the blocks to replay first, in order. Every one of them is
	// valid.
	Setup []string `json:"setup"`
	// Bytes is the block that MUST then be rejected.
	Bytes string `json:"bytes"`
	// ScanLimit is the limit the case must be validated with, for a case whose
	// rejection is the scan limit of spec/05-processing-model.md and nothing
	// else. It is absent when the case does not depend on the limit, and a
	// case that carries it is valid under the default limit.
	ScanLimit int `json:"scan_limit,omitempty"`
}

// An EntityCase is one content-addressed entity: what it says, the bytes it
// encodes to, and the identifiers those bytes produce.
type EntityCase struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Note string `json:"note,omitempty"`
	// Description is an atom's description string; Template is a bond's
	// template. A molecule has neither: it is described by its value.
	Description string `json:"description,omitempty"`
	Template    string `json:"template,omitempty"`
	// Variables are the variables of a bond template, in order.
	Variables []string `json:"variables,omitempty"`
	Value     Value    `json:"value"`
	DCBOR     string   `json:"dcbor"`
	Digest    string   `json:"digest"`
	CID       string   `json:"cid"`
	CIDText   string   `json:"cid_text"`
}

// A FillerCase is one filler of a molecule. A filler is not content-addressed
// on its own — it is hashed as part of the molecule that holds it — so it has
// bytes and no identifier.
type FillerCase struct {
	Name  string `json:"name"`
	Type  uint64 `json:"type"`
	Note  string `json:"note,omitempty"`
	Value Value  `json:"value"`
	DCBOR string `json:"dcbor"`
}

// A KeyCase is one Ed25519 identity of a scenario.
type KeyCase struct {
	Name string `json:"name"`
	Seed string `json:"seed"`
	// PrivateKey is the 64-byte expanded form (seed || public key) that many
	// libraries take; Seed is the 32-byte form RFC 8032 calls the private key.
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
}

// BlockInputs is the inputs block of vectors/blocks.json.
type BlockInputs struct {
	Note string    `json:"note"`
	Keys []KeyCase `json:"keys"`
}

// A BlockCase is one block: its fields, the exact bytes the signature covers,
// the signature, and the bytes the digest is taken over.
type BlockCase struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Author      string `json:"author"`
	Type        string `json:"type"`
	// Prev is the hex digest of the previous block, or null for a genesis
	// block.
	Prev *string  `json:"prev"`
	Refs []string `json:"refs,omitempty"`
	TS   uint64   `json:"ts,omitempty"`
	// Enc and Nonce are a private block's ciphertext and nonce; the other two
	// block types have neither.
	Enc   string `json:"enc,omitempty"`
	Nonce string `json:"nonce,omitempty"`
	// Value is the complete block map — operations, signature and all — as a
	// value model. It is the block's structure in full; the fields above are
	// the summary a reader wants at a glance.
	Value Value `json:"value"`
	// SigningBytes is dCBOR(block without "sig"); SigningInput is
	// "dialog-v1-block" || SigningBytes, the byte string Ed25519 signs
	// (spec/04-cryptography.md, "Signing procedure").
	SigningBytes string `json:"signing_bytes"`
	SigningInput string `json:"signing_input"`
	Signature    string `json:"signature"`
	Block        string `json:"block"`
	Digest       string `json:"digest"`
	CID          string `json:"cid"`
	CIDText      string `json:"cid_text"`
}

// A ForkCase names two blocks of one chain that share a prev — the condition
// rule 9 requires a node to detect.
type ForkCase struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Rule        string   `json:"rule"`
	Author      string   `json:"author"`
	Prev        string   `json:"prev"`
	Blocks      []string `json:"blocks"`
}

// PrivacyInputs is the inputs block of vectors/privacy.json: every byte the
// private-block cases are derived from.
type PrivacyInputs struct {
	Note string `json:"note"`
	// Keys are the author, the recipient and a third party who holds no key
	// to this chain.
	Keys []KeyCase `json:"keys"`
	// ContentKey is the chain's symmetric key, 32 bytes.
	ContentKey string `json:"content_key"`
	// BlockNonce is the 24-byte XChaCha20 nonce of the sealed block, and
	// WrapNonce the 24-byte nonce of the key wrap.
	BlockNonce string `json:"block_nonce"`
	WrapNonce  string `json:"wrap_nonce"`
}

// A PrivacyCase is one named byte string of the private-block path, with the
// dCBOR value behind it where it has one.
type PrivacyCase struct {
	Name  string `json:"name"`
	Note  string `json:"note,omitempty"`
	Value *Value `json:"value,omitempty"`
	Hex   string `json:"hex"`
}

// An X25519Case is the conversion of one Ed25519 identity to the X25519
// key agreement (spec/04-cryptography.md, "Ed25519-to-X25519 conversion").
type X25519Case struct {
	Name             string `json:"name"`
	Note             string `json:"note,omitempty"`
	Seed             string `json:"seed"`
	Ed25519PublicKey string `json:"ed25519_public_key"`
	X25519PrivateKey string `json:"x25519_private_key"`
	X25519PublicKey  string `json:"x25519_public_key"`
}

// A WrapCase is one per-recipient key wrap: the agreement it rests on, the
// key HKDF derives from it, and the 72 bytes that travel.
type WrapCase struct {
	Name string `json:"name"`
	Note string `json:"note,omitempty"`
	// Own is the party doing the wrapping and Peer the other end, by key name.
	Own  string `json:"own"`
	Peer string `json:"peer"`
	// SharedSecret is the raw X25519 agreement, Info the HKDF info string,
	// and WrappingKey the 32 bytes HKDF-SHA-256 produces from them.
	SharedSecret string `json:"shared_secret"`
	Info         string `json:"info"`
	WrappingKey  string `json:"wrapping_key"`
	// Nonce is the wrap's 24-byte nonce and WrappedKey the 72-byte result:
	// nonce || ciphertext || tag.
	Nonce      string `json:"nonce"`
	WrappedKey string `json:"wrapped_key"`
}

// A PrivacyInvalidCase is one input the privacy layer MUST reject, at one of
// its three points of failure: the Ed25519-to-X25519 conversion, a
// per-recipient key unwrap, or the AEAD open of a private block's payload.
// Which fields are populated says which:
//
//   - PublicKey alone: a raw 32-byte Ed25519 public key the birational map
//     itself MUST refuse (a non-canonical y, or y = 1).
//   - Own and PeerPublicKey: Own names a key in the file's inputs, whose
//     private half is the author side of an agreement with PeerPublicKey, a
//     small-order public key the agreement MUST reject before any wrapping
//     key is derived.
//   - Own, Peer and WrappedKey: Own wrapped, Peer (by their private key)
//     attempts to unwrap WrappedKey — of the wrong length, or the right
//     length but tampered.
//   - ContentKey and Block: Block is a complete, decodable private block (not
//     necessarily one whose signature is meant to matter — see Reason) whose
//     enc, nonce or AAD-covered fields make it fail to open under
//     ContentKey, or whose enc is structurally too short to be a ciphertext
//     at all.
type PrivacyInvalidCase struct {
	Name   string `json:"name"`
	Rule   string `json:"rule"`
	Reason string `json:"reason"`

	// PublicKey is a raw Ed25519 public key the X25519 conversion MUST
	// refuse.
	PublicKey string `json:"public_key,omitempty"`
	// PeerPublicKey is a public key of small order, paired with Own for a
	// key-agreement case.
	PeerPublicKey string `json:"peer_public_key,omitempty"`
	// Own and Peer name keys in the file's inputs, the same convention as a
	// WrapCase's fields of the same name.
	Own  string `json:"own,omitempty"`
	Peer string `json:"peer,omitempty"`
	// WrappedKey is the bytes offered to Unwrap, hex-encoded. It is a pointer
	// so that a zero-length case — one of the lengths this section pins —
	// can be told apart from a case this field does not apply to at all:
	// omitempty on a string would drop an empty one exactly like an absent
	// one, and "" is itself one of the wrapped-key-length cases.
	WrappedKey *string `json:"wrapped_key,omitempty"`
	// ContentKey is the chain's symmetric key and Block a private block to
	// attempt opening under it.
	ContentKey string `json:"content_key,omitempty"`
	Block      string `json:"block,omitempty"`
}
