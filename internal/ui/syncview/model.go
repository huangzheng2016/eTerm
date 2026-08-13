package syncview

import (
	"charm.land/bubbles/v2/textinput"
	"github.com/huangzheng2016/eTerm/internal/db"
	"github.com/huangzheng2016/eTerm/internal/security"
	"gorm.io/gorm"
)

const (
	fieldEnabled     = 0
	fieldMode        = 1
	fieldSSHHost     = 2 // selector (Host)
	fieldRemotePort  = 3
	fieldServerURL   = 4
	fieldInsecureTLS = 5
	fieldAPIKey      = 6
	fieldPassphrase  = 7
	fieldInterval    = 8
)

const inputCount = 5

// input array indices
const (
	inRemotePort = 0
	inServerURL  = 1
	inAPIKey     = 2
	inPassphrase = 3
	inInterval   = 4
)

var enableOptions = []string{"Off", "On"}
var modeOptions = []string{"HTTP", "SSH"}
var insecureOptions = []string{"Off", "On"}

const inputInnerWidth = 39

type Model struct {
	db          *gorm.DB
	masterKey   *security.MasterKeyManager
	inputs      [inputCount]textinput.Model
	enableIdx   int
	modeIdx     int
	insecureIdx int
	hostIdx     int // SSH host selector, -1 = none
	hostOpts    []db.Host
	focused     int
	width       int
	height      int
	err         string
	testing     bool
	// secret field values as loaded from DB; save only overwrites stored
	// secrets when the user actually modified these fields
	loadedAPIKey string
	loadedPass   string
}
