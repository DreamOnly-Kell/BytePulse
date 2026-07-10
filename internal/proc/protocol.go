// Shared protocol label helpers for connection samplers (darwin/windows).
// 连接采样器共用的协议标签辅助（darwin/windows）。
package proc

// protocolName maps socket type (and optional TCP status) to "tcp" or "udp".
// SOCK_STREAM=1 and SOCK_DGRAM=2 on major OSes including darwin and windows.
// protocolName 将套接字类型（及可选 TCP 状态）映射为 "tcp" 或 "udp"。
// 主流 OS（含 darwin、windows）上 SOCK_STREAM=1、SOCK_DGRAM=2。
func protocolName(socketType uint32, status string) string {
	const (
		sockStream = 1
		sockDgram  = 2
	)
	switch socketType {
	case sockStream:
		return "tcp"
	case sockDgram:
		return "udp"
	default:
		// Heuristic: non-empty status usually means a TCP state.
		// 启发式：非空 status 通常表示 TCP 状态。
		if status != "" {
			return "tcp"
		}
		return "udp"
	}
}
