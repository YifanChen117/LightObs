package app

import "time"

type Config struct {
	Interface       string
	ServerIP        string
	ServerPort      int
	RequestTimeout  time.Duration
	HTTPPostTimeout time.Duration
	EnableEBPF      bool
}
