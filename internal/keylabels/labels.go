// Package keylabels holds display strings for shortcuts shared between App and UI
// (avoids import cycles: home cannot import internal/app).
package keylabels

// KeysTab is the display label for Ctrl+T (SSH Keys tab).
const KeysTab = "C-t"

// KeysTabListHint matches KeysTab (single chord).
const KeysTabListHint = "C-t"

// AIOverlay is the display label for Ctrl+K (AI assistant overlay).
const AIOverlay = "C-k"
