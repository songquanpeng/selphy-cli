// Package cpnp implements Canon's SELPHY network protocol, independently
// reconstructed from SELPHY Photo Layout 4.4.0. See docs/protocol.md.
package cpnp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

const Port = 8609
const maxPayload = 2 << 20

var be = binary.BigEndian
var le = binary.LittleEndian

type packet struct {
	Command             byte
	Result, ID, Session uint16
	Payload             []byte
}

func encode(command byte, id, session uint16, payload []byte) []byte {
	b := make([]byte, 16+len(payload))
	copy(b, "CPNP")
	b[4], b[5] = 1, command
	be.PutUint16(b[8:], id)
	be.PutUint16(b[10:], session)
	be.PutUint32(b[12:], uint32(len(payload)))
	copy(b[16:], payload)
	return b
}

func decode(b []byte) (packet, error) {
	if len(b) < 16 || !bytes.Equal(b[:4], []byte("CPNP")) || b[4] != 0x81 {
		return packet{}, fmt.Errorf("invalid CPNP response header")
	}
	n := be.Uint32(b[12:16])
	if n > maxPayload || uint64(n)+16 != uint64(len(b)) {
		return packet{}, fmt.Errorf("invalid CPNP payload length %d (received %d)", n, len(b)-16)
	}
	p := packet{b[5], be.Uint16(b[6:]), be.Uint16(b[8:]), be.Uint16(b[10:]), b[16:]}
	if p.Result != 0 {
		return p, fmt.Errorf("CPNP command 0x%02x rejected: result 0x%04x%s", p.Command, p.Result, map[uint16]string{0x8501: " (session already exists)", 0x8502: " (printer busy; close the phone app and retry)"}[p.Result])
	}
	return p, nil
}

func readPacket(r io.Reader) (packet, error) {
	h := make([]byte, 16)
	if _, err := io.ReadFull(r, h); err != nil {
		return packet{}, err
	}
	if string(h[:4]) != "CPNP" || h[4] != 0x81 {
		return packet{}, fmt.Errorf("invalid CPNP TCP header")
	}
	n := be.Uint32(h[12:])
	if n > maxPayload {
		return packet{}, fmt.Errorf("CPNP response too large: %d", n)
	}
	b := make([]byte, 16+int(n))
	copy(b, h)
	if _, err := io.ReadFull(r, b[16:]); err != nil {
		return packet{}, err
	}
	return decode(b)
}

func command(code uint16, size int) []byte {
	b := make([]byte, size)
	le.PutUint16(b[2:], code)
	le.PutUint32(b[4:], uint32(size))
	return b
}
