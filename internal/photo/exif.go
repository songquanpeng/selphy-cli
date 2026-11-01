package photo

import "encoding/binary"

// Read only the orientation IFD entry, with bounds checks on all offsets.
func exifOrientation(b []byte) int {
	if len(b) < 2 || b[0] != 0xff || b[1] != 0xd8 {
		return 1
	}
	for pos := 2; pos+4 <= len(b); {
		if b[pos] != 0xff {
			return 1
		}
		marker := b[pos+1]
		pos += 2
		if marker == 0xda || marker == 0xd9 {
			return 1
		}
		if marker == 0xff {
			pos--
			continue
		}
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		n := int(binary.BigEndian.Uint16(b[pos:]))
		if n < 2 || n > len(b)-pos {
			return 1
		}
		if marker == 0xe1 && n >= 8 && string(b[pos+2:pos+8]) == "Exif\x00\x00" {
			return tiffOrientation(b[pos+8 : pos+n])
		}
		pos += n
	}
	return 1
}
func tiffOrientation(b []byte) int {
	if len(b) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(b[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(b[2:]) != 42 {
		return 1
	}
	off := uint64(order.Uint32(b[4:]))
	if off+2 > uint64(len(b)) {
		return 1
	}
	count := int(order.Uint16(b[off:]))
	for i := 0; i < count; i++ {
		start := off + 2 + uint64(i)*12
		if start+12 > uint64(len(b)) {
			return 1
		}
		e := b[start : start+12]
		if order.Uint16(e) == 0x112 && order.Uint16(e[2:]) == 3 && order.Uint32(e[4:]) == 1 {
			v := int(order.Uint16(e[8:]))
			if v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}
