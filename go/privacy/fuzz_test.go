package privacy

import (
	"bytes"
	"errors"
	"testing"

	"github.com/vrinek/Dialog/go/block"
)

// FuzzOpen asserts the property a decryptor must have before any other: it
// never panics. Whatever arrives in enc and nonce — a truncated ciphertext, a
// forgery, a valid ciphertext under another key — Open returns a payload or an
// error, and a payload it returns re-encodes to exactly the plaintext the AEAD
// produced, since the decoder is strict about canonical bytes.
func FuzzOpen(f *testing.F) {
	key := testContentKey(f, 0x11)
	author, err := block.NewBuilder(testKey(f, 1))
	if err != nil {
		f.Fatalf("NewBuilder: %v", err)
	}
	valid, err := SealBlock(author, key, samplePayload(), fixedRand(0x33))
	if err != nil {
		f.Fatalf("SealBlock: %v", err)
	}
	enc, _ := valid.Enc()
	nonce, _ := valid.Nonce()

	f.Add(enc, nonce)                                    // the real thing
	f.Add(enc[:TagSize], nonce)                          // a bare tag
	f.Add(bytes.Repeat([]byte{0}, TagSize), nonce)       // a zero ciphertext
	f.Add(append(append([]byte{}, enc...), 0x00), nonce) // one byte too long
	f.Add(enc, bytes.Repeat([]byte{0}, NonceSize))       // a zero nonce
	f.Add(enc, append([]byte{}, nonce[:NonceSize-1]...)) // a short nonce
	f.Add([]byte("not a ciphertext at all........."), nonce)

	f.Fuzz(func(t *testing.T, enc, nonce []byte) {
		// A block whose enc is shorter than the tag, or whose nonce is not 24
		// bytes, is structurally invalid and never reaches the AEAD
		// (spec/02-block-format.md, "Private block").
		b, err := block.Sign(block.Content{
			Version: block.Version, Type: block.TypePrivate,
			Enc: enc, Nonce: nonce,
		}, testKey(t, 1))
		if err != nil {
			if len(enc) >= TagSize && len(nonce) == NonceSize {
				t.Fatalf("a well-sized private block failed to sign: %v", err)
			}
			return
		}

		p, err := Open(b, key)
		if err != nil {
			return
		}
		encoded, err := p.Encode()
		if err != nil {
			t.Fatalf("Open returned a payload that does not re-encode: %v", err)
		}
		if len(encoded)+TagSize != len(enc) {
			t.Fatalf("the decoded payload is %d bytes but the ciphertext held %d", len(encoded), len(enc)-TagSize)
		}
	})
}

// FuzzOpenPlaintext fuzzes the other half of the decrypt path: bytes that
// authenticate. Anything a key holder seals is authentic by construction, so
// the strict decoding of the plaintext is all that stands between a malformed
// payload and the graph — and it too must never panic, whatever it is handed.
func FuzzOpenPlaintext(f *testing.F) {
	key := testContentKey(f, 0x11)
	canonical, err := samplePayload().Encode()
	if err != nil {
		f.Fatalf("Encode: %v", err)
	}
	f.Add(canonical)
	f.Add(append([]byte{}, canonical[:len(canonical)-1]...))
	f.Add([]byte{0xa3})
	f.Add([]byte{})
	f.Add([]byte("\xff\xff\xff\xff"))

	aead, err := newXChaCha(key[:])
	if err != nil {
		f.Fatalf("newXChaCha: %v", err)
	}
	nonce := bytes.Repeat([]byte{0x33}, NonceSize)

	f.Fuzz(func(t *testing.T, plaintext []byte) {
		author, err := block.NewBuilder(testKey(t, 1))
		if err != nil {
			t.Fatalf("NewBuilder: %v", err)
		}
		h := Header{Version: block.Version, Pub: testPub(t, 1)}
		aad, err := h.AAD()
		if err != nil {
			t.Fatalf("AAD: %v", err)
		}
		b, err := author.Private(aead.Seal(nil, nonce, plaintext, aad), nonce)
		if err != nil {
			t.Fatalf("Private: %v", err)
		}

		p, err := Open(b, key)
		if err != nil {
			// The ciphertext is ours, so it must authenticate; only the
			// plaintext can be at fault.
			if errors.Is(err, ErrAuthentication) {
				t.Fatalf("a ciphertext this test sealed failed to authenticate: %v", err)
			}
			return
		}
		encoded, err := p.Encode()
		if err != nil {
			t.Fatalf("Open returned a payload that does not re-encode: %v", err)
		}
		if !bytes.Equal(encoded, plaintext) {
			t.Fatalf("Open accepted a plaintext it does not reproduce:\n got %x\nwant %x", encoded, plaintext)
		}
	})
}
