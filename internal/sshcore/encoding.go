package sshcore

import (
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
)

// The canonical name used when no conversion is needed.
const EncodingUTF8 = "UTF-8"

// supportedEncodings is the list offered to the user in the UI.
var supportedEncodings = []string{
	"UTF-8",
	"GBK",
	"GB18030",
	"GB2312",
	"Big5",
	"Shift_JIS",
	"EUC-JP",
	"ISO-2022-JP",
	"EUC-KR",
	"ISO-8859-1",
	"Windows-1252",
	"KOI8-R",
	"UTF-16LE",
	"UTF-16BE",
}

// encodings maps every accepted alias to its implementation. GB2312 and GBK
// share an implementation because GBK is a strict superset and decoding with
// the larger table never loses information.
var encodings = map[string]encoding.Encoding{
	"UTF-8":        unicode.UTF8,
	"UTF8":         unicode.UTF8,
	"GBK":          simplifiedchinese.GBK,
	"GB2312":       simplifiedchinese.GBK,
	"GB18030":      simplifiedchinese.GB18030,
	"BIG5":         traditionalchinese.Big5,
	"SHIFT_JIS":    japanese.ShiftJIS,
	"SJIS":         japanese.ShiftJIS,
	"EUC-JP":       japanese.EUCJP,
	"ISO-2022-JP":  japanese.ISO2022JP,
	"EUC-KR":       korean.EUCKR,
	"ISO-8859-1":   charmap.ISO8859_1,
	"WINDOWS-1252": charmap.Windows1252,
	"KOI8-R":       charmap.KOI8R,
	"UTF-16LE":     unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM),
	"UTF-16BE":     unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM),
}

// SupportedEncodings returns the user selectable encoding names.
func SupportedEncodings() []string {
	out := make([]string, len(supportedEncodings))
	copy(out, supportedEncodings)
	return out
}

// normalizeEncoding resolves an encoding name to its canonical form. Unknown
// names fall back to UTF-8 so a bad configuration can never break a session.
func normalizeEncoding(name string) string {
	upper := strings.ToUpper(strings.TrimSpace(name))
	if _, ok := encodings[upper]; !ok {
		return EncodingUTF8
	}
	if upper == "UTF8" {
		return EncodingUTF8
	}
	for _, canonical := range supportedEncodings {
		if strings.ToUpper(canonical) == upper {
			return canonical
		}
	}
	return EncodingUTF8
}

// isUTF8 reports whether the encoding needs no conversion in either direction.
func isUTF8(name string) bool {
	return normalizeEncoding(name) == EncodingUTF8
}