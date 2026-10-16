// Package textfile preserves the encoding and line endings of editable text.
package textfile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type Format struct {
	encoding string
	bom      bool
	newline  string
}

// Decode supports UTF-8 and BOM-marked UTF-16. Ambiguous legacy/binary data
// is rejected rather than silently replacing bytes in an editable document.
func Decode(data []byte) (string, Format, error) {
	f := Format{encoding: "utf-8", newline: "\n"}
	var text string
	if bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff}) {
		f.bom = true
		f.encoding = "utf-16le"
		var order binary.ByteOrder = binary.LittleEndian
		if data[0] == 0xfe {
			f.encoding = "utf-16be"
			order = binary.BigEndian
		}
		data = data[2:]
		if len(data)%2 != 0 {
			return "", f, errors.New("invalid UTF-16 text")
		}
		units := make([]uint16, len(data)/2)
		for i := range units {
			units[i] = order.Uint16(data[i*2:])
		}
		for i := 0; i < len(units); i++ {
			u := units[i]
			if u >= 0xd800 && u <= 0xdbff {
				if i+1 >= len(units) || units[i+1] < 0xdc00 || units[i+1] > 0xdfff {
					return "", f, errors.New("invalid UTF-16 surrogate pair")
				}
				i++
			} else if u >= 0xdc00 && u <= 0xdfff {
				return "", f, errors.New("invalid UTF-16 surrogate pair")
			}
		}
		text = string(utf16.Decode(units))
	} else {
		if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
			f.bom = true
			data = data[3:]
		}
		if !utf8.Valid(data) {
			return "", f, errors.New("unsupported text encoding; use UTF-8 or UTF-16 with a BOM")
		}
		text = string(data)
	}
	if strings.ContainsRune(text, 0) {
		return "", f, errors.New("binary or BOM-less UTF-16 files cannot be edited")
	}
	if strings.Contains(text, "\r\n") {
		f.newline = "\r\n"
	} else if strings.Contains(text, "\r") {
		f.newline = "\r"
	}
	return text, f, nil
}

func (f Format) Encode(text string) ([]byte, error) {
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return nil, errors.New("invalid text content")
	}
	// Browser textareas normalize line endings. Restore the original convention.
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	if f.newline != "" && f.newline != "\n" {
		text = strings.ReplaceAll(text, "\n", f.newline)
	}
	if f.encoding == "utf-16le" || f.encoding == "utf-16be" {
		units := utf16.Encode([]rune(text))
		result := make([]byte, 2+len(units)*2)
		var order binary.ByteOrder = binary.LittleEndian
		result[0], result[1] = 0xff, 0xfe
		if f.encoding == "utf-16be" {
			order = binary.BigEndian
			result[0], result[1] = 0xfe, 0xff
		}
		for i, u := range units {
			order.PutUint16(result[2+i*2:], u)
		}
		return result, nil
	}
	result := []byte(text)
	if f.bom {
		result = append([]byte{0xef, 0xbb, 0xbf}, result...)
	}
	return result, nil
}
