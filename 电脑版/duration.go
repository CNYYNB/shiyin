package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"strings"
)

// probeDuration returns the duration in milliseconds, or 0 if unknown.
// The UI also reports the real duration back once the <audio> element has loaded it.
func probeDuration(path string) int64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0
	}
	size := st.Size()
	ext := strings.ToLower(strings.TrimPrefix(fileExt(path), "."))
	defer func() { recover() }()
	switch ext {
	case "flac":
		return durFLAC(f)
	case "wav":
		return durWAV(f)
	case "mp3":
		return durMP3(f, size)
	case "m4a", "mp4", "aac", "alac":
		if d := durMP4(f, size); d > 0 {
			return d
		}
		return durADTS(f, size)
	case "ogg", "oga", "opus":
		return durOGG(f, size)
	}
	return 0
}

func id3v2Size(f io.ReadSeeker) int64 {
	h := make([]byte, 10)
	f.Seek(0, io.SeekStart)
	if _, err := io.ReadFull(f, h); err != nil || string(h[:3]) != "ID3" {
		return 0
	}
	sz := int64(h[6]&0x7f)<<21 | int64(h[7]&0x7f)<<14 | int64(h[8]&0x7f)<<7 | int64(h[9]&0x7f)
	sz += 10
	if h[5]&0x10 != 0 {
		sz += 10
	}
	return sz
}

func durFLAC(f *os.File) int64 {
	off := id3v2Size(f)
	f.Seek(off, io.SeekStart)
	h := make([]byte, 4+4+34)
	if _, err := io.ReadFull(f, h); err != nil || string(h[:4]) != "fLaC" {
		return 0
	}
	si := h[8:]
	rate := int64(si[10])<<12 | int64(si[11])<<4 | int64(si[12])>>4
	total := int64(si[13]&0x0f)<<32 | int64(binary.BigEndian.Uint32(si[14:18]))
	if rate == 0 {
		return 0
	}
	return total * 1000 / rate
}

func durWAV(f *os.File) int64 {
	h := make([]byte, 12)
	if _, err := io.ReadFull(f, h); err != nil || string(h[:4]) != "RIFF" || string(h[8:12]) != "WAVE" {
		return 0
	}
	var byteRate int64
	ch := make([]byte, 8)
	for i := 0; i < 64; i++ {
		if _, err := io.ReadFull(f, ch); err != nil {
			return 0
		}
		id := string(ch[:4])
		sz := int64(binary.LittleEndian.Uint32(ch[4:]))
		if id == "fmt " {
			b := make([]byte, sz)
			if _, err := io.ReadFull(f, b); err != nil || len(b) < 12 {
				return 0
			}
			byteRate = int64(binary.LittleEndian.Uint32(b[8:12]))
			if sz%2 == 1 {
				f.Seek(1, io.SeekCurrent)
			}
			continue
		}
		if id == "data" {
			if byteRate == 0 {
				return 0
			}
			return sz * 1000 / byteRate
		}
		f.Seek(sz+sz%2, io.SeekCurrent)
	}
	return 0
}

var mp3Bitrates = [2][3][16]int{
	{ // MPEG1
		{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448, 0},
		{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, 0},
		{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},
	},
	{ // MPEG2/2.5
		{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256, 0},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
	},
}
var mp3Rates = [4][3]int{{11025, 12000, 8000}, {0, 0, 0}, {22050, 24000, 16000}, {44100, 48000, 32000}}

func durMP3(f *os.File, size int64) int64 {
	start := id3v2Size(f)
	f.Seek(start, io.SeekStart)
	buf := make([]byte, 64*1024)
	n, _ := io.ReadFull(f, buf)
	buf = buf[:n]
	for i := 0; i+4 < len(buf); i++ {
		if buf[i] != 0xff || buf[i+1]&0xe0 != 0xe0 {
			continue
		}
		ver := (buf[i+1] >> 3) & 3 // 3=MPEG1 2=MPEG2 0=MPEG2.5
		layer := (buf[i+1] >> 1) & 3
		bri := buf[i+2] >> 4
		sri := (buf[i+2] >> 2) & 3
		if ver == 1 || layer == 0 || bri == 0 || bri == 15 || sri == 3 {
			continue
		}
		v := 1
		if ver == 3 {
			v = 0
		}
		li := 3 - int(layer) // 0=L1 1=L2 2=L3
		br := mp3Bitrates[v][li][bri] * 1000
		sr := mp3Rates[ver][sri]
		if br == 0 || sr == 0 {
			continue
		}
		spf := 1152
		if li == 0 {
			spf = 384
		} else if li == 2 && v == 1 {
			spf = 576
		}
		mono := (buf[i+3] >> 6) == 3
		// Xing / Info
		var sideLen int
		if v == 0 {
			sideLen = 32
			if mono {
				sideLen = 17
			}
		} else {
			sideLen = 17
			if mono {
				sideLen = 9
			}
		}
		x := i + 4 + sideLen
		if x+12 < len(buf) {
			tag := string(buf[x : x+4])
			if tag == "Xing" || tag == "Info" {
				flags := binary.BigEndian.Uint32(buf[x+4:])
				if flags&1 != 0 {
					frames := int64(binary.BigEndian.Uint32(buf[x+8:]))
					return frames * int64(spf) * 1000 / int64(sr)
				}
			}
		}
		vb := i + 4 + 32
		if vb+18 < len(buf) && string(buf[vb:vb+4]) == "VBRI" {
			frames := int64(binary.BigEndian.Uint32(buf[vb+14:]))
			return frames * int64(spf) * 1000 / int64(sr)
		}
		audio := size - start - int64(i)
		if size > 128 {
			t := make([]byte, 3)
			f.ReadAt(t, size-128)
			if string(t) == "TAG" {
				audio -= 128
			}
		}
		return audio * 8 * 1000 / int64(br)
	}
	return 0
}

func durMP4(f *os.File, size int64) int64 {
	var walk func(start, end int64, depth int) int64
	walk = func(start, end int64, depth int) int64 {
		pos := start
		h := make([]byte, 16)
		for pos+8 <= end {
			if _, err := f.ReadAt(h[:8], pos); err != nil {
				return 0
			}
			sz := int64(binary.BigEndian.Uint32(h[:4]))
			typ := string(h[4:8])
			hdr := int64(8)
			if sz == 1 {
				if _, err := f.ReadAt(h[8:16], pos+8); err != nil {
					return 0
				}
				sz = int64(binary.BigEndian.Uint64(h[8:16]))
				hdr = 16
			} else if sz == 0 {
				sz = end - pos
			}
			if sz < hdr {
				return 0
			}
			switch typ {
			case "moov":
				if depth == 0 {
					return walk(pos+hdr, pos+sz, 1)
				}
			case "mvhd":
				b := make([]byte, 32)
				f.ReadAt(b, pos+hdr)
				if b[0] == 1 {
					ts := int64(binary.BigEndian.Uint32(b[20:24]))
					d := int64(binary.BigEndian.Uint64(b[24:32]))
					if ts > 0 {
						return d * 1000 / ts
					}
				} else {
					ts := int64(binary.BigEndian.Uint32(b[12:16]))
					d := int64(binary.BigEndian.Uint32(b[16:20]))
					if ts > 0 {
						return d * 1000 / ts
					}
				}
				return 0
			}
			pos += sz
		}
		return 0
	}
	return walk(0, size, 0)
}

func durADTS(f *os.File, size int64) int64 {
	rates := []int64{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}
	pos := id3v2Size(f)
	h := make([]byte, 7)
	var frames, rate int64
	for pos+7 <= size && frames < 200000 {
		if _, err := f.ReadAt(h, pos); err != nil {
			break
		}
		if h[0] != 0xff || h[1]&0xf6 != 0xf0 {
			break
		}
		ri := (h[2] >> 2) & 0xf
		if int(ri) < len(rates) {
			rate = rates[ri]
		}
		flen := int64(h[3]&3)<<11 | int64(h[4])<<3 | int64(h[5])>>5
		if flen < 7 {
			break
		}
		frames++
		pos += flen
	}
	if rate == 0 {
		return 0
	}
	return frames * 1024 * 1000 / rate
}

func durOGG(f *os.File, size int64) int64 {
	head := make([]byte, 4096)
	n, _ := f.ReadAt(head, 0)
	head = head[:n]
	var rate, preskip int64
	if i := bytes.Index(head, []byte("OpusHead")); i >= 0 && i+12 <= len(head) {
		rate = 48000
		preskip = int64(binary.LittleEndian.Uint16(head[i+10:]))
	} else if i := bytes.Index(head, []byte("\x01vorbis")); i >= 0 && i+16 <= len(head) {
		rate = int64(binary.LittleEndian.Uint32(head[i+12:]))
	} else if i := bytes.Index(head, []byte("\x7fFLAC")); i >= 0 && i+9+8+18 <= len(head) {
		si := head[i+9+8:]
		rate = int64(si[10])<<12 | int64(si[11])<<4 | int64(si[12])>>4
	}
	if rate == 0 {
		return 0
	}
	tailLen := int64(256 * 1024)
	if tailLen > size {
		tailLen = size
	}
	tail := make([]byte, tailLen)
	f.ReadAt(tail, size-tailLen)
	i := bytes.LastIndex(tail, []byte("OggS"))
	for i >= 0 && i+14 > len(tail) {
		i = bytes.LastIndex(tail[:i], []byte("OggS"))
	}
	if i < 0 {
		return 0
	}
	gran := int64(binary.LittleEndian.Uint64(tail[i+6:]))
	if gran <= preskip {
		return 0
	}
	return (gran - preskip) * 1000 / rate
}
