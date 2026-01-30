package capture

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/afpacket"
	"golang.org/x/net/bpf"
)

type AFPacketHandle struct {
	tp *afpacket.TPacket
}

func NewAFPacketHandle(iface string, snaplen int) (*AFPacketHandle, error) {
	if iface == "" {
		return nil, fmt.Errorf("interface 不能为空")
	}

	frameSize := nextPow2(snaplen)
	if frameSize < 2048 {
		frameSize = 2048
	}
	if frameSize > 1<<16 {
		frameSize = 1 << 16
	}

	blockSize := 1 << 20
	if blockSize%frameSize != 0 {
		blockSize = frameSize * 16
	}

	// AF_PACKET 是 Linux 原生抓包机制：在数据链路层直接读取网卡收发的原始帧（以太网帧）。
	// 这里使用 mmap + TPACKET_V3 方式提高吞吐（gopacket/afpacket 内部会自动选择合适版本）。
	var tp *afpacket.TPacket
	var err error
	if iface == "any" {
		tp, err = afpacket.NewTPacket(
			afpacket.OptFrameSize(frameSize),
			afpacket.OptBlockSize(blockSize),
			afpacket.OptNumBlocks(64),
			afpacket.OptPollTimeout(250*time.Millisecond),
		)
	} else {
		tp, err = afpacket.NewTPacket(
			afpacket.OptInterface(iface),
			afpacket.OptFrameSize(frameSize),
			afpacket.OptBlockSize(blockSize),
			afpacket.OptNumBlocks(64),
			afpacket.OptPollTimeout(250*time.Millisecond),
		)
	}
	if err != nil {
		if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
			return nil, fmt.Errorf("打开 AF_PACKET 失败：%w（需要 root 或 CAP_NET_RAW）", err)
		}
		if _, ok := err.(*net.OpError); ok {
			if iface == "any" {
				return nil, fmt.Errorf("打开 AF_PACKET 失败：%w", err)
			}
			return nil, fmt.Errorf("打开 AF_PACKET 失败：%w（检查网卡名是否存在：%s）", err, iface)
		}
		return nil, fmt.Errorf("打开 AF_PACKET 失败：%w", err)
	}

	return &AFPacketHandle{tp: tp}, nil
}

func nextPow2(v int) int {
	if v <= 1 {
		return 1
	}
	n := 1
	for n < v {
		n <<= 1
	}
	return n
}

func (h *AFPacketHandle) Close() {
	if h.tp != nil {
		h.tp.Close()
	}
}

func (h *AFPacketHandle) SetBPF(ins []bpf.RawInstruction) error {
	if h.tp == nil {
		return os.ErrInvalid
	}
	return h.tp.SetBPF(ins)
}

func (h *AFPacketHandle) ReadPacket(ctx context.Context) ([]byte, gopacket.CaptureInfo, error) {
	if h.tp == nil {
		return nil, gopacket.CaptureInfo{}, os.ErrInvalid
	}

	// gopacket/afpacket 的 Read 会在 poll 超时后返回错误；这里按 ctx 控制退出。
	for {
		data, ci, err := h.tp.ZeroCopyReadPacketData()
		if err == nil {
			return data, ci, nil
		}
		if ctx.Err() != nil {
			return nil, gopacket.CaptureInfo{}, ctx.Err()
		}
	}
}
