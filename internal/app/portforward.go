package app

import (
	"github.com/huangzheng2016/eTerm/internal/db"
	internalssh "github.com/huangzheng2016/eTerm/internal/ssh"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

// startPortForwards loads port forward configs for a host and starts them.
// Closers are attached to the InteractiveSession so they stop when the session ends.
func startPortForwards(database *gorm.DB, client *ssh.Client, hostID uint, is *internalssh.InteractiveSession) {
	var forwards []db.PortForward
	database.Where("host_id = ?", hostID).Find(&forwards)

	for _, fwd := range forwards {
		switch fwd.Direction {
		case "local":
			pfc, err := internalssh.StartLocalForward(client, fwd.LocalPort, fwd.RemoteHost, fwd.RemotePort)
			if err != nil {
				appDebugf("port forward L:%d:%s:%d failed: %v", fwd.LocalPort, fwd.RemoteHost, fwd.RemotePort, err)
				continue
			}
			is.AddCloser(pfc)
			appDebugf("port forward L:%d:%s:%d started", fwd.LocalPort, fwd.RemoteHost, fwd.RemotePort)
		case "remote":
			pfc, err := internalssh.StartRemoteForward(client, fwd.RemotePort, fwd.RemoteHost, fwd.LocalPort)
			if err != nil {
				appDebugf("port forward R:%d:%s:%d failed: %v", fwd.RemotePort, fwd.RemoteHost, fwd.LocalPort, err)
				continue
			}
			is.AddCloser(pfc)
			appDebugf("port forward R:%d:%s:%d started", fwd.RemotePort, fwd.RemoteHost, fwd.LocalPort)
		case "dynamic":
			pfc, err := internalssh.StartDynamicForward(client, fwd.LocalPort)
			if err != nil {
				appDebugf("port forward D:%d (SOCKS5) failed: %v", fwd.LocalPort, err)
				continue
			}
			is.AddCloser(pfc)
			appDebugf("port forward D:%d (SOCKS5) started", fwd.LocalPort)
		}
	}
}
