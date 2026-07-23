// Command genicons regenerates assets/app-icon.ico from the shared logo
// renderer. Run from the repository root:
//
//	go run ./scripts/genicons
//
// then `make generate` to re-embed the icon into the Windows resources.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/png"
	"os"

	"github.com/tokitoki-dev/tokitoki-windows/internal/logo"
)

// sizes are the standard Windows application icon sizes: 16–32 for lists and
// the taskbar at common DPI scales, 48–256 for Explorer views and tiles.
var sizes = []int{16, 20, 24, 32, 48, 64, 128, 256}

const output = "assets/app-icon.ico"

func main() {
	entries := make([][]byte, 0, len(sizes))
	for _, size := range sizes {
		img := logo.AppIcon(size)
		if size >= 256 {
			// PNG-compressed entries are the convention for 256 px — a raw
			// DIB at that size would quadruple the resource.
			var buf bytes.Buffer
			if err := png.Encode(&buf, img); err != nil {
				fatal(err)
			}
			entries = append(entries, buf.Bytes())
			continue
		}
		entries = append(entries, dibEntry(size))
	}

	if err := os.WriteFile(output, icoFile(entries), 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %s (%d sizes)\n", output, len(sizes))
}

// dibEntry encodes one icon image as a classic 32-bit BMP DIB: header with
// doubled height, bottom-up BGRA rows, then an all-zero AND mask (the alpha
// channel governs transparency).
func dibEntry(size int) []byte {
	img := logo.AppIcon(size)
	maskStride := ((size + 31) / 32) * 4

	var buf bytes.Buffer
	header := make([]byte, 40)
	binary.LittleEndian.PutUint32(header[0:], 40)
	binary.LittleEndian.PutUint32(header[4:], uint32(size))
	binary.LittleEndian.PutUint32(header[8:], uint32(size*2))
	binary.LittleEndian.PutUint16(header[12:], 1)
	binary.LittleEndian.PutUint16(header[14:], 32)
	binary.LittleEndian.PutUint32(header[20:], uint32(size*size*4+maskStride*size))
	buf.Write(header)

	for y := size - 1; y >= 0; y-- {
		for x := 0; x < size; x++ {
			pixel := img.NRGBAAt(x, y)
			buf.Write([]byte{pixel.B, pixel.G, pixel.R, pixel.A})
		}
	}
	buf.Write(make([]byte, maskStride*size))
	return buf.Bytes()
}

// icoFile assembles the ICONDIR container around the encoded images.
func icoFile(entries [][]byte) []byte {
	var buf bytes.Buffer
	head := make([]byte, 6)
	binary.LittleEndian.PutUint16(head[2:], 1)
	binary.LittleEndian.PutUint16(head[4:], uint16(len(entries)))
	buf.Write(head)

	offset := 6 + 16*len(entries)
	for index, data := range entries {
		entry := make([]byte, 16)
		side := sizes[index]
		if side < 256 {
			entry[0] = byte(side)
			entry[1] = byte(side)
		}
		binary.LittleEndian.PutUint16(entry[4:], 1)
		binary.LittleEndian.PutUint16(entry[6:], 32)
		binary.LittleEndian.PutUint32(entry[8:], uint32(len(data)))
		binary.LittleEndian.PutUint32(entry[12:], uint32(offset))
		buf.Write(entry)
		offset += len(data)
	}
	for _, data := range entries {
		buf.Write(data)
	}
	return buf.Bytes()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "genicons:", err)
	os.Exit(1)
}
