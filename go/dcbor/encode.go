package dcbor

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
)

// CBOR major types used by Dialog's profile.
const (
	majorUint  byte = 0
	majorNeg   byte = 1
	majorBytes byte = 2
	majorText  byte = 3
	majorArray byte = 4
	majorMap   byte = 5
	majorTag   byte = 6
	majorOther byte = 7
)

// aiNull is the additional-information value of the simple value null (0xf6).
const aiNull byte = 22

// tagDecimalFraction is CBOR tag 4, the only tag inside Dialog's profile
// (spec/03-encoding.md, "Deterministic CBOR" rule 6).
const tagDecimalFraction uint64 = 4

// Encode returns the canonical dCBOR encoding of v.
//
// It returns an error if v contains a duplicate map key, text that is not
// valid UTF-8, a non-canonical Decimal, a nil Value, or nesting deeper than
// MaxDepth.
func Encode(v Value) ([]byte, error) {
	var buf bytes.Buffer
	if err := encodeValue(&buf, v, 0); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MustEncode is Encode, panicking on error. It is meant for values known to
// be well-formed at the call site (tests, constants, generated vectors).
func MustEncode(v Value) []byte {
	b, err := Encode(v)
	if err != nil {
		panic(err)
	}
	return b
}

// encodeValue writes one value. depth is the nesting depth of the container
// holding it — 0 at the top level — so a container written here sits at
// depth+1 (spec/03-encoding.md, "Deterministic CBOR" rule 10).
func encodeValue(buf *bytes.Buffer, v Value, depth int) error {
	switch val := v.(type) {
	case Uint:
		writeHead(buf, majorUint, uint64(val))
	case Neg:
		writeHead(buf, majorNeg, uint64(val))
	case Decimal:
		// c4 82 <exponent> <mantissa>, both shortest-form integers
		// (spec/03-encoding.md, "Decimal fractions"). The tag and its content
		// array are one container for rule 10, holding two non-containers.
		if err := val.checkCanonical(); err != nil {
			return err
		}
		level, err := enter(depth)
		if err != nil {
			return err
		}
		writeHead(buf, majorTag, tagDecimalFraction)
		writeHead(buf, majorArray, 2)
		if err := encodeValue(buf, Int(val.Exponent), level); err != nil {
			return err
		}
		if err := encodeValue(buf, Int(val.Mantissa), level); err != nil {
			return err
		}
	case Text:
		if err := validText(string(val), "text string"); err != nil {
			return err
		}
		writeHead(buf, majorText, uint64(len(val)))
		buf.WriteString(string(val))
	case Bytes:
		writeHead(buf, majorBytes, uint64(len(val)))
		buf.Write(val)
	case Array:
		level, err := enter(depth)
		if err != nil {
			return err
		}
		writeHead(buf, majorArray, uint64(len(val)))
		for _, item := range val {
			if err := encodeValue(buf, item, level); err != nil {
				return err
			}
		}
	case Map:
		return encodeMap(buf, val, depth)
	case NullValue:
		buf.WriteByte(majorOther<<5 | aiNull)
	case nil:
		return fmt.Errorf("dcbor: nil value")
	default:
		return fmt.Errorf("dcbor: %T is not a dCBOR value", v)
	}
	return nil
}

// enter accounts for a container inside a container of the given depth,
// returning the new container's own depth. Rule 10 of spec/03-encoding.md puts
// the outermost container at depth 1 and bounds the deepest at MaxDepth.
func enter(depth int) (int, error) {
	level := depth + 1
	if level > MaxDepth {
		return 0, fmt.Errorf("dcbor: nesting deeper than %d levels", MaxDepth)
	}
	return level, nil
}

func encodeMap(buf *bytes.Buffer, m Map, depth int) error {
	level, err := enter(depth)
	if err != nil {
		return err
	}
	// Sort by the bytewise lexicographic order of each key's CBOR encoding
	// (spec/03-encoding.md, "Deterministic CBOR" rule 2).
	type encodedEntry struct {
		key   []byte
		plain string
		value Value
	}
	entries := make([]encodedEntry, 0, len(m))
	for _, e := range m {
		if err := validText(e.Key, "map key"); err != nil {
			return err
		}
		var kb bytes.Buffer
		writeHead(&kb, majorText, uint64(len(e.Key)))
		kb.WriteString(e.Key)
		entries = append(entries, encodedEntry{key: kb.Bytes(), plain: e.Key, value: e.Value})
	}
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].key, entries[j].key) < 0
	})
	for i := 1; i < len(entries); i++ {
		if bytes.Equal(entries[i-1].key, entries[i].key) {
			return fmt.Errorf("dcbor: duplicate map key %q", entries[i].plain)
		}
	}

	writeHead(buf, majorMap, uint64(len(entries)))
	for _, e := range entries {
		buf.Write(e.key)
		if err := encodeValue(buf, e.value, level); err != nil {
			return err
		}
	}
	return nil
}

// writeHead writes the shortest possible head (initial byte plus argument)
// for the given major type and argument (spec/03-encoding.md rule 1).
func writeHead(buf *bytes.Buffer, major byte, arg uint64) {
	switch {
	case arg < 24:
		buf.WriteByte(major<<5 | byte(arg))
	case arg <= 0xff:
		buf.WriteByte(major<<5 | 24)
		buf.WriteByte(byte(arg))
	case arg <= 0xffff:
		buf.WriteByte(major<<5 | 25)
		buf.Write(binary.BigEndian.AppendUint16(nil, uint16(arg)))
	case arg <= 0xffffffff:
		buf.WriteByte(major<<5 | 26)
		buf.Write(binary.BigEndian.AppendUint32(nil, uint32(arg)))
	default:
		buf.WriteByte(major<<5 | 27)
		buf.Write(binary.BigEndian.AppendUint64(nil, arg))
	}
}
