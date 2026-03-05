package filter

import (
	"encoding/binary"
	"fmt"
	"net"

	"golang.org/x/net/bpf"
)

// TCPPortBPF 生成一个 classic BPF 过滤器，只放行以下二者之一的包：
//   - dst port == port（client -> server 方向的请求包）
//   - src port == port（server -> client 方向的响应包）
//
// 这是向下兼容的通用版本，不做 IP 层过滤。
// targetPort 为 0 时使用 80（向下兼容旧行为）。
func TCPPortBPF(targetPort int) ([]bpf.RawInstruction, error) {
	if targetPort == 0 {
		targetPort = 80
	}
	if targetPort < 1 || targetPort > 65535 {
		return nil, fmt.Errorf("无效端口号: %d", targetPort)
	}
	port := uint32(targetPort)

	// classic BPF（cBPF）过滤器，假设链路层为 Ethernet：
	//   - 只放行 IPv4
	//   - 只放行 TCP
	//   - 只放行 src port == port 或 dst port == port
	ins := []bpf.Instruction{
		bpf.LoadAbsolute{Off: 12, Size: 2},                          // EtherType
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 0x0800, SkipFalse: 7}, // IPv4? 否则 drop

		bpf.LoadAbsolute{Off: 23, Size: 1},                       // IPv4 protocol
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: 6, SkipFalse: 5},   // TCP? 否则 drop
		bpf.LoadMemShift{Off: 14},                                 // X = 4*(ip[0]&0xf)

		bpf.LoadIndirect{Off: 14, Size: 2},                        // tcp src port
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: port, SkipTrue: 3},  // src==port -> accept
		bpf.LoadIndirect{Off: 16, Size: 2},                        // tcp dst port
		bpf.JumpIf{Cond: bpf.JumpEqual, Val: port, SkipTrue: 1},  // dst==port -> accept

		bpf.RetConstant{Val: 0},      // drop
		bpf.RetConstant{Val: 0xFFFF}, // accept
	}

	raw, err := bpf.Assemble(ins)
	if err != nil {
		return nil, fmt.Errorf("组装 BPF 失败：%w", err)
	}
	return raw, nil
}

// TCPPort80BPF 保留旧接口，供现有测试和老 DaemonSet 模式使用。
func TCPPort80BPF() ([]bpf.RawInstruction, error) {
	return TCPPortBPF(80)
}

// IPFilter 在用户态提供基于目标 IP 白名单的快速过滤。
// 当 Operator 注入了 TargetIPs 时，只处理与这些 IP 相关的包；
// 列表为空时视为"放行全部"，与旧 DaemonSet 模式兼容。
type IPFilter struct {
	// ipSet 存储目标 IP 的 uint32 表示（网络字节序），用于 O(1) 查找。
	ipSet map[uint32]struct{}
	empty bool // true 表示无需过滤（TargetIPs 为空）
}

// NewIPFilter 根据 IP 字符串切片构建 IPFilter。
// ips 为 nil 或空时返回"放行全部"的过滤器。
func NewIPFilter(ips []string) (*IPFilter, error) {
	if len(ips) == 0 {
		return &IPFilter{empty: true}, nil
	}
	set := make(map[uint32]struct{}, len(ips))
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			return nil, fmt.Errorf("无法解析 IP 地址: %q", ipStr)
		}
		ip4 := ip.To4()
		if ip4 == nil {
			return nil, fmt.Errorf("仅支持 IPv4 地址: %q", ipStr)
		}
		set[binary.BigEndian.Uint32(ip4)] = struct{}{}
	}
	return &IPFilter{ipSet: set, empty: false}, nil
}

// Allow 判断给定的 src/dst IP 组合是否命中白名单。
// 只要 srcIP 或 dstIP 任意一个在列表中，即放行（双向流量都能通过）。
func (f *IPFilter) Allow(srcIP, dstIP string) bool {
	if f.empty {
		return true
	}
	if f.matchIP(srcIP) || f.matchIP(dstIP) {
		return true
	}
	return false
}

func (f *IPFilter) matchIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	_, ok := f.ipSet[binary.BigEndian.Uint32(ip4)]
	return ok
}
