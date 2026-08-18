package cid

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
)

// The worked example of spec/03-encoding.md, "Text representation of author
// keys": the alice test identity of vectors/blocks.json.
const (
	exampleKey         = "8a88e3dd7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801b40f6f5c"
	examplePrefixedKey = "ed01" + exampleKey
	exampleKeyText     = "b5uayvchd3v2at4mv7vjnwlj4xjoxfsthbg7r3fasdpzxjcabwqhw6xa"
)

func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestSpecAuthorKeyExample reproduces the worked example of
// spec/03-encoding.md, "Text representation of author keys", byte for byte.
func TestSpecAuthorKeyExample(t *testing.T) {
	key := mustDecodeHex(t, exampleKey)

	text, err := AuthorKeyText(key)
	if err != nil {
		t.Fatalf("AuthorKeyText: %v", err)
	}
	if text != exampleKeyText {
		t.Fatalf("AuthorKeyText = %s, want %s", text, exampleKeyText)
	}
	if len(text) != AuthorKeyTextSize {
		t.Errorf("text length = %d, want %d", len(text), AuthorKeyTextSize)
	}
	if !strings.HasPrefix(text, "b5ua") {
		t.Errorf("author key text %s does not start with b5ua", text)
	}

	// The bytes the text encodes are the multicodec prefix and the key.
	decoded, err := base32Lower.DecodeString(text[len(MultibaseBase32):])
	if err != nil {
		t.Fatalf("decoding the base32 body: %v", err)
	}
	if got := hex.EncodeToString(decoded); got != examplePrefixedKey {
		t.Errorf("prefixed bytes = %s, want %s", got, examplePrefixedKey)
	}

	back, err := ParseAuthorKeyText(text)
	if err != nil {
		t.Fatalf("ParseAuthorKeyText: %v", err)
	}
	if !bytes.Equal(back, key) {
		t.Errorf("round trip = %x, want %x", back, key)
	}
}

// TestAuthorKeyMulticodecVarint pins the one parameter of this encoding a
// reader is most likely to get wrong: 0xed is above the single-byte varint
// range, so the prefix is two bytes, not one.
func TestAuthorKeyMulticodecVarint(t *testing.T) {
	if MulticodecEd25519Pub != 0xed {
		t.Fatalf("ed25519-pub multicodec = 0x%02x, want 0xed", MulticodecEd25519Pub)
	}
	if MulticodecEd25519Pub < 0x80 {
		t.Fatal("0xed must be outside the single-byte varint range, or the prefix would be one byte")
	}
	want := []byte{0xed, 0x01}
	if !bytes.Equal(authorKeyPrefix[:], want) {
		t.Errorf("varint(0xed) = %x, want %x", authorKeyPrefix, want)
	}
	if AuthorKeyPrefixedSize != 34 || AuthorKeyTextSize != 56 {
		t.Errorf("prefixed size = %d and text size = %d, want 34 and 56", AuthorKeyPrefixedSize, AuthorKeyTextSize)
	}
}

// TestAuthorKeyTextRoundTrip renders and parses back real Ed25519 keys, and
// checks that the fixed prefix leaves the first four characters constant.
func TestAuthorKeyTextRoundTrip(t *testing.T) {
	for seed := byte(1); seed <= 8; seed++ {
		key, ok := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
		if !ok {
			t.Fatal("an Ed25519 private key does not yield an Ed25519 public key")
		}

		text, err := AuthorKeyText(key)
		if err != nil {
			t.Fatalf("AuthorKeyText: %v", err)
		}
		if len(text) != AuthorKeyTextSize || !strings.HasPrefix(text, "b5ua") {
			t.Errorf("key text %s is not %d characters beginning b5ua", text, AuthorKeyTextSize)
		}
		back, err := ParseAuthorKeyText(text)
		if err != nil {
			t.Fatalf("ParseAuthorKeyText(%s): %v", text, err)
		}
		if !bytes.Equal(back, key) {
			t.Errorf("round trip of %x produced %x", []byte(key), back)
		}
	}
}

// TestAuthorKeyTextIsNotACIDString checks the two text forms cannot be
// confused: neither parser accepts the other's output.
func TestAuthorKeyTextIsNotACIDString(t *testing.T) {
	if _, err := ParseCIDString(exampleKeyText); err == nil {
		t.Error("ParseCIDString accepted an author key string")
	}
	if _, err := ParseAuthorKeyText(exampleCIDString); err == nil {
		t.Error("ParseAuthorKeyText accepted a CID string")
	}
}

func TestAuthorKeyTextRejectsWrongLength(t *testing.T) {
	for _, n := range []int{0, 31, 33, 34, 64} {
		if got, err := AuthorKeyText(make([]byte, n)); err == nil {
			t.Errorf("AuthorKeyText(%d bytes) = %s, want an error", n, got)
		}
	}
}

func TestParseAuthorKeyTextRejects(t *testing.T) {
	body := exampleKeyText[len(MultibaseBase32):]

	// A well-formed multibase base32 string over 34 bytes whose multicodec
	// prefix is the wrong one — x25519-pub (0xec), which encodes as ec 01.
	wrongCodec := MultibaseBase32 + base32Lower.EncodeToString(mustDecodeHex(t, "ec01"+exampleKey))
	// The bare key with no multicodec prefix at all, 32 bytes.
	noCodec := MultibaseBase32 + base32Lower.EncodeToString(mustDecodeHex(t, exampleKey))

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "empty author key string"},
		{"no multibase prefix", body, "multibase code"},
		{"wrong multibase prefix (base58btc)", "z" + body, "multibase code"},
		{"wrong multibase prefix (base16)", "f" + examplePrefixedKey, "multibase code"},
		{"uppercase multibase prefix", "B" + strings.ToUpper(body), "multibase code"},
		{"uppercase body", "b" + strings.ToUpper(body), "lowercase base32"},
		{"padded", "b" + body + "==", "padding"},
		{"truncated", "b" + body[:len(body)-1], "want 56"},
		{"too long", exampleKeyText + "a", "want 56"},
		{"hex byte dump", examplePrefixedKey, "multibase code"},
		{"non-base32 character", "b" + body[:len(body)-1] + "1", "invalid base32"},
		{"wrong multicodec", wrongCodec, "multicodec"},
		{"no multicodec prefix", noCodec, "want 56"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := ParseAuthorKeyText(tc.in); err == nil {
				t.Fatalf("ParseAuthorKeyText(%q) = %x, want an error", tc.in, got)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("ParseAuthorKeyText(%q) error = %v, want it to mention %q", tc.in, err, tc.want)
			}
		})
	}
}

func TestParseAuthorKeyBytes(t *testing.T) {
	key, err := ParseAuthorKeyBytes(mustDecodeHex(t, examplePrefixedKey))
	if err != nil {
		t.Fatalf("ParseAuthorKeyBytes: %v", err)
	}
	if got := hex.EncodeToString(key); got != exampleKey {
		t.Errorf("key = %s, want %s", got, exampleKey)
	}

	for _, tc := range []struct {
		name  string
		bytes string
		want  string
	}{
		{"too short", "ed01" + exampleKey[2:], "34"},
		{"too long", "ed01" + exampleKey + "00", "34"},
		{"wrong codec", "ec01" + exampleKey, "multicodec"},
		{"single-byte varint", "00ed" + exampleKey, "multicodec"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := ParseAuthorKeyBytes(mustDecodeHex(t, tc.bytes)); err == nil {
				t.Fatalf("ParseAuthorKeyBytes(%s) = %x, want an error", tc.bytes, got)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestParseAuthorKeyBytesCopies checks the parser does not alias its input.
func TestParseAuthorKeyBytesCopies(t *testing.T) {
	prefixed := mustDecodeHex(t, examplePrefixedKey)
	key, err := ParseAuthorKeyBytes(prefixed)
	if err != nil {
		t.Fatal(err)
	}
	prefixed[AuthorKeyPrefixSize] ^= 0xff
	if got := hex.EncodeToString(key); got != exampleKey {
		t.Errorf("the parsed key changed with its input: %s", got)
	}
}
