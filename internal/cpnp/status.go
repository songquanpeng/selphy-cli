package cpnp

import (
	"fmt"
	"strings"
)

type Status struct {
	State        string `json:"state"`
	StateCode    byte   `json:"state_code"`
	Tray         string `json:"tray"`
	Ink          string `json:"ink"`
	ErrorCode    uint32 `json:"error_code"`
	Error        string `json:"error"`
	Power        string `json:"power"`
	BatteryLevel byte   `json:"battery_level"`
	Firmware     string `json:"firmware"`
	Serial       string `json:"serial"`
	MinWidth     uint16 `json:"min_width"`
	MinHeight    uint16 `json:"min_height"`
	MaxWidth     uint16 `json:"max_width"`
	MaxHeight    uint16 `json:"max_height"`
	JPEG         bool   `json:"jpeg_supported"`
}

func label[T ~uint8 | ~uint16 | ~uint32](m map[T]string, k T) string {
	if v, ok := m[k]; ok {
		return v
	}
	return fmt.Sprintf("unknown (0x%x)", k)
}

var errorNames = map[uint32]string{0: "none", 266: "remove the printed sheet", 65793: "paper empty", 65794: "paper tray missing", 65795: "wrong paper tray size", 65798: "paper width error", 65799: "paper height error", 65801: "paper cover open", 65803: "paper feed sensor error", 65804: "wrong paper size", 65805: "incorrect paper loading", 66049: "ink empty", 66050: "ink cassette missing", 66051: "wrong ink size", 66052: "wrong ink type", 66053: "unknown ink cassette", 66306: "tray and ink missing", 66307: "tray and ink mismatch", 130819: "low battery", 261893: "JPEG decode error (CP)", 327427: "low battery (QX)", 16711940: "paper jam", 16712194: "ink cassette error", 16712198: "ink abnormal", 16776960: "printer error", 16776964: "command error", 16776965: "JPEG decode error (QX)", 16776966: "firmware error", 16776967: "battery error"}

func ErrorName(code uint32) string { return label(errorNames, code) }
func ParseStatus(b []byte) (Status, error) {
	if len(b) < 512 {
		return Status{}, fmt.Errorf("short connectionless status: %d < 512", len(b))
	}
	e := le.Uint32(b[68:])
	return Status{
		State: label(map[byte]string{1: "idle", 2: "busy", 241: "error", 255: "unknown"}, b[0]), StateCode: b[0],
		Tray:      label(map[byte]string{0: "unknown", 1: "none", 2: "cp-card", 3: "cp-l", 4: "cp-postcard", 255: "unsupported"}, b[2]),
		Ink:       label(map[byte]string{0: "unknown", 1: "none", 2: "cp-card", 3: "cp-l", 4: "cp-postcard", 5: "cp-postcard-54", 6: "cp-postcard-hd", 16: "qx-square", 18: "qx-card", 255: "unsupported"}, b[3]),
		ErrorCode: e, Error: ErrorName(e), Power: label(map[byte]string{0: "adapter", 1: "battery", 2: "usb"}, b[66]), BatteryLevel: b[67],
		Firmware: firmwareVersion(b[80:84]), Serial: strings.TrimRight(string(b[336:368]), "\x00"),
		MinWidth: le.Uint16(b[132:]), MinHeight: le.Uint16(b[134:]), MaxWidth: le.Uint16(b[136:]), MaxHeight: le.Uint16(b[138:]), JPEG: le.Uint32(b[144:])&1 != 0,
	}, nil
}

func firmwareVersion(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = string([]byte{v})
	}
	return strings.Join(parts, ".")
}

type ConnectedStatus struct {
	DeviceType byte   `json:"device_type"`
	StateCode  uint16 `json:"state_code"`
	State      string `json:"state"`
	ErrorCode  uint32 `json:"error_code"`
	Error      string `json:"error"`
	Request    uint32 `json:"request"`
	Retry      uint32 `json:"retry"`
	Offset     uint32 `json:"offset"`
	Length     uint32 `json:"length"`
	Spooled    byte   `json:"spooled"`
	Printed    byte   `json:"printed"`
}

func ParseConnected(b []byte) (ConnectedStatus, error) {
	if len(b) < 64 || le.Uint16(b) != 0 {
		return ConnectedStatus{}, fmt.Errorf("invalid connected status (%d bytes)", len(b))
	}
	code, e := le.Uint16(b[8:]), le.Uint32(b[12:])
	return ConnectedStatus{b[2], code, label(map[uint16]string{0: "idle", 15: "no-session", 255: "initializing", 240: "disconnecting", 65280: "canceling", 65281: "preparing", 65282: "heating-yellow", 65283: "printing-yellow", 65284: "heating-magenta", 65285: "printing-magenta", 65286: "heating-cyan", 65287: "printing-cyan", 65288: "heating-overcoat", 65289: "printing-overcoat", 65290: "finishing", 65291: "cooling-battery", 65292: "error-paused", 65293: "resume-waiting", 65296: "remove-sheet", 65535: "critical-error"}, code), e, ErrorName(e), le.Uint32(b[16:]), le.Uint32(b[20:]), le.Uint32(b[24:]), le.Uint32(b[28:]), b[40], b[41]}, nil
}
