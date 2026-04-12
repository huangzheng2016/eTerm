package syncview

import (
	"charm.land/bubbles/v2/textinput"
	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/security"
	"gorm.io/gorm"
)

const (
	fieldEnabled    = 0
	fieldMode       = 1
	fieldSSHHost    = 2 // selector (Host)
	fieldRemoteBin  = 3
	fieldRemoteDB   = 4
	fieldServerURL  = 5
	fieldAPIKey     = 6
	fieldPassphrase = 7
	fieldInterval   = 8
)

const inputCount = 6

// input array indices
const (
	inRemoteBin  = 0
	inRemoteDB   = 1
	inServerURL  = 2
	inAPIKey     = 3
	inPassphrase = 4
	inInterval   = 5
)

var enableOptions = []string{"Off", "On"}
var modeOptions = []string{"SSH", "HTTP", "HTTPS"}

const inputInnerWidth = 39

type Model struct {
	db        *gorm.DB
	masterKey *security.MasterKeyManager
	inputs    [inputCount]textinput.Model
	enableIdx int
	modeIdx   int
	hostIdx   int // SSH host selector, -1 = none
	hostOpts  []db.Host
	focused   int
	width     int
	height    int
	err       string
	testing   bool
}
