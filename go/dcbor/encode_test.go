package dcbor

import (
	"encoding/hex"
	"strings"
	"testing"
)

// mustHex decodes a hex literal used in the test tables.
func mustHex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatalf("bad hex literal %q: %v", s, err)
	}
	return b
}

func mustHexStatic(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// canonical holds a value and its one and only valid encoding.
type canonical struct {
	name string
	val  Value
	hex  string
}

// specTable covers the "CBOR encoding reference" table of spec/03-encoding.md
// plus the worked example from the same document.
var specTable = []canonical{
	{"map with 1 key", Map{{"description", Text("France")}}, "a16b6465736372697074696f6e664672616e6365"},
	{"map with 2 keys", Map{{"a", Uint(0)}, {"b", Uint(1)}}, "a2616100616201"},
	{"short text string", Text("France"), "664672616e6365"},
	{"empty text string", Text(""), "60"},
	{"text string of 23 bytes", Text(strings.Repeat("x", 23)), "77" + strings.Repeat("78", 23)},
	{"text string of 24 bytes", Text(strings.Repeat("x", 24)), "7818" + strings.Repeat("78", 24)},
	{"text string of 255 bytes", Text(strings.Repeat("x", 255)), "78ff" + strings.Repeat("78", 255)},
	{"text string of 256 bytes", Text(strings.Repeat("x", 256)), "790100" + strings.Repeat("78", 256)},
	{
		"32-byte byte string",
		Bytes(mustHexStatic("e57761b439ee0cbb7ef79422b0cce927d7d0147e00a5281cc173b0475512b842")),
		"5820e57761b439ee0cbb7ef79422b0cce927d7d0147e00a5281cc173b0475512b842",
	},
	{"empty byte string", Bytes(nil), "40"},
	{"null", Null, "f6"},
	{"integer 0", Uint(0), "00"},
	{"integer 1", Uint(1), "01"},
	{"empty array", Array{}, "80"},
	{"decimal fraction 3.14", Decimal{Exponent: -2, Mantissa: 314}, "c48221 19013a"},
}

// decimalTable covers the canonical tag 4 encoding
// (spec/03-encoding.md, "Decimal fractions").
var decimalTable = []canonical{
	{"decimal -3.14", Decimal{Exponent: -2, Mantissa: -314}, "c4822139 0139"},
	{"decimal 0.1", Decimal{Exponent: -1, Mantissa: 1}, "c4822001"},
	{"decimal -0.1", Decimal{Exponent: -1, Mantissa: -1}, "c4822020"},
	{"decimal smallest exponent", Decimal{Exponent: -1 << 63, Mantissa: 1}, "c4823b7fffffffffffffff01"},
	{"decimal largest mantissa", Decimal{Exponent: -1, Mantissa: 1<<63 - 1}, "c482201b7fffffffffffffff"},
	{"decimal most negative mantissa", Decimal{Exponent: -1, Mantissa: -1 << 63}, "c482203b7fffffffffffffff"},
	{"decimal in a molecule filler", Map{{"type", Uint(4)}, {"value", Decimal{Exponent: -2, Mantissa: 314}}}, "a2647479706504 6576616c7565 c4822119013a"},
}

// intTable exercises every boundary of the shortest-form integer encoding.
var intTable = []canonical{
	{"uint 23", Uint(23), "17"},
	{"uint 24", Uint(24), "1818"},
	{"uint 255", Uint(255), "18ff"},
	{"uint 256", Uint(256), "190100"},
	{"uint 65535", Uint(65535), "19ffff"},
	{"uint 65536", Uint(65536), "1a00010000"},
	{"uint 2^32-1", Uint(4294967295), "1affffffff"},
	{"uint 2^32", Uint(4294967296), "1b0000000100000000"},
	{"uint max", Uint(1<<64 - 1), "1bffffffffffffffff"},
	{"neg -1", Int(-1), "20"},
	{"neg -24", Int(-24), "37"},
	{"neg -25", Int(-25), "3818"},
	{"neg -256", Int(-256), "38ff"},
	{"neg -257", Int(-257), "390100"},
	{"neg -65536", Int(-65536), "39ffff"},
	{"neg -65537", Int(-65537), "3a00010000"},
	{"neg min int64", Int(-1 << 63), "3b7fffffffffffffff"},
	{"neg -2^64", Neg(1<<64 - 1), "3bffffffffffffffff"},
}

// structureTable covers nesting, key ordering and Dialog-shaped structures.
var structureTable = []canonical{
	{"array of ints", Array{Uint(0), Uint(1), Int(-1)}, "83000120"},
	{"nested array", Array{Array{Array{}}}, "818180"},
	{"map value null", Map{{"prev", Null}}, "a16470726576f6"},
	{
		// "b" encodes as 61 62 and "aa" as 62 61 61, so "b" sorts first:
		// ordering is over the encoded key, head byte included.
		"keys sorted by encoded head first",
		Map{{"aa", Uint(0)}, {"b", Uint(1)}},
		"a2616201626161 00",
	},
	{
		"keys sorted bytewise within a length",
		Map{{"c", Uint(2)}, {"a", Uint(0)}, {"b", Uint(1)}},
		"a3616100616201616302",
	},
	{
		"block-shaped map",
		Map{
			{"prev", Bytes(mustHexStatic("0000000000000000000000000000000000000000000000000000000000000001"))},
			{"ops", Array{Map{{"op", Text("create_atom")}}}},
			{"ts", Uint(1700000000)},
		},
		"a36274731a6553f100636f707381a1626f706b6372656174655f61746f6d64707265765820" +
			"0000000000000000000000000000000000000000000000000000000000000001",
	},
}

func allTables() []canonical {
	var all []canonical
	all = append(all, specTable...)
	all = append(all, intTable...)
	all = append(all, decimalTable...)
	all = append(all, structureTable...)
	return all
}

func TestEncodeCanonical(t *testing.T) {
	for _, tc := range allTables() {
		t.Run(tc.name, func(t *testing.T) {
			want := strings.ReplaceAll(tc.hex, " ", "")
			got, err := Encode(tc.val)
			if err != nil {
				t.Fatalf("Encode: unexpected error: %v", err)
			}
			if hex.EncodeToString(got) != want {
				t.Errorf("Encode =\n  %s\nwant\n  %s", hex.EncodeToString(got), want)
			}
		})
	}
}

func TestSpecWorkedExample(t *testing.T) {
	// spec/03-encoding.md, "Encoding an atom".
	const want = "a16b6465736372697074696f6e664672616e6365"
	got, err := Encode(Map{{"description", Text("France")}})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if hex.EncodeToString(got) != want {
		t.Fatalf("dCBOR({\"description\": \"France\"}) = %s, want %s", hex.EncodeToString(got), want)
	}
}

// TestSpecDecimalWorkedExample reproduces spec/03-encoding.md, "Encoding a
// decimal fraction".
func TestSpecDecimalWorkedExample(t *testing.T) {
	const want = "c4822119013a"
	got, err := Encode(Decimal{Exponent: -2, Mantissa: 314})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if hex.EncodeToString(got) != want {
		t.Fatalf("dCBOR(3.14) = %s, want %s", hex.EncodeToString(got), want)
	}
}

// TestEncodeRejectsNonCanonicalDecimal checks that the raw Encode path never
// emits a decimal fraction that violates the canonicalization rules of
// spec/03-encoding.md.
func TestEncodeRejectsNonCanonicalDecimal(t *testing.T) {
	tests := []struct {
		name string
		val  Decimal
		want string
	}{
		{"zero exponent", Decimal{Exponent: 0, Mantissa: 3}, "is not negative"},
		{"positive exponent", Decimal{Exponent: 2, Mantissa: 314}, "is not negative"},
		{"zero mantissa", Decimal{Exponent: -2, Mantissa: 0}, "mantissa is zero"},
		{"mantissa divisible by 10", Decimal{Exponent: -3, Mantissa: 3140}, "divisible by 10"},
		{"negative mantissa divisible by 10", Decimal{Exponent: -3, Mantissa: -3140}, "divisible by 10"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Encode(tc.val); err == nil {
				t.Fatalf("Encode(%+v) should have failed", tc.val)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Encode error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestNewDecimal(t *testing.T) {
	tests := []struct {
		name     string
		exp, man int64
		want     Value
	}{
		{"already canonical", -2, 314, Decimal{Exponent: -2, Mantissa: 314}},
		{"strips trailing zeros", -3, 3140, Decimal{Exponent: -2, Mantissa: 314}},
		{"strips several zeros", -5, 314000, Decimal{Exponent: -2, Mantissa: 314}},
		{"negative mantissa", -3, -3140, Decimal{Exponent: -2, Mantissa: -314}},
		{"whole number by cancellation", -2, 300, Uint(3)},
		{"negative whole number", -2, -300, Int(-3)},
		{"zero mantissa", -2, 0, Uint(0)},
		{"zero exponent", 0, 42, Uint(42)},
		{"positive exponent", 2, 314, Uint(31400)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewDecimal(tc.exp, tc.man)
			if err != nil {
				t.Fatalf("NewDecimal(%d, %d): %v", tc.exp, tc.man, err)
			}
			if !Equal(got, tc.want) {
				t.Errorf("NewDecimal(%d, %d) = %#v, want %#v", tc.exp, tc.man, got, tc.want)
			}
			// Whatever it returns must be encodable.
			if _, err := Encode(got); err != nil {
				t.Errorf("Encode(NewDecimal(%d, %d)): %v", tc.exp, tc.man, err)
			}
		})
	}

	if _, err := NewDecimal(30, 1<<62); err == nil {
		t.Error("NewDecimal should have reported int64 overflow")
	}
}

// TestTextIsNotNormalized pins the rule of spec/03-encoding.md, "Text strings
// and Unicode": content addressing operates on raw UTF-8 bytes, so the two
// Unicode forms of "é" are different strings and therefore different entities.
func TestTextIsNotNormalized(t *testing.T) {
	const (
		nfc = "café"  // é as U+00E9
		nfd = "café" // e + combining acute
	)
	a, err := Encode(Text(nfc))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encode(Text(nfd))
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(a) == hex.EncodeToString(b) {
		t.Fatal("NFC and NFD forms encoded identically; the encoder must not normalize")
	}
	if want := "65636166c3a9"; hex.EncodeToString(a) != want {
		t.Errorf("precomposed form encoded as %s, want %s", hex.EncodeToString(a), want)
	}
	if want := "6663616665cc81"; hex.EncodeToString(b) != want {
		t.Errorf("decomposed form encoded as %s, want %s", hex.EncodeToString(b), want)
	}
	// Both forms are valid UTF-8 and must round-trip unchanged.
	for _, s := range []string{nfc, nfd} {
		v, err := Decode(MustEncode(Text(s)))
		if err != nil {
			t.Fatalf("Decode(%q): %v", s, err)
		}
		if !Equal(v, Text(s)) {
			t.Errorf("Decode(Encode(%q)) = %#v; text must pass through byte for byte", s, v)
		}
	}
}

func TestEncodeSortsAndRejectsDuplicates(t *testing.T) {
	t.Run("unsorted input is sorted", func(t *testing.T) {
		unsorted := Map{{"z", Uint(1)}, {"a", Uint(0)}}
		sorted := Map{{"a", Uint(0)}, {"z", Uint(1)}}
		a, err := Encode(unsorted)
		if err != nil {
			t.Fatal(err)
		}
		b, err := Encode(sorted)
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(a) != hex.EncodeToString(b) {
			t.Errorf("entry order changed the encoding: %x vs %x", a, b)
		}
	})

	t.Run("duplicate key", func(t *testing.T) {
		_, err := Encode(Map{{"a", Uint(0)}, {"a", Uint(1)}})
		if err == nil {
			t.Fatal("Encode: expected an error for a duplicate key")
		}
		if !strings.Contains(err.Error(), `duplicate map key "a"`) {
			t.Errorf("Encode error = %v, want it to mention the duplicate key", err)
		}
	})

	t.Run("duplicate key nested", func(t *testing.T) {
		if _, err := Encode(Array{Map{{"k", Null}, {"k", Null}}}); err == nil {
			t.Fatal("Encode: expected an error for a duplicate key")
		}
	})
}

func TestEncodeErrors(t *testing.T) {
	tests := []struct {
		name string
		val  Value
		want string
	}{
		{"invalid UTF-8 text", Text("\xff\xfe"), "not valid UTF-8"},
		{"invalid UTF-8 map key", Map{{"\xff", Null}}, "not valid UTF-8"},
		{"nil value", Array{nil}, "nil value"},
		{"too deep", deepArray(MaxDepth + 1), "nesting deeper than"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Encode(tc.val)
			if err == nil {
				t.Fatalf("Encode: expected an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Encode error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func deepArray(depth int) Value {
	var v Value = Array{}
	for i := 0; i < depth; i++ {
		v = Array{v}
	}
	return v
}

func TestMustEncodePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustEncode did not panic on an invalid value")
		}
	}()
	MustEncode(Map{{"a", Uint(0)}, {"a", Uint(0)}})
}

func TestEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b Value
		want bool
	}{
		{"same uint", Uint(1), Uint(1), true},
		{"uint vs neg", Uint(1), Neg(1), false},
		{"uint vs text", Uint(1), Text("1"), false},
		{"same text", Text("a"), Text("a"), true},
		{"same bytes", Bytes{1, 2}, Bytes{1, 2}, true},
		{"different bytes", Bytes{1, 2}, Bytes{1, 3}, false},
		{"empty bytes vs nil bytes", Bytes{}, Bytes(nil), true},
		{"same array", Array{Uint(1)}, Array{Uint(1)}, true},
		{"different array length", Array{Uint(1)}, Array{Uint(1), Uint(2)}, false},
		{"map order irrelevant", Map{{"a", Uint(0)}, {"b", Uint(1)}}, Map{{"b", Uint(1)}, {"a", Uint(0)}}, true},
		{"map different value", Map{{"a", Uint(0)}}, Map{{"a", Uint(1)}}, false},
		{"map different key", Map{{"a", Uint(0)}}, Map{{"b", Uint(0)}}, false},
		{"map duplicate keys not equal", Map{{"a", Uint(0)}, {"a", Uint(0)}}, Map{{"a", Uint(0)}, {"b", Uint(0)}}, false},
		{"same decimal", Decimal{-2, 314}, Decimal{-2, 314}, true},
		{"different decimal exponent", Decimal{-2, 314}, Decimal{-1, 314}, false},
		{"different decimal mantissa", Decimal{-2, 314}, Decimal{-2, -314}, false},
		{"decimal vs uint", Decimal{-2, 314}, Uint(314), false},
		{"null", Null, Null, true},
		{"null vs uint", Null, Uint(0), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Equal(tc.a, tc.b); got != tc.want {
				t.Errorf("Equal = %v, want %v", got, tc.want)
			}
			if got := Equal(tc.b, tc.a); got != tc.want {
				t.Errorf("Equal (reversed) = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIntHelpers(t *testing.T) {
	for _, i := range []int64{0, 1, -1, 23, 24, -24, -25, 1 << 40, -1 << 40, 1<<63 - 1, -1 << 63} {
		v := Int(i)
		var got int64
		var ok bool
		switch n := v.(type) {
		case Uint:
			got, ok = n.Int64()
		case Neg:
			got, ok = n.Int64()
		default:
			t.Fatalf("Int(%d) returned %T", i, v)
		}
		if !ok || got != i {
			t.Errorf("Int(%d).Int64() = %d, %v", i, got, ok)
		}
	}
	if _, ok := Uint(1 << 63).Int64(); ok {
		t.Error("Uint(2^63).Int64() should not fit in an int64")
	}
	if _, ok := Neg(1 << 63).Int64(); ok {
		t.Error("Neg(2^63).Int64() should not fit in an int64")
	}
}

func TestMapGet(t *testing.T) {
	m := Map{{"a", Uint(1)}}
	if v, ok := m.Get("a"); !ok || !Equal(v, Uint(1)) {
		t.Errorf("Get(a) = %v, %v", v, ok)
	}
	if _, ok := m.Get("b"); ok {
		t.Error("Get(b) should report missing")
	}
}
