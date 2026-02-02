package filter

import (
	"testing"
)

func TestTCPPort80BPF(t *testing.T) {
	ins, err := TCPPort80BPF()
	if err != nil {
		t.Fatalf("TCPPort80BPF failed: %v", err)
	}
	if len(ins) == 0 {
		t.Error("Expected instructions, got 0")
	}
	// BPF 虚拟机校验比较复杂，这里只校验基本生成成功。
}
