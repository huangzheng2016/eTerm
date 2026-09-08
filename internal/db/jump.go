package db

import (
	"strings"

	"gorm.io/gorm"
)

func JumpChainPointsBackToHost(database *gorm.DB, editorHostID uint, startID uint) bool {
	const maxHops = 32
	cur := startID
	for i := 0; i < maxHops; i++ {
		if cur == editorHostID {
			return true
		}
		var h Host
		if err := database.Select("jump_host_id").First(&h, cur).Error; err != nil {
			return false
		}
		if h.JumpHostID == nil {
			return false
		}
		cur = *h.JumpHostID
	}
	return true
}

func FirstProxyJumpHop(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.Index(s, ","); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func HostPartFromProxyJump(proxyJump string) string {
	hop := FirstProxyJumpHop(proxyJump)
	if i := strings.LastIndex(hop, "@"); i >= 0 {
		return strings.TrimSpace(hop[i+1:])
	}
	return hop
}

func ResolveJumpHostID(database *gorm.DB, proxyJump string) *uint {
	hop := FirstProxyJumpHop(proxyJump)
	if hop == "" {
		return nil
	}
	hopLower := strings.ToLower(hop)
	hostPart := HostPartFromProxyJump(proxyJump)
	hostPartLower := strings.ToLower(hostPart)

	candidates := []string{hopLower}
	if hostPartLower != hopLower {
		candidates = append(candidates, hostPartLower)
	}

	var hosts []Host
	database.Where("LOWER(alias) IN ? OR LOWER(hostname) IN ?", candidates, candidates).Find(&hosts)

	for _, prio := range candidates {
		for _, h := range hosts {
			if strings.ToLower(h.Alias) == prio {
				id := h.ID
				return &id
			}
		}
		for _, h := range hosts {
			if strings.ToLower(h.Hostname) == prio {
				id := h.ID
				return &id
			}
		}
	}
	return nil
}
