package filter

import (
	"fmt"

	"golang.org/x/net/bpf"
)

func TCPPort80BPF() ([]bpf.RawInstruction, error) {
	// 这是 classic BPF（cBPF）过滤器，假设链路层为 Ethernet：
	// - 只放行 IPv4
	// - 只放行 TCP
	// - 只放行 src port=80 或 dst port=80
	//
	// 关键点：IPv4 头部长度不固定（options），因此需要使用 LoadMemShift：
	//   X = 4 * (packet[14] & 0x0f)  // 14 字节 Ethernet header 后的 IPv4 header length
	// 然后读取 TCP ports：src=[14+X], dst=[14+X+2]
	ins := []bpf.Instruction{
		bpf.LoadAbsolute{Off: 12, Size: 2},                         // EtherType
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0800, SkipFalse: 7}, // IPv4? 否则 drop

		bpf.LoadAbsolute{Off: 23, Size: 1},                        // IPv4 protocol
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 6, SkipFalse: 5},     // TCP? 否则 drop
		bpf.LoadMemShift{Off: 14},                                  // X = 4*(ip[0]&0xf)

		bpf.LoadIndirect{Off: 14, Size: 2},                        // tcp src port
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 80, SkipTrue: 3},     // src==80 -> accept
		bpf.LoadIndirect{Off: 16, Size: 2},                        // tcp dst port
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 80, SkipTrue: 1},     // dst==80 -> accept

		bpf.RetConstant{Val: 0},                                   // drop
		bpf.RetConstant{Val: 0xFFFF},                              // accept (snaplen 由 AF_PACKET 控制)
	}

	raw, err := bpf.Assemble(ins)
	if err != nil {
		return nil, fmt.Errorf("组装 BPF 失败：%w", err)
	}
	return raw, nil
}
