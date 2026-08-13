package entity

import (
	"strings"
	"testing"

	"github.com/vrinek/Dialog/go/dcbor"
)

// TestParseTemplateVariables covers the grammar and every disambiguation case
// spelled out in spec/01-data-model.md, "Bonds".
func TestParseTemplateVariables(t *testing.T) {
	cases := []struct {
		template string
		want     []string
	}{
		// The examples the specification lists.
		{"_A_ is the capital of _B_", []string{"A", "B"}},
		{"_X_ founded _Y_", []string{"X", "Y"}},
		{"_A_ occurred before _B_", []string{"A", "B"}},

		// The disambiguation table.
		{"_AB_", []string{"AB"}},
		{"_A_B_", []string{"A"}},
		{"_A__B_", []string{"A", "B"}},
		{"type_of", nil},
		{"_a_", nil},

		// Neighbouring cases the grammar decides the same way.
		{"", nil},
		{"_", nil},
		{"__", nil},
		{"___", nil},
		{"____", nil},
		{"_A", nil},
		{"A_", nil},
		{"_1_", nil},
		{"_A1_", nil},
		{"_AB_C_", []string{"AB"}},
		{"__A_", []string{"A"}},
		{"_A_ _A_", []string{"A", "A"}}, // repeats are parsed; NewBond rejects them
		{"x_A_y", []string{"A"}},
		{"_LONGNAME_", []string{"LONGNAME"}},
		{"_A_ and _B_ and _C_", []string{"A", "B", "C"}},
		{"café _A_", []string{"A"}}, // multi-byte text before a variable
		{"_A_ ünïcödé _B_", []string{"A", "B"}},
	}
	for _, tc := range cases {
		t.Run(tc.template, func(t *testing.T) {
			got := ParseTemplateVariables(tc.template)
			if !equalStrings(got, tc.want) {
				t.Errorf("ParseTemplateVariables(%q) = %v, want %v", tc.template, got, tc.want)
			}
		})
	}
}

// TestNewBondRejects covers the validity rules of spec/01-data-model.md,
// "Bonds": non-empty, valid UTF-8, at least one variable, unique names.
func TestNewBondRejects(t *testing.T) {
	cases := []struct {
		name     string
		template string
		wantErr  string
	}{
		{"empty", "", "is empty"},
		{"no variables", "is the capital of", "declares no variables"},
		{"lowercase variable", "_a_ is the capital of _b_", "declares no variables"},
		{"unclosed variable", "_A is the capital of B_", "declares no variables"},
		{"literal underscores only", "type_of", "declares no variables"},
		{"duplicate variable", "_A_ is the same as _A_", "repeats the variable _A_"},
		{"duplicate after literal", "_A_ _B_ _A_", "repeats the variable _A_"},
		{"invalid UTF-8", "_A_ \xff", "not valid UTF-8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewBond(tc.template); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("NewBond(%q) error = %v, want one containing %q", tc.template, err, tc.wantErr)
			}
		})
	}
}

// TestDecodeBondRejects is the wire-format half of the same rules.
func TestDecodeBondRejects(t *testing.T) {
	cases := []struct {
		name    string
		value   dcbor.Value
		wantErr string
	}{
		{"empty map", dcbor.Map{}, "want exactly 1"},
		{"wrong key", dcbor.Map{{Key: "templates", Value: dcbor.Text("_A_ x")}}, `missing the "template" key`},
		{"extra key", dcbor.Map{{Key: keyTemplate, Value: dcbor.Text("_A_ x")}, {Key: "extra", Value: dcbor.Uint(1)}}, "want exactly 1"},
		{"template not text", dcbor.Map{{Key: keyTemplate, Value: dcbor.Uint(1)}}, "must be a text string"},
		{"template is bytes", dcbor.Map{{Key: keyTemplate, Value: dcbor.Bytes("_A_ x")}}, "must be a text string"},
		{"empty template", dcbor.Map{{Key: keyTemplate, Value: dcbor.Text("")}}, "is empty"},
		{"no variables", dcbor.Map{{Key: keyTemplate, Value: dcbor.Text("no variables here")}}, "declares no variables"},
		{"duplicate variables", dcbor.Map{{Key: keyTemplate, Value: dcbor.Text("_A_ and _A_")}}, "repeats the variable"},
		{"not a map", dcbor.Text("_A_ is the capital of _B_"), "must be a CBOR map"},
		{"array", dcbor.Array{dcbor.Text("_A_ x")}, "must be a CBOR map"},
		{"null", dcbor.Null, "must be a CBOR map"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := dcbor.MustEncode(tc.value)
			if _, err := DecodeBond(b); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("DecodeBond(%x) error = %v, want one containing %q", b, err, tc.wantErr)
			}
		})
	}

	// Bytes that are not dCBOR at all, and canonical bytes with a tail.
	for _, tc := range []struct {
		name    string
		bytes   []byte
		wantErr string
	}{
		{"trailing bytes", append(MustBond("_A_ x").Bytes(), 0x00), "trailing byte"},
		{"truncated", MustBond("_A_ x").Bytes()[:3], "exceeds the 1 byte(s) of remaining input"},
		{"truncated head", MustBond("_A_ x").Bytes()[:1], "exceeds the 0 byte(s) of remaining input"},
		{"empty input", nil, "unexpected end of input"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeBond(tc.bytes); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("DecodeBond error = %v, want one containing %q", err, tc.wantErr)
			}
		})
	}
}

// TestBondAccessors checks the incidental surface: variable copies are
// independent, and the zero value refuses to produce an identifier.
func TestBondAccessors(t *testing.T) {
	b := MustBond("_A_ is the capital of _B_")
	vars := b.Variables()
	vars[0] = "MUTATED"
	if got := b.Variables(); got[0] != "A" {
		t.Errorf("Variables() is not a copy: %v", got)
	}
	if b.VariableCount() != 2 {
		t.Errorf("VariableCount = %d, want 2", b.VariableCount())
	}
	bytesCopy := b.Bytes()
	bytesCopy[0] = 0xff
	if b.Bytes()[0] == 0xff {
		t.Errorf("Bytes() is not a copy")
	}
	if !strings.Contains(b.String(), "bafyrei") {
		t.Errorf("String() = %q, want it to carry the CID", b.String())
	}

	var zero Bond
	if got := zero.String(); got != "bond(invalid)" {
		t.Errorf("zero Bond String() = %q", got)
	}
	assertPanics(t, "zero Bond Digest", func() { zero.Digest() })
	assertPanics(t, "zero Bond Bytes", func() { zero.Bytes() })
	assertPanics(t, "zero Bond CID", func() { zero.CID() })
}

func assertPanics(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s did not panic", what)
		}
	}()
	f()
}
