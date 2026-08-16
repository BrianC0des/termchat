package system

import (
	"strings"

	"github.com/skip2/go-qrcode"
)

// GenerateAsciiQR generates a compact ASCII QR code string for terminal display
func GenerateAsciiQR(content string) (string, error) {
	qr, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "", err
	}

	bitmap := qr.Bitmap()
	var sb strings.Builder

	// Render using half-block characters for high density
	for y := 0; y < len(bitmap); y += 2 {
		for x := 0; x < len(bitmap[y]); x++ {
			top := bitmap[y][x]
			bottom := false
			if y+1 < len(bitmap) {
				bottom = bitmap[y+1][x]
			}

			if top && bottom {
				sb.WriteString(" ")
			} else if top && !bottom {
				sb.WriteString("▄")
			} else if !top && bottom {
				sb.WriteString("▀")
			} else {
				sb.WriteString("█")
			}
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}
