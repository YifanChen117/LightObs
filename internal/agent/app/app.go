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
	if cfg.WatchPort == 0 {
		cfg.WatchPort = 80
	}

	handle, err := capture.NewAFPacketHandle(cfg.Interface, 65535)
	if err != nil {
		return err
	}
	defer handle.Close()

	// 内核态 BPF 过滤：只让目标端口的 TCP 包进入用户态，降低处理开销。
	rawIns, err := filter.TCPPortBPF(cfg.WatchPort)
	if err != nil {
		return err
	}
	if err := handle.SetBPF(rawIns); err != nil {
		return fmt.Errorf("设置 BPF 失败：%w", err)
	}

	// 用户态 IP 白名单过滤：当 Operator 注入了 TargetIPs 时，二次过滤只留目标 Pod 流量。
	// TargetIPs 为空时 IPFilter 自动进入"放行全部"模式，与旧 DaemonSet 行为完全兼容。
	ipFilter, err := filter.NewIPFilter(cfg.TargetIPs)
	if err != nil {
		return fmt.Errorf("构建 IP 过滤器失败：%w", err)
	}

	if len(cfg.TargetIPs) > 0 {
		log.Printf("IP 白名单过滤已启用，目标 IP：%v", cfg.TargetIPs)
	} else {
		log.Printf("IP 白名单过滤未启用，采集所有 %d 端口的流量", cfg.WatchPort)
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

	log.Printf("开始抓包：iface=%s port=%d -> server=%s:%d",
		cfg.Interface, cfg.WatchPort, cfg.ServerIP, cfg.ServerPort)

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

		// 用户态 IP 白名单过滤（Operator 模式下精准匹配目标 Pod）
		if !ipFilter.Allow(ipv4.SrcIP.String(), ipv4.DstIP.String()) {
			continue
		}

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
