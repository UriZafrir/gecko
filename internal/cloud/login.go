package cloud

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"time"
)

// Identity: for now an account is just a name on the relay, and what
// protects it is the approval gate: a machine that asks to join gets
// nothing until a machine already in the account approves it (or the
// recovery code signs it in). An OIDC provider (Google, GitHub, Apple)
// would plug in where the relay accepts /v1/enroll and /v1/account: the
// request would carry an ID token, and the relay would map the token's
// subject to the account instead of trusting the typed name. Nothing on
// the daemon side changes, since daemons trust the signed device list, not
// the relay's idea of who is logged in.

var accountName = regexp.MustCompile(`^[a-z0-9][a-z0-9._@+-]{0,63}$`)

// ValidAccount reports whether name can name an account.
func ValidAccount(name string) bool { return accountName.MatchString(name) }

// LoginOptions configure Login.
type LoginOptions struct {
	Relay       string
	Account     string
	Name        string // this machine's name
	Recovery    string // join with the recovery code instead of waiting for an approval
	Fingerprint string // the account fingerprint to expect (optional, from `gecko whoami` elsewhere)
	Out         io.Writer
	Poll        time.Duration // how often to ask whether we were approved (default 2s)
	Dir         string        // where the keys and account go (default Dir())
}

type listReq struct {
	List *List `json:"list"`
}

type chainReply struct {
	Lists []List `json:"lists"`
}

type pendingReply struct {
	Pending []Pending `json:"pending"`
}

// Login signs this machine in to an account on a relay. A new account is
// created with this machine as its first trusted one; for an existing
// account it waits until another machine approves this one.
func Login(o LoginOptions) (*Account, error) {
	if o.Dir == "" {
		o.Dir = Dir()
	}
	if a, _ := LoadAccount(o.Dir); a != nil {
		return nil, fmt.Errorf("already signed in to %s as %s; run gecko logout first", a.Relay, a.Name)
	}
	if !ValidAccount(o.Account) {
		return nil, fmt.Errorf("bad account name %q: use lower-case letters, digits and . _ @ + -", o.Account)
	}
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.Poll == 0 {
		o.Poll = 2 * time.Second
	}
	keys, err := LoadKeys(o.Dir, true)
	if err != nil {
		return nil, err
	}
	me := keys.Device(o.Name, time.Now().UnixMilli())
	if o.Recovery != "" {
		return loginRecovery(o, keys, me)
	}
	rc, err := newRelayClient(o.Relay, keys.Signer())
	if err != nil {
		return nil, err
	}
	for {
		var r EnrollReply
		if err := rc.do("POST", "/v1/enroll", EnrollReq{Account: o.Account, Device: me}, &r); err != nil {
			return nil, err
		}
		switch r.Status {
		case "new":
			a, err := createAccount(rc, o, keys, me)
			if isStatus(err, 409) {
				continue // someone created it a moment ago; ask to join instead
			}
			return a, err
		case "approved":
			return join(rc, o, keys, me)
		case "pending":
			if r.Code != PairingCode(&me) {
				return nil, errors.New("the relay showed a code that doesn't match this machine's keys; not trusting it")
			}
			return waitApproval(rc, o, keys, me, r)
		default:
			return nil, fmt.Errorf("relay: unexpected answer %q", r.Status)
		}
	}
}

func createAccount(rc *relayClient, o LoginOptions, keys *Keys, me Device) (*Account, error) {
	code := NewRecoveryCode()
	g := &List{Account: o.Account, Version: 1, Devices: []Device{me, RecoveryDevice(code, me.Added)}}
	s := keys.Signer()
	g.Sign(s.ID, s.Key)
	if err := rc.do("POST", "/v1/account", listReq{List: g}, nil); err != nil {
		return nil, err
	}
	a := &Account{Relay: rc.base.String(), Name: o.Account, Device: me.ID, Fingerprint: Fingerprint(g), List: g, dir: o.Dir}
	if err := a.save(); err != nil {
		return nil, err
	}
	fmt.Fprintf(o.Out, "Created account %s on %s. %s is its first trusted machine.\n", o.Account, rc.base.Host, me.Name)
	fmt.Fprintf(o.Out, "Account fingerprint: %s\n\n", a.Fingerprint)
	fmt.Fprintf(o.Out, "Recovery code (shown only now; keep it somewhere safe):\n\n    %s\n\n", code)
	fmt.Fprintf(o.Out, "With it you can add a machine when no signed-in machine is around:\n")
	fmt.Fprintf(o.Out, "  gecko login --relay %s --account %s --recovery <code>\n", rc.base, o.Account)
	return a, nil
}

func waitApproval(rc *relayClient, o LoginOptions, keys *Keys, me Device, r EnrollReply) (*Account, error) {
	mins := int(time.Until(time.UnixMilli(r.Expires)).Round(time.Minute).Minutes())
	fmt.Fprintf(o.Out, "Account %s already exists, so a machine that is signed in to it has to approve this one.\n\n", o.Account)
	fmt.Fprintf(o.Out, "    code: %s   (expires in %d minutes)\n\n", r.Code, mins)
	fmt.Fprintf(o.Out, "Approve it in Gecko on that machine, or run there:  gecko approve %s\n", r.Code)
	fmt.Fprintf(o.Out, "Waiting for approval...\n")
	deadline := time.UnixMilli(r.Expires).Add(30 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(o.Poll)
		var st EnrollReply
		if err := rc.do("GET", "/v1/enroll", nil, &st); err != nil {
			var ae *apiError
			if errors.As(err, &ae) && ae.Status/100 == 4 {
				return nil, err
			}
			continue // the network or the relay hiccupped; keep waiting
		}
		switch st.Status {
		case "approved":
			return join(rc, o, keys, me)
		case "gone":
			return nil, errors.New("the request was denied or expired; run gecko login again")
		}
	}
	return nil, errors.New("the code expired before anyone approved it; run gecko login again")
}

// join fetches the account's device list after this machine was added,
// checks every version from the first, and saves it.
func join(rc *relayClient, o LoginOptions, keys *Keys, me Device) (*Account, error) {
	var cr chainReply
	if err := rc.do("GET", "/v1/chain", nil, &cr); err != nil {
		return nil, err
	}
	latest, err := VerifyChain(cr.Lists)
	if err != nil {
		return nil, fmt.Errorf("the relay sent a device list that doesn't check out: %w", err)
	}
	d := latest.Find(me.ID)
	if d == nil || string(d.Noise) != string(keys.NoisePub) || latest.Account != o.Account {
		return nil, errors.New("the relay says this machine was approved, but the signed device list doesn't include it")
	}
	a := &Account{Relay: rc.base.String(), Name: o.Account, Device: me.ID, Fingerprint: Fingerprint(&cr.Lists[0]), List: latest, dir: o.Dir}
	if o.Fingerprint != "" && NormalizeCode(o.Fingerprint) != NormalizeCode(a.Fingerprint) {
		return nil, fmt.Errorf("account fingerprint is %s, not the %s you expected; not signing in", a.Fingerprint, o.Fingerprint)
	}
	if err := a.save(); err != nil {
		return nil, err
	}
	by := "a machine"
	if b := latest.Find(d.By); b != nil {
		by = b.Name
	}
	fmt.Fprintf(o.Out, "Approved by %s. %s is now in account %s.\n", by, me.Name, o.Account)
	fmt.Fprintf(o.Out, "Account fingerprint: %s (gecko whoami on %s shows the same one)\n", a.Fingerprint, by)
	return a, nil
}

// loginRecovery adds this machine with the recovery code's key, no
// approval needed.
func loginRecovery(o LoginOptions, keys *Keys, me Device) (*Account, error) {
	rk := RecoveryKey(o.Recovery)
	rs := Signer{ID: DeviceID(rk.Public().(ed25519.PublicKey)), Key: rk}
	rc, err := newRelayClient(o.Relay, rs)
	if err != nil {
		return nil, err
	}
	var cr chainReply
	if err := rc.do("GET", "/v1/chain", nil, &cr); err != nil {
		if isStatus(err, 401) || isStatus(err, 403) {
			return nil, errors.New("that recovery code doesn't belong to any account on this relay")
		}
		return nil, err
	}
	latest, err := VerifyChain(cr.Lists)
	if err != nil {
		return nil, fmt.Errorf("the relay sent a device list that doesn't check out: %w", err)
	}
	if latest.Account != o.Account || latest.Find(rs.ID) == nil {
		return nil, fmt.Errorf("that recovery code doesn't belong to account %s", o.Account)
	}
	fp := Fingerprint(&cr.Lists[0])
	if o.Fingerprint != "" && NormalizeCode(o.Fingerprint) != NormalizeCode(fp) {
		return nil, fmt.Errorf("account fingerprint is %s, not the %s you expected; not signing in", fp, o.Fingerprint)
	}
	me.By = rs.ID
	added, err := publish(rc, latest, rs, func(l *List) error {
		l.Devices = append(without(l.Devices, me.ID), me)
		return nil
	})
	if err != nil {
		return nil, err
	}
	a := &Account{Relay: rc.base.String(), Name: o.Account, Device: me.ID, Fingerprint: fp, List: &added[len(added)-1], dir: o.Dir}
	if err := a.save(); err != nil {
		return nil, err
	}
	fmt.Fprintf(o.Out, "Signed in to %s with the recovery code. Account fingerprint: %s\n", o.Account, fp)
	return a, nil
}

func without(ds []Device, id string) []Device {
	out := make([]Device, 0, len(ds))
	for _, d := range ds {
		if d.ID != id {
			out = append(out, d)
		}
	}
	return out
}

// publish signs and posts the next version of the device list, made by
// edit from the latest one. It catches up with the relay first and tries
// again if another machine published at the same moment. It returns every
// version after cur, verified, the new one last.
func publish(rc *relayClient, cur *List, s Signer, edit func(*List) error) ([]List, error) {
	var out []List
	for try := 0; try < 4; try++ {
		var cr chainReply
		if err := rc.do("GET", "/v1/chain?since="+strconv.Itoa(cur.Version), nil, &cr); err != nil {
			return nil, err
		}
		for i := range cr.Lists {
			if err := Verify(cur, &cr.Lists[i]); err != nil {
				return nil, fmt.Errorf("the relay sent a device list that doesn't check out: %w", err)
			}
			cur = &cr.Lists[i]
			out = append(out, *cur)
		}
		next := cur.Next()
		if err := edit(next); err != nil {
			return nil, err
		}
		next.Sign(s.ID, s.Key)
		err := rc.do("POST", "/v1/list", listReq{List: next}, nil)
		if isStatus(err, 409) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return append(out, *next), nil
	}
	return nil, errors.New("the device list keeps changing under us; try again")
}

// Logout signs this machine out when its daemon isn't running: it removes
// itself from the account's list (best effort) and deletes its keys.
func Logout() error {
	dir := Dir()
	a, err := LoadAccount(dir)
	if err != nil || a == nil {
		_ = forget(dir)
		return err
	}
	if keys, err := LoadKeys(dir, false); err == nil {
		removeSelf(a, keys)
	}
	return forget(dir)
}

func removeSelf(a *Account, keys *Keys) {
	rc, err := newRelayClient(a.Relay, keys.Signer())
	if err != nil {
		return
	}
	_, _ = publish(rc, a.List, keys.Signer(), func(l *List) error {
		l.Devices = without(l.Devices, a.Device)
		return nil
	})
}
