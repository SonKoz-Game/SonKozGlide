package glide

import "encoding/binary"

func splitPoint(data []byte, s strategy) int {
	if s.SplitSNI {
		if off, ln, ok := sniLocation(data); ok && ln > 1 {
			return off + ln/2
		}
	}
	if s.SplitPos > 0 {
		return s.SplitPos
	}
	return 1
}

func sniLocation(data []byte) (int, int, bool) {
	if len(data) < 43 || data[0] != 0x16 || data[5] != 0x01 {
		return 0, 0, false
	}
	pos := 5 + 4 + 2 + 32
	if pos >= len(data) {
		return 0, 0, false
	}
	sidLen := int(data[pos])
	pos += 1 + sidLen
	if pos+2 > len(data) {
		return 0, 0, false
	}
	csLen := int(binary.BigEndian.Uint16(data[pos:]))
	pos += 2 + csLen
	if pos+1 > len(data) {
		return 0, 0, false
	}
	compLen := int(data[pos])
	pos += 1 + compLen
	if pos+2 > len(data) {
		return 0, 0, false
	}
	extTotal := int(binary.BigEndian.Uint16(data[pos:]))
	pos += 2
	end := pos + extTotal
	if end > len(data) {
		end = len(data)
	}
	for pos+4 <= end {
		extType := binary.BigEndian.Uint16(data[pos:])
		extLen := int(binary.BigEndian.Uint16(data[pos+2:]))
		body := pos + 4
		if body+extLen > len(data) {
			return 0, 0, false
		}
		if extType == 0x0000 {
			return parseServerName(data, body, extLen)
		}
		pos = body + extLen
	}
	return 0, 0, false
}

func parseServerName(data []byte, body, extLen int) (int, int, bool) {
	if extLen < 5 {
		return 0, 0, false
	}
	p := body + 2
	if p+3 > body+extLen {
		return 0, 0, false
	}
	nameType := data[p]
	nameLen := int(binary.BigEndian.Uint16(data[p+1:]))
	start := p + 3
	if nameType != 0 || start+nameLen > body+extLen {
		return 0, 0, false
	}
	return start, nameLen, true
}
