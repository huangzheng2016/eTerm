package editor

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"gorm.io/gorm"

	"github.com/eterm/eterm/internal/db"
	"github.com/eterm/eterm/internal/security"
)

const (
	aliasField         = 0
	hostnameField      = 1
	portField          = 2
	usernameField      = 3
	authMethodField    = 4
	passwordField      = 5
	keyIDField         = 6
	groupField         = 7
	tagsField          = 8
	descriptionField   = 9
	jumpHostField      = 10
	proxyTypeField     = 11
	proxyHostField     = 12
	proxyPortField     = 13
	proxyUserField     = 14
	proxyPasswordField = 15
	gssapiSourceField  = 16
	krbPrincipalField  = 17
	gssapiKeytabField  = 18
	proxyCommandField  = 19
	forwardAgentField  = 20
	remoteCommandField = 21
	extraOptionsField  = 22
	advancedField      = 23
)

// inputCount is the number of textinput.Model instances in the inputs array.
// This is less than the total field constants above because selector fields
// (authMethodField, keyIDField, jumpHostField, proxyTypeField, gssapiSourceField) don't use a textinput.
const inputCount = 15

// editorInputInnerWidth matches view formStyle width(60) minus Padding(1,3) and label column — see bubbles textinput.placeholderView when Width<=0.
const editorInputInnerWidth = 39

var authOptions = []string{"password", "key", "agent", "interactive", "gssapi"}

var proxyOptions = []string{"none", "http", "socks5"}
var proxyDisplay = []string{"None", "HTTP", "SOCKS5"}

var gssapiSourceOptions = []string{"ccache", "keytab"}
var gssapiSourceDisplay = []string{"Ticket cache (kinit)", "Keytab file"}
var boolOptions = []bool{false, true}
var boolDisplay = []string{"No", "Yes"}

var fieldLabels = map[int]string{
	aliasField:         "Alias",
	hostnameField:      "Hostname",
	portField:          "Port",
	usernameField:      "Username",
	authMethodField:    "Auth Method",
	passwordField:      "Password",
	keyIDField:         "SSH Key",
	groupField:         "Group",
	tagsField:          "Tags",
	descriptionField:   "Description",
	jumpHostField:      "Jump host",
	proxyTypeField:     "Proxy",
	proxyHostField:     "Proxy host",
	proxyPortField:     "Proxy port",
	proxyUserField:     "Proxy user",
	proxyPasswordField: "Proxy password",
	gssapiSourceField:  "GSSAPI source",
	krbPrincipalField:  "Principal",
	gssapiKeytabField:  "Keytab path",
	proxyCommandField:  "ProxyCommand",
	forwardAgentField:  "ForwardAgent",
	remoteCommandField: "RemoteCommand",
	extraOptionsField:  "Extra SSH Options",
	advancedField:      "Advanced SSH",
}

type Model struct {
	inputs          [inputCount]textinput.Model
	remoteCommand   textarea.Model
	extraOptions    textarea.Model
	focused         int
	advancedFocused int
	advancedActive  bool
	host            *db.Host
	db              *gorm.DB
	masterKey       *security.MasterKeyManager
	width           int
	height          int
	err             string
	saving          bool
	authIdx         int
	keyOptions      []db.SSHKey
	keyIdx          int
	jumpHostOptions []db.Host
	jumpIdx         int // -1 = none
	proxyTypeIdx    int
	gssapiSourceIdx int
	forwardAgentIdx int
}

func inputIndexForField(field int) int {
	switch field {
	case aliasField:
		return 0
	case hostnameField:
		return 1
	case portField:
		return 2
	case usernameField:
		return 3
	case passwordField:
		return 4
	case groupField:
		return 5
	case tagsField:
		return 6
	case descriptionField:
		return 7
	case proxyHostField:
		return 8
	case proxyPortField:
		return 9
	case proxyUserField:
		return 10
	case proxyPasswordField:
		return 11
	case krbPrincipalField:
		return 12
	case gssapiKeytabField:
		return 13
	case proxyCommandField:
		return 14
	}
	return -1
}

func fieldUsesTextarea(field int) bool {
	return field == remoteCommandField || field == extraOptionsField
}

func fieldUsesSelector(field int) bool {
	switch field {
	case authMethodField, keyIDField, jumpHostField, proxyTypeField, gssapiSourceField, forwardAgentField:
		return true
	default:
		return false
	}
}

// syncInputWidths sets bubbles textinput width. If Width stays 0, placeholderView only renders the first rune of Placeholder (looks like stray default characters).
func (m *Model) syncInputWidths() {
	w := editorInputInnerWidth
	if m.width > 0 && m.width < 58 {
		w = max(12, m.width-22)
	}
	for i := range m.inputs {
		m.inputs[i].SetWidth(w)
	}
	m.remoteCommand.SetWidth(w)
	m.remoteCommand.SetHeight(3)
	m.extraOptions.SetWidth(w)
	m.extraOptions.SetHeight(4)
}

func proxyTypeFromDB(s string) int {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0
	}
	for i, p := range proxyOptions {
		if p == s {
			return i
		}
	}
	return 0
}

func gssapiSourceFromDB(s string) int {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "ccache" {
		return 0
	}
	if s == "keytab" {
		return 1
	}
	return 0
}

func boolIndex(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (m Model) mainVisibleFields() []int {
	fields := []int{aliasField, hostnameField, portField, usernameField, authMethodField}
	method := authOptions[m.authIdx]
	switch method {
	case "password", "interactive":
		fields = append(fields, passwordField)
	case "key":
		fields = append(fields, keyIDField)
	case "gssapi":
		fields = append(fields, gssapiSourceField)
		if m.gssapiSourceIdx == 1 { // keytab
			fields = append(fields, krbPrincipalField, gssapiKeytabField)
		}
	}
	fields = append(fields, groupField, tagsField, descriptionField, jumpHostField, advancedField)
	return fields
}

func (m Model) advancedVisibleFields() []int {
	fields := []int{forwardAgentField, proxyTypeField}
	if m.proxyTypeIdx > 0 {
		fields = append(fields, proxyHostField, proxyPortField, proxyUserField, proxyPasswordField)
	} else {
		fields = append(fields, proxyCommandField)
	}
	fields = append(fields, remoteCommandField, extraOptionsField)
	return fields
}

func (m Model) activeFields() []int {
	if m.advancedActive {
		return m.advancedVisibleFields()
	}
	return m.mainVisibleFields()
}

func (m Model) advancedSummary() string {
	var parts []string
	if boolOptions[m.forwardAgentIdx] {
		parts = append(parts, "agent")
	}
	if m.proxyTypeIdx > 0 || strings.TrimSpace(m.inputs[inputIndexForField(proxyCommandField)].Value()) != "" {
		parts = append(parts, "proxy")
	}
	if strings.TrimSpace(m.remoteCommand.Value()) != "" {
		parts = append(parts, "remote cmd")
	}
	if strings.TrimSpace(m.extraOptions.Value()) != "" {
		parts = append(parts, "extra opts")
	}
	if len(parts) == 0 {
		return "not set"
	}
	if len(parts) <= 2 {
		return strings.Join(parts, ", ")
	}
	return strings.Join(parts[:2], ", ") + " +" + strconv.Itoa(len(parts)-2)
}

func New(database *gorm.DB, masterKey *security.MasterKeyManager, host *db.Host) Model {
	var inputs [inputCount]textinput.Model
	for i := range inputs {
		inputs[i] = textinput.New()
	}

	inputs[0].Placeholder = "(optional)"
	inputs[1].Placeholder = "192.168.1.1"
	inputs[2].Placeholder = "22"
	inputs[3].Placeholder = "root"
	inputs[4].Placeholder = ""
	inputs[4].EchoMode = textinput.EchoPassword
	inputs[4].EchoCharacter = '*'
	inputs[5].Placeholder = "optional; blank = Default in list"
	inputs[6].Placeholder = "prod, web"
	inputs[7].Placeholder = "Production web server"
	inputs[8].Placeholder = "proxy.example.com"
	inputs[9].Placeholder = "1080"
	inputs[10].Placeholder = "(optional)"
	inputs[11].Placeholder = "(optional)"
	inputs[11].EchoMode = textinput.EchoPassword
	inputs[11].EchoCharacter = '*'
	inputs[12].Placeholder = "user@REALM"
	inputs[13].Placeholder = "/etc/krb5.keytab"
	inputs[14].Placeholder = "ssh -W %h:%p bastion"

	rc := textarea.New()
	rc.ShowLineNumbers = false
	rc.Placeholder = "Optional command to send after login"
	xo := textarea.New()
	xo.ShowLineNumbers = false
	xo.Placeholder = "One SSH option per line"

	authIdx := 0
	keyIdx := -1
	proxyTypeIdx := 0
	gssapiSourceIdx := 0
	forwardAgentIdx := 0

	if host != nil {
		inputs[0].SetValue(host.Alias)
		inputs[1].SetValue(host.Hostname)
		inputs[2].SetValue(strconv.Itoa(host.Port))
		inputs[3].SetValue(host.Username)
		if host.Password != "" {
			k := masterKey.GetKey()
			if k != nil {
				plain, err := security.Decrypt(host.Password, k.Bytes())
				k.Clear()
				if err == nil {
					inputs[4].SetValue(string(plain))
				}
			}
		}
		inputs[5].SetValue(host.Group)
		inputs[6].SetValue(host.Tags)
		inputs[7].SetValue(host.Description)

		for i, opt := range authOptions {
			if opt == host.AuthMethod {
				authIdx = i
				break
			}
		}

		proxyTypeIdx = proxyTypeFromDB(host.ProxyType)
		inputs[8].SetValue(host.ProxyHost)
		if host.ProxyPort > 0 {
			inputs[9].SetValue(strconv.Itoa(host.ProxyPort))
		}
		inputs[10].SetValue(host.ProxyUser)
		if host.ProxyPassword != "" {
			k := masterKey.GetKey()
			if k != nil {
				plain, err := security.Decrypt(host.ProxyPassword, k.Bytes())
				k.Clear()
				if err == nil {
					inputs[11].SetValue(string(plain))
				}
			}
		}

		// GSSAPI fields
		gssapiSourceIdx = gssapiSourceFromDB(host.GSSAPISource)
		inputs[12].SetValue(host.KrbPrincipal)
		inputs[13].SetValue(host.GSSAPIKeytab)
		inputs[14].SetValue(host.ProxyCommand)
		forwardAgentIdx = boolIndex(host.ForwardAgent)
		rc.SetValue(host.RemoteCommand)
		xo.SetValue(host.ExtraSSHOptions)
	}

	inputs[0].Focus()

	out := Model{
		inputs:          inputs,
		remoteCommand:   rc,
		extraOptions:    xo,
		focused:         0,
		host:            host,
		db:              database,
		masterKey:       masterKey,
		authIdx:         authIdx,
		keyIdx:          keyIdx,
		jumpIdx:         -1,
		jumpHostOptions: nil,
		proxyTypeIdx:    proxyTypeIdx,
		gssapiSourceIdx: gssapiSourceIdx,
		forwardAgentIdx: forwardAgentIdx,
	}
	out.syncInputWidths()
	return out
}
