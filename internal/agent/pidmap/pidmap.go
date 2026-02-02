package pidmap

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/btf"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
)

type Resolver struct {
	m    *ebpf.Map
	prog *ebpf.Program
	tp   link.Link
    debugDumped bool
}

type flowKey struct {
	SrcIP   uint32
	DstIP   uint32
	SrcPort uint16
	DstPort uint16
	Pad     uint32
}

type offsets struct {
	family   int16
	newstate int16
	sport    int16
	dport    int16
	saddr    int16
	daddr    int16
}

func NewResolver() (*Resolver, error) {
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("设置 memlock 失败：%w", err)
	}
	spec, err := btf.LoadKernelSpec()
	if err != nil {
		return nil, fmt.Errorf("加载 BTF 失败：%w", err)
	}
	var st *btf.Struct
	if err := spec.TypeByName("trace_event_raw_inet_sock_set_state", &st); err != nil {
		return nil, fmt.Errorf("查找 tracepoint 结构失败：%w", err)
	}
	off, err := resolveOffsets(st)
	if err != nil {
		return nil, err
	}
	m, err := ebpf.NewMap(&ebpf.MapSpec{
		Name:       "flow_pid_map",
		Type:       ebpf.Hash,
		KeySize:    16,
		ValueSize:  4,
		MaxEntries: 65535,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 map 失败：%w", err)
	}
	ins := buildProgram(m, off)
	prog, err := ebpf.NewProgram(&ebpf.ProgramSpec{
		Type:         ebpf.TracePoint,
		Instructions: ins,
		License:      "GPL",
	})
	if err != nil {
		m.Close()
		return nil, fmt.Errorf("加载 eBPF 程序失败：%w", err)
	}
	tp, err := link.Tracepoint("sock", "inet_sock_set_state", prog, nil)
	if err != nil {
		prog.Close()
		m.Close()
		return nil, fmt.Errorf("挂载 tracepoint 失败：%w", err)
	}
	return &Resolver{m: m, prog: prog, tp: tp}, nil
}

func (r *Resolver) Lookup(srcIP string, srcPort int, dstIP string, dstPort int) int {
	if r == nil || r.m == nil {
		return 0
	}
	var pid uint32
	keyNet, ok := makeKeyNet(srcIP, srcPort, dstIP, dstPort)
	if ok {
		if err := r.m.Lookup(&keyNet, &pid); err == nil {
			return int(pid)
		} else {
            // Debug logging for lookup failure
            // Only log if it's NOT a "not found" error to avoid spam, or log sparingly?
            // Actually, if user sees 0, it IS "not found".
            // Let's log the first few failures or just log it.
            // log.Printf("Lookup failed for Net Key: Src=%s:%d Dst=%s:%d Key=%+v Err=%v", srcIP, srcPort, dstIP, dstPort, keyNet, err)
		}
	}
	keyHost, ok := makeKeyHost(srcIP, srcPort, dstIP, dstPort)
	if ok {
		if err := r.m.Lookup(&keyHost, &pid); err == nil {
			return int(pid)
		}
	}
    
    if pid == 0 && !r.debugDumped {
		keyNetHex := fmt.Sprintf("%08x %08x %04x %04x", keyNet.SrcIP, keyNet.DstIP, keyNet.SrcPort, keyNet.DstPort)
		keyHostHex := fmt.Sprintf("%08x %08x %04x %04x", keyHost.SrcIP, keyHost.DstIP, keyHost.SrcPort, keyHost.DstPort)
		log.Printf("Lookup failed for %s:%d -> %s:%d. Dumping Map...", srcIP, srcPort, dstIP, dstPort)
		log.Printf("Tried KeyNet (LE): %s", keyNetHex)
		log.Printf("Tried KeyHost (LE): %s", keyHostHex)
		r.DebugDump()
		r.debugDumped = true
	}

	return int(pid)
}

func (r *Resolver) DebugDump() {
	if r == nil || r.m == nil {
		return
	}
	var key flowKey
	var val uint32
	iter := r.m.Iterate()
	log.Println("--- BPF Map Dump Start ---")
	count := 0
	for iter.Next(&key, &val) {
		srcIP := make(net.IP, 4)
		binary.LittleEndian.PutUint32(srcIP, key.SrcIP)
		dstIP := make(net.IP, 4)
		binary.LittleEndian.PutUint32(dstIP, key.DstIP)
		srcPort := toNetPort(key.SrcPort)
		dstPort := toNetPort(key.DstPort)
		
		keyHex := fmt.Sprintf("%08x %08x %04x %04x", key.SrcIP, key.DstIP, key.SrcPort, key.DstPort)
		log.Printf("Map Entry: %s:%d -> %s:%d | PID: %d | RawKey: %s", srcIP, srcPort, dstIP, dstPort, val, keyHex)
		count++
		if count >= 20 {
			log.Println("... (truncated)")
			break
		}
	}
	log.Println("--- BPF Map Dump End ---")
}

func (r *Resolver) Close() error {
	var firstErr error
	if r.tp != nil {
		if err := r.tp.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if r.prog != nil {
		if err := r.prog.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if r.m != nil {
		if err := r.m.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func makeKeyNet(srcIP string, srcPort int, dstIP string, dstPort int) (flowKey, bool) {
	sip := net.ParseIP(srcIP).To4()
	dip := net.ParseIP(dstIP).To4()
	if sip == nil || dip == nil {
		return flowKey{}, false
	}
	return flowKey{
		SrcIP:   binary.LittleEndian.Uint32(sip),
		DstIP:   binary.LittleEndian.Uint32(dip),
		SrcPort: toNetPort(uint16(srcPort)),
		DstPort: toNetPort(uint16(dstPort)),
	}, true
}

func makeKeyHost(srcIP string, srcPort int, dstIP string, dstPort int) (flowKey, bool) {
	sip := net.ParseIP(srcIP).To4()
	dip := net.ParseIP(dstIP).To4()
	if sip == nil || dip == nil {
		return flowKey{}, false
	}
	return flowKey{
		SrcIP:   binary.LittleEndian.Uint32(sip),
		DstIP:   binary.LittleEndian.Uint32(dip),
		SrcPort: uint16(srcPort),
		DstPort: uint16(dstPort),
	}, true
}

func toNetPort(p uint16) uint16 {
	return (p << 8) | (p >> 8)
}

func resolveOffsets(st *btf.Struct) (offsets, error) {
	var out offsets
	var err error
	if out.family, err = memberOffset(st, "family"); err != nil {
		return offsets{}, err
	}
	if out.newstate, err = memberOffset(st, "newstate"); err != nil {
		return offsets{}, err
	}
	if out.sport, err = memberOffset(st, "sport"); err != nil {
		return offsets{}, err
	}
	if out.dport, err = memberOffset(st, "dport"); err != nil {
		return offsets{}, err
	}
	if out.saddr, err = memberOffset(st, "saddr"); err != nil {
		return offsets{}, err
	}
	if out.daddr, err = memberOffset(st, "daddr"); err != nil {
		return offsets{}, err
	}
	return out, nil
}

func memberOffset(st *btf.Struct, name string) (int16, error) {
	for _, m := range st.Members {
		if m.Name == name {
			return int16(m.Offset / 8), nil
		}
	}
	return 0, fmt.Errorf("成员缺失：%s", name)
}

func buildProgram(m *ebpf.Map, off offsets) asm.Instructions {
	const (
		afInet          = 2
		tcpEstablished  = 1
		httpPortNet     = 0x5000
		httpPortHost    = 80
		altPortNet      = 0x901F // 8080 swapped
		altPortHost     = 8080
		keyOffset       = -32
		valueOffset     = -16
		keySrcIPOffset  = keyOffset
		keyDstIPOffset  = keyOffset + 4
		keySrcPOffset   = keyOffset + 8
		keyDstPOffset   = keyOffset + 10
		keyPadOffset    = keyOffset + 12
	)
	return asm.Instructions{
		asm.Mov.Reg(asm.R6, asm.R1),
		asm.LoadMem(asm.R1, asm.R6, off.family, asm.Half),
		asm.JNE.Imm(asm.R1, afInet, "exit"),
		asm.LoadMem(asm.R1, asm.R6, off.newstate, asm.Word),
		asm.JNE.Imm(asm.R1, tcpEstablished, "exit"),
		asm.LoadMem(asm.R2, asm.R6, off.sport, asm.Half),
		asm.LoadMem(asm.R3, asm.R6, off.dport, asm.Half),
		asm.JEq.Imm(asm.R2, httpPortNet, "match"),
		asm.JEq.Imm(asm.R2, httpPortHost, "match"),
		asm.JEq.Imm(asm.R2, altPortNet, "match"),
		asm.JEq.Imm(asm.R2, altPortHost, "match"),
		asm.JEq.Imm(asm.R3, httpPortNet, "match"),
		asm.JEq.Imm(asm.R3, httpPortHost, "match"),
		asm.JEq.Imm(asm.R3, altPortNet, "match"),
		asm.JEq.Imm(asm.R3, altPortHost, "match"),
		asm.Ja.Label("exit"),
		asm.Mov.Imm(asm.R0, 0).WithSymbol("match"),
		asm.LoadMem(asm.R4, asm.R6, off.saddr, asm.Word),
		asm.LoadMem(asm.R5, asm.R6, off.daddr, asm.Word),
		asm.StoreMem(asm.RFP, keySrcIPOffset, asm.R4, asm.Word),
		asm.StoreMem(asm.RFP, keyDstIPOffset, asm.R5, asm.Word),
		asm.StoreMem(asm.RFP, keySrcPOffset, asm.R2, asm.Half),
		asm.StoreMem(asm.RFP, keyDstPOffset, asm.R3, asm.Half),
		asm.StoreImm(asm.RFP, keyPadOffset, 0, asm.Word),
		asm.FnGetCurrentPidTgid.Call(),
		asm.RSh.Imm(asm.R0, 32),
		asm.StoreMem(asm.RFP, valueOffset, asm.R0, asm.Word),
		asm.LoadMapPtr(asm.R1, m.FD()),
		asm.Mov.Reg(asm.R2, asm.RFP),
		asm.Add.Imm(asm.R2, keyOffset),
		asm.Mov.Reg(asm.R3, asm.RFP),
		asm.Add.Imm(asm.R3, valueOffset),
		asm.Mov.Imm(asm.R4, 0),
		asm.FnMapUpdateElem.Call(),
		asm.LoadMem(asm.R2, asm.R6, off.sport, asm.Half),
		asm.LoadMem(asm.R3, asm.R6, off.dport, asm.Half),
		asm.LoadMem(asm.R4, asm.R6, off.saddr, asm.Word),
		asm.LoadMem(asm.R5, asm.R6, off.daddr, asm.Word),
		asm.StoreMem(asm.RFP, keySrcIPOffset, asm.R5, asm.Word),
		asm.StoreMem(asm.RFP, keyDstIPOffset, asm.R4, asm.Word),
		asm.StoreMem(asm.RFP, keySrcPOffset, asm.R3, asm.Half),
		asm.StoreMem(asm.RFP, keyDstPOffset, asm.R2, asm.Half),
		asm.StoreImm(asm.RFP, keyPadOffset, 0, asm.Word),
		asm.LoadMapPtr(asm.R1, m.FD()),
		asm.Mov.Reg(asm.R2, asm.RFP),
		asm.Add.Imm(asm.R2, keyOffset),
		asm.Mov.Reg(asm.R3, asm.RFP),
		asm.Add.Imm(asm.R3, valueOffset),
		asm.Mov.Imm(asm.R4, 0),
		asm.FnMapUpdateElem.Call(),
		asm.Mov.Imm(asm.R0, 0).WithSymbol("exit"),
		asm.Return(),
	}
}
