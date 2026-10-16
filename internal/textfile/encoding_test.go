package textfile

import (
	"bytes"
	"strings"
	"testing"
)

func TestTextEncodingRoundTripAndBrowserNewlines(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte("a\r\nمرحبا 😀"), append([]byte{0xef, 0xbb, 0xbf}, []byte("a\r\nمرحبا 😀")...),
		{0xff, 0xfe, 'a', 0, 13, 0, 10, 0, 0x27, 0x06, 0x3d, 0xd8, 0x00, 0xde},
		{0xfe, 0xff, 0, 'a', 0, 13, 0, 10, 0x06, 0x27, 0xd8, 0x3d, 0xde, 0x00},
	} {
		text, format, err := Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if strings.ContainsRune(text, '\ufffd') {
			t.Fatal("text was replaced", text)
		}
		encoded, err := format.Encode(strings.ReplaceAll(text, "\r\n", "\n"))
		if err != nil || !bytes.Equal(raw, encoded) {
			t.Fatalf("round trip changed bytes: %x -> %x (%v)", raw, encoded, err)
		}
	}
}

func TestTextEncodingRejectsAmbiguousOrMalformedData(t *testing.T) {
	for _, raw := range [][]byte{{0xff}, {'a', 0}, {0xff, 0xfe, 1}, {0xff, 0xfe, 0, 0xd8}, {0xff, 0xfe, 0, 0xdc}, {0xfe, 0xff, 0xd8, 0, 'a', 0}} {
		if _, _, err := Decode(raw); err == nil {
			t.Fatalf("accepted invalid text: %x", raw)
		}
	}
}
