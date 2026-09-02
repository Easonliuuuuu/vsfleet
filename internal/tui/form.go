package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/easonliuuuuu/vsfleet/internal/config"
	"github.com/easonliuuuuu/vsfleet/internal/contextops"
	"github.com/easonliuuuuu/vsfleet/internal/credentials"
	"github.com/easonliuuuuu/vsfleet/internal/vsphere"
)

// formRowKind is what a row does with a key press: text and secret rows take
// keystrokes as characters, select and toggle rows change value on left and
// right, button rows act on enter, and static rows just display something.
type formRowKind int

const (
	rowText formRowKind = iota
	rowSecret
	rowSelect
	rowToggle
	rowButton
	rowStatic
)

// formRow is one line of the form. idx and flag point back into the
// contextForm's own fields, so changing a row's value through the row is the
// same as changing the field directly.
type formRow struct {
	label   string
	kind    formRowKind
	input   *textinput.Model
	options []string
	idx     *int
	flag    *bool
	static  string
	hint    string
	action  func(m *Model) tea.Cmd
}

// contextForm is the state behind adding or editing one context. It holds a
// plain text input per free-text field and a plain int or bool per choice —
// bubbles has no form widget, and a context has few enough fields that one
// is not worth building as a reusable abstraction.
type contextForm struct {
	editing  bool
	origName string // the context being edited; empty for a new one

	name, endpoint, username, password textinput.Model
	datacenter, proxyAddr, proxyUser   textinput.Model
	proxyPass, thumbprint              textinput.Model

	credIdx      int // 0 keyring, 1 prompt
	transportIdx int // 0 direct, 1 socks5
	tlsIdx       int // 0 system, 1 thumbprint, 2 insecure
	remoteDNS    bool
	setCurrent   bool

	cursor      int
	testing     bool
	discovering bool
	saving      bool

	// forceSave appears once a test has failed: the Save row becomes "Save
	// anyway" and, pressed again, saves despite the failure rather than
	// silently retrying the same test forever.
	forceSave bool

	diag *vsphere.Diagnosis
	err  string
	note string
}

func newFormInput(placeholder string, width int) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.CharLimit = 256
	ti.Width = width
	return ti
}

func newFormSecret(width int) textinput.Model {
	ti := newFormInput("", width)
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	return ti
}

// newContextForm builds a blank form, or one prefilled from an existing
// context when edit is non-nil. Renaming is out of scope: the name is fixed
// once a context exists, because it is also the keyring key and the identity
// everything else — the config file, the last-selected state — refers to it
// by.
func newContextForm(edit *contextState) *contextForm {
	f := &contextForm{
		name:       newFormInput("prod", 36),
		endpoint:   newFormInput("https://vcsa.example.internal", 48),
		username:   newFormInput("administrator@vsphere.local", 40),
		password:   newFormSecret(40),
		datacenter: newFormInput("optional", 30),
		proxyAddr:  newFormInput("127.0.0.1:1080", 30),
		proxyUser:  newFormInput("optional", 30),
		proxyPass:  newFormSecret(40),
		thumbprint: newFormInput("", 60),
	}
	if edit == nil {
		return f
	}
	cc := edit.cc
	f.editing = true
	f.origName = cc.Name
	f.name.SetValue(cc.Name)
	f.endpoint.SetValue(cc.Endpoint)
	f.username.SetValue(cc.Username)
	f.datacenter.SetValue(cc.Datacenter)
	if cc.Credential.Scheme == credentials.SchemePrompt {
		f.credIdx = 1
	}
	switch cc.Transport.Type {
	case config.TransportSOCKS5:
		f.transportIdx = 1
	case config.TransportHTTPProxy:
		f.transportIdx = 2
	case config.TransportHTTPSProxy:
		f.transportIdx = 3
	}
	if f.transportIdx != 0 {
		f.proxyAddr.SetValue(cc.Transport.Address)
		f.proxyUser.SetValue(cc.Transport.Username)
		f.remoteDNS = cc.Transport.RemoteDNS
	}
	switch cc.TLS.Mode {
	case config.TLSThumbprint:
		f.tlsIdx = 1
		f.thumbprint.SetValue(cc.TLS.Thumbprint)
	case config.TLSInsecure:
		f.tlsIdx = 2
	}
	return f
}

// rows lays the form out. It is rebuilt on every keystroke rather than cached,
// because which rows exist depends on the current values of others — the
// SOCKS5 fields only make sense once socks5 is chosen, the thumbprint only
// once thumbprint pinning is.
func (f *contextForm) rows() []formRow {
	rows := make([]formRow, 0, 16)
	if f.editing {
		rows = append(rows, formRow{label: "Name", kind: rowStatic, static: f.name.Value()})
	} else {
		rows = append(rows, formRow{label: "Name", kind: rowText, input: &f.name})
	}
	rows = append(rows,
		formRow{label: "Endpoint", kind: rowText, input: &f.endpoint, hint: "e.g. https://vcsa.example.internal"},
		formRow{label: "Username", kind: rowText, input: &f.username, hint: "e.g. administrator@vsphere.local"},
		formRow{label: "Credential", kind: rowSelect, options: []string{"keyring", "prompt"}, idx: &f.credIdx,
			hint: "keyring stores the password in the OS secret store; prompt asks every run"},
	)
	if f.credIdx == 0 {
		label := "Password"
		if f.editing {
			label = "Password (blank keeps the stored one)"
		}
		rows = append(rows, formRow{label: label, kind: rowSecret, input: &f.password})
	}
	rows = append(rows, formRow{label: "Route", kind: rowSelect, options: []string{"direct", "socks5", "http", "https"}, idx: &f.transportIdx})
	if f.transportIdx != 0 {
		rows = append(rows,
			formRow{label: "Proxy address", kind: rowText, input: &f.proxyAddr, hint: "host:port"},
			formRow{label: "Proxy username (optional)", kind: rowText, input: &f.proxyUser},
		)
		if strings.TrimSpace(f.proxyUser.Value()) != "" {
			label := "Proxy password"
			if f.editing {
				label = "Proxy password (blank keeps the stored one)"
			}
			rows = append(rows, formRow{label: label, kind: rowSecret, input: &f.proxyPass})
		}
		if f.transportIdx == 1 {
			rows = append(rows, formRow{label: "Resolve DNS at the proxy", kind: rowToggle, flag: &f.remoteDNS,
				hint: "http and https always resolve at the proxy; only socks5 has a choice"})
		}
	}
	rows = append(rows, formRow{label: "Certificate policy", kind: rowSelect, options: []string{"system", "thumbprint", "insecure"}, idx: &f.tlsIdx})
	if f.tlsIdx == 1 {
		rows = append(rows,
			formRow{label: "Thumbprint", kind: rowText, input: &f.thumbprint, hint: "SHA-256 or SHA-1, or use Discover below"},
			formRow{label: "", kind: rowButton, static: "Discover from the server", action: (*Model).formDiscover},
		)
	}
	rows = append(rows,
		formRow{label: "Default datacenter (optional)", kind: rowText, input: &f.datacenter},
		formRow{label: "Make this the current context", kind: rowToggle, flag: &f.setCurrent},
	)
	saveLabel := "Save"
	if f.forceSave {
		saveLabel = "Save anyway"
	}
	rows = append(rows,
		formRow{label: "", kind: rowButton, static: "Test connection", action: (*Model).formTest},
		formRow{label: "", kind: rowButton, static: saveLabel, action: (*Model).formSave},
		formRow{label: "", kind: rowButton, static: "Cancel", action: (*Model).formCancel},
	)
	return rows
}

// syncFocus gives the textinput under the cursor keyboard focus and takes it
// away from every other one: an unfocused textinput.Model silently discards
// key presses, so exactly one row must be focused at a time.
func (f *contextForm) syncFocus() {
	rows := f.rows()
	if f.cursor >= len(rows) {
		f.cursor = len(rows) - 1
	}
	if f.cursor < 0 {
		f.cursor = 0
	}
	for i, r := range rows {
		if r.input == nil {
			continue
		}
		if i == f.cursor {
			r.input.Focus()
		} else {
			r.input.Blur()
		}
	}
}

// input builds the contextops.Input the backend actually acts on.
func (f *contextForm) input() contextops.Input {
	in := contextops.Input{
		Name:       strings.TrimSpace(f.name.Value()),
		Endpoint:   strings.TrimSpace(f.endpoint.Value()),
		Username:   strings.TrimSpace(f.username.Value()),
		Datacenter: strings.TrimSpace(f.datacenter.Value()),
		SetCurrent: f.setCurrent,
		Replace:    f.editing,
	}
	if f.editing {
		in.Name = f.origName
	}
	switch f.transportIdx {
	case 1, 2, 3:
		routeType := map[int]string{1: config.TransportSOCKS5, 2: config.TransportHTTPProxy, 3: config.TransportHTTPSProxy}[f.transportIdx]
		in.Transport = config.TransportConfig{
			Type:      routeType,
			Address:   strings.TrimSpace(f.proxyAddr.Value()),
			Username:  strings.TrimSpace(f.proxyUser.Value()),
			RemoteDNS: f.transportIdx == 1 && f.remoteDNS,
		}
		if in.Transport.Username != "" {
			if pw := f.proxyPass.Value(); pw != "" {
				in.ProxyPassword, in.HaveProxyPassword = pw, true
			}
		}
	default:
		in.Transport = config.TransportConfig{Type: config.TransportDirect}
	}
	switch f.tlsIdx {
	case 1:
		in.TLS = config.TLSConfig{Mode: config.TLSThumbprint, Thumbprint: strings.TrimSpace(f.thumbprint.Value())}
	case 2:
		in.TLS = config.TLSConfig{Mode: config.TLSInsecure}
	default:
		in.TLS = config.TLSConfig{Mode: config.TLSSystem}
	}
	if f.credIdx == 1 {
		in.Credential = credentials.Ref{Scheme: credentials.SchemePrompt}
	} else {
		in.Credential = credentials.Ref{Scheme: credentials.SchemeKeyring, Value: in.Name}
		if pw := f.password.Value(); pw != "" {
			in.Password, in.HavePassword = pw, true
		}
	}
	return in
}

// validate catches what does not need the network before a test or save is
// attempted, so a missing field is reported instantly rather than after a
// round trip.
func (f *contextForm) validate() string {
	in := f.input()
	switch {
	case in.Name == "":
		return "name is required"
	case in.Endpoint == "":
		return "endpoint is required"
	case in.Username == "":
		return "username is required"
	case in.Transport.Type != config.TransportDirect && in.Transport.Address == "":
		return "proxy address is required"
	case in.TLS.Mode == config.TLSThumbprint && in.TLS.Thumbprint == "":
		return "thumbprint is required in thumbprint mode — use Discover, or switch policy"
	default:
		return ""
	}
}

// enterForm opens the add/edit form. edit is nil for a new context.
func (m *Model) enterForm(edit *contextState) tea.Cmd {
	m.form = newContextForm(edit)
	m.mode = modeForm
	m.form.syncFocus()
	return textinput.Blink
}

func (m *Model) formTest() tea.Cmd {
	f := m.form
	if f == nil || f.testing {
		return nil
	}
	if err := f.validate(); err != "" {
		f.err = err
		return nil
	}
	f.testing, f.err, f.note = true, "", ""
	return tea.Batch(testFormContext(m.ctx, m.backend, f.input()), m.spin.Tick)
}

func (m *Model) formSave() tea.Cmd {
	f := m.form
	if f == nil || f.saving {
		return nil
	}
	if err := f.validate(); err != "" {
		f.err = err
		return nil
	}
	f.saving, f.err = true, ""
	in := f.input()
	in.SaveOnTestFailure = f.forceSave
	return tea.Batch(saveFormContext(m.ctx, m.backend, in), m.spin.Tick)
}

func (m *Model) formDiscover() tea.Cmd {
	f := m.form
	if f == nil || f.discovering {
		return nil
	}
	cc := contextops.Build(f.input())
	if cc.Endpoint == "" {
		f.err = "set the endpoint before discovering its certificate"
		return nil
	}
	f.discovering, f.err = true, ""
	return tea.Batch(discoverThumbprint(m.ctx, m.backend, cc), m.spin.Tick)
}

// formCancel leaves the form. With no contexts configured there is nothing to
// browse, so cancelling reopens a blank form instead of a screen with no way
// back into it.
func (m *Model) formCancel() tea.Cmd {
	m.form = nil
	if len(m.states) == 0 {
		return m.enterForm(nil)
	}
	m.leaveOverlay()
	return nil
}
