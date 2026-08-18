package services

import (
	"archive/zip"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// decodeZipEntryName returns a UTF-8 zip entry path.
// Standard UTF-8 archives (UTF-8 flag set / NonUTF8=false with valid UTF-8) are kept as-is.
// Windows-localized archives mark NonUTF8 and store GBK/GB18030 path bytes; those are decoded
// even when the raw bytes happen to be valid UTF-8 mojibake.
func decodeZipEntryName(entry *zip.File) string {
	if entry == nil {
		return ""
	}
	name := entry.Name
	if !entry.NonUTF8 && utf8.ValidString(name) {
		return name
	}
	if decoded, ok := decodeGB18030Bytes([]byte(name)); ok {
		return decoded
	}
	return name
}

func decodeGB18030Bytes(raw []byte) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(raw)
	if err != nil {
		return "", false
	}
	if !utf8.Valid(decoded) || len(decoded) == 0 {
		return "", false
	}
	return string(decoded), true
}
