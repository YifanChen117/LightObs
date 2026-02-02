package app

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"

	"lightobs/internal/agent/capture"
	"lightobs/internal/agent/filter"
	"lightobs/internal/agent/httpmatcher"
	"lightobs/internal/agent/pidmap"
	"lightobs/internal/agent/report"
)

func Run(ctx context.Context, cfg Config) error {
	if cfg.HTTPPostTimeout == 0 {
		cfg.HTTPPostTimeout = 5 * time.Second
	}

	handle, err := capture.NewAFPacketHandle(cfg.Interface, 65535)
	if err != nil {
		return err
	}
	defer handle.Close()

	// 这里使用 classic BPF 直接在内核态过滤，只把 TCP 且端口 80 的包送到用户态。
	// 这样能显著降低用户态解码与 HTTP 匹配的开销，也满足“必须设置 BPF”的要求。
	rawIns, err := filter.TCPPort80BPF()
	if err != nil {
		return err
	}
	if err := handle.SetBPF(rawIns); err != nil {
		return fmt.Errorf("设置 BPF 失败：%w", err)
	}

	rep := report.NewClient(cfg.ServerIP, cfg.ServerPort, cfg.HTTPPostTimeout)
	m := httpmatcher.NewMatcher(cfg.RequestTimeout)
	var resolver *pidmap.Resolver
	if cfg.EnableEBPF {
		r, err := pidmap.NewResolver()
		if err != nil {
			return err
		}
		resolver = r
		defer resolver.Close()
	}

	log.Printf("开始抓包：iface=%s -> server=%s:%d", cfg.Interface, cfg.ServerIP, cfg.ServerPort)

	cleanupTicker := time.NewTicker(2 * time.Second)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-cleanupTicker.C:
			m.Cleanup(time.Now())
		default:
		}

		data, ci, err := handle.ReadPacket(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}

		packet := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.NoCopy)
		ip4 := packet.Layer(layers.LayerTypeIPv4)
		if ip4 == nil {
			continue
		}
		ipv4, _ := ip4.(*layers.IPv4)

		tcpL := packet.Layer(layers.LayerTypeTCP)
		if tcpL == nil {
			continue
		}
		tcp, _ := tcpL.(*layers.TCP)
		if len(tcp.Payload) == 0 {
			continue
		}

		meta := httpmatcher.PacketMeta{
			Timestamp:  ci.Timestamp,
			SrcIP:      ipv4.SrcIP.String(),
			DstIP:      ipv4.DstIP.String(),
			SrcPort:    int(tcp.SrcPort),
			DstPort:    int(tcp.DstPort),
			Payload:    tcp.Payload,
			PacketSize: ci.Length,
		}

		if m.ObserveRequest(meta) {
			continue
		}

		if logEntry, ok := m.ObserveResponse(meta); ok {
			if resolver != nil {
				logEntry.PID = resolver.Lookup(logEntry.SrcIP, logEntry.SrcPort, logEntry.DstIP, logEntry.DstPort)
			}
			if err := rep.Upload(ctx, logEntry); err != nil {
				log.Printf("上报失败（忽略继续抓包）：%v", err)
			}
		}
	}
}
