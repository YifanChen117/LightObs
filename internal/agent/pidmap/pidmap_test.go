package pidmap

import (
	"testing"
	"unsafe"
)

func TestMakeKeyEndianness(t *testing.T) {
	srcIP := "192.168.1.1"
	dstIP := "10.0.0.1"
	srcPort := 12345
	dstPort := 80

	// Expected memory layout for 192.168.1.1 (C0 A8 01 01)
	// Kernel stores it as Network Order: C0 A8 01 01
	// We want our struct to have these bytes in memory.
	expectedSrcIPBytes := []byte{0xC0, 0xA8, 0x01, 0x01}

	key, ok := makeKeyNet(srcIP, srcPort, dstIP, dstPort)
	if !ok {
		t.Fatal("makeKeyNet failed")
	}

	// Verify SrcIP bytes in memory
	srcIPBytes := (*[4]byte)(unsafe.Pointer(&key.SrcIP))
	if *srcIPBytes != [4]byte(expectedSrcIPBytes) {
		t.Errorf("Expected SrcIP bytes %x, got %x", expectedSrcIPBytes, *srcIPBytes)
	}

	// Verify DstIP bytes
	expectedDstIPBytes := []byte{0x0A, 0x00, 0x00, 0x01}
	dstIPBytes := (*[4]byte)(unsafe.Pointer(&key.DstIP))
	if *dstIPBytes != [4]byte(expectedDstIPBytes) {
		t.Errorf("Expected DstIP bytes %x, got %x", expectedDstIPBytes, *dstIPBytes)
	}

	// Verify Port Endianness for makeKeyNet (Network Order)
	// 12345 = 0x3039. Network Order: 30 39.
	// makeKeyNet uses toNetPort, which swaps bytes.
	// 0x3039 (LE: 39 30). toNetPort -> 0x3930.
	// Memory of 0x3930 (LE): 30 39.
	// So memory matches Network Order.
	expectedSrcPortBytes := []byte{0x30, 0x39}
	srcPortBytes := (*[2]byte)(unsafe.Pointer(&key.SrcPort))
	if *srcPortBytes != [2]byte(expectedSrcPortBytes) {
		t.Errorf("Expected SrcPort bytes %x, got %x", expectedSrcPortBytes, *srcPortBytes)
	}
}

func TestMakeKeyHost(t *testing.T) {
	srcIP := "192.168.1.1"
	dstIP := "10.0.0.1"
	srcPort := 12345
	dstPort := 80

	// Host Order Test (assuming Little Endian machine for test)
	// SrcIP should still be Network Order bytes in memory because makeKeyHost uses To4() which gives network bytes,
	// and we store them as LittleEndian uint32.
	// Wait, makeKeyHost logic:
	// sip := net.ParseIP(srcIP).To4() -> [192, 168, 1, 1]
	// SrcIP: binary.LittleEndian.Uint32(sip) -> 0x0101A8C0 (in register)
	// Memory layout on LE machine: C0 A8 01 01. Matches Network Order.
	expectedSrcIPBytes := []byte{0xC0, 0xA8, 0x01, 0x01}

	key, ok := makeKeyHost(srcIP, srcPort, dstIP, dstPort)
	if !ok {
		t.Fatal("makeKeyHost failed")
	}

	srcIPBytes := (*[4]byte)(unsafe.Pointer(&key.SrcIP))
	if *srcIPBytes != [4]byte(expectedSrcIPBytes) {
		t.Errorf("Expected SrcIP bytes %x, got %x", expectedSrcIPBytes, *srcIPBytes)
	}

	// Port: makeKeyHost does NOT use toNetPort.
	// srcPort = 12345 (0x3039).
	// Memory on LE: 39 30.
	expectedSrcPortBytes := []byte{0x39, 0x30}
	srcPortBytes := (*[2]byte)(unsafe.Pointer(&key.SrcPort))
	if *srcPortBytes != [2]byte(expectedSrcPortBytes) {
		t.Errorf("Expected SrcPort bytes %x, got %x", expectedSrcPortBytes, *srcPortBytes)
	}
}

func TestToNetPort(t *testing.T) {
	// 80 = 0x0050
	// toNetPort(80) -> 0x5000
	if p := toNetPort(80); p != 0x5000 {
		t.Errorf("Expected 0x5000, got 0x%x", p)
	}

	// 8080 = 0x1F90
	// toNetPort(8080) -> 0x901F
	if p := toNetPort(8080); p != 0x901F {
		t.Errorf("Expected 0x901F, got 0x%x", p)
	}
}
